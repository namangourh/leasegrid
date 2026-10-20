package fake_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
	"github.com/namangourh/leasegrid/internal/providers/fake"
)

func TestProviderList(t *testing.T) {
	web := lease.Lease{ID: "1", Provider: "fake", Name: "web", Status: lease.StatusActive}
	db := lease.Lease{ID: "2", Provider: "fake", Name: "db", Status: lease.StatusClosed}
	errDown := errors.New("provider down")

	tests := []struct {
		name     string
		provider *fake.Provider
		criteria lease.Criteria
		want     []lease.Lease
		wantErr  error
	}{
		{
			name:     "returns all leases",
			provider: &fake.Provider{ProviderName: "fake", Leases: []lease.Lease{web, db}},
			want:     []lease.Lease{web, db},
		},
		{
			name:     "applies criteria",
			provider: &fake.Provider{ProviderName: "fake", Leases: []lease.Lease{web, db}},
			criteria: lease.Criteria{Status: lease.StatusActive},
			want:     []lease.Lease{web},
		},
		{
			name:     "returns configured error",
			provider: &fake.Provider{ProviderName: "fake", Err: errDown},
			wantErr:  errDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the fake only through the interface, as real callers will.
			var p providers.Provider = tt.provider

			got, err := p.List(t.Context(), tt.criteria)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("List() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("List() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderListCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before calling List

	p := &fake.Provider{ProviderName: "fake"}
	if _, err := p.List(ctx, lease.Criteria{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List() error = %v, want context.Canceled", err)
	}
}

func TestProviderName(t *testing.T) {
	p := &fake.Provider{ProviderName: "fake"}
	if got := p.Name(); got != "fake" {
		t.Errorf("Name() = %q, want %q", got, "fake")
	}
}

func TestBlockingReturnsOnCancel(t *testing.T) {
	p := fake.NewBlocking("slow")
	if got := p.Name(); got != "slow" {
		t.Errorf("Name() = %q, want %q", got, "slow")
	}

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := p.List(ctx, lease.Criteria{})
		errc <- err
	}()

	<-p.Started() // List is now blocked inside the provider
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("List() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("List did not return after cancel")
	}
}
