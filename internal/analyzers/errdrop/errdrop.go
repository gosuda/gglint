package errdrop

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

type neverFails struct{}

func (*neverFails) AFact() {}

// Analyzer reports dropped errors that later code depends on.
var Analyzer = &analysis.Analyzer{
	Name: "errdrop",
	Doc: `reports a dropped error only when later code depends on the call having succeeded

The neverFails fact is derived from the file set of the current build, so a function with build-tagged variants has the fact of the variant that is built.`,
	Requires:  []*analysis.Analyzer{inspect.Analyzer},
	Run:       run,
	FactTypes: []analysis.Fact{(*neverFails)(nil)},
}

func run(pass *analysis.Pass) (any, error) {
	set := computeNeverFails(pass)
	for origin := range set {
		pass.ExportObjectFact(origin, &neverFails{})
	}
	hash := hashInterface(pass.Pkg)
	for fn := range inspection.Of(pass).Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		processFunc(pass, fn, set, hash)
	}
	return nil, nil
}

func originOf(fn *types.Func) *types.Func {
	if o := fn.Origin(); o != nil {
		return o
	}
	return fn
}

func computeNeverFails(pass *analysis.Pass) map[*types.Func]bool {
	decls, objs := collectNeverFailsCandidates(pass)
	return fillNeverFailsSet(pass, decls, objs)
}

func collectNeverFailsCandidates(pass *analysis.Pass) ([]*ast.FuncDecl, []*types.Func) {
	decls := []*ast.FuncDecl{}
	objs := []*types.Func{}
	for _, f := range pass.Files {
		collectFileCandidates(pass, f, &decls, &objs)
	}
	return decls, objs
}

func collectFileCandidates(pass *analysis.Pass, f *ast.File, decls *[]*ast.FuncDecl, objs *[]*types.Func) {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
		if !ok {
			continue
		}
		sig, ok := obj.Type().(*types.Signature)
		if !ok || !hasErrorResult(sig) {
			continue
		}
		*decls = append(*decls, fn)
		*objs = append(*objs, obj)
	}
}

func fillNeverFailsSet(pass *analysis.Pass, decls []*ast.FuncDecl, objs []*types.Func) map[*types.Func]bool {
	set := map[*types.Func]bool{}
	changed := true
	for changed {
		changed = false
		for i, fn := range decls {
			if tryMarkNeverFails(pass, fn, objs[i], set) {
				changed = true
			}
		}
	}
	return set
}

func tryMarkNeverFails(pass *analysis.Pass, fn *ast.FuncDecl, obj *types.Func, set map[*types.Func]bool) bool {
	origin := originOf(obj)
	if set[origin] {
		return false
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig == nil {
		return false
	}
	if !namedResultsOK(pass, fn, sig) {
		return false
	}
	if !returnsOK(pass, fn, sig, set) {
		return false
	}
	set[origin] = true
	return true
}

func hasErrorResult(sig *types.Signature) bool {
	res := sig.Results()
	if res == nil {
		return false
	}
	for i := range res.Len() {
		if isError(res.At(i).Type()) {
			return true
		}
	}
	return false
}

func namedResultsOK(pass *analysis.Pass, fn *ast.FuncDecl, sig *types.Signature) bool {
	targets := namedErrorTargets(sig)
	if len(targets) == 0 {
		return true
	}
	assigned, addressed := scanNamedTargets(pass, fn, targets)
	return !assigned && !addressed
}

func namedErrorTargets(sig *types.Signature) []*types.Var {
	res := sig.Results()
	if res == nil {
		return nil
	}
	targets := []*types.Var{}
	for i := range res.Len() {
		v := res.At(i)
		if !isError(v.Type()) {
			continue
		}
		if v.Name() == "" || v.Name() == "_" {
			continue
		}
		targets = append(targets, v)
	}
	return targets
}

func scanNamedTargets(pass *analysis.Pass, fn *ast.FuncDecl, targets []*types.Var) (assigned, addressed bool) {
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		noteNamedUse(pass.TypesInfo, n, targets, &assigned, &addressed)
		return !assigned || !addressed
	})
	return assigned, addressed
}

