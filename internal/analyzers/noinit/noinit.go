package noinit

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

const message = "init function: move setup into an explicit function called from main"

var Analyzer = &analysis.Analyzer{
	Name: "noinit",
	Doc: "Report package init functions.\n\n" +
		"An init function runs at import time, in an order the reader cannot see, and cannot return an error. " +
		"Move the setup into an explicit function that main calls.",
	Run: run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && isInit(fn) {
				pass.Report(analysis.Diagnostic{Pos: fn.Pos(), Message: message})
			}
		}
	}
	return nil, nil
}

func isInit(fn *ast.FuncDecl) bool {
	return fn.Recv == nil && fn.Name.Name == "init"
}
