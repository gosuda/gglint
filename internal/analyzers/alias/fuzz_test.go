package alias_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"

	"github.com/gosuda/gglint/internal/analyzers/alias"
)

func FuzzNoPanic(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{1})
	f.Add([]byte{2})
	f.Add([]byte{3})
	f.Add([]byte{4, 1, 0, 1})
	f.Add([]byte{7, 3, 5, 1, 2})
	f.Add([]byte{2, 2, 2, 2})
	f.Add([]byte{3, 3, 3, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fuzzalias\n\ngo 1.27.0\n"), 0o600); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(buildAliasSource(data)), 0o600); err != nil {
			t.Fatalf("write f.go: %v", err)
		}
		cfg := &packages.Config{
			Dir: dir,
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
				packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo |
				packages.NeedTypesSizes | packages.NeedModule,
			Env: append(os.Environ(), "GO111MODULE=on", "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local"),
		}
		pkgs, err := packages.Load(cfg, ".")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(pkgs) != 1 {
			t.Fatalf("want one package, got %d", len(pkgs))
		}
		graph, err := checker.Analyze(alias.Analyzers(), pkgs, nil)
		if err != nil {
			t.Fatalf("analyze: %v", err)
		}
		for _, root := range graph.Roots {
			if root.Err != nil {
				t.Fatalf("%s: %v", root.Analyzer.Name, root.Err)
			}
			if len(root.Diagnostics) > 0 {
				t.Fatalf("%s: %d diagnostics: %v", root.Analyzer.Name, len(root.Diagnostics), root.Diagnostics[0].Message)
			}
		}
	})
}

func buildAliasSource(data []byte) string {
	var b strings.Builder
	b.WriteString("package p\n\ntype ent struct{ n int }\n\nfunc F(s []int, t []int, m map[string]ent, items []ent) {\n")
	count := 1
	if len(data) > 0 {
		count = 1 + int(data[0])%4
	}
	for i := range count {
		v := 0
		if len(data) > 0 {
			v = int(data[(i+1)%len(data)]) % 2
		}
		b.WriteString(aliasStmt(i, v))
	}
	b.WriteString("}\n")
	return b.String()
}

func aliasStmt(slot, variant int) string {
	switch slot {
	case 0:
		if variant == 0 {
			return "\tb := s[:1:1]\n\t_ = append(b, 2)\n"
		}
		return "\t_ = append(s, 1)\n"
	case 1:
		if variant == 0 {
			return "\tout := []int{}\n\tfor _, x := range t {\n\t\tout = append(out, x)\n\t}\n\t_ = out\n"
		}
		return "\t_ = copy(t, t)\n"
	case 2:
		if variant == 0 {
			return "\tv := m[\"k\"]\n\tv.n++\n\tm[\"k\"] = v\n"
		}
		return "\tv := m[\"k\"]\n\t_ = v.n\n"
	default:
		if variant == 0 {
			return "\tfor i := range items {\n\t\titems[i].n++\n\t}\n"
		}
		return "\tfor _, v := range items {\n\t\t_ = v.n\n\t}\n"
	}
}