func noteNamedUse(info *types.Info, n ast.Node, targets []*types.Var, assigned, addressed *bool) {
	switch t := n.(type) {
	case *ast.AssignStmt:
		noteAssignUse(info, t, targets, assigned)
	case *ast.UnaryExpr:
		if t.Op == token.AND && resolvesTo(info, t.X, targets) {
			*addressed = true
		}
	case *ast.IncDecStmt:
		if resolvesTo(info, t.X, targets) {
			*assigned = true
		}
	case *ast.RangeStmt:
		if rangeAssigns(info, t, targets) {
			*assigned = true
		}
	}
}

func noteAssignUse(info *types.Info, t *ast.AssignStmt, targets []*types.Var, assigned *bool) {
	for _, lhs := range t.Lhs {
		if resolvesTo(info, lhs, targets) {
			*assigned = true
		}
	}
}

func rangeAssigns(info *types.Info, t *ast.RangeStmt, targets []*types.Var) bool {
	if t.Tok != token.ASSIGN {
		return false
	}
	if t.Key != nil && resolvesTo(info, t.Key, targets) {
		return true
	}
	return t.Value != nil && resolvesTo(info, t.Value, targets)
}

func resolvesTo(info *types.Info, e ast.Expr, targets []*types.Var) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	for _, v := range targets {
		if o := info.ObjectOf(id); o == v {
			return true
		}
	}
	return false
}

func returnsOK(pass *analysis.Pass, fn *ast.FuncDecl, sig *types.Signature, set map[*types.Func]bool) bool {
	rets := []*ast.ReturnStmt{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if r, ok := n.(*ast.ReturnStmt); ok {
			rets = append(rets, r)
		}
		return true
	})
	for _, r := range rets {
		if !returnOK(pass, r, sig, set) {
			return false
		}
	}
	return true
}

func returnOK(pass *analysis.Pass, ret *ast.ReturnStmt, sig *types.Signature, set map[*types.Func]bool) bool {
	nres := resultsLen(sig)
	if len(ret.Results) == 0 {
		return allErrorsNamed(sig)
	}
	if len(ret.Results) == 1 && singleCallPasses(pass, ret.Results[0], nres, set) {
		return true
	}
	if len(ret.Results) != nres {
		return false
	}
	for i := range nres {
		if !isError(sig.Results().At(i).Type()) {
			continue
		}
		if !isNilIdent(pass.TypesInfo, ret.Results[i]) {
			return false
		}
	}
	return true
}

func resultsLen(sig *types.Signature) int {
	if sig.Results() == nil {
		return 0
	}
	return sig.Results().Len()
}

func singleCallPasses(pass *analysis.Pass, e ast.Expr, nres int, set map[*types.Func]bool) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nres == 1 && isNilIdent(pass.TypesInfo, e)
	}
	callee := calleeOrigin(pass, call)
	if callee == nil {
		return nres == 1 && isNilIdent(pass.TypesInfo, e)
	}
	csig, ok := callee.Type().(*types.Signature)
	if !ok {
		return false
	}
	return resultsLen(csig) == nres && hasFact(pass, callee, set)
}

func allErrorsNamed(sig *types.Signature) bool {
	res := sig.Results()
	if res == nil {
		return true
	}
	for i := range res.Len() {
		if !isError(res.At(i).Type()) {
			continue
		}
		if res.At(i).Name() == "" {
			return false
		}
	}
	return true
}

func calleeOrigin(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	obj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || obj == nil {
		return nil
	}
	return originOf(obj)
}

func hasFact(pass *analysis.Pass, origin *types.Func, set map[*types.Func]bool) bool {
	if set[origin] {
		return true
	}
	var f neverFails
	return pass.ImportObjectFact(origin, &f)
}

func isError(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	return named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

func isNilIdent(info *types.Info, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok || id.Name != "nil" {
		return false
	}
	_, ok = info.Uses[id].(*types.Nil)
	return ok
}

func callOf(e ast.Expr) *ast.CallExpr {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil
	}
	return call
}

func isRealCall(info *types.Info, call *ast.CallExpr) bool {
	tv := info.Types[call.Fun]
	return !tv.IsType() && !tv.IsBuiltin()
}

func resultTypes(info *types.Info, call *ast.CallExpr) []types.Type {
	t := info.TypeOf(call)
	tuple, ok := t.(*types.Tuple)
	if !ok {
		return []types.Type{t}
	}
	out := make([]types.Type, tuple.Len())
	for i := range out {
		out[i] = tuple.At(i).Type()
	}
	return out
}

func isBlank(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "_"
}

