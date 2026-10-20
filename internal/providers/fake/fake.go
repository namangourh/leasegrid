// Package fake provides deterministic, in-memory providers for tests.
// Nothing in this package touches the network.
package fake

import (
	"context"
	"sync"

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

// Blocking is a provider whose List never answers on its own: it waits until
// ctx is done, like a network call to a provider that has hung. Create it
// with NewBlocking.
type Blocking struct {
	ProviderName string

	started     chan struct{}
	startedOnce sync.Once
}

// Compile-time check: the build fails if *Blocking stops satisfying the interface.
var _ providers.Provider = (*Blocking)(nil)

// NewBlocking returns a Blocking provider called name.
func NewBlocking(name string) *Blocking {
	return &Blocking{ProviderName: name, started: make(chan struct{})}
}

// Name returns the configured provider name.
func (b *Blocking) Name() string { return b.ProviderName }

// Started returns a channel that is closed once List has been called, so a
// test can wait until the provider is blocked before cancelling.
func (b *Blocking) Started() <-chan struct{} { return b.started }

// List blocks until ctx is done, then returns ctx's error.
func (b *Blocking) List(ctx context.Context, _ lease.Criteria) ([]lease.Lease, error) {
	b.startedOnce.Do(func() { close(b.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}
