package gosec

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"

	"github.com/securego/gosec/v2/issue"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ast/astutil"
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
func constLeftViolations(pass *analysis.Pass, result *buildssa.SSA, issues []*issue.Issue) []constLeftFinding {
	candidates := make(map[issueLocation]bool)
	for _, iss := range issues {
		if iss.RuleID == boundRuleID && iss.What == boundWhat {
			candidates[issueLocation{iss.File, iss.Line, iss.Col}] = true
		}
	}
	var added []constLeftFinding
	for _, fn := range result.SrcFuncs {
		added = append(added, constLeftFuncViolations(pass, fn, candidates)...)
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
func constLeftFuncViolations(pass *analysis.Pass, fn *ssa.Function, candidates map[issueLocation]bool) []constLeftFinding {
	headers := loopHeaders(fn)
	var added []constLeftFinding
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			index, ok := instr.(*ssa.IndexAddr)
			if !ok {
				continue
			}
			if found := constLeftIndexViolation(pass, fn, headers, candidates, index); found != nil {
				added = append(added, *found)
			}
		}
	}
	return added
}

// constLeftIndexViolation decides one indexed access: recognize the local
// fixed-array target, match it against upstream candidates and proven-safe
// guards, then derive whether the const-on-left guard admits the boundary.
func constLeftIndexViolation(pass *analysis.Pass, fn *ssa.Function, headers map[*ssa.BasicBlock]bool, candidates map[issueLocation]bool, index *ssa.IndexAddr) *constLeftFinding {
	length, param, ok := constLeftTarget(pass, index)
	if !ok {
		return nil
	}
	file := pass.Fset.File(index.Pos())
	if file == nil {
		return nil
	}
	loc := issueLocation{file.Name(), strconv.Itoa(file.Line(index.Pos())), strconv.Itoa(pass.Fset.Position(index.Pos()).Column)}
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
	if !constLeftAdmitsBoundary(fn, pass.Files, index, param, length) {
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
// sourceIndexArray requires the index expression at pos to index an actual
// array-typed expression — a local array variable or an array literal — so a
// pointer to an array (`p := &[2]string{...}`) is not mistaken for the local
// fixed array the LoGOs declaration rule covers.
func sourceIndexArray(pass *analysis.Pass, pos token.Pos) bool {
	for _, file := range pass.Files {
		path, exact := astutil.PathEnclosingInterval(file, pos, pos)
		if !exact {
			continue
		}
		for _, node := range path {
			if expr, ok := node.(*ast.IndexExpr); ok {
				return indexArrayOperand(pass, path, expr)
			}
		}
		return false
	}
	return false
}

// indexArrayOperand requires the indexed expression to be array-typed and,
// when it is a plain name, a variable declared inside the enclosing
// function's body — SSA gives array parameters and results stack slots that
// look identical to local declarations.
func indexArrayOperand(pass *analysis.Pass, path []ast.Node, expr *ast.IndexExpr) bool {
	typ := pass.TypesInfo.TypeOf(expr.X)
	if typ == nil {
		return false
	}
	if _, isArray := typ.Underlying().(*types.Array); !isArray {
		return false
	}
	ident, isIdent := expr.X.(*ast.Ident)
	if !isIdent {
		return true // an array literal or expression is provable by type
	}
	v, isVar := pass.TypesInfo.ObjectOf(ident).(*types.Var)
	if !isVar {
		return false
	}
	body := enclosingBody(path)
	return body != nil && v.Pos() > body.Pos() && v.Pos() < body.End()
}

// enclosingBody returns the body of the innermost function literal or
// declaration containing the path.
func enclosingBody(path []ast.Node) *ast.BlockStmt {
	for _, node := range path {
		switch fn := node.(type) {
		case *ast.FuncDecl:
			return fn.Body
		case *ast.FuncLit:
			return fn.Body
		}
	}
	return nil
}

// witness representability check of the LoGOs rule.
func constLeftTarget(pass *analysis.Pass, index *ssa.IndexAddr) (int64, *ssa.Parameter, bool) {
	sizes := pass.TypesSizes
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
	if !sourceIndexArray(pass, index.Pos()) {
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
func constLeftAdmitsBoundary(fn *ssa.Function, files []*ast.File, index *ssa.IndexAddr, param ssa.Value, length int64) bool {
	matched := false
	visited := make(map[*ssa.BasicBlock]bool)
	block := fn.Blocks[0]
	for {
		if visited[block] {
			return false
		}
		visited[block] = true
		if !provablePath(block, index, files) {
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
// instructions the LoGOs proof can evaluate. In the access's block it checks
// the whole source statement around the access — a later operand in the same
// return or assignment still executes before it — and stops scanning at the
// statement's end since later statements cannot run before the access. The
// terminator is excluded: the walk resolves its edge separately.
func provablePath(block *ssa.BasicBlock, index *ssa.IndexAddr, files []*ast.File) bool {
	targetEnd := token.NoPos
	for i, instr := range block.Instrs {
		if i == len(block.Instrs)-1 {
			continue
		}
		if instr == index {
			targetEnd = targetStmtEnd(files, instr.Pos())
			continue
		}
		if targetEnd != token.NoPos {
			pos := instr.Pos()
			if pos == token.NoPos || pos > targetEnd {
				return true
			}
		}
		if !provableInstruction(instr, index, files) {
			return false
		}
	}
	return true
}

// targetStmtEnd returns the end of the statement region the proof evaluates
// around the access: the whole statement for a return, assignment or
// declaration, and the init-plus-condition region for an if statement, since
// the proof evaluates only the condition before reaching the access.
func targetStmtEnd(files []*ast.File, pos token.Pos) token.Pos {
	for _, file := range files {
		path, exact := astutil.PathEnclosingInterval(file, pos, pos)
		if !exact {
			continue
		}
		for _, node := range path {
			switch stmt := node.(type) {
			case *ast.IfStmt:
				return stmt.Cond.End()
			case ast.Stmt:
				return stmt.End()
			}
		}
	}
	return pos
}

// provableInstruction reports whether instr is an evaluation artifact of a
// statement the LoGOs proof walks: scalar comparisons, array construction
// and stores into locals, SSA join artifacts, local loads, and a boolean
// negation. A call, conversion, field or other index access, map or channel
// operation, and every other instruction kind maps to a statement or
// expression the proof cannot evaluate, so it stops — and this walk stays
// silent — wherever one appears on the witnessed path.
func provableInstruction(instr ssa.Instruction, index *ssa.IndexAddr, files []*ast.File) bool {
	switch instr := instr.(type) {
	case *ssa.BinOp:
		return boundsComparisonOp(instr.Op)
	case *ssa.UnOp:
		return provableUnOp(instr, index)
	case *ssa.IndexAddr:
		return literalElement(instr)
	case *ssa.Store:
		return provableStoreAddr(instr.Addr)
	case *ssa.Alloc:
		return provableAlloc(instr, files)
	case *ssa.ChangeType:
		return provableChangeType(instr, files)
	case *ssa.Phi, *ssa.Extract, *ssa.DebugRef, *ssa.RunDefers:
		return true
	}
	return false
}

// provableUnOp reports whether a unary operation is provable: a boolean
// negation the LoGOs operand rule evaluates, or a load reading a variable by
// name — a local, global, free variable, or the target access itself — which
// reads as a plain name at source level. Dereferencing any other value is an
// explicit dereference and stops the proof.
func provableUnOp(op *ssa.UnOp, index *ssa.IndexAddr) bool {
	if op.Op == token.NOT {
		return true
	}
	if op.Op != token.MUL {
		return false
	}
	if op.X == index {
		return true
	}
	switch op.X.(type) {
	case *ssa.Alloc, *ssa.Global, *ssa.FreeVar:
		return true
	}
	return false
}

// provableAlloc reports whether an allocation is provable. Heap construction
// helpers — new, make, slice literals, varargs — lower to call expressions
// the proof cannot evaluate and stop it; a lifted local array keeps its
// declaration provable regardless of its Heap flag, which describes the
// whole function, not the prefix before the access.
func provableAlloc(alloc *ssa.Alloc, files []*ast.File) bool {
	switch alloc.Comment {
	case "new", "makeslice", "slicelit", "varargs":
		// SSA tags synthetic heap construction with these comments, but a
		// source variable named `new` collides. A real helper sits inside a
		// call expression (new, make, spread call); a declaration does not —
		// and a declared variable keeps a scalar or array-of-scalar type.
		if insideCallExpr(files, alloc.Pos()) {
			return false
		}
		ptr, ok := alloc.Type().Underlying().(*types.Pointer)
		return ok && boundPlainType(ptr.Elem())
	}
	return true
}

// provableChangeType reports whether an implicit assignment type change is
// provable: the result keeps a scalar or array-of-scalar declaration type
// the LoGOs declaration rule accepts, and the instruction does not lower an
// explicit source conversion, which the proof sees as a call and stops at.
func provableChangeType(instr *ssa.ChangeType, files []*ast.File) bool {
	return boundPlainType(instr.Type()) && !insideCallExpr(files, instr.Pos())
}

// boundPlainType mirrors the LoGOs declaration rule's accepted types: scalar
// types and fixed arrays of scalar elements.
func boundPlainType(typ types.Type) bool {
	if scalarBoundType(typ) {
		return true
	}
	array, ok := typ.Underlying().(*types.Array)
	return ok && scalarBoundType(array.Elem())
}

// scalarBoundType mirrors the LoGOs scalar check: a basic const-capable type.
func scalarBoundType(typ types.Type) bool {
	basic, ok := typ.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsConstType != 0
}

// insideCallExpr reports whether pos lies inside a call expression in one of
// the package's syntax files.
func insideCallExpr(files []*ast.File, pos token.Pos) bool {
	for _, file := range files {
		path, exact := astutil.PathEnclosingInterval(file, pos, pos)
		if !exact {
			continue
		}
		for _, node := range path {
			if _, ok := node.(*ast.CallExpr); ok {
				return true
			}
		}
		return false
	}
	return false
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
