package ctxflow

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

const httpDoReplacement = "http.NewRequestWithContext and (*http.Client).Do"

var nocontextAnalyzer = &analysis.Analyzer{
	Name:     "nocontext",
	Doc:      "report the table calls that cannot be cancelled where a named context or *http.Request parameter, or a context local variable that is visible at the call exists; a wrapper is exempt only when its context parameter's value flows on: as a call argument, a method receiver, the right side of an assignment to another name, a composite literal element, a sent value, or a return operand; comparing the parameter with nil, overwriting it, or dropping it to blank does not exempt",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runNocontext,
}

func runNocontext(pass *analysis.Pass) (any, error) {
	cache := &nocontextCache{
		locals:   map[ast.Node][]*types.Var{},
		uses:     map[ast.Node]map[flow.Key]token.Pos{},
		wrappers: map[ast.Node]bool{},
	}
	for cur := range inspection.Of(pass).Root().Preorder((*ast.CallExpr)(nil)) {
		checkNocontext(pass, cur, cache)
	}
	return nil, nil
}

type nocontextCache struct {
	locals   map[ast.Node][]*types.Var
	uses     map[ast.Node]map[flow.Key]token.Pos
	wrappers map[ast.Node]bool
}

func checkNocontext(pass *analysis.Pass, cur inspector.Cursor, cache *nocontextCache) {
	call, ok := cur.Node().(*ast.CallExpr)
	if !ok {
		return
	}
	left, right, ok := nocontextTarget(pass.TypesInfo, call)
	if !ok || !nocontextInScope(pass, cur, call, cache) {
		return
	}
	if isWrapper(pass, cur, call, right, cache) {
		return
	}
	if left == "http.NewRequest" && newRequestGetsContext(pass, cur, call, cache) {
		return
	}
	if manuallyCancelled(pass, cur, call) {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: "a context is in scope and " + left + " cannot be cancelled: use " + right,
	})
}

func manuallyCancelled(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr) bool {
	k, ok := requestResultVar(pass, cur, call)
	if !ok {
		return false
	}
	fn := ownerOf(cur)
	if fn.Node() == nil {
		return false
	}
	return resultInDoneClause(pass, fn, k)
}

func resultInDoneClause(pass *analysis.Pass, fn inspector.Cursor, target flow.Key) bool {
	for sel := range fn.Preorder((*ast.SelectStmt)(nil)) {
		stmt, ok := sel.Node().(*ast.SelectStmt)
		if !ok {
			continue
		}
		if selectMentionsResult(pass, stmt, target) {
			return true
		}
	}
	return false
}

func selectMentionsResult(pass *analysis.Pass, stmt *ast.SelectStmt, target flow.Key) bool {
	for _, s := range stmt.Body.List {
		clause, ok := s.(*ast.CommClause)
		if !ok || clause.Comm == nil {
			continue
		}
		if !receivesDone(pass.TypesInfo, clause.Comm) {
			continue
		}
		if mentionsResultKey(pass.TypesInfo, clause.Body, target) {
			return true
		}
	}
	return false
}

type resultKeySearch struct {
	info   *types.Info
	target flow.Key
	found  bool
}

func (s *resultKeySearch) visit(n ast.Node) bool {
	if s.found {
		return false
	}
	e, ok := n.(ast.Expr)
	if !ok {
		return true
	}
	k, ok := flow.KeyOf(s.info, e)
	if !ok || k.Root != s.target.Root {
		return true
	}
	if s.target.Path == "" || k.Path == s.target.Path || strings.HasPrefix(k.Path, s.target.Path+".") {
		s.found = true
		return false
	}
	return true
}

