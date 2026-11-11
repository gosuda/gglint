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

const cancelMessage = "the deferred cancel runs before the caller uses the returned context, so the context is already cancelled: return the cancel function instead"

var cancelAnalyzer = &analysis.Analyzer{
	Name:     "cancelreturned",
	Doc:      "report defer cancel() when the function returns the context that cancel controls, unless it also returns cancel or reassigns that context",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runCancel,
}

func runCancel(pass *analysis.Pass) (any, error) {
	for fn := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		checkCancel(pass, fn)
	}
	return nil, nil
}

type cancelPair struct {
	ctx    *types.Var
	cancel *types.Var
}

func checkCancel(pass *analysis.Pass, fn inspector.Cursor) {
	s := summarizeCancel(pass, fn)
	for _, p := range collectPairs(pass, fn) {
		checkPair(pass, p, s)
	}
}

type cancelSummary struct {
	defers     map[*types.Var]*ast.DeferStmt
	returned   map[*types.Var]bool
	reassigned map[*types.Var]bool
}

func summarizeCancel(pass *analysis.Pass, fn inspector.Cursor) cancelSummary {
	s := cancelSummary{
		defers:     map[*types.Var]*ast.DeferStmt{},
		returned:   map[*types.Var]bool{},
		reassigned: map[*types.Var]bool{},
	}
	for cur := range fn.Preorder((*ast.DeferStmt)(nil), (*ast.ReturnStmt)(nil), (*ast.AssignStmt)(nil)) {
		if _, isAssign := cur.Node().(*ast.AssignStmt); !isAssign {
			if ownerOf(cur) != fn {
				continue
			}
		}
		switch n := cur.Node().(type) {
		case *ast.DeferStmt:
			s.recordDefer(pass, n)
		case *ast.ReturnStmt:
			s.recordReturn(pass, fn, n)
		case *ast.AssignStmt:
			s.recordAssign(pass, n)
		}
	}
	return s
}

func (s cancelSummary) recordDefer(pass *analysis.Pass, d *ast.DeferStmt) {
	v, ok := deferredVar(pass, d)
	if !ok {
		return
	}
	if _, ok := s.defers[v]; ok {
		return
	}
	s.defers[v] = d
}

func (s cancelSummary) recordReturn(pass *analysis.Pass, fn inspector.Cursor, r *ast.ReturnStmt) {
	if len(r.Results) == 0 {
		ft := funcTypeOf(fn.Node())
		if ft == nil || ft.Results == nil {
			return
		}
		for _, field := range ft.Results.List {
			for _, name := range field.Names {
				if v := varOf(pass.TypesInfo, name); v != nil {
					s.returned[v] = true
				}
			}
		}
		return
	}
	for _, res := range r.Results {
		collectReturnedVars(pass.TypesInfo, res, s.returned)
	}
}

func (s cancelSummary) recordAssign(pass *analysis.Pass, as *ast.AssignStmt) {
	if len(as.Lhs) == 2 && len(as.Rhs) == 1 {
		if call, ok := stripParens(as.Rhs[0]).(*ast.CallExpr); ok && isWithCall(pass.TypesInfo, call) {
			return
		}
	}
	for _, lhs := range as.Lhs {
		id, ok := stripParens(lhs).(*ast.Ident)
		if !ok {
			continue
		}
		if _, isDef := pass.TypesInfo.Defs[id]; isDef {
			continue
		}
		if v := varOf(pass.TypesInfo, id); v != nil {
			s.reassigned[v] = true
		}
	}
}

func checkPair(pass *analysis.Pass, p cancelPair, s cancelSummary) {
	site, ok := s.defers[p.cancel]
	if !ok {
		return
	}
	if s.returned[p.cancel] || !s.returned[p.ctx] {
		return
	}
	if s.reassigned[p.ctx] {
		return
	}
	pass.Report(analysis.Diagnostic{Pos: site.Pos(), End: site.End(), Message: cancelMessage})
}

func deferredVar(pass *analysis.Pass, d *ast.DeferStmt) (*types.Var, bool) {
	if d.Call == nil || d.Call.Ellipsis.IsValid() {
		return nil, false
	}
	id, ok := stripParens(d.Call.Fun).(*ast.Ident)
	if !ok {
		return nil, false
	}
	v := varOf(pass.TypesInfo, id)
	return v, v != nil
}

