package gorace

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
)

var mapAnalyzer = &analysis.Analyzer{
	Name:     "map",
	Doc:      "reports maps written by a goroutine and accessed concurrently",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runMap,
}

var fanoutAnalyzer = &analysis.Analyzer{
	Name:     "fanout",
	Doc:      "reports loop variables written by every goroutine a loop starts",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runFanout,
}

var sharedAnalyzer = &analysis.Analyzer{
	Name:     "shared",
	Doc:      "reports variables a goroutine accesses without ordering",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runShared,
}

var doublecheckAnalyzer = &analysis.Analyzer{
	Name:     "doublecheck",
	Doc:      "reports double-checked locking that reads outside the lock",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runDoublecheck,
}

var subtestAnalyzer = &analysis.Analyzer{
	Name:     "subtest",
	Doc:      "reports parallel subtests sharing a variable",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runSubtest,
}

// Analyzers returns the five gorace analyzers.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{mapAnalyzer, fanoutAnalyzer, sharedAnalyzer, doublecheckAnalyzer, subtestAnalyzer}
}

func keyText(pass *analysis.Pass, k flow.Key) string {
	if k.Root == nil {
		return ""
	}
	base := k.Root.Name()
	if k.Path != "" {
		base += k.Path
	}
	v, ok := k.Root.(*types.Var)
	if !ok || v.Pkg() == nil || pass.Pkg == nil || v.Pkg() == pass.Pkg {
		return base
	}
	return v.Pkg().Name() + "." + base
}

func deref(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	seen := map[types.Type]bool{}
	for {
		if seen[t] {
			return t
		}
		seen[t] = true
		p, ok := t.Underlying().(*types.Pointer)
		if !ok {
			return t
		}
		t = p.Elem()
	}
}

func isSyncType(t types.Type) bool {
	if t == nil {
		return false
	}
	n, ok := deref(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	p := n.Obj().Pkg().Path()
	return p == "sync" || p == "sync/atomic"
}

func isMapType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := deref(t).Underlying().(*types.Map)
	return ok
}

func isMutexNamed(t types.Type) bool {
	n, ok := deref(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	if n.Obj().Pkg().Path() != "sync" {
		return false
	}
	name := n.Obj().Name()
	return name == "Mutex" || name == "RWMutex"
}

func isMutexVar(v *types.Var) bool {
	if v == nil {
		return false
	}
	return isMutexNamed(v.Type())
}

func outsideLit(o types.Object, lit *ast.FuncLit) bool {
	if o == nil || lit == nil {
		return false
	}
	return o.Pos() < lit.Pos() || o.Pos() >= lit.End()
}

func passesMutex(info *types.Info, fn ast.Node) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit != fn {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callPassesMutex(info, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func callPassesMutex(info *types.Info, call *ast.CallExpr) bool {
	for _, a := range call.Args {
		k, ok := flow.KeyOf(info, a)
		if !ok {
			continue
		}
		if v, ok := k.Root.(*types.Var); ok && isMutexVar(v) {
			return true
		}
	}
	return false
}

func isMutexLock(full string) bool {
	return full == "(*sync.Mutex).Lock" || full == "(*sync.RWMutex).Lock"
}

func heldOutside(pass *analysis.Pass, lit *ast.FuncLit, stmt ast.Node) []flow.Key {
	if lit == nil || stmt == nil {
		return nil
	}
	return rootedKeys(newHolder(pass, lit).at(stmt), lit)
}

func rootedKeys(keys []flow.Key, lit *ast.FuncLit) []flow.Key {
	var out []flow.Key
	for _, k := range keys {
		if v, ok := k.Root.(*types.Var); ok && outsideLit(v, lit) {
			out = append(out, k)
		}
	}
	return out
}

type guardScope struct {
	pass     *analysis.Pass
	fn       ast.Node
	parents  *holder
	litLocks map[*ast.FuncLit]*holder
}

func newGuardScope(pass *analysis.Pass, fn ast.Node) *guardScope {
	return &guardScope{pass: pass, fn: fn, parents: newHolder(pass, fn), litLocks: map[*ast.FuncLit]*holder{}}
}

func (g *guardScope) parentHeld(stmt ast.Node) []flow.Key {
	if stmt == nil {
		return nil
	}
	return g.parents.at(stmt)
}

func (g *guardScope) litHeld(lit *ast.FuncLit, stmt ast.Node) []flow.Key {
	if lit == nil || stmt == nil {
		return nil
	}
	h, ok := g.litLocks[lit]
	if !ok {
		h = newHolder(g.pass, lit)
		g.litLocks[lit] = h
	}
	return rootedKeys(h.at(stmt), lit)
}

func commonKey(a, b []flow.Key) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func funcBodies(pass *analysis.Pass) []ast.Node {
	var decls []ast.Node
	var lits []*ast.FuncLit
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				decls = append(decls, n)
			case *ast.FuncLit:
				lits = append(lits, n)
			}
			return true
		})
	}
	out := make([]ast.Node, 0, len(decls)+len(lits))
	out = append(out, decls...)
	for _, lit := range lits {
		out = append(out, lit)
	}
	return out
}

