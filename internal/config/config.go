package config

import (
	json "encoding/json/v2"
	"fmt"
	"slices"
)

type Deny struct {
	Path      string   `json:"path" toml:"path"`
	AllowFrom []string `json:"allow_from" toml:"allow_from"`
}

type Config struct {
	Path        string     `json:"path"`
	ExcludeBase string     `json:"exclude_base"`
	Mode        Mode       `json:"mode"`
	Enable      []string   `json:"enable"`
	Disable     []string   `json:"disable"`
	Tests       bool       `json:"tests"`
	BuildTags   []string   `json:"build_tags"`
	Complexity  Complexity `json:"complexity"`
	Deny        []Deny     `json:"deny"`
	Excludes    []Exclude  `json:"excludes"`
}

func (c Config) Encode() []byte {
	c.Enable = slices.Sorted(slices.Values(c.Enable))
	c.Disable = slices.Sorted(slices.Values(c.Disable))
	c.BuildTags = slices.Sorted(slices.Values(c.BuildTags))
	data, err := json.Marshal(c, json.Deterministic(true))
	if err != nil {
		panic("config: encode: " + err.Error())
	}
	return data
}

func Decode(data []byte) (Config, error) {
	var c Config
	if err := json.Unmarshal(data, &c, json.RejectUnknownMembers(true)); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if _, err := CompileExcludes(c.Excludes); err != nil {
		return Config{}, err
	}
	return c, nil
}
