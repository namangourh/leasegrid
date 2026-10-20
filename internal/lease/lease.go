// Package lease defines LeaseGrid's provider-neutral lease model and pure
// helpers over it. It imports no other LeaseGrid package, so every other
// package can depend on it without creating an import cycle.
package lease

import (
	"errors"
	"fmt"
	"time"
)

// Lease is one compute lease, normalized from any provider's own format.
// Provider-specific details (raw IDs, per-block prices, chain heights) stay
// inside each provider's adapter.
type Lease struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"` // provider name, e.g. "akash"
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	Expires   time.Time `json:"expires,omitzero"` // estimated; zero means unknown
	Cost      Cost      `json:"cost,omitzero"`
	Resources Resources `json:"resources,omitzero"`
}

// Status is a lease's lifecycle state.
type Status string

// Lease states. Whether a lease is expiring is derived from Expires (see
// ExpiresWithin), not stored as a status.
const (
	StatusActive            Status = "active"
	StatusClosed            Status = "closed"
	StatusInsufficientFunds Status = "insufficient_funds"
)

// Valid reports whether s is one of the known lease states.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusClosed, StatusInsufficientFunds:
		return true
	default:
		return false
	}
}

// Cost is what a lease costs per hour, as an integer amount of the
// denomination's smallest unit (1 AKT = 1,000,000 uakt). Integers keep sums
// exact; convert to whole units only for display.
type Cost struct {
	PerHour int64  `json:"per_hour"`
	Denom   string `json:"denom"` // e.g. "uakt"
}

// Resources is the compute capacity a lease provides.
type Resources struct {
	CPUMillis    int64 `json:"cpu_millis"` // 1000 = one CPU
	MemoryBytes  int64 `json:"memory_bytes"`
	StorageBytes int64 `json:"storage_bytes"`
	GPUs         int64 `json:"gpus"`
}

// Validation errors. Validate wraps exactly one of them, so callers can check
// the cause with errors.Is.
var (
	ErrMissingID       = errors.New("missing lease ID")
	ErrMissingProvider = errors.New("missing provider name")
	ErrUnknownStatus   = errors.New("unsupported status")
	ErrNegativeCost    = errors.New("negative cost")
)

// Validate reports the first rule l breaks, wrapped with the lease ID, or nil.
func (l Lease) Validate() error {
	var err error
	switch {
	case l.ID == "":
		err = ErrMissingID
	case l.Provider == "":
		err = ErrMissingProvider
	case !l.Status.Valid():
		err = fmt.Errorf("%w %q", ErrUnknownStatus, l.Status)
	case l.Cost.PerHour < 0:
		err = ErrNegativeCost
	default:
		return nil
	}
	return fmt.Errorf("validate lease %q: %w", l.ID, err)
}

// ExpiresWithin reports whether l has a known expiry before now+d. Leases with
// an unknown (zero) expiry never count as expiring.
func (l Lease) ExpiresWithin(now time.Time, d time.Duration) bool {
	return !l.Expires.IsZero() && l.Expires.Before(now.Add(d))
}

// Criteria selects leases. A zero-value field matches any lease.
type Criteria struct {
	Provider string // match this provider name; "" matches all
	Status   Status // match this status; "" matches all
}

// Filter returns the leases that match c, in their original order.
// It never returns nil, so callers can always range over or encode the result.
func Filter(leases []Lease, c Criteria) []Lease {
	out := make([]Lease, 0, len(leases))
	for _, l := range leases {
		if c.Provider != "" && l.Provider != c.Provider {
			continue
		}
		if c.Status != "" && l.Status != c.Status {
			continue
		}
		out = append(out, l)
	}
	return out
}
