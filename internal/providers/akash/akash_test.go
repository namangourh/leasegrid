package akash

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
)

// Compile-time check: *Provider satisfies the interface its consumer
// declares. It lives in a test, so the akash package never imports app.
var _ app.Provider = (*Provider)(nil)

// fixtureAPI is a fake Akash REST API that serves files from testdata.
type fixtureAPI struct {
	*httptest.Server
	leaseCalls      atomic.Int32
	deploymentCalls atomic.Int32
}

// newFixtureAPI starts an HTTPS test server that answers like
// api.akashnet.net: the lease list (paged by pagination.key) and deployment
// info by dseq. Requests for any other owner are rejected.
func newFixtureAPI(t *testing.T) *fixtureAPI {
	t.Helper()
	api := &fixtureAPI{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET "+leasesPath, func(w http.ResponseWriter, r *http.Request) {
		api.leaseCalls.Add(1)
		q := r.URL.Query()
		if q.Get("filters.owner") != testOwner {
			http.Error(w, "unexpected owner", http.StatusBadRequest)
			return
		}
		switch q.Get("pagination.key") {
		case "":
			serveFixture(t, w, "leases_page1.json")
		case "cGFnZTI=":
			serveFixture(t, w, "leases_page2.json")
		default:
			http.Error(w, "unknown page key", http.StatusBadRequest)
		}
	})

	mux.HandleFunc("GET "+deploymentPath, func(w http.ResponseWriter, r *http.Request) {
		api.deploymentCalls.Add(1)
		q := r.URL.Query()
		dseq := q.Get("id.dseq")
		if q.Get("id.owner") != testOwner {
			http.Error(w, "unexpected owner", http.StatusBadRequest)
			return
		}
		if _, err := strconv.ParseUint(dseq, 10, 64); err != nil {
			http.Error(w, "bad dseq", http.StatusBadRequest)
			return
		}
		serveFixture(t, w, "deployment_"+dseq+".json")
	})

	api.Server = httptest.NewTLSServer(mux)
	t.Cleanup(api.Close)
	return api
}

// serveFixture writes testdata/name as a JSON response, or 404 if there's no
// such file. It runs in the server's goroutine, so it reports problems with
// t.Errorf: t.Fatal may only be called from the test's own goroutine.
func serveFixture(t *testing.T, w http.ResponseWriter, name string) {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, nil)
		return
	}
	if err != nil {
		t.Errorf("read fixture %s: %v", name, err)
		http.Error(w, "fixture error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// newTestProvider returns a Provider pointed at srv, with the fixed test clock.
func newTestProvider(t *testing.T, srv *httptest.Server) *Provider {
	t.Helper()
	p, err := New(Config{
		BaseURL:    srv.URL,
		Owner:      testOwner,
		HTTPClient: srv.Client(), // trusts the test server's certificate
		Now:        func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return p
}

// TestListLeasesFromFixtures is the end-to-end happy path: two pages of
// leases, two deployments, every field mapped.
func TestListLeasesFromFixtures(t *testing.T) {
	api := newFixtureAPI(t)
	p := newTestProvider(t, api.Server)

	got, err := p.ListLeases(t.Context())
	if err != nil {
		t.Fatalf("ListLeases() error = %v", err)
	}

	want := []lease.Lease{
		{
			ID: "1001/1/1", Provider: "akash", Name: "web", Status: lease.StatusActive,
			Expires:   testNow.Add(600 * time.Hour),
			Cost:      lease.Cost{PerHour: 3000, Denom: "uact"},
			Resources: lease.Resources{CPUMillis: 500, MemoryBytes: 536870912, StorageBytes: 1073741824},
		},
		{
			ID: "1002/1/1", Provider: "akash", Name: "worker", Status: lease.StatusActive,
			Expires:   testNow.Add(time.Hour),
			Cost:      lease.Cost{PerHour: 6000, Denom: "uakt"},
			Resources: lease.Resources{CPUMillis: 2000, MemoryBytes: 2147483648, StorageBytes: 12884901888, GPUs: 2},
		},
		{
			ID: "900/1/1", Provider: "akash", Status: lease.StatusClosed,
			Cost: lease.Cost{PerHour: 1500, Denom: "uakt"},
		},
	}
	if len(got) != len(want) {
		t.Fatalf("ListLeases() returned %d leases, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !sameLease(got[i], want[i]) {
			t.Errorf("lease %d =\n  %+v\nwant\n  %+v", i, got[i], want[i])
		}
		if err := got[i].Validate(); err != nil {
			t.Errorf("lease %d is invalid: %v", i, err)
		}
	}

	if n := api.leaseCalls.Load(); n != 2 {
		t.Errorf("lease list requests = %d, want 2 (two pages)", n)
	}
	if n := api.deploymentCalls.Load(); n != 2 {
		t.Errorf("deployment requests = %d, want 2 (none for the closed lease)", n)
	}
}

// TestListLeasesStopsAtPageLimit checks that paging is bounded.
func TestListLeasesStopsAtPageLimit(t *testing.T) {
	api := newFixtureAPI(t)
	p := newTestProvider(t, api.Server)
	p.maxPages = 1 // the fixtures have two pages

	_, err := p.ListLeases(t.Context())
	if !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("ListLeases() error = %v, want ErrTooManyPages", err)
	}
	if n := api.leaseCalls.Load(); n != 1 {
		t.Errorf("lease list requests = %d, want exactly 1", n)
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "valid", cfg: Config{BaseURL: "https://api.akashnet.net", Owner: testOwner}},
		{name: "missing owner", cfg: Config{BaseURL: "https://api.akashnet.net"}, wantErr: true},
		{name: "not an akash address", cfg: Config{BaseURL: "https://api.akashnet.net", Owner: "cosmos1abc"}, wantErr: true},
		{name: "plain http", cfg: Config{BaseURL: "http://api.akashnet.net", Owner: testOwner}, wantErr: true},
		{name: "no host", cfg: Config{BaseURL: "https://", Owner: testOwner}, wantErr: true},
		{name: "not a URL", cfg: Config{BaseURL: "::", Owner: testOwner}, wantErr: true},
		{name: "negative timeout", cfg: Config{BaseURL: "https://api.akashnet.net", Owner: testOwner, Timeout: -time.Second}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, want error: %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if p.Name() != "akash" || p.timeout != DefaultTimeout || p.blockTime != DefaultBlockTime {
				t.Errorf("New() = name %q, timeout %v, block time %v; want defaults", p.Name(), p.timeout, p.blockTime)
			}
		})
	}
}
