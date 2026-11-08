package gofumpt

import (
	"bytes"
	"go/ast"
	"go/version"
	"strings"

	"golang.org/x/tools/go/analysis"
	"mvdan.cc/gofumpt/format"
)

const doc = "gofumpt reports Go files whose source differs from the gofumpt formatting.\n\n" +
	"The suggested fix replaces the whole file with the gofumpt-formatted source."

var Analyzer = &analysis.Analyzer{
	Name: "gofumpt",
	Doc:  doc,
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	var opts format.Options
	if m := pass.Module; m != nil {
		opts = format.Options{LangVersion: langVersion(m.GoVersion), ModulePath: m.Path}
	}
	for _, file := range pass.Files {
		if err := check(pass, file, opts); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func langVersion(v string) string {
	if !strings.HasPrefix(v, "go") {
		v = "go" + v
	}
	if !version.IsValid(v) {
		return ""
	}
	return v
}

func check(pass *analysis.Pass, file *ast.File, opts format.Options) error {
	src, err := pass.ReadFile(pass.Fset.File(file.FileStart).Name())
	if err != nil {
		return err
	}
	if formatted, err := format.Source(src, opts); err == nil && !bytes.Equal(src, formatted) {
		pass.Report(analysis.Diagnostic{
			Pos:     file.Package,
			Message: "file is not gofumpt-formatted",
			SuggestedFixes: []analysis.SuggestedFix{{
				Message:   "format with gofumpt",
				TextEdits: []analysis.TextEdit{{Pos: file.FileStart, End: file.FileEnd, NewText: formatted}},
			}},
		})
	}
	return nil
}
