package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/config"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
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

// testNow is the fixed time the tests' clock returns.
var testNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func testClock() time.Time { return testNow }

// newTestDeps returns deps whose service uses ps and whose clock is fixed at
// testNow, plus the buffers that capture what run writes to stdout and stderr.
func newTestDeps(ps ...app.Provider) (deps, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	d := deps{
		stdout:     &stdout,
		stderr:     &stderr,
		now:        testClock,
		newService: func(string, func() time.Time) (*app.Service, error) { return app.New(ps...), nil },
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
		{name: "show help", args: []string{"show", "--help"}, wantStderr: "leasegrid show [flags] <id>"},
		{name: "usage lists show", args: nil, wantStderr: "show    Show one lease in detail"},
		{name: "status help", args: []string{"status", "--help"}, wantStderr: "leasegrid status [flags]"},
		{name: "usage lists status", args: nil, wantStderr: "status  Summarize leases"},
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
		{name: "filter by provider", args: []string{"list", "--provider", "flux", "--format", "json"}, want: []lease.Lease{api}},
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
			wantCode:  2,
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

// demoConfig is the example config that enables both demo providers. The
// README points users at it, so these tests keep the documented demo working.
var demoConfig = filepath.Join("..", "..", "examples", "demo.yaml")

// runDemo runs leasegrid with --config demoConfig followed by args, using
// real config loading and the demo providers, with the clock fixed at testNow.
func runDemo(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	d := deps{stdout: &out, stderr: &errOut, now: testClock, newService: loadService}
	err = run(t.Context(), append([]string{"--config", demoConfig}, args...), d)
	return out.String(), errOut.String(), err
}

// TestListDemoTable is the golden test for the README demo: with the clock
// fixed, the default command prints exactly this table on every run.
func TestListDemoTable(t *testing.T) {
	want := `ID        PROVIDER  NAME    STATUS              EXPIRES                                  COST/HOUR
1001/1/1  demo-b    worker  insufficient_funds  2026-10-20 10:00 UTC (expired 2h0m ago)  900 uakt
1001/1/1  demo-a    web     active              2026-10-20 15:00 UTC (in 3h0m)           4990 ibc/170C...F2F1
2001/1/1  demo-b    gpu     active              2026-10-20 15:00 UTC (in 3h0m)           36000 uakt
1002/1/1  demo-a    api     active              2026-10-30 12:00 UTC (in 10d0h)          1800 uakt
1003/1/1  demo-a    batch   closed              -                                        1200 uakt
`
	stdout, stderr, err := runDemo(t, "list")
	if err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
	}
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestListDemo checks which leases each combination of flags selects, and in
// what order, in JSON and YAML, through real config loading and the demo
// providers. Every demo lease has a different name, so the list of names
// identifies the result.
func TestListDemo(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string // lease names, in output order
	}{
		{name: "no flags", args: nil, want: []string{"worker", "web", "gpu", "api", "batch"}},
		{name: "one provider", args: []string{"--provider", "demo-a"}, want: []string{"web", "api", "batch"}},
		{name: "one status", args: []string{"--status", "active"}, want: []string{"web", "gpu", "api"}},
		{name: "provider and status", args: []string{"--provider", "demo-b", "--status", "active"}, want: []string{"gpu"}},
		{name: "expiring within includes expired", args: []string{"--expiring-within", "24h"}, want: []string{"worker", "web", "gpu"}},
		{name: "expiry at the cutoff is excluded", args: []string{"--expiring-within", "3h"}, want: []string{"worker"}},
		{name: "expiring within and status", args: []string{"--expiring-within", "24h", "--status", "active"}, want: []string{"web", "gpu"}},
		{name: "sort by expires is the default", args: []string{"--sort-by", "expires"}, want: []string{"worker", "web", "gpu", "api", "batch"}},
		{name: "sort by id, provider breaks ties", args: []string{"--sort-by", "id"}, want: []string{"web", "worker", "api", "batch", "gpu"}},
		{name: "sort by provider, id breaks ties", args: []string{"--sort-by", "provider"}, want: []string{"web", "api", "batch", "worker", "gpu"}},
		{name: "sort by status in lifecycle order", args: []string{"--sort-by", "status"}, want: []string{"web", "api", "gpu", "worker", "batch"}},
	}

	// JSON and YAML must select and order leases the same way. Both packages'
	// Unmarshal functions have the same signature, so either fits the field.
	formats := []struct {
		name      string
		unmarshal func([]byte, any) error
	}{
		{name: "json", unmarshal: json.Unmarshal},
		{name: "yaml", unmarshal: yaml.Unmarshal},
	}

	for _, tt := range tests {
		for _, f := range formats {
			t.Run(tt.name+"/"+f.name, func(t *testing.T) {
				stdout, stderr, err := runDemo(t, append([]string{"list", "--format", f.name}, tt.args...)...)
				if err != nil {
					t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
				}
				var leases []lease.Lease
				if err := f.unmarshal([]byte(stdout), &leases); err != nil {
					t.Fatalf("stdout is not valid %s: %v\nstdout: %q", f.name, err, stdout)
				}
				var got []string
				for _, l := range leases {
					got = append(got, l.Name)
				}
				if !slices.Equal(got, tt.want) {
					t.Errorf("lease names = %v, want %v", got, tt.want)
				}
			})
		}
	}
}

// TestListEmpty checks that an empty result keeps stdout for data: the table
// prints nothing there and says so on stderr, while JSON and YAML print [].
func TestListEmpty(t *testing.T) {
	tests := []struct {
		format     string
		wantStdout string
		wantStderr string
	}{
		{format: "table", wantStdout: "", wantStderr: "No leases found.\n"},
		{format: "json", wantStdout: "[]\n", wantStderr: ""},
		{format: "yaml", wantStdout: "[]\n", wantStderr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			stdout, stderr, err := runDemo(t, "list", "--provider", "demo-b", "--status", "closed", "--format", tt.format)
			if err != nil {
				t.Fatalf("run() error = %v, want success\nstderr: %s", err, stderr)
			}
			if stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
		})
	}
}

