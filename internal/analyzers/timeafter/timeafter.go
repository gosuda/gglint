package timeafter

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const message = "time.After in a loop allocates a timer per iteration: create one time.Timer before the loop and Reset it"

var Analyzer = &analysis.Analyzer{
	Name: "timeafter",
	Doc: `report calls to time.After inside a loop body

Each pass through the body of a for or range statement that calls time.After
allocates a new timer that stays alive until it fires. A function literal
inside the body counts as inside it; the loop header (init, condition, post
statement, range operand) runs outside the body and is not reported.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.CallExpr)(nil)) {
		call := inspection.Node[*ast.CallExpr](cur)
		if isTimeAfter(pass.TypesInfo, call) && inLoopBody(cur) {
			pass.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: message})
		}
	}
	return nil, nil
}

func isTimeAfter(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	return fn != nil && fn.Name() == "After" && fn.Pkg() != nil && fn.Pkg().Path() == "time" &&
		fn.Signature().Recv() == nil
}

func inLoopBody(cur inspector.Cursor) bool {
	for ; cur.Node() != nil; cur = cur.Parent() {
		if kind := cur.ParentEdgeKind(); kind == edge.ForStmt_Body || kind == edge.RangeStmt_Body {
			return true
		}
	}
	return false
}
