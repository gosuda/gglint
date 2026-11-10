package resstate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

// Analyzers returns the four use-after-release analyzers.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		useaftercloseAnalyzer,
		useafterputAnalyzer,
		useafterfreeAnalyzer,
		viewescapeAnalyzer,
	}
}

var useaftercloseAnalyzer = &analysis.Analyzer{
	Name:     "useafterclose",
	Doc:      "reports method calls on values closed on every path to the call",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUseafterclose,
}

var useafterputAnalyzer = &analysis.Analyzer{
	Name:     "useafterput",
	Doc:      "reports uses of values returned to a sync.Pool on every path",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUseafterput,
}

var useafterfreeAnalyzer = &analysis.Analyzer{
	Name:     "useafterfree",
	Doc:      "reports uses of values freed on every path to the use",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUseafterfree,
}

var viewescapeAnalyzer = &analysis.Analyzer{
	Name:     "viewescape",
	Doc:      "reports buffer views kept past the call that overwrites them",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runViewescape,
}

type ostate uint8

const (
	stLive ostate = iota
	stReleased
	stUnknown
)

func joinState(a, b ostate) ostate {
	if a == b {
		return a
	}
	return stUnknown
}

func runUseafterclose(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		closeFunc(pass, cur.Node())
	}
	return nil, nil
}

func runUseafterput(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		putFunc(pass, cur.Node())
	}
	return nil, nil
}

func runUseafterfree(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		freeFunc(pass, cur.Node())
	}
	return nil, nil
}

func closeFunc(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	for _, v := range localsOf(pass, fn) {
		if !hasCloseResult(v.Type()) {
			continue
		}
		closeVar(pass, fn, g, v)
	}
}

func putFunc(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	for _, v := range localsOf(pass, fn) {
		putVar(pass, fn, g, v)
	}
}

func freeFunc(pass *analysis.Pass, fn ast.Node) {
	g := flow.NewGraph(pass, fn)
	if g == nil {
		return
	}
	for _, v := range localsOf(pass, fn) {
		freeVar(pass, fn, g, v)
	}
}

func closeVar(pass *analysis.Pass, fn ast.Node, g *flow.Graph, v *types.Var) {
	flow.Forward(g, stLive,
		func(n ast.Node, s ostate) ostate { return closeTransfer(pass, fn, n, v, s) },
		joinState,
		func(n ast.Node, s ostate) { closeVisit(pass, n, v, s) })
}

func putVar(pass *analysis.Pass, fn ast.Node, g *flow.Graph, v *types.Var) {
	flow.Forward(g, stLive,
		func(n ast.Node, s ostate) ostate { return putTransfer(pass, fn, n, v, s) },
		joinState,
		func(n ast.Node, s ostate) { putVisit(pass, fn, n, v, s) })
}

func freeVar(pass *analysis.Pass, fn ast.Node, g *flow.Graph, v *types.Var) {
	flow.Forward(g, stLive,
		func(n ast.Node, s ostate) ostate { return freeTransfer(pass, fn, n, v, s) },
		joinState,
		func(n ast.Node, s ostate) { freeVisit(pass, fn, n, v, s) })
}

func closeTransfer(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) ostate {
	if hasCloseRelease(pass, n, v) {
		return stReleased
	}
	if assignsVar(pass, fn, n, v) {
		return stUnknown
	}
	if escapesVar(pass, fn, n, v) {
		return stUnknown
	}
	return s
}

func putTransfer(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) ostate {
	if hasPutRelease(pass, n, v) {
		return stReleased
	}
	if assignsVar(pass, fn, n, v) {
		return stUnknown
	}
	if escapesVar(pass, fn, n, v) {
		return stUnknown
	}
	return s
}

func freeTransfer(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) ostate {
	if hasFreeRelease(pass, fn, n, v) {
		return stReleased
	}
	if assignsVar(pass, fn, n, v) {
		return stUnknown
	}
	if escapesVar(pass, fn, n, v) {
		return stUnknown
	}
	return s
}

func closeVisit(pass *analysis.Pass, n ast.Node, v *types.Var, s ostate) {
	if s != stReleased {
		return
	}
	for _, c := range methodCallsOn(pass, n, v) {
		if !closeFailsOn(pass, c) {
			continue
		}
		pass.Reportf(c.Pos(), "%s is closed on every path to this call, so the call fails: move Close after the last use", v.Name())
	}
}

func putVisit(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) {
	if s != stReleased {
		return
	}
	if nilAssign(pass, n, v) {
		return
	}
	if hasPutRelease(pass, n, v) {
		pass.Reportf(mentionPos(pass, n, v), "%s was returned to the pool, and another goroutine may own it now: call Put after the last use", v.Name())
		return
	}
	if takesAddr(pass, n, v) {
		return
	}
	if redefinesVar(pass, fn, n, v) {
		return
	}
	if mentionsVar(pass, n, v) {
		pass.Reportf(mentionPos(pass, n, v), "%s was returned to the pool, and another goroutine may own it now: call Put after the last use", v.Name())
		return
	}
}

func freeVisit(pass *analysis.Pass, fn, n ast.Node, v *types.Var, s ostate) {
	if s != stReleased {
		return
	}
	if nilAssign(pass, n, v) {
		return
	}
	if hasFreeRelease(pass, fn, n, v) {
		pass.Reportf(mentionPos(pass, n, v), "%s is used after it was freed: set it to nil after the free and do not use it again", v.Name())
		return
	}
	if takesAddr(pass, n, v) {
		return
	}
	if redefinesVar(pass, fn, n, v) {
		return
	}
	if mentionsVar(pass, n, v) {
		pass.Reportf(mentionPos(pass, n, v), "%s is used after it was freed: set it to nil after the free and do not use it again", v.Name())
		return
	}
}

func nilAssign(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	as, ok := n.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	id, ok := ast.Unparen(as.Lhs[0]).(*ast.Ident)
	if !ok || pass.TypesInfo.Uses[id] != v {
		return false
	}
	return pass.TypesInfo.Types[as.Rhs[0]].IsNil()
}

func localsOf(pass *analysis.Pass, fn ast.Node) []*types.Var {
	seen := map[*types.Var]bool{}
	var out []*types.Var
	ast.Inspect(fn, func(n ast.Node) bool {
		if n != fn {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := pass.TypesInfo.Defs[id].(*types.Var)
		if !ok || seen[v] {
			return true
		}
		seen[v] = true
		if v.IsField() {
			return true
		}
		if flow.IsLocal(pass.TypesInfo, fn, v) {
			out = append(out, v)
		}
		return true
	})
	return out
}

func hasCloseResult(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "Close")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Signature()
	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type())
}

func isPoolType(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "Pool" && obj.Pkg() != nil && obj.Pkg().Path() == "sync"
}
