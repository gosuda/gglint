package checklinkname

import (
	"go/ast"
	"go/build"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

func linknameFiles(pass *analysis.Pass) []*ast.File {
	root := filepath.Join(build.Default.GOROOT, "src") + string(filepath.Separator)
	return slices.DeleteFunc(slices.Clone(pass.Files), func(f *ast.File) bool {
		return strings.HasPrefix(pass.Fset.Position(f.Package).Filename, root)
	})
}
