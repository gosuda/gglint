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

// constLeftViolations reports fixed-array accesses with an unsigned index
// that upstream G602 never flags: a guard comparing a constant on the left
// (for example `if tokenCount < t`) whose fall-through path admits the
// boundary value len. Upstream's bound extractor only relates constants on
// the right, so these accesses never become candidates. This mirrors the
// const-on-left shape of LoGOs correctness::array_index_out_of_bounds
// (LoGOs 7d15ec76); every other shape in that rule's Go fixture is already
// covered by upstream candidates plus arrayIndexGuarded.
//
// The check stays silent wherever the LoGOs proof stays silent: a guard that
// proves the access safe in either operand order (arrayIndexGuarded), a
// boundary value the index type cannot take (unrepresentable length), an
// access inside or after a loop (the LoGOs proof stops at loops), an index
// that is not a bare function parameter, a use captured by a closure, and
// an array that is not a local fixed array.
func constLeftViolations(fset *token.FileSet, result *buildssa.SSA, sizes types.Sizes, issues []*issue.Issue) []*issue.Issue {
	candidates := make(map[issueLocation]bool)
	for _, iss := range issues {
		if iss.RuleID == "G602" && iss.What == "slice index out of range" {
			candidates[issueLocation{iss.File, iss.Line, iss.Col}] = true
		}
	}
	var added []*issue.Issue
	for _, fn := range result.SrcFuncs {
		added = append(added, constLeftFuncViolations(fset, sizes, fn, candidates)...)
	}
	return added
}

// constLeftFuncViolations reports the const-on-left accesses in one function.
func constLeftFuncViolations(fset *token.FileSet, sizes types.Sizes, fn *ssa.Function, candidates map[issueLocation]bool) []*issue.Issue {
	headers := loopHeaders(fn)
	var added []*issue.Issue
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			index, ok := instr.(*ssa.IndexAddr)
			if !ok {
				continue
			}
			if found := constLeftIndexViolation(fset, sizes, fn, headers, candidates, index); found != nil {
				added = append(added, found)
			}
		}
	}
	return added
}

// constLeftIndexViolation decides one indexed access: recognize the local
// fixed-array target, match it against upstream candidates and proven-safe
// guards, then derive whether the const-on-left guard admits the boundary.
func constLeftIndexViolation(fset *token.FileSet, sizes types.Sizes, fn *ssa.Function, headers map[*ssa.BasicBlock]bool, candidates map[issueLocation]bool, index *ssa.IndexAddr) *issue.Issue {
	length, param, ok := constLeftTarget(sizes, index)
	if !ok {
		return nil
	}
	file := fset.File(index.Pos())
	if file == nil {
		return nil
	}
	loc := issueLocation{file.Name(), strconv.Itoa(file.Line(index.Pos())), strconv.Itoa(fset.Position(index.Pos()).Column)}
	if candidates[loc] {
		return nil // upstream already reports it
	}
	if arrayIndexGuarded(index) {
		return nil // a guard proves it safe
	}
	if inLoop(headers, index.Block()) {
		return nil // the LoGOs proof stops at loops
	}
	if captured(param, fn) {
		return nil // a closure use removes it from the LoGOs rule
	}
	if !constLeftAdmitsBoundary(index, param, length) {
		return nil
	}
	return &issue.Issue{
		RuleID: "G602",
		What:   "slice index out of range",
		File:   loc.file,
		Line:   loc.line,
		Col:    loc.col,
	}
}

// constLeftTarget recognizes an index into a local fixed array through a bare
// unsigned function parameter whose type can represent length, mirroring the
// witness representability check of the LoGOs rule.
func constLeftTarget(sizes types.Sizes, index *ssa.IndexAddr) (int64, *ssa.Parameter, bool) {
	param, ok := index.Index.(*ssa.Parameter)
	if !ok {
		return 0, nil, false
	}
	basic, ok := index.Index.Type().Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsUnsigned == 0 {
		return 0, nil, false
	}
	alloc, ok := index.X.(*ssa.Alloc)
	if !ok {
		return 0, nil, false
	}
	addr, ok := alloc.Type().Underlying().(*types.Pointer)
	if !ok {
		return 0, nil, false
	}
	array, ok := addr.Elem().Underlying().(*types.Array)
	if !ok {
		return 0, nil, false
	}
	length := array.Len()
	if int64(constant.BitLen(constant.MakeInt64(length))) > int64(sizes.Sizeof(basic)*8) {
		return 0, nil, false
	}
	return length, param, true
}

// loopHeaders returns the set of loop headers: blocks with a predecessor they
// dominate (a back edge).
func loopHeaders(fn *ssa.Function) map[*ssa.BasicBlock]bool {
	headers := make(map[*ssa.BasicBlock]bool)
	for _, block := range fn.Blocks {
		for _, pred := range block.Preds {
			if block.Dominates(pred) {
				headers[block] = true
			}
		}
	}
	return headers
}

// inLoop reports whether block sits inside a loop region, approximated as
// dominated by a loop header. Code after a loop is also dominated by its
// header, which matches the LoGOs proof stopping at any earlier loop.
func inLoop(headers map[*ssa.BasicBlock]bool, block *ssa.BasicBlock) bool {
	for header := range headers {
		if header.Dominates(block) {
			return true
		}
	}
	return false
}

// captured reports whether param is used by another function (a closure
// literal), which removes it from the LoGOs rule's tracked set.
func captured(param *ssa.Parameter, fn *ssa.Function) bool {
	refs := param.Referrers()
	if refs == nil {
		return false
	}
	for _, ref := range *refs {
		if ref.Parent() != fn {
			return true
		}
	}
	return false
}

// constLeftAdmitsBoundary reports whether a guard comparing a constant
// against the index admits the boundary value length on the path to the
// access. It covers the operand order upstream misses (constant on the left)
// with the same successor-dominance condition branchBoundsIndex uses.
func constLeftAdmitsBoundary(index *ssa.IndexAddr, param ssa.Value, length int64) bool {
	for block := index.Block().Idom(); block != nil; block = block.Idom() {
		branch, ok := block.Instrs[len(block.Instrs)-1].(*ssa.If)
		if !ok {
			continue
		}
		comparison, ok := branch.Cond.(*ssa.BinOp)
		if !ok {
			continue
		}
		switch comparison.Op {
		case token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ:
		default:
			continue
		}
		for successor, next := range block.Succs {
			if len(next.Preds) != 1 || !next.Dominates(index.Block()) {
				continue
			}
			if admitsOnPath(comparison, param, length, successor == 0) {
				return true
			}
		}
	}
	return false
}

// admitsOnPath evaluates the comparison with the index fixed to the boundary
// value and reports whether the outcome matches the path taken toward the
// access.
func admitsOnPath(comparison *ssa.BinOp, param ssa.Value, length int64, truth bool) bool {
	boundary := constant.MakeInt64(length)
	if comparison.X == param {
		c, ok := comparison.Y.(*ssa.Const)
		if !ok {
			return false
		}
		return constant.Compare(boundary, comparison.Op, c.Value) == truth
	}
	if comparison.Y == param {
		c, ok := comparison.X.(*ssa.Const)
		if !ok {
			return false
		}
		return constant.Compare(c.Value, comparison.Op, boundary) == truth
	}
	return false
}
