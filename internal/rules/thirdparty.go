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
	"4d63.com/gochecknoglobals/checknoglobals"
	"codeberg.org/polyfloyd/go-errorlint/errorlint"
	"github.com/Abirdcfly/dupword"
	errname "github.com/Antonboom/errname/pkg/analyzer"
	nilnil "github.com/Antonboom/nilnil/pkg/analyzer"
	err113 "github.com/Djarvur/go-err113"
	gochecksumtype "github.com/alecthomas/go-check-sumtype"
	"github.com/alexkohler/nakedret/v2"
	"github.com/alingse/asasalint"
	"github.com/alingse/nilnesserr"
	makezero "github.com/ashanbrown/makezero/v2/pkg/analyzer"
	"github.com/breml/bidichk/pkg/bidichk"
	ireturn "github.com/butuzov/ireturn/analyzer"
	"github.com/butuzov/mirror"
	perfsprint "github.com/catenacyber/perfsprint/analyzer"
	"github.com/charithe/durationcheck"
	reassign "github.com/curioswitch/go-reassign"
	gocritic "github.com/go-critic/go-critic/checkers/analyzer"
	"github.com/go-critic/go-critic/linter"
	"github.com/golangci/asciicheck"
	"github.com/gordonklaus/ineffassign/pkg/ineffassign"
	"github.com/gostaticanalysis/forcetypeassert"
	"github.com/gostaticanalysis/nilerr"
	"github.com/kisielk/errcheck/errcheck"
	"github.com/kkHAIKE/contextcheck"
	thelper "github.com/kulti/thelper/pkg/analyzer"
	"github.com/kunwardeep/paralleltest/pkg/paralleltest"
	"github.com/ldez/usetesting"
	"github.com/maratori/testableexamples/pkg/testableexamples"
	"github.com/moricho/tparallel"
	"github.com/nishanths/exhaustive/passes/exhaustive"
	"github.com/nishanths/predeclared/passes/predeclared"
	"github.com/raeperd/recvcheck"
	"github.com/sivchari/containedctx"
	"github.com/sonatard/noctx"
	"github.com/timakin/bodyclose/passes/bodyclose"
	"github.com/tomarrell/wrapcheck/v2/wrapcheck"
	"github.com/uudashr/iface/identical"
	"github.com/uudashr/iface/unexported"
	ifaceunused "github.com/uudashr/iface/unused"
	"github.com/uudashr/iface/unusedmethod"
	"go-simpler.org/musttag"
	"go-simpler.org/sloglint"
	fatcontext "go.augendre.info/fatcontext/pkg/analyzer"
	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/dupl"
	"github.com/gosuda/gglint/internal/analyzers/goconst"
	"github.com/gosuda/gglint/internal/analyzers/gofumpt"
	"github.com/gosuda/gglint/internal/analyzers/gosec"
	"github.com/gosuda/gglint/internal/analyzers/misspell"
	"github.com/gosuda/gglint/internal/analyzers/unconvert"
	"github.com/gosuda/gglint/internal/analyzers/unparam"
	"github.com/gosuda/gglint/internal/config"
)

var (
	errcheckRuns sync.Mutex
	gocriticRuns sync.Mutex

	disableGocriticCache = sync.OnceFunc(func() { gocritic.DisableCache = true })
	contextcheckAnalyzer = sync.OnceValue(func() *analysis.Analyzer {
		return contextcheck.NewAnalyzer(contextcheck.Configuration{})
	})
)

