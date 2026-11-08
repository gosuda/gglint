package vettool

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/gosuda/gglint/internal/config"
)

const configFlag = "config"

func Main(args []string) int {
	flag.String(configFlag, "", "path of the resolved gglint configuration")
	path, found := configPath(args)
	if !found {
		unitchecker.Main()
		return 0
	}
	analyzers, err := build(path)
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

func build(path string) ([]*analysis.Analyzer, error) {
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
	return e.analyzers(selected), nil
}
