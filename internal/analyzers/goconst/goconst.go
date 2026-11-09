package goconst

import (
	"go/token"

	goconstlib "github.com/jgautheron/goconst"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "goconst",
	Doc:  "Report string literals of 3 or more characters repeated 3 or more times.",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	issues, err := goconstlib.Run(pass.Files, pass.Fset, pass.TypesInfo, &goconstlib.Config{MinStringLength: 3, MinOccurrences: 3})
	if err != nil {
		return nil, err
	}
	files := make(map[string]*token.File, len(pass.Files))
	for _, f := range pass.Files {
		tf := pass.Fset.File(f.FileStart)
		files[tf.Name()] = tf
	}
	for _, issue := range issues {
		if tf, ok := files[issue.Pos.Filename]; ok {
			pass.Reportf(tf.Pos(issue.Pos.Offset), "string %q has %d occurrences, make it a constant", issue.Str, issue.OccurrencesCount)
		}
	}
	return nil, nil
}
