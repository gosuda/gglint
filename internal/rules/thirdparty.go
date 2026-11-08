package rules

import (
	"fmt"
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
	"github.com/go-critic/go-critic/checkers"
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

var gocriticCheckers = sync.OnceValues(func() ([]*linter.CheckerInfo, error) {
	if err := checkers.InitEmbeddedRules(); err != nil {
		return nil, err
	}
	return linter.GetCheckersInfo(), nil
})

func thirdPartyRules() []Rule {
	const basic, standard, strict, origin = config.Basic, config.Standard, config.Strict, originThirdParty
	return []Rule{
		{"errcheck", basic, origin, "Reports unchecked errors; standard adds blank assignments, strict adds unchecked type assertions.", buildErrcheck},
		{"ineffassign", basic, origin, "Reports assignments to variables whose value is never used.", single(ineffassign.Analyzer)},
		{"bidichk", basic, origin, "Reports dangerous Unicode bidirectional control characters in source.", construct(bidichk.NewAnalyzer)},
		{"gocheckcompilerdirectives", basic, origin, "Reports malformed or unknown //go: compiler directives.", construct(checkcompilerdirectives.Analyzer)},
		{"errorlint", standard, origin, "Reports error comparisons and assertions that break wrapping, and fmt.Errorf without %w.", buildErrorlint},
		{"gocritic", standard, origin, "Runs go-critic diagnostic and style checkers plus deferInLoop; strict adds opinionated and performance.", buildGocritic},
		{"nilerr", standard, origin, "Reports returning nil after checking a non-nil error, or an error after checking it is nil.", single(nilerr.Analyzer)},
		{"bodyclose", standard, origin, "Reports HTTP response bodies that are never closed.", single(bodyclose.Analyzer)},
		{"durationcheck", standard, origin, "Reports multiplication of two time.Duration values.", single(durationcheck.Analyzer)},
		{"exhaustive", standard, origin, "Requires switch statements on enum types to cover every enum member.", single(exhaustive.Analyzer)},
		{"gochecksumtype", standard, origin, "Requires type switches on sealed sum-type interfaces to cover every variant.", single(gochecksumtype.Analyzer)},
		{"reassign", standard, origin, "Reports assignments to variables of other packages, such as io.EOF.", construct(reassign.NewAnalyzer)},
		{"predeclared", standard, origin, "Reports declarations that shadow predeclared identifiers.", single(predeclared.Analyzer)},
		{"noctx", standard, origin, "Reports network and SQL calls made without a context.Context.", single(noctx.Analyzer)},
		{"fatcontext", standard, origin, "Reports contexts nested in loops and closures, which grow the context chain each iteration.", construct(fatcontext.NewAnalyzer)},
		{"thelper", standard, origin, "Requires test helpers to call Helper first and take their *testing.T, B, F, or TB first, conventionally named.", construct(thelper.NewAnalyzer)},
		{"tparallel", standard, origin, "Reports t.Parallel used in subtests but not the top-level test, or the reverse.", single(tparallel.Analyzer)},
		{"testableexamples", standard, origin, "Reports example functions without an output comment, which compile but never run.", construct(testableexamples.NewAnalyzer)},
		{"makezero", standard, origin, "Reports append to a slice created with a non-zero length.", construct(makezero.NewAnalyzer)},
		{"nilnesserr", standard, origin, "Reports returning a nil error after checking that a different error is non-nil.", construct(newNilnesserr)},
		{"asasalint", standard, origin, "Reports a []any passed as one element to a variadic ...any parameter.", construct(newAsasalint)},
		{"recvcheck", standard, origin, "Reports types whose methods mix value and pointer receivers.", construct(newRecvcheck)},
		{"nakedret", standard, origin, "Reports naked returns in functions longer than 30 lines.", construct(newNakedret)},
		{"musttag", standard, origin, "Requires field tags on structs passed to JSON, XML, YAML, and TOML marshaling functions.", construct(newMusttag)},
		{"sloglint", standard, origin, "Reports log/slog handlers built over io.Discard; use slog.DiscardHandler.", construct(newSloglint)},
		{"dupword", standard, origin, "Reports duplicated words in comments and strings, such as \"the the\".", construct(dupword.NewAnalyzer)},
		{"usetesting", standard, origin, "Reports os.MkdirTemp, os.CreateTemp, and os.Chdir in tests, which have testing package replacements.", construct(usetesting.NewAnalyzer)},
		{"gofumpt", standard, origin, "Reports files that are not gofumpt-formatted, with a fix that formats them.", single(gofumpt.Analyzer)},
		{"gosec", standard, origin, "Reports security problems found by gosec; standard skips G104, G115, G304, strict skips G104.", buildGosec},
		{"unconvert", standard, origin, "Reports type conversions to the type the value already has.", single(unconvert.Analyzer)},
		{"unparam", standard, origin, "Reports unexported function parameters and results that are unused or always the same value.", single(unparam.Analyzer)},
		{"misspell", standard, origin, "Reports commonly misspelled English words, with a fix that corrects them.", single(misspell.Analyzer)},
		{"err113", strict, origin, "Reports dynamic errors.New and fmt.Errorf values and errors compared with ==.", construct(err113.NewAnalyzer)},
		{"forcetypeassert", strict, origin, "Reports type assertions whose ok result is not checked.", single(forcetypeassert.Analyzer)},
		{"ireturn", strict, origin, "Reports functions that return interfaces; accept interfaces, return concrete types.", construct(ireturn.NewAnalyzer)},
		{"containedctx", strict, origin, "Reports struct types that hold a context.Context field.", single(containedctx.Analyzer)},
		{"mirror", strict, origin, "Reports bytes and string conversions where a mirrored bytes or strings function exists.", construct(mirror.NewAnalyzer)},
		{"asciicheck", strict, origin, "Reports identifiers that contain non-ASCII characters.", construct(asciicheck.NewAnalyzer)},
		{"errname", strict, origin, "Requires error types named XxxError and sentinel errors named ErrXxx.", construct(errname.New)},
		{"nilnil", strict, origin, "Reports returning a nil error together with an invalid nil value.", construct(nilnil.New)},
		{"gochecknoglobals", strict, origin, "Reports package-level variables.", construct(checknoglobals.Analyzer)},
		{"wrapcheck", strict, origin, "Reports errors from other packages returned without wrapping.", construct(newWrapcheck)},
		{"contextcheck", strict, origin, "Reports functions that use a non-inherited context.", construct(newContextcheck)},
		{"iface", strict, origin, "Reports identical, unused, and unexported interfaces and interface methods never called.", buildIface},
		{"paralleltest", strict, origin, "Requires tests and subtests to call t.Parallel.", construct(paralleltest.NewAnalyzer)},
		{"perfsprint", strict, origin, "Reports fmt.Sprintf calls that a faster strconv or string operation can replace.", construct(perfsprint.New)},
		{"goconst", strict, origin, "Reports strings of 3 or more bytes repeated 3 or more times that should be constants.", single(goconst.Analyzer)},
		{"dupl", strict, origin, "Reports duplicated code fragments of 150 or more tokens.", single(dupl.Analyzer)},
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

func buildErrcheck(cfg config.Config) []*analysis.Analyzer {
	flags := map[string]string{
		"blank":  strconv.FormatBool(cfg.Mode >= config.Standard),
		"assert": strconv.FormatBool(cfg.Mode >= config.Strict),
	}
	return []*analysis.Analyzer{setFlags("errcheck", errcheck.Analyzer, flags)}
}

func buildErrorlint(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{setFlags("errorlint", errorlint.NewAnalyzer(), map[string]string{"errorf": strconv.FormatBool(true)})}
}

func buildGocritic(cfg config.Config) []*analysis.Analyzer {
	excluded := []string{linter.ExperimentalTag, linter.SecurityTag}
	if cfg.Mode < config.Strict {
		excluded = append(excluded, linter.OpinionatedTag, linter.PerformanceTag)
	}
	infos, err := gocriticCheckers()
	var enabled []string
	for _, info := range must("gocritic", infos, err) {
		if !slices.ContainsFunc(info.Tags, func(tag string) bool { return slices.Contains(excluded, tag) }) {
			enabled = append(enabled, info.Name)
		}
	}
	flags := map[string]string{
		"enable":  strings.Join(append(enabled, "deferInLoop"), ","),
		"disable": "",
	}
	return []*analysis.Analyzer{setFlags("gocritic", gocritic.Analyzer, flags)}
}

func buildGosec(cfg config.Config) []*analysis.Analyzer {
	if cfg.Mode >= config.Strict {
		return []*analysis.Analyzer{gosec.New([]string{"G104"})}
	}
	return []*analysis.Analyzer{gosec.New([]string{"G104", "G115", "G304"})}
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

func newContextcheck() *analysis.Analyzer {
	return contextcheck.NewAnalyzer(contextcheck.Configuration{})
}
