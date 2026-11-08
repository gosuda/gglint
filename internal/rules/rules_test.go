package rules

import (
	"go/token"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/gosuda/gglint/internal/config"
)

const maxDocBytes = 120

func catalogConfig() config.Config {
	return config.Config{Mode: config.Strict, Complexity: config.ComplexityFor(config.Strict)}
}

func selectOrFatal(t *testing.T, cfg config.Config) []Rule {
	t.Helper()
	selected, err := Select(cfg)
	if err != nil {
		t.Fatalf("Select(mode %v): %v", cfg.Mode, err)
	}
	return selected
}

func selectNames(t *testing.T, cfg config.Config) map[string]bool {
	t.Helper()
	selected := selectOrFatal(t, cfg)
	names := make(map[string]bool, len(selected))
	for _, r := range selected {
		names[r.Name] = true
	}
	return names
}

func TestRuleNamesAreUniqueAndNonEmpty(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range All() {
		if r.Name == "" {
			t.Errorf("rule with doc %q has an empty name", r.Doc)
		}
		if seen[r.Name] {
			t.Errorf("rule %q is registered twice", r.Name)
		}
		seen[r.Name] = true
	}
}

func TestRuleDocsAreOneShortLine(t *testing.T) {
	for _, r := range All() {
		switch {
		case r.Doc == "":
			t.Errorf("rule %q has an empty doc", r.Name)
		case strings.ContainsAny(r.Doc, "\n\r\t"):
			t.Errorf("rule %q doc is not one tab-free line: %q", r.Name, r.Doc)
		case len(r.Doc) > maxDocBytes:
			t.Errorf("rule %q doc is %d bytes, over %d: %q", r.Name, len(r.Doc), maxDocBytes, r.Doc)
		}
	}
}

func TestOnlyDriverBuiltRulesLackAnalyzers(t *testing.T) {
	for _, r := range All() {
		driverBuilt := r.Name == TypecheckName || r.Name == NolintName
		if (r.Build == nil) != driverBuilt {
			t.Errorf("rule %q: Build is nil = %v, want %v", r.Name, r.Build == nil, driverBuilt)
		}
	}
}

func assertSubset(t *testing.T, smallName string, small map[string]bool, bigName string, big map[string]bool) {
	t.Helper()
	for name := range small {
		if !big[name] {
			t.Errorf("rule %q is selected in %s but not in %s", name, smallName, bigName)
		}
	}
	if len(small) >= len(big) {
		t.Errorf("%s selects %d rules, %s selects %d: want strictly more in %s", smallName, len(small), bigName, len(big), bigName)
	}
}

func assertModeSelection(t *testing.T, m config.Mode, selected map[string]bool) {
	t.Helper()
	for _, r := range All() {
		want := r.Mode <= m || r.Name == TypecheckName
		if selected[r.Name] != want {
			t.Errorf("mode %v: rule %q (mode %v) selected = %v, want %v", m, r.Name, r.Mode, selected[r.Name], want)
		}
	}
}

func TestModeSelectionsAreNested(t *testing.T) {
	selections := map[config.Mode]map[string]bool{}
	for _, m := range []config.Mode{config.Basic, config.Standard, config.Strict} {
		selections[m] = selectNames(t, config.Config{Mode: m})
		assertModeSelection(t, m, selections[m])
	}
	assertSubset(t, "basic", selections[config.Basic], "standard", selections[config.Standard])
	assertSubset(t, "standard", selections[config.Standard], "strict", selections[config.Strict])
}

func firstRuleOfMode(t *testing.T, m config.Mode) string {
	t.Helper()
	i := slices.IndexFunc(All(), func(r Rule) bool { return r.Mode == m })
	if i < 0 {
		t.Fatalf("no rule has mode %v", m)
	}
	return All()[i].Name
}

func TestSelectAppliesEnableAndDisable(t *testing.T) {
	extra := firstRuleOfMode(t, config.Strict)
	names := selectNames(t, config.Config{Mode: config.Basic, Enable: []string{extra}, Disable: []string{"printf"}})
	if !names[extra] {
		t.Errorf("enabled strict rule %q is not selected in basic", extra)
	}
	if names["printf"] {
		t.Error("disabled rule printf is still selected")
	}
	if !names[TypecheckName] {
		t.Errorf("rule %q is not selected", TypecheckName)
	}
}

