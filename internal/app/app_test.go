package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
	"github.com/namangourh/leasegrid/internal/providers/fake"
)

// inline is a provider defined right here in the test. It satisfies
// app.Provider just by having the right methods: Go needs no "implements".
type inline struct{ leases []lease.Lease }

func (i inline) Name() string { return "inline" }

func (i inline) ListLeases(context.Context) ([]lease.Lease, error) {
	return i.leases, nil
}

func TestServiceListLeases(t *testing.T) {
	web := lease.Lease{ID: "a1", Provider: "akash", Name: "web", Status: lease.StatusActive}
	db := lease.Lease{ID: "a2", Provider: "akash", Name: "db", Status: lease.StatusClosed}
	api := lease.Lease{ID: "f1", Provider: "flux", Name: "api", Status: lease.StatusActive}
	job := lease.Lease{ID: "i1", Provider: "inline", Name: "job", Status: lease.StatusActive}
	old := lease.Lease{ID: "i2", Provider: "inline", Name: "old", Status: lease.StatusClosed}
	errDown := errors.New("connection refused")

	akash := &fake.Provider{ProviderName: "akash", Leases: []lease.Lease{web, db}}
	flux := &fake.Provider{ProviderName: "flux", Leases: []lease.Lease{api}}
	fluxDown := &fake.Provider{ProviderName: "flux", Err: errDown}

	tests := []struct {
		name      string
		providers []app.Provider
		criteria  lease.Criteria
		want      []lease.Lease
		wantErr   error
	}{
		{
			name:      "injected fake provider",
			providers: []app.Provider{akash},
			want:      []lease.Lease{web, db},
		},
		{
			name:      "merges providers in order",
			providers: []app.Provider{akash, flux},
			want:      []lease.Lease{web, db, api},
		},
		{
			// fluxDown would fail the call if it were queried.
			name:      "queries only the selected provider",
			providers: []app.Provider{akash, fluxDown},
			criteria:  lease.Criteria{Provider: "akash"},
			want:      []lease.Lease{web, db},
		},
		{
			name:      "applies status criteria",
			providers: []app.Provider{inline{leases: []lease.Lease{job, old}}},
			criteria:  lease.Criteria{Status: lease.StatusActive},
			want:      []lease.Lease{job},
		},
		{
			name:    "no providers",
			wantErr: app.ErrNoProviders,
		},
		{
			name:      "unknown provider",
			providers: []app.Provider{akash},
			criteria:  lease.Criteria{Provider: "render"},
			wantErr:   app.ErrUnknownProvider,
		},
		{
			name:      "provider error is returned",
			providers: []app.Provider{akash, fluxDown},
			wantErr:   errDown,
		},
		{
			name: "invalid lease is a bad response",
			providers: []app.Provider{&fake.Provider{
				ProviderName: "akash",
				Leases:       []lease.Lease{{Provider: "akash", Status: lease.StatusActive}}, // no ID
			}},
			wantErr: providers.ErrBadResponse,
		},
		{
			name: "lease from the wrong provider is a bad response",
			providers: []app.Provider{&fake.Provider{
				ProviderName: "akash",
				Leases:       []lease.Lease{api}, // api says Provider "flux"
			}},
			wantErr: providers.ErrBadResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := app.New(tt.providers...)

			got, err := svc.ListLeases(t.Context(), tt.criteria)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListLeases() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ListLeases() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestServiceErrorMessages checks the messages a user will eventually see.
func TestServiceErrorMessages(t *testing.T) {
	tests := []struct {
		name     string
		provider app.Provider
		want     string
	}{
		{
			name:     "provider error names the provider",
			provider: &fake.Provider{ProviderName: "flux", Err: errors.New("connection refused")},
			want:     "provider flux: connection refused",
		},
		{
			name: "invalid lease explains the problem",
			provider: &fake.Provider{ProviderName: "akash", Leases: []lease.Lease{
				{ID: "x", Provider: "akash", Status: "pending"},
			}},
			want: `provider akash: bad response: validate lease "x": unsupported status "pending"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := app.New(tt.provider).ListLeases(t.Context(), lease.Criteria{})
			if err == nil || err.Error() != tt.want {
				t.Fatalf("ListLeases() error = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestServiceCancellationReachesProvider proves the service passes the
// caller's ctx through to providers instead of making its own.
func TestServiceCancellationReachesProvider(t *testing.T) {
	slow := fake.NewBlocking("slow")
	svc := app.New(slow)

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := svc.ListLeases(ctx, lease.Criteria{})
		errc <- err
	}()

	<-slow.Started()
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ListLeases() error = %v, want context.Canceled", err)
		}
		if want := "provider slow: context canceled"; err.Error() != want {
			t.Errorf("ListLeases() error = %q, want %q", err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListLeases did not return after cancel: ctx never reached the provider")
	}
}
