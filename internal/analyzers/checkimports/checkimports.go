// Copyright 2019 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package checkimports

import (
	"go/ast"
	"path"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

func New(deny []config.Deny) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "checkimports",
		Doc:  "enforces restrictions on package imports: unsafe only in *_unsafe.go and *_test.go files, plus the configured deny list",
		Run: func(pass *analysis.Pass) (any, error) {
			return run(pass, deny)
		},
	}
}

func run(pass *analysis.Pass, deny []config.Deny) (any, error) {
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			pkg, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if pkg == "unsafe" {
				checkUnsafe(pass, imp)
			}
			if denied(deny, pkg, pass.Pkg.Path()) {
				pass.Reportf(imp.Pos(), "import of %q is denied in %s", pkg, pass.Pkg.Path())
			}
		}
	}
	return nil, nil
}

func checkUnsafe(pass *analysis.Pass, imp *ast.ImportSpec) {
	filename := pass.Fset.File(imp.Pos()).Name()
	if !strings.HasSuffix(filename, "_unsafe.go") && !strings.HasSuffix(filename, "_test.go") {
		pass.Reportf(imp.Pos(), "package unsafe imported by %s; must end with _unsafe.go", path.Base(filename))
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
