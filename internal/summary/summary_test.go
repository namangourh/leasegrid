package summary_test

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/summary"
)

var now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// active, closed and stopped return a lease from provider "a" with ID n and
// the status their name says; stopped means insufficient_funds.
func active(n string) lease.Lease {
	return lease.Lease{ID: n, Provider: "a", Status: lease.StatusActive}
}

func closed(n string) lease.Lease {
	return lease.Lease{ID: n, Provider: "a", Status: lease.StatusClosed}
}

func stopped(n string) lease.Lease {
	return lease.Lease{ID: n, Provider: "a", Status: lease.StatusInsufficientFunds}
}

// expiring returns l with an expiry d after now.
func expiring(l lease.Lease, d time.Duration) lease.Lease {
	l.Expires = now.Add(d)
	return l
}

// costing returns l with a cost of perHour in denom.
func costing(l lease.Lease, perHour int64, denom string) lease.Lease {
	l.Cost = lease.Cost{PerHour: perHour, Denom: denom}
	return l
}

func TestCalculate(t *testing.T) {
	const ibc = "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"

	tests := []struct {
		name   string
		leases []lease.Lease
		window time.Duration
		want   summary.Summary
	}{
		{
			name: "nil input",
			want: summary.Summary{ByProvider: map[string]int{}, SpendPerHour: []lease.Cost{}},
		},
		{
			name:   "empty input",
			leases: []lease.Lease{},
			want:   summary.Summary{ByProvider: map[string]int{}, SpendPerHour: []lease.Cost{}},
		},
		{
			name: "status and provider counts",
			leases: []lease.Lease{
				active("1"), active("2"), closed("3"),
				{ID: "4", Provider: "b", Status: lease.StatusInsufficientFunds},
			},
			want: summary.Summary{
				Total: 4, Active: 2, InsufficientFunds: 1, Closed: 1,
				ByProvider:   map[string]int{"a": 3, "b": 1},
				SpendPerHour: []lease.Cost{},
			},
		},
		{
			name: "expiring soon",
			leases: []lease.Lease{
				expiring(active("in 1h"), time.Hour),                      // counts
				expiring(active("just inside"), 24*time.Hour-time.Second), // counts
				expiring(active("at the end"), 24*time.Hour),              // the window's end is excluded
				expiring(active("now"), 0),                                // expires now: already over
				expiring(active("expired"), -time.Hour),                   // already expired
				active("unknown expiry"),
				expiring(closed("closed"), time.Hour),
				expiring(stopped("stopped"), time.Hour),
			},
			window: 24 * time.Hour,
			want: summary.Summary{
				Total: 8, Active: 6, InsufficientFunds: 1, Closed: 1, ExpiringSoon: 2,
				ByProvider:   map[string]int{"a": 8},
				SpendPerHour: []lease.Cost{},
			},
		},
		{
			name: "default window is 7 days",
			leases: []lease.Lease{
				expiring(active("6d23h"), 7*24*time.Hour-time.Hour),
				expiring(active("7d"), 7*24*time.Hour),
			},
			window: summary.DefaultExpiringWithin,
			want: summary.Summary{
				Total: 2, Active: 2, ExpiringSoon: 1,
				ByProvider:   map[string]int{"a": 2},
				SpendPerHour: []lease.Cost{},
			},
		},
		{
			name: "spend rate of active leases, per denom",
			leases: []lease.Lease{
				costing(active("1"), 1000, "uakt"),
				costing(active("2"), 2500, "uakt"),
				costing(active("3"), 4990, ibc),
				costing(active("4"), 7, "uact"),
				costing(active("5"), 0, "uakt"), // a real zero
				active("6"),                     // no cost data
				costing(closed("7"), 9999, "uakt"),
				costing(stopped("8"), 500, "uakt"),
			},
			want: summary.Summary{
				Total: 8, Active: 6, InsufficientFunds: 1, Closed: 1,
				ByProvider: map[string]int{"a": 8},
				SpendPerHour: []lease.Cost{
					{PerHour: 4990, Denom: ibc},
					{PerHour: 7, Denom: "uact"},
					{PerHour: 3500, Denom: "uakt"},
				},
			},
		},
		{
			name:   "zero cost is shown",
			leases: []lease.Lease{costing(active("1"), 0, "uakt")},
			want: summary.Summary{
				Total: 1, Active: 1,
				ByProvider:   map[string]int{"a": 1},
				SpendPerHour: []lease.Cost{{PerHour: 0, Denom: "uakt"}},
			},
		},
		{
			name: "large amounts stay exact",
			leases: []lease.Lease{
				costing(active("1"), math.MaxInt64-1, "uakt"),
				costing(active("2"), 1, "uakt"),
				costing(active("3"), math.MaxInt64, "uact"), // other denoms never add up
			},
			want: summary.Summary{
				Total: 3, Active: 3,
				ByProvider: map[string]int{"a": 3},
				SpendPerHour: []lease.Cost{
					{PerHour: math.MaxInt64, Denom: "uact"},
					{PerHour: math.MaxInt64, Denom: "uakt"},
				},
			},
		},
		{
			name: "closed leases never overflow the total",
			leases: []lease.Lease{
				costing(active("1"), math.MaxInt64, "uakt"),
				costing(closed("2"), math.MaxInt64, "uakt"),
			},
			want: summary.Summary{
				Total: 2, Active: 1, Closed: 1,
				ByProvider:   map[string]int{"a": 2},
				SpendPerHour: []lease.Cost{{PerHour: math.MaxInt64, Denom: "uakt"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := summary.Calculate(tt.leases, now, tt.window)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Calculate() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestCalculateOverflow(t *testing.T) {
	leases := []lease.Lease{
		costing(active("1"), math.MaxInt64, "uakt"),
		costing(active("2"), 1, "uakt"),
	}
	_, err := summary.Calculate(leases, now, summary.DefaultExpiringWithin)
	if !errors.Is(err, summary.ErrCostOverflow) {
		t.Fatalf("Calculate() error = %v, want %v", err, summary.ErrCostOverflow)
	}
	if want := `cost total too large: "uakt" per hour`; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}
