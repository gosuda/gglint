package chanstate

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/gosuda/gglint/internal/flow"
)

type bodyFacts struct {
	assignCount map[flow.Key]int
	copied      map[flow.Key]bool
	ranged      map[flow.Key]bool
	rangedAny   map[flow.Key]bool
	makes       map[flow.Key][]*ast.CallExpr
	closes      map[flow.Key]bool
}

func (f *bodyFacts) isReassigned(k flow.Key) bool {
	return f.assignCount[k] > 1
}

func (f *bodyFacts) isCopied(k flow.Key) bool {
	return f.copied[k]
}

func (f *bodyFacts) hasRanged(k flow.Key) bool {
	return f.ranged[k]
}

func (f *bodyFacts) hasAnyRange(k flow.Key) bool {
	return f.rangedAny[k]
}

func (f *bodyFacts) makeCalls(k flow.Key) (*ast.CallExpr, bool) {
	calls := f.makes[k]
	if len(calls) != 1 {
		return nil, false
	}
	return calls[0], true
}

func (f *bodyFacts) isClosed(k flow.Key) bool {
	return f.closes[k]
}

func collectBodyFacts(info *types.Info, fn ast.Node) *bodyFacts {
	f := &bodyFacts{
		assignCount: map[flow.Key]int{},
		copied:      map[flow.Key]bool{},
		ranged:      map[flow.Key]bool{},
		rangedAny:   map[flow.Key]bool{},
		makes:       map[flow.Key][]*ast.CallExpr{},
		closes:      map[flow.Key]bool{},
	}
	body := flow.Body(fn)
	if body == nil {
		return f
	}
	var assigns []*ast.AssignStmt
	var specs []*ast.ValueSpec
	var ranges []*ast.RangeStmt
	var lits []*ast.FuncLit
	var calls []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			assigns = append(assigns, n)
		case *ast.ValueSpec:
			specs = append(specs, n)
		case *ast.RangeStmt:
			ranges = append(ranges, n)
		case *ast.FuncLit:
			lits = append(lits, n)
		case *ast.CallExpr:
			calls = append(calls, n)
		}
		return true
	})
	fillAssignCounts(info, f, assigns, specs, ranges, lits)
	fillCopiedSets(info, f, assigns, specs)
	fillRangedSets(info, f, ranges, lits)
	fillMakesSets(info, f, assigns, specs, lits)
	fillClosesSets(info, f, calls)
	fillAnyRanges(info, f, ranges)
	return f
}

func insideAny(lits []*ast.FuncLit, n ast.Node) bool {
	for _, lit := range lits {
		if withinNode(lit, n) {
			return true
		}
	}
	return false
}

func fillAssignCounts(info *types.Info, f *bodyFacts, assigns []*ast.AssignStmt, specs []*ast.ValueSpec, ranges []*ast.RangeStmt, lits []*ast.FuncLit) {
	countAssignStmts(info, f, assigns, lits)
	countValueSpecs(info, f, specs, lits)
	countRangeTargets(info, f, ranges, lits)
}

func countAssignStmts(info *types.Info, f *bodyFacts, assigns []*ast.AssignStmt, lits []*ast.FuncLit) {
	for _, a := range assigns {
		if insideAny(lits, a) {
			continue
		}
		for k := range assignKeys(info, a.Lhs) {
			f.assignCount[k]++
		}
	}
}

func countValueSpecs(info *types.Info, f *bodyFacts, specs []*ast.ValueSpec, lits []*ast.FuncLit) {
	for _, vs := range specs {
		if len(vs.Values) == 0 || insideAny(lits, vs) {
			continue
		}
		for k := range assignKeys(info, identExprs(vs.Names)) {
			f.assignCount[k]++
		}
	}
}

func countRangeTargets(info *types.Info, f *bodyFacts, ranges []*ast.RangeStmt, lits []*ast.FuncLit) {
	for _, r := range ranges {
		if r.Tok != token.ASSIGN || insideAny(lits, r) {
			continue
		}
		for k := range rangeAssignKeys(info, r) {
			f.assignCount[k]++
		}
	}
}

