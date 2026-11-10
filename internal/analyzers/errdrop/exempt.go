package errdrop

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

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
	if !ok || sel.Sel.Name != writeMethodName {
		return false
	}
	recv := pass.TypesInfo.TypeOf(sel.X)
	if recv == nil {
		return false
	}
	if hash != nil && implementsHash(recv, hash) {
		return true
	}
	if isSingleWriteIface(recv) {
		return true
	}
	return hashShape(pass, recv)
}

func isSingleWriteIface(recv types.Type) bool {
	named, ok := types.Unalias(recv).(*types.Named)
	if !ok || ast.IsExported(named.Obj().Name()) {
		return false
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok || iface.NumMethods() != 1 {
		return false
	}
	if iface.Method(0).Name() != writeMethodName {
		return false
	}
	sig, ok := iface.Method(0).Type().(*types.Signature)
	if !ok {
		return false
	}
	return writerSigShape(sig)
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

func isExempt(pass *analysis.Pass, call *ast.CallExpr, f facts, hash *types.Interface) bool {
	info := pass.TypesInfo
	if isHashWrite(pass, hash, call) {
		return true
	}
	if callee := calleeOrigin(pass, call); callee != nil && hasFact(pass, callee, f.never) {
		return true
	}
	if isErrorSink(pass, call) {
		return true
	}
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	if !ok || fn == nil {
		return false
	}
	return writerThrough(pass, info, fn, call, f, hash)
}

func writerThrough(pass *analysis.Pass, info *types.Info, fn *types.Func, call *ast.CallExpr, f facts, hash *types.Interface) bool {
	if tableWriterAtZero(pass, info, fn, call) {
		return dstInfallible(pass, info, call, 0, f.never, hash)
	}
	callee := calleeOrigin(pass, call)
	if callee == nil {
		return false
	}
	q, ok := writerFact(pass, callee, f.writer)
	if !ok {
		return false
	}
	return dstInfallible(pass, info, call, q, f.never, hash)
}

func tableWriterAtZero(pass *analysis.Pass, info *types.Info, fn *types.Func, call *ast.CallExpr) bool {
	switch fn.FullName() {
	case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln", "io.WriteString", "encoding/xml.EscapeText":
		return true
	}
	if isReplacerWriteString(pass, call) {
		return true
	}
	if isMemoryWriteTo(pass, call) {
		return true
	}
	if fn.FullName() == "io.Copy" {
		return copySourceInMemory(info, call)
	}
	return false
}

func dstInfallible(pass *analysis.Pass, info *types.Info, call *ast.CallExpr, q int, set map[*types.Func]bool, hash *types.Interface) bool {
	if q >= len(call.Args) {
		return false
	}
	t := info.TypeOf(call.Args[q])
	if t == nil {
		return false
	}
	if _, ok := types.Unalias(t).Underlying().(*types.Interface); ok {
		return hash != nil && types.Implements(t, hash)
	}
	return writeHasFact(pass, t, set) || writeStringHasFact(pass, t, set) || typeIsHash(pass, t, hash)
}

func writeStringHasFact(pass *analysis.Pass, t types.Type, set map[*types.Func]bool) bool {
	if obj, _, _ := types.LookupFieldOrMethod(t, true, pass.Pkg, "WriteString"); obj != nil {
		if fn, ok := obj.(*types.Func); ok && hasFact(pass, originOf(fn), set) {
			return true
		}
	}
	if _, isPtr := t.Underlying().(*types.Pointer); !isPtr {
		ptr := types.NewPointer(t)
		if obj, _, _ := types.LookupFieldOrMethod(ptr, true, pass.Pkg, "WriteString"); obj != nil {
			if fn, ok := obj.(*types.Func); ok && hasFact(pass, originOf(fn), set) {
				return true
			}
		}
	}
	return false
}

func isReplacerWriteString(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteString" {
		return false
	}
	return isReplacerRecv(pass.TypesInfo.TypeOf(sel.X))
}

func isReplacerRecv(recv types.Type) bool {
	if recv == nil {
		return false
	}
	if isReplacerNamed(types.Unalias(recv)) {
		return true
	}
	ptr, ok := types.Unalias(recv).(*types.Pointer)
	if !ok {
		return false
	}
	return isReplacerNamed(types.Unalias(ptr.Elem()))
}

func isReplacerNamed(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "strings" && named.Obj().Name() == "Replacer"
}

func isMemoryWriteTo(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteTo" {
		return false
	}
	recv := pass.TypesInfo.TypeOf(sel.X)
	return isInMemoryReader(recv) || isInMemoryValue(recv)
}

