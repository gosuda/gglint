package vettool

import (
	json "encoding/json/v2"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/gosuda/gglint/internal/config"
)

const configFlag = "config"

type unit struct {
	VetxOnly bool
}

func Main(args []string) int {
	flag.String(configFlag, "", "path of the resolved gglint configuration")
	path, found := configPath(args)
	if !found {
		unitchecker.Main()
		return 0
	}
	analyzers, err := build(path, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gglint: internal: %v\n", err)
		return 3
	}
	unitchecker.Main(analyzers...)
	return 0
}

func configPath(args []string) (string, bool) {
	for i, arg := range args {
		name := strings.TrimLeft(arg, "-")
		if name == configFlag && strings.HasPrefix(arg, "-") && i+1 < len(args) {
			return args[i+1], true
		}
		if value, ok := strings.CutPrefix(name, configFlag+"="); ok && strings.HasPrefix(arg, "-") {
			return value, true
		}
	}
	return "", false
}

func unitOf(args []string) (unit, error) {
	var u unit
	data, err := os.ReadFile(args[len(args)-1])
	if err != nil {
		return u, err
	}
	if err := json.Unmarshal(data, &u); err != nil {
		return u, fmt.Errorf("decode unit config: %w", err)
	}
	return u, nil
}

func build(path string, args []string) ([]*analysis.Analyzer, error) {
	u, err := unitOf(args)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Decode(data)
	if err != nil {
		return nil, err
	}
	e, selected, err := newEngine(cfg)
	if err != nil {
		return nil, err
	}
	built := e.analyzers(selected)
	if u.VetxOnly {
		return factAnalyzers(built), nil
	}
	return e.withNolint(reportPanics(built)), nil
}

func factAnalyzers(all []*analysis.Analyzer) []*analysis.Analyzer {
	var out []*analysis.Analyzer
	for _, a := range all {
		if usesFacts(a) {
			out = append(out, a)
		}
	}
	return out
}

func usesFacts(a *analysis.Analyzer) bool {
	return len(a.FactTypes) > 0 || slices.ContainsFunc(a.Requires, usesFacts)
}
