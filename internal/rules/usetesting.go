package rules

import (
	"go/ast"
	"maps"

	"github.com/ldez/usetesting"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/gosuda/gglint/internal/config"
)

func buildUsetesting(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{guardBodyless(usetesting.NewAnalyzer())}
}

func guardBodyless(upstream *analysis.Analyzer) *analysis.Analyzer {
	wrapped := *upstream
	run := upstream.Run
	wrapped.Run = func(pass *analysis.Pass) (any, error) {
		filtered, ok := withoutBodyless(pass.Files)
		if !ok {
			return run(pass)
		}
		next := *pass
		results := make(map[*analysis.Analyzer]any, len(pass.ResultOf)+1)
		maps.Copy(results, pass.ResultOf)
		results[inspect.Analyzer] = inspector.New(filtered)
		next.ResultOf = results
		next.Files = filtered
		return run(&next)
	}
	return &wrapped
}

func withoutBodyless(files []*ast.File) ([]*ast.File, bool) {
	filtered := make([]*ast.File, len(files))
	changed := false
	for i, file := range files {
		decls := make([]ast.Decl, 0, len(file.Decls))
		for _, d := range file.Decls {
			if fn, isFunc := d.(*ast.FuncDecl); isFunc && fn.Body == nil {
				continue
			}
			decls = append(decls, d)
		}
		if len(decls) == len(file.Decls) {
			filtered[i] = file
			continue
		}
		changed = true
		clone := *file
		clone.Decls = decls
		filtered[i] = &clone
	}
	if !changed {
		return nil, false
	}
	return filtered, true
}