func copySourceInMemory(info *types.Info, call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}
	src := info.TypeOf(call.Args[1])
	if src != nil && isInMemoryReader(src) {
		return true
	}
	inner, ok := limitReaderInner(info, call.Args[1])
	return ok && isInMemoryReader(inner)
}

func limitReaderInner(info *types.Info, e ast.Expr) (types.Type, bool) {
	c, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	fn, ok := typeutil.Callee(info, c).(*types.Func)
	if !ok || fn == nil || fn.FullName() != "io.LimitReader" || len(c.Args) < 1 {
		return nil, false
	}
	t := info.TypeOf(c.Args[0])
	if t == nil {
		return nil, false
	}
	return t, true
}

func isInMemoryReader(t types.Type) bool {
	ptr, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return false
	}
	return isInMemoryNamed(types.Unalias(ptr.Elem()))
}

func isInMemoryValue(t types.Type) bool {
	if t == nil {
		return false
	}
	return isInMemoryNamed(types.Unalias(t))
}

func isInMemoryNamed(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	switch named.Obj().Pkg().Path() + "." + named.Obj().Name() {
	case "bytes.Buffer", "bytes.Reader", "strings.Reader":
		return true
	}
	return false
}

func typeIsHash(pass *analysis.Pass, t types.Type, hash *types.Interface) bool {
	if hash != nil && implementsHash(t, hash) {
		return true
	}
	return hashShape(pass, t)
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

func calleeDecl(pass *analysis.Pass, call *ast.CallExpr) *ast.FuncDecl {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn == nil {
		return nil
	}
	want := originOf(fn)
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if obj, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok && originOf(obj) == want {
				return fd
			}
		}
	}
	return nil
}

func isErrorSink(pass *analysis.Pass, call *ast.CallExpr) bool {
	fd := calleeDecl(pass, call)
	if fd == nil {
		return false
	}
	sig, ok := pass.TypesInfo.Defs[fd.Name].Type().(*types.Signature)
	if !ok || !sinkSignature(sig) {
		return false
	}
	return sinkReturns(pass, fd) && sinkClean(pass, fd)
}

func sinkSignature(sig *types.Signature) bool {
	found := false
	for v := range sig.Params().Variables() {
		if isError(v.Type()) {
			found = true
		}
	}
	return found && sig.Results().Len() == 1 && isError(sig.Results().At(0).Type())
}

func sinkReturns(pass *analysis.Pass, fd *ast.FuncDecl) bool {
	sink := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			if ok {
				sink = false
			}
			return true
		}
		e := ast.Unparen(ret.Results[0])
		if isNilIdent(pass.TypesInfo, e) {
			return true
		}
		if callOf(e) != nil || !isError(pass.TypesInfo.TypeOf(e)) {
			sink = false
		}
		return true
	})
	return sink
}

func sinkClean(pass *analysis.Pass, fd *ast.FuncDecl) bool {
	clean := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		c, ok := n.(*ast.CallExpr)
		if !ok || !isRealCall(pass.TypesInfo, c) {
			return true
		}
		if slices.ContainsFunc(resultTypes(pass.TypesInfo, c), isError) {
			clean = false
			return false
		}
		return true
	})
	return clean
}
