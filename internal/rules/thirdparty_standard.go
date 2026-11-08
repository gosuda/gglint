package rules

import (
	"slices"
	"strings"
	"sync"

	"codeberg.org/polyfloyd/go-errorlint/errorlint"
	"github.com/Abirdcfly/dupword"
	gochecksumtype "github.com/alecthomas/go-check-sumtype"
	"github.com/alexkohler/nakedret/v2"
	"github.com/alingse/asasalint"
	"github.com/alingse/nilnesserr"
	makezero "github.com/ashanbrown/makezero/v2/pkg/analyzer"
	"github.com/charithe/durationcheck"
	reassign "github.com/curioswitch/go-reassign"
	"github.com/go-critic/go-critic/checkers"
	gocritic "github.com/go-critic/go-critic/checkers/analyzer"
	"github.com/go-critic/go-critic/linter"
	"github.com/gostaticanalysis/nilerr"
	thelper "github.com/kulti/thelper/pkg/analyzer"
	"github.com/ldez/usetesting"
	"github.com/maratori/testableexamples/pkg/testableexamples"
	"github.com/moricho/tparallel"
	"github.com/nishanths/exhaustive/passes/exhaustive"
	"github.com/nishanths/predeclared/passes/predeclared"
	"github.com/raeperd/recvcheck"
	"github.com/sonatard/noctx"
	"github.com/timakin/bodyclose/passes/bodyclose"
	"go-simpler.org/musttag"
	"go-simpler.org/sloglint"
	fatcontext "go.augendre.info/fatcontext/pkg/analyzer"
	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/gofumpt"
	"github.com/gosuda/gglint/internal/analyzers/gosec"
	"github.com/gosuda/gglint/internal/analyzers/misspell"
	"github.com/gosuda/gglint/internal/analyzers/unconvert"
	"github.com/gosuda/gglint/internal/analyzers/unparam"
	"github.com/gosuda/gglint/internal/config"
)

var gocriticCheckers = sync.OnceValues(loadGocriticCheckers)

func thirdPartyStandardRules() []Rule {
	return []Rule{
		standardRule("errorlint", "Reports error comparisons and assertions that break wrapping, and fmt.Errorf without %w.", buildErrorlint),
		standardRule("gocritic", "Runs go-critic diagnostic and style checkers plus deferInLoop; strict adds opinionated and performance.", buildGocritic),
		standardRule("nilerr", "Reports returning nil after checking a non-nil error, or an error after checking it is nil.", single(nilerr.Analyzer)),
		standardRule("bodyclose", "Reports HTTP response bodies that are never closed.", single(bodyclose.Analyzer)),
		standardRule("durationcheck", "Reports multiplication of two time.Duration values.", single(durationcheck.Analyzer)),
		standardRule("exhaustive", "Requires switch statements on enum types to cover every enum member.", single(exhaustive.Analyzer)),
		standardRule("gochecksumtype", "Requires type switches on sealed sum-type interfaces to cover every variant.", single(gochecksumtype.Analyzer)),
		standardRule("reassign", "Reports assignments to variables of other packages, such as io.EOF.", construct(reassign.NewAnalyzer)),
		standardRule("predeclared", "Reports declarations that shadow predeclared identifiers.", single(predeclared.Analyzer)),
		standardRule("noctx", "Reports network and SQL calls made without a context.Context.", single(noctx.Analyzer)),
		standardRule("fatcontext", "Reports contexts nested in loops and closures, which grow the context chain each iteration.", construct(fatcontext.NewAnalyzer)),
		standardRule("thelper", "Requires test helpers to call Helper first and take their *testing.T, B, F, or TB first, conventionally named.", construct(thelper.NewAnalyzer)),
		standardRule("tparallel", "Reports t.Parallel used in subtests but not the top-level test, or the reverse.", single(tparallel.Analyzer)),
		standardRule("testableexamples", "Reports example functions without an output comment, which compile but never run.", construct(testableexamples.NewAnalyzer)),
		standardRule("makezero", "Reports append to a slice created with a non-zero length.", construct(makezero.NewAnalyzer)),
		standardRule("nilnesserr", "Reports returning a nil error after checking that a different error is non-nil.", construct(newNilnesserr)),
		standardRule("asasalint", "Reports a []any passed as one element to a variadic ...any parameter.", construct(newAsasalint)),
		standardRule("recvcheck", "Reports types whose methods mix value and pointer receivers.", construct(newRecvcheck)),
		standardRule("nakedret", "Reports naked returns in functions longer than 30 lines.", construct(newNakedret)),
		standardRule("musttag", "Requires field tags on structs passed to JSON, XML, YAML, and TOML marshaling functions.", construct(newMusttag)),
		standardRule("sloglint", "Reports log/slog handlers built over io.Discard; use slog.DiscardHandler.", construct(newSloglint)),
		standardRule("dupword", "Reports duplicated words in comments and strings, such as \"the the\".", construct(dupword.NewAnalyzer)),
		standardRule("usetesting", "Reports os.MkdirTemp, os.CreateTemp, and os.Chdir in tests, which have testing package replacements.", construct(usetesting.NewAnalyzer)),
		standardRule("gofumpt", "Reports files that are not gofumpt-formatted, with a fix that formats them.", single(gofumpt.Analyzer)),
		standardRule("gosec", "Reports security problems found by gosec; standard skips G104, G115, G304, strict skips G104.", buildGosec),
		standardRule("unconvert", "Reports type conversions to the type the value already has.", single(unconvert.Analyzer)),
		standardRule("unparam", "Reports unexported function parameters and results that are unused or always the same value.", single(unparam.Analyzer)),
		standardRule("misspell", "Reports commonly misspelled English words, with a fix that corrects them.", single(misspell.Analyzer)),
	}
}

func standardRule(name, doc string, build func(config.Config) []*analysis.Analyzer) Rule {
	return Rule{Name: name, Mode: config.Standard, Origin: originThirdParty, Doc: doc, Build: build}
}

func buildErrorlint(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{setFlags("errorlint", errorlint.NewAnalyzer(), map[string]string{"errorf": flagTrue})}
}

func buildGocritic(cfg config.Config) []*analysis.Analyzer {
	flags := map[string]string{
		"enable":  strings.Join(gocriticEnabled(cfg.Mode), ","),
		"disable": "",
	}
	return []*analysis.Analyzer{setFlags("gocritic", gocritic.Analyzer, flags)}
}

func gocriticEnabled(m config.Mode) []string {
	infos, err := gocriticCheckers()
	excluded := gocriticExcludedTags(m)
	var names []string
	for _, info := range must("gocritic", infos, err) {
		if !slices.ContainsFunc(info.Tags, func(tag string) bool { return slices.Contains(excluded, tag) }) {
			names = append(names, info.Name)
		}
	}
	return append(names, "deferInLoop")
}

func gocriticExcludedTags(m config.Mode) []string {
	if m >= config.Strict {
		return []string{linter.ExperimentalTag, linter.SecurityTag}
	}
	return []string{linter.ExperimentalTag, linter.OpinionatedTag, linter.PerformanceTag, linter.SecurityTag}
}

func loadGocriticCheckers() ([]*linter.CheckerInfo, error) {
	if err := checkers.InitEmbeddedRules(); err != nil {
		return nil, err
	}
	return linter.GetCheckersInfo(), nil
}

func buildGosec(cfg config.Config) []*analysis.Analyzer {
	if cfg.Mode >= config.Strict {
		return []*analysis.Analyzer{gosec.New([]string{"G104"})}
	}
	return []*analysis.Analyzer{gosec.New([]string{"G104", "G115", "G304"})}
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