func TestSelectReportsConfigErrors(t *testing.T) {
	const path = "/p/gglint.toml"
	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{"unknown enable", config.Config{Path: path, Enable: []string{"nosuchrule"}}, path + `: enable: unknown rule "nosuchrule"`},
		{"unknown disable", config.Config{Path: path, Disable: []string{"nosuchrule"}}, path + `: disable: unknown rule "nosuchrule"`},
		{"unknown enable without file", config.Config{Enable: []string{"nosuchrule"}}, `enable: unknown rule "nosuchrule"`},
		{"enable and disable", config.Config{Path: path, Enable: []string{"printf"}, Disable: []string{"printf"}}, path + `: rule "printf" is in both enable and disable`},
		{"disable typecheck", config.Config{Path: path, Disable: []string{TypecheckName}}, path + `: rule "typecheck" cannot be disabled`},
		{"unknown exclude rule", config.Config{Path: path, Excludes: []config.Exclude{{Rules: []string{"nosuchrule:X1"}}}}, path + `: exclude[0].rules: unknown rule "nosuchrule"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Select(tt.cfg)
			if err == nil || err.Error() != tt.want {
				t.Errorf("Select error = %v, want %q", err, tt.want)
			}
		})
	}
}

func checkBuiltNames(t *testing.T, r Rule, built []*analysis.Analyzer, owners map[string]string) {
	t.Helper()
	if len(built) == 0 {
		t.Errorf("rule %q builds no analyzers", r.Name)
	}
	for _, a := range built {
		name := AnalyzerName(r, a, len(built))
		if !token.IsIdentifier(name) {
			t.Errorf("rule %q: analyzer name %q is not a Go identifier", r.Name, name)
		}
		if owner, taken := owners[name]; taken {
			t.Errorf("rule %q: analyzer name %q is already used by rule %q", r.Name, name, owner)
		}
		owners[name] = r.Name
	}
}

func TestFullCatalogAnalyzersAreValid(t *testing.T) {
	cfg := catalogConfig()
	selected := selectOrFatal(t, cfg)
	if len(selected) != len(All()) {
		t.Fatalf("strict selects %d of %d rules", len(selected), len(All()))
	}
	owners := map[string]string{}
	var analyzers []*analysis.Analyzer
	for _, r := range selected {
		if r.Build == nil {
			continue
		}
		built := r.Build(cfg)
		checkBuiltNames(t, r, built, owners)
		analyzers = append(analyzers, built...)
	}
	if err := analysis.Validate(analyzers); err != nil {
		t.Fatalf("analysis.Validate: %v", err)
	}
}

func TestTokenRecoversRuleAndCheck(t *testing.T) {
	cfg := catalogConfig()
	selected := selectOrFatal(t, cfg)
	for _, r := range selected {
		if r.Build == nil {
			continue
		}
		built := r.Build(cfg)
		for _, a := range built {
			wantCheck := ""
			if len(built) > 1 {
				wantCheck = a.Name
			}
			rule, check := Token(AnalyzerName(r, a, len(built)), selected)
			if rule != r.Name || check != wantCheck {
				t.Errorf("Token for rule %q analyzer %q = (%q, %q), want (%q, %q)", r.Name, a.Name, rule, check, r.Name, wantCheck)
			}
		}
	}
}

func findRule(t *testing.T, selected []Rule, name string) Rule {
	t.Helper()
	i := slices.IndexFunc(selected, func(r Rule) bool { return r.Name == name })
	if i < 0 {
		t.Fatalf("rule %q is not selected", name)
	}
	return selected[i]
}

func findAnalyzer(t *testing.T, built []*analysis.Analyzer, name string) *analysis.Analyzer {
	t.Helper()
	i := slices.IndexFunc(built, func(a *analysis.Analyzer) bool { return a.Name == name })
	if i < 0 {
		t.Fatalf("no built analyzer is named %q", name)
	}
	return built[i]
}

func printedToken(rule, check string) string {
	if check == "" {
		return rule
	}
	return rule + ":" + check
}

func TestAnalyzerNamesPrintAsPlanTokens(t *testing.T) {
	cfg := catalogConfig()
	selected := selectOrFatal(t, cfg)
	tests := []struct {
		rule, upstream, wantName, wantToken string
	}{
		{"printf", "printf", "printf", "printf"},
		{"gocritic", "ruleguard", "gocritic", "gocritic"},
		{"gochecksumtype", "sumtype", "gochecksumtype", "gochecksumtype"},
		{"staticcheck", "SA1015", "staticcheck_SA1015", "staticcheck:SA1015"},
		{"modernize", "forvar", "modernize_forvar", "modernize:forvar"},
		{"iface", "identical", "iface_identical", "iface:identical"},
	}
	for _, tt := range tests {
		t.Run(tt.wantToken, func(t *testing.T) {
			r := findRule(t, selected, tt.rule)
			built := r.Build(cfg)
			name := AnalyzerName(r, findAnalyzer(t, built, tt.upstream), len(built))
			if name != tt.wantName {
				t.Errorf("AnalyzerName = %q, want %q", name, tt.wantName)
			}
			if got := printedToken(Token(name, selected)); got != tt.wantToken {
				t.Errorf("token = %q, want %q", got, tt.wantToken)
			}
		})
	}
}
