package alias

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/flow"
)

func runMapcopy(pass *analysis.Pass) (any, error) {
	eachFunc(pass, func(fn ast.Node) {
		checkMapcopy(pass, fn)
	})
	return nil, nil
}

type mapDef struct {
	v    *types.Var
	text string
	mkey flow.Key
	pos  token.Pos
	stmt ast.Node
}

func checkMapcopy(pass *analysis.Pass, fn ast.Node) {
	defs := collectMapDefs(pass.TypesInfo, fn)
	if len(defs) == 0 {
		return
	}
	used := collectWholeUses(pass.TypesInfo, fn)
	writes := collectFieldWrites(pass.TypesInfo, fn)
	for _, d := range defs {
		if used[d.v] {
			continue
		}
		for _, st := range writes[d.v] {
			if st.Pos() > d.pos && !storedBack(pass.TypesInfo, fn, d, st) {
				reportMapCopy(pass, st, d)
			}
		}
	}
}

func collectMapDefs(info *types.Info, fn ast.Node) []mapDef {
	var out []mapDef
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if st, ok := n.(*ast.AssignStmt); ok {
			if d, ok := mapAssignDef(info, st); ok {
				out = append(out, d)
			}
		}
		return true
	})
	return out
}

func mapAssignDef(info *types.Info, st *ast.AssignStmt) (mapDef, bool) {
	if st.Tok != token.DEFINE || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return mapDef{}, false
	}
	id, ok := ast.Unparen(st.Lhs[0]).(*ast.Ident)
	if !ok || id.Name == "_" {
		return mapDef{}, false
	}
	v, ok := info.Defs[id].(*types.Var)
	if !ok {
		return mapDef{}, false
	}
	text, mkey, ok := mapIndex(info, st.Rhs[0])
	if !ok {
		return mapDef{}, false
	}
	return mapDef{v: v, text: text, mkey: mkey, pos: st.Pos(), stmt: st}, true
}

func mapIndex(info *types.Info, e ast.Expr) (string, flow.Key, bool) {
	var x ast.Expr
	switch n := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		x = n.X
	case *ast.IndexListExpr:
		x = n.X
	default:
		return "", flow.Key{}, false
	}
	t := info.TypeOf(x)
	if t == nil {
		return "", flow.Key{}, false
	}
	mp, ok := t.Underlying().(*types.Map)
	if !ok {
		return "", flow.Key{}, false
	}
	if _, ok := mp.Elem().Underlying().(*types.Struct); !ok {
		return "", flow.Key{}, false
	}
	mkey, _ := flow.KeyOf(info, x)
	return types.ExprString(x), mkey, true
}

func collectWholeUses(info *types.Info, fn ast.Node) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				markWholeUses(info, r, out)
			}
		case *ast.CallExpr:
			for _, a := range n.Args {
				markWholeUses(info, a, out)
			}
		case *ast.AssignStmt:
			for _, rhs := range n.Rhs {
				markWholeUses(info, rhs, out)
			}
		}
		return true
	})
	return out
}

func markWholeUses(info *types.Info, e ast.Expr, out map[*types.Var]bool) {
	var stack []ast.Node
	ast.Inspect(e, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types.Var); ok && !isFieldBase(stack, id) {
				out[v] = true
			}
		}
		stack = append(stack, n)
		return true
	})
}

func collectFieldWrites(info *types.Info, fn ast.Node) map[*types.Var][]ast.Stmt {
	out := map[*types.Var][]ast.Stmt{}
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			if v, ok := assignFieldRoot(info, n); ok {
				out[v] = append(out[v], n)
			}
		case *ast.IncDecStmt:
			if v, ok := fieldRoot(info, n.X); ok {
				out[v] = append(out[v], n)
			}
		}
		return true
	})
	return out
}

func assignFieldRoot(info *types.Info, st *ast.AssignStmt) (*types.Var, bool) {
	for _, lhs := range st.Lhs {
		if v, ok := fieldRoot(info, lhs); ok {
			return v, true
		}
	}
	return nil, false
}

func fieldRoot(info *types.Info, e ast.Expr) (*types.Var, bool) {
	if passesPointer(info, e) {
		return nil, false
	}
	k, ok := flow.KeyOf(info, e)
	if !ok || k.Path == "" {
		return nil, false
	}
	v, ok := k.Root.(*types.Var)
	return v, ok
}

func isFieldBase(stack []ast.Node, id *ast.Ident) bool {
	child := ast.Node(id)
	rest := stack
	for len(rest) > 0 {
		top := rest[len(rest)-1]
		par, ok := top.(*ast.ParenExpr)
		if !ok || par.X != child {
			break
		}
		child = par
		rest = rest[:len(rest)-1]
	}
	if len(rest) == 0 {
		return false
	}
	sel, ok := rest[len(rest)-1].(*ast.SelectorExpr)
	return ok && sel.X == child
}

func storedBack(info *types.Info, fn ast.Node, d mapDef, write ast.Stmt) bool {
	paths := writeFieldPaths(info, write, d.v)
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if !hasStore(info, fn, d, p, write) {
			return false
		}
	}
	return true
}

