package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Load reads, parses and validates the configuration file at path. The file
// must exist: a missing file returns an error matching fs.ErrNotExist.
// Problems with the file's contents return errors matching ErrInvalid.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg, err := parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// LoadOptional is like Load, but a missing file is not an error: it returns
// Default(). Use it for the default location, where having no file is normal.
func LoadOptional(path string) (Config, error) {
	cfg, err := Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	return cfg, err
}

// DefaultPath returns where LeaseGrid looks for its configuration when no
// path is given: leasegrid/config.yaml inside the user's configuration
// directory, such as %AppData% on Windows or ~/.config on Linux.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "leasegrid", "config.yaml"), nil
}

// parse decodes and validates a YAML document. Unknown fields are rejected,
// so a typo such as "enabeld" fails loudly instead of being ignored.
func parse(data []byte) (Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return Default(), nil // empty file or only comments: nothing configured
		}
		return Config{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
