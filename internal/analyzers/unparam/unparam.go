// Package unparam reports unused parameters and constant results of unexported functions.
package unparam

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/packages"
	"mvdan.cc/unparam/check"
)

// Analyzer reports the issues found by mvdan.cc/unparam in unexported functions.
var Analyzer = &analysis.Analyzer{
	Name:     "unparam",
	Doc:      "Report unused parameters and always-constant parameters and results of unexported functions.",
	Requires: []*analysis.Analyzer{buildssa.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	var checker check.Checker
	checker.ProgramSSA(pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).Pkg.Prog)
	checker.Packages([]*packages.Package{passPackage(pass)})
	checker.CheckExportedFuncs(false)
	issues, err := checker.Check()
	if err != nil {
		return nil, err
	}
	for _, issue := range issues {
		pass.Report(analysis.Diagnostic{Pos: issue.Pos(), Message: issue.Message()})
	}
	return nil, nil
}

func passPackage(pass *analysis.Pass) *packages.Package {
	return &packages.Package{
		Name:      pass.Pkg.Name(),
		PkgPath:   pass.Pkg.Path(),
		Fset:      pass.Fset,
		Syntax:    pass.Files,
		Types:     pass.Pkg,
		TypesInfo: pass.TypesInfo,
	}
}
