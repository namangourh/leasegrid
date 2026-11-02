package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/providers"
	"github.com/namangourh/leasegrid/internal/providers/akash"
)

// akashTestdata holds the Akash provider's recorded API responses. They are
// sanitized: a test owner, no real wallet, endpoint or credentials.
var akashTestdata = filepath.Join("..", "..", "internal", "providers", "akash", "testdata")

// akashOwner is the owner address the recorded responses belong to.
const akashOwner = "akash1testowner0000000000000000000000000000"

// secretBody is what a failing test response contains. If any part of it
// reaches stdout, stderr or the returned error, the CLI leaks response data.
const secretBody = `{"error": "SECRET-MARKER token=abc123"}`

// newAkashDeps returns deps whose service has one Akash provider pointed at
// srv, plus the buffers that capture stdout and stderr. The provider trusts
// srv's certificate, so requests stay on this machine.
func newAkashDeps(srv *httptest.Server) (deps, *bytes.Buffer, *bytes.Buffer) {
	return newAkashDepsFor(srv, akashOwner)
}

// newAkashDepsFor is newAkashDeps with the provider configured for owner.
func newAkashDepsFor(srv *httptest.Server, owner string) (deps, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	d := deps{
		stdout: &stdout,
		stderr: &stderr,
		now:    testClock,
		newService: func(_ string, now func() time.Time) (*app.Service, error) {
			p, err := akash.New(akash.Config{BaseURL: srv.URL, Owner: owner, HTTPClient: srv.Client(), Now: now})
			if err != nil {
				return nil, err
			}
			return app.New(p), nil
		},
	}
	return d, &stdout, &stderr
}

