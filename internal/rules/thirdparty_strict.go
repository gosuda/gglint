package rules

import (
	"4d63.com/gochecknoglobals/checknoglobals"
	errname "github.com/Antonboom/errname/pkg/analyzer"
	nilnil "github.com/Antonboom/nilnil/pkg/analyzer"
	err113 "github.com/Djarvur/go-err113"
	ireturn "github.com/butuzov/ireturn/analyzer"
	"github.com/butuzov/mirror"
	perfsprint "github.com/catenacyber/perfsprint/analyzer"
	"github.com/golangci/asciicheck"
	"github.com/gostaticanalysis/forcetypeassert"
	"github.com/kkHAIKE/contextcheck"
	"github.com/kunwardeep/paralleltest/pkg/paralleltest"
	"github.com/sivchari/containedctx"
	"github.com/tomarrell/wrapcheck/v2/wrapcheck"
	"github.com/uudashr/iface/identical"
	"github.com/uudashr/iface/unexported"
	ifaceunused "github.com/uudashr/iface/unused"
	"github.com/uudashr/iface/unusedmethod"
	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/analyzers/dupl"
	"github.com/gosuda/gglint/internal/analyzers/goconst"
	"github.com/gosuda/gglint/internal/config"
)

func thirdPartyStrictRules() []Rule {
	return []Rule{
		strictRule("err113", "Reports dynamic errors.New and fmt.Errorf values and errors compared with ==.", construct(err113.NewAnalyzer)),
		strictRule("forcetypeassert", "Reports type assertions whose ok result is not checked.", single(forcetypeassert.Analyzer)),
		strictRule("ireturn", "Reports functions that return interfaces; accept interfaces, return concrete types.", construct(ireturn.NewAnalyzer)),
		strictRule("containedctx", "Reports struct types that hold a context.Context field.", single(containedctx.Analyzer)),
		strictRule("mirror", "Reports bytes and string conversions where a mirrored bytes or strings function exists.", construct(mirror.NewAnalyzer)),
		strictRule("asciicheck", "Reports identifiers that contain non-ASCII characters.", construct(asciicheck.NewAnalyzer)),
		strictRule("errname", "Requires error types named XxxError and sentinel errors named ErrXxx.", construct(errname.New)),
		strictRule("nilnil", "Reports returning a nil error together with an invalid nil value.", construct(nilnil.New)),
		strictRule("gochecknoglobals", "Reports package-level variables.", construct(checknoglobals.Analyzer)),
		strictRule("wrapcheck", "Reports errors from other packages returned without wrapping.", construct(newWrapcheck)),
		strictRule("contextcheck", "Reports functions that use a non-inherited context.", construct(newContextcheck)),
		strictRule("iface", "Reports identical, unused, and unexported interfaces and interface methods never called.", buildIface),
		strictRule("paralleltest", "Requires tests and subtests to call t.Parallel.", construct(paralleltest.NewAnalyzer)),
		strictRule("perfsprint", "Reports fmt.Sprintf calls that a faster strconv or string operation can replace.", construct(perfsprint.New)),
		strictRule("goconst", "Reports strings of 3 or more bytes repeated 3 or more times that should be constants.", single(goconst.Analyzer)),
		strictRule("dupl", "Reports duplicated code fragments of 150 or more tokens.", single(dupl.Analyzer)),
	}
}

func strictRule(name, doc string, build func(config.Config) []*analysis.Analyzer) Rule {
	return Rule{Name: name, Mode: config.Strict, Origin: originThirdParty, Doc: doc, Build: build}
}

func newWrapcheck() *analysis.Analyzer {
	return wrapcheck.NewAnalyzer(wrapcheck.NewDefaultConfig())
}

func newContextcheck() *analysis.Analyzer {
	return contextcheck.NewAnalyzer(contextcheck.Configuration{})
}

func buildIface(config.Config) []*analysis.Analyzer {
	return []*analysis.Analyzer{
		identical.Analyzer,
		ifaceunused.Analyzer,
		unexported.Analyzer,
		unusedmethod.Analyzer,
	}
}
