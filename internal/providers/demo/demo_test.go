package demo_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers/demo"
)

// Compile-time check that a demo provider satisfies the interface its
// consumer declares.
var _ app.Provider = (*demo.Provider)(nil)

var testNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return testNow }

func TestNew(t *testing.T) {
	for _, name := range []string{demo.NameA, demo.NameB} {
		p, ok := demo.New(name, fixedClock)
		if !ok || p.Name() != name {
			t.Errorf("New(%q) = %v, %v; want a provider named %q", name, p, ok, name)
		}
	}

	if p, ok := demo.New("akash", fixedClock); ok || p != nil {
		t.Errorf("New(\"akash\") = %v, %v; want nil, false", p, ok)
	}
}

func TestListLeases(t *testing.T) {
	// row is the part of a lease the demo's behaviour depends on.
	type row struct {
		id      string
		name    string
		status  lease.Status
		expires time.Time // zero means unknown
	}

	tests := []struct {
		provider string
		want     []row
	}{
		{provider: demo.NameA, want: []row{
			{"1001/1/1", "web", lease.StatusActive, testNow.Add(3 * time.Hour)},
			{"1002/1/1", "api", lease.StatusActive, testNow.Add(240 * time.Hour)},
			{"1003/1/1", "batch", lease.StatusClosed, time.Time{}},
		}},
		{provider: demo.NameB, want: []row{
			{"1001/1/1", "worker", lease.StatusInsufficientFunds, testNow.Add(-2 * time.Hour)},
			{"2001/1/1", "gpu", lease.StatusActive, testNow.Add(3 * time.Hour)},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			p, _ := demo.New(tt.provider, fixedClock)
			leases, err := p.ListLeases(t.Context())
			if err != nil {
				t.Fatalf("ListLeases() error = %v", err)
			}

			got := make([]row, 0, len(leases))
			for _, l := range leases {
				// The same contract checks the app applies to every provider.
				if err := l.Validate(); err != nil {
					t.Errorf("lease %q is invalid: %v", l.ID, err)
				}
				if l.Provider != tt.provider {
					t.Errorf("lease %q has Provider %q, want %q", l.ID, l.Provider, tt.provider)
				}
				got = append(got, row{l.ID, l.Name, l.Status, l.Expires})
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ListLeases() =\n%v\nwant\n%v", got, tt.want)
			}
		})
	}
}

func TestListLeasesReturnsFreshSlices(t *testing.T) {
	p, _ := demo.New(demo.NameA, fixedClock)
	first, err := p.ListLeases(t.Context())
	if err != nil {
		t.Fatalf("ListLeases() error = %v", err)
	}
	first[0].Name = "changed by the caller"

	second, err := p.ListLeases(t.Context())
	if err != nil {
		t.Fatalf("ListLeases() error = %v", err)
	}
	if second[0].Name != "web" {
		t.Errorf("second call returned Name %q; a caller's change leaked into the samples", second[0].Name)
	}
}

func TestListLeasesCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	p, _ := demo.New(demo.NameA, fixedClock)
	leases, err := p.ListLeases(ctx)
	if !errors.Is(err, context.Canceled) || leases != nil {
		t.Fatalf("ListLeases() = %v, %v; want nil, context.Canceled", leases, err)
	}
}

func TestNewDefaultsToTimeNow(t *testing.T) {
	p, _ := demo.New(demo.NameA, nil)

	before := time.Now()
	leases, err := p.ListLeases(t.Context())
	after := time.Now()
	if err != nil {
		t.Fatalf("ListLeases() error = %v", err)
	}

	// The first sample expires 3h after the provider's clock reading.
	got := leases[0].Expires
	if got.Before(before.Add(3*time.Hour)) || got.After(after.Add(3*time.Hour)) {
		t.Errorf("Expires = %v, want 3h after a reading between %v and %v", got, before, after)
	}
}
