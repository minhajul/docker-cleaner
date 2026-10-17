// Package config loads docker-cleaner's optional YAML config file.
//
// The file lives at $XDG_CONFIG_HOME/docker-cleaner/config.yaml, or
// ~/.config/docker-cleaner/config.yaml when XDG_CONFIG_HOME is unset (on
// macOS too). Example:
//
//	# Resources matching any pattern are hidden and can't be deleted.
//	protect:
//	  - "postgres:*"   # image repo:tag glob
//	  - "my-db"        # container name
//	  - "prod-*"
//
// See docker.Protector for how patterns match.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/minhajul/docker-cleaner/docker"
	"gopkg.in/yaml.v3"
)

// Config is the content of the config file. Its zero value is the default
// configuration, used when there is no file.
type Config struct {
	// Protect lists glob patterns; resources whose name matches one are
	// hidden and never deleted. Each has passed docker.ValidatePattern.
	Protect []string `yaml:"protect"`
}

// DefaultPath returns where the config file is looked for when no path is
// given: $XDG_CONFIG_HOME/docker-cleaner/config.yaml, or
// ~/.config/docker-cleaner/config.yaml if XDG_CONFIG_HOME is unset or not
// absolute (the XDG spec says to ignore relative values).
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "docker-cleaner", "config.yaml"), nil
}

// Load reads the config file at path. With an empty path it reads the file at
// DefaultPath, and a missing file (or an unknown home directory) yields the
// default Config; a path given explicitly must exist. Malformed YAML, unknown
// keys and invalid patterns are errors naming the file.
func Load(path string) (Config, error) {
	explicit := path != ""
	if !explicit {
		var err error
		if path, err = DefaultPath(); err != nil {
			return Config{}, nil
		}
	}

	data, err := os.ReadFile(path) // #nosec G304 -- reading the user's own config file is the point
	if err != nil {
		if !explicit && errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("config: %w", err)
	}

	cfg, err := parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// parse decodes and validates the file's contents. Unknown keys are rejected
// so a typo such as "protcet:" is reported instead of silently ignored.
func parse(data []byte) (Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		// io.EOF means the file is empty or holds only comments.
		return Config{}, err
	}

	for i, pat := range cfg.Protect {
		if err := docker.ValidatePattern(pat); err != nil {
			return Config{}, fmt.Errorf("protect[%d]: %w", i, err)
		}
	}
	return cfg, nil
}
