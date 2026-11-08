package driver

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gosuda/gglint/internal/finding"
)

func TestDecodeJSONStream(t *testing.T) {
	stream := `{"m/a":{"printf":[{"posn":"/w/a.go:5:2","end":"/w/a.go:5:20","message":"bad verb",` +
		`"suggested_fixes":[{"message":"fix it","edits":[{"filename":"/w/a.go","start":1,"end":2,"new":"x"}]},{"message":"hint","edits":[]}],` +
		`"related":[{"posn":"/w/a.go:1:1","end":"/w/a.go:1:1","message":"declared"}]}]}}` + "\n" +
		`{"m/b":{"modernize_forvar":[{"posn":"/w/b.go:3:4","end":"/w/b.go:3:4","message":"copy"}]}}` + "\n"
	got, err := decodeJSON([]byte(stream))
	if err != nil {
		t.Fatal(err)
	}
	want := []finding.Finding{
		{
			Rule: "printf", Message: "bad verb",
			Pos:     finding.Position{Path: "/w/a.go", Line: 5, Column: 2},
			End:     finding.Position{Path: "/w/a.go", Line: 5, Column: 20},
			Fixes:   []string{"fix it", "hint"},
			Fixable: true,
			Related: []finding.Related{{Path: "/w/a.go", Line: 1, Column: 1, Message: "declared"}},
		},
		{Rule: "modernize_forvar", Message: "copy", Pos: finding.Position{Path: "/w/b.go", Line: 3, Column: 4}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeJSON = %+v\nwant %+v", got, want)
	}
}

func TestDecodeJSONEmptyOutput(t *testing.T) {
	got, err := decodeJSON(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("decodeJSON(nil) = %v, %v", got, err)
	}
}

func TestDecodeJSONAnalyzerError(t *testing.T) {
	_, err := decodeJSON([]byte(`{"m/a":{"errcheck":{"error":"boom"}}}`))
	internal, ok := errors.AsType[*InternalError](err)
	if !ok {
		t.Fatalf("error = %v, want *InternalError", err)
	}
	if want := "internal: m/a: errcheck: boom"; internal.Error() != want {
		t.Fatalf("error text = %q, want %q", internal.Error(), want)
	}
}

func TestDecodeJSONMalformed(t *testing.T) {
	_, err := decodeJSON([]byte(`{"m/a":`))
	if _, ok := errors.AsType[*LoadError](err); !ok {
		t.Fatalf("error = %v, want *LoadError", err)
	}
}

func TestParsePositionWithoutColumn(t *testing.T) {
	got := parsePosition("/w/a b.go:12")
	if want := (finding.Position{Path: "/w/a b.go", Line: 12}); got != want {
		t.Fatalf("parsePosition = %+v, want %+v", got, want)
	}
	if got := parsePosition("-"); got != (finding.Position{}) {
		t.Fatalf("invalid position = %+v", got)
	}
}

func TestTypecheckFindings(t *testing.T) {
	stderr := "# m/a\n./a.go:4:2: undefined: x\nvet: b.go:9: declared and not used: y\nnoise line\n"
	got := typecheckFindings([]byte(stderr), "/w")
	want := []finding.Finding{
		{Rule: "typecheck", Message: "undefined: x", Pos: finding.Position{Path: "/w/a.go", Line: 4, Column: 2}},
		{Rule: "typecheck", Message: "declared and not used: y", Pos: finding.Position{Path: "/w/b.go", Line: 9}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("typecheckFindings = %+v\nwant %+v", got, want)
	}
}
