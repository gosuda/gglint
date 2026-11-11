package chanstate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/flow"
)

func chanLocalsWith(ctx *fnCtx) []flow.Key {
	body := flow.Body(ctx.fn)
	if body == nil {
		return nil
	}
	var out []flow.Key
	seen := map[flow.Key]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := ctx.info.Defs[id].(*types.Var)
		if !ok || !isChan(v.Type()) || !flow.IsLocal(ctx.info, ctx.fn, v) {
			return true
		}
		k := flow.Key{Root: v}
		if seen[k] {
			return true
		}
		seen[k] = true
		out = append(out, k)
		return true
	})
	return keepLocalWith(out, ctx.info, ctx.fn, ctx.spawned)
}

func keepLocalWith(out []flow.Key, info *types.Info, fn ast.Node, spawned map[*ast.FuncLit]bool) []flow.Key {
	var kept []flow.Key
	for _, k := range out {
		if handsOnWith(info, fn, k, spawned) {
			continue
		}
		kept = append(kept, k)
	}
	return kept
}

func nilLocalsWith(ctx *fnCtx) []flow.Key {
	var out []flow.Key
	for _, k := range chanLocalsWith(ctx) {
		if nilVar(ctx.info, ctx.fn, k) {
			out = append(out, k)
		}
	}
	return out
}

func nilVar(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	seen := &nilSeen{}
	ast.Inspect(body, func(n ast.Node) bool {
		if seen.assigned {
			return false
		}
		seen.visit(info, n, k)
		return !seen.assigned
	})
	return seen.decl && !seen.assigned
}

type nilSeen struct {
	decl     bool
	assigned bool
}

func (s *nilSeen) visit(info *types.Info, n ast.Node, k flow.Key) {
	switch n := n.(type) {
	case *ast.ValueSpec:
		s.decl = s.decl || specDeclares(info, n, k)
	case *ast.AssignStmt:
		s.assignStmt(info, n, k)
	case *ast.IncDecStmt:
		s.assigned = s.assigned || targets(info, []ast.Expr{n.X}, k)
	case *ast.RangeStmt:
		s.assigned = s.assigned || rangeTargets(info, n, k)
	}
}

func (s *nilSeen) assignStmt(info *types.Info, n *ast.AssignStmt, k flow.Key) {
	s.assigned = s.assigned || targets(info, n.Lhs, k)
}

func rangeTargets(info *types.Info, r *ast.RangeStmt, k flow.Key) bool {
	if r.Tok != token.ASSIGN && r.Tok != token.DEFINE {
		return false
	}
	if r.Key != nil && targets(info, []ast.Expr{r.Key}, k) {
		return true
	}
	return r.Value != nil && targets(info, []ast.Expr{r.Value}, k)
}

func specDeclares(info *types.Info, vs *ast.ValueSpec, k flow.Key) bool {
	if len(vs.Values) != 0 {
		return false
	}
	return targets(info, identExprs(vs.Names), k)
}

func identExprs(ids []*ast.Ident) []ast.Expr {
	out := make([]ast.Expr, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func targets(info *types.Info, lhs []ast.Expr, k flow.Key) bool {
	for _, l := range lhs {
		if kk, ok := keyOfIdent(info, l); ok && kk == k {
			return true
		}
	}
	return false
}

func makeCall(info *types.Info, e ast.Expr) (*ast.CallExpr, bool) {
	if !isMake(info, e) {
		return nil, false
	}
	c, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	return c, true
}

func madeCapFacts(info *types.Info, facts *bodyFacts, k flow.Key) (int64, bool) {
	if facts.isReassigned(k) {
		return 0, false
	}
	call, ok := facts.makeCalls(k)
	if !ok {
		return 0, false
	}
	return constCap(info, call)
}

func makeInLoopFacts(facts *bodyFacts, loop ast.Stmt, k flow.Key) bool {
	call, ok := facts.makeCalls(k)
	if !ok {
		return false
	}
	return withinNode(loop, call)
}

func unbufferedFacts(info *types.Info, facts *bodyFacts, k flow.Key) bool {
	c, ok := madeCapFacts(info, facts, k)
	return ok && c == 0
}

func localKey(info *types.Info, fn ast.Node, k flow.Key) bool {
	v, ok := k.Root.(*types.Var)
	return ok && flow.IsLocal(info, fn, v)
}

func loopBody(st ast.Stmt) []ast.Stmt {
	switch st := st.(type) {
	case *ast.ForStmt:
		return st.Body.List
	case *ast.RangeStmt:
		return st.Body.List
	}
	return nil
}

func chanRanges(info *types.Info, fn ast.Node) []*ast.RangeStmt {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	var out []*ast.RangeStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if r, ok := n.(*ast.RangeStmt); ok && isChan(info.TypeOf(r.X)) {
			out = append(out, r)
		}
		return true
	})
	return out
}

