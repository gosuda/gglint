package driver

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gosuda/gglint/internal/finding"
)

type jsonDiagnostic struct {
	Posn           string `json:"posn"`
	End            string `json:"end"`
	Message        string `json:"message"`
	SuggestedFixes []struct {
		Message string           `json:"message"`
		Edits   []jsontext.Value `json:"edits"`
	} `json:"suggested_fixes"`
	Related []struct {
		Posn    string `json:"posn"`
		Message string `json:"message"`
	} `json:"related"`
}

type entry struct {
	pkg, analyzer string
	raw           jsontext.Value
}

var position = regexp.MustCompile(`^(.*?):(\d+)(?::(\d+))?$`)

func decodeJSON(stdout []byte) ([]finding.Finding, error) {
	entries, err := readEntries(stdout)
	if err != nil {
		return nil, err
	}
	var out []finding.Finding
	for _, e := range entries {
		found, err := decodeEntry(e)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

func readEntries(stdout []byte) ([]entry, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(stdout))
	var out []entry
	for {
		var tree map[string]map[string]jsontext.Value
		err := json.UnmarshalDecode(dec, &tree)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, decodeError(err)
		}
		for pkg, analyzers := range tree {
			for analyzer, raw := range analyzers {
				out = append(out, entry{pkg: pkg, analyzer: analyzer, raw: raw})
			}
		}
	}
	slices.SortFunc(out, func(a, b entry) int {
		return cmp.Or(strings.Compare(a.pkg, b.pkg), strings.Compare(a.analyzer, b.analyzer))
	})
	return out, nil
}

func decodeError(err error) error {
	return fmt.Errorf("load: decode go vet output: %w", err)
}

func decodeEntry(e entry) ([]finding.Finding, error) {
	if e.raw.Kind() == '{' {
		var failure struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(e.raw, &failure); err != nil {
			return nil, decodeError(err)
		}
		return nil, fmt.Errorf("internal: %s: %s: %s", e.pkg, e.analyzer, failure.Error)
	}
	var diagnostics []jsonDiagnostic
	if err := json.Unmarshal(e.raw, &diagnostics); err != nil {
		return nil, decodeError(err)
	}
	out := make([]finding.Finding, 0, len(diagnostics))
	for _, d := range diagnostics {
		out = append(out, toFinding(e.analyzer, d))
	}
	return out, nil
}

func toFinding(analyzer string, d jsonDiagnostic) finding.Finding {
	f := finding.Finding{Rule: analyzer, Message: d.Message, Pos: parsePosition(d.Posn)}
	if end := parsePosition(d.End); end != f.Pos {
		f.End = end
	}
	for _, fix := range d.SuggestedFixes {
		f.Fixes = append(f.Fixes, fix.Message)
		f.Fixable = f.Fixable || len(fix.Edits) > 0
	}
	for _, r := range d.Related {
		f.Related = append(f.Related, finding.Related{Position: parsePosition(r.Posn), Message: r.Message})
	}
	return f
}

func parsePosition(s string) finding.Position {
	m := position.FindStringSubmatch(s)
	if m == nil {
		return finding.Position{}
	}
	return finding.Position{Path: m[1], Line: number(m[2]), Column: number(m[3])}
}

func number(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
