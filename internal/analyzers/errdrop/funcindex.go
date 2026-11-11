package errdrop

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

type indexedUse struct {
	pos token.Pos
	cur inspector.Cursor
}

type funcIndex struct {
	info       *types.Info
	graph      *flow.Graph
	uses       map[*types.Var][]indexedUse
	assigns    map[*types.Var][]token.Pos
	pathWrites map[*types.Var][]token.Pos
	nilChecks  map[*types.Var][]token.Pos
}

func buildFuncIndex(pass *analysis.Pass, fn inspector.Cursor) *funcIndex {
	idx := &funcIndex{
		info:       pass.TypesInfo,
		graph:      flow.NewGraph(pass, fn.Node()),
		uses:       map[*types.Var][]indexedUse{},
		assigns:    map[*types.Var][]token.Pos{},
		pathWrites: map[*types.Var][]token.Pos{},
		nilChecks:  map[*types.Var][]token.Pos{},
	}
	collectUses(pass, fn, idx)
	collectAssigns(pass, fn, idx)
	collectNilChecks(pass, fn, idx)
	return idx
}

func collectUses(pass *analysis.Pass, fn inspector.Cursor, idx *funcIndex) {
	for cur := range fn.Preorder((*ast.Ident)(nil)) {
		if owner(cur) != fn {
			continue
		}
		id := inspection.As[*ast.Ident](cur.Node())
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
			idx.uses[v] = append(idx.uses[v], indexedUse{pos: id.Pos(), cur: cur})
		}
	}
}

func collectAssigns(pass *analysis.Pass, fn inspector.Cursor, idx *funcIndex) {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.IncDecStmt)(nil), (*ast.RangeStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		for _, v := range assignedVars(pass, cur.Node()) {
			idx.assigns[v] = append(idx.assigns[v], cur.Node().Pos())
		}
		for _, v := range pathVars(pass, cur.Node()) {
			idx.pathWrites[v] = append(idx.pathWrites[v], cur.Node().Pos())
		}
	}
}

