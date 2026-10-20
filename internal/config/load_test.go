package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/namangourh/leasegrid/internal/config"
)

// lines joins YAML lines with newlines. Writing test documents this way
// avoids raw-string indentation pitfalls: YAML forbids tabs for indentation.
func lines(ls ...string) string {
	return strings.Join(ls, "\n") + "\n"
}

// writeConfig writes content to a config file in a fresh temporary
// directory, which the test framework deletes after the test.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoad(t *testing.T) {
	akash := config.ProviderConfig{Name: "akash", Enabled: true}
	flux := config.ProviderConfig{Name: "flux", Enabled: false}

	tests := []struct {
		name    string
		content string
		want    []config.ProviderConfig
		wantErr error  // matched with errors.Is; nil means success
		wantMsg string // substring of the error message
	}{
		{
			name:    "valid",
			content: lines("providers:", "  - name: akash", "    enabled: true"),
			want:    []config.ProviderConfig{akash},
		},
		{name: "empty file", content: "", want: nil},
		{name: "only comments", content: lines("# nothing configured yet"), want: nil},
		{name: "providers key without entries", content: lines("providers:"), want: nil},
		{
			name:    "disabled provider",
			content: lines("providers:", "  - name: flux", "    enabled: false"),
			want:    []config.ProviderConfig{flux},
		},
		{
			name:    "omitted enabled means disabled",
			content: lines("providers:", "  - name: flux"),
			want:    []config.ProviderConfig{flux},
		},
		{
			name: "two providers keep file order",
			content: lines(
				"providers:",
				"  - name: akash",
				"    enabled: true",
				"  - name: flux",
			),
			want: []config.ProviderConfig{akash, flux},
		},
		{
			name:    "malformed yaml",
			content: lines("providers: ["),
			wantErr: config.ErrInvalid,
			wantMsg: "yaml:",
		},
		{
			name:    "unknown provider field",
			content: lines("providers:", "  - name: akash", "    enabeld: true"),
			wantErr: config.ErrInvalid,
			wantMsg: "field enabeld not found",
		},
		{
			name:    "unknown top-level field",
			content: lines("provider:", "  - name: akash"),
			wantErr: config.ErrInvalid,
			wantMsg: "field provider not found",
		},
		{
			name:    "yes is accepted as true",
			content: lines("providers:", "  - name: akash", `    enabled: "yes"`),
			want:    []config.ProviderConfig{akash},
		},
		{
			name:    "wrong value type",
			content: lines("providers:", "  - name: akash", "    enabled: maybe"),
			wantErr: config.ErrInvalid,
			wantMsg: "cannot unmarshal",
		},
		{
			name:    "not a mapping",
			content: lines("just a string"),
			wantErr: config.ErrInvalid,
		},
		{
			name:    "missing provider name",
			content: lines("providers:", "  - enabled: true"),
			wantErr: config.ErrInvalid,
			wantMsg: "providers[0]: name is required",
		},
		{
			name:    "duplicate provider name",
			content: lines("providers:", "  - name: akash", "  - name: akash"),
			wantErr: config.ErrInvalid,
			wantMsg: `providers[1]: duplicate name "akash"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)

			got, err := config.Load(path)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Load() error = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("Load() error = %q, want it to contain %q", err, tt.wantMsg)
				}
				if !strings.Contains(err.Error(), path) {
					t.Errorf("Load() error = %q, want it to name the file %q", err, path)
				}
				return
			}
			if !slices.Equal(got.Providers, tt.want) {
				t.Errorf("Load().Providers = %v, want %v", got.Providers, tt.want)
			}
		})
	}
}

// TestLoadReportsAllProblems checks that validation reports every problem at
// once and exposes them through errors.As.
func TestLoadReportsAllProblems(t *testing.T) {
	path := writeConfig(t, lines(
		"providers:",
		"  - enabled: true",
		"  - name: akash",
		"  - name: akash",
	))

	_, err := config.Load(path)

	var verr *config.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Load() error = %v, want a *config.ValidationError", err)
	}
	want := []string{"providers[0]: name is required", `providers[2]: duplicate name "akash"`}
	if !slices.Equal(verr.Problems, want) {
		t.Errorf("Problems = %q, want %q", verr.Problems, want)
	}
	if !errors.Is(err, config.ErrInvalid) {
		t.Errorf("errors.Is(err, ErrInvalid) = false, want true")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load() error = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadOptional(t *testing.T) {
	t.Run("missing file gives defaults", func(t *testing.T) {
		cfg, err := config.LoadOptional(filepath.Join(t.TempDir(), "missing.yaml"))
		if err != nil {
			t.Fatalf("LoadOptional() error = %v", err)
		}
		if len(cfg.Providers) != 0 {
			t.Errorf("Providers = %v, want none", cfg.Providers)
		}
	})

	t.Run("existing file is loaded", func(t *testing.T) {
		cfg, err := config.LoadOptional(writeConfig(t, lines("providers:", "  - name: akash")))
		if err != nil {
			t.Fatalf("LoadOptional() error = %v", err)
		}
		if len(cfg.Providers) != 1 {
			t.Errorf("Providers = %v, want 1 entry", cfg.Providers)
		}
	})

	t.Run("invalid file is still an error", func(t *testing.T) {
		_, err := config.LoadOptional(writeConfig(t, lines("providers: [")))
		if !errors.Is(err, config.ErrInvalid) {
			t.Fatalf("LoadOptional() error = %v, want ErrInvalid", err)
		}
	})
}

// setUserConfigDir points os.UserConfigDir at dir on every OS: Windows reads
// APPDATA, Linux reads XDG_CONFIG_HOME (then HOME), and macOS reads HOME.
func setUserConfigDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

func TestDefaultPath(t *testing.T) {
	dir := t.TempDir()
	setUserConfigDir(t, dir)

	got, err := config.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if !strings.HasPrefix(got, dir) || !strings.HasSuffix(got, filepath.Join("leasegrid", "config.yaml")) {
		t.Errorf("DefaultPath() = %q, want leasegrid/config.yaml under %q", got, dir)
	}
}

func TestDefaultPathUnavailable(t *testing.T) {
	setUserConfigDir(t, "")
	if _, err := config.DefaultPath(); err == nil {
		t.Fatal("DefaultPath() error = nil, want an error when no config directory is known")
	}
}
