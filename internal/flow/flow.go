// Package flow shares control-flow queries used by the gglint analyzers.
package flow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/cfg"
)

// Key names one variable or one field path: x, x.f, x.f.g. Parens, &, and * are ignored.
type Key struct {
	Root types.Object
	Path string // field names joined by "."
}

func (k Key) zero() bool {
	return k.Root == nil
}

const closeFun = "close"

// KeyOf resolves e to the variable or field path it denotes.
func KeyOf(info *types.Info, e ast.Expr) (Key, bool) {
	if info == nil {
		return Key{}, false
	}
	path := ""
	for e != nil {
		if id, ok := e.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types.Var); ok {
				return Key{Root: v, Path: path}, true
			}
			return Key{}, false
		}
		if sel, ok := e.(*ast.SelectorExpr); ok && info.Selections[sel] == nil {
			return packageRoot(info, sel, path)
		}
		inner, field, ok := keyStep(info, e)
		if !ok {
			return Key{}, false
		}
		if field != "" {
			path = "." + field + path
		}
		e = inner
	}
	return Key{}, false
}

func packageRoot(info *types.Info, sel *ast.SelectorExpr, path string) (Key, bool) {
	xid, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return Key{}, false
	}
	if _, isPkg := info.Uses[xid].(*types.PkgName); !isPkg {
		return Key{}, false
	}
	v, isVar := info.Uses[sel.Sel].(*types.Var)
	if !isVar {
		return Key{}, false
	}
	return Key{Root: v, Path: path}, true
}

func keyStep(info *types.Info, e ast.Expr) (ast.Expr, string, bool) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return x.X, "", true
	case *ast.StarExpr:
		return x.X, "", true
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return x.X, "", true
		}
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil && sel.Kind() == types.FieldVal {
			return x.X, promotionNames(sel, x.Sel.Name), true
		}
	}
	return nil, "", false
}

func promotionNames(sel *types.Selection, fallback string) string {
	idx := sel.Index()
	if len(idx) <= 1 {
		return fallback
	}
	out := ""
	t := sel.Recv()
	for _, i := range idx {
		st, ok := derefType(t).Underlying().(*types.Struct)
		if !ok || i < 0 || i >= st.NumFields() {
			return fallback
		}
		if out != "" {
			out += "."
		}
		out += st.Field(i).Name()
		t = st.Field(i).Type()
	}
	return out
}

// Graph is the control-flow graph of one function with a node index.
type Graph struct {
	fn     ast.Node
	blocks []*cfg.Block
	index  map[ast.Node]*cfg.Block
	rep    map[ast.Node]ast.Node
}

func newGraph(fn ast.Node, c *cfg.CFG) *Graph {
	if c == nil {
		return nil
	}
	g := &Graph{fn: fn, blocks: c.Blocks, index: make(map[ast.Node]*cfg.Block), rep: make(map[ast.Node]ast.Node)}
	for _, b := range c.Blocks {
		for _, n := range b.Nodes {
			g.index[n] = b
		}
	}
	g.mapEntries(fn)
	return g
}

func (g *Graph) mapEntries(fn ast.Node) {
	ast.Inspect(fn, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		s, ok := n.(ast.Stmt)
		if !ok {
			return true
		}
		if _, ok := g.index[s]; ok {
			return true
		}
		if e := g.entryOf(s); e != nil {
			if _, ok := g.index[e]; ok {
				g.rep[s] = e
			}
		}
		return true
	})
}

func (g *Graph) entryOf(s ast.Stmt) ast.Node {
	if s == nil {
		return nil
	}
	if _, ok := g.index[s]; ok {
		return s
	}
	switch s := s.(type) {
	case *ast.IfStmt:
		return g.entryOfIf(s)
	case *ast.ForStmt:
		return g.entryOfFor(s)
	case *ast.RangeStmt:
		return s.X
	default:
		return g.entryOfRest(s)
	}
}

func (g *Graph) entryOfIf(s *ast.IfStmt) ast.Node {
	if s.Init != nil {
		return g.entryOf(s.Init)
	}
	return s.Cond
}

