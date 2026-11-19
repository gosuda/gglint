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
	for node, groups := range comments {
		for _, g := range groups {
			if g != group {
				continue
			}
			if nodeEnd := fset.File(node.End()); nodeEnd != nil {
				if nodeLine := nodeEnd.Line(node.End()); nodeLine > end {
					end = nodeLine
				}
			}
		}
	}
	return start <= line && line <= end
}

// directiveRuleIDs parses a comment group's directive text into the set of
// suppressed rule IDs. It accepts #nosec and //gosec:disable forms, the legacy
// "block" keyword (all rules), and G followed by three digits. Justification
// after "--" is ignored.
func directiveRuleIDs(group *ast.CommentGroup) map[string]bool {
	ids := make(map[string]bool)
	if group == nil {
		return ids
	}
	args := ""
	found := false
	if text := group.Text(); text != "" {
		if idx := strings.Index(text, nosecTag); idx >= 0 {
			found = true
			args = text[idx+len(nosecTag):]
		}
	}
	for _, comment := range group.List {
		if after, ok := strings.CutPrefix(comment.Text, gosecDisable); ok {
			if len(after) == 0 || after[0] == ' ' {
				found = true
				args = strings.TrimSpace(after)
			}
		}
	}
	if !found {
		return ids
	}
	if idx := strings.Index(args, "--"); idx > -1 {
		args = args[:idx]
	}
	directive := strings.TrimSpace(args)
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

