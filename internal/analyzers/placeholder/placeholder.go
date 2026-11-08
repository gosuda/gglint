package placeholder

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

const (
	panicMessage  = "placeholder panic: implement this path"
	commentFormat = "placeholder %s comment: finish the work or delete the marker"
)

func New(m config.Mode) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "placeholder",
		Doc: "Report unfinished work left in non-test files.\n\n" +
			"A call of the builtin panic whose argument is a constant string containing unimplemented or " +
			"not implemented in any letter case, or TODO as written, is reported. In strict mode, a comment " +
			"whose text, after the comment marker and any white space, starts with the word TODO or FIXME " +
			"is also reported; a marker followed by a letter, digit, or underscore is prose.",
		Run: func(pass *analysis.Pass) (any, error) {
			run(pass, m)
			return nil, nil
		},
	}
}

func run(pass *analysis.Pass, m config.Mode) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Package).Filename, "_test.go") {
			continue
		}
		reportPanics(pass, f)
		if m >= config.Strict {
			reportComments(pass, f)
		}
	}
}

func reportPanics(pass *analysis.Pass, f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isPlaceholderPanic(pass.TypesInfo, call) {
			pass.ReportRangef(call, panicMessage)
		}
		return true
	})
}

func isPlaceholderPanic(info *types.Info, call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if b, ok := info.Uses[id].(*types.Builtin); !ok || b.Name() != "panic" {
		return false
	}
	v := info.Types[call.Args[0]].Value
	return v != nil && v.Kind() == constant.String && namesUnfinishedWork(constant.StringVal(v))
}

func namesUnfinishedWork(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "unimplemented") ||
		strings.Contains(lower, "not implemented") ||
		strings.Contains(msg, "TODO")
}

func reportComments(pass *analysis.Pass, f *ast.File) {
	for _, group := range f.Comments {
		for _, c := range group.List {
			if marker := leadingMarker(c.Text); marker != "" {
				pass.ReportRangef(c, commentFormat, marker)
			}
		}
	}
}

func leadingMarker(text string) string {
	body := strings.TrimLeftFunc(text[2:], unicode.IsSpace)
	for _, marker := range [...]string{"TODO", "FIXME"} {
		if rest, ok := strings.CutPrefix(body, marker); ok && !startsWord(rest) {
			return marker
		}
	}
	return ""
}

func startsWord(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
