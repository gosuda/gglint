package finding

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/golangci/revgrep"
)

const TypecheckRule = "typecheck"

type Position struct {
	Path         string
	Line, Column int
}

type Related struct {
	Position
	Message string
}

type Finding struct {
	Rule, Check, Message string
	Pos, End             Position
	Related              []Related
	Fixes                []string
	Fixable              bool
}

type Options struct {
	WorkDir    string
	Tests      bool
	NewFromRev string
}

func (f Finding) Token() string {
	if f.Check == "" {
		return f.Rule
	}
	return f.Rule + ":" + f.Check
}

func Process(ctx context.Context, fs []Finding, opts Options) ([]Finding, error) {
	kept := make([]Finding, 0, len(fs))
	for _, f := range fs {
		f = printed(f, opts.WorkDir)
		if opts.Tests || !strings.HasSuffix(f.Pos.Path, "_test.go") {
			kept = append(kept, f)
		}
	}
	if opts.NewFromRev != "" {
		var err error
		if kept, err = changedSince(ctx, kept, opts.NewFromRev); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(kept, compare)
	return slices.CompactFunc(kept, func(a, b Finding) bool { return compare(a, b) == 0 }), nil
}

func printed(f Finding, workDir string) Finding {
	f.Pos = printedPosition(f.Pos, workDir)
	f.Pos.Column = max(f.Pos.Column, 1)
	f.End = printedPosition(f.End, workDir)
	f.Related = slices.Clone(f.Related)
	for i := range f.Related {
		f.Related[i].Position = printedPosition(f.Related[i].Position, workDir)
		f.Related[i].Column = max(f.Related[i].Column, 1)
	}
	return f
}

func printedPosition(p Position, workDir string) Position {
	p.Path = printedPath(p.Path, workDir)
	return p
}

func printedPath(path, workDir string) string {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(workDir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func changedSince(ctx context.Context, fs []Finding, rev string) ([]Finding, error) {
	checker := &revgrep.Checker{RevisionFrom: rev}
	if err := checker.Prepare(ctx); err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	return slices.DeleteFunc(fs, func(f Finding) bool {
		if f.Rule == TypecheckRule {
			return false
		}
		_, isNew := checker.IsNew(f.Pos.Path, f.Pos.Line)
		return !isNew
	}), nil
}

func compare(a, b Finding) int {
	return cmp.Or(
		strings.Compare(a.Pos.Path, b.Pos.Path),
		cmp.Compare(a.Pos.Line, b.Pos.Line),
		cmp.Compare(a.Pos.Column, b.Pos.Column),
		strings.Compare(a.Token(), b.Token()),
		strings.Compare(a.Message, b.Message),
	)
}
