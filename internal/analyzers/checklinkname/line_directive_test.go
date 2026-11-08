package checklinkname

import (
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

const unresolvedRuntimeLinkname = `remote symbol "notARealSymbolZZZ" not defined in package "runtime"`

const unresolvedRuntimeLinknameMain = `package main

import (
	_ "runtime"
	_ "unsafe"
)

//go:linkname missing runtime.notARealSymbolZZZ
func missing()

func main() { missing() }
`

func TestLineDirectiveClaimingGOROOTDoesNotHideUnresolvedLinkname(t *testing.T) {
	gorootSrc, err := filepath.EvalSymlinks(filepath.Join(build.Default.GOROOT, "src"))
	if err != nil {
		t.Fatal(err)
	}
	claimedGOROOTFile := filepath.Join(gorootSrc, "linehide", "main.go")
	cases := []struct {
		name   string
		header string
	}{
		{name: "physical position only", header: ""},
		{name: "line directive claims GOROOT", header: "//line " + claimedGOROOTFile + ":1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			messages := rootDiagnosticMessages(t, tc.header+unresolvedRuntimeLinknameMain)
			if !slices.Contains(messages, unresolvedRuntimeLinkname) {
				t.Errorf("diagnostics = %q, want one equal to %q", messages, unresolvedRuntimeLinkname)
			}
		})
	}
}

func rootDiagnosticMessages(t *testing.T, mainSource string) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module linehide\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSource), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgs, err := packages.Load(&packages.Config{
		Context: t.Context(),
		Mode:    packages.LoadAllSyntax,
		Dir:     dir,
		Env:     append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off"),
	}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("temporary linehide module does not load")
	}
	graph, err := checker.Analyze([]*analysis.Analyzer{Analyzer}, pkgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, act := range graph.Roots {
		if act.Err != nil {
			t.Fatalf("%s: %v", act, act.Err)
		}
		for _, d := range act.Diagnostics {
			messages = append(messages, d.Message)
		}
	}
	return messages
}
