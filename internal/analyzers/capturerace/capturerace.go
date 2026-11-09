package capturerace

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const (
	parentWritesFormat = "%s is written after a goroutine that captures it starts: pass it as an argument or synchronize first"
	childWritesFormat  = "%s is written by a goroutine and read here without synchronization: wait for the goroutine first"
	childOnlyFormat    = "%s is written by a goroutine that captures it: pass it as an argument or synchronize first"
	errgroupGroup      = "golang.org/x/sync/errgroup.Group"
	syncWaitGroup      = "sync.WaitGroup"
)

var Analyzer = &analysis.Analyzer{
	Name: "capturerace",
	Doc: "Report multi-word locals that a goroutine captures while either side writes them.\n\n" +
		"A string, slice, interface, or a complex number, struct, or array whose value bytes exceed one word " +
		"is not written atomically, so a goroutine that captures it can observe a torn value. " +
		"The rule reports a spawn site (a go statement, errgroup.Group.Go or TryGo, sync.WaitGroup.Go) " +
		"when such a captured local is written anywhere from the spawn site to the end of the enclosing " +
		"top-level declaration, the goroutine's own body included. Order is source order; " +
		"joins such as Wait are not modelled. Types from sync and sync/atomic are exempt.",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

type spawn struct {
	site ast.Node
	body *ast.FuncLit
}

type access struct {
	v    *types.Var
	expr ast.Expr
}

type declScan struct {
	end    token.Pos
	spawns []spawn
	writes []access
	refs   []access
}

func run(pass *analysis.Pass) (any, error) {
	insp := inspection.Of(pass)
	for file := range insp.Root().Children() {
		for decl := range file.Children() {
			if _, ok := decl.Node().(ast.Decl); ok {
				scanDecl(pass.TypesInfo, decl).report(pass)
			}
		}
	}
	return nil, nil
}

func scanDecl(info *types.Info, decl inspector.Cursor) *declScan {
	d := &declScan{end: decl.Node().End()}
	kinds := []ast.Node{
		(*ast.GoStmt)(nil), (*ast.CallExpr)(nil), (*ast.AssignStmt)(nil),
		(*ast.IncDecStmt)(nil), (*ast.RangeStmt)(nil), (*ast.Ident)(nil),
	}
	for c := range decl.Preorder(kinds...) {
		d.note(info, c.Node())
	}
	return d
}

func (d *declScan) note(info *types.Info, node ast.Node) {
	switch n := node.(type) {
	case *ast.GoStmt:
		d.addSpawn(n, n.Call.Fun)
	case *ast.CallExpr:
		if isSpawnCall(info, n) {
			d.addSpawn(n, n.Args[0])
		}
	case *ast.AssignStmt:
		d.addWrites(info, n.Lhs...)
	case *ast.IncDecStmt:
		d.addWrites(info, n.X)
	case *ast.RangeStmt:
		if n.Tok == token.ASSIGN {
			d.addWrites(info, n.Key, n.Value)
		}
	case *ast.Ident:
		d.addRef(info, n)
	}
}

func (d *declScan) addRef(info *types.Info, id *ast.Ident) {
	if v, ok := info.Uses[id].(*types.Var); ok {
		d.refs = append(d.refs, access{v: v, expr: id})
	}
}

func (d *declScan) addSpawn(site ast.Node, fun ast.Expr) {
	if lit, ok := ast.Unparen(fun).(*ast.FuncLit); ok {
		d.spawns = append(d.spawns, spawn{site: site, body: lit})
	}
}

func (d *declScan) addWrites(info *types.Info, targets ...ast.Expr) {
	for _, t := range targets {
		if v := writtenVar(info, t); v != nil {
			d.writes = append(d.writes, access{v: v, expr: t})
		}
	}
}

func writtenVar(info *types.Info, e ast.Expr) *types.Var {
	for e != nil {
		if id, ok := e.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types.Var); ok {
				return v
			}
			return nil
		}
		e = inPlaceOperand(info, e)
	}
	return nil
}

func inPlaceOperand(info *types.Info, e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return x.X
	case *ast.SelectorExpr:
		if sel := info.Selections[x]; sel != nil && sel.Kind() == types.FieldVal && !sel.Indirect() {
			return x.X
		}
	case *ast.IndexExpr:
		if _, ok := info.TypeOf(x.X).Underlying().(*types.Array); ok {
			return x.X
		}
	}
	return nil
}

