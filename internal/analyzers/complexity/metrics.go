package complexity

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
)

type metrics struct {
	depth, params, lines, score, cyclomatic, boolParams int
}

func funcMetrics(fset *token.FileSet, info *types.Info, fn *ast.FuncDecl) metrics {
	m := bodyMetrics(fset, fn.Pos(), fn.Type.Params, fn.Body)
	if fn.Recv == nil && fn.Name.IsExported() {
		m.boolParams = countBoolParams(info, fn.Type.Params)
	}
	return m
}

func bodyMetrics(fset *token.FileSet, start token.Pos, params *ast.FieldList, body *ast.BlockStmt) metrics {
	w := &walker{chained: make(map[*ast.IfStmt]bool), inChain: make(map[*ast.BinaryExpr]bool)}
	ast.Inspect(body, w.visit)
	return metrics{
		depth:      w.depth,
		params:     countParams(params),
		lines:      fset.Position(body.Rbrace).Line - fset.Position(start).Line + 1,
		score:      w.score,
		cyclomatic: 1 + w.cyclomatic,
	}
}

func countParams(fields *ast.FieldList) int {
	n := 0
	for _, f := range fields.List {
		n += paramNames(f)
	}
	return n
}

func countBoolParams(info *types.Info, fields *ast.FieldList) int {
	n := 0
	for _, f := range fields.List {
		if t := info.TypeOf(f.Type); t != nil && types.Identical(t, types.Typ[types.Bool]) {
			n += paramNames(f)
		}
	}
	return n
}

func paramNames(f *ast.Field) int {
	return max(1, len(f.Names))
}

type walker struct {
	frames     []bool
	level      int
	depth      int
	score      int
	cyclomatic int
	chained    map[*ast.IfStmt]bool
	inChain    map[*ast.BinaryExpr]bool
}

func (w *walker) visit(n ast.Node) bool {
	if n == nil {
		w.leave()
		return false
	}
	if _, ok := n.(*ast.FuncLit); ok {
		return false
	}
	w.enter(w.weigh(n))
	return true
}

func (w *walker) enter(opens bool) {
	w.frames = append(w.frames, opens)
	if opens {
		w.level++
		w.depth = max(w.depth, w.level)
	}
}

func (w *walker) leave() {
	last := len(w.frames) - 1
	if w.frames[last] {
		w.level--
	}
	w.frames = w.frames[:last]
}

func (w *walker) weigh(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.IfStmt:
		return w.branch(n)
	case *ast.ForStmt, *ast.RangeStmt:
		w.cyclomatic++
		return w.structure()
	case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return w.structure()
	case *ast.CaseClause, *ast.CommClause:
		if conditioned(n) {
			w.cyclomatic++
			w.score += w.level
		}
	case *ast.BranchStmt:
		w.jump(n)
	case *ast.BinaryExpr:
		w.chain(n)
	}
	return false
}

func (w *walker) structure() bool {
	w.score += 1 + w.level
	return true
}

func (w *walker) branch(n *ast.IfStmt) bool {
	w.cyclomatic++
	if next, ok := n.Else.(*ast.IfStmt); ok {
		w.chained[next] = true
	}
	if w.chained[n] {
		w.score += w.level
		return false
	}
	return w.structure()
}

func conditioned(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CaseClause:
		return n.List != nil
	case *ast.CommClause:
		return n.Comm != nil
	}
	return false
}

func (w *walker) jump(n *ast.BranchStmt) {
	labeled := n.Label != nil && (n.Tok == token.BREAK || n.Tok == token.CONTINUE)
	if n.Tok == token.GOTO || labeled {
		w.score += 1 + w.level
	}
}

func (w *walker) chain(n *ast.BinaryExpr) {
	if !logical(n) {
		return
	}
	w.cyclomatic++
	if !w.inChain[n] {
		w.score += len(slices.Compact(w.chainOps(n))) - 1
	}
}

func logical(n *ast.BinaryExpr) bool {
	return n.Op == token.LAND || n.Op == token.LOR
}

func (w *walker) chainOps(e ast.Expr) []token.Token {
	b, ok := e.(*ast.BinaryExpr)
	if !ok || !logical(b) {
		return nil
	}
	w.inChain[b] = true
	ops := append(w.chainOps(b.X), b.Op)
	return append(ops, w.chainOps(b.Y)...)
}
