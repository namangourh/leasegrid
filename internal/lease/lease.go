// Package lease defines LeaseGrid's provider-neutral lease model and pure
// helpers over it. It imports no other LeaseGrid package, so every other
// package can depend on it without creating an import cycle.
package lease

// Lease is one compute lease, normalized from any provider's own format.
type Lease struct {
	ID       string `json:"id"`
	Provider string `json:"provider"` // provider name, e.g. "akash"
	Name     string `json:"name"`
	Status   Status `json:"status"`
}

// Status is a lease's lifecycle state.
type Status string

// Lease states. More are added as providers need them.
const (
	StatusActive Status = "active"
	StatusClosed Status = "closed"
)

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
