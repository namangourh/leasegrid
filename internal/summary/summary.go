// Package summary calculates the figures on the status dashboard from a list
// of leases. Its functions are pure: the caller passes the current time, so
// results never depend on the wall clock.
package summary

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
)

// DefaultExpiringWithin is the window for "expiring soon" when the caller
// doesn't choose one: 7 days.
const DefaultExpiringWithin = 7 * 24 * time.Hour

// ErrCostOverflow is returned when a spend-rate total is too large for an
// int64. Printing a wrong total would be worse than printing none.
var ErrCostOverflow = errors.New("cost total too large")

// Summary is what the status dashboard shows about a list of leases.
type Summary struct {
	Total             int
	Active            int
	InsufficientFunds int
	Closed            int

	// ExpiringSoon counts active leases with a known expiry after now and
	// before now plus the window. Already-expired leases don't count.
	ExpiringSoon int

	// ByProvider counts leases per provider name.
	ByProvider map[string]int

	// SpendPerHour is what the active leases cost per hour: one total per
	// denom, sorted by denom. Different denoms are never added together.
	SpendPerHour []lease.Cost
}

// ExpiringSoon reports whether l counts as expiring soon: it is active, and
// its known expiry is after now and before now plus window. The dashboard's
// count and its colors both use it, so they always agree.
func ExpiringSoon(l lease.Lease, now time.Time, window time.Duration) bool {
	return l.Status == lease.StatusActive && l.ExpiresWithin(now, window) && l.Expires.After(now)
}

// Calculate summarizes leases as of now, counting leases that expire within
// window as expiring soon. It expects leases that pass lease.Validate, so
// costs are never negative.
//
// The result never holds a nil map or slice, so an empty input still has an
// empty ByProvider and SpendPerHour.
func Calculate(leases []lease.Lease, now time.Time, window time.Duration) (Summary, error) {
	s := Summary{ByProvider: map[string]int{}, SpendPerHour: []lease.Cost{}}
	spend := map[string]int64{}
	for _, l := range leases {
		s.Total++
		s.ByProvider[l.Provider]++
		switch l.Status {
		case lease.StatusActive:
			s.Active++
		case lease.StatusInsufficientFunds:
			s.InsufficientFunds++
		case lease.StatusClosed:
			s.Closed++
		}
		if l.Status != lease.StatusActive {
			continue
		}

		if ExpiringSoon(l, now, window) {
			s.ExpiringSoon++
		}
		if c := l.Cost; c.Denom != "" { // no denom means no cost data
			if c.PerHour > math.MaxInt64-spend[c.Denom] {
				return Summary{}, fmt.Errorf("%w: %q per hour", ErrCostOverflow, c.Denom)
			}
			spend[c.Denom] += c.PerHour
		}
	}

	for _, denom := range slices.Sorted(maps.Keys(spend)) {
		s.SpendPerHour = append(s.SpendPerHour, lease.Cost{PerHour: spend[denom], Denom: denom})
	}
	return s, nil
}
