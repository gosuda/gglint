package rules

import (
	"flag"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"4d63.com/gocheckcompilerdirectives/checkcompilerdirectives"
	"codeberg.org/polyfloyd/go-errorlint/errorlint"
	gochecksumtype "github.com/alecthomas/go-check-sumtype"
	"github.com/alingse/asasalint"
	"github.com/alingse/nilnesserr"
	makezero "github.com/ashanbrown/makezero/v2/pkg/analyzer"
	"github.com/breml/bidichk/pkg/bidichk"
	"github.com/charithe/durationcheck"
	reassign "github.com/curioswitch/go-reassign"
	gocritic "github.com/go-critic/go-critic/checkers/analyzer"
	"github.com/go-critic/go-critic/linter"
	"github.com/gordonklaus/ineffassign/pkg/ineffassign"
	"github.com/gostaticanalysis/forcetypeassert"
	"github.com/gostaticanalysis/nilerr"
	"github.com/kkHAIKE/contextcheck"
	"github.com/ldez/usetesting"
	"github.com/moricho/tparallel"
	"github.com/nishanths/exhaustive/passes/exhaustive"
	"github.com/sonatard/noctx"
	"github.com/timakin/bodyclose/passes/bodyclose"
	"go-simpler.org/musttag"
	fatcontext "go.augendre.info/fatcontext/pkg/analyzer"
	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/dupl"
	"github.com/gosuda/gglint/internal/analyzers/gofumpt"
	"github.com/gosuda/gglint/internal/analyzers/gosec"
	"github.com/gosuda/gglint/internal/analyzers/misspell"
	"github.com/gosuda/gglint/internal/analyzers/unparam"
	"github.com/gosuda/gglint/internal/config"
)

var (
	gocriticRuns sync.Mutex

	disableGocriticCache = sync.OnceFunc(func() { gocritic.DisableCache = true })
	contextcheckAnalyzer = sync.OnceValue(func() *analysis.Analyzer {
		return contextcheck.NewAnalyzer(contextcheck.Configuration{})
	})
	exhaustiveAnalyzer = sync.OnceValue(func() *analysis.Analyzer {
		return setFlags("exhaustive", exhaustive.Analyzer, map[string]string{"d": "true"})
	})
)

func gosecExcluded(m config.Mode) []string {
	if m >= config.Strict {
		return []string{"G103", "G104"}
	}
	return []string{"G101", "G103", "G104", "G115", "G204", "G301", "G302", "G304", "G306", "G404"}
}

