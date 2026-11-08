package checklinkname

import (
	"go/ast"
	"go/build"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

func linknameFiles(pass *analysis.Pass) []*ast.File {
	root := filepath.Join(build.Default.GOROOT, "src") + string(filepath.Separator)
	var out []*ast.File
	for _, f := range pass.Files {
		if !strings.HasPrefix(pass.Fset.Position(f.Package).Filename, root) {
			out = append(out, f)
		}
	}
	return out
}
