package unsizedint

import (
	"go/ast"
	"go/types"
	"reflect"
	"slices"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const message = "field %s has platform-dependent type %s in a JSON struct: use int64 or int32"

var Analyzer = &analysis.Analyzer{
	Name: "unsizedint",
	Doc: "Report int and uint fields in structs serialized as JSON.\n\n" +
		"In a struct type where at least one field tag has a json key, every field of the predeclared " +
		"type int or uint is reported, because its width depends on the target platform. " +
		"Defined types over int, pointers, and slices are other types and are not reported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	for c := range insp.Root().Preorder((*ast.StructType)(nil)) {
		fields := c.Node().(*ast.StructType).Fields.List
		if slices.ContainsFunc(fields, hasJSONTag) {
			reportUnsized(pass, fields)
		}
	}
	return nil, nil
}

func hasJSONTag(f *ast.Field) bool {
	if f.Tag == nil {
		return false
	}
	tag, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return false
	}
	_, ok := reflect.StructTag(tag).Lookup("json")
	return ok
}

func reportUnsized(pass *analysis.Pass, fields []*ast.Field) {
	for _, f := range fields {
		if typeName := unsizedName(pass.TypesInfo.TypeOf(f.Type)); typeName != "" {
			reportField(pass, f, typeName)
		}
	}
}

func reportField(pass *analysis.Pass, f *ast.Field, typeName string) {
	if len(f.Names) == 0 {
		pass.Reportf(f.Type.Pos(), message, typeName, typeName)
		return
	}
	for _, name := range f.Names {
		pass.Reportf(name.Pos(), message, name.Name, typeName)
	}
}

func unsizedName(t types.Type) string {
	if t == nil {
		return ""
	}
	basic, ok := types.Unalias(t).(*types.Basic)
	if !ok || (basic.Kind() != types.Int && basic.Kind() != types.Uint) {
		return ""
	}
	return basic.Name()
}
