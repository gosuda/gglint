package infiniterecursion

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const message = "function %s calls itself with its own arguments on every path"

var Analyzer = &analysis.Analyzer{
	Name: "infinite_recursion",
	Doc: `report functions that call themselves with their own arguments on every path

A function declaration is reported when every path from entry reaches a call
to itself, with arguments identical to its parameters, before any return. A
path that can leave the function another way (a return, a branch, a panic, a
process exit) or never reach the call (a for statement with no condition)
clears the function, and so does any assignment to, increment of, range into,
or address-of the receiver or a parameter. Arguments match by object, so a
shadowing variable of the same name does not count. Port of loGos
correctness::infinite_recursion.`,
	Run: run,
}

var terminators = []string{"os.Exit", "syscall.Exit", "runtime.Goexit", "log.Fatal", "log.Panic", "(*log.Logger).Fatal", "(*log.Logger).Panic"}

type recursion struct {
	info     *types.Info
	fn       *types.Func
	recv     types.Object
	params   []types.Object
	variadic bool
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if r := newRecursion(pass.TypesInfo, fd); r != nil && r.sequence(fd.Body.List) == recurses {
				pass.Reportf(fd.Name.Pos(), message, fd.Name.Name)
			}
		}
	}
	return nil, nil
}

func newRecursion(info *types.Info, fd *ast.FuncDecl) *recursion {
	fn, ok := info.Defs[fd.Name].(*types.Func)
	if !ok {
		return nil
	}
	r := &recursion{info: info, fn: fn}
	if fd.Recv != nil {
		recv := r.defined(fd.Recv.List)
		if len(recv) != 1 {
			return nil
		}
		r.recv = recv[0]
	}
	params := r.defined(fd.Type.Params.List)
	if params == nil {
		return nil
	}
	r.params = params
	if r.assignsParam(fd.Body) {
		return nil
	}
	return r
}

func (r *recursion) defined(fields []*ast.Field) []types.Object {
	objs := []types.Object{}
	for _, f := range fields {
		if len(f.Names) == 0 {
			return nil
		}
		for _, name := range f.Names {
			obj := r.info.Defs[name]
			if name.Name == "_" || obj == nil {
				return nil
			}
			objs = append(objs, obj)
		}
		_, r.variadic = f.Type.(*ast.Ellipsis)
	}
	return objs
}

func (r *recursion) assignsParam(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		found = found || r.mutates(n)
		return !found
	})
	return found
}

func (r *recursion) mutates(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		return r.rooted(n.Lhs...)
	case *ast.IncDecStmt:
		return r.rooted(n.X)
	case *ast.RangeStmt:
		return r.rooted(n.Key, n.Value)
	case *ast.UnaryExpr:
		return n.Op == token.AND && r.rooted(n.X)
	}
	return false
}

func (r *recursion) rooted(exprs ...ast.Expr) bool {
	for _, x := range exprs {
		name := rootName(x)
		if name == nil {
			continue
		}
		obj := r.info.Uses[name]
		if obj != nil && (obj == r.recv || slices.Contains(r.params, obj)) {
			return true
		}
	}
	return false
}

func rootName(e ast.Expr) *ast.Ident {
	for e != nil {
		if id, ok := e.(*ast.Ident); ok {
			return id
		}
		e = operand(e)
	}
	return nil
}

func operand(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.X
	case *ast.IndexExpr:
		return x.X
	case *ast.IndexListExpr:
		return x.X
	case *ast.ParenExpr:
		return x.X
	case *ast.StarExpr:
		return x.X
	case *ast.UnaryExpr:
		return x.X
	}
	return nil
}

type outcome int

const (
	falls outcome = iota
	recurses
	leaves
)

func (r *recursion) sequence(list []ast.Stmt) outcome {
	for _, s := range list {
		if r.stmtCalls(s) {
			return recurses
		}
		if r.mayLeave(s) && !r.exitsCovered(s) {
			return leaves
		}
	}
	return falls
}

func (r *recursion) stmtCalls(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ExprStmt:
		return r.exprCalls(s.X)
	case *ast.ReturnStmt:
		return r.anyCalls(s.Results)
	case *ast.AssignStmt:
		return r.anyCalls(s.Rhs)
	case *ast.BlockStmt:
		return r.sequence(s.List) == recurses
	}
	return r.branchingCalls(s)
}

func (r *recursion) branchingCalls(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.IfStmt:
		return r.ifCalls(s)
	case *ast.SwitchStmt:
		return s.Init == nil && (r.exprCalls(s.Tag) || r.armsCall(s.Body))
	case *ast.TypeSwitchStmt:
		return s.Init == nil && r.armsCall(s.Body)
	}
	return false
}

func (r *recursion) ifCalls(s *ast.IfStmt) bool {
	if s.Init != nil {
		return false
	}
	if r.exprCalls(s.Cond) {
		return true
	}
	return s.Else != nil && r.sequence(s.Body.List) == recurses && r.stmtCalls(s.Else)
}

