package config_test

import (
	"slices"
	"testing"

	"github.com/namangourh/leasegrid/internal/config"
)

func TestDefault(t *testing.T) {
	if got := config.Default(); len(got.Providers) != 0 {
		t.Errorf("Default().Providers = %v, want none", got.Providers)
	}
}

func TestEnabledProviders(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want []string
	}{
		{name: "no providers", cfg: config.Config{}, want: nil},
		{
			name: "only enabled ones, in file order",
			cfg: config.Config{Providers: []config.ProviderConfig{
				{Name: "akash", Enabled: true},
				{Name: "flux", Enabled: false},
				{Name: "render", Enabled: true},
			}},
			want: []string{"akash", "render"},
		},
		{
			name: "all disabled",
			cfg:  config.Config{Providers: []config.ProviderConfig{{Name: "akash"}}},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.EnabledProviders(); !slices.Equal(got, tt.want) {
				t.Errorf("EnabledProviders() = %v, want %v", got, tt.want)
			}
		})
	}
}
