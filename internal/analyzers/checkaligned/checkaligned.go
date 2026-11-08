// Copyright 2022 The gVisor Authors.
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

package checkaligned

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "checkaligned",
	Doc: "prohibits direct use of sync/atomic int functions; use the typed atomics\n\n" +
		"A package with a // +checkalignedignore comment is not checked.",
	Run: run,
}

var atomicTypes = []string{"Uint32", "Uint64", "Int32", "Int64"}

func run(pass *analysis.Pass) (any, error) {
	if hasIgnoreDirective(pass.Files) {
		return nil, nil
	}
	for ident, obj := range pass.TypesInfo.Uses {
		if fn, typed, ok := blockedFunc(obj); ok {
			pass.Reportf(ident.Pos(), "don't call atomic.%s; use the atomic.%s type instead", fn, typed)
		}
	}
	return nil, nil
}

func hasIgnoreDirective(files []*ast.File) bool {
	for _, file := range files {
		if file.Doc == nil {
			continue
		}
		for _, comment := range file.Doc.List {
			if len(comment.Text) > 2 && strings.HasPrefix(comment.Text[2:], " +checkalignedignore") {
				return true
			}
		}
	}
	return false
}

func blockedFunc(obj types.Object) (name, typed string, ok bool) {
	fn, isFunc := obj.(*types.Func)
	if !isFunc || fn.Pkg() == nil || fn.Pkg().Path() != "sync/atomic" || fn.Signature().Recv() != nil {
		return "", "", false
	}
	i := slices.IndexFunc(atomicTypes, func(t string) bool { return strings.HasSuffix(fn.Name(), t) })
	if i < 0 {
		return "", "", false
	}
	return fn.Name(), atomicTypes[i], true
}
