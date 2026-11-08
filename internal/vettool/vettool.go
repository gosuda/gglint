package vettool

import (
	json "encoding/json/v2"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/rules"
)

const configFlag = "config"

func Main(args []string) int {
	flag.String(configFlag, "", "path of the resolved gglint configuration")
	path, found := configPath(args)
	if !found {
		unitchecker.Main()
		return 0
	}
	analyzers, err := build(path, args[len(args)-1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gglint: internal: %v\n", err)
		return 3
	}
	unitchecker.Main(analyzers...)
	return 0
}

func configPath(args []string) (string, bool) {
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if value, ok := strings.CutPrefix(name, configFlag+"="); ok {
			return value, true
		}
		if name == configFlag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func vetxOnly(unitConfig string) (bool, error) {
	data, err := os.ReadFile(unitConfig)
	if err != nil {
		return false, err
	}
	var unit struct{ VetxOnly bool }
	if err := json.Unmarshal(data, &unit); err != nil {
		return false, fmt.Errorf("decode unit config: %w", err)
	}
	return unit.VetxOnly, nil
}

func build(path, unitConfig string) ([]*analysis.Analyzer, error) {
	factsOnly, err := vetxOnly(unitConfig)
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
	if factsOnly {
		return e.factsOnly(selected), nil
	}
	return e.reporting(selected), nil
}

func (e *engine) factsOnly(selected []rules.Rule) []*analysis.Analyzer {
	p := producers{seen: map[*analysis.Analyzer]bool{}}
	for _, a := range e.analyzers(selected) {
		p.visit(a)
	}
	return p.out
}

type producers struct {
	seen map[*analysis.Analyzer]bool
	out  []*analysis.Analyzer
}

func (p *producers) visit(a *analysis.Analyzer) {
	if p.seen[a] {
		return
	}
	p.seen[a] = true
	if len(a.FactTypes) > 0 {
		p.out = append(p.out, a)
	}
	for _, req := range a.Requires {
		p.visit(req)
	}
}

func (e *engine) reporting(selected []rules.Rule) []*analysis.Analyzer {
	built := reportPanics(e.analyzers(selected))
	if e.selected[rules.NolintName] {
		built = append(built, e.nolintAnalyzer(built))
	}
	return built
}
