// Package config loads and validates LeaseGrid's configuration file.
//
// The file is YAML, for example:
//
//	providers:
//	  - name: akash
//	    enabled: true
//
// The package only reads, parses and validates. Which providers exist, and
// how errors are shown to the user, is decided by the caller.
package config

import (
	"errors"
	"fmt"
	"strings"
)

// Config is LeaseGrid's configuration.
type Config struct {
	// Providers lists the configured providers in file order.
	Providers []ProviderConfig `yaml:"providers"`
}

// ProviderConfig configures one provider.
type ProviderConfig struct {
	// Name identifies the provider, e.g. "akash". It is required and must be
	// unique within the file.
	Name string `yaml:"name"`

	// Enabled turns the provider on. When omitted, it is false.
	Enabled bool `yaml:"enabled"`
}

// Default returns the configuration used when no file exists: no providers.
func Default() Config {
	return Config{}
}

// EnabledProviders returns the names of the enabled providers, in file order.
func (c Config) EnabledProviders() []string {
	var names []string
	for _, p := range c.Providers {
		if p.Enabled {
			names = append(names, p.Name)
		}
	}
	return names
}

// ErrInvalid matches every error caused by a configuration file's contents:
// YAML syntax errors, unknown fields, wrong value types and validation
// problems. Check for it with errors.Is.
var ErrInvalid = errors.New("invalid configuration")

// ValidationError lists every validation problem found in a configuration,
// so users can fix them all at once. It matches ErrInvalid with errors.Is.
type ValidationError struct {
	Problems []string
}

// Error joins the problems into one message.
func (e *ValidationError) Error() string {
	return ErrInvalid.Error() + ": " + strings.Join(e.Problems, "; ")
}

// Unwrap makes errors.Is(err, ErrInvalid) report true for validation errors.
func (e *ValidationError) Unwrap() error {
	return ErrInvalid
}

// validate checks the rules that YAML decoding alone can't enforce.
func (c Config) validate() error {
	var problems []string
	seen := make(map[string]bool, len(c.Providers))
	for i, p := range c.Providers {
		switch {
		case strings.TrimSpace(p.Name) == "":
			problems = append(problems, fmt.Sprintf("providers[%d]: name is required", i))
		case seen[p.Name]:
			problems = append(problems, fmt.Sprintf("providers[%d]: duplicate name %q", i, p.Name))
		default:
			seen[p.Name] = true
		}
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}
