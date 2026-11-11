package ctxflow

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const rootMessage = "a context is in scope, and this root context drops its cancellation and deadline: derive from it, or use context.WithoutCancel to detach on purpose"

var rootAnalyzer = &analysis.Analyzer{
	Name:     "root",
	Doc:      "report context.Background and context.TODO whose result is used where a named context is in scope; it stays silent in the bodies of main and init but not in literals inside them that take their own context parameter, in a nil-check fallback, in a comparison, and in a composite literal that holds that context",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runRoot,
}

func runRoot(pass *analysis.Pass) (any, error) {
	cache := &rootCache{shared: map[compositeKey]bool{}}
	for cur := range inspection.Of(pass).Root().Preorder((*ast.CallExpr)(nil)) {
		checkRoot(pass, cur, cache)
	}
	return nil, nil
}

type rootCache struct {
	shared map[compositeKey]bool
}

type compositeKey struct {
	lit   *ast.CompositeLit
	owner ast.Node
}

func checkRoot(pass *analysis.Pass, cur inspector.Cursor, cache *rootCache) {
	call, ok := cur.Node().(*ast.CallExpr)
	if !ok || !isRootCall(pass.TypesInfo, call) {
		return
	}
	if !resultUsed(cur) || !contextInScope(pass, cur) || inMainOrInit(cur) || nilGuarded(pass, cur, call) {
		return
	}
	if isCompared(cur) || sharesCompositeWithContext(pass, cur, cache) {
		return
	}
	pass.Report(analysis.Diagnostic{Pos: call.Pos(), End: call.End(), Message: rootMessage})
}

func isRootCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || len(call.Args) != 0 {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil {
		return false
	}
	if pkg := fn.Pkg(); pkg == nil || pkg.Path() != contextPath {
		return false
	}
	return fn.Name() == "Background" || fn.Name() == "TODO"
}

func resultUsed(cur inspector.Cursor) bool {
	switch n := parentAboveParens(cur).Node().(type) {
	case *ast.ExprStmt, *ast.GoStmt, *ast.DeferStmt:
		return false
	case *ast.AssignStmt:
		return !allBlank(n.Lhs)
	case *ast.ValueSpec:
		return !allBlankIdents(n.Names)
	}
	return true
}

func parentAboveParens(cur inspector.Cursor) inspector.Cursor {
	p := cur.Parent()
	for {
		if _, ok := p.Node().(*ast.ParenExpr); !ok {
			return p
		}
		p = p.Parent()
	}
}

func allBlank(exprs []ast.Expr) bool {
	for _, e := range exprs {
		id, ok := e.(*ast.Ident)
		if !ok || id.Name != "_" {
			return false
		}
	}
	return true
}

func allBlankIdents(ids []*ast.Ident) bool {
	for _, id := range ids {
		if id.Name != "_" {
			return false
		}
	}
	return true
}

func nilGuarded(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr) bool {
	as := enclosingAssign(cur, call)
	if as == nil {
		return false
	}
	v := assignedContextVar(pass, as)
	if v == nil {
		return false
	}
	return inNilIfBody(pass, cur, call, v) || afterGuardReturn(pass, cur, v, as)
}

func inNilIfBody(pass *analysis.Pass, cur inspector.Cursor, call *ast.CallExpr, v *types.Var) bool {
	for icur := range cur.Enclosing((*ast.IfStmt)(nil)) {
		ifs, ok := icur.Node().(*ast.IfStmt)
		if !ok {
			continue
		}
		if inBody(call, ifs) && impliesNil(pass.TypesInfo, ifs.Cond, v) {
			return true
		}
		if inElse(call, ifs) && impliesLive(pass.TypesInfo, ifs.Cond, v) {
			return true
		}
	}
	return false
}

func afterGuardReturn(pass *analysis.Pass, cur inspector.Cursor, v *types.Var, as *ast.AssignStmt) bool {
	for bcur := range cur.Enclosing((*ast.BlockStmt)(nil)) {
		block, ok := bcur.Node().(*ast.BlockStmt)
		if !ok {
			continue
		}
		return guardReturnBefore(pass, block, as, v)
	}
	return false
}

func guardReturnBefore(pass *analysis.Pass, block *ast.BlockStmt, as *ast.AssignStmt, v *types.Var) bool {
	for i, st := range block.List {
		if st != as {
			continue
		}
		for _, prev := range block.List[:i] {
			if guardReturnsOnLive(pass, prev, v) {
				return true
			}
		}
		return false
	}
	return false
}

func guardReturnsOnLive(pass *analysis.Pass, st ast.Stmt, v *types.Var) bool {
	ifs, ok := st.(*ast.IfStmt)
	if !ok || ifs.Else != nil || !comparesLiveWith(pass.TypesInfo, ifs.Cond, v) {
		return false
	}
	return bodyEndsInReturn(ifs.Body)
}

