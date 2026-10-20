// Package fake provides deterministic, in-memory providers for tests.
// Nothing in this package touches the network.
package fake

import (
	"context"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
)

// Provider returns a fixed set of leases, or a fixed error.
type Provider struct {
	ProviderName string        // returned by Name
	Leases       []lease.Lease // returned by List, filtered by its criteria
	Err          error         // if non-nil, List returns it instead of leases
}

// Compile-time check: the build fails if *Provider stops satisfying the interface.
var _ providers.Provider = (*Provider)(nil)

// Name returns the configured provider name.
func (p *Provider) Name() string { return p.ProviderName }

// List returns the configured error, or the configured leases that match criteria.
func (p *Provider) List(ctx context.Context, criteria lease.Criteria) ([]lease.Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err // already cancelled: behave like a real provider would
	}
	if p.Err != nil {
		return nil, p.Err
	}
	return lease.Filter(p.Leases, criteria), nil
}
