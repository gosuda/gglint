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

// Package checkaligned ensures that atomic (u)int operations happen
// exclusively via the typed atomics of sync/atomic (atomic.Int64 and friends).
//
// We support a "// +checkalignedignore" escape hatch in the package comment
// that disables checking throughout the package.
package checkaligned

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer defines the entrypoint.
var Analyzer = &analysis.Analyzer{
	Name: "checkaligned",
	Doc:  "prohibits direct use of sync/atomic int functions; use the typed atomics",
	Run:  run,
}

var typedAtomicsLongestFirst = []string{"Uint32", "Uint64", "Int32", "Int64"}

func run(pass *analysis.Pass) (any, error) {
	// Check for the "// +checkalignedignore" escape hatch.
	for _, file := range pass.Files {
		if file.Doc == nil {
			continue
		}
		for _, comment := range file.Doc.List {
			if len(comment.Text) > 2 && strings.HasPrefix(comment.Text[2:], " +checkalignedignore") {
				return nil, nil
			}
		}
	}

	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			fn, typed, ok := blockedFunc(pass.TypesInfo, ident)
			if ok {
				pass.Reportf(ident.Pos(), "don't call atomic.%s; use the atomic.%s type instead", fn, typed)
			}
			return true
		})
	}

	return nil, nil
}

func blockedFunc(info *types.Info, ident *ast.Ident) (name, typed string, ok bool) {
	fn, isFunc := info.Uses[ident].(*types.Func)
	if !isFunc || fn.Pkg() == nil || fn.Pkg().Path() != "sync/atomic" || fn.Signature().Recv() != nil {
		return "", "", false
	}
	i := slices.IndexFunc(typedAtomicsLongestFirst, func(t string) bool { return strings.HasSuffix(fn.Name(), t) })
	if i < 0 {
		return "", "", false
	}
	return fn.Name(), typedAtomicsLongestFirst[i], true
}