func collectReturnedVars(info *types.Info, res ast.Expr, into map[*types.Var]bool) {
	e := stripParens(res)
	if id, ok := e.(*ast.Ident); ok {
		if v := varOf(info, id); v != nil {
			into[v] = true
		}
		return
	}
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		collectReturnedVars(info, u.X, into)
		return
	}
	if lit, ok := e.(*ast.CompositeLit); ok {
		collectCompositeVars(info, lit, into)
		return
	}
	if call, ok := e.(*ast.CallExpr); ok {
		collectDerivedVars(info, call, into)
	}
}

func collectCompositeVars(info *types.Info, lit *ast.CompositeLit, into map[*types.Var]bool) {
	for _, elt := range lit.Elts {
		kv, ok := stripParens(elt).(*ast.KeyValueExpr)
		if ok {
			collectReturnedVars(info, kv.Value, into)
			continue
		}
		collectReturnedVars(info, elt, into)
	}
}

func collectDerivedVars(info *types.Info, call *ast.CallExpr, into map[*types.Var]bool) {
	if !isDerivedContextCall(info, call) || len(call.Args) == 0 {
		return
	}
	collectReturnedVars(info, call.Args[0], into)
}

func collectPairs(pass *analysis.Pass, fn inspector.Cursor) []cancelPair {
	var out []cancelPair
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.DeclStmt)(nil)) {
		if ownerOf(cur) != fn {
			continue
		}
		switch n := cur.Node().(type) {
		case *ast.AssignStmt:
			if p, ok := pairFromAssign(pass, n); ok {
				out = append(out, p)
			}
		case *ast.DeclStmt:
			out = append(out, pairsFromDecl(pass, n)...)
		}
	}
	return out
}

func pairFromAssign(pass *analysis.Pass, as *ast.AssignStmt) (cancelPair, bool) {
	if len(as.Lhs) != 2 || len(as.Rhs) != 1 {
		return cancelPair{}, false
	}
	call, ok := stripParens(as.Rhs[0]).(*ast.CallExpr)
	if !ok || !isWithCall(pass.TypesInfo, call) {
		return cancelPair{}, false
	}
	return pairFromIdents(pass, as.Lhs[0], as.Lhs[1])
}

func pairFromIdents(pass *analysis.Pass, x, y ast.Expr) (cancelPair, bool) {
	xid, ok := stripParens(x).(*ast.Ident)
	if !ok {
		return cancelPair{}, false
	}
	yid, ok := stripParens(y).(*ast.Ident)
	if !ok {
		return cancelPair{}, false
	}
	ctx := varOf(pass.TypesInfo, xid)
	cancel := varOf(pass.TypesInfo, yid)
	if ctx == nil || cancel == nil {
		return cancelPair{}, false
	}
	return cancelPair{ctx: ctx, cancel: cancel}, true
}

func pairsFromDecl(pass *analysis.Pass, d *ast.DeclStmt) []cancelPair {
	gd, ok := d.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return nil
	}
	var out []cancelPair
	for _, spec := range gd.Specs {
		if p, ok := pairFromValueSpec(pass, spec); ok {
			out = append(out, p)
		}
	}
	return out
}

func pairFromValueSpec(pass *analysis.Pass, spec ast.Spec) (cancelPair, bool) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Names) != 2 || len(vs.Values) != 1 {
		return cancelPair{}, false
	}
	call, ok := stripParens(vs.Values[0]).(*ast.CallExpr)
	if !ok || !isWithCall(pass.TypesInfo, call) {
		return cancelPair{}, false
	}
	ctx := varOf(pass.TypesInfo, vs.Names[0])
	cancel := varOf(pass.TypesInfo, vs.Names[1])
	if ctx == nil || cancel == nil {
		return cancelPair{}, false
	}
	return cancelPair{ctx: ctx, cancel: cancel}, true
}

func isWithCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil {
		return false
	}
	if pkg := fn.Pkg(); pkg == nil || pkg.Path() != contextPath {
		return false
	}
	switch fn.Name() {
	case "WithCancel", "WithTimeout", "WithDeadline", "WithCancelCause", "WithTimeoutCause", "WithDeadlineCause":
		return true
	}
	return false
}

func isDerivedContextCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil {
		return false
	}
	if pkg := fn.Pkg(); pkg == nil || pkg.Path() != contextPath {
		return false
	}
	switch fn.Name() {
	case "WithValue", "WithCancel", "WithTimeout", "WithDeadline", "WithCancelCause", "WithTimeoutCause", "WithDeadlineCause":
		return true
	}
	return false
}
