// Package placeholder reports placeholder panics and, in strict mode,
// TODO and FIXME comments in non-test files.
package placeholder

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
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

// New returns the placeholder analyzer for mode m: placeholder panics in
// every mode, and TODO and FIXME comments from strict mode on.
func New(m config.Mode) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "placeholder",
		Doc: "Report unfinished work left in non-test files.\n\n" +
			"A call of the builtin panic whose argument is a string literal containing, in any letter case, " +
			"unimplemented, not implemented, or todo is reported. In strict mode, a comment holding the whole " +
			"word TODO or FIXME is also reported; directive comments are skipped.",
		Run: func(pass *analysis.Pass) (any, error) {
			run(pass, m)
			return nil, nil
		},
	}
}

func run(pass *analysis.Pass, m config.Mode) {
	for _, f := range pass.Files {
		if isTestFile(pass.Fset, f) {
			continue
		}
		reportPanics(pass, f)
		if m >= config.Strict {
			reportComments(pass, f)
		}
	}
}

func isTestFile(fset *token.FileSet, f *ast.File) bool {
	return strings.HasSuffix(fset.Position(f.Package).Filename, "_test.go")
}

func reportPanics(pass *analysis.Pass, f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isPlaceholderPanic(pass.TypesInfo, call) {
			pass.Report(analysis.Diagnostic{Pos: call.Pos(), Message: panicMessage})
		}
		return true
	})
}

func isPlaceholderPanic(info *types.Info, call *ast.CallExpr) bool {
	if len(call.Args) != 1 || !isBuiltinPanic(info, call.Fun) {
		return false
	}
	lit, ok := ast.Unparen(call.Args[0]).(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && namesUnfinishedWork(lit.Value)
}

func isBuiltinPanic(info *types.Info, fun ast.Expr) bool {
	id, ok := ast.Unparen(fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	return ok && b.Name() == "panic"
}

func namesUnfinishedWork(quoted string) bool {
	text, err := strconv.Unquote(quoted)
	if err != nil {
		return false
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "unimplemented") ||
		strings.Contains(lower, "not implemented") ||
		strings.Contains(lower, "todo")
}

func reportComments(pass *analysis.Pass, f *ast.File) {
	for _, group := range f.Comments {
		for _, c := range group.List {
			reportComment(pass, c)
		}
	}
}

func reportComment(pass *analysis.Pass, c *ast.Comment) {
	if isDirective(c.Text) {
		return
	}
	word, at := findMarker(c.Text)
	if at < 0 {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:     c.Slash + token.Pos(at),
		End:     c.Slash + token.Pos(at+len(word)),
		Message: fmt.Sprintf(commentFormat, word),
	})
}

func isDirective(text string) bool {
	return strings.HasPrefix(text, "//nolint") ||
		strings.HasPrefix(text, "// +build") ||
		isToolDirective(text)
}

func isToolDirective(text string) bool {
	rest, ok := strings.CutPrefix(text, "//")
	if !ok {
		return false
	}
	name, value, ok := strings.Cut(rest, ":")
	return ok && name != "" && value != "" &&
		strings.IndexFunc(name, isNotDirectiveRune) < 0 &&
		!isNotDirectiveRune(rune(value[0]))
}

func isNotDirectiveRune(r rune) bool {
	return (r < 'a' || r > 'z') && (r < '0' || r > '9')
}

func findMarker(text string) (string, int) {
	word, at := "", -1
	for _, marker := range [...]string{"TODO", "FIXME"} {
		if i := wordIndex(text, marker); i >= 0 && (at < 0 || i < at) {
			word, at = marker, i
		}
	}
	return word, at
}

func wordIndex(text, word string) int {
	for start := 0; start <= len(text)-len(word); {
		i := strings.Index(text[start:], word)
		if i < 0 {
			return -1
		}
		i += start
		if isWholeWord(text, i, i+len(word)) {
			return i
		}
		start = i + 1
	}
	return -1
}

func isWholeWord(text string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, _ := utf8.DecodeRuneInString(text[end:])
	return !isWordRune(before) && !isWordRune(after)
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
