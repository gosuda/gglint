package driver_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/driver"
)

const unrelatedFinding = `{"m/ok":{"printf":[{"posn":"/w/ok.go:1:1","message":"bad verb"}]}}`

func TestFailedCommandsPreserveMixedToolErrors(t *testing.T) {
	const source = "# m/source [m/source.test]\r\n/w/source.go:4:2: undefined: x\r\n"
	cases := []struct {
		name, fatal string
	}{
		{"killed child", "# m/dependency\n/opt/gglint: signal: killed\n"},
		{"panic and stack", "# m/dependency\npanic: missing facts\n\ngoroutine 18 [running]:\nchecker.run()\n\t/w/analyzer.go:42 +0x81\n"},
		{"indented panic", "\tpanic: missing facts\n"},
		{"runtime failure", "fatal error: runtime: out of memory\n\nruntime stack:\n"},
		{"compiler ICE", "# m/dependency\n<unknown line number>: internal compiler error: unexpected object\n\nPlease file a bug report.\n"},
		{"positioned compiler ICE", "# m/dependency\n/w/dep.go:12:9: internal compiler error: unexpected object\n"},
		{"positioned vet ICE", "vet: /w/dep.go:12:9: internal compiler error: unexpected object\n"},
		{"positioned panic", "/w/dep.go:12:9: panic: missing facts\n"},
		{"positioned runtime failure", "/w/dep.go:12:9: fatal error: runtime: out of memory\n"},
		{"positioned killed child", "/w/dep.go:12:9: /opt/gglint: signal: killed\n"},
		{"unknown failure", "# m/dependency\nunrecognized tool failure\n  additional context\n"},
		{"indented unknown failure", "\tunrecognized tool failure\n"},
		{"stack frame without heading", "\t/w/analyzer.go:42 +0x81\n"},
		{"header with error text", "# m/dependency: signal: killed\n"},
		{"download with error text", "go: downloading example.com/x v1.0.0: connection reset\n"},
		{"unattached have want", "# m/dependency\n\thave (int)\n\twant (string)\n"},
		{"have prefix on a failure", "\thave panic: missing facts\n"},
		{"want prefix on a failure", "\twant tool failed\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, first := range []bool{false, true} {
				name, stderr := "diagnostic first", source+tc.fatal
				if first {
					name, stderr = "fatal first", tc.fatal+source
				}
				t.Run(name, func(t *testing.T) {
					stderr = "\n" + stderr + "\n"
					t.Setenv("PATH", installGo(t, goRun{stdout: unrelatedFinding, stderr: stderr, code: 1})+string(os.PathListSeparator)+os.Getenv("PATH"))
					inv := invocation(t.TempDir())
					want := "load: " + strings.TrimSpace(stderr)
					t.Run("Vet", func(t *testing.T) {
						got, err := driver.Vet(t.Context(), inv)
						if err == nil || err.Error() != want || got != nil {
							t.Fatalf("Vet = %+v, %v; want no partial findings and %q", got, err, want)
						}
					})
					t.Run("Fix", func(t *testing.T) {
						if err := driver.Fix(t.Context(), inv); err == nil || err.Error() != want {
							t.Fatalf("Fix error = %v; want %q", err, want)
						}
					})
				})
			}
		})
	}
}

func TestFailedCommandsKeepOrdinarySourceReports(t *testing.T) {
	cases := []struct {
		name, stderr string
		messages     []string
	}{
		{
			"multiple packages and progress",
			"go: downloading example.com/x v1.0.0\n\n# m/a\n/w/a.go:4:2: undefined: x\n# m/b [m/b.test]\nvet: /w/b.go:5:3: undefined: y\n",
			[]string{"undefined: x", "undefined: y"},
		},
		{
			"toolchain download",
			"go: downloading go1.27.4 (linux/amd64)\n# command-line-arguments\n/w/a.go:4:2: undefined: x\n",
			[]string{"undefined: x"},
		},
		{
			"have want",
			"# m/a\n/w/a.go:4:2: not enough arguments in call to f\n\thave (int)\n\twant (int, string)\n",
			[]string{"not enough arguments in call to f"},
		},
		{
			"method have want with CRLF",
			"# m/a\r\n/w/a.go:4:2: T does not implement I (wrong type for method M)\r\n\t\thave M(int)\r\n\t\twant M(string)\r\n",
			[]string{"T does not implement I (wrong type for method M)"},
		},
		{
			"method named panic",
			"# m/a\n/w/a.go:4:2: T does not implement I (wrong type for method panic)\n\t\thave panic(int)\n\t\twant panic(string)\n",
			[]string{"T does not implement I (wrong type for method panic)"},
		},
		{
			"unicode method name",
			"# m/a\n/w/a.go:4:2: T does not implement I (wrong type for method Δ)\n\t\thave Δ(int)\n\t\twant Δ(string)\n",
			[]string{"T does not implement I (wrong type for method Δ)"},
		},
		{
			"secondary declaration position",
			"# m/a\n/w/a.go:4:2: x redeclared in this block\n\t/w/b.go:2:3: other declaration of x\n",
			[]string{"x redeclared in this block", "other declaration of x"},
		},
		{
			"compiler error phrase in source constant",
			"# m/a\n/w/a.go:4:2: cannot use \"internal compiler error: x\" (untyped string constant) as int value\n",
			[]string{`cannot use "internal compiler error: x" (untyped string constant) as int value`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", installGo(t, goRun{stdout: unrelatedFinding, stderr: tc.stderr, code: 1})+string(os.PathListSeparator)+os.Getenv("PATH"))
			inv := invocation(t.TempDir())
			got, err := driver.Vet(t.Context(), inv)
			if err != nil {
				t.Fatal(err)
			}
			var messages []string
			for _, f := range got {
				messages = append(messages, f.Rule+"|"+f.Message)
			}
			want := make([]string, 0, len(tc.messages)+1)
			for _, message := range tc.messages {
				want = append(want, "typecheck|"+message)
			}
			want = append(want, "printf|bad verb")
			if !slices.Equal(messages, want) {
				t.Fatalf("findings = %q; want %q", messages, want)
			}
			if err := driver.Fix(t.Context(), inv); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSuccessfulCommandsKeepStderrHandling(t *testing.T) {
	t.Setenv("PATH", installGo(t, goRun{
		stdout: unrelatedFinding,
		stderr: "go: an unfamiliar progress notice\n",
	})+string(os.PathListSeparator)+os.Getenv("PATH"))
	inv := invocation(t.TempDir())
	got, err := driver.Vet(t.Context(), inv)
	if err != nil || len(got) != 1 || got[0].Rule != "printf" {
		t.Fatalf("Vet = %+v, %v; want the analyzer finding without an error", got, err)
	}
	if err := driver.Fix(t.Context(), inv); err != nil {
		t.Fatal(err)
	}
}
