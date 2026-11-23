package gosec_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/gosuda/gglint/internal/analyzers/gosec"
)

func TestReportsIssues(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), gosec.New(nil), "sec")
}

func TestExcludedRulesStaySilent(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), gosec.New([]string{"G401", "G501"}), "secexcluded")
}

func TestGuardedEnumIndices(t *testing.T) {
	t.Parallel()
	results := analysistest.Run(t, analysistest.TestData(), gosec.New(nil), "enumindex")
	var collisionLines []int
	for _, result := range results {
		for _, diagnostic := range result.Action.Diagnostics {
			pos := result.Action.Package.Fset.PositionFor(diagnostic.Pos, false)
			if filepath.Base(pos.Filename) == "position.go" {
				collisionLines = append(collisionLines, pos.Line)
			}
		}
	}
	if want := []int{8}; !slices.Equal(collisionLines, want) {
		t.Errorf("physical collision lines = %v, want unsafe access at %v", collisionLines, want)
	}
}

func TestLineDirectivePositions(t *testing.T) {
	t.Parallel()
	results := analysistest.Run(t, analysistest.TestData(), gosec.New(nil), "secposition")
	var got []string
	for _, result := range results {
		for _, diagnostic := range result.Action.Diagnostics {
			pos := result.Action.Package.Fset.Position(diagnostic.Pos)
			got = append(got, fmt.Sprintf("%s:%d:%d", filepath.Base(pos.Filename), pos.Line, pos.Column))
		}
	}
	slices.Sort(got)
	want := []string{"ast.go:400:28", "bounds.go:1000:53", "inline.go:1700:12", "shown.go:700:53", "unknown.go:2000:0"}
	if !slices.Equal(got, want) {
		t.Errorf("diagnostic positions = %v, want %v", got, want)
	}
}