// TestListInvalidFlags checks that a bad flag is a usage error (exit 2) that
// names the problem and writes nothing to stdout. It is caught before the
// service is even built, so no provider is ever called.
func TestListInvalidFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string // must appear in stderr
	}{
		{name: "unknown format", args: []string{"--format", "csv"}, wantMsg: `invalid value "csv" for flag -format: unknown output format "csv"`},
		{name: "expiring is not a status", args: []string{"--status", "expiring"}, wantMsg: `invalid value "expiring" for flag -status: unsupported status "expiring"`},
		{name: "cost is not a sort key", args: []string{"--sort-by", "cost"}, wantMsg: `invalid value "cost" for flag -sort-by: unknown sort key "cost"`},
		{name: "days are not a duration unit", args: []string{"--expiring-within", "3d"}, wantMsg: "not a duration such as 90m or 24h"},
		{name: "zero duration", args: []string{"--expiring-within", "0"}, wantMsg: "must be greater than zero"},
		{name: "negative duration", args: []string{"--expiring-within", "-1h"}, wantMsg: "must be greater than zero"},
		{name: "missing value", args: []string{"--status"}, wantMsg: "flag needs an argument: -status"},
		{name: "unknown flag", args: []string{"--reverse"}, wantMsg: "flag provided but not defined: -reverse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps()
			built := false
			d.newService = func(string, func() time.Time) (*app.Service, error) {
				built = true
				return app.New(), nil
			}

			err := run(t.Context(), append([]string{"list"}, tt.args...), d)
			if code := exitCode(err); code != 2 {
				t.Errorf("exit code = %d, want 2 (error: %v)", code, err)
			}
			if !strings.Contains(stderr.String(), tt.wantMsg) {
				t.Errorf("stderr does not contain %q:\n%s", tt.wantMsg, stderr)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if built {
				t.Error("the service was built, want the flag rejected before any provider is called")
			}
		})
	}
}

