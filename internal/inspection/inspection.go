package inspection

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

func Of(pass *analysis.Pass) *inspector.Inspector {
	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		panic(pass.Analyzer.Name + ": missing inspect result")
	}
	return insp
}

func As[N ast.Node](n ast.Node) N {
	node, ok := n.(N)
	if !ok {
		var want N
		panic(fmt.Sprintf("node is %T, want %T", n, want))
	}
	return node
}
