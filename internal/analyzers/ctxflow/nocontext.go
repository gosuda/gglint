package ctxflow

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/gosuda/gglint/internal/inspection"
)

const httpDoReplacement = "http.NewRequestWithContext and (*http.Client).Do"

var nocontextAnalyzer = &analysis.Analyzer{
	Name:     "nocontext",
	Doc:      "report calls that cannot be cancelled where a context is in scope",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runNocontext,
}

func runNocontext(pass *analysis.Pass) (any, error) {
	for cur := range inspection.Of(pass).Root().Preorder((*ast.CallExpr)(nil)) {
		checkNocontext(pass, cur)
	}
	return nil, nil
}

func checkNocontext(pass *analysis.Pass, cur inspector.Cursor) {
	call, ok := cur.Node().(*ast.CallExpr)
	if !ok {
		return
	}
	left, right, ok := nocontextTarget(pass.TypesInfo, call)
	if !ok || !contextInScope(pass, cur) {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: "a context is in scope and " + left + " cannot be cancelled: use " + right,
	})
}

func nocontextTarget(info *types.Info, call *ast.CallExpr) (left, right string, found bool) {
	fn := typeutil.StaticCallee(info, call)
	if fn == nil {
		return "", "", false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return "", "", false
	}
	if recv := sig.Recv(); recv != nil {
		return nocontextMethod(fn, recv)
	}
	return nocontextFunc(fn)
}

func nocontextFunc(fn *types.Func) (left, right string, found bool) {
	pkg := fn.Pkg()
	if pkg == nil {
		return "", "", false
	}
	switch pkg.Path() {
	case "net":
		return netTarget(fn.Name())
	case netHTTPPath:
		return httpFuncTarget(fn.Name())
	case "os/exec":
		if fn.Name() == "Command" {
			return "exec.Command", "exec.CommandContext", true
		}
	}
	return "", "", false
}

func netTarget(name string) (left, right string, found bool) {
	switch name {
	case "Dial":
		return "net.Dial", "(&net.Dialer{}).DialContext", true
	case "Listen":
		return "net.Listen", "(&net.ListenConfig{}).Listen", true
	}
	return "", "", false
}

func httpFuncTarget(name string) (left, right string, found bool) {
	switch name {
	case "Get", "Head", "Post", "PostForm":
		return "http." + name, httpDoReplacement, true
	case "NewRequest":
		return "http.NewRequest", "http.NewRequestWithContext", true
	}
	return "", "", false
}

func nocontextMethod(fn *types.Func, recv *types.Var) (left, right string, found bool) {
	t := recv.Type()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return "", "", false
	}
	obj := named.Obj()
	pkg := obj.Pkg()
	if pkg == nil {
		return "", "", false
	}
	recvName := "(*" + pkg.Name() + "." + obj.Name() + ")."
	switch pkg.Path() {
	case netHTTPPath:
		return httpClientTarget(recvName, obj.Name(), fn.Name())
	case "database/sql":
		return sqlTarget(recvName, obj.Name(), fn.Name())
	}
	return "", "", false
}

func httpClientTarget(recvName, typeName, method string) (left, right string, found bool) {
	if typeName != "Client" {
		return "", "", false
	}
	switch method {
	case "Get", "Head", "Post", "PostForm":
		return recvName + method, httpDoReplacement, true
	}
	return "", "", false
}

func sqlTarget(recvName, typeName, method string) (left, right string, found bool) {
	switch typeName {
	case "DB":
		return sqlDBTarget(recvName, method)
	case "Tx":
		switch method {
		case "Query", "QueryRow", "Exec", "Prepare":
			return recvName + method, recvName + method + "Context", true
		}
	}
	return "", "", false
}

func sqlDBTarget(recvName, method string) (left, right string, found bool) {
	switch method {
	case "Query", "QueryRow", "Exec", "Prepare", "Ping":
		return recvName + method, recvName + method + "Context", true
	case "Begin":
		return recvName + "Begin", recvName + "BeginTx", true
	}
	return "", "", false
}
