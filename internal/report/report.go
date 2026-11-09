package report

import (
	"cmp"
	json "encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/finding"
)

const (
	FormatAgent = "agent"
	FormatText  = "text"
	FormatJSON  = "json"
)

func Write(w io.Writer, format string, mode config.Mode, configPath string, fs []finding.Finding) error {
	switch format {
	case FormatAgent:
		return writeAgent(w, mode, fs)
	case FormatText:
		return writeText(w, fs)
	case FormatJSON:
		return writeJSON(w, mode, configPath, fs)
	default:
		return fmt.Errorf("unknown format %q (want agent, text, or json)", format)
	}
}

func writeAgent(w io.Writer, mode config.Mode, fs []finding.Finding) error {
	_, err := w.Write(appendAgentSummary(appendFindings(nil, fs, "\t"), mode, fs))
	return err
}

func appendAgentSummary(b []byte, mode config.Mode, fs []finding.Finding) []byte {
	rules := map[string]int{}
	files := map[string]struct{}{}
	for _, f := range fs {
		rules[f.Rule]++
		files[f.Pos.Path] = struct{}{}
	}
	b = fmt.Appendf(b, "gglint: %d findings in %d files (mode %s)", len(fs), len(files), mode)
	byCount := func(x, y string) int { return cmp.Or(cmp.Compare(rules[y], rules[x]), cmp.Compare(x, y)) }
	for _, rule := range slices.SortedFunc(maps.Keys(rules), byCount) {
		b = fmt.Appendf(b, " %s=%d", rule, rules[rule])
	}
	return append(b, '\n')
}

func writeText(w io.Writer, fs []finding.Finding) error {
	_, err := w.Write(appendFindings(nil, fs, "\n\t"))
	return err
}

func appendFindings(b []byte, fs []finding.Finding, detailSep string) []byte {
	for _, f := range fs {
		b = fmt.Appendf(b, "%s:%d:%d: %s [%s]", f.Pos.Path, f.Pos.Line, max(f.Pos.Column, 1), oneLine(f.Message), f.Token())
		for _, r := range f.Related {
			b = fmt.Appendf(b, "%srelated: %s:%d:%d: %s", detailSep, r.Path, r.Line, max(r.Column, 1), oneLine(r.Message))
		}
		messages := fixMessages(f)
		for _, message := range messages {
			b = fmt.Appendf(b, "%sfix: %s", detailSep, oneLine(message))
		}
		if len(messages) == 0 && f.Fixable {
			b = fmt.Appendf(b, "%sfix: available", detailSep)
		}
		b = append(b, '\n')
	}
	return b
}

func oneLine(message string) string {
	return strings.Join(strings.FieldsFunc(message, isLineBreak), " ")
}

func isLineBreak(r rune) bool {
	return r == '\n' || r == '\r'
}

func fixMessages(f finding.Finding) []string {
	return slices.DeleteFunc(slices.Clone(f.Fixes), func(message string) bool { return message == "" })
}

type document struct {
	Mode     string        `json:"mode"`
	Config   string        `json:"config,omitzero"`
	Findings []jsonFinding `json:"findings"`
}

type jsonFinding struct {
	Rule      string        `json:"rule"`
	Check     string        `json:"check,omitzero"`
	Path      string        `json:"path"`
	Line      int           `json:"line"`
	Column    int           `json:"column"`
	EndLine   int           `json:"end_line,omitzero"`
	EndColumn int           `json:"end_column,omitzero"`
	Message   string        `json:"message"`
	Related   []jsonRelated `json:"related,omitempty"`
	Fixes     []string      `json:"fixes,omitempty"`
	Fixable   bool          `json:"fixable,omitzero"`
}

type jsonRelated struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

func writeJSON(w io.Writer, mode config.Mode, configPath string, fs []finding.Finding) error {
	doc := document{Mode: mode.String(), Config: configPath, Findings: make([]jsonFinding, 0, len(fs))}
	for _, f := range fs {
		doc.Findings = append(doc.Findings, toJSON(f))
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

func toJSON(f finding.Finding) jsonFinding {
	out := jsonFinding{
		Rule:      f.Rule,
		Check:     f.Check,
		Path:      f.Pos.Path,
		Line:      f.Pos.Line,
		Column:    max(f.Pos.Column, 1),
		EndLine:   f.End.Line,
		EndColumn: f.End.Column,
		Message:   f.Message,
		Fixes:     fixMessages(f),
		Fixable:   f.Fixable,
	}
	for _, r := range f.Related {
		out.Related = append(out.Related, jsonRelated{Path: r.Path, Line: r.Line, Column: max(r.Column, 1), Message: r.Message})
	}
	return out
}
