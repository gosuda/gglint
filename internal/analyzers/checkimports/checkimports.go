package checkimports

import (
	"go/ast"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

func New(deny []config.Deny) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "checkimports",
		Doc:  "Enforces the configured import deny list",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, f := range pass.Files {
				checkFile(pass, f, deny)
			}
			return nil, nil
		},
	}
}

func checkFile(pass *analysis.Pass, f *ast.File, deny []config.Deny) {
	for _, imp := range f.Imports {
		pkg, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if denied(deny, pkg, pass.Pkg.Path()) {
			pass.Reportf(imp.Pos(), "import of %q is denied in %s", pkg, pass.Pkg.Path())
		}
	}
}

func denied(deny []config.Deny, imported, importer string) bool {
	return slices.ContainsFunc(deny, func(d config.Deny) bool {
		return matchPath(d.Path, imported) && !slices.ContainsFunc(d.AllowFrom, func(allow string) bool {
			return matchPath(allow, importer)
		})
	})
}

func matchPath(pattern, pkg string) bool {
	prefix, tree := strings.CutSuffix(pattern, "/...")
	if !tree {
		return pkg == pattern
	}
	return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
}
