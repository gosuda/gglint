package cli_test

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
)

func TestBasicCheckimportsReportsDeniedImport(t *testing.T) {
	t.Parallel()
	toml := "mode = \"basic\"\n\n[rules.checkimports]\ndeny = [{ path = \"strings\" }]\n"
	dir := workspace(t, toml)

	pkg := filepath.Join(dir, "im")
	if err := os.Mkdir(pkg, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pkg, "im.go"), "package im\n\nimport \"strings\"\n\nvar Trim = strings.TrimSpace\n")
	got := gglint(t, dir, "run", "-format", "json", "./im")
	if got.code != 1 || got.stderr != configLine(toml) {
		t.Fatalf("exit %d, stderr %q; want exit 1, stderr %q", got.code, got.stderr, configLine(toml))
	}
	var doc jsonReport
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatalf("decode %q: %v", got.stdout, err)
	}
	line, _ := position(t, dir, "im/im.go", `"strings"`)
	if _, found := findAt(doc, "checkimports", "im/im.go", line); !found {
		t.Errorf("mode basic: no checkimports finding at im/im.go:%d; findings: %+v", line, doc.Findings)
	}
}