// TestListTableReplacesControlCharacters checks, at the CLI boundary, that
// text from a provider can't send escape sequences to the user's terminal.
func TestListTableReplacesControlCharacters(t *testing.T) {
	// \x1b]0;...\a sets the terminal window's title; \x1b[2J clears the screen.
	evil := &fake.Provider{ProviderName: "evil", Leases: []lease.Lease{
		{ID: "1", Provider: "evil", Name: "\x1b]0;pwned\a\x1b[2J", Status: lease.StatusActive},
	}}
	d, stdout, stderr := newTestDeps(evil)
	if err := run(t.Context(), []string{"list"}, d); err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
	}
	if strings.ContainsAny(stdout.String(), "\x1b\a") {
		t.Errorf("stdout = %q, want no control characters", stdout.String())
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
			name:    "unknown provider",
			cfg:     config.Config{Providers: []config.ProviderConfig{{Name: "flux", Enabled: true}}},
			wantErr: `unknown provider "flux" (available: akash, demo-a, demo-b)`,
		},
		{
			name: "demo providers",
			cfg: config.Config{Providers: []config.ProviderConfig{
				{Name: "demo-a", Enabled: true},
				{Name: "demo-b", Enabled: true},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := buildService(tt.cfg, testClock)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("buildService() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildService() error = %v", err)
			}
			if _, err := svc.ListLeases(t.Context(), app.ListOptions{}); !errors.Is(err, tt.wantListErr) {
				t.Errorf("ListLeases() error = %v, want %v", err, tt.wantListErr)
			}
		})
	}
}

