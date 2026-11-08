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
		{
			Name:   "checklocks",
			Mode:   config.Basic,
			Origin: originGVisor,
			Doc:    "Checks +checklocks and +checkatomic annotations and reports double acquisition of a held lock.",
			Build:  buildChecklocks,
		},
		{
			Name:   "checkescape",
			Mode:   config.Basic,
			Origin: originGVisor,
			Doc:    "Checks +checkescape and +mustescape annotations; unannotated callees read as unknown, failing only :all and :unknown.",
			Build: func(cfg config.Config) []*analysis.Analyzer {
				return []*analysis.Analyzer{checkescape.New(checkescape.ListExport(cfg.BuildTags))}
			},
		},
		{
			Name:   "checkconst",
			Mode:   config.Basic,
			Origin: originGVisor,
			Doc:    "Checks +checkconst, +checkalign, +checkoffset, and +checksize against assembly constants.",
			Build:  single(checkconst.Analyzer),
		},
		{
			Name:   "checklinkname",
			Mode:   config.Basic,
			Origin: originGVisor,
			Doc:    "Validates //go:linkname directives against the signatures of their targets.",
			Build:  single(checklinkname.Analyzer),
		},
		{
			Name:   "checkaligned",
			Mode:   config.Standard,
			Origin: originGVisor,
			Doc:    "Bans sync/atomic 32-bit and 64-bit function calls; use the typed atomics.",
			Build:  single(checkaligned.Analyzer),
		},
		{
			Name:   "checkimports",
			Mode:   config.Strict,
			Origin: originGVisor,
			Doc:    "Restricts unsafe to *_unsafe.go files and enforces the configured import deny list.",
			Build: func(cfg config.Config) []*analysis.Analyzer {
				return []*analysis.Analyzer{checkimports.New(cfg.Deny)}
			},
		},
	}
}

func buildChecklocks(cfg config.Config) []*analysis.Analyzer {
	flags := map[string]string{
		"atomic":   flagTrue,
		"wrappers": flagTrue,
		"inferred": strconv.FormatBool(cfg.Mode >= config.Strict),
	}
	for name, value := range flags {
		if err := checklocks.Analyzer.Flags.Set(name, value); err != nil {
			panic("rules: checklocks: " + err.Error())
		}
	}
	return []*analysis.Analyzer{checklocks.Analyzer}
}

func single(a *analysis.Analyzer) func(config.Config) []*analysis.Analyzer {
	return func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{a} }
}
