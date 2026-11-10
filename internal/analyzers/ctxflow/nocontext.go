package ctxflow

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const httpDoReplacement = "http.NewRequestWithContext and (*http.Client).Do"

var nocontextAnalyzer = &analysis.Analyzer{
	Name:     "nocontext",
	Doc:      "report calls that cannot be cancelled where a usable context is in scope",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runNocontext,
}

func runNocontext(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.CallExpr)(nil)) {
		checkNocontext(pass, cur)
	}
	return nil, nil
}

func checkNocontext(pass *analysis.Pass, cur inspector.Cursor) {
	call, ok := cur.Node().(*ast.CallExpr)
	if !ok {
		return
	}
	left, right, ok := nocontextTarget(pass.TypesInfo, call)
	if !ok || !nocontextInScope(pass, cur, call) {
		return
	}
	if isWrapper(pass, cur, call, right) {
		return
	}
	if left == "http.NewRequest" && newRequestGetsContext(pass, cur, call) {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: "a context is in scope and " + left + " cannot be cancelled: use " + right,
	})
}

func nocontextTarget(info *types.Info, call *ast.CallExpr) (left, right string, found bool) {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return "", "", false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return "", "", false
	}
	if recv := sig.Recv(); recv != nil {
		return nocontextMethod(fn, recv)
	}
	return nocontextFunc(fn)
}

func nocontextFunc(fn *types.Func) (left, right string, found bool) {
	pkg := fn.Pkg()
	if pkg == nil {
		return "", "", false
	}
	switch pkg.Path() {
	case "net":
		return netTarget(fn.Name())
	case netHTTPPath:
		return httpFuncTarget(fn.Name())
	case "os/exec":
		if fn.Name() == "Command" {
			return "exec.Command", "exec.CommandContext", true
		}
	}
	return "", "", false
}

func netTarget(name string) (left, right string, found bool) {
	if name == "Dial" {
		return "net.Dial", "(&net.Dialer{}).DialContext", true
	}
	return "", "", false
}

func httpFuncTarget(name string) (left, right string, found bool) {
	switch name {
	case "Get", "Head", "Post", "PostForm":
		return "http." + name, httpDoReplacement, true
	case "NewRequest":
		return "http.NewRequest", "http.NewRequestWithContext", true
	}
	return "", "", false
}

func nocontextMethod(fn *types.Func, recv *types.Var) (left, right string, found bool) {
	t := recv.Type()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return "", "", false
	}
	obj := named.Obj()
	pkg := obj.Pkg()
	if pkg == nil {
		return "", "", false
	}
	recvName := "(*" + pkg.Name() + "." + obj.Name() + ")."
	switch pkg.Path() {
	case netHTTPPath:
		return httpClientTarget(recvName, obj.Name(), fn.Name())
	case "database/sql":
		return sqlTarget(recvName, obj.Name(), fn.Name())
	}
	return "", "", false
}

func httpClientTarget(recvName, typeName, method string) (left, right string, found bool) {
	if typeName != "Client" {
		return "", "", false
	}
	switch method {
	case "Get", "Head", "Post", "PostForm":
		return recvName + method, httpDoReplacement, true
	}
	return "", "", false
}

func sqlTarget(recvName, typeName, method string) (left, right string, found bool) {
	switch typeName {
	case "DB":
		return sqlDBTarget(recvName, method)
	case "Tx":
		switch method {
		case "Query", "QueryRow", "Exec", "Prepare":
			return recvName + method, recvName + method + "Context", true
		}
	}
	return "", "", false
}

func sqlDBTarget(recvName, method string) (left, right string, found bool) {
	switch method {
	case "Query", "QueryRow", "Exec", "Prepare", "Ping":
		return recvName + method, recvName + method + "Context", true
	case "Begin":
		return recvName + "Begin", recvName + "BeginTx", true
	}
	return "", "", false
}

func isWrapper(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr, right string) bool {
	var name string
	var fn inspector.Cursor
	for fcur := range cur.Enclosing((*ast.FuncDecl)(nil)) {
		if d, ok := fcur.Node().(*ast.FuncDecl); ok {
			name = d.Name.Name
			fn = fcur
			break
		}
	}
	if name == "" || !matchesReplacement(name, right) {
		return false
	}
	if samePackage(pass, call) {
		return true
	}
	return usesContextParam(pass, fn, cur)
}

func matchesReplacement(name, right string) bool {
	return slices.Contains(replacementNames(right), name)
}