func mentionsResultKey(info *types.Info, body []ast.Stmt, target flow.Key) bool {
	s := &resultKeySearch{info: info, target: target}
	for _, stmt := range body {
		ast.Inspect(stmt, s.visit)
		if s.found {
			return true
		}
	}
	return false
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

func isWrapper(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr, right string, cache *nocontextCache) bool {
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
	if got, ok := cache.wrappers[fn.Node()]; ok {
		return got
	}
	got := usesContextParam(pass, fn)
	cache.wrappers[fn.Node()] = got
	return got
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

func usesContextParam(pass *analysis.Pass, fn inspector.Cursor) bool {
	for c := range fn.Preorder((*ast.Ident)(nil)) {
		if mentionsContextParam(pass, fn, c) {
			return true
		}
	}
	return false
}

func mentionsContextParam(pass *analysis.Pass, fn, c inspector.Cursor) bool {
	id, ok := c.Node().(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := pass.TypesInfo.Uses[id].(*types.Var)
	if !ok || v == nil {
		return false
	}
	if !funcDeclaresVarAsContextParam(pass.TypesInfo, fn.Node(), v) {
		return false
	}
	return flowsContextValue(c)
}

func flowsContextValue(c inspector.Cursor) bool {
	cur := c
	for p := cur.Parent(); p.Node() != nil; p = p.Parent() {
		switch n := p.Node().(type) {
		case *ast.ParenExpr:
			cur = p
			continue
		case *ast.BinaryExpr:
			return n.Op != token.EQL && n.Op != token.NEQ
		case *ast.AssignStmt:
			return assignedInRHS(n, cur.Node())
		case *ast.ValueSpec:
			return definedInNames(n, cur.Node())
		case *ast.ExprStmt:
			return false
		}
		return true
	}
	return true
}

func assignedInRHS(as *ast.AssignStmt, n ast.Node) bool {
	for _, lhs := range as.Lhs {
		if lhs == n || stripParens(lhs) == n {
			return false
		}
	}
	for i, rhs := range as.Rhs {
		if rhs == n || stripParens(rhs) == n {
			return !blankAt(as, i)
		}
	}
	return true
}

func definedInNames(vs *ast.ValueSpec, n ast.Node) bool {
	for i, val := range vs.Values {
		if val != n && stripParens(val) != n {
			continue
		}
		return i >= len(vs.Names) || vs.Names[i].Name != "_"
	}
	return true
}

func blankAt(as *ast.AssignStmt, i int) bool {
	if len(as.Lhs) != len(as.Rhs) {
		return false
	}
	lhsID, ok := stripParens(as.Lhs[i]).(*ast.Ident)
	return ok && lhsID.Name == "_"
}

func nocontextInScope(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr, cache *nocontextCache) bool {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		if hasNamedContextParam(pass.TypesInfo, fn.Node()) {
			return true
		}
		if hasContextLocalBefore(pass, fn, call, cache) {
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

func hasContextLocalBefore(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr, cache *nocontextCache) bool {
	pkgScope := pass.Pkg.Scope()
	if pkgScope == nil {
		return false
	}
	fnNode := fn.Node()
	if fnNode == nil {
		return false
	}
	for _, v := range contextLocals(pass, fnNode, cache) {
		if v.Pos() < call.Pos() && scopeHolds(pkgScope, v, call) {
			return true
		}
	}
	return false
}

func contextLocals(pass *analysis.Pass, fnNode ast.Node, cache *nocontextCache) []*types.Var {
	if vs, ok := cache.locals[fnNode]; ok {
		return vs
	}
	var vs []*types.Var
	for _, obj := range pass.TypesInfo.Defs {
		if v, ok := obj.(*types.Var); ok && isLocalContextVar(v, fnNode) {
			vs = append(vs, v)
		}
	}
	cache.locals[fnNode] = vs
	return vs
}

func isLocalContextVar(v *types.Var, fnNode ast.Node) bool {
	if v.IsField() || v.Name() == "" || v.Name() == "_" {
		return false
	}
	if !isNamedContextType(v.Type()) {
		return false
	}
	return v.Pos() >= fnNode.Pos() && v.Pos() <= fnNode.End()
}

func scopeHolds(pkgScope *types.Scope, v *types.Var, call *ast.CallExpr) bool {
	parent := v.Parent()
	if parent == nil || parent == pkgScope || !parent.Contains(call.Pos()) {
		return false
	}
	inner := pkgScope.Innermost(call.Pos())
	if inner == nil {
		return false
	}
	_, visible := inner.LookupParent(v.Name(), call.Pos())
	return visible == v
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

func newRequestGetsContext(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr, cache *nocontextCache) bool {
	k, ok := requestResultVar(pass, cur, call)
	if !ok {
		return false
	}
	fn := ownerOf(cur)
	if fn.Node() == nil {
		return false
	}
	return getsContextBeforeSend(pass, fn, call, k, cache)
}

func requestResultVar(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr) (flow.Key, bool) {
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
		return flow.Key{}, false
	}
	return flow.Key{}, false
}

func firstResultVar(pass *analysis.Pass, lhs, rhs []ast.Expr, call *ast.CallExpr) (flow.Key, bool) {
	if len(rhs) != 1 || stripParens(rhs[0]) != call {
		return flow.Key{}, false
	}
	if len(lhs) == 0 {
		return flow.Key{}, false
	}
	first := stripParens(lhs[0])
	if id, ok := first.(*ast.Ident); ok {
		v := varOf(pass.TypesInfo, id)
		if v == nil {
			return flow.Key{}, false
		}
		return flow.Key{Root: v}, true
	}
	k, ok := flow.KeyOf(pass.TypesInfo, first)
	if !ok {
		return flow.Key{}, false
	}
	return k, true
}

func getsContextBeforeSend(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr, k flow.Key, cache *nocontextCache) bool {
	return latestContextUse(pass, fn, cache)[k] >= call.End()
}

func latestContextUse(pass *analysis.Pass, fn inspector.Cursor, cache *nocontextCache) map[flow.Key]token.Pos {
	if done, ok := cache.uses[fn.Node()]; ok {
		return done
	}
	done := map[flow.Key]token.Pos{}
	for cc := range fn.Preorder((*ast.CallExpr)(nil)) {
		if c, ok := cc.Node().(*ast.CallExpr); ok {
			recordContextUse(pass, cc, c, done)
		}
	}
	cache.uses[fn.Node()] = done
	return done
}

func recordContextUse(pass *analysis.Pass, cc inspector.Cursor, c *ast.CallExpr, done map[flow.Key]token.Pos) {
	for _, k := range callKeys(pass, c) {
		if withContextOn(pass, cc, c, k) || passedWithContext(pass, c, k) {
			if pos := c.Pos(); pos > done[k] {
				done[k] = pos
			}
		}
	}
}

func callKeys(pass *analysis.Pass, c *ast.CallExpr) []flow.Key {
	var out []flow.Key
	seen := map[flow.Key]bool{}
	add := func(e ast.Expr) {
		k, ok := flow.KeyOf(pass.TypesInfo, stripParens(e))
		if !ok || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	if sel, ok := stripParens(c.Fun).(*ast.SelectorExpr); ok {
		add(sel.X)
	}
	for _, arg := range c.Args {
		add(arg)
	}
	return out
}

func withContextOn(pass *analysis.Pass, cc inspector.Cursor, c *ast.CallExpr, k flow.Key) bool {
	if !isWithContextCall(pass, c, k) {
		return false
	}
	if _, ok := cc.Parent().Node().(*ast.ExprStmt); ok {
		return false
	}
	for _, arg := range c.Args {
		if usableContextArg(pass, arg) {
			return true
		}
	}
	return false
}

func isWithContextCall(pass *analysis.Pass, c *ast.CallExpr, k flow.Key) bool {
	sel, ok := stripParens(c.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "WithContext" {
		return false
	}
	got, ok := flow.KeyOf(pass.TypesInfo, sel.X)
	return ok && got == k
}

func usableContextArg(pass *analysis.Pass, arg ast.Expr) bool {
	if !isContextValueType(pass.TypesInfo, arg) {
		return false
	}
	if call, ok := stripParens(arg).(*ast.CallExpr); ok && isRootCall(pass.TypesInfo, call) {
		return false
	}
	return true
}

func passedWithContext(pass *analysis.Pass, c *ast.CallExpr, k flow.Key) bool {
	seenVar := false
	seenCtx := false
	for _, arg := range c.Args {
		if got, ok := flow.KeyOf(pass.TypesInfo, arg); ok && got == k {
			seenVar = true
		} else if isContextValueType(pass.TypesInfo, arg) {
			seenCtx = true
		}
	}
	return seenVar && seenCtx
}
