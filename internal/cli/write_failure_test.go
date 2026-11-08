package cli_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/gosuda/gglint/internal/cli"
)

const exitInternal = 3

var (
	errSinkClosed      = errors.New("sink closed")
	sinkClosedLine     = regexp.MustCompile(`^gglint: internal: .*sink closed\n$`)
	deviceFullLine     = regexp.MustCompile(`^gglint: internal: .*no space left on device$`)
	internalLinePrefix = "gglint: internal: "
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errSinkClosed }

func mainWithBrokenStdout(t *testing.T, args ...string) (code int, stderr string) {
	t.Helper()
	var errBuf bytes.Buffer
	code = cli.Main(t.Context(), args, brokenWriter{}, &errBuf)
	return code, errBuf.String()
}

func TestVersionStdoutWriteFailureExitsInternalWithOneLine(t *testing.T) {
	code, stderr := mainWithBrokenStdout(t, "version")
	if code != exitInternal {
		t.Errorf("exit code = %d, want %d", code, exitInternal)
	}
	if !sinkClosedLine.MatchString(stderr) {
		t.Errorf("stderr = %q, want one line matching %q", stderr, sinkClosedLine)
	}
}

func TestRulesStdoutWriteFailureExitsInternalWithOneLine(t *testing.T) {
	code, stderr := mainWithBrokenStdout(t, "rules")
	if code != exitInternal {
		t.Errorf("exit code = %d, want %d", code, exitInternal)
	}
	if !sinkClosedLine.MatchString(stderr) {
		t.Errorf("stderr = %q, want one line matching %q", stderr, sinkClosedLine)
	}
}

func TestVersionWorkingStdoutExitsCleanWithSilentStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Main(t.Context(), []string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "gglint ") {
		t.Errorf("stdout = %q, want a gglint version line", stdout.String())
	}
}

func TestRunFindingsOnFullStdoutExitsInternalWithOneLine(t *testing.T) {
	devFull, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("open /dev/full: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := devFull.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	dir := workspace(t, "")
	cmd := exec.CommandContext(t.Context(), binary, "run", "./a")
	cmd.Dir = dir
	cmd.Env = childEnv(t, dir)
	var stderr strings.Builder
	cmd.Stdout, cmd.Stderr = devFull, &stderr
	err = cmd.Run()
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		t.Fatalf("gglint run ./a: err = %v, want exit status %d", err, exitInternal)
	}
	if code := exitErr.ExitCode(); code != exitInternal {
		t.Errorf("exit code = %d, want %d", code, exitInternal)
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	var internal []string
	for _, line := range lines {
		if strings.HasPrefix(line, internalLinePrefix) {
			internal = append(internal, line)
		}
	}
	if len(internal) != 1 || !deviceFullLine.MatchString(internal[0]) {
		t.Errorf("stderr = %q, want exactly one line matching %q", stderr.String(), deviceFullLine)
	}
}
