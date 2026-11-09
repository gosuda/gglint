package rules

import (
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
	return []Rule{
		{TypecheckName, config.Basic, originGglint, "Reports package load and type errors; it cannot be disabled or suppressed.", nil},
		{NolintName, config.Basic, originGglint, "Checks //nolint directives for unknown rules, missing names or reasons, and unused suppressions.", nil},
		{"complexity", config.Basic, originLogos, "Caps cognitive score, nesting, lines, parameters, and file size; large units exceed what agents edit reliably.", func(cfg config.Config) []*analysis.Analyzer {
			return []*analysis.Analyzer{complexity.New(cfg.Complexity)}
		}},
		{"typednil", config.Basic, originLogos, "Reports a typed nil pointer returned as an interface, which is never nil.", single(typednil.Analyzer)},
		{"stackaddr", config.Basic, originLogos, "Reports a stack address kept as uintptr, or an unsafe.Pointer to stack storage that leaves its function.", single(stackaddr.Analyzer)},
		{"infinite_recursion", config.Basic, originLogos, "Reports functions that call themselves with their own arguments on every path before any return.", single(infiniterecursion.Analyzer)},
		{"structtag_malformed", config.Basic, originLogos, "Reports struct tags reflect.StructTag.Get cannot parse and json or xml tags on unexported fields.", single(structtagmalformed.Analyzer)},
		{"reentrant_withlock_deadlock", config.Basic, originLogos, "Reports locking a sync mutex the goroutine already holds, directly or through same-package calls.", single(reentrantlock.Analyzer)},
		{"discarded_error", config.Standard, originLogos, "Reports call statements and blank assignments that drop an error result.", single(discardederror.Analyzer)},
		{"err_chain_reset", config.Standard, originLogos, "Reports fmt.Errorf calls that format an error with a verb other than %w or pass err.Error().", single(errchainreset.Analyzer)},
		{"capturerace", config.Standard, originLogos, "Reports multi-word variables written across a goroutine boundary without synchronization.", single(capturerace.Analyzer)},
		{"goroutine", config.Standard, originLogos, "Requires joined goroutines with exit paths; strict requires errgroup or WaitGroup.Go and bounded fan-out.", func(cfg config.Config) []*analysis.Analyzer {
			return []*analysis.Analyzer{goroutine.New(cfg.Mode)}
		}},
		{"timeafter", config.Standard, originLogos, "Reports time.After inside a loop, which allocates a timer per iteration.", single(timeafter.Analyzer)},
		{"placeholder", config.Standard, originLogos, "Reports placeholder panics; strict also reports comments that start with TODO or FIXME.", func(cfg config.Config) []*analysis.Analyzer {
			return []*analysis.Analyzer{placeholder.New(cfg.Mode)}
		}},
		{"noinit", config.Standard, originGglint, "Reports init functions; move setup into an explicit function called from main.", single(noinit.Analyzer)},
		{"chanblock", config.Strict, originLogos, "Reports channel sends and receives that can block forever without a ctx.Done() case or buffer proof.", single(chanblock.Analyzer)},
		{"closer", config.Strict, originLogos, "Reports local io closers that are neither closed with defer nor handed on.", single(closer.Analyzer)},
		{"backingpin", config.Strict, originLogos, "Reports subslices and substrings that keep a backing store over 64 KiB alive past the function.", single(backingpin.Analyzer)},
		{"unsizedint", config.Strict, originLogos, "Reports int and uint fields in JSON structs; their size depends on the platform.", single(unsizedint.Analyzer)},
	}
}
