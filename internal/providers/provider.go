// Package providers defines the interface every lease provider implements.
package providers

import (
	"context"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Provider lists leases from one DePIN network.
//
// Implementations must stop work and return when ctx is cancelled,
// because real providers make network calls that can hang.
type Provider interface {
	// Name returns the provider's identifier, e.g. "akash".
	// It matches the Provider field of the leases it returns.
	Name() string

	// List returns the leases that match criteria. Implementations may
	// ignore criteria fields they cannot apply; callers filter the result
	// again with lease.Filter.
	List(ctx context.Context, criteria lease.Criteria) ([]lease.Lease, error)
}