func replacementNames(right string) []string {
	var out []string
	for part := range strings.SplitSeq(right, " and ") {
		last := ""
		for seg := range strings.SplitSeq(part, ".") {
			last = seg
		}
		if last != "" {
			out = append(out, last)
		}
	}
	return out
}

func samePackage(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || pass.Pkg == nil {
		return false
	}
	return fn.Pkg().Path() == pass.Pkg.Path()
}

func usesContextParam(pass *analysis.Pass, fn, cur inspector.Cursor) bool {
	for c := range fn.Preorder((*ast.Ident)(nil)) {
		id, ok := c.Node().(*ast.Ident)
		if !ok {
			continue
		}
		if _, isDef := pass.TypesInfo.Defs[id]; isDef {
			continue
		}
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok && v != nil && isContextParam(pass, cur, v) {
			return true
		}
	}
	return false
}

func nocontextInScope(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr) bool {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		if hasNamedContextParam(pass.TypesInfo, fn.Node()) {
			return true
		}
		if hasContextLocalBefore(pass, fn, call) {
			return true
		}
	}
	return false
}

func hasNamedContextParam(info *types.Info, fn ast.Node) bool {
	ft := funcTypeOf(fn)
	if ft == nil || ft.Params == nil {
		return false
	}
	for _, field := range ft.Params.List {
		if !isContextType(info, field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name.Name != "" && name.Name != "_" {
				return true
			}
		}
	}
	return false
}

func hasContextLocalBefore(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr) bool {
	for c := range fn.Preorder((*ast.Ident)(nil)) {
		id, ok := c.Node().(*ast.Ident)
		if !ok || id.Name == "" || id.Name == "_" || id.Pos() >= call.Pos() {
			continue
		}
		if ownerOf(c) != fn {
			continue
		}
		if v := varOf(pass.TypesInfo, id); v != nil && isNamedContextType(v.Type()) {
			return true
		}
	}
	return false
}

func isNamedContextType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == contextName && obj.Pkg() != nil && obj.Pkg().Path() == contextPath
}

func isContextValueType(info *types.Info, e ast.Expr) bool {
	return isNamedContextType(info.TypeOf(e))
}

func newRequestGetsContext(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr) bool {
	v := requestResultVar(pass, cur, call)
	if v == nil {
		return false
	}
	fn := ownerOf(cur)
	if fn.Node() == nil {
		return false
	}
	return getsContextBeforeSend(pass, fn, call, v)
}

func requestResultVar(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr) *types.Var {
	for ac := range cur.Enclosing((*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil)) {
		switch n := ac.Node().(type) {
		case *ast.AssignStmt:
			return firstResultVar(pass, n.Lhs, n.Rhs, call)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, 0, len(n.Names))
			for _, id := range n.Names {
				lhs = append(lhs, id)
			}
			return firstResultVar(pass, lhs, n.Values, call)
		}
		return nil
	}
	return nil
}

func firstResultVar(pass *analysis.Pass, lhs, rhs []ast.Expr, call *ast.CallExpr) *types.Var {
	if len(rhs) != 1 || stripParens(rhs[0]) != call {
		return nil
	}
	if len(lhs) == 0 {
		return nil
	}
	id, ok := stripParens(lhs[0]).(*ast.Ident)
	if !ok || id.Name == "" || id.Name == "_" {
		return nil
	}
	return varOf(pass.TypesInfo, id)
}

func getsContextBeforeSend(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr, v *types.Var) bool {
	for cc := range fn.Preorder((*ast.CallExpr)(nil)) {
		c, ok := cc.Node().(*ast.CallExpr)
		if !ok || c == call || c.Pos() < call.End() {
			continue
		}
		if withContextOn(pass, c, v) || passedWithContext(pass, c, v) {
			return true
		}
	}
	return false
}

func withContextOn(pass *analysis.Pass, c *ast.CallExpr, v *types.Var) bool {
	sel, ok := stripParens(c.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "WithContext" {
		return false
	}
	if !identResolvesTo(pass.TypesInfo, sel.X, v) {
		return false
	}
	for _, arg := range c.Args {
		if isContextValueType(pass.TypesInfo, arg) {
			return true
		}
	}
	return false
}

func passedWithContext(pass *analysis.Pass, c *ast.CallExpr, v *types.Var) bool {
	seenVar := false
	seenCtx := false
	for _, arg := range c.Args {
		switch {
		case identResolvesTo(pass.TypesInfo, arg, v):
			seenVar = true
		case isContextValueType(pass.TypesInfo, arg):
			seenCtx = true
		}
	}
	return seenVar && seenCtx
}
