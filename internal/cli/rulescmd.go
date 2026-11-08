package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
		return usage(stderr, err)
	}
	if configPath != "" && !selected {
		return usage(stderr, errors.New("-config requires -selected"))
	}
	listed, err := listRules(mode, configPath, selected)
	if err != nil {
		return usage(stderr, err)
	}
	for _, r := range listed {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", r.Name, r.Mode, r.Origin, oneLine(r.Doc))
	}
	return exitClean
}

func listRules(mode, configPath string, selected bool) ([]rules.Rule, error) {
	if selected {
		return selectedRules(mode, configPath)
	}
	if mode == "" {
		return rules.All(), nil
	}
	limit, err := config.ParseMode(mode)
	if err != nil {
		return nil, err
	}
	var out []rules.Rule
	for _, r := range rules.All() {
		if r.Mode <= limit {
			out = append(out, r)
		}
	}
	return out, nil
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

func oneLine(doc string) string {
	return strings.Join(strings.Fields(doc), " ")
}