func writeFieldPaths(info *types.Info, st ast.Stmt, v *types.Var) []string {
	switch st := st.(type) {
	case *ast.AssignStmt:
		var out []string
		for _, lhs := range st.Lhs {
			if p, ok := fieldPathOf(info, lhs, v); ok {
				out = append(out, p)
			}
		}
		return out
	case *ast.IncDecStmt:
		if p, ok := fieldPathOf(info, st.X, v); ok {
			return []string{p}
		}
		return nil
	}
	return nil
}

func fieldPathOf(info *types.Info, e ast.Expr, v *types.Var) (string, bool) {
	if passesPointer(info, e) {
		return "", false
	}
	k, ok := flow.KeyOf(info, e)
	if !ok {
		return "", false
	}
	if k.Root != v {
		return "", false
	}
	if k.Path == "" {
		return "", false
	}
	return k.Path, true
}

type storeQuery struct {
	info  *types.Info
	d     mapDef
	want  string
	write ast.Stmt
	lits  []*ast.FuncLit
}

func (q storeQuery) matches(st *ast.AssignStmt) bool {
	if st.Tok != token.ASSIGN {
		return false
	}
	if !sameMap(q.info, st, q.d) {
		return false
	}
	if !qualifies(st, q.write, q.lits) {
		return false
	}
	for _, rhs := range st.Rhs {
		if rhsReadsField(q.info, rhs, q.d.v, q.want) {
			return true
		}
	}
	return false
}

func hasStore(info *types.Info, fn ast.Node, d mapDef, want string, write ast.Stmt) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	q := storeQuery{info: info, d: d, want: want, write: write, lits: collectLits(body)}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if q.matches(st) {
			found = true
		}
		return !found
	})
	return found
}

func sameMap(info *types.Info, st *ast.AssignStmt, d mapDef) bool {
	for _, lhs := range st.Lhs {
		var x ast.Expr
		switch n := ast.Unparen(lhs).(type) {
		case *ast.IndexExpr:
			x = n.X
		case *ast.IndexListExpr:
			x = n.X
		default:
			continue
		}
		t := info.TypeOf(x)
		if t == nil {
			continue
		}
		if _, ok := t.Underlying().(*types.Map); !ok {
			continue
		}
		if mapKeysEqual(info, x, d) {
			return true
		}
	}
	return false
}

func mapKeysEqual(info *types.Info, x ast.Expr, d mapDef) bool {
	k, ok := flow.KeyOf(info, x)
	if !ok || d.mkey.Root == nil {
		return types.ExprString(x) == d.text
	}
	return k == d.mkey
}

func qualifies(st *ast.AssignStmt, write ast.Stmt, lits []*ast.FuncLit) bool {
	if stmtInLit(st, lits) {
		return true
	}
	return st.Pos() > write.End()
}

func stmtInLit(st *ast.AssignStmt, lits []*ast.FuncLit) bool {
	for _, l := range lits {
		if st.Pos() >= l.Pos() && st.End() <= l.End() {
			return true
		}
	}
	return false
}

func rhsReadsField(info *types.Info, rhs ast.Expr, v *types.Var, want string) bool {
	lhs := collectLHS(rhs)
	inc := collectInc(rhs)
	found := false
	ast.Inspect(rhs, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if !overlaps(info, sel, v, want) {
			return true
		}
		if isWrite(sel, lhs, inc) {
			return true
		}
		found = true
		return false
	})
	return found
}

func overlaps(info *types.Info, sel *ast.SelectorExpr, v *types.Var, want string) bool {
	k, ok := flow.KeyOf(info, sel)
	if !ok {
		return false
	}
	if k.Root != v {
		return false
	}
	if k.Path == want {
		return true
	}
	if strings.HasPrefix(k.Path, want+".") {
		return true
	}
	return strings.HasPrefix(want, k.Path+".")
}

func collectLits(n ast.Node) []*ast.FuncLit {
	var out []*ast.FuncLit
	ast.Inspect(n, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			out = append(out, lit)
		}
		return true
	})
	return out
}

func collectLHS(n ast.Node) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(n, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		out = append(out, st.Lhs...)
		return true
	})
	return out
}

func collectInc(n ast.Node) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(n, func(n ast.Node) bool {
		st, ok := n.(*ast.IncDecStmt)
		if !ok {
			return true
		}
		out = append(out, st.X)
		return true
	})
	return out
}

func isWrite(sel *ast.SelectorExpr, lhs, inc []ast.Expr) bool {
	for _, e := range lhs {
		if within(sel, e) {
			return true
		}
	}
	for _, e := range inc {
		if within(sel, e) {
			return true
		}
	}
	return false
}

func within(sel *ast.SelectorExpr, e ast.Expr) bool {
	return sel.Pos() >= e.Pos() && sel.End() <= e.End()
}

func reportMapCopy(pass *analysis.Pass, st ast.Stmt, d mapDef) {
	pass.Reportf(st.Pos(), "%s is a copy of the element of %s, so this write is lost: store %s back into the map", d.v.Name(), d.text, d.v.Name())
}