func thirdPartyRules() []Rule {
	const basic, standard, strict, origin = config.Basic, config.Standard, config.Strict, originThirdParty
	return []Rule{
		{"ineffassign", basic, origin, "Reports assignments to variables whose value is never used.", ReportEverywhere, single(ineffassign.Analyzer)},
		{"bidichk", basic, origin, "Reports dangerous Unicode bidirectional control characters in source.", ReportEverywhere, construct(bidichk.NewAnalyzer)},
		{"gocheckcompilerdirectives", basic, origin, "Reports malformed or unknown //go: compiler directives.", ReportEverywhere, construct(checkcompilerdirectives.Analyzer)},
		{"errorlint", strict, origin, "Requires errors.Is and errors.As for error comparisons and assertions.", ReportEverywhere, buildErrorlint},
		{"gocritic", standard, origin, "Runs go-critic diagnostic checkers plus deferInLoop.", ReportEverywhere, buildGocritic},
		{"nilerr", standard, origin, "Reports returning nil after checking a non-nil error, or an error after checking it is nil.", ReportEverywhere, single(nilerr.Analyzer)},
		{"bodyclose", standard, origin, "Reports HTTP response bodies that are never closed.", ReportOutsideTests, single(bodyclose.Analyzer)},
		{"durationcheck", standard, origin, "Reports multiplication of two time.Duration values.", ReportEverywhere, single(durationcheck.Analyzer)},
		{"exhaustive", strict, origin, "Requires switch statements on enum types without a default case to cover every enum member.", ReportEverywhere, func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{exhaustiveAnalyzer()} }},
		{"gochecksumtype", standard, origin, "Requires type switches on sealed sum-type interfaces to cover every variant.", ReportEverywhere, single(gochecksumtype.Analyzer)},
		{"reassign", standard, origin, "Reports assignments to variables of other packages, such as io.EOF.", ReportEverywhere, construct(reassign.NewAnalyzer)},
		{"noctx", strict, origin, "Reports network and SQL calls made without a context.Context.", ReportOutsideTests, single(noctx.Analyzer)},
		{"fatcontext", standard, origin, "Reports contexts nested in loops and closures, which grow the context chain each iteration.", ReportEverywhere, construct(fatcontext.NewAnalyzer)},
		{"tparallel", standard, origin, "Reports t.Parallel used in subtests but not the top-level test, or the reverse.", ReportEverywhere, single(tparallel.Analyzer)},
		{"makezero", standard, origin, "Reports append to a slice created with a non-zero length.", ReportEverywhere, construct(makezero.NewAnalyzer)},
		{"nilnesserr", standard, origin, "Reports returning a nil error after checking that a different error is non-nil.", ReportEverywhere, construct(newNilnesserr)},
		{"asasalint", standard, origin, "Reports a []any passed as one element to a variadic ...any parameter.", ReportEverywhere, construct(newAsasalint)},
		{"musttag", standard, origin, "Requires field tags on structs passed to JSON, XML, YAML, and TOML marshaling functions.", ReportOutsideTests, construct(newMusttag)},
		{"usetesting", standard, origin, "Reports os.MkdirTemp, os.CreateTemp, and os.Chdir in tests, which have testing package replacements.", ReportWithFix, construct(usetesting.NewAnalyzer)},
		{"gofumpt", standard, origin, "Reports files that are not gofumpt-formatted, with a fix that formats them.", ReportWithFix, single(gofumpt.Analyzer)},
		{"gosec", standard, origin, "Reports security problems found by gosec; standard skips audit and permission checks.", ReportOutsideTests, buildGosec},
		{"unparam", standard, origin, "Reports unexported function parameters and results that are unused or always the same value.", ReportEverywhere, single(unparam.Analyzer)},
		{"misspell", standard, origin, "Reports commonly misspelled English words, with a fix that corrects them.", ReportWithFix, single(misspell.Analyzer)},
		{"forcetypeassert", strict, origin, "Reports type assertions whose ok result is not checked.", ReportOutsideTests, single(forcetypeassert.Analyzer)},
		{"contextcheck", strict, origin, "Reports functions that use a non-inherited context.", ReportOutsideTests, buildContextcheck},
		{"dupl", strict, origin, "Reports duplicated code fragments of 150 or more tokens.", ReportEverywhere, single(dupl.Analyzer)},
	}
}

func must[T any](rule string, v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("rules: %s: %v", rule, err))
	}
	return v
}

func setFlags(rule string, a *analysis.Analyzer, flags map[string]string) *analysis.Analyzer {
	for name, value := range flags {
		a = must(rule, a, a.Flags.Set(name, value))
	}
	return a
}

func construct(newAnalyzer func() *analysis.Analyzer) func(config.Config) []*analysis.Analyzer {
	return func(config.Config) []*analysis.Analyzer { return []*analysis.Analyzer{newAnalyzer()} }
}

func buildErrorlint(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{setFlags("errorlint", errorlint.NewAnalyzer(), map[string]string{"errorf": strconv.FormatBool(false)})}
}

func buildGosec(cfg config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{gosec.New(gosecExcluded(cfg.Mode))}
}

type deferredFlag struct {
	name     string
	usage    string
	value    string
	isBool   bool
	validate func(string) error
}

func (f *deferredFlag) String() string { return f.value }

func (f *deferredFlag) Set(value string) error {
	if err := f.validate(value); err != nil {
		return err
	}
	f.value = value
	return nil
}

func (f *deferredFlag) IsBoolFlag() bool { return f.isBool }

