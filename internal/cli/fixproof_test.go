package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/config"
)

// gglintCache runs the test binary with a caller-chosen XDG cache home so
// two runs share one vet cache.
func gglintCache(t *testing.T, dir, cache string, args ...string) result {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binary, args...)
	cmd.Dir = dir
	env := append(os.Environ(), hermeticGo...)
	env = append(env,
		"PWD="+dir,
		"XDG_CACHE_HOME="+cache,
		"GOCACHE="+goCache,
		"GOFLAGS=-mod=mod",
		"GOWORK=off",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("gglint %v: %v", args, err)
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestFixSiblingPatternIsVerifiedInPlace(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	// A sibling package outside the working directory: the gate resolves
	// the pattern through go list and still verifies the fix.
	const src = "package sib\n\nfunc Sib() int {\n\n\treturn 1\n}\n"
	sib := filepath.Join(dir, "sib")
	if err := os.Mkdir(sib, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sib, "sib.go"), src)
	got := gglint(t, sub, "run", "-fix", "../sib")
	assertResult(t, got, result{stdout: agentSummary(0, 0, "")})
	if fixed := readFile(t, filepath.Join(sib, "sib.go")); fixed == src {
		t.Fatal("sibling package was not fixed")
	}
}

func TestFixFailureStillReportsLoadError(t *testing.T) {
	t.Parallel()
	dir := workspace(t, "")
	got := gglint(t, dir, "run", "-fix", "./nosuchpkg")
	if got.code != 3 || got.stdout != "" || !strings.HasPrefix(got.stderr, "gglint: load: ") {
		t.Fatalf("exit %d, stdout %q, stderr %q; want exit 3, empty stdout, gglint: load: error", got.code, got.stdout, got.stderr)
	}
}

func TestFixRepeatsTestsOffRefusalFromCache(t *testing.T) {
	t.Parallel()
	// Second input of the same fault class: the refusal replay must repeat
	// every refusal kind, not just the generated-file one.
	const testsOff = "[run]\ntests = false\n"
	dir := workspace(t, "")
	writeFile(t, filepath.Join(dir, config.FileName), testsOff)
	before := readFile(t, filepath.Join(dir, "fixtests", "f_test.go"))
	cache := t.TempDir()
	first := gglintCache(t, dir, cache, "run", "-format", "text", "-fix", "./fixtests")
	second := gglintCache(t, dir, cache, "run", "-format", "text", "-fix", "./fixtests")
	want := result{stderr: configLine(testsOff) + "gglint: refused fixtests/f_test.go: test file excluded when tests are off\n"}
	assertResult(t, first, want)
	assertResult(t, second, want)
	if after := readFile(t, filepath.Join(dir, "fixtests", "f_test.go")); after != before {
		t.Fatalf("fixtests/f_test.go changed with tests = false:\n got: %q\nwant: %q", after, before)
	}
}
