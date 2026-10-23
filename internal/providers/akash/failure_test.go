package akash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/providers"
)

// secretBody is what failing test responses contain. If any part of it
// appears in an error message, the error leaks response data.
const secretBody = `{"error": "SECRET-MARKER token=abc123"}`

// oneActiveLease is a single-page lease list with one active lease, so
// ListLeases goes on to request its deployment.
const oneActiveLease = `{
  "leases": [{"lease": {
    "id": {"owner": "akash1testowner0000000000000000000000000000", "dseq": "1001", "gseq": 1, "oseq": 1,
           "provider": "akash1testhost00000000000000000000000000000"},
    "state": "active",
    "price": {"denom": "uact", "amount": "5"}
  }}],
  "pagination": {"next_key": null}
}`

// stubAPI answers every request with the same status and body, and counts
// the requests it receives.
type stubAPI struct {
	*httptest.Server
	calls atomic.Int32
}

func newStubAPI(t *testing.T, status int, body string) *stubAPI {
	t.Helper()
	api := &stubAPI{}
	api.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(api.Close)
	return api
}

// readFixture returns the contents of testdata/name.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

// newHangingAPI starts a server whose handler never answers: it waits until
// the client gives up, which ends the request's context. started is closed
// when the first request arrives.
func newHangingAPI(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done(): // the client timed out or cancelled
		case <-time.After(10 * time.Second): // safety net: never hang the test run
		}
	}))
	t.Cleanup(srv.Close)
	return srv, started
}

// assertSafeError fails the test if err's message leaks the query string
// (where other APIs carry keys), an address, or anything from a response
// body. The server's host may appear: it's the configured endpoint, and
// naming it makes "connection refused" actionable.
func assertSafeError(t *testing.T, err error) {
	t.Helper()
	msg := err.Error()
	for _, leak := range []string{testOwner, testHost, "filters.owner", "pagination.", "SECRET-MARKER", "abc123"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error %q leaks %q", msg, leak)
		}
	}
}

func TestListLeasesHTTPStatus(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusBadRequest, want: providers.ErrBadResponse},
		{status: http.StatusUnauthorized, want: providers.ErrUnauthorized},
		{status: http.StatusForbidden, want: providers.ErrUnauthorized},
		{status: http.StatusNotFound, want: providers.ErrNotFound},
		{status: http.StatusTooManyRequests, want: providers.ErrRateLimited},
		{status: http.StatusInternalServerError, want: providers.ErrUnavailable},
		{status: http.StatusNotImplemented, want: providers.ErrNotFound}, // an outdated API version
		{status: http.StatusServiceUnavailable, want: providers.ErrUnavailable},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			api := newStubAPI(t, tt.status, secretBody)

			_, err := newTestProvider(t, api.Server).ListLeases(t.Context())
			if !errors.Is(err, tt.want) {
				t.Fatalf("ListLeases() error = %v, want %v", err, tt.want)
			}
			if want := "lease list returned HTTP " + strconv.Itoa(tt.status); !strings.Contains(err.Error(), want) {
				t.Errorf("ListLeases() error = %q, want it to contain %q", err, want)
			}
			assertSafeError(t, err)
			if n := api.calls.Load(); n != 1 {
				t.Errorf("requests = %d, want 1: failures must not be retried", n)
			}
		})
	}
}

// TestListLeasesDeploymentFailure checks a failure after the lease list
// succeeded: the error must name the request that failed.
func TestListLeasesDeploymentFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+leasesPath, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, oneActiveLease)
	})
	mux.HandleFunc("GET "+deploymentPath, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, secretBody, http.StatusInternalServerError)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	_, err := newTestProvider(t, srv).ListLeases(t.Context())
	if !errors.Is(err, providers.ErrUnavailable) {
		t.Fatalf("ListLeases() error = %v, want providers.ErrUnavailable", err)
	}
	if want := "deployment info returned HTTP 500"; !strings.Contains(err.Error(), want) {
		t.Errorf("ListLeases() error = %q, want it to contain %q", err, want)
	}
	assertSafeError(t, err)
}

