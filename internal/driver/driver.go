package driver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
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

type LoadError struct{ Message string }

func (e *LoadError) Error() string { return "load: " + e.Message }

type InternalError struct{ Package, Analyzer, Message string }

func (e *InternalError) Error() string {
	return fmt.Sprintf("internal: %s: %s: %s", e.Package, e.Analyzer, e.Message)
}

var typecheckLine = regexp.MustCompile(`^(?:vet: )?(.+\.go):(\d+)(?::(\d+))?: (.+)$`)

func Vet(ctx context.Context, inv Invocation) ([]finding.Finding, error) {
	out, err := execute(ctx, inv, "-json")
	if err != nil {
		return nil, err
	}
	findings, err := decodeJSON(out.stdout)
	if err != nil {
		return nil, err
	}
	typecheck := typecheckFindings(out.stderr, inv.WorkDir)
	if out.failed && len(typecheck) == 0 {
		return nil, &LoadError{Message: strings.TrimSpace(string(out.stderr))}
	}
	return append(typecheck, findings...), nil
}

func Fix(ctx context.Context, inv Invocation) error {
	out, err := execute(ctx, inv, "-fix")
	if err != nil {
		return err
	}
	if out.failed && len(typecheckFindings(out.stderr, inv.WorkDir)) == 0 {
		return &LoadError{Message: strings.TrimSpace(string(out.stderr))}
	}
	return nil
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
		return output{}, &LoadError{Message: err.Error()}
	}
	return output{stdout: stdout.Bytes(), stderr: stderr.Bytes(), failed: err != nil}, nil
}

func typecheckFindings(stderr []byte, workDir string) []finding.Finding {
	var out []finding.Finding
	for line := range strings.SplitSeq(string(stderr), "\n") {
		m := typecheckLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		out = append(out, finding.Finding{
			Rule:    finding.TypecheckRule,
			Message: m[4],
			Pos:     finding.Position{Path: absolute(m[1], workDir), Line: number(m[2]), Column: number(m[3])},
		})
	}
	return out
}

func absolute(path, workDir string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workDir, path)
}