func literalMentions(info *types.Info, fn ast.Node, k flow.Key) bool {
	body := flow.Body(fn)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			found = mentionsKey(info, lit, k)
			return false
		}
		return true
	})
	return found
}

func literalTouch(info *types.Info, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := x.(*ast.FuncLit); ok {
			found = mentionsKey(info, lit, k)
			return false
		}
		return true
	})
	return found
}

func mentionsKey(info *types.Info, n ast.Node, k flow.Key) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if found {
			return false
		}
		if id, ok := x.(*ast.Ident); ok {
			if kk, ok := keyOfIdent(info, id); ok && kk == k {
				found = true
			}
		}
		return !found
	})
	return found
}

func chanOps(info *types.Info, fn ast.Node, k flow.Key, want opKind) []chanOp {
	body := flow.Body(fn)
	if body == nil {
		return nil
	}
	w := &opWalker{info: info, key: k}
	w.stmts(body.List)
	var out []chanOp
	for _, op := range w.out {
		if op.Kind&want != 0 && !op.InSelect {
			out = append(out, op)
		}
	}
	slices.SortStableFunc(out, func(a, b chanOp) int { return cmp.Compare(a.Pos, b.Pos) })
	return out
}

func deadPair(g *flow.Graph, ops []chanOp) (chanOp, bool) {
	for i := range ops {
		for j := i + 1; j < len(ops); j++ {
			if ops[i].Kind == ops[j].Kind {
				continue
			}
			if !g.Reaches(ops[i].Expr, ops[j].Expr, nil) {
				continue
			}
			return ops[i], true
		}
	}
	return chanOp{}, false
}

func spawnLits(info *types.Info, fn ast.Node) map[*ast.FuncLit]bool {
	body := flow.Body(fn)
	out := map[*ast.FuncLit]bool{}
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GoStmt:
			if lit, ok := ast.Unparen(n.Call.Fun).(*ast.FuncLit); ok {
				out[lit] = true
			}
		case *ast.CallExpr:
			markGroupSpawn(info, n, out)
		}
		return true
	})
	return out
}

func spawnDrainsWith(info *types.Info, spawned map[*ast.FuncLit]bool, k flow.Key) bool {
	for lit := range spawned {
		if litDrains(info, lit, k) {
			return true
		}
	}
	return false
}

func litDrains(info *types.Info, lit *ast.FuncLit, k flow.Key) bool {
	drains := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if drains {
			return false
		}
		drains = drainsAt(info, n, k)
		return !drains
	})
	return drains
}

func drainsAt(info *types.Info, n ast.Node, k flow.Key) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		return n.Op == token.ARROW && keyMatches(info, n.X, k)
	case *ast.RangeStmt:
		return isChan(info.TypeOf(n.X)) && keyMatches(info, n.X, k)
	}
	return false
}

func keyMatches(info *types.Info, e ast.Expr, k flow.Key) bool {
	kk, ok := flow.KeyOf(info, e)
	return ok && kk == k
}

func markGroupSpawn(info *types.Info, call *ast.CallExpr, out map[*ast.FuncLit]bool) {
	callee := typeutil.StaticCallee(info, call)
	if callee == nil || len(call.Args) == 0 {
		return
	}
	switch callee.FullName() {
	case "(*sync.WaitGroup).Go", "(*golang.org/x/sync/errgroup.Group).Go", "(*golang.org/x/sync/errgroup.Group).TryGo":
		if lit, ok := ast.Unparen(call.Args[0]).(*ast.FuncLit); ok {
			out[lit] = true
		}
	}
}

func spawnExitsWith(ctx *fnCtx, k flow.Key) bool {
	for lit := range ctx.spawned {
		if !mentionsKey(ctx.info, lit, k) {
			continue
		}
		if litExits(ctx.info, lit) {
			return true
		}
	}
	return false
}

func litExits(info *types.Info, lit *ast.FuncLit) bool {
	exited := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if exited {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if s, ok := n.(*ast.ExprStmt); ok {
			if c, ok := s.X.(*ast.CallExpr); ok && isNeverReturn(info, c) {
				exited = true
			}
		}
		return !exited
	})
	return exited
}

func isNeverReturn(info *types.Info, c *ast.CallExpr) bool {
	if isPanicCall(info, c) {
		return true
	}
	fn := typeutil.StaticCallee(info, c)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() + "." + fn.Name() {
	case "os.Exit", "log.Fatal", "log.Fatalf", "log.Fatalln":
		return true
	}
	return false
}
