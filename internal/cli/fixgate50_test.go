package cli_test

import (
	"os"
	"path/filepath"
	"testing"
)

const nestedFixable = "package nested\n\nfunc One() int {\n\n\treturn 1\n}\n"
const deepFixable = "package deep\n\nfunc One() int {\n\n\treturn 1\n}\n"

func writeNestedFixable(t *testing.T, dir string) (nested, deep string) {
	t.Helper()
	nested = filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(nested, "nested.go"), nestedFixable)
	deep = filepath.Join(nested, "deep")
	if err := os.Mkdir(deep, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(deep, "deep.go"), deepFixable)
	return nested, deep
}

func TestFixWithRelativeTmpdirReachesNestedPackages(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	nested, _ := writeNestedFixable(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "reltmp"), 0o750); err != nil {
		t.Fatal(err)
	}
	env := append(childEnv(t, dir), "TMPDIR=reltmp")
	got := gglintEnv(t, dir, env, "run", "-fix", "./nested/...")
	if got.code != 0 {
		t.Fatalf("relative TMPDIR result = %+v", got)
	}
	if after := readFile(t, filepath.Join(nested, "nested.go")); after == nestedFixable {
		t.Fatal("nested fix did not run with a relative TMPDIR")
	}
}

func TestFixWithRelativeTmpdirFromNestedDirectory(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	nested, deep := writeNestedFixable(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "reltmp"), 0o750); err != nil {
		t.Fatal(err)
	}
	env := append(childEnv(t, nested), "TMPDIR=../reltmp")
	got := gglintEnv(t, nested, env, "run", "-fix", "./deep")
	if got.code != 0 {
		t.Fatalf("relative TMPDIR result = %+v", got)
	}
	if after := readFile(t, filepath.Join(deep, "deep.go")); after == deepFixable {
		t.Fatal("deep fix did not run with a relative TMPDIR")
	}
}