// TestNewProvider checks how each config entry becomes a provider. None of
// these cases makes a network call: building a provider never does.
func TestNewProvider(t *testing.T) {
	const owner = "akash1testowner0000000000000000000000000000"
	tests := []struct {
		name     string
		pc       config.ProviderConfig
		wantName string // the built provider's Name, when no error is expected
		wantErr  string // substring of the error; empty means success
	}{
		{
			name:     "akash with an owner",
			pc:       config.ProviderConfig{Name: "akash", Owner: owner},
			wantName: "akash",
		},
		{
			name:     "akash with an owner and an endpoint",
			pc:       config.ProviderConfig{Name: "akash", Owner: owner, Endpoint: "https://akash.example.com"},
			wantName: "akash",
		},
		{
			name:    "akash without an owner",
			pc:      config.ProviderConfig{Name: "akash"},
			wantErr: "akash: owner must be an akash1... wallet address",
		},
		{
			name:    "akash with a plain http endpoint",
			pc:      config.ProviderConfig{Name: "akash", Owner: owner, Endpoint: "http://akash.example.com"},
			wantErr: "akash: base URL must be an https URL",
		},
		{
			name:     "demo provider",
			pc:       config.ProviderConfig{Name: "demo-a"},
			wantName: "demo-a",
		},
		{
			name:    "demo provider with an owner",
			pc:      config.ProviderConfig{Name: "demo-a", Owner: owner},
			wantErr: `provider "demo-a" does not take an owner`,
		},
		{
			name:    "demo provider with an endpoint",
			pc:      config.ProviderConfig{Name: "demo-b", Endpoint: "https://akash.example.com"},
			wantErr: `provider "demo-b" does not take an endpoint`,
		},
		{
			name:    "unknown provider",
			pc:      config.ProviderConfig{Name: "flux"},
			wantErr: `unknown provider "flux" (available: akash, demo-a, demo-b)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := newProvider(tt.pc, testClock)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("newProvider() error = %v, want it to contain %q", err, tt.wantErr)
				}
				if p != nil {
					t.Errorf("newProvider() = %#v, want nil along with the error", p)
				}
				return
			}
			if err != nil {
				t.Fatalf("newProvider() error = %v", err)
			}
			if got := p.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

// TestListAkashFailureHidesSettings runs list against an Akash endpoint that
// fails, and checks that neither the endpoint nor the owner address appears
// in the output. The test server's certificate isn't trusted, so the request
// fails during the TLS handshake and nothing leaves the machine.
func TestListAkashFailureHidesSettings(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the failed handshake is expected
	srv.StartTLS()
	defer srv.Close()

	const owner = "akash1testowner0000000000000000000000000000"
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "providers:\n  - name: akash\n    enabled: true\n    owner: " + owner + "\n    endpoint: " + srv.URL + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	d := deps{stdout: &stdout, stderr: &stderr, now: testClock, newService: loadService}
	err := run(t.Context(), []string{"--config", path, "list"}, d)
	if !errors.Is(err, providers.ErrUnavailable) {
		t.Fatalf("run() error = %v, want %v", err, providers.ErrUnavailable)
	}
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on error", stdout.String())
	}
	seen := stderr.String() + err.Error()
	for _, private := range []string{owner, srv.Listener.Addr().String()} {
		if strings.Contains(seen, private) {
			t.Errorf("output contains %q:\n%s", private, seen)
		}
	}
}

// TestLoadService checks the default-location wiring main uses.
func TestLoadService(t *testing.T) {
	t.Run("no file at the default location", func(t *testing.T) {
		isolateUserConfig(t)
		svc, err := loadService("", testClock)
		if err != nil {
			t.Fatalf("loadService() error = %v", err)
		}
		if _, err := svc.ListLeases(t.Context(), app.ListOptions{}); !errors.Is(err, app.ErrNoProviders) {
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
		content := "providers:\n  - name: flux\n    enabled: true\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		_, err = loadService("", testClock)
		if err == nil || !strings.Contains(err.Error(), `unknown provider "flux"`) {
			t.Fatalf("loadService() error = %v, want unknown provider", err)
		}
	})

	t.Run("explicit path is used", func(t *testing.T) {
		isolateUserConfig(t)
		path := filepath.Join(t.TempDir(), "custom.yaml")
		if err := os.WriteFile(path, []byte("providers:\n  - name: flux\n    enabled: true\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		_, err := loadService(path, testClock)
		if err == nil || !strings.Contains(err.Error(), `unknown provider "flux"`) {
			t.Fatalf("loadService() error = %v, want unknown provider", err)
		}
	})

	t.Run("explicit path must exist", func(t *testing.T) {
		isolateUserConfig(t)
		_, err := loadService(filepath.Join(t.TempDir(), "missing.yaml"), testClock)
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
			name:     "unknown provider",
			content:  "providers:\n  - name: flux\n    enabled: true\n",
			args:     []string{"--config", "CONFIG", "list"},
			wantCode: 1,
			wantMsg:  `unknown provider "flux"`,
		},
		{
			name:     "akash without an owner",
			content:  "providers:\n  - name: akash\n    enabled: true\n",
			args:     []string{"--config", "CONFIG", "list"},
			wantCode: 1,
			wantMsg:  "akash: owner must be an akash1... wallet address",
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
		{
			name:     "show loads the config",
			content:  "providers: [\n",
			args:     []string{"--config", "CONFIG", "show", "1001/1/1"},
			wantErr:  config.ErrInvalid,
			wantCode: 1,
			wantMsg:  "invalid configuration",
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
			err := run(t.Context(), args, deps{stdout: &stdout, stderr: &stderr, now: testClock, newService: loadService})

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
	valid := write("valid.yaml", "providers:\n  - name: flux\n    enabled: true\n")
	malformed := write("malformed.yaml", "providers: [\n")
	missing := filepath.Join(dir, "missing.yaml")

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{name: "valid file", args: []string{"--config", valid, "list"}, wantCode: 1, wantOut: `leasegrid: unknown provider "flux"`},
		{name: "malformed file", args: []string{"--config", malformed, "list"}, wantCode: 1, wantOut: "invalid configuration"},
		{name: "missing file", args: []string{"--config", missing, "list"}, wantCode: 1, wantOut: "leasegrid: read config"},
		{name: "version ignores config", args: []string{"--config", missing, "--version"}, wantCode: 0, wantOut: "leasegrid 0.1.0"},
		{name: "demo config", args: []string{"--config", demoConfig, "list"}, wantCode: 0, wantOut: "4990 ibc/170C...F2F1"},
		{name: "demo empty result", args: []string{"--config", demoConfig, "list", "--provider", "demo-b", "--status", "closed"}, wantCode: 0, wantOut: "No leases found."},
		{name: "demo unknown provider", args: []string{"--config", demoConfig, "list", "--provider", "akahs"}, wantCode: 2, wantOut: `leasegrid: unknown provider "akahs" (configured: demo-a, demo-b)`},
		{name: "demo invalid status", args: []string{"--config", demoConfig, "list", "--status", "expiring"}, wantCode: 2, wantOut: `invalid value "expiring" for flag -status`},
		{name: "demo show", args: []string{"--config", demoConfig, "show", "1002/1/1"}, wantCode: 0, wantOut: "Name:       api\n"},
		{name: "demo show missing", args: []string{"--config", demoConfig, "show", "missing-id"}, wantCode: 1, wantOut: `leasegrid: lease "missing-id" not found (searched: demo-a, demo-b)`},
		{name: "demo show ambiguous", args: []string{"--config", demoConfig, "show", "1001/1/1"}, wantCode: 2, wantOut: "choose one with --provider NAME"},
		{name: "show without an ID", args: []string{"--config", demoConfig, "show"}, wantCode: 2, wantOut: "show needs exactly one lease ID, got []"},
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

// TestShowDemoDetail is the golden test for show: with the clock fixed, the
// README demo command prints exactly this view on every run.
func TestShowDemoDetail(t *testing.T) {
	want := `ID:         1002/1/1
Provider:   demo-a
Name:       api
Status:     active
Expires:    2026-10-30 12:00 UTC (in 10d0h)
Cost/hour:  1800 uakt
CPU:        1
Memory:     1 GiB
Storage:    5 GiB
GPUs:       0
`
	stdout, stderr, err := runDemo(t, "show", "1002/1/1")
	if err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
	}
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestShowDemo checks how show finds, or fails to find, a lease through real
// config loading and the demo providers. Both demo providers have a lease
// with ID 1001/1/1.
func TestShowDemo(t *testing.T) {
	tests := []struct {
		name     string
		args     []string // after "show"
		wantName string   // the Name line on stdout, for a success
		wantErr  error    // matched with errors.Is when set
		wantCode int
		wantMsg  string // must appear in stderr or the returned error
	}{
		{name: "provider before the ID", args: []string{"--provider", "demo-a", "1001/1/1"}, wantName: "web"},
		{name: "provider after the ID", args: []string{"1001/1/1", "--provider", "demo-b"}, wantName: "worker"},
		{name: "unique ID needs no provider", args: []string{"2001/1/1"}, wantName: "gpu"},
		{
			name:     "ambiguous ID",
			args:     []string{"1001/1/1"},
			wantCode: 2,
			wantMsg:  "leasegrid: lease \"1001/1/1\" is ambiguous (found in: demo-a, demo-b)\nleasegrid: choose one with --provider NAME\n",
		},
		{
			name:     "missing ID",
			args:     []string{"missing-id"},
			wantErr:  app.ErrLeaseNotFound,
			wantCode: 1,
			wantMsg:  `lease "missing-id" not found (searched: demo-a, demo-b)`,
		},
		{
			name:     "ID only in a provider that wasn't searched",
			args:     []string{"--provider", "demo-a", "2001/1/1"},
			wantErr:  app.ErrLeaseNotFound,
			wantCode: 1,
			wantMsg:  `lease "2001/1/1" not found (searched: demo-a)`,
		},
		{
			name:     "unknown provider",
			args:     []string{"1001/1/1", "--provider", "akahs"},
			wantErr:  app.ErrUnknownProvider,
			wantCode: 2,
			wantMsg:  `leasegrid: unknown provider "akahs" (configured: demo-a, demo-b)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runDemo(t, append([]string{"show"}, tt.args...)...)

			if code := exitCode(err); code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (error: %v)\nstderr: %s", code, tt.wantCode, err, stderr)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("run() error = %v, want it to match %v", err, tt.wantErr)
			}
			if tt.wantName != "" {
				if want := "Name:       " + tt.wantName + "\n"; !strings.Contains(stdout, want) {
					t.Errorf("stdout does not contain %q:\n%s", want, stdout)
				}
				return
			}
			seen := stderr
			if err != nil {
				seen += err.Error()
			}
			if !strings.Contains(seen, tt.wantMsg) {
				t.Errorf("message %q not found in stderr or error:\n%s", tt.wantMsg, seen)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty on error", stdout)
			}
		})
	}
}

