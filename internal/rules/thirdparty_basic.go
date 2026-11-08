package rules

import (
	"strconv"

	"4d63.com/gocheckcompilerdirectives/checkcompilerdirectives"
	"github.com/breml/bidichk/pkg/bidichk"
	"github.com/gordonklaus/ineffassign/pkg/ineffassign"
	"github.com/kisielk/errcheck/errcheck"
	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

func thirdPartyBasicRules() []Rule {
	return []Rule{
		basicRule("errcheck", "Reports unchecked errors; standard adds blank assignments, strict adds unchecked type assertions.", buildErrcheck),
		basicRule("ineffassign", "Reports assignments to variables whose value is never used.", single(ineffassign.Analyzer)),
		basicRule("bidichk", "Reports dangerous Unicode bidirectional control characters in source.", construct(bidichk.NewAnalyzer)),
		basicRule("gocheckcompilerdirectives", "Reports malformed or unknown //go: compiler directives.", construct(checkcompilerdirectives.Analyzer)),
	}
}

func basicRule(name, doc string, build func(config.Config) []*analysis.Analyzer) Rule {
	return Rule{Name: name, Mode: config.Basic, Origin: "third-party", Doc: doc, Build: build}
}

func buildErrcheck(cfg config.Config) []*analysis.Analyzer {
	flags := map[string]string{
		"blank":  strconv.FormatBool(cfg.Mode >= config.Standard),
		"assert": strconv.FormatBool(cfg.Mode >= config.Strict),
	}
	return []*analysis.Analyzer{setFlags("errcheck", errcheck.Analyzer, flags)}
}
