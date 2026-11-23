package cli_test

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestNolintKeepsUnrelatedRemappedDiagnostics(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	src := "package a\n\nimport \"fmt\"\n\n" +
		"func logf(format string, args ...any) { fmt.Printf(format, args...) }\n\n" +
		"func Bad() {\n" +
		"//line virtual.go:100\n" +
		"\tlogf(\"%d\\n\", \"suppressed\") //nolint:printf // intentional\n" +
		"//line virtual.go:100\n" +
		"\tlogf(\"%d\\n\", \"reported\")\n}\n"
	writeFile(t, filepath.Join(dir, "a", "a.go"), src)
	doc, _ := lintJSON(t, dir, "-mode", "basic", "./a")
	want := []jsonFinding{{
		Rule:    "printf",
		Path:    "a/virtual.go",
		Line:    100,
		Column:  1,
		Message: printfMessage("a", "reported"),
	}}
	if doc.Mode != "basic" || !slices.Equal(doc.Findings, want) {
		t.Fatalf("report %+v; want mode basic, findings %+v", doc, want)
	}
}
