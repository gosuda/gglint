package rules

import (
	"strconv"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/checkconst"
	"github.com/gosuda/gglint/internal/analyzers/checkescape"
	"github.com/gosuda/gglint/internal/analyzers/checkimports"
	"github.com/gosuda/gglint/internal/analyzers/checklinkname"
	"github.com/gosuda/gglint/internal/analyzers/checklocks"
	"github.com/gosuda/gglint/internal/config"
)

var checklocksRuns sync.Mutex

func gvisorRules() []Rule {
	return []Rule{
		{"checklocks", config.Strict, originGVisor, "Checks +checklocks and +checkatomic annotations and reports double acquisition of a held lock.", ReportEverywhere, buildChecklocks},
		{"checkescape", config.Basic, originGVisor, "Checks +checkescape and +mustescape annotations; unannotated callees read as unknown, failing only :all and :unknown.", ReportEverywhere, func(cfg config.Config) []*analysis.Analyzer {
			return []*analysis.Analyzer{checkescape.New(checkescape.ListExport(cfg.BuildTags))}
		}},
		{"checkconst", config.Basic, originGVisor, "Checks +checkconst, +checkalign, +checkoffset, and +checksize against assembly constants.", ReportEverywhere, single(checkconst.Analyzer)},
		{"checklinkname", config.Basic, originGVisor, "Validates //go:linkname directives against the signatures of their targets.", ReportEverywhere, single(checklinkname.Analyzer)},
		{"checkimports", config.Basic, originGVisor, "Enforces the configured import deny list.", ReportEverywhere, func(cfg config.Config) []*analysis.Analyzer {
			return []*analysis.Analyzer{checkimports.New(cfg.Deny)}
		}},
	}
}

func buildChecklocks(cfg config.Config) []*analysis.Analyzer {
	checklocksGlobal := globalAnalyzer{"checklocks", checklocks.Analyzer, &checklocksRuns}
	return []*analysis.Analyzer{checklocksGlobal.build(map[string]string{
		"atomic":   strconv.FormatBool(true),
		"wrappers": strconv.FormatBool(true),
		"inferred": strconv.FormatBool(cfg.Mode >= config.Strict),
	})}
}

func single(a *analysis.Analyzer) func(config.Config) []*analysis.Analyzer {
	return func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{a} }
}
