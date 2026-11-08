package rules

import (
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/backingpin"
	"github.com/gosuda/gglint/internal/analyzers/capturerace"
	"github.com/gosuda/gglint/internal/analyzers/chanblock"
	"github.com/gosuda/gglint/internal/analyzers/closer"
	"github.com/gosuda/gglint/internal/analyzers/complexity"
	"github.com/gosuda/gglint/internal/analyzers/goroutine"
	"github.com/gosuda/gglint/internal/analyzers/noinit"
	"github.com/gosuda/gglint/internal/analyzers/placeholder"
	"github.com/gosuda/gglint/internal/analyzers/stackaddr"
	"github.com/gosuda/gglint/internal/analyzers/timeafter"
	"github.com/gosuda/gglint/internal/analyzers/typednil"
	"github.com/gosuda/gglint/internal/analyzers/unsizedint"
	"github.com/gosuda/gglint/internal/config"
)

func gglintRules() []Rule {
	return slices.Concat(basicGglintRules(), standardGglintRules(), strictGglintRules())
}

func basicGglintRules() []Rule {
	return []Rule{
		{
			Name:   "complexity",
			Mode:   config.Basic,
			Origin: originLogos,
			Doc:    "Caps cognitive score, nesting, lines, parameters, and file size; large units exceed what agents edit reliably.",
			Build: func(cfg config.Config) []*analysis.Analyzer {
				return []*analysis.Analyzer{complexity.New(cfg.Complexity)}
			},
		},
		{
			Name:   "typednil",
			Mode:   config.Basic,
			Origin: originLogos,
			Doc:    "Reports a typed nil pointer, slice, map, chan, or func returned as an interface, which is never nil.",
			Build:  single(typednil.Analyzer),
		},
		{
			Name:   "stackaddr",
			Mode:   config.Basic,
			Origin: originLogos,
			Doc:    "Reports the address of a stack variable kept as uintptr outside a call expression.",
			Build:  single(stackaddr.Analyzer),
		},
	}
}

func standardGglintRules() []Rule {
	return []Rule{
		{
			Name:   "capturerace",
			Mode:   config.Standard,
			Origin: originLogos,
			Doc:    "Reports multi-word variables written across a goroutine boundary without synchronization.",
			Build:  single(capturerace.Analyzer),
		},
		{
			Name:   "goroutine",
			Mode:   config.Standard,
			Origin: originLogos,
			Doc:    "Requires joined goroutines with exit paths; strict requires errgroup or WaitGroup.Go and bounded fan-out.",
			Build: func(cfg config.Config) []*analysis.Analyzer {
				return []*analysis.Analyzer{goroutine.New(cfg.Mode)}
			},
		},
		{
			Name:   "timeafter",
			Mode:   config.Standard,
			Origin: originLogos,
			Doc:    "Reports time.After inside a loop, which allocates a timer per iteration.",
			Build:  single(timeafter.Analyzer),
		},
		{
			Name:   "placeholder",
			Mode:   config.Standard,
			Origin: originLogos,
			Doc:    "Reports placeholder panics; strict also reports TODO and FIXME comments.",
			Build: func(cfg config.Config) []*analysis.Analyzer {
				return []*analysis.Analyzer{placeholder.New(cfg.Mode)}
			},
		},
		{
			Name:   "noinit",
			Mode:   config.Standard,
			Origin: originGglint,
			Doc:    "Reports init functions; move setup into an explicit function called from main.",
			Build:  single(noinit.Analyzer),
		},
	}
}

func strictGglintRules() []Rule {
	return []Rule{
		{
			Name:   "chanblock",
			Mode:   config.Strict,
			Origin: originLogos,
			Doc:    "Reports channel sends and receives that can block forever without a ctx.Done() case or buffer proof.",
			Build:  single(chanblock.Analyzer),
		},
		{
			Name:   "closer",
			Mode:   config.Strict,
			Origin: originLogos,
			Doc:    "Reports local io closers that are neither closed with defer nor handed on.",
			Build:  single(closer.Analyzer),
		},
		{
			Name:   "backingpin",
			Mode:   config.Strict,
			Origin: originLogos,
			Doc:    "Reports subslices that keep a backing array of 64 KiB or more alive past the function.",
			Build:  single(backingpin.Analyzer),
		},
		{
			Name:   "unsizedint",
			Mode:   config.Strict,
			Origin: originLogos,
			Doc:    "Reports int and uint fields in JSON structs; their size depends on the platform.",
			Build:  single(unsizedint.Analyzer),
		},
	}
}
