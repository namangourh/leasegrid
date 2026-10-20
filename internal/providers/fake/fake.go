// Package fake provides deterministic, in-memory providers for tests.
// Nothing in this package touches the network.
//
// The fakes satisfy app.Provider implicitly; this package doesn't import app.
// The compile-time checks live in fake_test.go.
package fake

import (
	"context"
	"slices"
	"sync"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Provider returns a fixed set of leases, or a fixed error.
type Provider struct {
	ProviderName string        // returned by Name
	Leases       []lease.Lease // returned by ListLeases
	Err          error         // if non-nil, ListLeases returns it instead of leases
}

// Name returns the configured provider name.
func (p *Provider) Name() string { return p.ProviderName }

// ListLeases returns the configured error, or a copy of the configured leases.
func (p *Provider) ListLeases(ctx context.Context) ([]lease.Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err // already cancelled: behave like a real provider would
	}
	if p.Err != nil {
		return nil, p.Err
	}
	return slices.Clone(p.Leases), nil
}

// Blocking is a provider whose ListLeases never answers on its own: it waits
// until ctx is done, like a network call to a provider that has hung. Create
// it with NewBlocking.
type Blocking struct {
	ProviderName string

	started     chan struct{}
	startedOnce sync.Once
}

// NewBlocking returns a Blocking provider called name.
func NewBlocking(name string) *Blocking {
	return &Blocking{ProviderName: name, started: make(chan struct{})}
}

// Name returns the configured provider name.
func (b *Blocking) Name() string { return b.ProviderName }

// Started returns a channel that is closed once ListLeases has been called,
// so a test can wait until the provider is blocked before cancelling.
func (b *Blocking) Started() <-chan struct{} { return b.started }

// ListLeases blocks until ctx is done, then returns ctx's error.
func (b *Blocking) ListLeases(ctx context.Context) ([]lease.Lease, error) {
	b.startedOnce.Do(func() { close(b.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}
