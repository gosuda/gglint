package report

import (
	json "encoding/json/v2"
	"fmt"
	"io"
	"slices"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/finding"
)

const (
	FormatText = "text"
	FormatJSON = "json"
)

func Write(w io.Writer, format string, mode config.Mode, configPath string, fs []finding.Finding) error {
	switch format {
	case FormatText:
		return writeText(w, fs)
	case FormatJSON:
		return writeJSON(w, mode, configPath, fs)
	default:
		return fmt.Errorf("unknown format %q (want text or json)", format)
	}
}

func writeText(w io.Writer, fs []finding.Finding) error {
	var b []byte
	for _, f := range fs {
		b = fmt.Appendf(b, "%s:%d:%d: %s [%s]\n", f.Pos.Path, f.Pos.Line, max(f.Pos.Column, 1), f.Message, f.Token())
		for _, r := range f.Related {
			b = fmt.Appendf(b, "\trelated: %s:%d:%d: %s\n", r.Path, r.Line, max(r.Column, 1), r.Message)
		}
		messages := fixMessages(f)
		for _, message := range messages {
			b = fmt.Appendf(b, "\tfix: %s\n", message)
		}
		if len(messages) == 0 && f.Fixable {
			b = append(b, "\tfix: available\n"...)
		}
	}
	_, err := w.Write(b)
	return err
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
