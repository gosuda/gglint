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
os.Stdout or os.Stderr, the Write* methods of *bytes.Buffer and
*strings.Builder, and Write on a receiver whose static type (or its pointer)
implements hash.Hash, whose Write never returns an error. defer and go
statements are not reported.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

type checker struct {
	info *types.Info
	hash *types.Interface
}

func run(pass *analysis.Pass) (any, error) {
	c := checker{info: pass.TypesInfo, hash: hashInterface(pass.Pkg)}
	nodes := []ast.Node{(*ast.ExprStmt)(nil), (*ast.AssignStmt)(nil)}
	inspection.Of(pass).Preorder(nodes, func(n ast.Node) {
		call := c.droppedCall(n)
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

func hashInterface(pkg *types.Package) *types.Interface {
	hash := findPackage(pkg, "hash")
	if hash == nil {
		return nil
	}
	obj, ok := hash.Scope().Lookup("Hash").(*types.TypeName)
	if !ok {
		return nil
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	return iface
}

func findPackage(root *types.Package, path string) *types.Package {
	seen := map[*types.Package]bool{root: true}
	queue := []*types.Package{root}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if pkg.Path() == path {
			return pkg
		}
		for _, imp := range pkg.Imports() {
			if !seen[imp] {
				seen[imp] = true
				queue = append(queue, imp)
			}
		}
	}
	return nil
}

func (c checker) droppedCall(n ast.Node) *ast.CallExpr {
	switch s := n.(type) {
	case *ast.ExprStmt:
		return c.exprStmtCall(s)
	case *ast.AssignStmt:
		return c.assignStmtCall(s)
	}
	return nil
}

func (c checker) exprStmtCall(s *ast.ExprStmt) *ast.CallExpr {
	call := callOf(s.X)
	if call == nil || c.benignCall(call) {
		return nil
	}
	if !slices.ContainsFunc(c.resultTypes(call), isError) {
		return nil
	}
	return call
}

func (c checker) assignStmtCall(s *ast.AssignStmt) *ast.CallExpr {
	if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
		return nil
	}
	switch {
	case len(s.Lhs) == len(s.Rhs):
		return c.pairedBlankCall(s.Lhs, s.Rhs)
	case len(s.Rhs) == 1:
		return c.tupleBlankCall(s.Lhs, s.Rhs[0])
	}
	return nil
}

func (c checker) pairedBlankCall(lhs, rhs []ast.Expr) *ast.CallExpr {
	for i := range lhs {
		call := callOf(rhs[i])
		if call == nil || !isBlank(lhs[i]) || c.benignCall(call) {
			continue
		}
		if res := c.resultTypes(call); len(res) == 1 && isError(res[0]) {
			return call
		}
	}
	return nil
}

func (c checker) tupleBlankCall(lhs []ast.Expr, e ast.Expr) *ast.CallExpr {
	call := callOf(e)
	if call == nil || c.benignCall(call) {
		return nil
	}
	res := c.resultTypes(call)
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

func (c checker) benignCall(call *ast.CallExpr) bool {
	fn := c.calleeFunc(call)
	if fn == nil {
		return false
	}
	switch name := fn.FullName(); name {
	case "fmt.Print", "fmt.Printf", "fmt.Println":
		return true
	case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln":
		return len(call.Args) > 0 && c.isStdoutOrStderr(call.Args[0])
	default:
		return strings.HasPrefix(name, "(*bytes.Buffer).Write") ||
			strings.HasPrefix(name, "(*strings.Builder).Write") ||
			c.isHashWrite(call)
	}
}

func (c checker) isHashWrite(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || c.hash == nil || sel.Sel.Name != "Write" {
		return false
	}
	recv := c.info.TypeOf(sel.X)
	if recv == nil {
		return false
	}
	if types.Implements(recv, c.hash) {
		return true
	}
	_, isPtr := recv.Underlying().(*types.Pointer)
	return !isPtr && types.Implements(types.NewPointer(recv), c.hash)
}

func (c checker) calleeFunc(call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return nil
	}
	fn, ok := c.info.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	return fn
}

func (c checker) isStdoutOrStderr(w ast.Expr) bool {
	sel, ok := w.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, ok := c.info.Uses[sel.Sel].(*types.Var)
	if !ok || v.Pkg() == nil || v.Pkg().Path() != "os" {
		return false
	}
	return v.Name() == "Stdout" || v.Name() == "Stderr"
}

func (c checker) resultTypes(call *ast.CallExpr) []types.Type {
	t := c.info.TypeOf(call)
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

func callOf(e ast.Expr) *ast.CallExpr {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil
	}
	return call
}

func isError(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}
