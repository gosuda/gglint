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
	Mode    *string  `toml:"mode"`
	Enable  []string `toml:"enable"`
	Disable []string `toml:"disable"`
	Run     struct {
		Tests     *bool    `toml:"tests"`
		BuildTags []string `toml:"build_tags"`
	} `toml:"run"`
	Rules   rulesSchema `toml:"rules"`
	Exclude []Exclude   `toml:"exclude"`
}

type rulesSchema struct {
	Complexity   complexitySchema `toml:"complexity"`
	Checkimports struct {
		Deny []Deny `toml:"deny"`
	} `toml:"checkimports"`
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

func Discover(wd string) (string, error) {
	dir, err := filepath.Abs(wd)
	if err != nil {
		return "", err
	}
	for {
		candidate, last, err := discoverIn(dir)
		if err != nil || candidate != "" {
			return candidate, err
		}
		parent := filepath.Dir(dir)
		if last || parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func discoverIn(dir string) (candidate string, last bool, err error) {
	untrusted, err := worldWritable(dir)
	if err != nil || untrusted {
		return "", true, err
	}
	path := filepath.Join(dir, FileName)
	found, err := exists(path)
	if err != nil {
		return "", true, err
	}
	if found {
		return path, true, nil
	}
	last, err = exists(filepath.Join(dir, ".git"))
	return "", last, err
}

func worldWritable(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().Perm()&0o002 != 0, nil
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
		cfg.Path = filepath.Clean(path)
		if !filepath.IsAbs(path) {
			cfg.Path = filepath.Join(wd, path)
		}
		cfg.ExcludeBase = filepath.Dir(cfg.Path)
		if err := readFile(cfg.Path, &file); err != nil {
			return Config{}, fmt.Errorf("%s: %w", cfg.Path, err)
		}
	}
	if err := applyFile(&cfg, file, modeOverride); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readFile(path string, file *fileSchema) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	err = toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(file)
	if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
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

func applyFile(c *Config, file fileSchema, modeOverride string) error {
	if file.Mode != nil {
		mode, err := ParseMode(*file.Mode)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Path, err)
		}
		c.Mode = mode
	}
	if modeOverride != "" {
		mode, err := ParseMode(modeOverride)
		if err != nil {
			return err
		}
		c.Mode = mode
	}
	if file.Run.Tests != nil {
		c.Tests = *file.Run.Tests
	}
	c.Enable, c.Disable, c.BuildTags = file.Enable, file.Disable, file.Run.BuildTags
	c.Excludes = file.Exclude
	if err := file.Rules.applyTo(c); err != nil {
		return fmt.Errorf("%s: %w", c.Path, err)
	}
	if _, err := CompileExcludes(c.Excludes); err != nil {
		return fmt.Errorf("%s: %w", c.Path, err)
	}
	return nil
}

func (s rulesSchema) applyTo(c *Config) error {
	c.Complexity = ComplexityFor(c.Mode)
	overrides := []struct {
		key      string
		src, dst *int
	}{
		{"max_cognitive", s.Complexity.MaxCognitive, &c.Complexity.MaxCognitive},
		{"max_cyclomatic", s.Complexity.MaxCyclomatic, &c.Complexity.MaxCyclomatic},
		{"max_nesting", s.Complexity.MaxNesting, &c.Complexity.MaxNesting},
		{"max_lines", s.Complexity.MaxLines, &c.Complexity.MaxLines},
		{"max_params", s.Complexity.MaxParams, &c.Complexity.MaxParams},
		{"max_bool_params", s.Complexity.MaxBoolParams, &c.Complexity.MaxBoolParams},
		{"max_interface_methods", s.Complexity.MaxInterfaceMethods, &c.Complexity.MaxInterfaceMethods},
		{"max_file_lines", s.Complexity.MaxFileLines, &c.Complexity.MaxFileLines},
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
	for i, d := range s.Checkimports.Deny {
		if d.Path == "" {
			return fmt.Errorf("rules.checkimports.deny[%d]: path is empty", i)
		}
	}
	c.Deny = s.Checkimports.Deny
	return nil
}