func bodyEndsInReturn(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	_, ok := b.List[len(b.List)-1].(*ast.ReturnStmt)
	return ok
}

func inBody(call *ast.CallExpr, ifs *ast.IfStmt) bool {
	return ifs.Body != nil && call.Pos() >= ifs.Body.Pos() && call.End() <= ifs.Body.End()
}

func inElse(call *ast.CallExpr, ifs *ast.IfStmt) bool {
	return ifs.Else != nil && call.Pos() >= ifs.Else.Pos() && call.End() <= ifs.Else.End()
}

func impliesNil(info *types.Info, cond ast.Expr, v *types.Var) bool {
	cond = stripParens(cond)
	if b, ok := cond.(*ast.BinaryExpr); ok {
		if b.Op == token.LOR {
			return impliesNil(info, b.X, v) && impliesNil(info, b.Y, v)
		}
		if b.Op == token.LAND {
			return impliesNil(info, b.X, v) || impliesNil(info, b.Y, v)
		}
	}
	return comparesNilWith(info, cond, v)
}

func impliesLive(info *types.Info, cond ast.Expr, v *types.Var) bool {
	cond = stripParens(cond)
	if b, ok := cond.(*ast.BinaryExpr); ok {
		if b.Op == token.LOR {
			return impliesLive(info, b.X, v) || impliesLive(info, b.Y, v)
		}
		if b.Op == token.LAND {
			return impliesLive(info, b.X, v) && impliesLive(info, b.Y, v)
		}
	}
	return comparesLiveWith(info, cond, v)
}

func comparesLiveWith(info *types.Info, cond ast.Expr, v *types.Var) bool {
	b, ok := stripParens(cond).(*ast.BinaryExpr)
	if !ok || b.Op != token.NEQ {
		return false
	}
	if identResolvesTo(info, b.X, v) && isNilIdent(info, b.Y) {
		return true
	}
	return identResolvesTo(info, b.Y, v) && isNilIdent(info, b.X)
}

func enclosingAssign(cur inspector.Cursor, call *ast.CallExpr) *ast.AssignStmt {
	c := cur
	for {
		p := c.Parent()
		n := p.Node()
		if n == nil {
			return nil
		}
		if as, ok := n.(*ast.AssignStmt); ok {
			if inRHS(as, call) {
				return as
			}
			return nil
		}
		if _, ok := n.(ast.Stmt); ok {
			return nil
		}
		if _, ok := n.(ast.Decl); ok {
			return nil
		}
		c = p
	}
}

func inRHS(as *ast.AssignStmt, call *ast.CallExpr) bool {
	if len(as.Rhs) == 0 {
		return false
	}
	start := as.Rhs[0].Pos()
	end := as.Rhs[len(as.Rhs)-1].End()
	return call.Pos() >= start && call.End() <= end
}

func assignedContextVar(pass *analysis.Pass, as *ast.AssignStmt) *types.Var {
	for _, lhs := range as.Lhs {
		id, ok := stripParens(lhs).(*ast.Ident)
		if !ok {
			continue
		}
		v := varOf(pass.TypesInfo, id)
		if v != nil && isContextVarType(v.Type()) {
			return v
		}
	}
	return nil
}

func isContextVarType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == contextName && obj.Pkg() != nil && obj.Pkg().Path() == contextPath
}

func comparesNilWith(info *types.Info, cond ast.Expr, v *types.Var) bool {
	b, ok := stripParens(cond).(*ast.BinaryExpr)
	if !ok || b.Op != token.EQL {
		return false
	}
	if identResolvesTo(info, b.X, v) && isNilIdent(info, b.Y) {
		return true
	}
	return identResolvesTo(info, b.Y, v) && isNilIdent(info, b.X)
}

func isCompared(cur inspector.Cursor) bool {
	bin, ok := parentAboveParens(cur).Node().(*ast.BinaryExpr)
	if !ok {
		return false
	}
	return bin.Op == token.EQL || bin.Op == token.NEQ
}

func sharesCompositeWithContext(pass *analysis.Pass, cur inspector.Cursor, cache *rootCache) bool {
	var lit *ast.CompositeLit
	for c := range cur.Enclosing((*ast.CompositeLit)(nil)) {
		if l, ok := c.Node().(*ast.CompositeLit); ok {
			lit = l
			break
		}
	}
	if lit == nil {
		return false
	}
	key := compositeKey{lit: lit, owner: ownerOf(cur).Node()}
	if got, ok := cache.shared[key]; ok {
		return got
	}
	found := false
	ast.Inspect(lit, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if v := varOf(pass.TypesInfo, id); v != nil && isContextParam(pass, cur, v) {
			found = true
			return false
		}
		return true
	})
	cache.shared[key] = found
	return found
}
