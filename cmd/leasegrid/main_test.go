package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/config"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers/fake"
)

// binPath is the leasegrid binary that TestMain builds once for smoke tests.
var binPath string

// TestMain runs once per package, before any test. It builds the real binary
// so smoke tests exercise exactly what users run, without recompiling per case.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "leasegrid-test")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	binPath = filepath.Join(dir, "leasegrid")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		log.Fatalf("build leasegrid: %v\n%s", err, out)
	}

	code := m.Run()
	_ = os.RemoveAll(dir) // best-effort cleanup of the temp binary
	os.Exit(code)
}

// isolateUserConfig points os.UserConfigDir at an empty temporary directory,
// so tests never read the developer's real LeaseGrid configuration.
func isolateUserConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)         // Windows
	t.Setenv("XDG_CONFIG_HOME", dir) // Linux
	t.Setenv("HOME", dir)            // macOS, and the Linux fallback
}

// runBinary runs the built binary with an empty user config directory, so
// results never depend on the developer's real configuration. It returns
// the combined output and the exit code.
func runBinary(t *testing.T, args ...string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "APPDATA="+dir, "XDG_CONFIG_HOME="+dir, "HOME="+dir)

	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	case err != nil:
		t.Fatalf("start binary: %v", err)
	}
	return string(out), 0
}

// newTestDeps returns deps whose service uses ps, plus the buffers that
// capture what run writes to stdout and stderr.
func newTestDeps(ps ...app.Provider) (deps, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	d := deps{
		stdout:     &stdout,
		stderr:     &stderr,
		newService: func(string) (*app.Service, error) { return app.New(ps...), nil },
	}
	return d, &stdout, &stderr
}

