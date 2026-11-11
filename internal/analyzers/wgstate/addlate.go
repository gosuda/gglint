package wgstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
	"github.com/gosuda/gglint/internal/inspection"
)

var addlateAnalyzer = &analysis.Analyzer{
	Name:     "addlate",
	Doc:      "report a WaitGroup Add that is a plain statement after a go statement whose function literal calls Done, in the same statement list; an Add earlier in the function silences it, also an Add inside a function literal that is called before the go statement directly or through a local variable that can hold it at that call; an Add in a go statement never counts as earlier, and an Add in a defer statement in the function itself does not, while a defer inside a called literal does; a literal that receives or sends, ranges over a channel, or locks or waits is not reported, because that wait can order the Add; the check cannot know the capacity of every channel, so it stays silent for a buffered channel too; a call to another package or through a function value is assumed not to wait; a call passed the WaitGroup counts unless it is a same-package function whose body neither Adds on the receiving parameter directly, through an alias, or inside a literal, nor hands it on",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runAddlate,
}

type lateGroup struct {
	list  []ast.Stmt
	goIdx int
	seen  map[token.Pos]bool
}

func runAddlate(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		fn := cur.Node()
		checkAddlate(pass, fn, enclosingFuncBodies(cur, fn))
	}
	return nil, nil
}

func checkAddlate(pass *analysis.Pass, fn ast.Node, enclosing []*ast.BlockStmt) {
	first := firstAddPositions(pass, fn, enclosing)
	eachList(fn, func(list []ast.Stmt) {
		seen := map[token.Pos]bool{}
		for i, s := range list {
			g, ok := unwrap(s).(*ast.GoStmt)
			if !ok {
				continue
			}
			checkGoAddlate(pass, fn, g, lateGroup{list: list, goIdx: i, seen: seen}, first, enclosing)
		}
	})
}

func firstAddPositions(pass *analysis.Pass, fn ast.Node, enclosing []*ast.BlockStmt) map[flow.Key]token.Pos {
	out := map[flow.Key]token.Pos{}
	body := funcBody(fn)
	if body == nil {
		return out
	}
	collectDirectAdds(pass, body, out)
	mergeInPlaceAdds(pass, body, out)
	mergeVarLitAdds(pass, fn, body, out)
	if containsDirectGo(body) {
		mergeEnclosingAdds(enclosing, fn.Pos(), pass, out)
	}
	return out
}

func collectDirectAdds(pass *analysis.Pass, body *ast.BlockStmt, out map[flow.Key]token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ak, _, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok {
			return true
		}
		if p, dup := out[ak]; !dup || call.Pos() < p {
			out[ak] = call.Pos()
		}
		return true
	})
}

func mergeEnclosingAdds(enclosing []*ast.BlockStmt, before token.Pos, pass *analysis.Pass, out map[flow.Key]token.Pos) {
	for _, body := range enclosing {
		collectEnclosingAdds(pass, body, before, out)
	}
}

func enclosingFuncBodies(cur inspector.Cursor, fn ast.Node) []*ast.BlockStmt {
	var out []*ast.BlockStmt
	for enc := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		if enc.Node() == fn {
			continue
		}
		if body := funcBody(enc.Node()); body != nil {
			out = append(out, body)
		}
	}
	return out
}

func containsDirectGo(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			found = true
			return false
		}
		return true
	})
	return found
}

func collectEnclosingAdds(pass *analysis.Pass, body *ast.BlockStmt, before token.Pos, out map[flow.Key]token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= before {
			return true
		}
		ak, _, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok {
			return true
		}
		if p, dup := out[ak]; !dup || call.Pos() < p {
			out[ak] = call.Pos()
		}
		return true
	})
}

func litAddKeys(pass *analysis.Pass, lit *ast.FuncLit) []flow.Key {
	seen := map[flow.Key]bool{}
	var out []flow.Key
	if lit.Body == nil {
		return out
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		k, _, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok || seen[k] {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return true
	})
	return out
}

func mergeInPlaceAdds(pass *analysis.Pass, body *ast.BlockStmt, out map[flow.Key]token.Pos) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
		if !ok {
			return true
		}
		for _, k := range litAddKeys(pass, lit) {
			if p, dup := out[k]; !dup || call.Pos() < p {
				out[k] = call.Pos()
			}
		}
		return true
	})
}

func mergeVarLitAdds(pass *analysis.Pass, fn ast.Node, body *ast.BlockStmt, out map[flow.Key]token.Pos) {
	set := newVarLitSet()
	collectVarLits(pass.TypesInfo, body, set)
	applyVarLits(pass, fn, set, out)
}

