package gorace

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
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

func funcDecls(pass *analysis.Pass) []inspector.Cursor {
	var out []inspector.Cursor
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil)) {
		out = append(out, cur)
	}
	return out
}

func keyText(k flow.Key) string {
	if k.Root == nil {
		return ""
	}
	if k.Path == "" {
		return k.Root.Name()
	}
	return k.Root.Name() + k.Path
}

func deref(t types.Type) types.Type {
	for {
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
		for _, a := range call.Args {
			k, ok := flow.KeyOf(info, a)
			if !ok {
				continue
			}
			if v, ok := k.Root.(*types.Var); ok && isMutexVar(v) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isMutexLock(full string) bool {
	return full == "(*sync.Mutex).Lock" || full == "(*sync.RWMutex).Lock"
}

func heldOutside(pass *analysis.Pass, lit *ast.FuncLit, stmt ast.Node) []flow.Key {
	if lit == nil || stmt == nil {
		return nil
	}
	g := flow.NewGraph(pass, lit)
	if g == nil {
		return nil
	}
	var out []flow.Key
	for _, k := range flow.HeldAt(pass, g)(stmt) {
		if v, ok := k.Root.(*types.Var); ok && outsideLit(v, lit) {
			out = append(out, k)
		}
	}
	return out
}

func parentHeld(pass *analysis.Pass, g *flow.Graph, stmt ast.Node) []flow.Key {
	if g == nil || stmt == nil {
		return nil
	}
	return flow.HeldAt(pass, g)(stmt)
}

func commonKey(a, b []flow.Key) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func declaredOutsideLoop(fn *ast.FuncDecl, v types.Object, site ast.Node) bool {
	loops := []*ast.BlockStmt{}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt:
			if n.Body != nil {
				loops = append(loops, n.Body)
			}
		case *ast.RangeStmt:
			if n.Body != nil {
				loops = append(loops, n.Body)
			}
		}
		return true
	})
	for _, b := range loops {
		inBody := func(p token.Pos) bool { return p >= b.Pos() && p < b.End() }
		if inBody(v.Pos()) && inBody(site.Pos()) {
			return false
		}
	}
	return true
}
