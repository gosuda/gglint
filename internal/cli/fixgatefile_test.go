package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixNamesFileInMainModule(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	nested, _ := writeNestedFixable(t, dir)

	got := gglint(t, dir, "run", "-fix", "./nested/nested.go")
	if got.code != 0 {
		t.Fatalf("named file result = %+v", got)
	}
	if after := readFile(t, filepath.Join(nested, "nested.go")); after == nestedFixable {
		t.Fatal("named fixable file did not change")
	}
}

func TestFixDoesNotRewriteNamedVendoredFile(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	vendored := filepath.Join(dir, "vendor", "dep")
	if err := os.MkdirAll(vendored, 0o750); err != nil {
		t.Fatal(err)
	}
	const before = "package dep\n\nfunc F() int {\n\n\treturn 1\n}\n"
	writeFile(t, filepath.Join(vendored, "dep.go"), before)
	got := gglint(t, dir, "run", "-fix", "./vendor/dep/dep.go")
	if after := readFile(t, filepath.Join(vendored, "dep.go")); after != before {
		t.Fatalf("vendored file was rewritten: %q", after)
	}
	if !strings.Contains(got.stdout, "dep.go") {
		t.Fatalf("vendored diagnostic missing from report: %+v", got)
	}
}

func TestFixStagesNamedFilePastNestedWorkspace(t *testing.T) {
	// The invocation runs from the root workspace, but the vettool starts
	// in the package directory, whose nested go.work excludes the member.
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(root, "app"), filepath.Join(root, "app", "tools")} {
		if err := os.Mkdir(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "go.work"), "go 1.27\n\nuse ./app\n")
	writeFile(t, filepath.Join(root, "app", "go.mod"), "module example.com/app\n")
	writeFile(t, filepath.Join(root, "app", "go.work"), "go 1.27\n\nuse ./tools\n")
	writeFile(t, filepath.Join(root, "app", "tools", "go.mod"), "module example.com/tools\n")
	app := filepath.Join(root, "app", "app.go")
	writeFile(t, app, nestedFixable)
	var env []string
	for _, kv := range childEnv(t, root) {
		switch kv {
		case "GOWORK=off":
			continue
		case "GOFLAGS=-mod=mod":
			kv = "GOFLAGS=-mod=readonly"
		}
		env = append(env, kv)
	}
	got := gglintEnv(t, root, env, "run", "-fix", "./app/app.go")
	if got.code != 0 {
		t.Fatalf("nested workspace result = %+v", got)
	}
	if after := readFile(t, app); after == nestedFixable {
		t.Fatal("named fixable file did not change past the nested workspace")
	}
}
