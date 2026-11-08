package complexity_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/complexity"
	"github.com/gosuda/gglint/internal/config"
)

func TestMetricDefinitionsMatchReadmeAtLimitBoundary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		property string
		pkg      string
		limits   config.Complexity
	}{
		{"nesting_counts_if_for_switch_select_not_cases_else_if_or_literals", "nesting", config.Complexity{MaxNesting: 3}},
		{"cognitive_adds_one_plus_levels_else_if_same_level_chain_switches_only", "cognitive", config.Complexity{MaxCognitive: 5}},
		{"cyclomatic_counts_if_loops_conditioned_cases_and_every_logical_op", "cyclomatic", config.Complexity{MaxCyclomatic: 4}},
		{"lines_run_from_func_line_through_closing_brace_including_literals", "lines", config.Complexity{MaxLines: 10}},
		{"params_count_names_unnamed_as_one_without_receiver_bools_exported_only", "params", config.Complexity{MaxParams: 3, MaxBoolParams: 1}},
		{"interface_counts_explicit_methods_of_named_interfaces", "interfaces", config.Complexity{MaxInterfaceMethods: 3}},
		{"file_lines_report_at_package_clause_above_max", "filelines", config.Complexity{MaxFileLines: 20}},
		{"test_files_skip_line_and_file_limits_only", "testskip", config.Complexity{MaxNesting: 2, MaxLines: 12, MaxFileLines: 25}},
		{"one_finding_lists_exceeded_limits_in_documented_order", "order", config.Complexity{
			MaxCognitive: 2, MaxCyclomatic: 2, MaxNesting: 1, MaxLines: 6, MaxParams: 1, MaxBoolParams: 1,
		}},
		{"zero_limit_disables_only_its_own_check_set_a", "zeroa", config.Complexity{
			MaxParams: 1, MaxCognitive: 1, MaxBoolParams: 1, MaxFileLines: 1,
		}},
		{"zero_limit_disables_only_its_own_check_set_b", "zerob", config.Complexity{
			MaxNesting: 1, MaxLines: 1, MaxCyclomatic: 1, MaxInterfaceMethods: 1,
		}},
	}
	for _, c := range cases {
		t.Run(c.property, func(t *testing.T) {
			t.Parallel()
			analysistest.Run(t, analysistest.TestData(), complexity.New(c.limits), c.pkg)
		})
	}
}

type limitLaw struct {
	key     string
	least   int
	limits  func(n int) config.Complexity
	subject func(v int) string
	file    func(v int, want string) string
}

func limitLaws() []limitLaw {
	function := func(label string) func(v int) string {
		return func(v int) string { return fmt.Sprintf("function F%d is too complex: %s", v, label) }
	}
	return []limitLaw{
		{
			key:     "max_cognitive",
			least:   0,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxCognitive: n} },
			subject: function("cognitive score"),
			file: func(v int, want string) string {
				return fmt.Sprintf("package p\n\nfunc F%d(x bool) {%s\n%s}\n", v, want, strings.Repeat("\tif x {\n\t}\n", v))
			},
		},
		{
			key:     "max_cyclomatic",
			least:   1,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxCyclomatic: n} },
			subject: function("cyclomatic complexity"),
			file: func(v int, want string) string {
				return fmt.Sprintf("package p\n\nfunc F%d(x bool) bool {%s\n\treturn x%s\n}\n", v, want, strings.Repeat(" || x", v-1))
			},
		},
		{
			key:     "max_nesting",
			least:   0,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxNesting: n} },
			subject: function("nesting depth"),
			file: func(v int, want string) string {
				return fmt.Sprintf("package p\n\nfunc F%d(x bool) {%s\n%s%s}\n", v, want, strings.Repeat("if x {\n", v), strings.Repeat("}\n", v))
			},
		},
		{
			key:     "max_lines",
			least:   1,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxLines: n} },
			subject: function("line count"),
			file: func(v int, want string) string {
				return fmt.Sprintf("package p\n\nfunc F%d() { /*%s */%s}\n", v, want, strings.Repeat("\n", v-1))
			},
		},
		{
			key:     "max_params",
			least:   0,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxParams: n} },
			subject: function("parameter count"),
			file: func(v int, want string) string {
				return fmt.Sprintf("package p\n\nfunc F%d(%s) {}%s\n", v, fields("p%d int", v, ", "), want)
			},
		},
		{
			key:     "max_bool_params",
			least:   0,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxBoolParams: n} },
			subject: function("bool parameter count"),
			file: func(v int, want string) string {
				return fmt.Sprintf("package p\n\nfunc F%d(%s) {}%s\n", v, fields("b%d bool", v, ", "), want)
			},
		},
		{
			key:     "max_interface_methods",
			least:   0,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxInterfaceMethods: n} },
			subject: func(v int) string { return fmt.Sprintf("interface F%d is too large: method count", v) },
			file: func(v int, want string) string {
				return fmt.Sprintf("package p\n\ntype F%d interface {%s\n%s}\n", v, want, fields("\tM%d()\n", v, ""))
			},
		},
		{
			key:     "max_file_lines",
			least:   1,
			limits:  func(n int) config.Complexity { return config.Complexity{MaxFileLines: n} },
			subject: func(int) string { return "file is too long: line count" },
			file: func(v int, want string) string {
				return "package p" + want + "\n" + strings.Repeat("//\n", v-1)
			},
		},
	}
}

func fields(format string, n int, sep string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(format, i)
	}
	return strings.Join(parts, sep)
}

func TestLimitReportsExactlyValuesAboveNonZeroMax(t *testing.T) {
	t.Parallel()
	for _, law := range limitLaws() {
		for limit := range lawTop {
			t.Run(fmt.Sprintf("%s=%d", law.key, limit), func(t *testing.T) {
				t.Parallel()
				dir := writeLawPackage(t, law, limit)
				analysistest.Run(t, dir, complexity.New(law.limits(limit)), "p")
			})
		}
	}
}

const lawTop = 5

func writeLawPackage(t *testing.T, law limitLaw, limit int) string {
	t.Helper()
	dir := t.TempDir()
	pkg := filepath.Join(dir, "src", "p")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	for v := law.least; v <= lawTop; v++ {
		want := ""
		if limit > 0 && v > limit {
			want = fmt.Sprintf(" // want `^%s %d exceeds %d$`", law.subject(v), v, limit)
		}
		name := filepath.Join(pkg, fmt.Sprintf("v%d.go", v))
		if err := os.WriteFile(name, []byte(law.file(v, want)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
