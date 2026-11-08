package vettool

import (
	"bytes"
	"go/ast"
	"go/token"
	"math"
	"slices"
	"strings"
)

type directive struct {
	slash     token.Pos
	end       token.Pos
	tokens    []string
	reason    string
	malformed bool
	first     int
	last      int
	matched   map[string]bool
}

func (d *directive) bare() bool {
	return !d.malformed && len(d.tokens) == 0
}

func (d *directive) covers(line int) bool {
	return d.first <= line && line <= d.last
}

func parseDirective(text string) (directive, bool) {
	body, ok := strings.CutPrefix(text, "//")
	if !ok {
		return directive{}, false
	}
	spaced := strings.TrimLeft(body, " \t")
	rest, ok := strings.CutPrefix(spaced, "nolint")
	if !ok || rest != "" && strings.IndexByte(":\t ", rest[0]) < 0 {
		return directive{}, false
	}
	if spaced != body {
		return directive{malformed: true}, true
	}
	if spec, named := strings.CutPrefix(rest, ":"); named {
		return parseNamed(spec), true
	}
	reason, ok := reasonOf(strings.TrimSpace(rest))
	return directive{reason: reason, malformed: !ok}, true
}

func parseNamed(spec string) directive {
	names, tail := spec, ""
	if i := strings.IndexAny(spec, " \t"); i >= 0 {
		names, tail = spec[:i], spec[i+1:]
	}
	tokens := strings.Split(names, ",")
	reason, ok := reasonOf(strings.TrimSpace(tail))
	if !ok || slices.Contains(tokens, "") {
		return directive{malformed: true}
	}
	return directive{tokens: tokens, reason: reason}
}

func reasonOf(tail string) (reason string, ok bool) {
	if tail == "" {
		return "", true
	}
	rest, ok := strings.CutPrefix(tail, "//")
	return strings.TrimSpace(rest), ok
}

func collectDirectives(fset *token.FileSet, file *ast.File, src []byte) []*directive {
	var out []*directive
	for _, group := range file.Comments {
		for _, c := range group.List {
			d, ok := parseDirective(c.Text)
			if !ok {
				continue
			}
			d.slash, d.end = c.Slash, c.End()
			d.first, d.last = scopeOf(fset, file, src, c)
			d.matched = map[string]bool{}
			out = append(out, &d)
		}
	}
	return out
}

func scopeOf(fset *token.FileSet, file *ast.File, src []byte, c *ast.Comment) (first, last int) {
	line := fset.Position(c.Slash).Line
	if c.Slash < file.Package {
		return 1, math.MaxInt
	}
	if trailing(fset, src, c) {
		return line, line
	}
	next := nextNode(file, c.End())
	if next == nil {
		return line, line
	}
	return fset.Position(next.Pos()).Line, fset.Position(next.End()).Line
}

func trailing(fset *token.FileSet, src []byte, c *ast.Comment) bool {
	tf := fset.File(c.Slash)
	line := tf.Line(c.Slash)
	before := src[tf.Offset(tf.LineStart(line)):tf.Offset(c.Slash)]
	return len(bytes.TrimSpace(before)) > 0
}

func nextNode(file *ast.File, after token.Pos) ast.Node {
	var best ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil || n.End() <= after {
			return false
		}
		if n.Pos() >= after && isScopeNode(n) && better(n, best) {
			best = n
		}
		return true
	})
	return best
}

func isScopeNode(n ast.Node) bool {
	switch n.(type) {
	case ast.Decl, ast.Stmt, ast.Spec, *ast.Field:
		return true
	default:
		return false
	}
}

func better(n, best ast.Node) bool {
	return best == nil || n.Pos() < best.Pos() || (n.Pos() == best.Pos() && n.End() > best.End())
}