func (f *deferredFlag) Get() any { return f.value }

func isBoolFlag(v flag.Value) bool {
	b, ok := v.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

var flagValueType = reflect.TypeFor[flag.Value]()

func freshParser(rule string, f *flag.Flag) func(string) error {
	t := reflect.TypeOf(f.Value)
	if t == nil || t.Kind() != reflect.Pointer || !t.Implements(flagValueType) {
		panic(fmt.Sprintf("rules: %s: flag %q has unsupported value type %T", rule, f.Name, f.Value))
	}
	return func(s string) error {
		fresh, ok := reflect.TypeAssert[flag.Value](reflect.New(t.Elem()))
		if !ok {
			return fmt.Errorf("flag %s: value type %s is not a flag.Value", f.Name, t)
		}
		return fresh.Set(s)
	}
}

type globalAnalyzer struct {
	rule     string
	upstream *analysis.Analyzer
	runs     *sync.Mutex
}

func (g globalAnalyzer) build(overrides map[string]string) *analysis.Analyzer {
	a := &analysis.Analyzer{
		Name:             g.upstream.Name,
		Doc:              g.upstream.Doc,
		URL:              g.upstream.URL,
		RunDespiteErrors: g.upstream.RunDespiteErrors,
		Requires:         g.upstream.Requires,
		ResultType:       g.upstream.ResultType,
		FactTypes:        g.upstream.FactTypes,
	}
	a.Flags.Init(g.rule, flag.ContinueOnError)
	deferred := g.deferredFlags(overrides)
	for _, d := range deferred {
		a.Flags.Var(d, d.name, d.usage)
	}
	a.Run = func(pass *analysis.Pass) (any, error) {
		g.runs.Lock()
		defer g.runs.Unlock()
		for _, d := range deferred {
			if err := g.upstream.Flags.Set(d.name, d.value); err != nil {
				return nil, fmt.Errorf("flag %s: %w", d.name, err)
			}
		}
		return g.upstream.Run(pass)
	}
	return a
}

func (g globalAnalyzer) deferredFlags(overrides map[string]string) []*deferredFlag {
	g.runs.Lock()
	defer g.runs.Unlock()
	for name := range overrides {
		if g.upstream.Flags.Lookup(name) == nil {
			panic(fmt.Sprintf("rules: %s: unknown flag %q", g.rule, name))
		}
	}
	var deferred []*deferredFlag
	g.upstream.Flags.VisitAll(func(f *flag.Flag) {
		d := &deferredFlag{name: f.Name, usage: f.Usage, value: f.DefValue, isBool: isBoolFlag(f.Value), validate: freshParser(g.rule, f)}
		if value, ok := overrides[f.Name]; ok {
			must(g.rule, struct{}{}, d.Set(value))
		}
		deferred = append(deferred, d)
	})
	return deferred
}

func buildGocritic(config.Config) []*analysis.Analyzer {
	disableGocriticCache()
	var enabled []string
	for _, info := range linter.GetCheckersInfo() {
		if !slices.Contains(info.Tags, linter.ExperimentalTag) && slices.Contains(info.Tags, linter.DiagnosticTag) {
			enabled = append(enabled, info.Name)
		}
	}
	gocriticGlobal := globalAnalyzer{"gocritic", gocritic.Analyzer, &gocriticRuns}
	return []*analysis.Analyzer{gocriticGlobal.build(map[string]string{
		"enable":  strings.Join(append(enabled, "deferInLoop"), ","),
		"disable": "",
	})}
}

func newNilnesserr() *analysis.Analyzer {
	a, err := nilnesserr.NewAnalyzer(nilnesserr.LinterSetting{})
	return must("nilnesserr", a, err)
}

func newAsasalint() *analysis.Analyzer {
	a, err := asasalint.NewAnalyzer(asasalint.LinterSetting{})
	return must("asasalint", a, err)
}

func newMusttag() *analysis.Analyzer {
	return musttag.New()
}

func buildContextcheck(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{contextcheckAnalyzer()}
}
