// Package config loads LeaseGrid's configuration.
//
// Load currently returns built-in defaults and reads no file. The file
// format and library are chosen in the config slice.
package config

// Config is LeaseGrid's configuration.
type Config struct {
	// Providers names the providers to enable, e.g. "akash".
	// Empty means none are enabled.
	Providers []string
}

// Load returns the configuration to use: for now, the defaults, with no
// providers enabled. It returns an error so callers already handle the
// failures that reading a real file will bring.
func Load() (Config, error) {
	return Config{}, nil
}
