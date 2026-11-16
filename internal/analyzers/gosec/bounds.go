package gosec

import (
	"go/constant"
	"go/token"
	"go/types"
	"strconv"

	"github.com/securego/gosec/v2/issue"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

type issueLocation struct {
	file string
	line string
	col  string
}

func guardedArrayIndices(fset *token.FileSet, result *buildssa.SSA, issues []*issue.Issue) map[issueLocation]bool {
	candidates := make(map[issueLocation]bool)
	for _, iss := range issues {
		if iss.RuleID == "G602" && iss.What == "slice index out of range" {
			candidates[issueLocation{iss.File, iss.Line, iss.Col}] = true
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	proven := make(map[issueLocation]bool)
	for _, fn := range result.SrcFuncs {
		for _, block := range fn.Blocks {
			recordArrayGuards(fset, block, candidates, proven)
		}
	}
	return proven
}

func recordArrayGuards(fset *token.FileSet, block *ssa.BasicBlock, candidates, proven map[issueLocation]bool) {
	for _, instr := range block.Instrs {
		index, ok := instr.(*ssa.IndexAddr)
		if !ok {
			continue
		}
		file := fset.File(index.Pos())
		if file == nil {
			continue
		}
		// Match gosec's physical filename and adjusted //line coordinates.
		loc := issueLocation{file.Name(), strconv.Itoa(file.Line(index.Pos())), strconv.Itoa(file.Position(index.Pos()).Column)}
		if !candidates[loc] {
			continue
		}
		if safe, seen := proven[loc]; seen && !safe {
			continue
		}
		proven[loc] = arrayIndexGuarded(index)
	}
}

func arrayIndexGuarded(index *ssa.IndexAddr) bool {
	basic, ok := index.Index.Type().Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsUnsigned == 0 {
		return false
	}
	arrayType := index.X.Type().Underlying()
	if pointer, isPointer := arrayType.(*types.Pointer); isPointer {
		arrayType = pointer.Elem().Underlying()
	}
	array, ok := arrayType.(*types.Array)
	if !ok {
		return false
	}
	for block := index.Block().Idom(); block != nil; block = block.Idom() {
		if branchBoundsIndex(block, index, array.Len()) {
			return true
		}
	}
	return false
}

func branchBoundsIndex(block *ssa.BasicBlock, index *ssa.IndexAddr, length int64) bool {
	branch, ok := block.Instrs[len(block.Instrs)-1].(*ssa.If)
	if !ok {
		return false
	}
	comparison, ok := branch.Cond.(*ssa.BinOp)
	if !ok {
		return false
	}
	for successor, next := range block.Succs {
		if len(next.Preds) != 1 || !next.Dominates(index.Block()) {
			continue
		}
		if comparisonBoundsIndex(comparison, index.Index, length, successor == 0) {
			return true
		}
	}
	return false
}

func comparisonBoundsIndex(comparison *ssa.BinOp, index ssa.Value, length int64, truth bool) bool {
	onLeft := comparison.X == index
	value := comparison.Y
	if !onLeft {
		if comparison.Y != index {
			return false
		}
		value = comparison.X
	}
	limit, ok := value.(*ssa.Const)
	if !ok || !upperBoundComparison(comparison.Op, onLeft, truth) {
		return false
	}
	relation := token.LEQ
	if (comparison.Op == token.LEQ || comparison.Op == token.GEQ) == truth {
		relation = token.LSS
	}
	return constant.Compare(limit.Value, relation, constant.MakeInt64(length))
}

func upperBoundComparison(op token.Token, onLeft, truth bool) bool {
	if op == token.LSS || op == token.LEQ {
		return onLeft == truth
	}
	if op == token.GTR || op == token.GEQ {
		return onLeft != truth
	}
	return false
}
