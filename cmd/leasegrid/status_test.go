package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
	"github.com/namangourh/leasegrid/internal/providers/fake"
	"github.com/namangourh/leasegrid/internal/summary"
)

// TestStatusDemoTable is the golden test for the README's status demo: with
// the clock fixed, it prints exactly this dashboard on every run. The output
// isn't a terminal, so it has no color.
func TestStatusDemoTable(t *testing.T) {
	want := `Leases:         5 (3 active, 1 insufficient_funds, 1 closed)
Expiring soon:  2 within 7d0h
Active spend:   4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1 per hour
                37800 uakt per hour
By provider:    demo-a 3, demo-b 2

ID        PROVIDER  NAME    STATUS              EXPIRES                                  COST/HOUR
1001/1/1  demo-b    worker  insufficient_funds  2026-10-20 10:00 UTC (expired 2h0m ago)  900 uakt
1001/1/1  demo-a    web     active              2026-10-20 15:00 UTC (in 3h0m)           4990 ibc/170C...F2F1
2001/1/1  demo-b    gpu     active              2026-10-20 15:00 UTC (in 3h0m)           36000 uakt
1002/1/1  demo-a    api     active              2026-10-30 12:00 UTC (in 10d0h)          1800 uakt
1003/1/1  demo-a    batch   closed              -                                        1200 uakt
`
	stdout, stderr, err := runDemo(t, "status")
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

// TestStatusDemoSummary checks the summary lines for each flag, through real
// config loading and the demo providers. The web and gpu leases expire in
// exactly 3 hours.
func TestStatusDemoSummary(t *testing.T) {
	tests := []struct {
		name string
		args []string // after "status"
		want string   // the lines above the blank line
	}{
		{
			name: "one provider",
			args: []string{"--provider", "demo-a"},
			want: `Leases:         3 (2 active, 0 insufficient_funds, 1 closed)
Expiring soon:  1 within 7d0h
Active spend:   4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1 per hour
                1800 uakt per hour
By provider:    demo-a 3`,
		},
		{
			name: "provider and status",
			args: []string{"--provider", "demo-b", "--status", "active"},
			want: `Leases:         1 (1 active, 0 insufficient_funds, 0 closed)
Expiring soon:  1 within 7d0h
Active spend:   36000 uakt per hour
By provider:    demo-b 1`,
		},
		{
			name: "no active leases, no spend",
			args: []string{"--status", "closed"},
			want: `Leases:         1 (0 active, 0 insufficient_funds, 1 closed)
Expiring soon:  0 within 7d0h
Active spend:   -
By provider:    demo-a 1`,
		},
		{
			// The window doesn't filter: all five leases are still counted.
			name: "window ending at the expiries excludes them",
			args: []string{"--expiring-within", "3h"},
			want: `Leases:         5 (3 active, 1 insufficient_funds, 1 closed)
Expiring soon:  0 within 3h0m
Active spend:   4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1 per hour
                37800 uakt per hour
By provider:    demo-a 3, demo-b 2`,
		},
		{
			name: "window just past the expiries",
			args: []string{"--expiring-within", "3h1m"},
			want: `Leases:         5 (3 active, 1 insufficient_funds, 1 closed)
Expiring soon:  2 within 3h1m
Active spend:   4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1 per hour
                37800 uakt per hour
By provider:    demo-a 3, demo-b 2`,
		},
		{
			name: "no-color is accepted",
			args: []string{"--no-color", "--format", "table"},
			want: `Leases:         5 (3 active, 1 insufficient_funds, 1 closed)
Expiring soon:  2 within 7d0h
Active spend:   4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1 per hour
                37800 uakt per hour
By provider:    demo-a 3, demo-b 2`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runDemo(t, append([]string{"status"}, tt.args...)...)
			if err != nil {
				t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
			}
			got, _, _ := strings.Cut(stdout, "\n\n")
			if got != tt.want {
				t.Errorf("summary =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// statusJSON is what scripts read from status --format json.
type statusJSON struct {
	AsOf              time.Time      `json:"as_of"`
	ExpiringWithin    string         `json:"expiring_within"`
	Total             int            `json:"total"`
	Active            int            `json:"active"`
	InsufficientFunds int            `json:"insufficient_funds"`
	Closed            int            `json:"closed"`
	ExpiringSoon      int            `json:"expiring_soon"`
	ByProvider        map[string]int `json:"by_provider"`
	SpendPerHour      []lease.Cost   `json:"spend_per_hour"`
	Leases            []lease.Lease  `json:"leases"`
}

// decodeStatus decodes stdout as status JSON, failing the test if it isn't.
func decodeStatus(t *testing.T, stdout string) statusJSON {
	t.Helper()
	var got statusJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
	}
	return got
}

// names returns the leases' names, in order.
func names(leases []lease.Lease) []string {
	var ns []string
	for _, l := range leases {
		ns = append(ns, l.Name)
	}
	return ns
}

func TestStatusDemoJSON(t *testing.T) {
	stdout, stderr, err := runDemo(t, "status", "--format", "json", "--expiring-within", "24h")
	if err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
	}
	got := decodeStatus(t, stdout)

	if want := []string{"worker", "web", "gpu", "api", "batch"}; !slices.Equal(names(got.Leases), want) {
		t.Errorf("leases = %v, want %v", names(got.Leases), want)
	}
	got.Leases = nil
	want := statusJSON{
		AsOf: testNow, ExpiringWithin: "24h0m0s",
		Total: 5, Active: 3, InsufficientFunds: 1, Closed: 1, ExpiringSoon: 2,
		ByProvider: map[string]int{"demo-a": 3, "demo-b": 2},
		SpendPerHour: []lease.Cost{
			{PerHour: 4990, Denom: "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"},
			{PerHour: 37800, Denom: "uakt"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status =\n  %+v\nwant\n  %+v", got, want)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Errorf("stdout = %q, want no escape codes in JSON", stdout)
	}
}

func TestStatusEmpty(t *testing.T) {
	// demo-b has no closed leases. The dashboard still shows its zero counts.
	args := []string{"status", "--provider", "demo-b", "--status", "closed"}

	t.Run("table", func(t *testing.T) {
		want := `Leases:         0 (0 active, 0 insufficient_funds, 0 closed)
Expiring soon:  0 within 7d0h
Active spend:   -
By provider:    -

No leases found.
`
		stdout, stderr, err := runDemo(t, args...)
		if err != nil {
			t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
		}
		if stdout != want {
			t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})

	t.Run("json", func(t *testing.T) {
		stdout, stderr, err := runDemo(t, append(args, "--format", "json")...)
		if err != nil {
			t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
		}
		for _, want := range []string{`"total": 0,`, `"by_provider": {},`, `"spend_per_hour": [],`, `"leases": []`} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout = %s\nwant it to contain %s", stdout, want)
			}
		}
	})
}

// TestStatusInvalidArgs checks that bad input is a usage error (exit 2) that
// names the problem, writes nothing to stdout and calls no provider.
func TestStatusInvalidArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string // after "status"
		wantMsg string   // must appear in stderr
	}{
		{name: "yaml format", args: []string{"--format", "yaml"}, wantMsg: `invalid value "yaml" for flag -format: unknown output format "yaml" (supported: table, json)`},
		{name: "unknown format", args: []string{"--format", "csv"}, wantMsg: `unknown output format "csv"`},
		{name: "expiring is not a status", args: []string{"--status", "expiring"}, wantMsg: `invalid value "expiring" for flag -status: unsupported status "expiring"`},
		{name: "days are not a duration unit", args: []string{"--expiring-within", "7d"}, wantMsg: "not a duration such as 90m or 24h"},
		{name: "zero duration", args: []string{"--expiring-within", "0"}, wantMsg: "must be greater than zero"},
		{name: "negative duration", args: []string{"--expiring-within", "-1h"}, wantMsg: "must be greater than zero"},
		{name: "missing value", args: []string{"--expiring-within"}, wantMsg: "flag needs an argument: -expiring-within"},
		{name: "no sort flag", args: []string{"--sort-by", "id"}, wantMsg: "flag provided but not defined: -sort-by"},
		{name: "an argument", args: []string{"demo-a"}, wantMsg: `status takes no arguments, got ["demo-a"]`},
		{name: "an argument after a flag", args: []string{"--provider", "demo-a", "active"}, wantMsg: `status takes no arguments, got ["active"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps()
			built := false
			d.newService = func(string, func() time.Time) (*app.Service, error) {
				built = true
				return app.New(), nil
			}

			err := run(t.Context(), append([]string{"status"}, tt.args...), d)
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

// TestStatusErrors checks that a failure prints no dashboard at all: a
// summary without one provider's leases would show wrong totals.
func TestStatusErrors(t *testing.T) {
	ok := &fake.Provider{ProviderName: "ok", Leases: []lease.Lease{
		{ID: "1", Provider: "ok", Status: lease.StatusActive, Cost: lease.Cost{PerHour: 10, Denom: "uakt"}},
	}}
	down := &fake.Provider{ProviderName: "flux", Err: errors.New("connection refused")}
	huge := &fake.Provider{ProviderName: "huge", Leases: []lease.Lease{
		{ID: "1", Provider: "huge", Status: lease.StatusActive, Cost: lease.Cost{PerHour: math.MaxInt64, Denom: "uakt"}},
		{ID: "2", Provider: "huge", Status: lease.StatusActive, Cost: lease.Cost{PerHour: 1, Denom: "uakt"}},
	}}

	tests := []struct {
		name      string
		providers []app.Provider
		args      []string // after "status"
		wantErr   error
		wantCode  int
		wantMsg   string // must appear in stderr or in the returned error
	}{
		{name: "one provider of two fails", providers: []app.Provider{ok, down}, wantErr: down.Err, wantCode: 1, wantMsg: "provider flux: connection refused"},
		{name: "no providers", wantErr: app.ErrNoProviders, wantCode: 1, wantMsg: "no providers configured"},
		{name: "unknown provider", providers: []app.Provider{ok}, args: []string{"--provider", "render"}, wantErr: app.ErrUnknownProvider, wantCode: 2, wantMsg: `unknown provider "render"`},
		{name: "total too large", providers: []app.Provider{huge}, wantErr: summary.ErrCostOverflow, wantCode: 1, wantMsg: `cost total too large: "uakt" per hour`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps(tt.providers...)
			err := run(t.Context(), append([]string{"status"}, tt.args...), d)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
			}
			if code := exitCode(err); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if seen := stderr.String() + err.Error(); !strings.Contains(seen, tt.wantMsg) {
				t.Errorf("message %q not found in stderr or error:\n%s", tt.wantMsg, seen)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty on error", stdout.String())
			}
		})
	}
}

func TestStatusConfigError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	var stdout, stderr bytes.Buffer
	d := deps{stdout: &stdout, stderr: &stderr, now: testClock, newService: loadService}

	err := run(t.Context(), []string{"--config", missing, "status"}, d)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("run() error = %v, want fs.ErrNotExist", err)
	}
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on error", stdout.String())
	}
}

// TestStatusReplacesControlCharacters checks, at the CLI boundary, that text
// from a provider can't send escape sequences to the user's terminal.
func TestStatusReplacesControlCharacters(t *testing.T) {
	evil := &fake.Provider{ProviderName: "evil", Leases: []lease.Lease{
		{ID: "1", Provider: "evil", Name: "\x1b]0;pwned\a", Status: lease.StatusActive,
			Cost: lease.Cost{PerHour: 1, Denom: "uakt\x1b[2J"}},
	}}
	d, stdout, stderr := newTestDeps(evil)
	if err := run(t.Context(), []string{"status"}, d); err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
	}
	if strings.ContainsAny(stdout.String(), "\x1b\a") {
		t.Errorf("stdout = %q, want no control characters", stdout.String())
	}
}

// TestStatusCancellation proves the ctx passed to run reaches the providers.
func TestStatusCancellation(t *testing.T) {
	slow := fake.NewBlocking("slow")
	d, stdout, _ := newTestDeps(slow)
	ctx, cancel := context.WithCancel(t.Context())

	errc := make(chan error, 1)
	go func() { errc <- run(ctx, []string{"status"}, d) }()
	select {
	case <-slow.Started():
	case err := <-errc:
		// Without this case, a run that never calls the provider would leave
		// the test waiting until go test's 10-minute timeout.
		t.Fatalf("run() = %v before calling the provider", err)
	}
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

// TestStatusAkash runs status against recorded Akash responses. The two
// active leases are priced in different denoms, so there are two totals.
func TestStatusAkash(t *testing.T) {
	want := `Leases:         3 (2 active, 0 insufficient_funds, 1 closed)
Expiring soon:  1 within 7d0h
Active spend:   3000 uact per hour
                6000 uakt per hour
By provider:    akash 3

ID        PROVIDER  NAME    STATUS  EXPIRES                          COST/HOUR
1002/1/1  akash     worker  active  2026-10-20 13:00 UTC (in 1h0m)   6000 uakt
1001/1/1  akash     web     active  2026-11-14 12:00 UTC (in 25d0h)  3000 uact
900/1/1   akash     -       closed  -                                1500 uakt
`
	d, stdout, stderr := newAkashDeps(newAkashFixtureServer(t))
	if err := run(t.Context(), []string{"status"}, d); err != nil {
		t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
	}
	if got := stdout.String(); got != want {
		t.Errorf("stdout =\n%s\nwant\n%s", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// TestStatusAkashFailureHidesSecrets checks that when the Akash API fails,
// status prints no dashboard and reveals none of the request's details.
func TestStatusAkashFailureHidesSecrets(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer srv.Close()

	d, stdout, stderr := newAkashDeps(srv)
	err := run(t.Context(), []string{"status", "--format", "json"}, d)
	if !errors.Is(err, providers.ErrUnavailable) {
		t.Fatalf("run() error = %v, want %v", err, providers.ErrUnavailable)
	}
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on error", stdout.String())
	}
	assertHidesPrivateData(t, stderr.String()+err.Error(), srv)
}

// TestBinaryStatus runs the real binary. Its stdout is a pipe, not a
// terminal, so the table must have no color even though --no-color isn't
// given.
func TestBinaryStatus(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{name: "demo status", args: []string{"--config", demoConfig, "status"}, wantCode: 0, wantOut: "Active spend:   4990 ibc/"},
		{name: "demo status json", args: []string{"--config", demoConfig, "status", "--format", "json"}, wantCode: 0, wantOut: `"total": 5,`},
		{name: "demo status empty", args: []string{"--config", demoConfig, "status", "--provider", "demo-b", "--status", "closed"}, wantCode: 0, wantOut: "No leases found."},
		{name: "yaml format", args: []string{"--config", demoConfig, "status", "--format", "yaml"}, wantCode: 2, wantOut: "(supported: table, json)"},
		{name: "unknown provider", args: []string{"--config", demoConfig, "status", "--provider", "akahs"}, wantCode: 2, wantOut: `leasegrid: unknown provider "akahs" (configured: demo-a, demo-b)`},
		{name: "without providers", args: []string{"status"}, wantCode: 1, wantOut: "leasegrid: no providers configured"},
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
			if strings.Contains(out, "\x1b") {
				t.Errorf("output = %q, want no escape codes through a pipe", out)
			}
		})
	}
}