func hashInterface(pkg *types.Package) *types.Interface {
	hash := findPackage(pkg, "hash")
	if hash == nil {
		return nil
	}
	obj, ok := hash.Scope().Lookup("Hash").(*types.TypeName)
	if !ok {
		return nil
	}
	iface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	return iface
}

func findPackage(root *types.Package, path string) *types.Package {
	seen := map[*types.Package]bool{root: true}
	queue := []*types.Package{root}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if pkg.Path() == path {
			return pkg
		}
		for _, imp := range pkg.Imports() {
			if !seen[imp] {
				seen[imp] = true
				queue = append(queue, imp)
			}
		}
	}
	return nil
}

func isHashWrite(pass *analysis.Pass, hash *types.Interface, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Write" {
		return false
	}
	recv := pass.TypesInfo.TypeOf(sel.X)
	if recv == nil {
		return false
	}
	if hash != nil && implementsHash(recv, hash) {
		return true
	}
	return hashShape(pass, recv)
}

func implementsHash(recv types.Type, hash *types.Interface) bool {
	if types.Implements(recv, hash) {
		return true
	}
	_, isPtr := recv.Underlying().(*types.Pointer)
	return !isPtr && types.Implements(types.NewPointer(recv), hash)
}

func hashShape(pass *analysis.Pass, recv types.Type) bool {
	if !writerSigShape(hashSig(pass, recv, "Write")) {
		return false
	}
	if !isHashSumSig(hashSig(pass, recv, "Sum")) {
		return false
	}
	if !isHashResetSig(hashSig(pass, recv, "Reset")) {
		return false
	}
	return isHashSizeSig(hashSig(pass, recv, "Size")) && isHashSizeSig(hashSig(pass, recv, "BlockSize"))
}

func hashSig(pass *analysis.Pass, recv types.Type, name string) *types.Signature {
	obj, _, _ := types.LookupFieldOrMethod(recv, true, pass.Pkg, name)
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	return sig
}

func sigShape(sig *types.Signature, params, results int) bool {
	if sig == nil {
		return false
	}
	return tupleLen(sig.Params()) == params && tupleLen(sig.Results()) == results
}

func tupleLen(t *types.Tuple) int {
	if t == nil {
		return 0
	}
	return t.Len()
}

func isHashSumSig(sig *types.Signature) bool {
	if !sigShape(sig, 1, 1) {
		return false
	}
	return writerParamIsBytes(sig.Params().At(0).Type()) && writerParamIsBytes(sig.Results().At(0).Type())
}

func isHashResetSig(sig *types.Signature) bool {
	return sigShape(sig, 0, 0)
}

func isHashSizeSig(sig *types.Signature) bool {
	if !sigShape(sig, 0, 1) {
		return false
	}
	return types.Identical(sig.Results().At(0).Type(), types.Typ[types.Int])
}

func isExempt(pass *analysis.Pass, call *ast.CallExpr, set map[*types.Func]bool, hash *types.Interface) bool {
	info := pass.TypesInfo
	if isHashWrite(pass, hash, call) {
		return true
	}
	if callee := calleeOrigin(pass, call); callee != nil && hasFact(pass, callee, set) {
		return true
	}
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || fn == nil {
		return false
	}
	return isBenignStdoutWrite(pass, info, fn, call, set)
}

func isBenignStdoutWrite(pass *analysis.Pass, info *types.Info, fn *types.Func, call *ast.CallExpr, set map[*types.Func]bool) bool {
	switch fn.FullName() {
	case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln", "io.WriteString":
		return firstArgHasWriteFact(pass, info, call, set)
	}
	return false
}

func firstArgHasWriteFact(pass *analysis.Pass, info *types.Info, call *ast.CallExpr, set map[*types.Func]bool) bool {
	if len(call.Args) == 0 {
		return false
	}
	t0 := info.TypeOf(call.Args[0])
	if t0 == nil {
		return false
	}
	if _, ok := types.Unalias(t0).Underlying().(*types.Interface); ok {
		return false
	}
	return writeHasFact(pass, t0, set)
}

func writeHasFact(pass *analysis.Pass, t types.Type, set map[*types.Func]bool) bool {
	if obj, _, _ := types.LookupFieldOrMethod(t, true, pass.Pkg, "Write"); obj != nil {
		if fn, ok := obj.(*types.Func); ok && hasFact(pass, originOf(fn), set) {
			return true
		}
	}
	if _, isPtr := t.Underlying().(*types.Pointer); !isPtr {
		ptr := types.NewPointer(t)
		if obj, _, _ := types.LookupFieldOrMethod(ptr, true, pass.Pkg, "Write"); obj != nil {
			if fn, ok := obj.(*types.Func); ok && hasFact(pass, originOf(fn), set) {
				return true
			}
		}
	}
	return false
}

