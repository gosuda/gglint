package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
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
		return usage(stderr, errors.New("usage: gglint run|rules|version [flags]"))
	}
	switch args[0] {
	case "run":
		return runCommand(ctx, args[1:], stdout, stderr)
	case "rules":
		return rulesCommand(args[1:], stdout, stderr)
	case "version":
		return versionCommand(stdout)
	default:
		return usage(stderr, fmt.Errorf("unknown command %q (want run, rules, or version)", args[0]))
	}
}

func isVettoolInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		if arg == "-V=full" || arg == "-flags" {
			return true
		}
	}
	return strings.HasSuffix(args[len(args)-1], ".cfg")
}

func usage(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "gglint: %v\n", err)
	return exitUsage
}

func failure(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "gglint: %v\n", err)
	return exitFailure
}

func versionCommand(stdout io.Writer) int {
	version := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		version = info.Main.Version
	}
	fmt.Fprintf(stdout, "gglint %s %s\n", version, runtime.Version())
	return exitClean
}