func checkGoAddlate(pass *analysis.Pass, fn ast.Node, g *ast.GoStmt, group lateGroup, first map[flow.Key]token.Pos, enclosing []*ast.BlockStmt) {
	lit, ok := ast.Unparen(g.Call.Fun).(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return
	}
	plain, deferred := donePositions(pass, lit)
	if len(plain) == 0 && len(deferred) == 0 {
		return
	}
	if waitsForParent(pass, lit) {
		return
	}
	for _, k := range sortedDoneKeys(plain, deferred) {
		if hasAddBefore(first, g, k) {
			continue
		}
		if possibleAddBefore(pass, fn, enclosing, g, k) {
			continue
		}
		reportLateAdds(pass, group, k)
	}
}

func possibleAddBefore(pass *analysis.Pass, fn ast.Node, enclosing []*ast.BlockStmt, g *ast.GoStmt, k flow.Key) bool {
	if possibleAddInBody(pass, funcBody(fn), g.Pos(), k) {
		return true
	}
	for _, body := range enclosing {
		if possibleAddInBody(pass, body, g.Pos(), k) {
			return true
		}
	}
	return false
}

func possibleAddInBody(pass *analysis.Pass, body *ast.BlockStmt, before token.Pos, k flow.Key) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= before {
			return true
		}
		if possibleAddCall(pass, call, k) {
			found = true
			return false
		}
		return true
	})
	return found
}

func waitsForParent(pass *analysis.Pass, lit *ast.FuncLit) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		}
		if nodeWaits(pass, n) {
			found = true
			return false
		}
		return true
	})
	return found
}

func nodeWaits(pass *analysis.Pass, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.ARROW
	case *ast.SendStmt:
		return true
	case *ast.RangeStmt:
		return isChanRange(pass, n)
	case *ast.ForStmt:
		return spinsOnLoad(pass, n)
	case *ast.CallExpr:
		return isSyncWait(pass, n) || helperWaits(pass, n)
	}
	return false
}

func helperWaits(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != pass.Pkg {
		return false
	}
	body := helperBody(pass, fn)
	if body == nil {
		return false
	}
	waits := false
	ast.Inspect(body, func(n ast.Node) bool {
		if waits {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if directWaits(pass, n) {
			waits = true
			return false
		}
		return true
	})
	return waits
}

func directWaits(pass *analysis.Pass, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.ARROW
	case *ast.SendStmt:
		return true
	case *ast.RangeStmt:
		return isChanRange(pass, n)
	case *ast.CallExpr:
		return isSyncWait(pass, n)
	}
	return false
}

func helperDecl(pass *analysis.Pass, fn *types.Func) *ast.FuncDecl {
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if pass.TypesInfo.ObjectOf(fd.Name) == fn {
				return fd
			}
		}
	}
	return nil
}

func helperBody(pass *analysis.Pass, fn *types.Func) *ast.BlockStmt {
	if decl := helperDecl(pass, fn); decl != nil {
		return decl.Body
	}
	return nil
}

func isPackageVar(obj types.Object, pass *analysis.Pass) bool {
	return obj != nil && obj.Parent() == pass.Pkg.Scope()
}

func receiverVar(pass *analysis.Pass, decl *ast.FuncDecl) *types.Var {
	if decl.Recv == nil || len(decl.Recv.List) == 0 || len(decl.Recv.List[0].Names) == 0 {
		return nil
	}
	v, ok := pass.TypesInfo.ObjectOf(decl.Recv.List[0].Names[0]).(*types.Var)
	if !ok {
		return nil
	}
	return v
}