func owner(cur inspector.Cursor) inspector.Cursor {
	for fn := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		return fn
	}
	return inspector.Cursor{}
}

func processFunc(pass *analysis.Pass, fn inspector.Cursor, set map[*types.Func]bool, hash *types.Interface) {
	reported := map[token.Pos]bool{}
	checkValueSites(pass, fn, set, hash)
	checkOutSites(pass, fn, set, hash, reported)
	checkDurableSites(pass, fn, set, hash)
	checkVerdictLists(pass, fn, set, hash, reported)
}

func fnBody(fn inspector.Cursor) *ast.BlockStmt {
	switch n := fn.Node().(type) {
	case *ast.FuncDecl:
		return n.Body
	case *ast.FuncLit:
		return n.Body
	}
	return nil
}

func fnSignature(pass *analysis.Pass, fn inspector.Cursor) *types.Signature {
	switch n := fn.Node().(type) {
	case *ast.FuncDecl:
		if obj, ok := pass.TypesInfo.Defs[n.Name].(*types.Func); ok {
			sig, ok := obj.Type().(*types.Signature)
			if !ok {
				return nil
			}
			return sig
		}
	case *ast.FuncLit:
		sig, ok := pass.TypesInfo.TypeOf(n).(*types.Signature)
		if !ok {
			return nil
		}
		return sig
	}
	return nil
}

func declaredHere(fn inspector.Cursor, v *types.Var) bool {
	if v.Kind() != types.LocalVar {
		return false
	}
	body := fnBody(fn)
	if body == nil {
		return false
	}
	return body.Pos() <= v.Pos() && v.Pos() < body.End()
}

func checkValueSites(pass *analysis.Pass, fn inspector.Cursor, set map[*types.Func]bool, hash *types.Interface) {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		as := inspection.As[*ast.AssignStmt](cur.Node())
		if as.Tok != token.DEFINE && as.Tok != token.ASSIGN {
			continue
		}
		if allBlank(as.Lhs) {
			continue
		}
		call, vobj, vname := valueSite(pass, as)
		if call == nil {
			continue
		}
		if isExempt(pass, call, set, hash) {
			continue
		}
		if valueHolds(pass, fn, as, vobj) {
			callee := types.ExprString(call.Fun)
			pass.Reportf(call.Pos(), "%s is used after the error of %s is dropped, and it is nil when the call fails: check the error, or test %s for nil first", vname, callee, vname)
		}
	}
}

func allBlank(lhs []ast.Expr) bool {
	for _, e := range lhs {
		if !isBlank(e) {
			return false
		}
	}
	return len(lhs) > 0
}

func valueSite(pass *analysis.Pass, as *ast.AssignStmt) (*ast.CallExpr, *types.Var, string) {
	info := pass.TypesInfo
	if len(as.Rhs) != 1 {
		return nil, nil, ""
	}
	call := callOf(as.Rhs[0])
	if call == nil || !isRealCall(info, call) {
		return nil, nil, ""
	}
	res := resultTypes(info, call)
	if len(res) != len(as.Lhs) {
		return nil, nil, ""
	}
	errIdx := blankErrorIndex(res, as.Lhs)
	if errIdx < 0 {
		return nil, nil, ""
	}
	return qualifyingLHSVar(info, as, errIdx)
}

func blankErrorIndex(res []types.Type, lhs []ast.Expr) int {
	for i := range res {
		if isError(res[i]) && isBlank(lhs[i]) {
			return i
		}
	}
	return -1
}

func qualifyingLHSVar(info *types.Info, as *ast.AssignStmt, errIdx int) (*ast.CallExpr, *types.Var, string) {
	call := callOf(as.Rhs[0])
	for j := range as.Lhs {
		if j == errIdx || isBlank(as.Lhs[j]) {
			continue
		}
		v, name, ok := lhsNamedVar(info, as.Lhs[j])
		if !ok {
			continue
		}
		if !qualifyingValueType(v.Type()) {
			continue
		}
		return call, v, name
	}
	return nil, nil, ""
}

func lhsNamedVar(info *types.Info, e ast.Expr) (*types.Var, string, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, "", false
	}
	v, ok := info.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, "", false
	}
	return v, id.Name, true
}