func rangeAssignKeys(info *types.Info, r *ast.RangeStmt) map[flow.Key]bool {
	var lhs []ast.Expr
	if r.Key != nil {
		lhs = append(lhs, r.Key)
	}
	if r.Value != nil {
		lhs = append(lhs, r.Value)
	}
	return assignKeys(info, lhs)
}

func assignKeys(info *types.Info, lhs []ast.Expr) map[flow.Key]bool {
	seen := map[flow.Key]bool{}
	for _, l := range lhs {
		if kk, ok := keyOfIdent(info, l); ok {
			seen[kk] = true
		}
	}
	return seen
}

func fillCopiedSets(info *types.Info, f *bodyFacts, assigns []*ast.AssignStmt, specs []*ast.ValueSpec) {
	for _, a := range assigns {
		markCopied(info, f, a.Lhs, a.Rhs)
	}
	for _, vs := range specs {
		markCopied(info, f, identExprs(vs.Names), vs.Values)
	}
}

func markCopied(info *types.Info, f *bodyFacts, lhs, rhs []ast.Expr) {
	rhsKeys := map[flow.Key]bool{}
	for _, e := range rhs {
		if kk, ok := keyOfIdent(info, ast.Unparen(e)); ok {
			rhsKeys[kk] = true
		}
	}
	if len(rhsKeys) == 0 {
		return
	}
	for _, l := range lhs {
		kk, ok := keyOfIdent(info, l)
		if !ok {
			continue
		}
		markCopiedKey(f, kk, rhsKeys)
	}
}

func markCopiedKey(f *bodyFacts, kk flow.Key, rhsKeys map[flow.Key]bool) {
	for rk := range rhsKeys {
		if kk != rk {
			f.copied[rk] = true
		}
	}
}

func fillRangedSets(info *types.Info, f *bodyFacts, ranges []*ast.RangeStmt, lits []*ast.FuncLit) {
	for _, r := range ranges {
		if insideAny(lits, r) {
			continue
		}
		if !isChan(info.TypeOf(r.X)) {
			continue
		}
		if kk, ok := flow.KeyOf(info, r.X); ok {
			f.ranged[kk] = true
		}
	}
}

func fillMakesSets(info *types.Info, f *bodyFacts, assigns []*ast.AssignStmt, specs []*ast.ValueSpec, lits []*ast.FuncLit) {
	collectAssignMakes(info, f, assigns, lits)
	collectSpecMakes(info, f, specs, lits)
}

func collectAssignMakes(info *types.Info, f *bodyFacts, assigns []*ast.AssignStmt, lits []*ast.FuncLit) {
	for _, a := range assigns {
		if a.Tok != token.DEFINE || len(a.Rhs) != len(a.Lhs) || insideAny(lits, a) {
			continue
		}
		for i, l := range a.Lhs {
			kk, ok := keyOfIdent(info, l)
			if !ok {
				continue
			}
			if c, ok := makeCall(info, a.Rhs[i]); ok {
				f.makes[kk] = append(f.makes[kk], c)
			}
		}
	}
}

func collectSpecMakes(info *types.Info, f *bodyFacts, specs []*ast.ValueSpec, lits []*ast.FuncLit) {
	for _, vs := range specs {
		if len(vs.Values) != 1 || insideAny(lits, vs) {
			continue
		}
		c, ok := makeCall(info, vs.Values[0])
		if !ok {
			continue
		}
		for _, id := range vs.Names {
			if kk, ok := keyOfIdent(info, id); ok {
				f.makes[kk] = append(f.makes[kk], c)
			}
		}
	}
}

func fillClosesSets(info *types.Info, f *bodyFacts, calls []*ast.CallExpr) {
	for _, c := range calls {
		if kk, ok := closeKey(info, c); ok {
			f.closes[kk] = true
		}
	}
}

func fillAnyRanges(info *types.Info, f *bodyFacts, ranges []*ast.RangeStmt) {
	for _, r := range ranges {
		if kk, ok := flow.KeyOf(info, r.X); ok {
			f.rangedAny[kk] = true
		}
	}
}
