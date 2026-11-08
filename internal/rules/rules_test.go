package rules_test

import (
	"fmt"
	"go/token"
	"math/rand"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/quick"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/quickfix"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

const maxDocBytes = 120

func allModes() []config.Mode {
	return []config.Mode{config.Basic, config.Standard, config.Strict}
}

func modeConfig(m config.Mode) config.Config {
	return config.Config{Mode: m, Complexity: config.ComplexityFor(m)}
}

func selectOrFatal(t *testing.T, cfg config.Config) []rules.Rule {
	t.Helper()
	selected, err := rules.Select(cfg)
	if err != nil {
		t.Fatalf("Select(mode %v): %v", cfg.Mode, err)
	}
	return selected
}

func ruleNames(rs []rules.Rule) []string {
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name
	}
	return names
}

func findRule(t *testing.T, name string) rules.Rule {
	t.Helper()
	all := rules.All()
	i := slices.IndexFunc(all, func(r rules.Rule) bool { return r.Name == name })
	if i < 0 {
		t.Fatalf("catalog has no rule %q", name)
	}
	return all[i]
}

func checkNames(built []*analysis.Analyzer) []string {
	names := make([]string, len(built))
	for i, a := range built {
		names[i] = a.Name
	}
	slices.Sort(names)
	return names
}

func readme(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	return string(data)
}

