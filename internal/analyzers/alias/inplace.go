package alias

import (
	"go/ast"
	"go/types"
)

func inPlaceSilent(scope *appendScope, b *types.Var, def sliceDef) bool {
	info := scope.info
	fn := scope.fn
	if !def.highKnown || def.high != 0 || !def.zeroLow {
		return false
	}
	appends := appendsTo(info, fn, b)
	if len(appends) == 0 {
		return false
	}
	for _, st := range appends {
		if !singleInReadingLoop(scope, st, def.a) {
			return false
		}
	}
	if !atMostOnePerIteration(scope.pass, fn, appends) {
		return false
	}
	return !hasElementReadAfterLoop(scope, def.a, appends)
}

func hasElementReadAfterLoop(scope *appendScope, a *types.Var, appends []*ast.AssignStmt) bool {
	for _, st := range appends {
		loop := enclosingLoopCached(scope, st)
		if loop == nil {
			return true
		}
		if overwrittenReadAfter(scope, a, readSite{from: st, after: loop.End(), bound: 0, end: 1 << 30}) {
			return true
		}
	}
	return false
}
