package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const FileName = "gglint.toml"

type fileSchema struct {
	Mode    *string         `toml:"mode"`
	Enable  []string        `toml:"enable"`
	Disable []string        `toml:"disable"`
	Run     runSchema       `toml:"run"`
	Rules   rulesSchema     `toml:"rules"`
	Exclude []excludeSchema `toml:"exclude"`
}

type runSchema struct {
	Tests     *bool    `toml:"tests"`
	BuildTags []string `toml:"build_tags"`
}

type rulesSchema struct {
	Complexity   complexitySchema   `toml:"complexity"`
	Checkimports checkimportsSchema `toml:"checkimports"`
}

type complexitySchema struct {
	MaxCognitive        *int `toml:"max_cognitive"`
	MaxCyclomatic       *int `toml:"max_cyclomatic"`
	MaxNesting          *int `toml:"max_nesting"`
	MaxLines            *int `toml:"max_lines"`
	MaxParams           *int `toml:"max_params"`
	MaxBoolParams       *int `toml:"max_bool_params"`
	MaxInterfaceMethods *int `toml:"max_interface_methods"`
	MaxFileLines        *int `toml:"max_file_lines"`
}

type checkimportsSchema struct {
	Deny []denySchema `toml:"deny"`
}

type denySchema struct {
	Path      string   `toml:"path"`
	AllowFrom []string `toml:"allow_from"`
}

type excludeSchema struct {
	Paths []string `toml:"paths"`
	Rules []string `toml:"rules"`
	Text  string   `toml:"text"`
}

func Discover(wd string) (string, error) {
	dir, err := filepath.Abs(wd)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, FileName)
		found, err := exists(candidate)
		if err != nil {
			return "", err
		}
		if found {
			return candidate, nil
		}
		boundary, err := exists(filepath.Join(dir, ".git"))
		if err != nil {
			return "", err
		}
		parent := filepath.Dir(dir)
		if boundary || parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func Load(path, wd, modeOverride string) (Config, error) {
	wd, err := filepath.Abs(wd)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{ExcludeBase: wd, Mode: Standard, Tests: true}
	var file fileSchema
	if path != "" {
		cfg.Path = absolute(path, wd)
		cfg.ExcludeBase = filepath.Dir(cfg.Path)
		if err := readFile(cfg.Path, &file); err != nil {
			return Config{}, wrapPath(cfg.Path, err)
		}
	}
	if err := apply(&cfg, file, modeOverride); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func absolute(path, wd string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(wd, path)
}

func readFile(path string, file *fileSchema) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeTOML(data, file)
}

func decodeTOML(data []byte, file *fileSchema) error {
	err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(file)
	if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok && len(strict.Errors) > 0 {
		first := strict.Errors[0]
		row, column := first.Position()
		return fmt.Errorf("%d:%d: unknown key %s", row, column, strings.Join(first.Key(), "."))
	}
	if decode, ok := errors.AsType[*toml.DecodeError](err); ok {
		row, column := decode.Position()
		return fmt.Errorf("%d:%d: %s", row, column, strings.TrimPrefix(decode.Error(), "toml: "))
	}
	return err
}

func apply(c *Config, file fileSchema, modeOverride string) error {
	if err := applyMode(c, file, modeOverride); err != nil {
		return err
	}
	c.Enable = file.Enable
	c.Disable = file.Disable
	c.BuildTags = file.Run.BuildTags
	if file.Run.Tests != nil {
		c.Tests = *file.Run.Tests
	}
	c.Complexity = ComplexityFor(c.Mode)
	if err := applyDetails(c, file); err != nil {
		return wrapPath(c.Path, err)
	}
	return nil
}

func applyMode(c *Config, file fileSchema, modeOverride string) error {
	if file.Mode != nil {
		mode, err := ParseMode(*file.Mode)
		if err != nil {
			return wrapPath(c.Path, err)
		}
		c.Mode = mode
	}
	if modeOverride == "" {
		return nil
	}
	mode, err := ParseMode(modeOverride)
	if err != nil {
		return err
	}
	c.Mode = mode
	return nil
}

func applyDetails(c *Config, file fileSchema) error {
	if err := file.Rules.Complexity.applyTo(&c.Complexity); err != nil {
		return err
	}
	if err := applyDeny(c, file.Rules.Checkimports.Deny); err != nil {
		return err
	}
	return applyExcludes(c, file.Exclude)
}

func applyDeny(c *Config, deny []denySchema) error {
	for i, d := range deny {
		if d.Path == "" {
			return fmt.Errorf("rules.checkimports.deny[%d]: path is empty", i)
		}
		c.Deny = append(c.Deny, Deny(d))
	}
	return nil
}

func applyExcludes(c *Config, excludes []excludeSchema) error {
	for _, e := range excludes {
		c.Excludes = append(c.Excludes, Exclude(e))
	}
	_, err := CompileExcludes(c.Excludes)
	return err
}

func wrapPath(path string, err error) error {
	if path == "" {
		return err
	}
	return fmt.Errorf("%s: %w", path, err)
}

func (s complexitySchema) applyTo(c *Complexity) error {
	overrides := []struct {
		key string
		src *int
		dst *int
	}{
		{"max_cognitive", s.MaxCognitive, &c.MaxCognitive},
		{"max_cyclomatic", s.MaxCyclomatic, &c.MaxCyclomatic},
		{"max_nesting", s.MaxNesting, &c.MaxNesting},
		{"max_lines", s.MaxLines, &c.MaxLines},
		{"max_params", s.MaxParams, &c.MaxParams},
		{"max_bool_params", s.MaxBoolParams, &c.MaxBoolParams},
		{"max_interface_methods", s.MaxInterfaceMethods, &c.MaxInterfaceMethods},
		{"max_file_lines", s.MaxFileLines, &c.MaxFileLines},
	}
	for _, o := range overrides {
		if o.src == nil {
			continue
		}
		if *o.src < 0 {
			return fmt.Errorf("rules.complexity.%s: must be >= 0", o.key)
		}
		*o.dst = *o.src
	}
	return nil
}
