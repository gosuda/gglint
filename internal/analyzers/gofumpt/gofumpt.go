package gofumpt

import (
	"bytes"
	"go/ast"
	"go/version"
	"os"
	"path/filepath"
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
	opts := options(pass.Module)
	read := readFunc(pass)
	for _, file := range pass.Files {
		if err := check(pass, file, read, opts); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func options(m *analysis.Module) format.Options {
	if m == nil {
		return format.Options{}
	}
	return format.Options{LangVersion: langVersion(m.GoVersion), ModulePath: m.Path}
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

func readFunc(pass *analysis.Pass) func(string) ([]byte, error) {
	if pass.ReadFile != nil {
		return pass.ReadFile
	}
	return os.ReadFile
}

func check(pass *analysis.Pass, file *ast.File, read func(string) ([]byte, error), opts format.Options) error {
	name := pass.Fset.File(file.FileStart).Name()
	if filepath.Ext(name) != ".go" {
		return nil
	}
	src, err := read(name)
	if err != nil {
		return err
	}
	formatted, ok := formatSource(src, opts)
	if !ok || bytes.Equal(src, formatted) {
		return nil
	}
	pass.Report(analysis.Diagnostic{
		Pos:     file.Package,
		Message: "file is not gofumpt-formatted",
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   "format with gofumpt",
			TextEdits: []analysis.TextEdit{{Pos: file.FileStart, End: file.FileEnd, NewText: formatted}},
		}},
	})
	return nil
}

func formatSource(src []byte, opts format.Options) (formatted []byte, ok bool) {
	formatted, err := format.Source(src, opts)
	return formatted, err == nil
}
