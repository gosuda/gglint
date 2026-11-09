package structtagmalformed

import (
	"errors"
	"fmt"
	"go/ast"
	"reflect"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/gosuda/gglint/internal/inspection"
)

const (
	malformedMessage  = "struct tag %#q cannot be parsed by reflect.StructTag.Get: %v"
	unexportedMessage = "unexported field %s has tag key %s: encoding/%s ignores unexported fields"
	badLiteral        = "bad syntax"
	badPair           = "bad syntax for struct tag pair"
	badKey            = "bad syntax for struct tag key"
	badValue          = "bad syntax for struct tag value"
	missingSpace      = `key:"value" pairs not separated by spaces`
	duplicateKey      = "duplicate key %q in struct tag"
)

var Analyzer = &analysis.Analyzer{
	Name: "structtag_malformed",
	Doc: `report malformed struct tags and json or xml tags on unexported fields

A tag must be a sequence of space-separated key:"value" pairs with
non-empty keys, quoted values, and no repeated key. A field whose name is
unexported and whose json or xml tag value is neither empty nor "-" carries
a tag the encoder never reads.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	structs := []ast.Node{(*ast.StructType)(nil)}
	inspection.Of(pass).Preorder(structs, func(n ast.Node) {
		for _, field := range inspection.As[*ast.StructType](n).Fields.List {
			if field.Tag != nil {
				checkField(pass, field)
			}
		}
	})
	return nil, nil
}

func checkField(pass *analysis.Pass, field *ast.Field) {
	lit := field.Tag
	tag, err := strconv.Unquote(lit.Value)
	if err != nil {
		pass.ReportRangef(lit, malformedMessage, lit.Value, badLiteral)
		return
	}
	if err := validate(tag); err != nil {
		pass.ReportRangef(lit, malformedMessage, tag, err)
		return
	}
	for _, name := range field.Names {
		if !name.IsExported() {
			checkUnexported(pass, lit, name.Name, reflect.StructTag(tag))
		}
	}
}

func checkUnexported(pass *analysis.Pass, lit *ast.BasicLit, name string, tag reflect.StructTag) {
	for _, enc := range [...]string{"json", "xml"} {
		if v := tag.Get(enc); v != "" && v != "-" {
			pass.ReportRangef(lit, unexportedMessage, name, enc, enc)
		}
	}
}

func validate(tag string) error {
	seen := make(map[string]bool)
	tag = strings.TrimLeft(tag, " ")
	for tag != "" {
		rest, err := nextPair(tag, seen)
		if err != nil {
			return err
		}
		if rest != "" && rest[0] != ' ' {
			return errors.New(missingSpace)
		}
		tag = strings.TrimLeft(rest, " ")
	}
	return nil
}

func nextPair(tag string, seen map[string]bool) (string, error) {
	i := keyEnd(tag)
	switch {
	case i == 0:
		return "", errors.New(badKey)
	case i+1 >= len(tag) || tag[i] != ':':
		return "", errors.New(badPair)
	case tag[i+1] != '"':
		return "", errors.New(badValue)
	}
	key := tag[:i]
	if seen[key] {
		return "", fmt.Errorf(duplicateKey, key)
	}
	seen[key] = true
	return nextValue(tag[i+1:])
}

func keyEnd(tag string) int {
	i := 0
	for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
		i++
	}
	return i
}

func nextValue(tag string) (string, error) {
	i := 1
	for i < len(tag) && tag[i] != '"' {
		if tag[i] == '\\' {
			i++
		}
		i++
	}
	if i >= len(tag) {
		return "", errors.New(badValue)
	}
	if _, err := strconv.Unquote(tag[:i+1]); err != nil {
		return "", errors.New(badValue)
	}
	return tag[i+1:], nil
}
