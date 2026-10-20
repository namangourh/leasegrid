// Package app holds LeaseGrid's use cases. It reaches providers only through
// the providers.Provider interface, so callers and tests can inject any
// implementation, including fakes.
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
)

var (
	// ErrNoProviders is returned when the service has no providers at all.
	ErrNoProviders = errors.New("no providers configured")

	// ErrUnknownProvider is returned when criteria name a provider the
	// service does not have.
	ErrUnknownProvider = errors.New("unknown provider")
)

// Service runs LeaseGrid use cases against a fixed set of providers.
type Service struct {
	providers []providers.Provider
}

// New returns a Service that queries ps. The slice is copied, so later
// changes to the caller's slice don't affect the service.
func New(ps ...providers.Provider) *Service {
	return &Service{providers: slices.Clone(ps)}
}

// ListLeases returns the leases matching criteria. If criteria.Provider is
// set, only that provider is queried. Results are filtered again here, so
// they are correct even if a provider ignored part of the criteria.
//
// Providers are queried one after another and the first error stops the
// call. Concurrent queries and partial results come with the
// second real provider.
func (s *Service) ListLeases(ctx context.Context, criteria lease.Criteria) ([]lease.Lease, error) {
	if len(s.providers) == 0 {
		return nil, ErrNoProviders
	}

	selected := s.selectProviders(criteria.Provider)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w %q", ErrUnknownProvider, criteria.Provider)
	}

	var all []lease.Lease
	for _, p := range selected {
		leases, err := p.List(ctx, criteria)
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", p.Name(), err)
		}
		all = append(all, leases...)
	}
	return lease.Filter(all, criteria), nil
}

// selectProviders returns every provider when name is empty, otherwise only
// the provider with that name (or none).
func (s *Service) selectProviders(name string) []providers.Provider {
	if name == "" {
		return s.providers
	}
	for _, p := range s.providers {
		if p.Name() == name {
			return []providers.Provider{p}
		}
	}
	return nil
}
