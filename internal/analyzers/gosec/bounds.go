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
		if iss.RuleID == boundRuleID && iss.What == boundWhat {
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
// that upstream G602 never flags: a guard comparing a constant against the
// index (for example `if tokenCount < t`) whose fall-through path admits the
// boundary value len. Upstream's bound extractor only relates constants on
// the right, so these accesses never become candidates. This mirrors the
// const-on-left shape of LoGOs correctness::array_index_out_of_bounds
// (LoGOs 7d15ec76); every other shape in that rule's Go fixture is already
// covered by upstream candidates plus arrayIndexGuarded. A report needs the
// boundary witness to reach the access along a fully resolved path that
// traverses at least one parameter-against-constant guard, in either operand
// order and including equality, mirroring the LoGOs proof's witness walk.
//
// The check stays silent wherever the LoGOs proof stays silent: a guard that
// proves the access safe in either operand order (arrayIndexGuarded), a
// boundary value the index type cannot take (unrepresentable length), an
// access inside or after a loop (the LoGOs proof stops at loops), an index
// that is not a bare function parameter, a use captured by a closure, and
// an array that is not a local fixed array.
func constLeftViolations(fset *token.FileSet, result *buildssa.SSA, sizes types.Sizes, issues []*issue.Issue) []constLeftFinding {
	candidates := make(map[issueLocation]bool)
	for _, iss := range issues {
		if iss.RuleID == boundRuleID && iss.What == boundWhat {
			candidates[issueLocation{iss.File, iss.Line, iss.Col}] = true
		}
	}
	var added []constLeftFinding
	for _, fn := range result.SrcFuncs {
		added = append(added, constLeftFuncViolations(fset, sizes, fn, candidates)...)
	}
	return added
}

// constLeftFinding is one supplemental G602 with the access position it is
// reported at. Adjusted line coordinates still match upstream candidates, but
// the diagnostic itself is reported at pos directly: rebuilding a position
// from adjusted line offsets misplaces findings under //line directives.
type constLeftFinding struct {
	issue *issue.Issue
	pos   token.Pos
}

// constLeftFuncViolations reports the const-on-left accesses in one function.
func constLeftFuncViolations(fset *token.FileSet, sizes types.Sizes, fn *ssa.Function, candidates map[issueLocation]bool) []constLeftFinding {
	headers := loopHeaders(fn)
	var added []constLeftFinding
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			index, ok := instr.(*ssa.IndexAddr)
			if !ok {
				continue
			}
			if found := constLeftIndexViolation(fset, sizes, fn, headers, candidates, index); found != nil {
				added = append(added, *found)
			}
		}
	}
	return added
}

