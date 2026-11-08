package errchainreset

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const (
	operandMessage   = "fmt.Errorf formats error operand %s with %s: use %%w to keep the error chain"
	errorCallMessage = "fmt.Errorf passes %s: pass the error itself with %%w to keep the error chain"
	fixMessage       = "replace the verb with %w"
)

var Analyzer = &analysis.Analyzer{
	Name: "err_chain_reset",
	Doc: `report fmt.Errorf calls that format an error without %w or pass err.Error()

A fmt.Errorf call that formats an operand of an error type with a verb other
than %w, or that passes err.Error() or a string concatenated from it, hides the
cause from errors.Is and errors.As. The verbs come from the constant format
string, and each verb's operand is found the way package fmt finds it: a *
width or precision and an explicit [n] index move the operand, and %% takes
none. A call whose arguments are spread with ... is skipped; a call whose
format is not constant is reported only for an err.Error() argument. When the
format is a string literal and every offending operand has a verb to replace
(an err.Error() argument needs %s or %v), the finding offers one fix that
rewrites those verbs to %w and passes the error itself.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

var errorInterface = universeError()

func universeError() *types.Interface {
	iface, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		panic("errchainreset: the universe error type is not an interface")
	}
	return iface
}

type directive struct {
	text     string
	verb     rune
	arg      int
	off, end int
}

type fault struct {
	arg     int
	verb    *directive
	errCall *ast.CallExpr
}

type chainCall struct {
	pass *analysis.Pass
	call *ast.CallExpr
}

func run(pass *analysis.Pass) (any, error) {
	calls := []ast.Node{(*ast.CallExpr)(nil)}
	inspection.Of(pass).Preorder(calls, func(n ast.Node) {
		call := inspection.As[*ast.CallExpr](n)
		if isErrorf(pass.TypesInfo, call) {
			chainCall{pass: pass, call: call}.diagnose()
		}
	})
	return nil, nil
}

func isErrorf(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	return fn != nil && fn.FullName() == "fmt.Errorf"
}

func (c chainCall) diagnose() {
	args := c.call.Args
	if len(args) == 0 || c.call.Ellipsis.IsValid() {
		return
	}
	spec := stringConst(c.pass.TypesInfo, args[0])
	verbs := formatVerbs(spec)
	faults := c.faults(verbs)
	if len(faults) == 0 {
		return
	}
	d := analysis.Diagnostic{Pos: c.call.Pos(), End: c.call.End(), Message: c.message(faults[0])}
	if edits, ok := c.fixEdits(spec, verbs, faults); ok {
		d.SuggestedFixes = []analysis.SuggestedFix{{Message: fixMessage, TextEdits: edits}}
	}
	c.pass.Report(d)
}

func (c chainCall) faults(verbs []directive) []fault {
	args := c.call.Args
	var out []fault
	for i := range verbs {
		v := &verbs[i]
		if v.verb != 'w' && v.arg+1 < len(args) && c.isError(args[v.arg+1]) {
			out = append(out, fault{arg: v.arg + 1, verb: v})
		}
	}
	for i, arg := range args {
		if call := c.errorString(arg); call != nil {
			out = append(out, fault{arg: i, errCall: call})
		}
	}
	slices.SortStableFunc(out, func(a, b fault) int { return cmp.Compare(a.arg, b.arg) })
	return out
}

func (c chainCall) errorString(e ast.Expr) *ast.CallExpr {
	switch e := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if c.isErrorMethodCall(e) {
			return e
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			if call := c.errorString(e.X); call != nil {
				return call
			}
			return c.errorString(e.Y)
		}
	}
	return nil
}

func (c chainCall) isErrorMethodCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" || len(call.Args) != 0 {
		return false
	}
	fn, ok := c.pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Signature()
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	return types.Identical(sig.Results().At(0).Type(), types.Typ[types.String]) && c.isError(sel.X)
}

func (c chainCall) isError(e ast.Expr) bool {
	t := c.pass.TypesInfo.TypeOf(e)
	return t != nil && types.Implements(t, errorInterface)
}

func (c chainCall) message(f fault) string {
	if f.verb != nil {
		return fmt.Sprintf(operandMessage, c.render(c.call.Args[f.arg]), f.verb.text)
	}
	return fmt.Sprintf(errorCallMessage, c.render(f.errCall))
}

func (c chainCall) render(e ast.Expr) string {
	var b strings.Builder
	if err := format.Node(&b, c.pass.Fset, e); err != nil {
		return types.ExprString(e)
	}
	return b.String()
}

func (c chainCall) fixEdits(spec string, verbs []directive, faults []fault) ([]analysis.TextEdit, bool) {
	lit, ok := c.call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil, false
	}
	var targets []directive
	var edits []analysis.TextEdit
	for _, f := range faults {
		v, recv, ok := c.fixTarget(verbs, f)
		if !ok {
			return nil, false
		}
		targets = append(targets, v)
		if recv != nil {
			arg := c.call.Args[f.arg]
			edits = append(edits, analysis.TextEdit{Pos: arg.Pos(), End: arg.End(), NewText: []byte(c.render(recv))})
		}
	}
	newFormat := requote(lit, spliceVerbs(spec, targets))
	return append(edits, analysis.TextEdit{Pos: lit.Pos(), End: lit.End(), NewText: []byte(newFormat)}), true
}

func (c chainCall) fixTarget(verbs []directive, f fault) (directive, ast.Expr, bool) {
	if f.verb != nil {
		return *f.verb, nil, true
	}
	sel, ok := f.errCall.Fun.(*ast.SelectorExpr)
	if !ok || ast.Unparen(c.call.Args[f.arg]) != ast.Expr(f.errCall) {
		return directive{}, nil, false
	}
	for _, v := range verbs {
		if v.arg+1 == f.arg && (v.verb == 's' || v.verb == 'v') {
			return v, sel.X, true
		}
	}
	return directive{}, nil, false
}

func spliceVerbs(spec string, targets []directive) string {
	slices.SortFunc(targets, func(a, b directive) int { return cmp.Compare(b.off, a.off) })
	for _, v := range slices.Compact(targets) {
		spec = spec[:v.off] + "w" + spec[v.end:]
	}
	return spec
}

func requote(lit *ast.BasicLit, value string) string {
	if strings.HasPrefix(lit.Value, "`") {
		return "`" + value + "`"
	}
	return strconv.Quote(value)
}