// newAkashFixtureServer starts an HTTPS server that answers like the Akash
// API from the recorded responses: two pages of leases, and deployment info
// by dseq. A request for any other owner gets 400.
func newAkashFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	serve := func(w http.ResponseWriter, name string) {
		data, err := os.ReadFile(filepath.Join(akashTestdata, name))
		if err != nil {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /akash/market/v1beta5/leases/list", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filters.owner") != akashOwner {
			http.Error(w, "unexpected owner", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("pagination.key") == "" {
			serve(w, "leases_page1.json")
		} else {
			serve(w, "leases_page2.json")
		}
	})
	mux.HandleFunc("GET /akash/deployment/v1beta4/deployments/info", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id.owner") != akashOwner {
			http.Error(w, "unexpected owner", http.StatusBadRequest)
			return
		}
		serve(w, "deployment_"+r.URL.Query().Get("id.dseq")+".json")
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// assertHidesPrivateData fails the test if seen, everything the CLI printed
// or returned, contains the secret response body, the owner address, the
// query string or srv's address.
func assertHidesPrivateData(t *testing.T, seen string, srv *httptest.Server) {
	t.Helper()
	for _, private := range []string{"SECRET-MARKER", "abc123", akashOwner, "filters.owner", srv.Listener.Addr().String()} {
		if strings.Contains(seen, private) {
			t.Errorf("output contains %q", private)
		}
	}
}

// TestShowAkash runs show against recorded Akash responses: the whole path
// from HTTP through the Akash mapping and the lookup to the detail view.
func TestShowAkash(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{
			id: "1002/1/1",
			want: `ID:         1002/1/1
Provider:   akash
Name:       worker
Status:     active
Expires:    2026-10-20 13:00 UTC (in 1h0m)
Cost/hour:  6000 uakt
CPU:        2
Memory:     2 GiB
Storage:    12 GiB
GPUs:       2
`,
		},
		{
			// A closed lease has no deployment info, so no name or resources.
			id: "900/1/1",
			want: `ID:         900/1/1
Provider:   akash
Name:       -
Status:     closed
Expires:    -
Cost/hour:  1500 uakt
CPU:        -
Memory:     -
Storage:    -
GPUs:       -
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			d, stdout, stderr := newAkashDeps(newAkashFixtureServer(t))
			if err := run(t.Context(), []string{"show", tt.id}, d); err != nil {
				t.Fatalf("run() error = %v\nstderr: %s", err, stderr)
			}
			if got := stdout.String(); got != tt.want {
				t.Errorf("stdout =\n%s\nwant\n%s", got, tt.want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestShowAkashNotFound(t *testing.T) {
	d, stdout, _ := newAkashDeps(newAkashFixtureServer(t))
	err := run(t.Context(), []string{"show", "9999/1/1"}, d)
	if !errors.Is(err, app.ErrLeaseNotFound) {
		t.Fatalf("run() error = %v, want %v", err, app.ErrLeaseNotFound)
	}
	if want := `lease "9999/1/1" not found (searched: akash)`; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

// TestShowAkashFailureHidesSecrets checks that when the Akash API fails,
// nothing the CLI prints or returns contains the response body, the owner
// address, the query string or the endpoint.
func TestShowAkashFailureHidesSecrets(t *testing.T) {
	malformed, err := os.ReadFile(filepath.Join(akashTestdata, "lease_malformed.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "malformed response", status: http.StatusOK, body: string(malformed), wantErr: providers.ErrBadResponse},
		{name: "server error", status: http.StatusInternalServerError, body: secretBody, wantErr: providers.ErrUnavailable},
		{name: "unauthorized", status: http.StatusUnauthorized, body: secretBody, wantErr: providers.ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d, stdout, stderr := newAkashDeps(srv)
			err := run(t.Context(), []string{"show", "1001/1/1"}, d)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
			}
			if code := exitCode(err); code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty on error", stdout.String())
			}
			assertHidesPrivateData(t, stdout.String()+stderr.String()+err.Error(), srv)
		})
	}
}

// TestShowAkashCancellation proves that cancelling the context passed to run
// aborts an Akash HTTP request that would otherwise hang, and that the
// resulting error doesn't reveal the request URL.
func TestShowAkashCancellation(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done(): // the client cancelled
		case <-time.After(10 * time.Second): // safety net: never hang the test run
		}
	}))
	defer srv.Close()

	d, stdout, stderr := newAkashDeps(srv)
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() { errc <- run(ctx, []string{"show", "1001/1/1"}, d) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the server")
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
	assertHidesPrivateData(t, stderr.String()+err.Error(), srv)
}

// TestAkashFailureContract is the failure-contract matrix in
// docs/akash-adapter.md, run end to end through "leasegrid list": a local TLS
// server plays the Akash API. Each row checks the error category, the exit
// code, the exact message main prints after "leasegrid: ", an empty stdout,
// and that nothing private leaks.
func TestAkashFailureContract(t *testing.T) {
	malformed, err := os.ReadFile(filepath.Join(akashTestdata, "lease_malformed.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// oneLease is a lease list holding one lease with the given state and
	// price per block in uakt.
	oneLease := func(state, amount string) string {
		return `{"leases": [{"lease": {
			"id": {"owner": "` + akashOwner + `", "dseq": "1001", "gseq": 1, "oseq": 1,
			       "provider": "akash1testhost00000000000000000000000000000"},
			"state": "` + state + `", "price": {"denom": "uakt", "amount": "` + amount + `"}}}],
			"pagination": {"next_key": null}}`
	}

	tests := []struct {
		name     string
		owner    string // the configured owner; "" means akashOwner
		status   int    // the HTTP status of every response
		body     string // the body of every response
		cancel   string // "before": cancel before run starts; "during": the server hangs and the test cancels
		wantErr  error  // nil means success
		wantCode int
		wantMsg  string // the error's text; main prints it after "leasegrid: "
	}{
		{
			name: "1 unknown lease state", status: 200, body: oneLease("pending", "5"),
			wantErr: providers.ErrBadResponse, wantCode: 1,
			wantMsg: `provider akash: bad response: lease 1001/1/1: unsupported state "pending"`,
		},
		{
			name: "2 HTTP 429", status: 429, body: secretBody,
			wantErr: providers.ErrRateLimited, wantCode: 1,
			wantMsg: "provider akash: rate limited: lease list returned HTTP 429",
		},
		{
			name: "3 malformed JSON", status: 200, body: string(malformed),
			wantErr: providers.ErrBadResponse, wantCode: 1,
			wantMsg: "provider akash: bad response: lease list: invalid JSON: unexpected end of JSON input",
		},
		{
			name: "4 cancelled before the request", status: 200, body: oneLease("active", "5"), cancel: "before",
			wantErr: context.Canceled, wantCode: 130,
			wantMsg: "provider akash: lease list: context canceled",
		},
		{
			name: "5 cancelled during a slow request", cancel: "during",
			wantErr: context.Canceled, wantCode: 130,
			wantMsg: "provider akash: lease list: context canceled",
		},
		{
			name: "6 HTTP 500 with secrets in the body", status: 500, body: secretBody,
			wantErr: providers.ErrUnavailable, wantCode: 1,
			wantMsg: "provider akash: provider unavailable: lease list returned HTTP 500",
		},
		{
			// The server answers 400 unless it receives the owner intact.
			name: "7 owner that needs URL escaping", owner: "akash1x&y=z+w #v",
			status: 200, body: `{"leases": [], "pagination": {"next_key": null}}`,
			wantCode: 0,
		},
		{
			name: "8 per-hour cost beyond int64", status: 200, body: oneLease("active", "100000000000000000"),
			wantErr: providers.ErrBadResponse, wantCode: 1,
			wantMsg: "provider akash: bad response: lease 1001/1/1: price: amount out of range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := tt.owner
			if owner == "" {
				owner = akashOwner
			}
			started := make(chan struct{}, 1)
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.cancel == "during" {
					started <- struct{}{}
					select {
					case <-r.Context().Done(): // the client cancelled
					case <-time.After(10 * time.Second): // safety net: never hang the test run
					}
					return
				}
				if r.URL.Query().Get("filters.owner") != owner {
					http.Error(w, "unexpected owner", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d, stdout, stderr := newAkashDepsFor(srv, owner)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel == "before" {
				cancel()
			}
			errc := make(chan error, 1)
			go func() { errc <- run(ctx, []string{"list"}, d) }()
			if tt.cancel == "during" {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("the request never reached the server")
				}
				cancel()
			}
			err := waitErr(t, errc)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
			}
			if code := exitCode(err); code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			seen := stdout.String() + stderr.String()
			if err != nil {
				if err.Error() != tt.wantMsg {
					t.Errorf("error = %q, want %q", err, tt.wantMsg)
				}
				seen += err.Error()
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			assertHidesPrivateData(t, seen, srv)
		})
	}
}
