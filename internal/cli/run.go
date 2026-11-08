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

func parseRunFlags(args []string, stderr io.Writer) (runOptions, error) {
	var opts runOptions
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
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

func newSession(args []string, wd string, stderr io.Writer) (session, error) {
	opts, err := parseRunFlags(args, stderr)
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
		return fail(stderr, exitFailure, fmt.Errorf("internal: %w", err))
	}
	s, err := newSession(args, wd, stderr)
	if err != nil {
		return fail(stderr, exitUsage, err)
	}
	if s.printed != "" && say(stderr, "config %s", s.printed) != nil {
		return exitFailure
	}
	findings, err := s.lint(ctx)
	switch {
	case err == nil:
		return s.finish(stdout, stderr, findings)
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		return fail(stderr, exitFailure, errors.New("interrupted"))
	default:
		return fail(stderr, exitFailure, err)
	}
}

func (s *session) finish(stdout, stderr io.Writer, findings []finding.Finding) int {
	if report.Write(stdout, s.opts.format, s.cfg.Mode, s.printed, findings) != nil {
		return exitFailure
	}
	if len(findings) == 0 {
		return exitClean
	}
	if s.opts.format == report.FormatText && say(stderr, "%d findings (mode %s)", len(findings), s.cfg.Mode) != nil {
		return exitFailure
	}
	return exitFindings
}

func (s *session) lint(ctx context.Context) ([]finding.Finding, error) {
	inv, err := s.invocation()
	if err != nil {
		return nil, fmt.Errorf("internal: %w", err)
	}
	if s.opts.fix {
		if fixErr := driver.Fix(ctx, inv); fixErr != nil {
			return nil, fixErr
		}
	}
	raw, err := driver.Vet(ctx, inv)
	if err != nil {
		return nil, err
	}
	for i := range raw {
		raw[i].Rule, raw[i].Check = rules.Token(raw[i].Rule, s.selected)
	}
	return finding.Process(ctx, raw, finding.Options{WorkDir: s.wd, Tests: s.cfg.Tests, NewFromRev: s.opts.newFromRev})
}

func (s *session) invocation() (driver.Invocation, error) {
	exe, err := os.Executable()
	if err != nil {
		return driver.Invocation{}, err
	}
	configFile, err := writeResolvedConfig(s.cfg)
	if err != nil {
		return driver.Invocation{}, err
	}
	return driver.Invocation{
		Exe:        exe,
		ConfigFile: configFile,
		WorkDir:    s.wd,
		BuildTags:  s.cfg.BuildTags,
		Patterns:   s.opts.patterns,
	}, nil
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
	_, err = tmp.Write(data)
	err = errors.Join(err, tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), os.Remove(tmp.Name()))
	}
	return nil
}