// TestShowInvalidArgs checks that a bad argument list is a usage error (exit
// 2) that names the problem and writes nothing to stdout. It is caught before
// the service is even built, so no provider is ever called.
func TestShowInvalidArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string // after "show"
		wantMsg string   // must appear in stderr
	}{
		{name: "no ID", args: nil, wantMsg: "show needs exactly one lease ID, got []"},
		{name: "two IDs", args: []string{"1001/1/1", "1002/1/1"}, wantMsg: `got ["1001/1/1" "1002/1/1"]`},
		{name: "extra argument after a flag", args: []string{"1001/1/1", "--provider", "demo-a", "extra"}, wantMsg: `got ["1001/1/1" "extra"]`},
		{name: "empty ID", args: []string{""}, wantMsg: `got [""]`},
		{name: "unknown flag", args: []string{"--format", "json", "1001/1/1"}, wantMsg: "flag provided but not defined: -format"},
		{name: "missing value", args: []string{"1001/1/1", "--provider"}, wantMsg: "flag needs an argument: -provider"},
		{name: "config flag after the command", args: []string{"--config", "x.yaml", "1001/1/1"}, wantMsg: "flag provided but not defined: -config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps()
			built := false
			d.newService = func(string, func() time.Time) (*app.Service, error) {
				built = true
				return app.New(), nil
			}

			err := run(t.Context(), append([]string{"show"}, tt.args...), d)
			if code := exitCode(err); code != 2 {
				t.Errorf("exit code = %d, want 2 (error: %v)", code, err)
			}
			if !strings.Contains(stderr.String(), tt.wantMsg) {
				t.Errorf("stderr does not contain %q:\n%s", tt.wantMsg, stderr)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if built {
				t.Error("the service was built, want the arguments rejected before any provider is called")
			}
		})
	}
}