func tableCells(line string) []string {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	cells := strings.Split(strings.Trim(line, "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

type documentedRule struct {
	Mode   config.Mode
	Origin string
	Doc    string
}

func readmeRuleTables(t *testing.T) map[string]documentedRule {
	t.Helper()
	sections := map[string]config.Mode{
		"### basic":             config.Basic,
		"### Added in standard": config.Standard,
		"### Added in strict":   config.Strict,
	}
	out := map[string]documentedRule{}
	mode, inRules := config.Basic, false
	for line := range strings.Lines(readme(t)) {
		if strings.HasPrefix(line, "#") {
			mode, inRules = sections[strings.TrimSpace(line)]
			continue
		}
		cells := tableCells(line)
		if !inRules || len(cells) != 3 || !strings.HasPrefix(cells[0], "`") {
			continue
		}
		name := strings.Trim(cells[0], "`")
		if _, dup := out[name]; dup {
			t.Errorf("README lists rule %q twice", name)
		}
		out[name] = documentedRule{mode, cells[1], strings.ReplaceAll(cells[2], `\*`, "*")}
	}
	if len(out) == 0 {
		t.Fatal("README has no rule tables")
	}
	return out
}

func TestCatalogNamesModesOriginsAndDocsMatchReadmeTables(t *testing.T) {
	documented := readmeRuleTables(t)
	seen := map[string]bool{}
	for _, r := range rules.All() {
		if seen[r.Name] {
			t.Errorf("rule %q is registered twice", r.Name)
		}
		seen[r.Name] = true
		want, ok := documented[r.Name]
		if !ok {
			t.Errorf("rule %q is missing from the README rule tables", r.Name)
			continue
		}
		if got := (documentedRule{r.Mode, r.Origin, r.Doc}); got != want {
			t.Errorf("rule %q = {%v %q %q}, README documents {%v %q %q}", r.Name, got.Mode, got.Origin, got.Doc, want.Mode, want.Origin, want.Doc)
		}
	}
	for name := range documented {
		if !seen[name] {
			t.Errorf("README documents rule %q, which the catalog lacks", name)
		}
	}
}

func TestSelectedRuleCountsPerModeMatchReadme(t *testing.T) {
	re := regexp.MustCompile("The catalog has (\\d+) rules\\. `basic` runs (\\d+) of them, `standard` runs (\\d+), and `strict` runs all (\\d+)\\.")
	m := re.FindStringSubmatch(readme(t))
	if m == nil {
		t.Fatal("README does not state the per-mode rule counts")
	}
	labels := []string{"catalog", "basic", "standard", "strict"}
	got := []int{len(rules.All())}
	for _, mode := range allModes() {
		got = append(got, len(selectOrFatal(t, config.Config{Mode: mode})))
	}
	for i, g := range got {
		want, err := strconv.Atoi(m[i+1])
		if err != nil {
			t.Fatalf("README %s count %q: %v", labels[i], m[i+1], err)
		}
		if g != want {
			t.Errorf("%s has %d rules, README states %d", labels[i], g, want)
		}
	}
}

func readmeDroppedLinters(t *testing.T) []string {
	t.Helper()
	tick := regexp.MustCompile("`([^`]+)`")
	var out []string
	inDropped := false
	for line := range strings.Lines(readme(t)) {
		cells := tableCells(line)
		switch {
		case cells == nil:
			inDropped = false
		case cells[len(cells)-1] == "Dropped linters":
			inDropped = true
		case inDropped:
			for _, sub := range tick.FindAllStringSubmatch(cells[len(cells)-1], -1) {
				out = append(out, sub[1])
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("README has no dropped-linter tables")
	}
	return out
}

func TestCatalogOmitsEveryDroppedOrRenamedGolangciLinter(t *testing.T) {
	renamedOrFormatters := []string{"govet", "nolintlint", "gochecknoinits", "gci", "gofmt", "goimports", "golines", "swaggo"}
	catalog := ruleNames(rules.All())
	for _, name := range slices.Concat(readmeDroppedLinters(t), renamedOrFormatters) {
		if slices.Contains(catalog, name) {
			t.Errorf("README drops or renames golangci-lint linter %q, but the catalog registers it", name)
		}
	}
}

func TestAllIsStrictlySortedByModeThenName(t *testing.T) {
	all := rules.All()
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if prev.Mode > cur.Mode || (prev.Mode == cur.Mode && prev.Name >= cur.Name) {
			t.Errorf("All()[%d] = (%v, %q) does not sort after All()[%d] = (%v, %q)", i, cur.Mode, cur.Name, i-1, prev.Mode, prev.Name)
		}
	}
}

func TestRuleDocsAreOneTabFreeLineOfAtMost120Bytes(t *testing.T) {
	for _, r := range rules.All() {
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

func TestBuildIsNilExactlyForDriverBuiltRules(t *testing.T) {
	for _, r := range rules.All() {
		driverBuilt := r.Name == rules.TypecheckName || r.Name == rules.NolintName
		if (r.Build == nil) != driverBuilt {
			t.Errorf("rule %q: Build is nil = %v, want %v", r.Name, r.Build == nil, driverBuilt)
		}
	}
}

func wantSelection(m config.Mode, enable, disable []string) []string {
	var out []string
	for _, r := range rules.All() {
		in := r.Mode <= m || slices.Contains(enable, r.Name) || r.Name == rules.TypecheckName
		if in && !slices.Contains(disable, r.Name) {
			out = append(out, r.Name)
		}
	}
	return out
}

func selectionDiff(got, want []string) string {
	extra := slices.DeleteFunc(slices.Clone(got), func(n string) bool { return slices.Contains(want, n) })
	missing := slices.DeleteFunc(slices.Clone(want), func(n string) bool { return slices.Contains(got, n) })
	if len(extra) == 0 && len(missing) == 0 {
		return fmt.Sprintf("order or duplicates differ: got %q", got)
	}
	return fmt.Sprintf("missing %q, extra %q", missing, extra)
}

func assertSelectionLaw(t *testing.T, cfg config.Config) {
	t.Helper()
	got, err := rules.Select(cfg)
	if err != nil {
		t.Errorf("Select(mode %v, enable %q, disable %q): %v", cfg.Mode, cfg.Enable, cfg.Disable, err)
		return
	}
	want := wantSelection(cfg.Mode, cfg.Enable, cfg.Disable)
	if names := ruleNames(got); !slices.Equal(names, want) {
		t.Errorf("Select(mode %v, enable %q, disable %q): %s", cfg.Mode, cfg.Enable, cfg.Disable, selectionDiff(names, want))
	}
}

func TestSelectIsModeClosurePlusEnableMinusDisableForEveryRuleAtEveryMode(t *testing.T) {
	for _, m := range allModes() {
		assertSelectionLaw(t, config.Config{Mode: m})
		for _, r := range rules.All() {
			assertSelectionLaw(t, config.Config{Mode: m, Enable: []string{r.Name}})
			if r.Name != rules.TypecheckName {
				assertSelectionLaw(t, config.Config{Mode: m, Disable: []string{r.Name}})
			}
		}
	}
}

func TestModeSelectionsAreStrictlyNested(t *testing.T) {
	var prev []string
	for _, m := range allModes() {
		cur := ruleNames(selectOrFatal(t, config.Config{Mode: m}))
		for _, name := range prev {
			if !slices.Contains(cur, name) {
				t.Errorf("rule %q is selected in a lower mode but not in %v", name, m)
			}
		}
		if prev != nil && len(cur) <= len(prev) {
			t.Errorf("mode %v selects %d rules, not more than the %d of the mode below", m, len(cur), len(prev))
		}
		prev = cur
	}
}

type selectionInput struct {
	Mode    config.Mode
	Enable  []string
	Disable []string
}

func (selectionInput) Generate(r *rand.Rand, _ int) reflect.Value {
	in := selectionInput{Mode: config.Mode(r.Intn(3))}
	for _, rule := range rules.All() {
		switch r.Intn(4) {
		case 0:
			in.Enable = append(in.Enable, rule.Name, rule.Name)
		case 1:
			if rule.Name != rules.TypecheckName {
				in.Disable = append(in.Disable, rule.Name)
			}
		}
	}
	r.Shuffle(len(in.Enable), func(i, j int) { in.Enable[i], in.Enable[j] = in.Enable[j], in.Enable[i] })
	r.Shuffle(len(in.Disable), func(i, j int) { in.Disable[i], in.Disable[j] = in.Disable[j], in.Disable[i] })
	return reflect.ValueOf(in)
}

func selectNamesOrNil(cfg config.Config) []string {
	got, err := rules.Select(cfg)
	if err != nil {
		return nil
	}
	return ruleNames(got)
}

func TestSelectLawHoldsForRandomEnableDisableSetsInAnyOrder(t *testing.T) {
	law := func(in selectionInput) bool {
		enable, disable := slices.Clone(in.Enable), slices.Clone(in.Disable)
		got := selectNamesOrNil(config.Config{Mode: in.Mode, Enable: in.Enable, Disable: in.Disable})
		unchanged := slices.Equal(enable, in.Enable) && slices.Equal(disable, in.Disable)
		slices.Reverse(enable)
		slices.Reverse(disable)
		reordered := selectNamesOrNil(config.Config{Mode: in.Mode, Enable: enable, Disable: disable})
		want := wantSelection(in.Mode, in.Enable, in.Disable)
		return unchanged && slices.Equal(got, want) && slices.Equal(reordered, want)
	}
	if err := quick.Check(law, &quick.Config{MaxCount: 300, Rand: rand.New(rand.NewSource(1))}); err != nil {
		t.Error(err)
	}
}

type selectErrorCase struct {
	name string
	cfg  config.Config
	want string
}

func unknownNameCases(path string) []selectErrorCase {
	var cases []selectErrorCase
	for _, bad := range []string{"", "nosuchrule", "Printf", " printf", "printf,errcheck", "staticcheck:SA1015", "err_chain", "structtag_", "govet"} {
		cases = append(cases,
			selectErrorCase{"enable " + strconv.Quote(bad), config.Config{Path: path, Enable: []string{"printf", bad}}, fmt.Sprintf("%s: enable: unknown rule %q", path, bad)},
			selectErrorCase{"disable " + strconv.Quote(bad), config.Config{Path: path, Disable: []string{"printf", bad}}, fmt.Sprintf("%s: disable: unknown rule %q", path, bad)},
		)
	}
	return cases
}

func conflictCases(path string) []selectErrorCase {
	var cases []selectErrorCase
	for _, m := range allModes() {
		for _, name := range []string{"printf", "chanblock", "err_chain_reset"} {
			cases = append(cases, selectErrorCase{
				fmt.Sprintf("%v enable and disable %s", m, name),
				config.Config{Path: path, Mode: m, Enable: []string{name}, Disable: []string{name}},
				fmt.Sprintf("%s: rule %q is in both enable and disable", path, name),
			})
		}
		for _, disable := range [][]string{{rules.TypecheckName}, {"printf", rules.TypecheckName}} {
			cases = append(cases, selectErrorCase{
				fmt.Sprintf("%v disable %q", m, disable),
				config.Config{Path: path, Mode: m, Disable: disable},
				path + `: rule "typecheck" cannot be disabled`,
			})
		}
	}
	return cases
}

func excludeCases(path string) []selectErrorCase {
	return []selectErrorCase{
		{"unknown exclude rule in second entry", config.Config{Path: path, Excludes: []config.Exclude{{Rules: []string{"printf"}}, {Rules: []string{"staticcheck:SA1019", "nosuchrule:X1"}}}}, path + `: exclude[1].rules: unknown rule "nosuchrule"`},
		{"exclude token prefix of underscore rule", config.Config{Path: path, Excludes: []config.Exclude{{Rules: []string{"err_chain:reset"}}}}, path + `: exclude[0].rules: unknown rule "err_chain"`},
		{"exclude token without rule", config.Config{Path: path, Excludes: []config.Exclude{{Rules: []string{":SA1019"}}}}, path + `: exclude[0].rules: unknown rule ""`},
		{"unknown enable without config file", config.Config{Enable: []string{"nosuchrule"}}, `enable: unknown rule "nosuchrule"`},
		{"unknown disable without config file", config.Config{Disable: []string{"nosuchrule"}}, `disable: unknown rule "nosuchrule"`},
	}
}

func TestSelectRejectsInvalidConfigWithDocumentedMessageAndNoRules(t *testing.T) {
	const path = "/p/gglint.toml"
	for _, tt := range slices.Concat(unknownNameCases(path), conflictCases(path), excludeCases(path)) {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rules.Select(tt.cfg)
			if err == nil || err.Error() != tt.want {
				t.Errorf("Select error = %v, want %q", err, tt.want)
			}
			if got != nil {
				t.Errorf("Select returned %d rules alongside an error", len(got))
			}
		})
	}
}

func TestSelectAcceptsExcludeTokensWithoutChangingSelection(t *testing.T) {
	excludes := []config.Exclude{{Rules: []string{"printf", "staticcheck:SA1019", "err_chain_reset", rules.NolintName, "chanblock:anything"}}}
	for _, m := range []config.Mode{config.Basic, config.Standard} {
		assertSelectionLaw(t, config.Config{Path: "/p/gglint.toml", Mode: m, Excludes: excludes})
	}
}

func fullCatalogConfig(m config.Mode) config.Config {
	cfg := modeConfig(m)
	cfg.Enable = ruleNames(rules.All())
	return cfg
}

func buildAll(t *testing.T, cfg config.Config, owners map[string]string) []*analysis.Analyzer {
	t.Helper()
	var analyzers []*analysis.Analyzer
	for _, r := range selectOrFatal(t, cfg) {
		if r.Build == nil {
			continue
		}
		built := r.Build(cfg)
		if len(built) == 0 {
			t.Errorf("mode %v: rule %q builds no analyzers", cfg.Mode, r.Name)
		}
		for _, a := range built {
			name := rules.AnalyzerName(r, a, len(built))
			if !token.IsIdentifier(name) {
				t.Errorf("mode %v: rule %q: analyzer name %q is not a Go identifier", cfg.Mode, r.Name, name)
			}
			if owner, taken := owners[name]; taken {
				t.Errorf("mode %v: rule %q: analyzer name %q is already used by %q", cfg.Mode, r.Name, name, owner)
			}
			owners[name] = r.Name
		}
		analyzers = append(analyzers, built...)
	}
	return analyzers
}

func TestFullCatalogAnalyzerNamesAreUniqueIdentifiersAndValidateAtEveryMode(t *testing.T) {
	for _, m := range allModes() {
		cfg := fullCatalogConfig(m)
		if n := len(selectOrFatal(t, cfg)); n != len(rules.All()) {
			t.Fatalf("mode %v with every rule enabled selects %d of %d rules", m, n, len(rules.All()))
		}
		owners := map[string]string{rules.TypecheckName: "the driver", rules.NolintName: "the driver"}
		if err := analysis.Validate(buildAll(t, cfg, owners)); err != nil {
			t.Errorf("mode %v: analysis.Validate: %v", m, err)
		}
	}
}

func shuffledOrders(selected []rules.Rule) [][]rules.Rule {
	reversed := slices.Clone(selected)
	slices.Reverse(reversed)
	shuffled := slices.Clone(selected)
	rand.New(rand.NewSource(7)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	return [][]rules.Rule{selected, reversed, shuffled}
}

func assertTokenRoundTrip(t *testing.T, r rules.Rule, built []*analysis.Analyzer, orders [][]rules.Rule) {
	t.Helper()
	for _, a := range built {
		wantName, wantCheck := r.Name, ""
		if len(built) > 1 {
			wantName, wantCheck = r.Name+"_"+a.Name, a.Name
		}
		name := rules.AnalyzerName(r, a, len(built))
		if name != wantName {
			t.Errorf("AnalyzerName(%q, %q, %d) = %q, want %q", r.Name, a.Name, len(built), name, wantName)
		}
		for i, order := range orders {
			if rule, check := rules.Token(name, order); rule != r.Name || check != wantCheck {
				t.Errorf("order %d: Token(%q) = (%q, %q), want (%q, %q)", i, name, rule, check, r.Name, wantCheck)
			}
		}
	}
}

func TestTokenInvertsAnalyzerNameForEveryBuiltAnalyzerInAnyRuleOrder(t *testing.T) {
	strict := ruleNames(selectOrFatal(t, modeConfig(config.Strict)))
	for _, name := range []string{"err_chain_reset", "discarded_error", "infinite_recursion", "structtag_malformed", "reentrant_withlock_deadlock"} {
		if !slices.Contains(strict, name) {
			t.Fatalf("strict does not select underscored rule %q, so the round trip cannot cover it", name)
		}
	}
	for _, m := range allModes() {
		cfg := modeConfig(m)
		selected := selectOrFatal(t, cfg)
		orders := shuffledOrders(selected)
		for _, r := range selected {
			if r.Build != nil {
				assertTokenRoundTrip(t, r, r.Build(cfg), orders)
			}
		}
		for _, driver := range []string{rules.TypecheckName, rules.NolintName} {
			for i, order := range orders {
				if rule, check := rules.Token(driver, order); rule != driver || check != "" {
					t.Errorf("mode %v order %d: Token(%q) = (%q, %q), want (%q, \"\")", m, i, driver, rule, check, driver)
				}
			}
		}
	}
}

func findBuilt(t *testing.T, built []*analysis.Analyzer, upstream string) *analysis.Analyzer {
	t.Helper()
	i := slices.IndexFunc(built, func(a *analysis.Analyzer) bool { return a.Name == upstream })
	if i < 0 {
		t.Fatalf("no built analyzer is named %q", upstream)
	}
	return built[i]
}

func TestReadmeTokensPrintFromUpstreamAnalyzerNames(t *testing.T) {
	cfg := modeConfig(config.Strict)
	selected := selectOrFatal(t, cfg)
	tests := []struct{ rule, upstream, wantToken string }{
		{"printf", "printf", "printf"},
		{"gocritic", "ruleguard", "gocritic"},
		{"gochecksumtype", "sumtype", "gochecksumtype"},
		{"unused", "U1000", "unused"},
		{"staticcheck", "SA1015", "staticcheck:SA1015"},
		{"staticcheck", "SA2002", "staticcheck:SA2002"},
		{"staticcheck", "SA1008", "staticcheck:SA1008"},
		{"modernize", "forvar", "modernize:forvar"},
		{"modernize", "rangeint", "modernize:rangeint"},
		{"modernize", "waitgroupgo", "modernize:waitgroupgo"},
		{"iface", "identical", "iface:identical"},
	}
	for _, tt := range tests {
		t.Run(tt.wantToken, func(t *testing.T) {
			r := findRule(t, tt.rule)
			built := r.Build(cfg)
			rule, check := rules.Token(rules.AnalyzerName(r, findBuilt(t, built, tt.upstream), len(built)), selected)
			if got := strings.TrimSuffix(rule+":"+check, ":"); got != tt.wantToken {
				t.Errorf("token = %q, want %q", got, tt.wantToken)
			}
		})
	}
}

func upstreamStaticcheckFamilies() map[string]string {
	lists := map[string][]*lint.Analyzer{"SA": staticcheck.Analyzers, "S": simple.Analyzers, "ST": stylecheck.Analyzers, "QF": quickfix.Analyzers}
	families := map[string]string{}
	for family, list := range lists {
		for _, a := range list {
			families[a.Analyzer.Name] = family
		}
	}
	return families
}

func familiesOf(names []string, families map[string]string) []string {
	var out []string
	for _, n := range names {
		out = append(out, families[n])
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func TestStaticcheckRunsEverySACheckInBasicAndAddsReadmeFamiliesPerModeNested(t *testing.T) {
	families := upstreamStaticcheckFamilies()
	wantFamilies := map[config.Mode][]string{
		config.Basic:    {"SA"},
		config.Standard: {"S", "SA", "ST"},
		config.Strict:   {"QF", "S", "SA", "ST"},
	}
	r := findRule(t, "staticcheck")
	var below []string
	for _, m := range allModes() {
		got := checkNames(r.Build(modeConfig(m)))
		if fams := familiesOf(got, families); !slices.Equal(fams, wantFamilies[m]) {
			t.Errorf("mode %v: staticcheck check families = %q, want %q", m, fams, wantFamilies[m])
		}
		for _, name := range below {
			if !slices.Contains(got, name) {
				t.Errorf("staticcheck check %s runs below %v but not in %v", name, m, m)
			}
		}
		below = got
	}
	sa := make([]*analysis.Analyzer, len(staticcheck.Analyzers))
	for i, a := range staticcheck.Analyzers {
		sa[i] = a.Analyzer
	}
	if got, want := checkNames(r.Build(modeConfig(config.Basic))), checkNames(sa); !slices.Equal(got, want) {
		t.Errorf("basic staticcheck checks %s", selectionDiff(got, want))
	}
}

func TestIfaceBuildsTheDocumentedChecksAtEveryMode(t *testing.T) {
	iface := findRule(t, "iface")
	wantIface := []string{"identical", "unexported", "unused", "unusedmethod"}
	for _, m := range allModes() {
		if got := checkNames(iface.Build(modeConfig(m))); !slices.Equal(got, wantIface) {
			t.Errorf("mode %v: iface checks %s", m, selectionDiff(got, wantIface))
		}
	}
}

func flagValue(t *testing.T, rule string, cfg config.Config, flag string) string {
	t.Helper()
	built := findRule(t, rule).Build(cfg)
	if len(built) != 1 {
		t.Fatalf("rule %q builds %d analyzers, want 1", rule, len(built))
	}
	f := built[0].Flags.Lookup(flag)
	if f == nil {
		t.Fatalf("rule %q analyzer has no flag %q", rule, flag)
	}
	return f.Value.String()
}

func TestModeDependentFlagsFollowTheModeOfTheLatestBuild(t *testing.T) {
	tests := []struct {
		rule, flag string
		want       [3]bool
	}{
		{"errcheck", "blank", [3]bool{false, true, true}},
		{"errcheck", "assert", [3]bool{false, false, true}},
		{"checklocks", "inferred", [3]bool{false, false, true}},
		{"checklocks", "atomic", [3]bool{true, true, true}},
		{"checklocks", "wrappers", [3]bool{true, true, true}},
		{"errorlint", "errorf", [3]bool{true, true, true}},
	}
	for _, m := range []config.Mode{config.Strict, config.Basic, config.Standard, config.Strict} {
		for _, tt := range tests {
			if got, want := flagValue(t, tt.rule, modeConfig(m), tt.flag), strconv.FormatBool(tt.want[m]); got != want {
				t.Errorf("mode %v: %s flag %s = %q, want %q", m, tt.rule, tt.flag, got, want)
			}
		}
	}
}

func TestGocriticEnablesDiagnosticsAndDeferInLoopAndAddsOpinionatedAndPerformanceOnlyInStrict(t *testing.T) {
	tests := []struct {
		checker string
		want    [3]bool
	}{
		{"appendAssign", [3]bool{true, true, true}},
		{"deferInLoop", [3]bool{true, true, true}},
		{"hugeParam", [3]bool{false, false, true}},
		{"paramTypeCombine", [3]bool{false, false, true}},
	}
	for _, m := range []config.Mode{config.Strict, config.Basic, config.Standard} {
		cfg := modeConfig(m)
		enabled := strings.Split(flagValue(t, "gocritic", cfg, "enable"), ",")
		for _, tt := range tests {
			if got := slices.Contains(enabled, tt.checker); got != tt.want[m] {
				t.Errorf("mode %v: gocritic enables %s = %v, want %v", m, tt.checker, got, tt.want[m])
			}
		}
		if disabled := flagValue(t, "gocritic", cfg, "disable"); disabled != "" {
			t.Errorf("mode %v: gocritic disable = %q, want empty", m, disabled)
		}
	}
}
