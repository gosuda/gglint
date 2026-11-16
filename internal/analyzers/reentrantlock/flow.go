package reentrantlock

import (
	"go/ast"
	"go/token"
)

type flow struct {
	c         *checker
	onCall    func(*ast.CallExpr)
	onGo      func(*ast.GoStmt)
	onBarrier func()
	onStmts   func([]ast.Stmt)
	deferHeld func(*ast.DeferStmt) bool
	state     pathState
}

func (f *flow) walk(n ast.Node) {
	if n == nil || f.state.stopped {
		return
	}
	ast.Inspect(n, f.visit)
}

func (f *flow) walkStmts(list []ast.Stmt) {
	for _, s := range list {
		f.walk(s)
	}
}

func (f *flow) visit(n ast.Node) bool {
	if f.state.stopped || f.invocation(n) || f.control(n) {
		return false
	}
	return f.descend(n)
}

func (f *flow) descend(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.BranchStmt:
		f.state.opaque = true
		f.barrier()
	case *ast.AssignStmt, *ast.IncDecStmt:
		f.barrier()
	case *ast.ValueSpec:
		if len(n.Values) != 0 {
			f.barrier()
		}
	case *ast.BlockStmt:
		f.stmts(n.List)
		f.walkStmts(n.List)
		return false
	case *ast.CaseClause:
		f.stmts(n.Body)
	case *ast.CommClause:
		f.stmts(n.Body)
	}
	return true
}

// invocation handles calls whose children have a specific execution phase.
func (f *flow) invocation(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.DeferStmt:
		f.deferred(n)
	case *ast.GoStmt:
		f.operands(n.Call)
		if f.onGo != nil {
			f.onGo(n)
		}
	case *ast.FuncLit:
		// A stored literal is visited only by invokeLiteral.
	case *ast.CallExpr:
		f.call(n)
	default:
		return false
	}
	return true
}

// control handles alternatives and exits before ordinary AST descent.
func (f *flow) control(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.IfStmt:
		f.conditional(n)
	case *ast.BinaryExpr:
		if n.Op != token.LAND && n.Op != token.LOR {
			return false
		}
		f.shortCircuit(n)
	case *ast.ReturnStmt:
		for _, result := range n.Results {
			f.walk(result)
		}
		f.state.returnedHeld = f.state.returnedHeld || f.state.held
		f.state.stopped = true
	default:
		return f.barrierControl(n)
	}
	return true
}

func (f *flow) barrierControl(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.ForStmt:
		f.barrier()
		f.loop(n)
	case *ast.RangeStmt:
		f.barrier()
		f.rangeLoop(n)
	case *ast.SwitchStmt:
		f.barrier()
		f.switchStmt(n)
	case *ast.TypeSwitchStmt:
		f.barrier()
		f.walk(n.Init)
		f.walk(n.Assign)
		f.cases(n.Body.List)
	case *ast.SelectStmt:
		f.barrier()
		f.selectStmt(n)
	default:
		return false
	}
	return true
}

func (f *flow) barrier() {
	if f.onBarrier != nil {
		f.onBarrier()
	}
}

func (f *flow) deferred(d *ast.DeferStmt) {
	f.operands(d.Call)
	if f.deferHeld == nil || f.deferHeld(d) {
		f.fork(func() { f.invoke(d.Call) })
	}
}

func (f *flow) stmts(list []ast.Stmt) {
	if f.onStmts != nil {
		f.onStmts(list)
	}
}

func (f *flow) call(call *ast.CallExpr) {
	// Constant builtins such as unsafe.Sizeof do not evaluate their operands.
	if f.c.pass.TypesInfo.Types[call].Value != nil {
		return
	}
	f.operands(call)
	f.invoke(call)
}

func (f *flow) invoke(call *ast.CallExpr) {
	if f.state.stopped {
		return
	}
	if f.onCall != nil {
		f.onCall(call)
	}
	f.invokeLiteral(call.Fun)
	invoked := f.c.invokes[f.c.callee(call)]
	_, _, args := f.c.callOperands(call)
	skip := len(call.Args) - len(args)
	for i, arg := range call.Args {
		if invoked[i-skip] {
			// This fact proves possible invocation, not its path or order.
			f.fork(func() { f.invokeLiteral(arg) })
		}
	}
}

func (f *flow) operands(call *ast.CallExpr) {
	if _, ok := ast.Unparen(call.Fun).(*ast.FuncLit); !ok {
		f.walk(call.Fun)
	}
	for _, arg := range call.Args {
		f.walk(arg)
	}
}
