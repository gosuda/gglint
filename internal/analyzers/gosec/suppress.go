package gosec

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// These mirror the gosec defaults for #nosec and //gosec:disable.
const (
	nosecTag     = "#nosec"
	gosecDisable = "//gosec:disable"
	legacyBlock  = "block"
	allRules     = "*"
	boundRuleID  = "G602"
	boundWhat    = "slice index out of range"
)

// suppressedByDirective reports whether a supplemental G602 finding is
// suppressed by a #nosec or //gosec:disable directive. It mirrors the
// suppression rules gosec applies to its own findings: comments attach to
// nodes through the file's comment map, the first recognized directive on a
// node is decisive, a directive without rule IDs suppresses every rule, and
// the node-plus-comment span covers the finding's line. Filenames are the
// token files' physical names and lines are //line-adjusted, the same
// pairing upstream's ignore ranges and issue locations use.
func suppressedByDirective(pass *analysis.Pass, found constLeftFinding) bool {
	tf := pass.Fset.File(found.pos)
	if tf == nil {
		return false
	}
	filename := tf.Name()
	line := tf.Position(found.pos).Line
	for _, file := range pass.Files {
		ff := pass.Fset.File(file.FileStart)
		if ff == nil || ff.Name() != filename {
			continue
		}
		comments := ast.NewCommentMap(pass.Fset, file, file.Comments)
		for node, groups := range comments {
			if nodeSuppressesG602(pass.Fset, node, groups, line) {
				return true
			}
		}
	}
	return false
}

// nodeSuppressesG602 mirrors gosec's per-node ignore: only the first comment
// group carrying a recognized directive decides. When that directive names
// G602 or suppresses every rule, the finding is suppressed if its line falls
// inside the span covering both the node and the comment group; a directive
// naming only other rules leaves the finding reported, as upstream leaves it.
func nodeSuppressesG602(fset *token.FileSet, node ast.Node, groups []*ast.CommentGroup, line int) bool {
	for _, group := range groups {
		ids, found := directiveIDs(group)
		if !found {
			continue
		}
		if !ids[boundRuleID] && !ids[allRules] {
			return false
		}
		return directiveSpanCoversLine(fset, node, group, line)
	}
	return false
}

// directiveSpanCoversLine reports whether line falls inside the directive's
// range: min(node.Pos, group.Pos) through max(node.End, group.End) in
// //line-adjusted lines, the span gosec stores for each ignored node. The
// adjusted lines are compared unordered because a //line directive inside a
// remapped region can invert them; upstream's range containment matches a
// finding either way around.
func directiveSpanCoversLine(fset *token.FileSet, node ast.Node, group *ast.CommentGroup, line int) bool {
	start, end := node.Pos(), node.End()
	if group.Pos() < start {
		start = group.Pos()
	}
	if group.End() > end {
		end = group.End()
	}
	sf, ef := fset.File(start), fset.File(end)
	if sf == nil || ef == nil {
		return false
	}
	lo, hi := sf.Line(start), ef.Line(end)
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo <= line && line <= hi
}

// directiveIDs parses a comment group's directive into the rule IDs it
// suppresses and whether the group carries a directive at all. A directive
// that is empty, the legacy "block" keyword, or text with no G followed by
// three digits suppresses every rule, matching upstream's naked case.
func directiveIDs(group *ast.CommentGroup) (map[string]bool, bool) {
	ids := make(map[string]bool)
	directive, found := directiveText(group)
	if !found {
		return nil, false
	}
	if directive != "" && directive != legacyBlock {
		for i := 0; i+4 <= len(directive); i++ {
			if directive[i] != 'G' {
				continue
			}
			if candidate := directive[i : i+4]; validRuleID(candidate) {
				ids[candidate] = true
				i += 3
			}
		}
	}
	if len(ids) == 0 {
		ids[allRules] = true
	}
	return ids, true
}

// nosecArgs returns the argument of a #nosec directive in the group's text.
// The tag must start the comment text or begin a line inside it, possibly
// after whitespace; a mid-sentence mention is not a directive, matching
// gosec's findNoSecTag.
func nosecArgs(group *ast.CommentGroup) (string, bool) {
	text := strings.TrimSpace(group.Text())
	if text == "" {
		return "", false
	}
	if strings.HasPrefix(text, nosecTag) {
		return text[len(nosecTag):], true
	}
	if idx := strings.Index(text, nosecTag); idx > 0 {
		for i := idx - 1; i >= 0; i-- {
			if text[i] == '\n' {
				return text[idx+len(nosecTag):], true
			}
			if text[i] != ' ' && text[i] != '\t' {
				break
			}
		}
	}
	return "", false
}

// disableArgs returns the argument of a //gosec:disable comment in the group.
func disableArgs(group *ast.CommentGroup) (string, bool) {
	for _, comment := range group.List {
		after, ok := strings.CutPrefix(comment.Text, gosecDisable)
		if ok && (after == "" || after[0] == ' ') {
			return strings.TrimSpace(after), true
		}
	}
	return "", false
}

// directiveText returns the trimmed directive argument of a comment group and
// whether the group carries a #nosec or //gosec:disable directive at all. An
// empty argument is a naked directive, which gosec treats as suppressing
// every rule. Justification after "--" is ignored.
func directiveText(group *ast.CommentGroup) (string, bool) {
	if group == nil {
		return "", false
	}
	args, found := nosecArgs(group)
	if !found {
		args, found = disableArgs(group)
	}
	if !found {
		return "", false
	}
	if idx := strings.Index(args, "--"); idx > -1 {
		args = args[:idx]
	}
	return strings.TrimSpace(args), true
}

// validRuleID reports whether s has the G followed by three digits form.
func validRuleID(s string) bool {
	if len(s) != 4 || s[0] != 'G' {
		return false
	}
	for i := 1; i < 4; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
