package driver

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gosuda/gglint/internal/finding"
)

type Invocation struct {
	Exe        string
	ConfigFile string
	WorkDir    string
	BuildTags  []string
	Patterns   []string
}

func loadError(message string) error {
	return errors.New("load: " + message)
}

func Vet(ctx context.Context, inv Invocation) ([]finding.Finding, error) {
	out, err := execute(ctx, inv, "-json")
	if err != nil {
		return nil, err
	}
	findings, err := decodeJSON(out.stdout)
	if err != nil {
		return nil, err
	}
	typecheck, sourceOnly := parseStderr(out.stderr, inv.WorkDir)
	if out.failed && !sourceOnly {
		return nil, loadError(strings.TrimSpace(string(out.stderr)))
	}
	return append(typecheck, findings...), nil
}

func Fix(ctx context.Context, inv Invocation) ([]string, error) {
	out, err := execute(ctx, inv, "-fix")
	if err != nil {
		return nil, err
	}
	_, sourceOnly := parseStderr(out.stderr, inv.WorkDir)
	if out.failed && !sourceOnly {
		return nil, loadError(strings.TrimSpace(string(out.stderr)))
	}
	return Refusals(out.stderr), nil
}

// RefusalPrefix marks a vettool refusal line on stderr. The vettool prints
// one per refused suggested fix during a `-fix` run; report runs stay
// silent. The text after the prefix is the publication contract: it names
// the refused path and the reason, and the same strings appear in the README
// section "Automatic fixes".
const RefusalPrefix = "gglint: refused "

// Refusals extracts the vettool refusal lines from vet stderr, verbatim.
func Refusals(stderr []byte) []string {
	var out []string
	for line := range strings.SplitSeq(string(stderr), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, RefusalPrefix) {
			out = append(out, line)
		}
	}
	return out
}

type output struct {
	stdout, stderr []byte
	failed         bool
}

func execute(ctx context.Context, inv Invocation, modeFlag string) (output, error) {
	args := []string{"vet", "-vettool=" + inv.Exe, modeFlag, "-config=" + inv.ConfigFile}
	if len(inv.BuildTags) > 0 {
		args = append(args, "-tags="+strings.Join(inv.BuildTags, ","))
	}
	args = append(args, inv.Patterns...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = inv.WorkDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return output{}, ctx.Err()
	}
	if _, isExit := errors.AsType[*exec.ExitError](err); err != nil && !isExit {
		return output{}, loadError(err.Error())
	}
	return output{stdout: stdout.Bytes(), stderr: stderr.Bytes(), failed: err != nil}, nil
}

func absolute(path, workDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workDir, path)
}
