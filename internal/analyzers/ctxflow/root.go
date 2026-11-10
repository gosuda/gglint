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
	Doc:      "report context.Background and context.TODO where a caller context is in scope",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runRoot,
}

func runRoot(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.CallExpr)(nil)) {
		checkRoot(pass, cur)
	}
	return nil, nil
}

func checkRoot(pass *analysis.Pass, cur inspector.Cursor) {
	call, ok := cur.Node().(*ast.CallExpr)
	if !ok || !isRootCall(pass.TypesInfo, call) {
		return
	}
	if !resultUsed(cur) || !contextInScope(pass, cur) || inMainOrInit(cur) || nilGuarded(pass, cur, call) {
		return
	}
	if isCompared(cur) || sharesCompositeWithContext(pass, cur) {
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
	switch n := cur.Parent().Node().(type) {
	case *ast.ExprStmt, *ast.GoStmt, *ast.DeferStmt:
		return false
	case *ast.AssignStmt:
		return !allBlank(n.Lhs)
	case *ast.ValueSpec:
		return !allBlankIdents(n.Names)
	}
	return true
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
	v := assignedContextParam(pass, cur, as)
	if v == nil {
		return false
	}
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
	for _, rhs := range as.Rhs {
		if stripParens(rhs) == call {
			return true
		}
	}
	return false
}

func assignedContextParam(pass *analysis.Pass, cur inspector.Cursor, as *ast.AssignStmt) *types.Var {
	for _, lhs := range as.Lhs {
		id, ok := stripParens(lhs).(*ast.Ident)
		if !ok {
			continue
		}
		if v := varOf(pass.TypesInfo, id); v != nil && isContextParam(pass, cur, v) {
			return v
		}
	}
	return nil
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
	bin, ok := cur.Parent().Node().(*ast.BinaryExpr)
	if !ok {
		return false
	}
	return bin.Op == token.EQL || bin.Op == token.NEQ
}

func sharesCompositeWithContext(pass *analysis.Pass, cur inspector.Cursor) bool {
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
	return found
}
