package rules_test

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzertest"
	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

type logosPort struct {
	dir  string
	rule string
	cfg  config.Config
}

func logosPorts() []logosPort {
	basic := config.Config{Mode: config.Basic}
	standard := config.Config{Mode: config.Standard}
	strict := config.Config{Mode: config.Strict}
	logosLimits := config.Config{
		Mode:       config.Basic,
		Complexity: config.Complexity{MaxCognitive: 15, MaxNesting: 3, MaxLines: 60, MaxParams: 4},
	}
	return []logosPort{
		{dir: "correctness_infinite_recursion", rule: "infinite_recursion", cfg: basic},
		{dir: "correctness_arena_pointer_escape", rule: "stackaddr", cfg: basic},
		{dir: "correctness_typed_nil_return", rule: "typednil", cfg: basic},
		{dir: "correctness_unclosed_io_closer", rule: "closer", cfg: strict},
		{dir: "correctness_unbounded_channel_op", rule: "chanblock", cfg: strict},
		{dir: "correctness_printf_misuse", rule: "printf", cfg: basic},
		{dir: "correctness_httpresponse", rule: "httpresponse", cfg: basic},
		{dir: "correctness_structtag_malformed", rule: "structtag_malformed", cfg: basic},
		{dir: "concurrency_concurrent_multiword_race", rule: "capturerace", cfg: standard},
		{dir: "concurrency_unsupervised_goroutine", rule: "goroutine", cfg: standard},
		{dir: "concurrency_reentrant_withlock_deadlock", rule: "reentrant_withlock_deadlock", cfg: basic},
		{dir: "slop_discarded_error", rule: "discarded_error", cfg: standard},
		{dir: "slop_err_chain_reset", rule: "err_chain_reset", cfg: standard},
		{dir: "slop_todo_placeholder", rule: "placeholder", cfg: strict},
		{dir: "complexity_cognitive_overflow", rule: "complexity", cfg: logosLimits},
		{dir: "perf_large_backing_array_pin", rule: "backingpin", cfg: strict},
		{dir: "perf_time_after_in_loop", rule: "timeafter", cfg: standard},
		{dir: "perf_zeroalloc_violation", rule: "checkescape", cfg: basic},
		{dir: "suspicious_copy_lock", rule: "copylocks", cfg: basic},
		{dir: "suspicious_string_intconv", rule: "stringintconv", cfg: basic},
		{dir: "suspicious_self_assignment", rule: "assign", cfg: basic},
		{dir: "suspicious_defer_in_loop", rule: "gocritic", cfg: standard},
		{dir: "portability_serialized_unsized_int", rule: "unsizedint", cfg: strict},
	}
}

func TestLogosPorts(t *testing.T) {
	ports := logosPorts()
	for i := range ports {
		port := &ports[i]
		t.Run(port.dir, func(t *testing.T) {
			analyzertest.RunIgnoringFacts(t, analysistest.TestData(), portAnalyzer(t, port), port.dir)
		})
	}
}

func portAnalyzer(t *testing.T, port *logosPort) *analysis.Analyzer {
	t.Helper()
	selected, err := rules.Select(port.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range selected {
		if r.Name != port.rule {
			continue
		}
		built := r.Build(port.cfg)
		if len(built) != 1 {
			t.Fatalf("rule %q builds %d analyzers; want 1", port.rule, len(built))
		}
		return built[0]
	}
	t.Fatalf("rule %q is not selected in mode %s", port.rule, port.cfg.Mode)
	return nil
}
