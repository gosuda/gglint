package rules

import (
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/appends"
	"golang.org/x/tools/go/analysis/passes/asmdecl"
	"golang.org/x/tools/go/analysis/passes/assign"
	"golang.org/x/tools/go/analysis/passes/atomic"
	"golang.org/x/tools/go/analysis/passes/atomicalign"
	"golang.org/x/tools/go/analysis/passes/bools"
	"golang.org/x/tools/go/analysis/passes/buildtag"
	"golang.org/x/tools/go/analysis/passes/cgocall"
	"golang.org/x/tools/go/analysis/passes/composite"
	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/go/analysis/passes/deepequalerrors"
	"golang.org/x/tools/go/analysis/passes/defers"
	"golang.org/x/tools/go/analysis/passes/directive"
	"golang.org/x/tools/go/analysis/passes/errorsas"
	"golang.org/x/tools/go/analysis/passes/framepointer"
	"golang.org/x/tools/go/analysis/passes/gofix"
	"golang.org/x/tools/go/analysis/passes/hostport"
	"golang.org/x/tools/go/analysis/passes/httpmux"
	"golang.org/x/tools/go/analysis/passes/httpresponse"
	"golang.org/x/tools/go/analysis/passes/ifaceassert"
	"golang.org/x/tools/go/analysis/passes/inline"
	"golang.org/x/tools/go/analysis/passes/loopclosure"
	"golang.org/x/tools/go/analysis/passes/lostcancel"
	"golang.org/x/tools/go/analysis/passes/modernize"
	"golang.org/x/tools/go/analysis/passes/nilfunc"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/analysis/passes/reflectvaluecompare"
	"golang.org/x/tools/go/analysis/passes/scannererr"
	"golang.org/x/tools/go/analysis/passes/shift"
	"golang.org/x/tools/go/analysis/passes/sigchanyzer"
	"golang.org/x/tools/go/analysis/passes/slog"
	"golang.org/x/tools/go/analysis/passes/sortslice"
	"golang.org/x/tools/go/analysis/passes/sqlrowserr"
	"golang.org/x/tools/go/analysis/passes/stdmethods"
	"golang.org/x/tools/go/analysis/passes/stdversion"
	"golang.org/x/tools/go/analysis/passes/stringintconv"
	"golang.org/x/tools/go/analysis/passes/structtag"
	"golang.org/x/tools/go/analysis/passes/testinggoroutine"
	"golang.org/x/tools/go/analysis/passes/tests"
	"golang.org/x/tools/go/analysis/passes/timeformat"
	"golang.org/x/tools/go/analysis/passes/unmarshal"
	"golang.org/x/tools/go/analysis/passes/unreachable"
	"golang.org/x/tools/go/analysis/passes/unsafeptr"
	"golang.org/x/tools/go/analysis/passes/unusedresult"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/analysis/passes/waitgroup"

	"github.com/gosuda/gglint/internal/config"
)

func xtoolsRules() []Rule {
	basic := []*analysis.Analyzer{
		appends.Analyzer, asmdecl.Analyzer, assign.Analyzer, atomic.Analyzer, bools.Analyzer,
		buildtag.Analyzer, cgocall.Analyzer, composite.Analyzer, copylock.Analyzer, defers.Analyzer,
		directive.Analyzer, errorsas.Analyzer, framepointer.Analyzer, gofix.Analyzer, hostport.Analyzer,
		httpresponse.Analyzer, ifaceassert.Analyzer, loopclosure.Analyzer, lostcancel.Analyzer,
		nilfunc.Analyzer, printf.Analyzer, shift.Analyzer, sigchanyzer.Analyzer, slog.Analyzer,
		stdmethods.Analyzer, stdversion.Analyzer, stringintconv.Analyzer, structtag.Analyzer,
		testinggoroutine.Analyzer, tests.Analyzer, timeformat.Analyzer, unmarshal.Analyzer,
		unreachable.Analyzer, unsafeptr.Analyzer, unusedresult.Analyzer, waitgroup.Analyzer,
	}
	standard := []*analysis.Analyzer{
		atomicalign.Analyzer, deepequalerrors.Analyzer, httpmux.Analyzer, inline.Analyzer,
		nilness.Analyzer, reflectvaluecompare.Analyzer, scannererr.Analyzer, sortslice.Analyzer,
		sqlrowserr.Analyzer, unusedwrite.Analyzer,
	}
	out := []Rule{modernizeRule()}
	for _, a := range basic {
		out = append(out, passRule(config.Basic, a))
	}
	for _, a := range standard {
		out = append(out, passRule(config.Standard, a))
	}
	return out
}

func passRule(mode config.Mode, a *analysis.Analyzer) Rule {
	return Rule{
		Name:   a.Name,
		Mode:   mode,
		Origin: "go/analysis",
		Doc:    firstLine(a.Doc),
		Build:  single(a),
	}
}

func modernizeRule() Rule {
	return Rule{
		Name:   "modernize",
		Mode:   config.Standard,
		Origin: "go/analysis",
		Doc:    "Suggests modern Go replacements for outdated idioms and library calls.",
		Build: func(config.Config) []*analysis.Analyzer {
			return slices.DeleteFunc(slices.Clone(modernize.Suite), func(a *analysis.Analyzer) bool {
				return a == modernize.AtomicTypesAnalyzer
			})
		},
	}
}

func firstLine(doc string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(doc), "\n")
	return strings.Join(strings.Fields(line), " ")
}