func (g *Graph) entryOfFor(s *ast.ForStmt) ast.Node {
	if s.Init != nil {
		return g.entryOf(s.Init)
	}
	if s.Cond != nil {
		return s.Cond
	}
	if s.Body == nil {
		return nil
	}
	return g.listEntry(s.Body.List)
}

func (g *Graph) entryOfRest(s ast.Stmt) ast.Node {
	switch s := s.(type) {
	case *ast.DeclStmt:
		return declEntry(s)
	case *ast.SwitchStmt:
		if s.Init != nil {
			return g.entryOf(s.Init)
		}
		if s.Tag != nil {
			return s.Tag
		}
		return g.listEntry(s.Body.List)
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			return g.entryOf(s.Init)
		}
		return g.entryOf(s.Assign)
	case *ast.SelectStmt:
		return firstComm(s.Body)
	default:
		return g.entryOfClause(s)
	}
}

func (g *Graph) entryOfClause(s ast.Stmt) ast.Node {
	switch s := s.(type) {
	case *ast.LabeledStmt:
		return g.entryOf(s.Stmt)
	case *ast.BlockStmt:
		return g.listEntry(s.List)
	case *ast.CaseClause:
		if len(s.List) > 0 {
			return s.List[0]
		}
		return g.listEntry(s.Body)
	case *ast.CommClause:
		if s.Comm != nil {
			return s.Comm
		}
		return g.listEntry(s.Body)
	}
	return nil
}

func (g *Graph) listEntry(list []ast.Stmt) ast.Node {
	if len(list) == 0 {
		return nil
	}
	return g.entryOf(list[0])
}

func declEntry(s *ast.DeclStmt) ast.Node {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		return nil
	}
	for _, spec := range d.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			return vs
		}
	}
	return nil
}

func firstComm(body *ast.BlockStmt) ast.Node {
	for _, cc := range body.List {
		c, ok := cc.(*ast.CommClause)
		if !ok || c.Comm == nil {
			continue
		}
		return c.Comm
	}
	return nil
}

// Body returns the body of a function declaration or literal, or nil.
func Body(fn ast.Node) *ast.BlockStmt {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		return fn.Body
	case *ast.FuncLit:
		return fn.Body
	}
	return nil
}

// NewGraph returns nil when fn has no body; fn is *ast.FuncDecl or *ast.FuncLit.
func NewGraph(pass *analysis.Pass, fn ast.Node) *Graph {
	if pass == nil || pass.TypesInfo == nil {
		return nil
	}
	cfgs, ok := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	if !ok || cfgs == nil {
		return nil
	}
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		if fn.Body == nil {
			return nil
		}
		if _, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func); !ok {
			return nil
		}
		return newGraph(fn, cfgs.FuncDecl(fn))
	case *ast.FuncLit:
		return newGraph(fn, cfgs.FuncLit(fn))
	}
	return nil
}

func (g *Graph) locate(n ast.Node) ast.Node {
	if n == nil || g.nested(n) {
		return nil
	}
	if r, ok := g.rep[n]; ok {
		if _, ok := g.index[r]; ok {
			return r
		}
		return nil
	}
	if _, ok := g.index[n]; ok {
		return n
	}
	return g.container(n)
}

func (g *Graph) nested(n ast.Node) bool {
	found := false
	ast.Inspect(g.fn, func(x ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := x.(*ast.FuncLit); ok && lit != g.fn {
			if n != lit && within(lit, n) {
				found = true
				return false
			}
			return false
		}
		return true
	})
	return found
}

func (g *Graph) container(n ast.Node) ast.Node {
	var best ast.Node
	for _, b := range g.blocks {
		for _, c := range b.Nodes {
			if within(c, n) && smaller(c, best) {
				best = c
			}
		}
	}
	return best
}

func within(c, n ast.Node) bool {
	return c.Pos() <= n.Pos() && n.End() <= c.End()
}

func smaller(c, best ast.Node) bool {
	return best == nil || nodeSpan(c) < nodeSpan(best)
}

