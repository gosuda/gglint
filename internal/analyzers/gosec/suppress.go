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
// suppressed by a #nosec G602 or //gosec:disable G602 directive. It applies the
// same source-suppression rules gosec applies to its own findings: the
// directive must name the rule (a naked directive or one naming another rule
// does not suppress), and the directive's line range must cover the finding.
// Ranges are computed on physical file lines, which is what gosec's own
// ignore ranges use.
func suppressedByDirective(pass *analysis.Pass, found constLeftFinding) bool {
	pos := pass.Fset.Position(found.pos)
	if pos.Filename == "" {
		return false
	}
	for _, file := range pass.Files {
		tf := pass.Fset.File(file.FileStart)
		if tf == nil || tf.Name() != pos.Filename {
			continue
		}
		comments := ast.NewCommentMap(pass.Fset, file, file.Comments)
		for _, group := range file.Comments {
			if !directiveSuppressesGroup(group) {
				continue
			}
			if groupCoversLine(pass.Fset, comments, group, pos.Line) {
				return true
			}
		}
	}
	return false
}

// directiveSuppressesGroup reports whether a comment group carries a directive
// that suppresses G602: #nosec G602, //gosec:disable G602, or the legacy
// "block" keyword. A naked directive or one naming only other rules does not
// suppress.
func directiveSuppressesGroup(group *ast.CommentGroup) bool {
	ids := directiveRuleIDs(group)
	return ids[boundRuleID] || ids[allRules]
}

// groupCoversLine reports whether line falls inside the directive's range: the
// comment group's lines extended through the end of the node the group is
// attached to, mirroring gosec's node-plus-comment span.
func groupCoversLine(fset *token.FileSet, comments ast.CommentMap, group *ast.CommentGroup, line int) bool {
	tf := fset.File(group.Pos())
	if tf == nil {
		return false
	}
	start, end := tf.Line(group.Pos()), tf.Line(group.End())
	if nodeEnd := attachedNodeEnd(fset, comments, group); nodeEnd > end {
		end = nodeEnd
	}
	return start <= line && line <= end
}

// attachedNodeEnd returns the last line of the node the comment group is
// attached to, or 0 when the group maps to no node.
func attachedNodeEnd(fset *token.FileSet, comments ast.CommentMap, group *ast.CommentGroup) int {
	end := 0
	for node, groups := range comments {
		for _, g := range groups {
			if g == group {
				if nodeFile := fset.File(node.End()); nodeFile != nil {
					end = nodeFile.Line(node.End())
				}
			}
		}
	}
	return end
}

// directiveRuleIDs parses a comment group's directive text into the set of
// suppressed rule IDs. It accepts #nosec and //gosec:disable forms, the legacy
// "block" keyword (all rules), and G followed by three digits. Justification
// after "--" is ignored.
func directiveRuleIDs(group *ast.CommentGroup) map[string]bool {
	ids := make(map[string]bool)
	directive := directiveText(group)
	if directive == "" {
		return ids
	}
	if directive == legacyBlock {
		ids[allRules] = true
		return ids
	}
	for i := 0; i+4 <= len(directive); i++ {
		if directive[i] != 'G' {
			continue
		}
		candidate := directive[i : i+4]
		if validRuleID(candidate) {
			ids[candidate] = true
			i += 3
		}
	}
	return ids
}

// nosecArgs returns the argument of a #nosec directive in the group's text.
func nosecArgs(group *ast.CommentGroup) (string, bool) {
	text := group.Text()
	if text == "" {
		return "", false
	}
	_, after, ok := strings.Cut(text, nosecTag)
	if !ok {
		return "", false
	}
	return after, true
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

// directiveText returns the trimmed directive argument of a comment group, or
// "" when the group carries no #nosec or //gosec:disable directive.
func directiveText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	args, found := nosecArgs(group)
	if !found {
		args, found = disableArgs(group)
	}
	if !found {
		return ""
	}
	if idx := strings.Index(args, "--"); idx > -1 {
		args = args[:idx]
	}
	return strings.TrimSpace(args)
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
