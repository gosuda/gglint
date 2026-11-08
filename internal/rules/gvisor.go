package rules

import (
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/checkaligned"
	"github.com/gosuda/gglint/internal/analyzers/checkconst"
	"github.com/gosuda/gglint/internal/analyzers/checkescape"
	"github.com/gosuda/gglint/internal/analyzers/checkimports"
	"github.com/gosuda/gglint/internal/analyzers/checklinkname"
	"github.com/gosuda/gglint/internal/analyzers/checklocks"
	"github.com/gosuda/gglint/internal/config"
)

func gvisorRules() []Rule {
	return []Rule{
		{"checklocks", config.Basic, originGVisor, "Checks +checklocks and +checkatomic annotations and reports double acquisition of a held lock.", buildChecklocks},
		{"checkescape", config.Basic, originGVisor, "Checks +checkescape and +mustescape annotations; unannotated callees read as unknown, failing only :all and :unknown.", func(cfg config.Config) []*analysis.Analyzer {
			return []*analysis.Analyzer{checkescape.New(checkescape.ListExport(cfg.BuildTags))}
		}},
		{"checkconst", config.Basic, originGVisor, "Checks +checkconst, +checkalign, +checkoffset, and +checksize against assembly constants.", single(checkconst.Analyzer)},
		{"checklinkname", config.Basic, originGVisor, "Validates //go:linkname directives against the signatures of their targets.", single(checklinkname.Analyzer)},
		{"checkaligned", config.Standard, originGVisor, "Bans sync/atomic 32-bit and 64-bit function calls; use the typed atomics.", single(checkaligned.Analyzer)},
		{"checkimports", config.Strict, originGVisor, "Restricts unsafe to *_unsafe.go files and enforces the configured import deny list.", func(cfg config.Config) []*analysis.Analyzer {
			return []*analysis.Analyzer{checkimports.New(cfg.Deny)}
		}},
	}
}

func buildChecklocks(cfg config.Config) []*analysis.Analyzer {
	flags := map[string]string{
		"atomic":   strconv.FormatBool(true),
		"wrappers": strconv.FormatBool(true),
		"inferred": strconv.FormatBool(cfg.Mode >= config.Strict),
	}
	return []*analysis.Analyzer{setFlags("checklocks", checklocks.Analyzer, flags)}
}

func single(a *analysis.Analyzer) func(config.Config) []*analysis.Analyzer {
	return func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{a} }
}
