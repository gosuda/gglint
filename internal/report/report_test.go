package report_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/finding"
	"github.com/gosuda/gglint/internal/report"
)

func pos(path string, line, column int) finding.Position {
	return finding.Position{Path: path, Line: line, Column: column}
}

func render(t *testing.T, format, configPath string, fs []finding.Finding) string {
	t.Helper()
	var out bytes.Buffer
	if err := report.Write(&out, format, config.Standard, configPath, fs); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestTextFormat(t *testing.T) {
	fs := []finding.Finding{
		{Rule: "printf", Message: "bad verb", Pos: pos("a/b.go", 3, 2)},
		{
			Rule: "staticcheck", Check: "SA1015", Message: "leaks", Pos: pos("c.go", 4, 0),
			Related: []finding.Related{{Position: pos("c.go", 1, 0), Message: "declared here"}},
			Fixes:   []string{"use NewTicker"}, Fixable: true,
		},
		{Rule: "gofumpt", Message: "unformatted", Pos: pos("d.go", 1, 1), Fixes: []string{""}, Fixable: true},
	}
	want := strings.Join([]string{
		"a/b.go:3:2: bad verb [printf]",
		"c.go:4:1: leaks [staticcheck:SA1015]",
		"\trelated: c.go:1:1: declared here",
		"\tfix: use NewTicker",
		"d.go:1:1: unformatted [gofumpt]",
		"\tfix: available",
		"",
	}, "\n")
	if got := render(t, "text", "", fs); got != want {
		t.Fatalf("text output:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONFormat(t *testing.T) {
	fs := []finding.Finding{{
		Rule: "staticcheck", Check: "SA1015", Message: "leaks <here>",
		Pos: pos("a/b.go", 3, 2), End: pos("a/b.go", 3, 9),
		Related: []finding.Related{{Position: pos("a/b.go", 1, 1), Message: "declared"}},
		Fixes:   []string{"use NewTicker"}, Fixable: true,
	}}
	want := `{"mode":"standard","config":"gglint.toml","findings":[{"rule":"staticcheck","check":"SA1015","path":"a/b.go","line":3,"column":2,"end_line":3,"end_column":9,"message":"leaks <here>","related":[{"path":"a/b.go","line":1,"column":1,"message":"declared"}],"fixes":["use NewTicker"],"fixable":true}]}` + "\n"
	if got := render(t, "json", "gglint.toml", fs); got != want {
		t.Fatalf("json output:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONOmitsEmptyFields(t *testing.T) {
	fs := []finding.Finding{{Rule: "printf", Message: "m", Pos: pos("a.go", 1, 0)}}
	want := `{"mode":"standard","findings":[{"rule":"printf","path":"a.go","line":1,"column":1,"message":"m"}]}` + "\n"
	if got := render(t, "json", "", fs); got != want {
		t.Fatalf("json output:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONCleanRunHasEmptyFindingsArray(t *testing.T) {
	want := `{"mode":"standard","findings":[]}` + "\n"
	if got := render(t, "json", "", nil); got != want {
		t.Fatalf("json output %q, want %q", got, want)
	}
}

func TestUnknownFormat(t *testing.T) {
	if err := report.Write(&bytes.Buffer{}, "sarif", config.Basic, "", nil); err == nil {
		t.Fatal("unknown format accepted")
	}
}