func isSpawnCall(info *types.Info, call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil || len(call.Args) != 1 {
		return false
	}
	recv := fn.Signature().Recv()
	if recv == nil {
		return false
	}
	switch typeName(recv.Type()) {
	case errgroupGroup:
		return fn.Name() == "Go" || fn.Name() == "TryGo"
	case syncWaitGroup:
		return fn.Name() == "Go"
	}
	return false
}

func typeName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name()
}

func (d *declScan) report(pass *analysis.Pass) {
	for _, s := range d.spawns {
		for _, v := range d.captured(pass.Pkg, s.body) {
			if multiWord(v.Type(), pass.TypesSizes) {
				d.check(pass, s, v)
			}
		}
	}
}

func (d *declScan) captured(pkg *types.Package, body *ast.FuncLit) []*types.Var {
	var out []*types.Var
	for _, r := range d.refs {
		if within(r.expr.Pos(), body) && isOuterLocal(pkg, r.v, body) && !slices.Contains(out, r.v) {
			out = append(out, r.v)
		}
	}
	return out
}

func isOuterLocal(pkg *types.Package, v *types.Var, body *ast.FuncLit) bool {
	return v.Pkg() == pkg && v.Parent() != nil && v.Parent() != pkg.Scope() && !within(v.Pos(), body)
}

func within(p token.Pos, n ast.Node) bool {
	return n.Pos() <= p && p < n.End()
}

func (d *declScan) check(pass *analysis.Pass, s spawn, v *types.Var) {
	w, ok := d.windowWrite(s, v)
	if !ok {
		return
	}
	if !within(w.expr.Pos(), s.body) {
		reportAt(pass, s, parentWritesFormat, v, w, "written here")
		return
	}
	if r, ok := d.laterRef(s, v); ok {
		reportAt(pass, s, childWritesFormat, v, r, "read here")
		return
	}
	reportAt(pass, s, childOnlyFormat, v, w, "written here")
}

func (d *declScan) windowWrite(s spawn, v *types.Var) (access, bool) {
	var first access
	found := false
	for _, w := range d.writes {
		if w.v != v || w.expr.Pos() < s.site.Pos() || w.expr.Pos() >= d.end {
			continue
		}
		if !within(w.expr.Pos(), s.body) {
			return w, true
		}
		if !found {
			first, found = w, true
		}
	}
	return first, found
}

func (d *declScan) laterRef(s spawn, v *types.Var) (access, bool) {
	for _, r := range d.refs {
		if r.v == v && r.expr.Pos() >= s.site.End() && r.expr.Pos() < d.end {
			return r, true
		}
	}
	return access{}, false
}

func reportAt(pass *analysis.Pass, s spawn, format string, v *types.Var, at access, note string) {
	pass.Report(analysis.Diagnostic{
		Pos:     s.site.Pos(),
		Message: fmt.Sprintf(format, v.Name()),
		Related: []analysis.RelatedInformation{{Pos: at.expr.Pos(), End: at.expr.End(), Message: v.Name() + " " + note}},
	})
}

func multiWord(t types.Type, sizes types.Sizes) bool {
	return !isSyncType(t) && valueBytes(t, sizes) > wordSize(sizes)
}

func isSyncType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	path := named.Obj().Pkg().Path()
	return path == "sync" || path == "sync/atomic"
}

func wordSize(sizes types.Sizes) int64 {
	return sizes.Sizeof(types.Typ[types.Int])
}

func valueBytes(t types.Type, sizes types.Sizes) int64 {
	if _, ok := types.Unalias(t).(*types.TypeParam); ok {
		return wordSize(sizes)
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return structBytes(u, sizes)
	case *types.Array:
		return arrayBytes(u, sizes)
	}
	return sizes.Sizeof(t)
}

func structBytes(s *types.Struct, sizes types.Sizes) int64 {
	total := int64(0)
	for field := range s.Fields() {
		total += valueBytes(field.Type(), sizes)
	}
	return total
}

func arrayBytes(a *types.Array, sizes types.Sizes) int64 {
	elem := valueBytes(a.Elem(), sizes)
	ceiling := 2 * wordSize(sizes)
	if elem == 0 {
		return 0
	}
	if a.Len() > ceiling/elem {
		return ceiling
	}
	return a.Len() * elem
}
