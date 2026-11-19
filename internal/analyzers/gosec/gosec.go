package gosec

import (
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"

	securego "github.com/securego/gosec/v2"
	"github.com/securego/gosec/v2/analyzers"
	"github.com/securego/gosec/v2/issue"
	"github.com/securego/gosec/v2/rules"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/packages"
)

const doc = "gosec reports security problems found by the gosec rules and SSA analyzers.\n\n" +
	"Each finding is printed as <rule ID>: <description>; excluded rule IDs are never run."

func New(excluded []string) *analysis.Analyzer {
	ruleList := rules.Generate(false, rules.NewRuleFilter(true, excluded...))
	analyzerList := analyzers.Generate(false, analyzers.NewAnalyzerFilter(true, excluded...))
	return &analysis.Analyzer{
		Name:     "gosec",
		Doc:      doc,
		Requires: []*analysis.Analyzer{buildssa.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			result, ok := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
			if !ok {
				panic("gosec: missing buildssa result")
			}
			scanner := securego.NewAnalyzer(securego.NewConfig(), true, true, false, 1, log.New(io.Discard, "", 0))
			scanner.LoadRules(ruleList.RulesInfo())
			scanner.LoadAnalyzers(analyzerList.AnalyzersInfo())
			pkg := toPackage(pass)
			scanner.CheckRules(pkg)
			scanner.CheckAnalyzersWithSSA(pkg, result)
			issues, _, errs := scanner.Report()
			if len(errs) > 0 {
				names := slices.Sorted(maps.Keys(errs))
				return nil, fmt.Errorf("gosec: %s: %s", names[0], errs[names[0]][0].Err)
			}
			guarded := guardedArrayIndices(pass.Fset, result, issues)
			if !slices.Contains(excluded, boundRuleID) {
				for _, found := range constLeftViolations(pass.Fset, result, pass.TypesSizes, issues) {
					if !suppressedByDirective(pass, found) {
						reportSupplemental(pass, found)
					}
				}
			}
			for _, iss := range issues {
				if iss.RuleID == "G602" && iss.What == "slice index out of range" &&
					guarded[issueLocation{iss.File, iss.Line, iss.Col}] {
					continue
				}
				report(pass, iss)
			}
			return nil, nil
		},
	}
}

func report(pass *analysis.Pass, iss *issue.Issue) {
	file := fileNamed(pass, iss.File)
	if file == nil {
		pass.Reportf(pass.Files[0].Package, "%s: %s (at %s:%s)", iss.RuleID, iss.What, iss.File, iss.Line)
		return
	}
	pass.Reportf(position(pass, file, iss), "%s: %s", iss.RuleID, iss.What)
}

// reportSupplemental reports a const-left finding at the access position the
// SSA pass recorded, without reconstructing it from line coordinates.
func reportSupplemental(pass *analysis.Pass, found constLeftFinding) {
	pass.Reportf(found.pos, "%s: %s", found.issue.RuleID, found.issue.What)
}

func toPackage(pass *analysis.Pass) *packages.Package {
	names := make([]string, len(pass.Files))
	for i, file := range pass.Files {
		names[i] = pass.Fset.File(file.FileStart).Name()
	}
	return &packages.Package{
		Name:         pass.Pkg.Name(),
		PkgPath:      pass.Pkg.Path(),
		GoFiles:      names,
		OtherFiles:   pass.OtherFiles,
		IgnoredFiles: pass.IgnoredFiles,
		Types:        pass.Pkg,
		Fset:         pass.Fset,
		Syntax:       pass.Files,
		TypesInfo:    pass.TypesInfo,
		TypesSizes:   pass.TypesSizes,
	}
}

func position(pass *analysis.Pass, file *ast.File, iss *issue.Issue) token.Pos {
	tf := pass.Fset.File(file.FileStart)
	firstLine, _, _ := strings.Cut(iss.Line, "-")
	line, err := strconv.Atoi(firstLine)
	if err != nil || line < 1 || line > tf.LineCount() {
		return file.Package
	}
	lineStart := tf.LineStart(line)
	col, err := strconv.Atoi(iss.Col)
	if err != nil || col < 1 {
		return lineStart
	}
	offset := tf.Offset(lineStart) + col - 1
	if offset > tf.Size() {
		return lineStart
	}
	return tf.Pos(offset)
}

func fileNamed(pass *analysis.Pass, name string) *ast.File {
	for _, file := range pass.Files {
		if pass.Fset.File(file.FileStart).Name() == name {
			return file
		}
	}
	return nil
}
