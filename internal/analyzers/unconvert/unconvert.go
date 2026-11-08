package unconvert

import (
	"go/token"

	unconvertlib "github.com/golangci/unconvert"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "unconvert",
	Doc:  "Report conversions of an expression to the type it already has.",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	files := tokenFiles(pass)
	for _, p := range unconvertlib.Run(pass) {
		if tf, ok := files[p.Filename]; ok {
			pass.Reportf(tf.LineStart(p.Line)+token.Pos(p.Column-1), "unnecessary conversion")
		}
	}
	return nil, nil
}

func tokenFiles(pass *analysis.Pass) map[string]*token.File {
	files := make(map[string]*token.File, len(pass.Files))
	for _, f := range pass.Files {
		tf := pass.Fset.File(f.FileStart)
		files[tf.Name()] = tf
	}
	return files
}
