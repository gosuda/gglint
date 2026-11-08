package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gosuda/gglint/internal/config"
	"github.com/gosuda/gglint/internal/driver"
	"github.com/gosuda/gglint/internal/finding"
	"github.com/gosuda/gglint/internal/report"
	"github.com/gosuda/gglint/internal/rules"
)

type runOptions struct {
	configPath string
	mode       string
	format     string
	fix        bool
	newFromRev string
	patterns   []string
}

func parseRunFlags(args []string) (runOptions, error) {
	var opts runOptions
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.configPath, "config", "", "path of gglint.toml")
	fs.StringVar(&opts.mode, "mode", "", "basic, standard, or strict")
	fs.StringVar(&opts.format, "format", report.FormatText, "text or json")
	fs.BoolVar(&opts.fix, "fix", false, "apply suggested fixes before reporting")
	fs.StringVar(&opts.newFromRev, "new-from-rev", "", "report only findings in lines changed since REV")
	if err := fs.Parse(args); err != nil {
		return runOptions{}, err
	}
	opts.patterns = fs.Args()
	if len(opts.patterns) == 0 {
		opts.patterns = []string{"./..."}
	}
	if opts.format != report.FormatText && opts.format != report.FormatJSON {
		return runOptions{}, fmt.Errorf("unknown format %q (want text or json)", opts.format)
	}
	return opts, nil
}

type session struct {
	opts     runOptions
	cfg      config.Config
	selected []rules.Rule
	wd       string
	printed  string
}

func newSession(args []string, wd string) (session, error) {
	opts, err := parseRunFlags(args)
	if err != nil {
		return session{}, err
	}
	cfg, err := loadConfig(opts.configPath, opts.mode, wd)
	if err != nil {
		return session{}, err
	}
	selected, err := rules.Select(cfg)
	if err != nil {
		return session{}, err
	}
	return session{opts: opts, cfg: cfg, selected: selected, wd: wd, printed: printedConfigPath(cfg, wd)}, nil
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	wd, err := os.Getwd()
	if err != nil {
		return failure(stderr, fmt.Errorf("internal: %w", err))
	}
	s, err := newSession(args, wd)
	if err != nil {
		return usage(stderr, err)
	}
	if s.printed != "" {
		fmt.Fprintf(stderr, "gglint: config %s\n", s.printed)
	}
	findings, err := s.lint(ctx)
	if err != nil {
		return reportFailure(ctx, stderr, err)
	}
	return s.finish(stdout, stderr, findings)
}

func (s *session) finish(stdout, stderr io.Writer, findings []finding.Finding) int {
	if err := report.Write(stdout, s.opts.format, s.cfg.Mode, s.printed, findings); err != nil {
		return failure(stderr, fmt.Errorf("internal: %w", err))
	}
	if len(findings) == 0 {
		return exitClean
	}
	if s.opts.format == report.FormatText {
		fmt.Fprintf(stderr, "gglint: %d findings (mode %s)\n", len(findings), s.cfg.Mode)
	}
	return exitFindings
}

func (s *session) lint(ctx context.Context) ([]finding.Finding, error) {
	inv, err := s.invocation()
	if err != nil {
		return nil, err
	}
	if s.opts.fix {
		return s.fixThenVet(ctx, inv)
	}
	return s.vet(ctx, inv)
}

func (s *session) fixThenVet(ctx context.Context, inv driver.Invocation) ([]finding.Finding, error) {
	if err := driver.Fix(ctx, inv); err != nil {
		return nil, err
	}
	return s.vet(ctx, inv)
}

func (s *session) vet(ctx context.Context, inv driver.Invocation) ([]finding.Finding, error) {
	raw, err := driver.Vet(ctx, inv)
	if err != nil {
		return nil, err
	}
	options := finding.Options{WorkDir: s.wd, Tests: s.cfg.Tests, NewFromRev: s.opts.newFromRev}
	return finding.Process(ctx, relabel(raw, s.selected), options)
}

func (s *session) invocation() (driver.Invocation, error) {
	exe, err := os.Executable()
	if err != nil {
		return driver.Invocation{}, fmt.Errorf("internal: %w", err)
	}
	configFile, err := writeResolvedConfig(s.cfg)
	if err != nil {
		return driver.Invocation{}, fmt.Errorf("internal: %w", err)
	}
	return driver.Invocation{
		Exe:        exe,
		ConfigFile: configFile,
		WorkDir:    s.wd,
		BuildTags:  s.cfg.BuildTags,
		Patterns:   s.opts.patterns,
	}, nil
}

func relabel(fs []finding.Finding, selected []rules.Rule) []finding.Finding {
	for i := range fs {
		if fs[i].Rule != finding.TypecheckRule {
			fs[i].Rule, fs[i].Check = rules.Token(fs[i].Rule, selected)
		}
	}
	return fs
}

func reportFailure(ctx context.Context, stderr io.Writer, err error) int {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "gglint: interrupted")
		return exitFailure
	}
	return failure(stderr, err)
}

func loadConfig(path, mode, wd string) (config.Config, error) {
	if path == "" {
		discovered, err := config.Discover(wd)
		if err != nil {
			return config.Config{}, err
		}
		path = discovered
	}
	return config.Load(path, wd, mode)
}

func printedConfigPath(cfg config.Config, wd string) string {
	if cfg.Path == "" {
		return ""
	}
	rel, err := filepath.Rel(wd, cfg.Path)
	if err != nil {
		return filepath.ToSlash(cfg.Path)
	}
	return filepath.ToSlash(rel)
}

func writeResolvedConfig(cfg config.Config) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "gglint")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	data := cfg.Encode()
	sum := sha256.Sum256(data)
	path := filepath.Join(dir, "config-"+hex.EncodeToString(sum[:])+".json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, replaceFile(dir, path, data)
}

func replaceFile(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
