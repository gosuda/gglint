package dupl

import (
	"go/token"
	"path/filepath"

	"github.com/golangci/dupl/lib"
	"golang.org/x/tools/go/analysis"
)

const minTokens = 150

var Analyzer = &analysis.Analyzer{
	Name: "dupl",
	Doc:  "Report duplicated code fragments of at least 150 syntax tokens within a package.",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	files := make(map[string]*token.File, len(pass.Files))
	names := make([]string, 0, len(pass.Files))
	for _, f := range pass.Files {
		tf := pass.Fset.File(f.FileStart)
		files[tf.Name()] = tf
		names = append(names, tf.Name())
	}
	issues, err := lib.Run(names, minTokens)
	if err != nil {
		return nil, err
	}
	for _, issue := range issues {
		if tf, ok := files[issue.From.Filename()]; ok {
			to := issue.To
			pass.Reportf(tf.LineStart(issue.From.LineStart()), "duplicate of %s:%d-%d", filepath.Base(to.Filename()), to.LineStart(), to.LineEnd())
		}
	}
	return nil, nil
}
