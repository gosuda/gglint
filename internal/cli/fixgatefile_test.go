package cli_test

import (
	"os"
	"path/filepath"
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
	if got.stdout == "" {
		t.Fatalf("vendored finding left the report: %+v", got)
	}
}
