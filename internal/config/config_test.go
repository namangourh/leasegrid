package config_test

import (
	"testing"

	"github.com/namangourh/leasegrid/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Providers) != 0 {
		t.Errorf("Load().Providers = %v, want none enabled by default", cfg.Providers)
	}
}