func paramVars(pass *analysis.Pass, decl *ast.FuncDecl) []*types.Var {
	var out []*types.Var
	if decl.Type == nil || decl.Type.Params == nil {
		return out
	}
	for _, field := range decl.Type.Params.List {
		for _, name := range field.Names {
			if v, ok := pass.TypesInfo.ObjectOf(name).(*types.Var); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

func callReceiverKey(pass *analysis.Pass, call *ast.CallExpr) flow.Key {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return flow.Key{}
	}
	k, _ := flow.KeyOf(pass.TypesInfo, sel.X)
	return k
}

func joinPath(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "." + b
}

func spinsOnLoad(pass *analysis.Pass, stmt *ast.ForStmt) bool {
	if stmt.Init != nil || stmt.Post != nil || stmt.Cond == nil {
		return false
	}
	return isLoadWaitCond(pass, stmt.Cond)
}

var loadCompareTokens = map[token.Token]bool{
	token.EQL: true,
	token.NEQ: true,
	token.LSS: true,
	token.LEQ: true,
	token.GTR: true,
	token.GEQ: true,
}

func isLoadWaitCond(pass *analysis.Pass, cond ast.Expr) bool {
	switch n := ast.Unparen(cond).(type) {
	case *ast.CallExpr:
		return isLoadCall(pass, n)
	case *ast.UnaryExpr:
		return isNegatedLoad(pass, n)
	case *ast.BinaryExpr:
		return isComparedLoad(pass, n)
	}
	return false
}

func isLoadCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isAtomicLoad(pass, call)
}

func isNegatedLoad(pass *analysis.Pass, n *ast.UnaryExpr) bool {
	if n.Op != token.NOT {
		return false
	}
	call, ok := ast.Unparen(n.X).(*ast.CallExpr)
	return ok && isAtomicLoad(pass, call)
}

func isComparedLoad(pass *analysis.Pass, n *ast.BinaryExpr) bool {
	if !loadCompareTokens[n.Op] {
		return false
	}
	left, lok := ast.Unparen(n.X).(*ast.CallExpr)
	right, rok := ast.Unparen(n.Y).(*ast.CallExpr)
	if lok && isAtomicLoad(pass, left) && isConst(pass, n.Y) {
		return true
	}
	return rok && isAtomicLoad(pass, right) && isConst(pass, n.X)
}

func isConst(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	return ok && tv.Value != nil
}

func isAtomicLoad(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	if fn.Pkg().Path() != "sync/atomic" {
		return false
	}
	return fn.Name() == "Load" || strings.HasPrefix(fn.Name(), "Load")
}

func isChanRange(pass *analysis.Pass, n *ast.RangeStmt) bool {
	t := pass.TypesInfo.TypeOf(n.X)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func isSyncWait(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(pass.TypesInfo, call)
	if fn == nil {
		return false
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock", "(*sync.RWMutex).RLock", "(*sync.WaitGroup).Wait", "(*sync.Cond).Wait":
		return true
	}
	return false
}

func sortedDoneKeys(plain, deferred map[flow.Key][]token.Pos) []flow.Key {
	merged := map[flow.Key]struct{}{}
	for k := range plain {
		merged[k] = struct{}{}
	}
	for k := range deferred {
		merged[k] = struct{}{}
	}
	keys := slices.Collect(maps.Keys(merged))
	slices.SortFunc(keys, func(a, b flow.Key) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(keyName(a), keyName(b)), cmp.Compare(rootPos(a), rootPos(b)))
	})
	return keys
}

func keyName(k flow.Key) string {
	if k.Root == nil {
		return ""
	}
	return k.Root.Name()
}

func rootPos(k flow.Key) token.Pos {
	if k.Root == nil {
		return token.NoPos
	}
	return k.Root.Pos()
}

func reportLateAdds(pass *analysis.Pass, group lateGroup, k flow.Key) {
	after := group.list[group.goIdx+1:]
	for _, s := range after {
		call, ok := directCall(s)
		if !ok {
			continue
		}
		ak, text, ok := wgKey(pass.TypesInfo, call, "Add")
		if !ok || ak != k {
			continue
		}
		if group.seen[call.Pos()] {
			continue
		}
		group.seen[call.Pos()] = true
		pass.Reportf(call.Pos(), "this Add runs after the goroutine it counts has started, so its Done can run first and panic with a negative WaitGroup counter, or a Wait before this Add can return early: call %s.Add before the go statement", text)
	}
}

func donePositions(pass *analysis.Pass, lit *ast.FuncLit) (plain, deferred map[flow.Key][]token.Pos) {
	plain = map[flow.Key][]token.Pos{}
	deferred = map[flow.Key][]token.Pos{}
	for _, s := range lit.Body.List {
		if d, ok := unwrap(s).(*ast.DeferStmt); ok {
			collectDonesIn(pass, d, deferred)
		} else {
			collectDonesIn(pass, s, plain)
		}
	}
	return plain, deferred
}

func collectDonesIn(pass *analysis.Pass, n ast.Node, out map[flow.Key][]token.Pos) {
	ast.Inspect(n, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if k, _, ok := wgKey(pass.TypesInfo, call, "Done"); ok {
			out[k] = append(out[k], call.Pos())
		}
		return true
	})
}

func hasAddBefore(first map[flow.Key]token.Pos, g *ast.GoStmt, k flow.Key) bool {
	p, ok := first[k]
	return ok && p < g.Pos()
}
