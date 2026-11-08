package rules

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

const (
	TypecheckName = "typecheck"
	NolintName    = "nolint"
)

const (
	originGoAnalysis  = "go/analysis"
	originStaticcheck = "staticcheck"
	originThirdParty  = "third-party"
	originGVisor      = "gvisor"
	originLogos       = "logos"
	originGglint      = "gglint"
)

type Rule struct {
	Name   string
	Mode   config.Mode
	Origin string
	Doc    string
	Build  func(config.Config) []*analysis.Analyzer
}

func All() []Rule {
	all := slices.Concat(
		xtoolsRules(),
		staticcheckRules(),
		thirdPartyRules(),
		gvisorRules(),
		gglintRules(),
		coreRules(),
	)
	slices.SortFunc(all, func(a, b Rule) int {
		return cmp.Or(cmp.Compare(a.Mode, b.Mode), strings.Compare(a.Name, b.Name))
	})
	return all
}

func coreRules() []Rule {
	return []Rule{
		{
			Name:   TypecheckName,
			Mode:   config.Basic,
			Origin: originGglint,
			Doc:    "Reports package load and type errors; it cannot be disabled or suppressed.",
		},
		{
			Name:   NolintName,
			Mode:   config.Basic,
			Origin: originGglint,
			Doc:    "Checks //nolint directives for unknown rules, missing names or reasons, and unused suppressions.",
		},
	}
}

func Select(cfg config.Config) ([]Rule, error) {
	all := All()
	if err := validate(cfg, all); err != nil {
		return nil, pathError(cfg, err)
	}
	enabled := set(cfg.Enable)
	disabled := set(cfg.Disable)
	return slices.DeleteFunc(all, func(r Rule) bool {
		if r.Name == TypecheckName {
			return false
		}
		return disabled[r.Name] || (r.Mode > cfg.Mode && !enabled[r.Name])
	}), nil
}

func validate(cfg config.Config, all []Rule) error {
	known := make(map[string]bool, len(all))
	for _, r := range all {
		known[r.Name] = true
	}
	for _, name := range cfg.Enable {
		if !known[name] {
			return fmt.Errorf("enable: unknown rule %q", name)
		}
	}
	for _, name := range cfg.Disable {
		if !known[name] {
			return fmt.Errorf("disable: unknown rule %q", name)
		}
	}
	enabled := set(cfg.Enable)
	for _, name := range cfg.Disable {
		if enabled[name] {
			return fmt.Errorf("rule %q is in both enable and disable", name)
		}
		if name == TypecheckName {
			return fmt.Errorf("rule %q cannot be disabled", name)
		}
	}
	return validateExcludeRules(cfg.Excludes, known)
}

func validateExcludeRules(excludes []config.Exclude, known map[string]bool) error {
	for i, e := range excludes {
		for _, token := range e.Rules {
			rule, _, _ := strings.Cut(token, ":")
			if !known[rule] {
				return fmt.Errorf("exclude[%d].rules: unknown rule %q", i, rule)
			}
		}
	}
	return nil
}

func pathError(cfg config.Config, err error) error {
	if cfg.Path == "" {
		return err
	}
	return fmt.Errorf("%s: %w", cfg.Path, err)
}

func set(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

func AnalyzerName(r Rule, a *analysis.Analyzer, n int) string {
	if n == 1 {
		return r.Name
	}
	return r.Name + "_" + a.Name
}

func Token(analyzerName string, selected []Rule) (rule, check string) {
	for _, r := range selected {
		if r.Name == analyzerName {
			return r.Name, ""
		}
	}
	for _, r := range selected {
		if rest, ok := strings.CutPrefix(analyzerName, r.Name+"_"); ok {
			return r.Name, rest
		}
	}
	return analyzerName, ""
}
