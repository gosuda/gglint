package driver

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

type FixResult struct {
	Refusals []string
	Files    map[string][]byte
}

func Fix(ctx context.Context, inv Invocation) (FixResult, error) {
	directory, err := os.MkdirTemp("", "gglint-fixes-")
	if err != nil {
		return FixResult{}, loadError(err.Error())
	}
	// The vettool starts in each package directory, so a relative TMPDIR
	// would resolve the staging directory against the wrong directory for
	// every subpackage. Resolve it once against the invoking directory.
	directory, err = filepath.Abs(directory)
	if err != nil {
		return FixResult{}, loadError(err.Error())
	}
	defer func() { _ = os.RemoveAll(directory) }()
	out, err := execute(ctx, inv, "-json", "-fix-output="+directory)
	result := FixResult{Refusals: Refusals(out.stderr), Files: map[string][]byte{}}
	if err != nil {
		return result, err
	}
	_, sourceOnly := parseStderr(out.stderr, inv.WorkDir)
	if out.failed && !sourceOnly {
		return result, loadError(strings.TrimSpace(string(out.stderr)))
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return result, fmt.Errorf("read staged fixes: %w", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".fix.zip") {
			if err := readFixArchive(filepath.Join(directory, entry.Name()), result.Files); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func readFixArchive(path string, files map[string][]byte) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open staged fixes: %w", err)
	}
	defer func() { _ = archive.Close() }()
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			return fmt.Errorf("open staged file %s: %w", file.Name, err)
		}
		data, err := io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return fmt.Errorf("read staged file %s: %w", file.Name, err)
		}
		if previous, ok := files[file.Name]; ok && !bytes.Equal(previous, data) {
			return fmt.Errorf("conflicting staged fixes for %s", file.Name)
		}
		files[file.Name] = data
	}
	return nil
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

func execute(ctx context.Context, inv Invocation, modeFlags ...string) (output, error) {
	args := []string{"vet", "-vettool=" + inv.Exe, "-config=" + inv.ConfigFile}
	args = append(args, modeFlags...)
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