func nodeSpan(n ast.Node) token.Pos {
	return n.End() - n.Pos()
}

// Reaches reports whether some path leads from the node after from to the given to node without crossing a node for which avoid returns true; avoid may be nil.
func (g *Graph) Reaches(from, to ast.Node, avoid func(ast.Node) bool) bool {
	if g == nil {
		return false
	}
	f := g.locate(from)
	t := g.locate(to)
	if f == nil || t == nil {
		return false
	}
	return g.reach(f, t, avoid)
}

func (g *Graph) reach(from, to ast.Node, avoid func(ast.Node) bool) bool {
	visited := map[ast.Node]bool{}
	stack := g.next(from)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == to {
			return true
		}
		if visited[n] {
			continue
		}
		visited[n] = true
		if avoid != nil && avoid(n) {
			continue
		}
		stack = append(stack, g.next(n)...)
	}
	return false
}

func (g *Graph) next(n ast.Node) []ast.Node {
	b := g.index[n]
	if b == nil {
		return nil
	}
	for i, c := range b.Nodes {
		if c != n {
			continue
		}
		if i+1 < len(b.Nodes) {
			return []ast.Node{b.Nodes[i+1]}
		}
		var out []ast.Node
		seen := map[*cfg.Block]bool{}
		for _, s := range b.Succs {
			g.entries(s, seen, &out)
		}
		return out
	}
	return nil
}

func (g *Graph) entries(b *cfg.Block, seen map[*cfg.Block]bool, out *[]ast.Node) {
	if b == nil || seen[b] {
		return
	}
	seen[b] = true
	if len(b.Nodes) > 0 {
		*out = append(*out, b.Nodes[0])
		return
	}
	for _, s := range b.Succs {
		g.entries(s, seen, out)
	}
}

func (g *Graph) predecessors() map[*cfg.Block][]*cfg.Block {
	out := make(map[*cfg.Block][]*cfg.Block)
	for _, b := range g.blocks {
		for _, s := range b.Succs {
			out[s] = append(out[s], b)
		}
	}
	return out
}

func Forward[S comparable](g *Graph, entry S, transfer func(ast.Node, S) S, join func(S, S) S, visit func(ast.Node, S)) {
	if g == nil || transfer == nil {
		return
	}
	blocks := g.blocks
	if len(blocks) == 0 {
		return
	}
	in := make(map[*cfg.Block]S, len(blocks))
	reached := make(map[*cfg.Block]bool, len(blocks))
	in[blocks[0]] = entry
	reached[blocks[0]] = true
	preds := g.predecessors()
	for propagate(blocks, preds, in, reached, transfer, join) {
	}
	if visit == nil {
		return
	}
	for _, b := range blocks {
		if !reached[b] {
			continue
		}
		s := in[b]
		for _, n := range b.Nodes {
			visit(n, s)
			s = transfer(n, s)
		}
	}
}

func propagate[S comparable](blocks []*cfg.Block, preds map[*cfg.Block][]*cfg.Block, in map[*cfg.Block]S, reached map[*cfg.Block]bool, transfer func(ast.Node, S) S, join func(S, S) S) bool {
	changed := false
	for _, b := range blocks {
		if b == blocks[0] {
			continue
		}
		s, ok := joinReached(preds[b], in, reached, transfer, join)
		if !ok {
			continue
		}
		if !reached[b] || s != in[b] {
			in[b] = s
			reached[b] = true
			changed = true
		}
	}
	return changed
}

func joinReached[S comparable](ps []*cfg.Block, in map[*cfg.Block]S, reached map[*cfg.Block]bool, transfer func(ast.Node, S) S, join func(S, S) S) (S, bool) {
	var s S
	first := true
	for _, p := range ps {
		if !reached[p] {
			continue
		}
		out := applyBlock(p, in[p], transfer)
		if first {
			s = out
			first = false
			continue
		}
		s = join(s, out)
	}
	return s, !first
}

func applyBlock[S comparable](b *cfg.Block, s S, transfer func(ast.Node, S) S) S {
	for _, n := range b.Nodes {
		s = transfer(n, s)
	}
	return s
}