func stringConst(info *types.Info, e ast.Expr) string {
	v := info.Types[e].Value
	if v == nil || v.Kind() != constant.String {
		return ""
	}
	return constant.StringVal(v)
}

func formatVerbs(spec string) []directive {
	s := scanner{format: spec}
	var out []directive
	for {
		v, ok := s.next()
		if !ok {
			return out
		}
		if v.verb != '%' {
			out = append(out, v)
		}
	}
}

type scanner struct {
	format string
	pos    int
	arg    int
}

func (s *scanner) next() (directive, bool) {
	i := strings.IndexByte(s.format[s.pos:], '%')
	if i < 0 {
		return directive{}, false
	}
	from := s.pos + i
	s.pos = from + 1
	s.skipFlags()
	indexed := s.indexedSize()
	if s.pos < len(s.format) && s.format[s.pos] == '.' {
		s.pos++
		indexed = s.indexedSize()
	}
	if !indexed {
		s.argIndex()
	}
	if s.pos >= len(s.format) {
		return directive{}, false
	}
	r, size := utf8.DecodeRuneInString(s.format[s.pos:])
	v := directive{verb: r, arg: s.arg, off: s.pos, end: s.pos + size}
	v.text = s.format[from:v.end]
	s.pos = v.end
	if r != '%' {
		s.arg++
	}
	return v, true
}

func (s *scanner) argIndex() bool {
	rest := s.format[s.pos:]
	if !strings.HasPrefix(rest, "[") {
		return false
	}
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		s.pos++
		return false
	}
	s.pos += end + 1
	n, err := strconv.Atoi(rest[1:end])
	if err != nil || n < 1 {
		return false
	}
	s.arg = n - 1
	return true
}

func (s *scanner) indexedSize() bool {
	indexed := s.argIndex()
	starred := s.sizeOperand()
	return indexed && !starred
}

func (s *scanner) skipFlags() {
	for s.pos < len(s.format) && strings.IndexByte("#0+- ", s.format[s.pos]) >= 0 {
		s.pos++
	}
}

func (s *scanner) sizeOperand() bool {
	if s.pos < len(s.format) && s.format[s.pos] == '*' {
		s.pos++
		s.arg++
		return true
	}
	for s.pos < len(s.format) && s.format[s.pos] >= '0' && s.format[s.pos] <= '9' {
		s.pos++
	}
	return false
}
