package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
	"github.com/namangourh/leasegrid/internal/providers/fake"
)

// unfiltered ignores criteria and returns everything, as a real provider may
// when its API can't filter. Defining it here shows that any type with the
// right methods can be injected, even one that exists only in a test.
type unfiltered struct{ leases []lease.Lease }

func (u unfiltered) Name() string { return "unfiltered" }

func (u unfiltered) List(context.Context, lease.Criteria) ([]lease.Lease, error) {
	return u.leases, nil
}

func TestServiceListLeases(t *testing.T) {
	web := lease.Lease{ID: "a1", Provider: "akash", Name: "web", Status: lease.StatusActive}
	db := lease.Lease{ID: "a2", Provider: "akash", Name: "db", Status: lease.StatusClosed}
	api := lease.Lease{ID: "f1", Provider: "flux", Name: "api", Status: lease.StatusActive}
	u1 := lease.Lease{ID: "u1", Provider: "unfiltered", Name: "job", Status: lease.StatusActive}
	u2 := lease.Lease{ID: "u2", Provider: "unfiltered", Name: "old", Status: lease.StatusClosed}
	errDown := errors.New("connection refused")

	akash := &fake.Provider{ProviderName: "akash", Leases: []lease.Lease{web, db}}
	flux := &fake.Provider{ProviderName: "flux", Leases: []lease.Lease{api}}
	fluxDown := &fake.Provider{ProviderName: "flux", Err: errDown}

	tests := []struct {
		name      string
		providers []providers.Provider
		criteria  lease.Criteria
		want      []lease.Lease
		wantErr   error
	}{
		{
			name:      "injected fake provider",
			providers: []providers.Provider{akash},
			want:      []lease.Lease{web, db},
		},
		{
			name:      "merges providers in order",
			providers: []providers.Provider{akash, flux},
			want:      []lease.Lease{web, db, api},
		},
		{
			// fluxDown would fail the call if it were queried.
			name:      "queries only the selected provider",
			providers: []providers.Provider{akash, fluxDown},
			criteria:  lease.Criteria{Provider: "akash"},
			want:      []lease.Lease{web, db},
		},
		{
			name:      "filters results a provider did not filter",
			providers: []providers.Provider{unfiltered{leases: []lease.Lease{u1, u2}}},
			criteria:  lease.Criteria{Status: lease.StatusActive},
			want:      []lease.Lease{u1},
		},
		{
			name:    "no providers",
			wantErr: app.ErrNoProviders,
		},
		{
			name:      "unknown provider",
			providers: []providers.Provider{akash},
			criteria:  lease.Criteria{Provider: "render"},
			wantErr:   app.ErrUnknownProvider,
		},
		{
			name:      "provider error is returned",
			providers: []providers.Provider{akash, fluxDown},
			wantErr:   errDown,
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

// TestServiceErrorNamesProvider checks the message a user will eventually see.
func TestServiceErrorNamesProvider(t *testing.T) {
	svc := app.New(&fake.Provider{ProviderName: "flux", Err: errors.New("connection refused")})

	_, err := svc.ListLeases(t.Context(), lease.Criteria{})
	if want := "provider flux: connection refused"; err == nil || err.Error() != want {
		t.Fatalf("ListLeases() error = %v, want %q", err, want)
	}
}
