package report_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/finding"
	"github.com/gosuda/gglint/internal/report"
)

func pos(path string, line, column int) finding.Position {
	return finding.Position{Path: path, Line: line, Column: column}
}

func render(t *testing.T, format string, mode config.Mode, configPath string, fs []finding.Finding) string {
	t.Helper()
	var out bytes.Buffer
	if err := report.Write(&out, format, mode, configPath, fs); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func decode(t *testing.T, output string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(output), &doc); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, output)
	}
	return doc
}

func TestTextPrintsEachClauseOfTheDocumentedLineShape(t *testing.T) {
	t.Parallel()
	plain := finding.Finding{Rule: "printf", Message: "bad verb", Pos: pos("a/b.go", 3, 2)}
	cases := []struct {
		name string
		fs   []finding.Finding
		want string
	}{
		{"clean run prints nothing", nil, ""},
		{"rule token", []finding.Finding{plain}, "a/b.go:3:2: bad verb [printf]\n"},
		{
			"rule:check token",
			[]finding.Finding{{Rule: "staticcheck", Check: "SA1015", Message: "leaks", Pos: pos("../c.go", 40, 7)}},
			"../c.go:40:7: leaks [staticcheck:SA1015]\n",
		},
		{"missing column prints 1", []finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 4, 0)}}, "a.go:4:1: m [r]\n"},
		{
			"related positions in order, missing column prints 1",
			[]finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 9, 3), Related: []finding.Related{
				{Position: pos("b.go", 1, 4), Message: "first"},
				{Position: pos("a.go", 2, 0), Message: "second"},
			}}},
			"a.go:9:3: m [r]\n\trelated: b.go:1:4: first\n\trelated: a.go:2:1: second\n",
		},
		{
			"one fix line per fix message",
			[]finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1), Fixes: []string{"use x", "use y"}, Fixable: true}},
			"a.go:1:1: m [r]\n\tfix: use x\n\tfix: use y\n",
		},
		{
			"fix message without edits still prints",
			[]finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1), Fixes: []string{"hint"}}},
			"a.go:1:1: m [r]\n\tfix: hint\n",
		},
		{
			"fixes with edits and no message print available once",
			[]finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1), Fixes: []string{"", ""}, Fixable: true}},
			"a.go:1:1: m [r]\n\tfix: available\n",
		},
		{
			"related lines precede fix lines",
			[]finding.Finding{{
				Rule: "r", Message: "m", Pos: pos("a.go", 5, 5),
				Related: []finding.Related{{Position: pos("a.go", 1, 1), Message: "here"}},
				Fixes:   []string{"do it"}, Fixable: true,
			}},
			"a.go:5:5: m [r]\n\trelated: a.go:1:1: here\n\tfix: do it\n",
		},
		{
			"fix lines stay with their own finding",
			[]finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1), Fixes: []string{""}, Fixable: true}, plain},
			"a.go:1:1: m [r]\n\tfix: available\na/b.go:3:2: bad verb [printf]\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := render(t, report.FormatText, config.Standard, "", c.fs); got != c.want {
				t.Fatalf("text output:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}

func TestAgentPrintsOneLinePerFindingAndATailSummary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		fs   []finding.Finding
		want string
	}{
		{"clean run prints only the summary", nil, "gglint: 0 findings in 0 files (mode standard)\n"},
		{
			"related and fixes stay on the finding line",
			[]finding.Finding{{
				Rule: "r", Message: "m", Pos: pos("a.go", 5, 0),
				Related: []finding.Related{{Position: pos("b.go", 1, 0), Message: "here"}},
				Fixes:   []string{"do it", ""}, Fixable: true,
			}},
			"a.go:5:1: m [r]\trelated: b.go:1:1: here\tfix: do it\ngglint: 1 findings in 1 files (mode standard) r=1\n",
		},
		{
			"fixes with edits and no message print available",
			[]finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1), Fixes: []string{""}, Fixable: true}},
			"a.go:1:1: m [r]\tfix: available\ngglint: 1 findings in 1 files (mode standard) r=1\n",
		},
		{
			"line breaks inside messages fold into spaces",
			[]finding.Finding{{
				Rule: "r", Message: "first\nsecond\r\n", Pos: pos("a.go", 1, 1),
				Related: []finding.Related{{Position: pos("a.go", 2, 1), Message: "x\ny"}},
				Fixes:   []string{"do\nit"},
			}},
			"a.go:1:1: first second [r]\trelated: a.go:2:1: x y\tfix: do it\ngglint: 1 findings in 1 files (mode standard) r=1\n",
		},
		{
			"summary counts distinct files and rules by count then name",
			[]finding.Finding{
				{Rule: "zeta", Message: "z", Pos: pos("a.go", 1, 1)},
				{Rule: "beta", Message: "b", Pos: pos("a.go", 2, 1)},
				{Rule: "staticcheck", Check: "SA1", Message: "s", Pos: pos("b.go", 1, 1)},
				{Rule: "staticcheck", Check: "SA2", Message: "t", Pos: pos("c.go", 1, 1)},
			},
			"a.go:1:1: z [zeta]\na.go:2:1: b [beta]\nb.go:1:1: s [staticcheck:SA1]\nc.go:1:1: t [staticcheck:SA2]\n" +
				"gglint: 4 findings in 3 files (mode standard) staticcheck=2 beta=1 zeta=1\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := render(t, report.FormatAgent, config.Standard, "", c.fs); got != c.want {
				t.Fatalf("agent output:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}

func TestJSONMatchesTheReadmeExample(t *testing.T) {
	t.Parallel()
	const readme = `{"mode":"standard","config":"gglint.toml","findings":[{"rule":"staticcheck","check":"SA1015","path":"a/b.go","line":3,"column":2,"end_line":3,"end_column":9,"message":"...","related":[{"path":"a/b.go","line":1,"column":1,"message":"..."}],"fixes":["..."],"fixable":true}]}`
	fs := []finding.Finding{{
		Rule: "staticcheck", Check: "SA1015", Message: "...",
		Pos: pos("a/b.go", 3, 2), End: pos("a/b.go", 3, 9),
		Related: []finding.Related{{Position: pos("a/b.go", 1, 1), Message: "..."}},
		Fixes:   []string{"..."}, Fixable: true,
	}}
	got := render(t, report.FormatJSON, config.Standard, "gglint.toml", fs)
	if strings.TrimSuffix(got, "\n") != readme {
		t.Fatalf("json output:\n%s\nwant one line:\n%s", got, readme)
	}
}

func TestJSONOptionalFindingFieldsAppearExactlyWhenSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  string
		set  func(*finding.Finding)
		want any
	}{
		{"check", func(f *finding.Finding) { f.Check = "SA1015" }, "SA1015"},
		{"end_line", func(f *finding.Finding) { f.End = pos("a.go", 7, 9) }, float64(7)},
		{"end_column", func(f *finding.Finding) { f.End = pos("a.go", 7, 9) }, float64(9)},
		{"related", func(f *finding.Finding) {
			f.Related = []finding.Related{{Position: pos("b.go", 2, 0), Message: "r"}}
		}, []any{map[string]any{"path": "b.go", "line": float64(2), "column": float64(1), "message": "r"}}},
		{"fixes", func(f *finding.Finding) { f.Fixes = []string{"use x"} }, []any{"use x"}},
		{"fixable", func(f *finding.Finding) { f.Fixes, f.Fixable = []string{"use x"}, true }, true},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			t.Parallel()
			f := finding.Finding{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1)}
			if _, present := onlyFinding(t, f)[c.key]; present {
				t.Fatalf("%s present on a finding that does not set it", c.key)
			}
			c.set(&f)
			if got, present := onlyFinding(t, f)[c.key]; !present || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("%s = %#v (present %v), want %#v", c.key, got, present, c.want)
			}
		})
	}
}