func qualifyingValueType(t types.Type) bool {
	if t == nil || isError(t) {
		return false
	}
	switch types.Unalias(t).Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	}
	return false
}

func valueHolds(pass *analysis.Pass, fn inspector.Cursor, as *ast.AssignStmt, v *types.Var) bool {
	first, ok := firstUseAfter(pass, fn, v, as.End())
	if !ok {
		return false
	}
	if !isDeref(first, v) {
		return false
	}
	if interveningNilCheck(pass, fn, v, as.End(), first.Node().Pos()) {
		return false
	}
	return !interveningAssign(pass, fn, v, as.End(), first.Node().Pos())
}

func firstUseAfter(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, after token.Pos) (inspector.Cursor, bool) {
	var best inspector.Cursor
	bestPos := token.NoPos
	found := false
	for cur := range fn.Preorder((*ast.Ident)(nil)) {
		if owner(cur) != fn {
			continue
		}
		id := inspection.As[*ast.Ident](cur.Node())
		if id.Pos() <= after {
			continue
		}
		if pass.TypesInfo.Uses[id] != v {
			continue
		}
		if !found || id.Pos() < bestPos {
			best = cur
			bestPos = id.Pos()
			found = true
		}
	}
	return best, found
}

func skipParens(cur inspector.Cursor) inspector.Cursor {
	for cur.ParentEdgeKind() == edge.ParenExpr_X {
		cur = cur.Parent()
	}
	return cur
}

func isDeref(cur inspector.Cursor, v *types.Var) bool {
	id, ok := cur.Node().(*ast.Ident)
	if !ok {
		return false
	}
	up := skipParens(cur.Parent())
	switch p := up.Node().(type) {
	case *ast.SelectorExpr:
		return unparenIdentEquals(p.X, id)
	case *ast.StarExpr:
		return unparenIdentEquals(p.X, id)
	case *ast.CallExpr:
		fun, ok := ast.Unparen(p.Fun).(*ast.Ident)
		return ok && fun == id
	case *ast.IndexExpr:
		return isMapStoreAssign(up, p, v, id)
	}
	return false
}

func unparenIdentEquals(e ast.Expr, id *ast.Ident) bool {
	xid, ok := ast.Unparen(e).(*ast.Ident)
	return ok && xid == id
}

func isMapStoreAssign(up inspector.Cursor, p *ast.IndexExpr, v *types.Var, id *ast.Ident) bool {
	if !unparenIdentEquals(p.X, id) {
		return false
	}
	if _, ok := types.Unalias(v.Type()).Underlying().(*types.Map); !ok {
		return false
	}
	gp := skipParens(up.Parent())
	as, ok := gp.Node().(*ast.AssignStmt)
	if !ok {
		return false
	}
	return lhsStoresIndex(as, up, p)
}

func lhsStoresIndex(as *ast.AssignStmt, up inspector.Cursor, p *ast.IndexExpr) bool {
	for _, lhs := range as.Lhs {
		if ast.Unparen(lhs) == up.Node() {
			return true
		}
		if idx, ok := ast.Unparen(lhs).(*ast.IndexExpr); ok && idx == p {
			return true
		}
	}
	return false
}

func interveningNilCheck(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, from, to token.Pos) bool {
	for cur := range fn.Preorder((*ast.BinaryExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		b := inspection.As[*ast.BinaryExpr](cur.Node())
		if b.Op != token.EQL && b.Op != token.NEQ {
			continue
		}
		if b.Pos() <= from || b.Pos() >= to {
			continue
		}
		if nilCompareWith(pass.TypesInfo, b, v) {
			return true
		}
	}
	return false
}

func nilCompareWith(info *types.Info, b *ast.BinaryExpr, v *types.Var) bool {
	return sideIsVarAndOtherNil(info, b.X, b.Y, v) || sideIsVarAndOtherNil(info, b.Y, b.X, v)
}

func sideIsVarAndOtherNil(info *types.Info, side, other ast.Expr, v *types.Var) bool {
	id, ok := ast.Unparen(side).(*ast.Ident)
	if !ok {
		return false
	}
	if info.Uses[id] != v && info.Defs[id] != v {
		return false
	}
	oid, ok := ast.Unparen(other).(*ast.Ident)
	if !ok || oid.Name != "nil" {
		return false
	}
	_, ok = info.Uses[oid].(*types.Nil)
	return ok
}

func interveningAssign(pass *analysis.Pass, fn inspector.Cursor, v *types.Var, from, to token.Pos) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.IncDecStmt)(nil), (*ast.RangeStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if nodeInWindow(cur.Node(), from, to) && nodeAssignsVar(pass.TypesInfo, cur.Node(), v) {
			return true
		}
	}
	return false
}

