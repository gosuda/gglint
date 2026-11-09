package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/gosuda/gglint/internal/vettool"
)

const (
	exitClean    = 0
	exitFindings = 1
	exitUsage    = 2
	exitFailure  = 3
)

func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if isVettoolInvocation(args) {
		return vettool.Main(args)
	}
	if len(args) == 0 {
		return fail(stderr, exitUsage, errors.New("usage: gglint run|rules|version [flags]"))
	}
	switch args[0] {
	case "run":
		return runCommand(ctx, args[1:], stdout, stderr)
	case "rules":
		return rulesCommand(args[1:], stdout, stderr)
	case "version":
		return versionCommand(stdout, stderr)
	default:
		return fail(stderr, exitUsage, fmt.Errorf("unknown command %q (want run, rules, or version)", args[0]))
	}
}

func isVettoolInvocation(args []string) bool {
	if slices.Contains(args, "-V=full") || slices.Contains(args, "-flags") {
		return true
	}
	return len(args) > 0 && strings.HasSuffix(args[len(args)-1], ".cfg")
}

func say(stderr io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(stderr, "gglint: "+format+"\n", args...)
	return err
}

func fail(stderr io.Writer, code int, err error) int {
	if say(stderr, "%v", err) != nil {
		return exitFailure
	}
	return code
}

func versionCommand(stdout, stderr io.Writer) int {
	version := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		version = info.Main.Version
	}
	if _, err := fmt.Fprintf(stdout, "gglint %s %s\n", version, runtime.Version()); err != nil {
		return fail(stderr, exitFailure, fmt.Errorf("internal: %w", err))
	}
	return exitClean
}
