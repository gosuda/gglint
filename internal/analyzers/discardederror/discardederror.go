package discardederror

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/inspection"
)

const messageFormat = "error result of %s is dropped; handle or propagate it"

var Analyzer = &analysis.Analyzer{
	Name: "discarded_error",
	Doc: `report call statements and blank assignments that drop an error result

A call used as an expression statement whose results include error drops the
error. So does an = or := assignment whose blank identifier receives the
single error result of a call (_ = f()) or the error element of a call's
tuple result (_, _ = g()). Parentheses around the call are looked through.
Calls that cannot fail in practice are exempt: fmt.Print, fmt.Printf,
fmt.Println, fmt.Fprint, fmt.Fprintf, and fmt.Fprintln whose writer is
os.Stdout or os.Stderr, and the Write* methods of *bytes.Buffer and
*strings.Builder. defer and go statements are not reported.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	nodes := []ast.Node{(*ast.ExprStmt)(nil), (*ast.AssignStmt)(nil)}
	inspection.Of(pass).Preorder(nodes, func(n ast.Node) {
		call := droppedCall(pass.TypesInfo, n)
		if call == nil {
			return
		}
		pass.Report(analysis.Diagnostic{
			Pos:     call.Pos(),
			End:     call.End(),
			Message: fmt.Sprintf(messageFormat, types.ExprString(call)),
		})
	})
	return nil, nil
}

func droppedCall(info *types.Info, n ast.Node) *ast.CallExpr {
	switch s := n.(type) {
	case *ast.ExprStmt:
		return exprStmtCall(info, s)
	case *ast.AssignStmt:
		return assignStmtCall(info, s)
	}
	return nil
}

func exprStmtCall(info *types.Info, s *ast.ExprStmt) *ast.CallExpr {
	call := callOf(s.X)
	if call == nil || benignCall(info, call) {
		return nil
	}
	if !slices.ContainsFunc(resultTypes(info, call), isError) {
		return nil
	}
	return call
}

func assignStmtCall(info *types.Info, s *ast.AssignStmt) *ast.CallExpr {
	if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
		return nil
	}
	switch {
	case len(s.Lhs) == len(s.Rhs):
		return pairedBlankCall(info, s.Lhs, s.Rhs)
	case len(s.Rhs) == 1:
		return tupleBlankCall(info, s.Lhs, s.Rhs[0])
	}
	return nil
}

func pairedBlankCall(info *types.Info, lhs, rhs []ast.Expr) *ast.CallExpr {
	for i := range lhs {
		call := callOf(rhs[i])
		if call == nil || !isBlank(lhs[i]) || benignCall(info, call) {
			continue
		}
		if res := resultTypes(info, call); len(res) == 1 && isError(res[0]) {
			return call
		}
	}
	return nil
}

func tupleBlankCall(info *types.Info, lhs []ast.Expr, e ast.Expr) *ast.CallExpr {
	call := callOf(e)
	if call == nil || benignCall(info, call) {
		return nil
	}
	res := resultTypes(info, call)
	if len(res) != len(lhs) {
		return nil
	}
	for i, l := range lhs {
		if isBlank(l) && isError(res[i]) {
			return call
		}
	}
	return nil
}

func benignCall(info *types.Info, call *ast.CallExpr) bool {
	fn := calleeFunc(info, call)
	if fn == nil {
		return false
	}
	switch name := fn.FullName(); name {
	case "fmt.Print", "fmt.Printf", "fmt.Println":
		return true
	case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln":
		return len(call.Args) > 0 && isStdoutOrStderr(info, call.Args[0])
	default:
		return strings.HasPrefix(name, "(*bytes.Buffer).Write") ||
			strings.HasPrefix(name, "(*strings.Builder).Write")
	}
}

func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return nil
	}
	fn, ok := info.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	return fn
}

func isStdoutOrStderr(info *types.Info, w ast.Expr) bool {
	sel, ok := w.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, ok := info.Uses[sel.Sel].(*types.Var)
	if !ok || v.Pkg() == nil || v.Pkg().Path() != "os" {
		return false
	}
	return v.Name() == "Stdout" || v.Name() == "Stderr"
}

func callOf(e ast.Expr) *ast.CallExpr {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil
	}
	return call
}

func resultTypes(info *types.Info, call *ast.CallExpr) []types.Type {
	t := info.TypeOf(call)
	if t == nil {
		return nil
	}
	tuple, ok := t.(*types.Tuple)
	if !ok {
		return []types.Type{t}
	}
	out := make([]types.Type, tuple.Len())
	for i := range out {
		out[i] = tuple.At(i).Type()
	}
	return out
}

func isError(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}
