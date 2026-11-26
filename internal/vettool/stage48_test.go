package vettool

import (
	"encoding/json"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"
)

func TestStageUnitSkipsUnitsOutsideMainModule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		module     *analysis.Module
		wantStaged bool
	}{
		{"standard package", nil, false},
		{"dependency", &analysis.Module{Path: "example.com/dep"}, false},
		{"main module", &analysis.Module{Path: "example.com/m", Main: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := json.Marshal(&unitchecker.Config{
				ID:         "example.com/m/p",
				ImportPath: "example.com/m/p",
				GoFiles:    []string{"p.go"},
				Module:     tc.module,
			})
			if err != nil {
				t.Fatal(err)
			}
			unit := writeFile(t, "unit.cfg", cfg)
			got, err := stageUnit(unit, t.TempDir())
			if err != nil {
				t.Fatalf("stageUnit: %v", err)
			}
			if tc.wantStaged != (got != "") {
				t.Fatalf("stageUnit = %q, want staged = %v", got, tc.wantStaged)
			}
		})
	}
}