func declaredOutsideLoop(info *types.Info, fn ast.Node, v types.Object, site ast.Node) bool {
	loop, body := innermostLoop(fn, site.Pos())
	if body == nil || loop == nil {
		return true
	}
	if v.Pos() >= body.Pos() && v.Pos() < body.End() {
		return false
	}
	return !headerDefines(info, loop, body, v)
}

func declAvoid(info *types.Info, fn ast.Node, v types.Object, site ast.Node) func(ast.Node) bool {
	_, body := innermostLoop(fn, site.Pos())
	if body == nil {
		return nil
	}
	if v == nil || v.Pos() < body.Pos() || v.Pos() >= body.End() {
		return nil
	}
	return func(n ast.Node) bool {
		return definesVar(info, n, v)
	}
}

func definesVar(info *types.Info, n ast.Node, v types.Object) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if id, ok := x.(*ast.Ident); ok && info.Defs[id] == v {
			found = true
			return false
		}
		return true
	})
	return found
}

func innermostLoop(fn ast.Node, pos token.Pos) (ast.Node, *ast.BlockStmt) {
	var bestLoop ast.Node
	var bestBody *ast.BlockStmt
	ast.Inspect(fn, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != fn {
			return false
		}
		body := loopBodyOf(n)
		if !bodyContains(body, pos) {
			return true
		}
		if bestBody == nil || body.End()-body.Pos() < bestBody.End()-bestBody.Pos() {
			bestLoop = n
			bestBody = body
		}
		return true
	})
	return bestLoop, bestBody
}

func loopBodyOf(n ast.Node) *ast.BlockStmt {
	switch n := n.(type) {
	case *ast.ForStmt:
		return n.Body
	case *ast.RangeStmt:
		return n.Body
	}
	return nil
}

func bodyContains(body *ast.BlockStmt, pos token.Pos) bool {
	return body != nil && pos >= body.Pos() && pos < body.End()
}

func exitsListAfter(fn, site ast.Node) bool {
	list, owner := stmtListAfter(fn, site)
	switch owner.(type) {
	case *ast.CaseClause, *ast.CommClause:
		return slices.ContainsFunc(list, isReturnStmt)
	default:
		return slices.ContainsFunc(list, isLoopExit)
	}
}

func stmtListAfter(fn, site ast.Node) ([]ast.Stmt, ast.Node) {
	var out []ast.Stmt
	var owner ast.Node
	ast.Inspect(flow.Body(fn), func(n ast.Node) bool {
		if out != nil {
			return false
		}
		list := stmtListOf(n)
		for i, stmt := range list {
			if stmt == site {
				out = list[i+1:]
				owner = n
				return false
			}
		}
		return true
	})
	return out, owner
}

func stmtListOf(n ast.Node) []ast.Stmt {
	switch b := n.(type) {
	case *ast.BlockStmt:
		return b.List
	case *ast.CaseClause:
		return b.Body
	case *ast.CommClause:
		return b.Body
	}
	return nil
}

func isLoopExit(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.BranchStmt:
		return s.Tok == token.BREAK && s.Label == nil
	case *ast.ReturnStmt:
		return true
	}
	return false
}

func isReturnStmt(stmt ast.Stmt) bool {
	_, ok := stmt.(*ast.ReturnStmt)
	return ok
}

func bodyParks(fn ast.Node) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	parks := false
	ast.Inspect(body, func(n ast.Node) bool {
		if parks {
			return false
		}
		switch x := n.(type) {
		case *ast.SendStmt:
			parks = true
			return false
		case *ast.UnaryExpr:
			if x.Op == token.ARROW {
				parks = true
				return false
			}
		case *ast.SelectStmt:
			parks = true
			return false
		case *ast.CallExpr:
			if parkedCall(x) {
				parks = true
				return false
			}
		}
		return true
	})
	return parks
}

func parkedCall(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		id, ok := ast.Unparen(call.Fun).(*ast.Ident)
		return ok && id.Name == "Sleep"
	}
	switch sel.Sel.Name {
	case lockOpLock, lockOpRLock, "Wait", "Sleep", "Recv", "Accept":
		return true
	}
	return false
}

func headerDefines(info *types.Info, loop ast.Node, body *ast.BlockStmt, v types.Object) bool {
	found := false
	ast.Inspect(loop, func(n ast.Node) bool {
		if found || n == ast.Node(body) {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && info.Defs[id] == v {
			found = true
			return false
		}
		return true
	})
	return found
}