// TestRun covers the top-level flags and command dispatch, in-process.
func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    error  // nil means success
		wantStdout string // exact match
		wantStderr string // substring; empty means stderr must be empty
	}{
		{name: "version flag", args: []string{"-version"}, wantStdout: "leasegrid 0.1.0\n"},
		{name: "double-dash version flag", args: []string{"--version"}, wantStdout: "leasegrid 0.1.0\n"},
		{name: "help flag", args: []string{"--help"}, wantStderr: "LeaseGrid manages compute leases"},
		{name: "unknown flag", args: []string{"--bogus"}, wantErr: errUsage, wantStderr: "flag provided but not defined: -bogus"},
		{name: "no arguments shows usage", args: nil, wantStderr: "Usage:"},
		{name: "unknown command", args: []string{"frobnicate"}, wantErr: errUsage, wantStderr: `unknown command "frobnicate"`},
		{name: "list help", args: []string{"list", "--help"}, wantStderr: "leasegrid list [flags]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps()
			err := run(t.Context(), tt.args, d)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", got, tt.wantStdout)
			}
			if tt.wantStderr == "" {
				if stderr.Len() != 0 {
					t.Errorf("stderr = %q, want empty", stderr.String())
				}
			} else if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

// TestListJSON is the CLI-boundary test: it drives the list command with
// injected fake providers and checks that stdout decodes into Go structs.
func TestListJSON(t *testing.T) {
	web := lease.Lease{ID: "a1", Provider: "akash", Name: "web", Status: lease.StatusActive}
	db := lease.Lease{ID: "a2", Provider: "akash", Name: "db", Status: lease.StatusClosed}
	api := lease.Lease{ID: "f1", Provider: "flux", Name: "api", Status: lease.StatusActive}
	akash := &fake.Provider{ProviderName: "akash", Leases: []lease.Lease{web, db}}
	flux := &fake.Provider{ProviderName: "flux", Leases: []lease.Lease{api}}

	tests := []struct {
		name string
		args []string
		want []lease.Lease
	}{
		{name: "explicit json format", args: []string{"list", "--format", "json"}, want: []lease.Lease{web, db, api}},
		{name: "json is the default", args: []string{"list"}, want: []lease.Lease{web, db, api}},
		{name: "filter by provider", args: []string{"list", "--provider", "flux"}, want: []lease.Lease{api}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps(akash, flux)
			if err := run(t.Context(), tt.args, d); err != nil {
				t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
			}

			var got []lease.Lease
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("leases = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestListErrors checks that failures give a useful message, the right exit
// code, and no partial output on stdout.
func TestListErrors(t *testing.T) {
	akash := &fake.Provider{ProviderName: "akash"}
	fluxDown := &fake.Provider{ProviderName: "flux", Err: errors.New("connection refused")}

	tests := []struct {
		name      string
		providers []app.Provider
		args      []string
		wantErr   error
		wantCode  int
		wantMsg   string // must appear in stderr or in the returned error
	}{
		{
			name:      "unknown format",
			providers: []app.Provider{akash},
			args:      []string{"list", "--format", "yaml"},
			wantErr:   errUsage,
			wantCode:  2,
			wantMsg:   `unknown output format "yaml"`,
		},
		{
			name:      "provider error",
			providers: []app.Provider{fluxDown},
			args:      []string{"list"},
			wantErr:   fluxDown.Err,
			wantCode:  1,
			wantMsg:   "provider flux: connection refused",
		},
		{
			name:     "no providers",
			args:     []string{"list"},
			wantErr:  app.ErrNoProviders,
			wantCode: 1,
			wantMsg:  "no providers configured",
		},
		{
			name:      "unknown provider",
			providers: []app.Provider{akash},
			args:      []string{"list", "--provider", "render"},
			wantErr:   app.ErrUnknownProvider,
			wantCode:  1,
			wantMsg:   `unknown provider "render"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps(tt.providers...)
			err := run(t.Context(), tt.args, d)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
			}
			if code := exitCode(err); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			seen := stderr.String()
			if err != nil {
				seen += err.Error()
			}
			if !strings.Contains(seen, tt.wantMsg) {
				t.Errorf("message %q not found in stderr or error:\n%s", tt.wantMsg, seen)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty on error", stdout.String())
			}
		})
	}
}

// TestExitCode checks the single place where errors become exit statuses.
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: 0},
		{name: "usage error", err: fmt.Errorf("%w: bad flag", errUsage), want: 2},
		{name: "other error", err: errors.New("provider unavailable"), want: 1},
		{name: "interrupted", err: fmt.Errorf("provider slow: %w", context.Canceled), want: 130},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// TestBuildService checks that configuration turns into the right service.
func TestBuildService(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.Config
		wantErr     string // substring of buildService's error; empty means success
		wantListErr error  // expected error from ListLeases on the built service
	}{
		{
			name:        "no providers configured",
			cfg:         config.Config{},
			wantListErr: app.ErrNoProviders,
		},
		{
			name:        "only disabled providers",
			cfg:         config.Config{Providers: []config.ProviderConfig{{Name: "akash"}}},
			wantListErr: app.ErrNoProviders,
		},
		{
			name:    "enabled provider not implemented",
			cfg:     config.Config{Providers: []config.ProviderConfig{{Name: "akash", Enabled: true}}},
			wantErr: `provider "akash" is not implemented yet`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := buildService(tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("buildService() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildService() error = %v", err)
			}
			if _, err := svc.ListLeases(t.Context(), lease.Criteria{}); !errors.Is(err, tt.wantListErr) {
				t.Errorf("ListLeases() error = %v, want %v", err, tt.wantListErr)
			}
		})
	}
}

// TestLoadService checks the default-location wiring main uses.
func TestLoadService(t *testing.T) {
	t.Run("no file at the default location", func(t *testing.T) {
		isolateUserConfig(t)
		svc, err := loadService("")
		if err != nil {
			t.Fatalf("loadService() error = %v", err)
		}
		if _, err := svc.ListLeases(t.Context(), lease.Criteria{}); !errors.Is(err, app.ErrNoProviders) {
			t.Errorf("ListLeases() error = %v, want %v", err, app.ErrNoProviders)
		}
	})

	t.Run("file at the default location is used", func(t *testing.T) {
		isolateUserConfig(t)
		path, err := config.DefaultPath()
		if err != nil {
			t.Fatalf("DefaultPath() error = %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create config dir: %v", err)
		}
		content := "providers:\n  - name: akash\n    enabled: true\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		_, err = loadService("")
		if err == nil || !strings.Contains(err.Error(), `provider "akash" is not implemented yet`) {
			t.Fatalf("loadService() error = %v, want provider not implemented", err)
		}
	})

	t.Run("explicit path is used", func(t *testing.T) {
		isolateUserConfig(t)
		path := filepath.Join(t.TempDir(), "custom.yaml")
		if err := os.WriteFile(path, []byte("providers:\n  - name: flux\n    enabled: true\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		_, err := loadService(path)
		if err == nil || !strings.Contains(err.Error(), `provider "flux" is not implemented yet`) {
			t.Fatalf("loadService() error = %v, want provider not implemented", err)
		}
	})

	t.Run("explicit path must exist", func(t *testing.T) {
		isolateUserConfig(t)
		_, err := loadService(filepath.Join(t.TempDir(), "missing.yaml"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadService() error = %v, want fs.ErrNotExist", err)
		}
	})
}

// TestBinarySmoke runs the built binary as a separate process, checking real
// exit codes and output: the CLI-boundary check that in-process tests can't do.
func TestBinarySmoke(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string // substring of combined stdout+stderr
	}{
		{name: "version", args: []string{"--version"}, wantCode: 0, wantOut: "leasegrid 0.1.0"},
		{name: "help", args: []string{"--help"}, wantCode: 0, wantOut: "LeaseGrid manages compute leases"},
		{name: "unknown flag", args: []string{"--bogus"}, wantCode: 2, wantOut: "flag provided but not defined"},
		{name: "list without providers", args: []string{"list"}, wantCode: 1, wantOut: "leasegrid: no providers configured"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runBinary(t, tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\noutput: %s", code, tt.wantCode, out)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("output does not contain %q:\n%s", tt.wantOut, out)
			}

		})
	}
}

// TestListCancellation proves the ctx passed to run reaches the provider:
// a provider that never answers on its own returns as soon as ctx ends.
func TestListCancellation(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		slow := fake.NewBlocking("slow")
		d, stdout, _ := newTestDeps(slow)
		ctx, cancel := context.WithCancel(t.Context())

		errc := make(chan error, 1)
		go func() { errc <- run(ctx, []string{"list"}, d) }()
		<-slow.Started()
		cancel()

		err := waitErr(t, errc)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run() error = %v, want context.Canceled", err)
		}
		if code := exitCode(err); code != 130 {
			t.Errorf("exit code = %d, want 130", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("stdout = %q, want empty", stdout.String())
		}
	})

	t.Run("deadline", func(t *testing.T) {
		d, _, _ := newTestDeps(fake.NewBlocking("slow"))
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		errc := make(chan error, 1)
		go func() { errc <- run(ctx, []string{"list"}, d) }()

		err := waitErr(t, errc)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("run() error = %v, want context.DeadlineExceeded", err)
		}
		if code := exitCode(err); code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	})
}

// waitErr returns the error sent on errc, or fails the test if none arrives
// in time, which would mean cancellation never reached the provider.
func waitErr(t *testing.T, errc <-chan error) error {
	t.Helper()
	select {
	case err := <-errc:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after its context ended")
		return nil
	}
}

// TestConfigFlag drives the CLI with real config loading (only the user
// config directory is redirected) to check that --config is parsed, the
// file is loaded, and errors reach the process boundary.
func TestConfigFlag(t *testing.T) {
	tests := []struct {
		name       string
		content    string   // written to a temporary config file
		noFile     bool     // use a path that doesn't exist instead
		args       []string // "CONFIG" is replaced with the config file's path
		wantErr    error    // matched with errors.Is when set
		wantCode   int
		wantMsg    string // must appear in stderr or the returned error
		wantStdout string
	}{
		{
			name:     "enabled provider is not implemented yet",
			content:  "providers:\n  - name: akash\n    enabled: true\n",
			args:     []string{"--config", "CONFIG", "list"},
			wantCode: 1,
			wantMsg:  `provider "akash" is not implemented yet`,
		},
		{
			name:     "only disabled providers",
			content:  "providers:\n  - name: akash\n",
			args:     []string{"--config", "CONFIG", "list"},
			wantErr:  app.ErrNoProviders,
			wantCode: 1,
			wantMsg:  "no providers configured",
		},
		{
			name:     "empty file",
			content:  "",
			args:     []string{"--config", "CONFIG", "list"},
			wantErr:  app.ErrNoProviders,
			wantCode: 1,
		},
		{
			name:     "malformed file",
			content:  "providers: [\n",
			args:     []string{"--config", "CONFIG", "list"},
			wantErr:  config.ErrInvalid,
			wantCode: 1,
			wantMsg:  "invalid configuration",
		},
		{
			name:     "unknown field",
			content:  "providers:\n  - name: akash\n    enabeld: true\n",
			args:     []string{"--config", "CONFIG", "list"},
			wantErr:  config.ErrInvalid,
			wantCode: 1,
			wantMsg:  "field enabeld not found",
		},
		{
			name:     "missing explicit file",
			noFile:   true,
			args:     []string{"--config", "CONFIG", "list"},
			wantErr:  fs.ErrNotExist,
			wantCode: 1,
			wantMsg:  "read config",
		},
		{
			name:       "version does not load config",
			noFile:     true,
			args:       []string{"--config", "CONFIG", "--version"},
			wantCode:   0,
			wantStdout: "leasegrid 0.1.0\n",
		},
		{
			name:     "config flag after the command is rejected",
			content:  "providers:\n",
			args:     []string{"list", "--config", "CONFIG"},
			wantErr:  errUsage,
			wantCode: 2,
			wantMsg:  "flag provided but not defined: -config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateUserConfig(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if !tt.noFile {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			args := slices.Clone(tt.args)
			for i, a := range args {
				if a == "CONFIG" {
					args[i] = path
				}
			}

			var stdout, stderr bytes.Buffer
			err := run(t.Context(), args, deps{stdout: &stdout, stderr: &stderr, newService: loadService})

			if code := exitCode(err); code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (error: %v)\nstderr: %s", code, tt.wantCode, err, stderr.String())
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("run() error = %v, want it to match %v", err, tt.wantErr)
			}
			seen := stderr.String()
			if err != nil {
				seen += err.Error()
			}
			if !strings.Contains(seen, tt.wantMsg) {
				t.Errorf("message %q not found in stderr or error:\n%s", tt.wantMsg, seen)
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", got, tt.wantStdout)
			}
		})
	}
}

// TestBinaryConfig runs the real binary with --config, proving the flag,
// file loading and exit codes work end to end in a separate process.
func TestBinaryConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	valid := write("valid.yaml", "providers:\n  - name: akash\n    enabled: true\n")
	malformed := write("malformed.yaml", "providers: [\n")
	missing := filepath.Join(dir, "missing.yaml")

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{name: "valid file", args: []string{"--config", valid, "list"}, wantCode: 1, wantOut: `leasegrid: provider "akash" is not implemented yet`},
		{name: "malformed file", args: []string{"--config", malformed, "list"}, wantCode: 1, wantOut: "invalid configuration"},
		{name: "missing file", args: []string{"--config", missing, "list"}, wantCode: 1, wantOut: "leasegrid: read config"},
		{name: "version ignores config", args: []string{"--config", missing, "--version"}, wantCode: 0, wantOut: "leasegrid 0.1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runBinary(t, tt.args...)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\noutput: %s", code, tt.wantCode, out)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("output does not contain %q:\n%s", tt.wantOut, out)
			}
		})
	}
}
