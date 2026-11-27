package driver_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

const fakeGoCaptureScope = `#!/bin/sh
d=$(dirname "$0")
out=""
for a in "$@"; do
	case "$a" in
		-fix-output=*) out="${a#-fix-output=}/active-roots.json";;
	esac
done
if [ -n "$out" ] && [ -f "$out" ]; then
	cat "$out" >"$d/roots"
else
	printf 'MISSING\n' >"$d/roots"
fi
printf '%s\n' "$@" >"$d/args"
exit 0
`

func installGoCaptureScope(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake go command needs a POSIX shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeGoCaptureScope), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFixWritesExplicitEmptyScopeOutsideModules(t *testing.T) {
	// Sequential: GOWORK=off keeps the bare work directory outside any
	// module so the resolved scope is empty.
	t.Setenv("GOWORK", "off")
	dir := installGoCaptureScope(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := driver.Fix(t.Context(), invocation(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(dir, "roots"))); got != "[]" {
		t.Fatalf("active-roots manifest = %q, want [] for an empty scope", got)
	}
}

func TestFixKeepsWorkspaceScopeOutOfCommandLine(t *testing.T) {
	// Sequential: workspace discovery reads GOWORK and go.work.
	t.Setenv("GOWORK", "")
	root := t.TempDir()
	const members = 300
	use := make([]string, 0, members)
	for i := range members {
		name := fmt.Sprintf("m%03d", i)
		writeGo(t, root, filepath.Join(name, "go.mod"), "module example.com/"+name+"\n")
		use = append(use, "use ./"+name)
	}
	writeGo(t, root, "go.work", "go 1.27\n\n"+strings.Join(use, "\n")+"\n")
	dir := installGoCaptureScope(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := driver.Fix(t.Context(), invocation(root)); err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(strings.TrimSpace(readFile(t, filepath.Join(dir, "args")))) {
		if len(line) > 4096 {
			t.Fatalf("go argument is %d bytes, want every argument under 4096", len(line))
		}
		if strings.Contains(line, "m000") {
			t.Fatalf("go argument names workspace members: %.80q", line)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "roots"))
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	if err := json.Unmarshal(data, &roots); err != nil {
		t.Fatalf("manifest is not a JSON array: %v", err)
	}
	if len(roots) != members {
		t.Fatalf("manifest holds %d roots, want %d", len(roots), members)
	}
}