func nodeInWindow(n ast.Node, from, to token.Pos) bool {
	return n.Pos() > from && n.Pos() < to
}

func nodeAssignsVar(info *types.Info, n ast.Node, v *types.Var) bool {
	switch t := n.(type) {
	case *ast.AssignStmt:
		for _, lhs := range t.Lhs {
			if resolvesTo(info, lhs, []*types.Var{v}) {
				return true
			}
		}
	case *ast.IncDecStmt:
		if resolvesTo(info, t.X, []*types.Var{v}) {
			return true
		}
	case *ast.RangeStmt:
		return rangeAssigns(info, t, []*types.Var{v})
	}
	return false
}

func checkOutSites(pass *analysis.Pass, fn inspector.Cursor, set map[*types.Func]bool, hash *types.Interface, reported map[token.Pos]bool) {
	for cur := range fn.Preorder((*ast.ExprStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		es := inspection.As[*ast.ExprStmt](cur.Node())
		call := callOf(es.X)
		if call == nil || !isRealCall(pass.TypesInfo, call) {
			continue
		}
		if !dropsError(pass.TypesInfo, call) {
			continue
		}
		if isExempt(pass, call, set, hash) {
			continue
		}
		xobj, xname := outCandidate(pass, fn, call)
		if xobj == nil {
			continue
		}
		callee := types.ExprString(call.Fun)
		pass.Reportf(call.Pos(), "%s is read after the error of %s is dropped, and it keeps its zero value when the call fails: check the error, or write _ = %s(...) to state that the zero value is intended", xname, callee, callee)
		reported[call.Pos()] = true
	}
}

func dropsError(info *types.Info, call *ast.CallExpr) bool {
	for _, t := range resultTypes(info, call) {
		if isError(t) {
			return true
		}
	}
	return false
}

func outCandidate(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr) (found *types.Var, name string) {
	for _, arg := range call.Args {
		u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			continue
		}
		id, ok := ast.Unparen(u.X).(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok {
			continue
		}
		if outHolds(pass, fn, call, v) {
			return v, id.Name
		}
	}
	return nil, ""
}

func outHolds(pass *analysis.Pass, fn inspector.Cursor, call *ast.CallExpr, x *types.Var) bool {
	if !declaredHere(fn, x) {
		return false
	}
	if !isBareVarDecl(pass, fn, x) {
		return false
	}
	if assignsVar(pass, fn, x) {
		return false
	}
	return mentionedAfter(pass, fn, x, call.End())
}

func isBareVarDecl(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	found := false
	none := true
	for cur := range fn.Preorder((*ast.ValueSpec)(nil)) {
		if owner(cur) != fn {
			continue
		}
		vs := inspection.As[*ast.ValueSpec](cur.Node())
		for _, name := range vs.Names {
			if pass.TypesInfo.Defs[name] == x {
				found = true
				if len(vs.Values) != 0 {
					none = false
				}
			}
		}
	}
	return found && none
}

func assignsVar(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil), (*ast.IncDecStmt)(nil), (*ast.RangeStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if nodeAssignsVar(pass.TypesInfo, cur.Node(), x) {
			return true
		}
	}
	return false
}

func mentionedAfter(pass *analysis.Pass, fn inspector.Cursor, x *types.Var, after token.Pos) bool {
	for cur := range fn.Preorder((*ast.Ident)(nil)) {
		if owner(cur) != fn {
			continue
		}
		id := inspection.As[*ast.Ident](cur.Node())
		if id.Pos() <= after {
			continue
		}
		if pass.TypesInfo.Uses[id] == x {
			return true
		}
	}
	return false
}

const closeMethodName = "Close"