func thirdPartyRules() []Rule {
	const basic, standard, strict, origin = config.Basic, config.Standard, config.Strict, originThirdParty
	return []Rule{
		{"errcheck", basic, origin, "Reports unchecked errors; standard adds blank assignments, strict adds unchecked type assertions.", ReportEverywhere, buildErrcheck},
		{"ineffassign", basic, origin, "Reports assignments to variables whose value is never used.", ReportEverywhere, single(ineffassign.Analyzer)},
		{"bidichk", basic, origin, "Reports dangerous Unicode bidirectional control characters in source.", ReportEverywhere, construct(bidichk.NewAnalyzer)},
		{"gocheckcompilerdirectives", basic, origin, "Reports malformed or unknown //go: compiler directives.", ReportEverywhere, construct(checkcompilerdirectives.Analyzer)},
		{"errorlint", standard, origin, "Reports error comparisons and assertions that break wrapping, and fmt.Errorf without %w.", ReportEverywhere, buildErrorlint},
		{"gocritic", standard, origin, "Runs go-critic diagnostic and style checkers plus deferInLoop; strict adds opinionated and performance.", ReportEverywhere, buildGocritic},
		{"nilerr", standard, origin, "Reports returning nil after checking a non-nil error, or an error after checking it is nil.", ReportEverywhere, single(nilerr.Analyzer)},
		{"bodyclose", standard, origin, "Reports HTTP response bodies that are never closed.", ReportEverywhere, single(bodyclose.Analyzer)},
		{"durationcheck", standard, origin, "Reports multiplication of two time.Duration values.", ReportEverywhere, single(durationcheck.Analyzer)},
		{"exhaustive", standard, origin, "Requires switch statements on enum types to cover every enum member.", ReportEverywhere, single(exhaustive.Analyzer)},
		{"gochecksumtype", standard, origin, "Requires type switches on sealed sum-type interfaces to cover every variant.", ReportEverywhere, single(gochecksumtype.Analyzer)},
		{"reassign", standard, origin, "Reports assignments to variables of other packages, such as io.EOF.", ReportEverywhere, construct(reassign.NewAnalyzer)},
		{"predeclared", standard, origin, "Reports declarations that shadow predeclared identifiers.", ReportEverywhere, single(predeclared.Analyzer)},
		{"noctx", standard, origin, "Reports network and SQL calls made without a context.Context.", ReportEverywhere, single(noctx.Analyzer)},
		{"fatcontext", standard, origin, "Reports contexts nested in loops and closures, which grow the context chain each iteration.", ReportEverywhere, construct(fatcontext.NewAnalyzer)},
		{"thelper", standard, origin, "Requires test helpers to call Helper first and take their *testing.T, B, F, or TB first, conventionally named.", ReportEverywhere, construct(thelper.NewAnalyzer)},
		{"tparallel", standard, origin, "Reports t.Parallel used in subtests but not the top-level test, or the reverse.", ReportEverywhere, single(tparallel.Analyzer)},
		{"testableexamples", standard, origin, "Reports example functions without an output comment, which compile but never run.", ReportEverywhere, construct(testableexamples.NewAnalyzer)},
		{"makezero", standard, origin, "Reports append to a slice created with a non-zero length.", ReportEverywhere, construct(makezero.NewAnalyzer)},
		{"nilnesserr", standard, origin, "Reports returning a nil error after checking that a different error is non-nil.", ReportEverywhere, construct(newNilnesserr)},
		{"asasalint", standard, origin, "Reports a []any passed as one element to a variadic ...any parameter.", ReportEverywhere, construct(newAsasalint)},
		{"recvcheck", standard, origin, "Reports types whose methods mix value and pointer receivers.", ReportEverywhere, construct(newRecvcheck)},
		{"nakedret", standard, origin, "Reports naked returns in functions longer than 30 lines.", ReportEverywhere, construct(newNakedret)},
		{"musttag", standard, origin, "Requires field tags on structs passed to JSON, XML, YAML, and TOML marshaling functions.", ReportEverywhere, construct(newMusttag)},
		{"sloglint", standard, origin, "Reports log/slog handlers built over io.Discard; use slog.DiscardHandler.", ReportEverywhere, construct(newSloglint)},
		{"dupword", standard, origin, "Reports duplicated words in comments and strings, such as \"the the\".", ReportEverywhere, construct(dupword.NewAnalyzer)},
		{"usetesting", standard, origin, "Reports os.MkdirTemp, os.CreateTemp, and os.Chdir in tests, which have testing package replacements.", ReportEverywhere, construct(usetesting.NewAnalyzer)},
		{"gofumpt", standard, origin, "Reports files that are not gofumpt-formatted, with a fix that formats them.", ReportEverywhere, single(gofumpt.Analyzer)},
		{"gosec", standard, origin, "Reports security problems found by gosec; standard skips G104, G115, G304, strict skips G104.", ReportEverywhere, buildGosec},
		{"unconvert", standard, origin, "Reports type conversions to the type the value already has.", ReportEverywhere, single(unconvert.Analyzer)},
		{"unparam", standard, origin, "Reports unexported function parameters and results that are unused or always the same value.", ReportEverywhere, single(unparam.Analyzer)},
		{"misspell", standard, origin, "Reports commonly misspelled English words, with a fix that corrects them.", ReportEverywhere, single(misspell.Analyzer)},
		{"err113", strict, origin, "Reports dynamic errors.New and fmt.Errorf values and errors compared with ==.", ReportEverywhere, construct(err113.NewAnalyzer)},
		{"forcetypeassert", strict, origin, "Reports type assertions whose ok result is not checked.", ReportEverywhere, single(forcetypeassert.Analyzer)},
		{"ireturn", strict, origin, "Reports functions that return interfaces; accept interfaces, return concrete types.", ReportEverywhere, construct(ireturn.NewAnalyzer)},
		{"containedctx", strict, origin, "Reports struct types that hold a context.Context field.", ReportEverywhere, single(containedctx.Analyzer)},
		{"mirror", strict, origin, "Reports bytes and string conversions where a mirrored bytes or strings function exists.", ReportEverywhere, construct(mirror.NewAnalyzer)},
		{"asciicheck", strict, origin, "Reports identifiers that contain non-ASCII characters.", ReportEverywhere, construct(asciicheck.NewAnalyzer)},
		{"errname", strict, origin, "Requires error types named XxxError and sentinel errors named ErrXxx.", ReportEverywhere, construct(errname.New)},
		{"nilnil", strict, origin, "Reports returning a nil error together with an invalid nil value.", ReportEverywhere, construct(nilnil.New)},
		{"gochecknoglobals", strict, origin, "Reports package-level variables.", ReportEverywhere, construct(checknoglobals.Analyzer)},
		{"wrapcheck", strict, origin, "Reports errors from other packages returned without wrapping.", ReportEverywhere, construct(newWrapcheck)},
		{"contextcheck", strict, origin, "Reports functions that use a non-inherited context.", ReportEverywhere, buildContextcheck},

		{"iface", strict, origin, "Reports identical, unused, and unexported interfaces and interface methods never called.", ReportEverywhere, buildIface},
		{"paralleltest", strict, origin, "Requires tests and subtests to call t.Parallel.", ReportEverywhere, construct(paralleltest.NewAnalyzer)},
		{"perfsprint", strict, origin, "Reports fmt.Sprintf calls that a faster strconv or string operation can replace.", ReportEverywhere, construct(perfsprint.New)},
		{"goconst", strict, origin, "Reports strings of 3 or more bytes repeated 3 or more times that should be constants.", ReportEverywhere, single(goconst.Analyzer)},
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
	return []*analysis.Analyzer{setFlags("errorlint", errorlint.NewAnalyzer(), map[string]string{"errorf": strconv.FormatBool(true)})}
}

func buildGosec(cfg config.Config) []*analysis.Analyzer {
	if cfg.Mode >= config.Strict {
		return []*analysis.Analyzer{gosec.New([]string{"G104"})}
	}
	return []*analysis.Analyzer{gosec.New([]string{"G104", "G115", "G304"})}
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

func buildErrcheck(cfg config.Config) []*analysis.Analyzer {
	errcheckGlobal := globalAnalyzer{"errcheck", errcheck.Analyzer, &errcheckRuns}
	return []*analysis.Analyzer{errcheckGlobal.build(map[string]string{
		"blank":  strconv.FormatBool(cfg.Mode >= config.Standard),
		"assert": strconv.FormatBool(cfg.Mode >= config.Strict),
	})}
}

func buildGocritic(cfg config.Config) []*analysis.Analyzer {
	disableGocriticCache()
	excluded := []string{linter.ExperimentalTag, linter.SecurityTag}
	if cfg.Mode < config.Strict {
		excluded = append(excluded, linter.OpinionatedTag, linter.PerformanceTag)
	}
	var enabled []string
	for _, info := range linter.GetCheckersInfo() {
		if !slices.ContainsFunc(info.Tags, func(tag string) bool { return slices.Contains(excluded, tag) }) {
			enabled = append(enabled, info.Name)
		}
	}
	gocriticGlobal := globalAnalyzer{"gocritic", gocritic.Analyzer, &gocriticRuns}
	return []*analysis.Analyzer{gocriticGlobal.build(map[string]string{
		"enable":  strings.Join(append(enabled, "deferInLoop"), ","),
		"disable": "",
	})}
}

func buildIface(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{
		identical.Analyzer,
		ifaceunused.Analyzer,
		unexported.Analyzer,
		unusedmethod.Analyzer,
	}
}

func newNilnesserr() *analysis.Analyzer {
	a, err := nilnesserr.NewAnalyzer(nilnesserr.LinterSetting{})
	return must("nilnesserr", a, err)
}

func newAsasalint() *analysis.Analyzer {
	a, err := asasalint.NewAnalyzer(asasalint.LinterSetting{})
	return must("asasalint", a, err)
}

func newRecvcheck() *analysis.Analyzer {
	return recvcheck.NewAnalyzer(recvcheck.Settings{})
}

func newNakedret() *analysis.Analyzer {
	return nakedret.NakedReturnAnalyzer(&nakedret.NakedReturnRunner{MaxLength: 30})
}

func newMusttag() *analysis.Analyzer {
	return musttag.New()
}

func newSloglint() *analysis.Analyzer {
	return sloglint.New(&sloglint.Options{})
}

func newWrapcheck() *analysis.Analyzer {
	return wrapcheck.NewAnalyzer(wrapcheck.NewDefaultConfig())
}

func buildContextcheck(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{contextcheckAnalyzer()}
}