func collectNilChecks(pass *analysis.Pass, fn inspector.Cursor, idx *funcIndex) {
	for cur := range fn.Preorder((*ast.BinaryExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		b := inspection.As[*ast.BinaryExpr](cur.Node())
		if b.Op != token.EQL && b.Op != token.NEQ {
			continue
		}
		if v := nilComparedVar(pass, b); v != nil {
			idx.nilChecks[v] = append(idx.nilChecks[v], b.Pos())
		}
	}
}

func assignedVars(pass *analysis.Pass, n ast.Node) []*types.Var {
	switch t := n.(type) {
	case *ast.AssignStmt:
		return assignStmtVars(pass, t)
	case *ast.IncDecStmt:
		return incDecVars(pass, t)
	case *ast.RangeStmt:
		return rangeStmtVars(pass, t)
	}
	return nil
}

func assignStmtVars(pass *analysis.Pass, t *ast.AssignStmt) []*types.Var {
	var out []*types.Var
	for _, lhs := range t.Lhs {
		if v := varOfIdent(pass, lhs); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func incDecVars(pass *analysis.Pass, t *ast.IncDecStmt) []*types.Var {
	if v := varOfIdent(pass, t.X); v != nil {
		return []*types.Var{v}
	}
	return nil
}

func rangeStmtVars(pass *analysis.Pass, t *ast.RangeStmt) []*types.Var {
	if t.Tok != token.ASSIGN {
		return nil
	}
	var out []*types.Var
	for _, e := range []ast.Expr{t.Key, t.Value} {
		if e == nil {
			continue
		}
		if v := varOfIdent(pass, e); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func varOfIdent(pass *analysis.Pass, e ast.Expr) *types.Var {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		return v
	}
	return nil
}

func pathVars(pass *analysis.Pass, n ast.Node) []*types.Var {
	switch t := n.(type) {
	case *ast.AssignStmt:
		return pathStmtVars(pass, t)
	case *ast.IncDecStmt:
		if v := pathRootVar(pass, t.X); v != nil {
			return []*types.Var{v}
		}
	case *ast.RangeStmt:
		if t.Tok != token.ASSIGN {
			return nil
		}
		var out []*types.Var
		for _, e := range []ast.Expr{t.Key, t.Value} {
			if e == nil {
				continue
			}
			if v := pathRootVar(pass, e); v != nil {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

func pathStmtVars(pass *analysis.Pass, t *ast.AssignStmt) []*types.Var {
	var out []*types.Var
	for _, lhs := range t.Lhs {
		if v := pathRootVar(pass, lhs); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func pathRootVar(pass *analysis.Pass, e ast.Expr) *types.Var {
	seen := false
	for {
		child, ok := pathStep(e)
		if !ok {
			break
		}
		e = child
		seen = true
	}
	if !seen {
		return nil
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		return v
	}
	return nil
}

func pathStep(e ast.Expr) (ast.Expr, bool) {
	switch t := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		return t.X, true
	case *ast.IndexExpr:
		return t.X, true
	case *ast.IndexListExpr:
		return t.X, true
	case *ast.StarExpr:
		return t.X, true
	}
	return nil, false
}

func nilComparedVar(pass *analysis.Pass, b *ast.BinaryExpr) *types.Var {
	if v := varIfNilOther(pass, b.X, b.Y); v != nil {
		return v
	}
	return varIfNilOther(pass, b.Y, b.X)
}

func varIfNilOther(pass *analysis.Pass, side, other ast.Expr) *types.Var {
	id, ok := ast.Unparen(side).(*ast.Ident)
	if !ok {
		return nil
	}
	if !isNilIdent(pass.TypesInfo, other) {
		return nil
	}
	if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		return v
	}
	return nil
}

func (idx *funcIndex) firstUse(v *types.Var, after token.Pos) (inspector.Cursor, bool) {
	list := idx.uses[v]
	i := sort.Search(len(list), func(i int) bool { return list[i].pos > after })
	if i >= len(list) {
		return inspector.Cursor{}, false
	}
	return list[i].cur, true
}

type freshMark struct {
	path string
	stmt ast.Node
}

func pathHasPrefix(path, mark string) bool {
	return path == mark || strings.HasPrefix(path, mark+".")
}

func avoidStmt(stmt ast.Node) func(ast.Node) bool {
	return func(n ast.Node) bool { return n == stmt }
}

type coverQuery struct {
	graph *flow.Graph
	call  ast.Node
	cur   ast.Node
}

func freshCovers(fresh []freshMark, path string, q coverQuery) bool {
	for _, m := range fresh {
		if pathHasPrefix(path, m.path) && !q.graph.Reaches(q.call, q.cur, avoidStmt(m.stmt)) {
			return true
		}
	}
	return false
}

type mentionPath struct {
	path   string
	derefs []string
	store  bool
	stmt   ast.Node
}

func rootIsPointer(v *types.Var) bool {
	_, ok := types.Unalias(v.Type()).Underlying().(*types.Pointer)
	return ok
}

func selectsIndirect(info *types.Info, sel *ast.SelectorExpr) bool {
	s, ok := info.Selections[sel]
	return !ok || s.Indirect()
}

func joinFields(fields []string) string {
	return strings.Join(fields, ".")
}

func pathOfMention(info *types.Info, root *types.Var, cur inspector.Cursor) mentionPath {
	child := cur.Node()
	up := cur
	var fields []string
	var derefs []string
	for {
		parent := skipParens(up.Parent())
		sel, ok := parent.Node().(*ast.SelectorExpr)
		if !ok {
			break
		}
		if ast.Unparen(sel.X) != child {
			break
		}
		if selectsIndirect(info, sel) {
			derefs = append(derefs, joinFields(fields))
		}
		fields = append(fields, sel.Sel.Name)
		child = parent.Node()
		up = parent
	}
	path := joinFields(fields)
	if rootIsPointer(root) && path != "" {
		derefs = append(derefs, "")
	}
	mention := mentionPath{path: path, derefs: derefs}
	if as, ok := skipParens(up.Parent()).Node().(*ast.AssignStmt); ok && lhsStoresField(as, child) {
		mention.store = true
		mention.stmt = as
	}
	return mention
}

func storeReadsDecode(store mentionPath, fresh []freshMark, q coverQuery) bool {
	for _, d := range store.derefs {
		if !freshCovers(fresh, d, q) {
			return true
		}
	}
	return false
}

func (idx *funcIndex) firstRead(call *ast.CallExpr, v *types.Var, after token.Pos) (inspector.Cursor, bool) {
	list := idx.uses[v]
	start := sort.Search(len(list), func(i int) bool { return list[i].pos > after })
	var fresh []freshMark
	for i := start; i < len(list); i++ {
		mention := pathOfMention(idx.info, v, list[i].cur)
		q := coverQuery{graph: idx.graph, call: call, cur: list[i].cur.Node()}
		if mention.store {
			if storeReadsDecode(mention, fresh, q) {
				return list[i].cur, true
			}
			fresh = append(fresh, freshMark{path: mention.path, stmt: mention.stmt})
			continue
		}
		if freshCovers(fresh, mention.path, q) {
			continue
		}
		return list[i].cur, true
	}
	return inspector.Cursor{}, false
}

func lhsStoresField(as *ast.AssignStmt, child ast.Node) bool {
	if as.Tok != token.ASSIGN {
		return false
	}
	for _, lhs := range as.Lhs {
		if ast.Unparen(lhs) == child {
			return true
		}
	}
	return false
}

func (idx *funcIndex) hasAssignIn(v *types.Var, from, to token.Pos) bool {
	return hasPosIn(idx.assigns[v], from, to)
}

func (idx *funcIndex) hasNilCheckIn(v *types.Var, from, to token.Pos) bool {
	return hasPosIn(idx.nilChecks[v], from, to)
}

func (idx *funcIndex) hasPathWriteBefore(v *types.Var, before token.Pos) bool {
	list := idx.pathWrites[v]
	return len(list) > 0 && list[0] < before
}

func hasPosIn(list []token.Pos, from, to token.Pos) bool {
	i := sort.Search(len(list), func(i int) bool { return list[i] > from })
	return i < len(list) && list[i] < to
}