func checkDurableSites(pass *analysis.Pass, fn inspector.Cursor, set map[*types.Func]bool, hash *types.Interface) {
	for cur := range fn.Preorder((*ast.ExprStmt)(nil), (*ast.DeferStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		call := durableCall(cur)
		if call == nil || !isRealCall(pass.TypesInfo, call) {
			continue
		}
		xobj, xname := closeReceiver(pass, call)
		if xobj == nil {
			continue
		}
		if !isExactlyError(pass.TypesInfo, call) {
			continue
		}
		if isExempt(pass, call, set, hash) {
			continue
		}
		if !durableHolds(pass, fn, xobj) {
			continue
		}
		pass.Reportf(call.Pos(), "%s is written in this function and the error of %s.Close is dropped, so a failed Close loses data: return or check the Close error", xname, xname)
	}
}

func durableCall(cur inspector.Cursor) *ast.CallExpr {
	switch n := cur.Node().(type) {
	case *ast.ExprStmt:
		return callOf(n.X)
	case *ast.DeferStmt:
		if call := callOf(n.Call); call != nil {
			return call
		}
		return n.Call
	}
	return nil
}

func closeReceiver(pass *analysis.Pass, call *ast.CallExpr) (found *types.Var, name string) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != closeMethodName {
		return nil, ""
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil, ""
	}
	if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		return v, id.Name
	}
	return nil, ""
}

func isExactlyError(info *types.Info, call *ast.CallExpr) bool {
	t := info.TypeOf(call)
	return t != nil && isError(t)
}

func durableHolds(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	if !isLocalPtrStruct(fn, x) {
		return false
	}
	if !writtenInFunc(pass, fn, x) {
		return false
	}
	return !hasUsedCloseSync(pass, fn, x)
}

func isLocalPtrStruct(fn inspector.Cursor, x *types.Var) bool {
	if !declaredHere(fn, x) {
		return false
	}
	ptr, ok := types.Unalias(x.Type()).(*types.Pointer)
	if !ok {
		return false
	}
	_, ok = types.Unalias(ptr.Elem()).Underlying().(*types.Struct)
	return ok
}

func writtenInFunc(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	return assignedFromCreate(pass, fn, x) || writtenByMethod(pass, fn, x) || writtenByFirstArg(pass, fn, x)
}

func assignedFromCreate(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.AssignStmt)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if createWritesFile(pass, inspection.As[*ast.AssignStmt](cur.Node()), x) {
			return true
		}
	}
	return false
}

func createWritesFile(pass *analysis.Pass, as *ast.AssignStmt, x *types.Var) bool {
	if !assignTargetsVar(pass.TypesInfo, as, x) {
		return false
	}
	if len(as.Rhs) != 1 {
		return false
	}
	call := callOf(as.Rhs[0])
	if call == nil {
		return false
	}
	return createCallWritable(pass, call)
}

func assignTargetsVar(info *types.Info, as *ast.AssignStmt, x *types.Var) bool {
	for _, lhs := range as.Lhs {
		if resolvesTo(info, lhs, []*types.Var{x}) {
			return true
		}
	}
	return false
}

func createCallWritable(pass *analysis.Pass, call *ast.CallExpr) bool {
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return false
	}
	switch fnobj.FullName() {
	case "os.Create", "os.CreateTemp":
		return true
	case "os.OpenFile":
		return len(call.Args) >= 2 && flagWritable(pass.TypesInfo, call.Args[1])
	}
	return false
}

func flagWritable(info *types.Info, e ast.Expr) bool {
	v := info.Types[ast.Unparen(e)].Value
	if v == nil {
		return false
	}
	n, ok := constant.Int64Val(v)
	return ok && n&3 != 0
}

func writtenByMethod(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		call := inspection.As[*ast.CallExpr](cur.Node())
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			continue
		}
		id, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.ObjectOf(id) != types.Object(x) {
			continue
		}
		if strings.HasPrefix(sel.Sel.Name, "Write") || sel.Sel.Name == "ReadFrom" {
			return true
		}
	}
	return false
}

func writtenByFirstArg(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		if firstArgTakesWriter(pass, inspection.As[*ast.CallExpr](cur.Node()), x) {
			return true
		}
	}
	return false
}

func firstArgTakesWriter(pass *analysis.Pass, call *ast.CallExpr, x *types.Var) bool {
	if len(call.Args) == 0 {
		return false
	}
	id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
		return false
	}
	fnobj, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fnobj == nil {
		return false
	}
	sig, ok := fnobj.Type().(*types.Signature)
	if !ok || sig.Params() == nil || sig.Params().Len() == 0 {
		return false
	}
	return isWriterInterface(sig.Params().At(0).Type())
}

