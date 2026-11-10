package alias

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func inPlaceSilent(pass *analysis.Pass, fn ast.Node, b *types.Var, def sliceDef) bool {
	info := pass.TypesInfo
	if !def.highKnown || def.high != 0 || !def.zeroLow {
		return false
	}
	appends := appendsTo(info, fn, b)
	if len(appends) == 0 {
		return false
	}
	for _, st := range appends {
		if !singleInReadingLoop(info, fn, st, def.a) {
			return false
		}
	}
	if !atMostOnePerIteration(pass, fn, appends) {
		return false
	}
	return !hasElementReadAfterLoop(pass, fn, def.a, appends)
}

func hasElementReadAfterLoop(pass *analysis.Pass, fn ast.Node, a *types.Var, appends []*ast.AssignStmt) bool {
	for _, st := range appends {
		loop := enclosingLoop(fn, st)
		if loop == nil {
			return true
		}
		if overwrittenReadAfter(pass, fn, a, readSite{from: st, after: loop.End(), bound: 0, end: 1 << 30}) {
			return true
		}
	}
	return false
}
