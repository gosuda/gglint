package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

func rulesCommand(args []string, stdout, stderr io.Writer) int {
	var mode, configPath string
	var selected bool
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&mode, "mode", "", "list rules at or below this mode")
	fs.StringVar(&configPath, "config", "", "path of gglint.toml (with -selected)")
	fs.BoolVar(&selected, "selected", false, "list the rules a run would select")
	if err := fs.Parse(args); err != nil {
		return fail(stderr, exitUsage, err)
	}
	if configPath != "" && !selected {
		return fail(stderr, exitUsage, errors.New("-config requires -selected"))
	}
	list := catalogRules
	if selected {
		list = selectedRules
	}
	listed, err := list(mode, configPath)
	if err != nil {
		return fail(stderr, exitUsage, err)
	}
	var out []byte
	for _, r := range listed {
		out = fmt.Appendf(out, "%s\t%s\t%s\t%s\n", r.Name, r.Mode, r.Origin, strings.Join(strings.Fields(r.Doc), " "))
	}
	if _, err := stdout.Write(out); err != nil {
		return fail(stderr, exitFailure, fmt.Errorf("internal: %w", err))
	}
	return exitClean
}

func catalogRules(mode, _ string) ([]rules.Rule, error) {
	all := rules.All()
	if mode == "" {
		return all, nil
	}
	limit, err := config.ParseMode(mode)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(r rules.Rule) bool { return r.Mode > limit }), nil
}

func selectedRules(mode, configPath string) ([]rules.Rule, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(configPath, mode, wd)
	if err != nil {
		return nil, err
	}
	return rules.Select(cfg)
}
