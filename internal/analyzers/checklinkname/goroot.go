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
	root := realPath(filepath.Join(build.Default.GOROOT, "src")) + string(filepath.Separator)
	return slices.DeleteFunc(slices.Clone(pass.Files), func(f *ast.File) bool {
		return strings.HasPrefix(realPath(pass.Fset.File(f.Package).Name()), root)
	})
}

func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
