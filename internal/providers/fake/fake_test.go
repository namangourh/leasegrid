package fake_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers/fake"
)

// Compile-time checks: the build fails if a fake stops satisfying the
// interface its consumer declares. They live in a test so the fake package
// itself never imports app.
var (
	_ app.Provider = (*fake.Provider)(nil)
	_ app.Provider = (*fake.Blocking)(nil)
)

func TestProviderListLeases(t *testing.T) {
	web := lease.Lease{ID: "1", Provider: "fake", Name: "web", Status: lease.StatusActive}
	db := lease.Lease{ID: "2", Provider: "fake", Name: "db", Status: lease.StatusClosed}
	errDown := errors.New("provider down")

	tests := []struct {
		name     string
		provider *fake.Provider
		want     []lease.Lease
		wantErr  error
	}{
		{
			name:     "returns all leases",
			provider: &fake.Provider{ProviderName: "fake", Leases: []lease.Lease{web, db}},
			want:     []lease.Lease{web, db},
		},
		{
			name:     "no leases",
			provider: &fake.Provider{ProviderName: "fake"},
			want:     nil,
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
			var p app.Provider = tt.provider

			got, err := p.ListLeases(t.Context())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListLeases() error = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ListLeases() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderListLeasesCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before calling ListLeases

	p := &fake.Provider{ProviderName: "fake"}
	if _, err := p.ListLeases(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListLeases() error = %v, want context.Canceled", err)
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
		_, err := p.ListLeases(ctx)
		errc <- err
	}()

	<-p.Started() // ListLeases is now blocked inside the provider
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ListLeases() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListLeases did not return after cancel")
	}
}