// constLeftIndexViolation decides one indexed access: recognize the local
// fixed-array target, match it against upstream candidates and proven-safe
// guards, then derive whether the const-on-left guard admits the boundary.
func constLeftIndexViolation(fset *token.FileSet, sizes types.Sizes, fn *ssa.Function, headers map[*ssa.BasicBlock]bool, candidates map[issueLocation]bool, index *ssa.IndexAddr) *constLeftFinding {
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
	if !constLeftAdmitsBoundary(fn, index, param, length) {
		return nil
	}
	return &constLeftFinding{
		issue: &issue.Issue{
			RuleID: boundRuleID,
			What:   boundWhat,
			File:   loc.file,
			Line:   loc.line,
			Col:    loc.col,
		},
		pos: index.Pos(),
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
	if int64(constant.BitLen(constant.MakeInt64(length))) > sizes.Sizeof(basic)*8 {
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

// constLeftAdmitsBoundary reports whether the boundary witness reaches the
// access along the path the function takes under param = length, walking
// resolved edges from the entry block as the LoGOs proof walks statements
// with the parameter fixed to the witness (LoGOs arrayIndexProof). Every
// instruction on the walked path must be provable, matching the proof
// stopping at a call or any other statement it cannot evaluate. A
// parameter-against-constant condition resolves to its outcome at the
// boundary; a constant condition resolves statically; any other condition or
// terminator ends the walk, mirroring the proof stopping at the first
// statement it cannot evaluate. At least one parameter-against-constant
// guard must be traversed: with none, the access is not the const-comparison
// shape this pass adds, and the walk reports reached only when the access
// block is on the boundary path.
func constLeftAdmitsBoundary(fn *ssa.Function, index *ssa.IndexAddr, param ssa.Value, length int64) bool {
	matched := false
	visited := make(map[*ssa.BasicBlock]bool)
	block := fn.Blocks[0]
	for {
		if visited[block] {
			return false
		}
		visited[block] = true
		if !provablePath(block, index) {
			return false
		}
		if block == index.Block() {
			return matched
		}
		next, ok := boundarySuccessor(block, param, length, &matched)
		if !ok {
			return false
		}
		block = next
	}
}

// provablePath reports whether the witnessed path through block holds only
// instructions the LoGOs proof can evaluate, stopping at the target access
// since instructions after it cannot execute before it. The terminator is
// excluded: the walk resolves its edge separately.
func provablePath(block *ssa.BasicBlock, index *ssa.IndexAddr) bool {
	for i, instr := range block.Instrs {
		if instr == index {
			return true
		}
		if i == len(block.Instrs)-1 {
			continue
		}
		if !provableInstruction(instr) {
			return false
		}
	}
	return true
}

// provableInstruction reports whether instr is an evaluation artifact of a
// statement the LoGOs proof walks: scalar comparisons, array construction
// and stores into locals, SSA join artifacts, local loads, and a boolean
// negation. A call, conversion, field or other index access, map or channel
// operation, and every other instruction kind maps to a statement or
// expression the proof cannot evaluate, so it stops — and this walk stays
// silent — wherever one appears on the witnessed path.
func provableInstruction(instr ssa.Instruction) bool {
	switch instr := instr.(type) {
	case *ssa.BinOp:
		return boundsComparisonOp(instr.Op)
	case *ssa.UnOp:
		return provableUnOp(instr)
	case *ssa.IndexAddr:
		return literalElement(instr)
	case *ssa.Store:
		return provableStoreAddr(instr.Addr)
	case *ssa.Alloc:
		return !instr.Heap
	case *ssa.Phi, *ssa.Extract, *ssa.DebugRef, *ssa.RunDefers:
		return true
	}
	return false
}

// provableUnOp reports whether a unary operation is provable: a boolean
// negation the LoGOs operand rule evaluates, or a load of a local variable,
// which reads as a plain name at source level. A dereference of any other
// value stops the proof.
func provableUnOp(op *ssa.UnOp) bool {
	if op.Op == token.NOT {
		return true
	}
	_, local := op.X.(*ssa.Alloc)
	return op.Op == token.MUL && local
}

// literalElement reports whether addr indexes a constant position of the
// array literal under construction, the SSA shape of a provable composite
// literal element. Any other index expression — another array access — is
// not the target and stops the proof.
func literalElement(addr *ssa.IndexAddr) bool {
	alloc, isAlloc := addr.X.(*ssa.Alloc)
	_, isConst := addr.Index.(*ssa.Const)
	return isAlloc && isConst && alloc.Comment == "complit"
}

// provableStoreAddr reports whether a store's destination is a statement the
// LoGOs proof can evaluate: a plain local or global name, or an element of
// the array literal being built. A store through a pointer or into any other
// indexed or selected destination stops the proof.
func provableStoreAddr(addr ssa.Value) bool {
	switch addr := addr.(type) {
	case *ssa.Alloc, *ssa.Global:
		return true
	case *ssa.IndexAddr:
		return literalElement(addr)
	}
	return false
}

// boundarySuccessor resolves the edge the boundary witness takes out of
// block: the successor chosen by the condition's outcome at the boundary
// value. It reports ok=false when the walk ends here — a terminator that does
// not transfer control (return, panic) or a condition the proof cannot
// resolve (a non-comparison, an operator outside the LoGOs comparison set, or
// operands that are not the parameter against a constant).
func boundarySuccessor(block *ssa.BasicBlock, param ssa.Value, length int64, matched *bool) (*ssa.BasicBlock, bool) {
	last := block.Instrs[len(block.Instrs)-1]
	if branch, ok := last.(*ssa.If); ok {
		outcome, resolved := boundaryOutcome(branch.Cond, param, length, matched)
		if !resolved {
			return nil, false
		}
		if outcome {
			return block.Succs[0], true
		}
		return block.Succs[1], true
	}
	if _, ok := last.(*ssa.Jump); ok {
		return block.Succs[0], true
	}
	return nil, false
}

// boundaryOutcome evaluates an if condition with param fixed to the boundary
// value. Only a parameter-against-constant comparison — in either operand
// order, equality included — or a statically known condition resolves to a
// definite edge, as the LoGOs operand rule maps the parameter to the witness,
// a constant to its value, and anything else to unknown, which stops the
// proof (LoGOs compoundBoundary and equalityBoundary report; disjointGuard
// stays silent). Traversed comparisons mark the walk as guarded.
func boundaryOutcome(cond, param ssa.Value, length int64, matched *bool) (take, ok bool) {
	if c, isConst := cond.(*ssa.Const); isConst {
		return constant.BoolVal(c.Value), true
	}
	comparison, isBinOp := cond.(*ssa.BinOp)
	if !isBinOp || !boundsComparisonOp(comparison.Op) {
		return false, false
	}
	boundary := constant.MakeInt64(length)
	if comparison.X == param {
		limit, isConst := comparison.Y.(*ssa.Const)
		if !isConst {
			return false, false
		}
		*matched = true
		return constant.Compare(boundary, comparison.Op, limit.Value), true
	}
	if comparison.Y == param {
		limit, isConst := comparison.X.(*ssa.Const)
		if !isConst {
			return false, false
		}
		*matched = true
		return constant.Compare(limit.Value, comparison.Op, boundary), true
	}
	return false, false
}

// boundsComparisonOp reports whether op is one of the six comparisons the
// LoGOs array-index proof evaluates.
func boundsComparisonOp(op token.Token) bool {
	return op == token.LSS || op == token.LEQ || op == token.GTR ||
		op == token.GEQ || op == token.EQL || op == token.NEQ
}
