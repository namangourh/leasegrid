package lease

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// SortKey names the field leases are sorted by.
type SortKey string

// Sort keys. Ties are always broken by ID and then provider name, so the
// order is the same on every run.
const (
	SortByID       SortKey = "id"
	SortByProvider SortKey = "provider"
	SortByStatus   SortKey = "status"
	SortByExpires  SortKey = "expires"
)

// ErrUnknownSortKey is returned for a sort key LeaseGrid doesn't support.
var ErrUnknownSortKey = errors.New("unknown sort key")

// ParseSortKey converts a --sort-by flag value into a SortKey.
func ParseSortKey(s string) (SortKey, error) {
	k := SortKey(s)
	if _, err := comparer(k); err != nil {
		return "", fmt.Errorf("%w (supported: id, provider, status, expires)", err)
	}
	return k, nil
}

// Sort returns a sorted copy of leases and leaves the input unchanged. Ties
// are broken by ID and then provider name, because IDs are unique only
// within one provider. Leases equal on all of these keep their input order.
// On success the result is never nil.
func Sort(leases []Lease, key SortKey) ([]Lease, error) {
	compare, err := comparer(key)
	if err != nil {
		return nil, err
	}

	out := make([]Lease, len(leases))
	copy(out, leases)
	slices.SortStableFunc(out, func(a, b Lease) int {
		return cmp.Or(
			compare(a, b),
			strings.Compare(a.ID, b.ID),
			strings.Compare(a.Provider, b.Provider),
		)
	})
	return out, nil
}

// comparer returns the function that orders two leases by key.
func comparer(key SortKey) (func(a, b Lease) int, error) {
	switch key {
	case SortByID:
		return func(a, b Lease) int { return strings.Compare(a.ID, b.ID) }, nil
	case SortByProvider:
		return func(a, b Lease) int { return strings.Compare(a.Provider, b.Provider) }, nil
	case SortByStatus:
		return func(a, b Lease) int { return cmp.Compare(statusRank(a.Status), statusRank(b.Status)) }, nil
	case SortByExpires:
		return func(a, b Lease) int { return compareExpiry(a.Expires, b.Expires) }, nil
	default:
		return nil, fmt.Errorf("%w %q", ErrUnknownSortKey, key)
	}
}

// statusRank orders statuses by lifecycle: running leases first, then
// leases that ran out of funds, then closed ones.
func statusRank(s Status) int {
	switch s {
	case StatusActive:
		return 0
	case StatusInsufficientFunds:
		return 1
	case StatusClosed:
		return 2
	default:
		return 3
	}
}

// compareExpiry orders known expiries earliest first and unknown (zero)
// expiries last.
func compareExpiry(a, b time.Time) int {
	switch {
	case a.IsZero() && b.IsZero():
		return 0
	case a.IsZero():
		return 1
	case b.IsZero():
		return -1
	default:
		return a.Compare(b)
	}
}
