package rules

import (
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/backingpin"
	"github.com/gosuda/gglint/internal/analyzers/capturerace"
	"github.com/gosuda/gglint/internal/analyzers/chanblock"
	"github.com/gosuda/gglint/internal/analyzers/closer"
	"github.com/gosuda/gglint/internal/analyzers/complexity"
	"github.com/gosuda/gglint/internal/analyzers/discardederror"
	"github.com/gosuda/gglint/internal/analyzers/errchainreset"
	"github.com/gosuda/gglint/internal/analyzers/goroutine"
	"github.com/gosuda/gglint/internal/analyzers/infiniterecursion"
	"github.com/gosuda/gglint/internal/analyzers/noinit"
	"github.com/gosuda/gglint/internal/analyzers/placeholder"
	"github.com/gosuda/gglint/internal/analyzers/reentrantlock"
	"github.com/gosuda/gglint/internal/analyzers/stackaddr"
	"github.com/gosuda/gglint/internal/analyzers/structtagmalformed"
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
			Doc:    "Reports a typed nil pointer returned as an interface, which is never nil.",
			Build:  single(typednil.Analyzer),
		},
		{
			Name:   "stackaddr",
			Mode:   config.Basic,
			Origin: originLogos,
			Doc:    "Reports a stack address kept as uintptr, or an unsafe.Pointer to stack storage that leaves its function.",
			Build:  single(stackaddr.Analyzer),
		},
		{
			Name:   "infinite_recursion",
			Mode:   config.Basic,
			Origin: originLogos,
			Doc:    "Reports functions that call themselves with their own arguments on every path before any return.",
			Build:  single(infiniterecursion.Analyzer),
		},
		{
			Name:   "structtag_malformed",
			Mode:   config.Basic,
			Origin: originLogos,
			Doc:    "Reports struct tags reflect.StructTag.Get cannot parse and json or xml tags on unexported fields.",
			Build:  single(structtagmalformed.Analyzer),
		},
		{
			Name:   "reentrant_withlock_deadlock",
			Mode:   config.Basic,
			Origin: originLogos,
			Doc:    "Reports locking a sync mutex the goroutine already holds, directly or through same-package calls.",
			Build:  single(reentrantlock.Analyzer),
		},
	}
}

func standardGglintRules() []Rule {
	return []Rule{
		{
			Name:   "discarded_error",
			Mode:   config.Standard,
			Origin: originLogos,
			Doc:    "Reports call statements and blank assignments that drop an error result.",
			Build:  single(discardederror.Analyzer),
		},
		{
			Name:   "err_chain_reset",
			Mode:   config.Standard,
			Origin: originLogos,
			Doc:    "Reports fmt.Errorf calls that format an error with a verb other than %w or pass err.Error().",
			Build:  single(errchainreset.Analyzer),
		},
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
			Doc:    "Reports placeholder panics; strict also reports comments that start with TODO or FIXME.",
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
			Doc:    "Reports subslices and substrings that keep a backing store over 64 KiB alive past the function.",
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