// TestShowProviderError checks that a failing provider fails show, with the
// usual exit code and no output, even when another provider has the lease:
// the failing one might have it too.
func TestShowProviderError(t *testing.T) {
	ok := &fake.Provider{ProviderName: "ok", Leases: []lease.Lease{{ID: "1", Provider: "ok", Status: lease.StatusActive}}}
	down := &fake.Provider{ProviderName: "down", Err: errors.New("connection refused")}
	d, stdout, _ := newTestDeps(ok, down)

	err := run(t.Context(), []string{"show", "1"}, d)
	if !errors.Is(err, down.Err) {
		t.Fatalf("run() error = %v, want %v", err, down.Err)
	}
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on error", stdout.String())
	}
}

// TestShowReplacesControlCharacters checks, at the CLI boundary, that text
// from a provider can't send escape sequences to the user's terminal.
func TestShowReplacesControlCharacters(t *testing.T) {
	evil := &fake.Provider{ProviderName: "evil", Leases: []lease.Lease{
		{ID: "1", Provider: "evil", Name: "\x1b]0;pwned\a\x1b[2J", Status: lease.StatusActive},
	}}
	d, stdout, stderr := newTestDeps(evil)
	if err := run(t.Context(), []string{"show", "1"}, d); err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
	}
	if strings.ContainsAny(stdout.String(), "\x1b\a") {
		t.Errorf("stdout = %q, want no control characters", stdout.String())
	}
}

// TestShowCancellation proves the ctx passed to run reaches the provider
// that show is waiting on.
func TestShowCancellation(t *testing.T) {
	slow := fake.NewBlocking("slow")
	d, stdout, _ := newTestDeps(slow)
	ctx, cancel := context.WithCancel(t.Context())

	errc := make(chan error, 1)
	go func() { errc <- run(ctx, []string{"show", "1001/1/1"}, d) }()
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
}