func (r *recursion) armsCall(body *ast.BlockStmt) bool {
	def := false
	for _, s := range body.List {
		c := inspection.As[*ast.CaseClause](s)
		def = def || c.List == nil
		if r.sequence(c.Body) != recurses {
			return false
		}
	}
	return def
}

func (r *recursion) exitsCovered(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.IfStmt:
		return s.Init == nil && r.sequence(s.Body.List) != leaves &&
			(s.Else == nil || r.sequence([]ast.Stmt{s.Else}) != leaves)
	case *ast.SwitchStmt:
		return s.Init == nil && r.armsCovered(s.Body)
	case *ast.TypeSwitchStmt:
		return s.Init == nil && r.armsCovered(s.Body)
	}
	return false
}

func (r *recursion) armsCovered(body *ast.BlockStmt) bool {
	for _, s := range body.List {
		if r.sequence(inspection.As[*ast.CaseClause](s).Body) == leaves {
			return false
		}
	}
	return true
}

func (r *recursion) exprCalls(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return r.exprCalls(e.X)
	case *ast.CallExpr:
		return r.isSelfCall(e) || r.anyCalls(e.Args)
	case *ast.StarExpr:
		return r.exprCalls(e.X)
	case *ast.UnaryExpr:
		return r.exprCalls(e.X)
	case *ast.BinaryExpr:
		return r.binaryCalls(e)
	}
	return false
}

func (r *recursion) binaryCalls(e *ast.BinaryExpr) bool {
	if r.exprCalls(e.X) {
		return true
	}
	conditional := e.Op == token.LAND || e.Op == token.LOR
	return !conditional && r.exprCalls(e.Y)
}

func (r *recursion) anyCalls(list []ast.Expr) bool {
	return slices.ContainsFunc(list, r.exprCalls)
}

func (r *recursion) isSelfCall(call *ast.CallExpr) bool {
	if len(call.Args) != len(r.params) || call.Ellipsis.IsValid() != r.variadic || !r.callsFn(call.Fun) {
		return false
	}
	for i, a := range call.Args {
		if n, ok := a.(*ast.Ident); !ok || r.info.Uses[n] != r.params[i] {
			return false
		}
	}
	return true
}

func (r *recursion) callsFn(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.ParenExpr:
		return r.callsFn(f.X)
	case *ast.Ident:
		return r.recv == nil && r.info.Uses[f] == r.fn
	case *ast.SelectorExpr:
		return r.onReceiver(f) && r.info.Uses[f.Sel] == r.fn
	case *ast.IndexExpr:
		return r.callsGeneric(f.X)
	case *ast.IndexListExpr:
		return r.callsGeneric(f.X)
	}
	return false
}

func (r *recursion) onReceiver(sel *ast.SelectorExpr) bool {
	x, ok := sel.X.(*ast.Ident)
	return ok && r.recv != nil && r.info.Uses[x] == r.recv
}

func (r *recursion) callsGeneric(base ast.Expr) bool {
	tparams := r.fn.Signature().TypeParams()
	if tparams == nil || tparams.Len() == 0 {
		return false
	}
	id := r.instantiated(ast.Unparen(base))
	if id == nil {
		return false
	}
	inst, ok := r.info.Instances[id]
	if !ok || inst.TypeArgs == nil || inst.TypeArgs.Len() != tparams.Len() {
		return false
	}
	for i := range tparams.Len() {
		if !types.Identical(inst.TypeArgs.At(i), tparams.At(i)) {
			return false
		}
	}
	return true
}

func (r *recursion) instantiated(base ast.Expr) *ast.Ident {
	switch b := base.(type) {
	case *ast.Ident:
		if r.recv == nil && r.info.Uses[b] == r.fn {
			return b
		}
	case *ast.SelectorExpr:
		if r.onReceiver(b) && r.methodIsFn(b) {
			return b.Sel
		}
	}
	return nil
}

func (r *recursion) methodIsFn(sel *ast.SelectorExpr) bool {
	if s, ok := r.info.Selections[sel]; ok {
		fn, ok := s.Obj().(*types.Func)
		return ok && fn.Origin() == r.fn.Origin()
	}
	fn, ok := r.info.Uses[sel.Sel].(*types.Func)
	return ok && fn.Origin() == r.fn.Origin()
}

func (r *recursion) mayLeave(s ast.Stmt) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		found = found || r.leavesSequence(n)
		_, lit := n.(*ast.FuncLit)
		return !found && !lit
	})
	return found
}

func (r *recursion) leavesSequence(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.CallExpr:
		return r.endsProcess(n)
	case *ast.ForStmt:
		return n.Cond == nil
	}
	return false
}

func (r *recursion) endsProcess(call *ast.CallExpr) bool {
	if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "panic" {
		return true
	}
	fn, ok := typeutil.Callee(r.info, call).(*types.Func)
	if !ok {
		return false
	}
	full := fn.FullName()
	return slices.ContainsFunc(terminators, func(t string) bool { return strings.HasPrefix(full, t) })
}