func TestListLeasesBadBodies(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{name: "malformed JSON", body: readFixture(t, "lease_malformed.json"), wantMsg: "lease list: invalid JSON"},
		{name: "missing required field", body: readFixture(t, "lease_missing_field.json"), wantMsg: "missing id.dseq"},
		{name: "not an object", body: `"just a string"`, wantMsg: "lease list: invalid JSON"},
		{name: "unsupported lease state", body: strings.Replace(oneActiveLease, `"active"`, `"pending"`, 1), wantMsg: `unsupported state "pending"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newStubAPI(t, http.StatusOK, tt.body)

			_, err := newTestProvider(t, api.Server).ListLeases(t.Context())
			if !errors.Is(err, providers.ErrBadResponse) {
				t.Fatalf("ListLeases() error = %v, want providers.ErrBadResponse", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("ListLeases() error = %q, want it to contain %q", err, tt.wantMsg)
			}
			assertSafeError(t, err)
			if n := api.calls.Load(); n != 1 {
				t.Errorf("requests = %d, want 1: a bad lease list must stop before any deployment request", n)
			}
		})
	}
}

func TestListLeasesEmpty(t *testing.T) {
	api := newStubAPI(t, http.StatusOK, `{"leases": [], "pagination": {"next_key": null}}`)

	got, err := newTestProvider(t, api.Server).ListLeases(t.Context())
	if err != nil {
		t.Fatalf("ListLeases() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("ListLeases() = %#v, want an empty, non-nil slice", got)
	}
}

func TestListLeasesResponseTooLarge(t *testing.T) {
	api := newStubAPI(t, http.StatusOK, `{"leases": [`+strings.Repeat(" ", 64<<10)+`]}`)
	p := newTestProvider(t, api.Server)
	p.maxBytes = 1024

	_, err := p.ListLeases(t.Context())
	if !errors.Is(err, providers.ErrBadResponse) {
		t.Fatalf("ListLeases() error = %v, want providers.ErrBadResponse", err)
	}
	if want := "response larger than 1024 bytes"; !strings.Contains(err.Error(), want) {
		t.Errorf("ListLeases() error = %q, want it to contain %q", err, want)
	}
}

func TestListLeasesServerDown(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	p := newTestProvider(t, srv)
	srv.Close() // nothing listens at the address any more

	_, err := p.ListLeases(t.Context())
	if !errors.Is(err, providers.ErrUnavailable) {
		t.Fatalf("ListLeases() error = %v, want providers.ErrUnavailable", err)
	}
	assertSafeError(t, err)
}

// TestListLeasesTimeout is the plan's exercise: a server that never answers,
// a 75 ms timeout, and a prompt deadline error.
func TestListLeasesTimeout(t *testing.T) {
	srv, _ := newHangingAPI(t)
	p := newTestProvider(t, srv)
	p.timeout = 75 * time.Millisecond

	start := time.Now()
	_, err := p.ListLeases(t.Context())
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("ListLeases took %v with a 75ms timeout, want it to return promptly", elapsed)
	}
	if !strings.Contains(err.Error(), "lease list") {
		t.Errorf("ListLeases() error = %q, want it to name the request", err)
	}
}

// TestListLeasesCallerCancellation proves the caller's context reaches the
// HTTP request: cancelling it aborts a request that would otherwise hang.
func TestListLeasesCallerCancellation(t *testing.T) {
	srv, started := newHangingAPI(t)
	p := newTestProvider(t, srv)

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := p.ListLeases(ctx)
		errc <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the server")
	}
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ListLeases() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListLeases did not return after cancel: the context never reached the HTTP request")
	}
}

// countingTransport wraps an http.RoundTripper and counts the response
// bodies it hands out and how many get closed.
type countingTransport struct {
	base   http.RoundTripper
	opened atomic.Int32
	closed atomic.Int32
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	c.opened.Add(1)
	resp.Body = &countingBody{ReadCloser: resp.Body, closed: &c.closed}
	return resp, nil
}

// countingBody is a response body that counts its first Close.
type countingBody struct {
	io.ReadCloser
	closed *atomic.Int32
	once   sync.Once
}

func (b *countingBody) Close() error {
	b.once.Do(func() { b.closed.Add(1) })
	return b.ReadCloser.Close()
}

func TestListLeasesClosesResponseBodies(t *testing.T) {
	tests := []struct {
		name       string
		server     func(t *testing.T) *httptest.Server
		wantErr    bool
		wantBodies int32
	}{
		{
			name:       "success across pages and deployments",
			server:     func(t *testing.T) *httptest.Server { return newFixtureAPI(t).Server },
			wantBodies: 4, // two lease pages, two deployments
		},
		{
			name: "error status",
			server: func(t *testing.T) *httptest.Server {
				return newStubAPI(t, http.StatusInternalServerError, secretBody).Server
			},
			wantErr:    true,
			wantBodies: 1,
		},
		{
			name: "malformed JSON",
			server: func(t *testing.T) *httptest.Server {
				return newStubAPI(t, http.StatusOK, readFixture(t, "lease_malformed.json")).Server
			},
			wantErr:    true,
			wantBodies: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.server(t)
			counter := &countingTransport{base: srv.Client().Transport}
			p := newTestProvider(t, srv)
			p.client = &http.Client{Transport: counter}

			_, err := p.ListLeases(t.Context())
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListLeases() error = %v, want error: %v", err, tt.wantErr)
			}
			opened, closed := counter.opened.Load(), counter.closed.Load()
			if opened != tt.wantBodies || closed != opened {
				t.Errorf("bodies opened %d, closed %d; want %d opened, all closed", opened, closed, tt.wantBodies)
			}
		})
	}
}