func isWriterInterface(t types.Type) bool {
	if _, ok := types.Unalias(t).Underlying().(*types.Interface); !ok {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(t, false, nil, "Write")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	return writerSigShape(sig)
}

func writerSigShape(sig *types.Signature) bool {
	if sig.Params() == nil || sig.Params().Len() != 1 {
		return false
	}
	if sig.Results() == nil || sig.Results().Len() != 2 {
		return false
	}
	return writerParamIsBytes(sig.Params().At(0).Type()) && writerResultsShape(sig)
}

func writerParamIsBytes(p0 types.Type) bool {
	slice, ok := types.Unalias(p0).(*types.Slice)
	return ok && types.Identical(slice.Elem(), types.Typ[types.Uint8])
}

func writerResultsShape(sig *types.Signature) bool {
	return types.Identical(sig.Results().At(0).Type(), types.Typ[types.Int]) && isError(sig.Results().At(1).Type())
}

func hasUsedCloseSync(pass *analysis.Pass, fn inspector.Cursor, x *types.Var) bool {
	for cur := range fn.Preorder((*ast.CallExpr)(nil)) {
		if owner(cur) != fn {
			continue
		}
		call := inspection.As[*ast.CallExpr](cur.Node())
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != closeMethodName && sel.Sel.Name != "Sync") {
			continue
		}
		id, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok || pass.TypesInfo.ObjectOf(id) != types.Object(x) {
			continue
		}
		if resultUsed(cur) {
			return true
		}
	}
	return false
}

func resultUsed(cur inspector.Cursor) bool {
	p := skipParens(cur.Parent())
	switch n := p.Node().(type) {
	case *ast.ExprStmt, *ast.DeferStmt, *ast.GoStmt:
		return false
	case *ast.AssignStmt:
		return !allBlank(n.Lhs)
	}
	return true
}

func checkVerdictLists(pass *analysis.Pass, fn inspector.Cursor, set map[*types.Func]bool, hash *types.Interface, reported map[token.Pos]bool) {
	sig := fnSignature(pass, fn)
	if sig == nil || sig.Results() == nil || sig.Results().Len() == 0 {
		return
	}
	if !isError(sig.Results().At(sig.Results().Len() - 1).Type()) {
		return
	}
	for cur := range fn.Preorder((*ast.BlockStmt)(nil), (*ast.CaseClause)(nil), (*ast.CommClause)(nil)) {
		if owner(cur) != fn {
			continue
		}
		list := verdictListOf(cur)
		if len(list) < 2 {
			continue
		}
		checkVerdictPairs(pass, list, set, hash, reported)
	}
}

func verdictListOf(cur inspector.Cursor) []ast.Stmt {
	switch n := cur.Node().(type) {
	case *ast.BlockStmt:
		return n.List
	case *ast.CaseClause:
		return n.Body
	case *ast.CommClause:
		return n.Body
	}
	return nil
}

func checkVerdictPairs(pass *analysis.Pass, list []ast.Stmt, set map[*types.Func]bool, hash *types.Interface, reported map[token.Pos]bool) {
	for i := range len(list) - 1 {
		call, ok := verdictPairCall(pass, list, i, set, hash, reported)
		if !ok {
			continue
		}
		callee := types.ExprString(call.Fun)
		pass.Reportf(call.Pos(), "the error of %s is dropped and the next statement returns nil, so the caller is told this step succeeded: return the error, or write _ = %s(...) to state that the step is best effort", callee, callee)
	}
}

func verdictPairCall(pass *analysis.Pass, list []ast.Stmt, i int, set map[*types.Func]bool, hash *types.Interface, reported map[token.Pos]bool) (*ast.CallExpr, bool) {
	es, ok := list[i].(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call := callOf(es.X)
	if call == nil || !isRealCall(pass.TypesInfo, call) {
		return nil, false
	}
	if isCloseMethod(call) {
		return nil, false
	}
	if !dropsError(pass.TypesInfo, call) {
		return nil, false
	}
	if reported[call.Pos()] {
		return nil, false
	}
	if isExempt(pass, call, set, hash) {
		return nil, false
	}
	if !verdictNextReturnsNil(pass, list[i+1]) {
		return nil, false
	}
	return call, true
}

func verdictNextReturnsNil(pass *analysis.Pass, stmt ast.Stmt) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	return isNilIdent(pass.TypesInfo, ret.Results[len(ret.Results)-1])
}

func isCloseMethod(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	return ok && sel.Sel.Name == closeMethodName
}