func onlyFinding(t *testing.T, f finding.Finding) map[string]any {
	t.Helper()
	doc := decode(t, render(t, report.FormatJSON, config.Standard, "", []finding.Finding{f}))
	findings, ok := doc["findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("findings = %#v, want one element", doc["findings"])
	}
	object, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("finding = %#v, want an object", findings[0])
	}
	return object
}

func TestJSONRequiredFindingFieldsAlwaysAppear(t *testing.T) {
	t.Parallel()
	got := onlyFinding(t, finding.Finding{Rule: "printf", Message: "", Pos: pos("a.go", 12, 0)})
	want := map[string]any{"rule": "printf", "path": "a.go", "line": float64(12), "column": float64(1), "message": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("finding = %#v, want %#v", got, want)
	}
}

func TestJSONMessagesRoundTripThroughEscaping(t *testing.T) {
	t.Parallel()
	for _, message := range []string{`quote " and \ backslash`, "<html> & ampersand", "line\nbreak\ttab", "unicode é 世界", "\x01 control"} {
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			got := onlyFinding(t, finding.Finding{Rule: "r", Message: message, Pos: pos("a.go", 1, 1)})
			if got["message"] != message {
				t.Fatalf("message decoded as %q, want %q", got["message"], message)
			}
		})
	}
}

func TestJSONDocumentCarriesModeConfigAndEmptyFindingsArray(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode       config.Mode
		configPath string
		want       map[string]any
	}{
		{config.Basic, "", map[string]any{"mode": "basic", "findings": []any{}}},
		{config.Standard, "../gglint.toml", map[string]any{"mode": "standard", "config": "../gglint.toml", "findings": []any{}}},
		{config.Strict, "sub/gglint.toml", map[string]any{"mode": "strict", "config": "sub/gglint.toml", "findings": []any{}}},
	}
	for _, c := range cases {
		t.Run(c.mode.String(), func(t *testing.T) {
			t.Parallel()
			if got := decode(t, render(t, report.FormatJSON, c.mode, c.configPath, nil)); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("document = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestWriteRejectsUnknownFormatsWithoutWriting(t *testing.T) {
	t.Parallel()
	fs := []finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1)}}
	for _, format := range []string{"", "sarif", "TEXT", "Json", " text", "json ", "Agent"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			if err := report.Write(&out, format, config.Standard, "", fs); err == nil || out.Len() != 0 {
				t.Fatalf("format %q: error %v, wrote %q; want an error and no output", format, err, out.String())
			}
		})
	}
}

var errBroken = errors.New("broken pipe")

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errBroken }

func TestWritePropagatesTheWriterError(t *testing.T) {
	t.Parallel()
	one := []finding.Finding{{Rule: "r", Message: "m", Pos: pos("a.go", 1, 1)}}
	cases := []struct {
		format string
		fs     []finding.Finding
	}{
		{report.FormatAgent, one},
		{report.FormatAgent, nil},
		{report.FormatText, one},
		{report.FormatJSON, one},
		{report.FormatJSON, nil},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			t.Parallel()
			if err := report.Write(brokenWriter{}, c.format, config.Standard, "", c.fs); !errors.Is(err, errBroken) {
				t.Fatalf("error = %v, want the writer's error", err)
			}
		})
	}
}
