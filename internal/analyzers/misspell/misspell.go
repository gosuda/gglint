package misspell

import (
	"fmt"
	"go/token"

	misspelllib "github.com/golangci/misspell"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "misspell",
	Doc:  "Report commonly misspelled English words in comments.",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	replacer := misspelllib.New()
	for _, f := range pass.Files {
		if err := checkFile(pass, replacer, pass.Fset.File(f.FileStart)); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func checkFile(pass *analysis.Pass, replacer *misspelllib.Replacer, tf *token.File) error {
	src, err := pass.ReadFile(tf.Name())
	if err != nil {
		return err
	}
	_, diffs := replacer.ReplaceGo(string(src))
	for _, d := range diffs {
		pass.Report(diagnostic(tf, d))
	}
	return nil
}

func diagnostic(tf *token.File, d misspelllib.Diff) analysis.Diagnostic {
	pos := tf.LineStart(d.Line) + token.Pos(d.Column)
	end := pos + token.Pos(len(d.Original))
	return analysis.Diagnostic{
		Pos:     pos,
		End:     end,
		Message: fmt.Sprintf("%q is a misspelling of %q", d.Original, d.Corrected),
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   fmt.Sprintf("replace %q with %q", d.Original, d.Corrected),
			TextEdits: []analysis.TextEdit{{Pos: pos, End: end, NewText: []byte(d.Corrected)}},
		}},
	}
}
