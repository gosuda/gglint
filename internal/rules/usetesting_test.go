package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

const usetestingStubSrc = "package p\n\nimport (\n\t_ \"unsafe\"\n\t\"os\"\n\t\"testing\"\n)\n\n//go:linkname stub p.impl\nfunc stub(t *testing.T)\n\nfunc TestMkdir(t *testing.T) {\n\t_, _ = os.MkdirTemp(\"\", \"\")\n}\n"

func writeUsetestingFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/p\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(usetestingStubSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func loadUsetestingPackage(t *testing.T, dir string) *packages.Package {
	t.Helper()
	cfg := &packages.Config{
		Context: t.Context(),
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes | packages.NeedModule,
		Env:     append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("load: got %v", pkgs)
	}
	return pkgs[0]
}

func buildUsetestingAnalyzer(t *testing.T) *analysis.Analyzer {
	t.Helper()
	selected, err := rules.Select(config.Config{Mode: config.Standard})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range selected {
		if r.Name != "usetesting" {
			continue
		}
		built := r.Build(config.Config{Mode: config.Standard})
		if len(built) != 1 {
			t.Fatalf("usetesting builds %d analyzers; want 1", len(built))
		}
		return built[0]
	}
	t.Fatal("usetesting is not selected")
	return nil
}

func TestUsetestingSkipsBodylessFuncDecl(t *testing.T) {
	t.Parallel()
	pkg := loadUsetestingPackage(t, writeUsetestingFixture(t))
	graph, err := checker.Analyze([]*analysis.Analyzer{buildUsetestingAnalyzer(t)}, []*packages.Package{pkg}, nil)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, root := range graph.Roots {
		if root.Err != nil {
			t.Fatalf("analyzer %s: %v", root.Analyzer.Name, root.Err)
		}
		for _, d := range root.Diagnostics {
			if strings.Contains(d.Message, "MkdirTemp") {
				return
			}
		}
	}
	t.Fatal("want one MkdirTemp finding, got none")
}
