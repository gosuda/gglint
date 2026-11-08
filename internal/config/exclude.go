package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Exclude struct {
	Paths []string `json:"paths"`
	Rules []string `json:"rules"`
	Tests *bool    `json:"tests,omitzero"`
	Text  string   `json:"text"`
}

type CompiledExclude struct {
	paths []*regexp.Regexp
	rules []string
	tests *bool
	text  *regexp.Regexp
}

func CompileExcludes(excludes []Exclude) ([]CompiledExclude, error) {
	compiled := make([]CompiledExclude, 0, len(excludes))
	for i, e := range excludes {
		c, err := compileExclude(i, e)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, c)
	}
	return compiled, nil
}

func compileExclude(index int, e Exclude) (CompiledExclude, error) {
	if len(e.Paths) == 0 && len(e.Rules) == 0 && e.Tests == nil && e.Text == "" {
		return CompiledExclude{}, fmt.Errorf("exclude[%d]: set at least one of paths, rules, tests, text", index)
	}
	c := CompiledExclude{rules: slices.Clone(e.Rules), tests: e.Tests}
	for j, source := range e.Paths {
		re, err := regexp.Compile(source)
		if err != nil {
			return CompiledExclude{}, fmt.Errorf("exclude[%d].paths[%d]: %w", index, j, err)
		}
		c.paths = append(c.paths, re)
	}
	if e.Text != "" {
		re, err := regexp.Compile(e.Text)
		if err != nil {
			return CompiledExclude{}, fmt.Errorf("exclude[%d].text: %w", index, err)
		}
		c.text = re
	}
	return c, nil
}

func (c CompiledExclude) Matches(path, rule, check, message string) bool {
	return c.matchesPath(path) && c.matchesTests(path) && c.matchesRule(rule, check) && c.matchesText(message)
}

func (c CompiledExclude) matchesPath(path string) bool {
	if len(c.paths) == 0 {
		return true
	}
	return slices.ContainsFunc(c.paths, func(re *regexp.Regexp) bool { return re.MatchString(path) })
}

func (c CompiledExclude) matchesTests(path string) bool {
	return c.tests == nil || *c.tests == strings.HasSuffix(path, "_test.go")
}

func (c CompiledExclude) matchesRule(rule, check string) bool {
	if len(c.rules) == 0 {
		return true
	}
	return slices.ContainsFunc(c.rules, func(token string) bool { return MatchToken(token, rule, check) })
}

func (c CompiledExclude) matchesText(message string) bool {
	return c.text == nil || c.text.MatchString(message)
}

func MatchToken(token, rule, check string) bool {
	if token == rule {
		return true
	}
	return check != "" && token == rule+":"+check
}
