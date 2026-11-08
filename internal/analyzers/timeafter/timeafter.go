package timeafter

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

const message = "time.After in a loop allocates a timer per iteration: create one time.Timer before the loop and Reset it"

var Analyzer = &analysis.Analyzer{
	Name: "timeafter",
	Doc: `report calls to time.After inside a loop body

Each pass through the body of a for or range statement that calls time.After
allocates a new timer that stays alive until it fires. The loop header runs
outside the body, and a function literal inside the body is a function of its
own, so neither is reported.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	calls := []ast.Node{(*ast.CallExpr)(nil)}
	ins.WithStack(calls, func(n ast.Node, push bool, stack []ast.Node) bool {
		call := n.(*ast.CallExpr)
		if push && isTimeAfter(pass.TypesInfo, call) && inLoopBody(stack) {
			pass.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: message})
		}
		return true
	})
	return nil, nil
}

func isTimeAfter(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	return fn != nil && fn.Name() == "After" && fn.Pkg() != nil && fn.Pkg().Path() == "time" &&
		fn.Signature().Recv() == nil
}

func inLoopBody(stack []ast.Node) bool {
	for i := len(stack) - 2; i >= 0; i-- {
		switch n := stack[i].(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return false
		case *ast.ForStmt:
			if stack[i+1] == n.Body {
				return true
			}
		case *ast.RangeStmt:
			if stack[i+1] == n.Body {
				return true
			}
		}
	}
	return false
}
