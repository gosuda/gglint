package flow

import (
	"cmp"
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// HeldAt returns, for a node, the mutex keys definitely held before it.
func HeldAt(pass *analysis.Pass, g *Graph) func(n ast.Node) []Key {
	none := func(ast.Node) []Key { return nil }
	if pass == nil || pass.TypesInfo == nil || g == nil {
		return none
	}
	info := pass.TypesInfo
	keys := mutexKeys(info, g.fn)
	held := foldKeys(info, g, keys)
	sortHeld(held)
	return func(n ast.Node) []Key {
		r := g.locate(n)
		if r == nil {
			return nil
		}
		return slices.Clone(held[r])
	}
}

func foldKeys(info *types.Info, g *Graph, keys []Key) map[ast.Node][]Key {
	held := map[ast.Node][]Key{}
	for _, k := range keys {
		Forward(g, false,
			func(n ast.Node, h bool) bool { return lockTransfer(info, n, k, h) },
			func(a, b bool) bool { return a && b },
			func(n ast.Node, h bool) {
				if h {
					held[n] = append(held[n], k)
				}
			})
	}
	return held
}

func sortHeld(held map[ast.Node][]Key) {
	for _, s := range held {
		slices.SortFunc(s, func(a, b Key) int {
			if c := cmp.Compare(a.Root.Pos(), b.Root.Pos()); c != 0 {
				return c
			}
			return cmp.Compare(a.Path, b.Path)
		})
	}
}

func mutexKeys(info *types.Info, fn ast.Node) []Key {
	var keys []Key
	seen := map[Key]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return n == fn
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || mutexMethod(typeutil.StaticCallee(info, call)) == "" {
			return true
		}
		if k, ok := MethodKey(info, call); ok && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		return true
	})
	return keys
}

func lockTransfer(info *types.Info, n ast.Node, k Key, h bool) bool {
	out := h
	ast.Inspect(n, func(x ast.Node) bool {
		switch x.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		m := mutexMethod(typeutil.StaticCallee(info, call))
		if m == "" {
			return true
		}
		ck, ok := MethodKey(info, call)
		if !ok || ck != k {
			return true
		}
		out = m == "Lock" || m == "RLock"
		return true
	})
	return out
}

func mutexMethod(fn *types.Func) string {
	if fn == nil {
		return ""
	}
	switch fn.FullName() {
	case "(*sync.Mutex).Lock", "(*sync.RWMutex).Lock":
		return "Lock"
	case "(*sync.RWMutex).RLock":
		return "RLock"
	case "(*sync.Mutex).Unlock", "(*sync.RWMutex).Unlock":
		return "Unlock"
	case "(*sync.RWMutex).RUnlock":
		return "RUnlock"
	case "(*sync.Mutex).TryLock", "(*sync.RWMutex).TryLock":
		return "TryLock"
	case "(*sync.RWMutex).TryRLock":
		return "TryRLock"
	}
	return ""
}
