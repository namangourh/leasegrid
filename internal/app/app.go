// Package app holds LeaseGrid's use cases. It declares the Provider interface
// it needs and reaches providers only through it, so callers and tests can
// inject any implementation, including fakes.
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
)

// Provider is what the service needs from a source of leases.
//
// It is declared here, by its consumer, not next to its implementations: Go
// interfaces are satisfied implicitly, so implementations such as the Akash
// adapter and the test fakes never need to import this package.
type Provider interface {
	// Name returns the provider's identifier, e.g. "akash".
	Name() string

	// ListLeases returns every lease the provider knows for its configured
	// owner. Implementations must:
	//   - return leases that pass lease.Validate, with Provider equal to Name();
	//   - return promptly once ctx is done, with an error matching ctx.Err();
	//   - wrap other failures with a category from internal/providers;
	//   - return a nil slice whenever they return an error.
	ListLeases(ctx context.Context) ([]lease.Lease, error)
}

var (
	// ErrNoProviders is returned when the service has no providers at all.
	ErrNoProviders = errors.New("no providers configured")

	// ErrUnknownProvider is returned when criteria name a provider the
	// service does not have.
	ErrUnknownProvider = errors.New("unknown provider")
)

// Service runs LeaseGrid use cases against a fixed set of providers.
type Service struct {
	providers []Provider
}

// New returns a Service that queries ps. The slice is copied, so later
// changes to the caller's slice don't affect the service.
func New(ps ...Provider) *Service {
	return &Service{providers: slices.Clone(ps)}
}

// DefaultSort is the order ListLeases uses when ListOptions.SortBy is empty:
// soonest expiry first.
const DefaultSort = lease.SortByExpires

// ListOptions selects and orders the leases ListLeases returns.
type ListOptions struct {
	Criteria lease.Criteria // which leases to return
	SortBy   lease.SortKey  // how to order them; "" means DefaultSort
}

// ListLeases returns the leases matching opts.Criteria, sorted by
// opts.SortBy. If opts.Criteria.Provider is set, only that provider is
// queried. Providers return all their leases; filtering and sorting happen
// here, so every provider gets the same semantics. Invalid options are
// rejected before any provider is called.
//
// Every lease a provider returns is checked against the Provider contract,
// so a buggy adapter is reported as providers.ErrBadResponse instead of
// producing misleading output.
//
// Providers are queried one after another and the first error stops the
// call. Concurrent queries and partial results come with the second real
// provider.
func (s *Service) ListLeases(ctx context.Context, opts ListOptions) ([]lease.Lease, error) {
	sortBy := cmp.Or(opts.SortBy, DefaultSort)
	if _, err := lease.ParseSortKey(string(sortBy)); err != nil {
		return nil, err
	}
	if len(s.providers) == 0 {
		return nil, ErrNoProviders
	}

	name := opts.Criteria.Provider
	selected := s.selectProviders(name)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w %q (configured: %s)", ErrUnknownProvider, name, strings.Join(s.names(), ", "))
	}

	var all []lease.Lease
	for _, p := range selected {
		leases, err := p.ListLeases(ctx)
		if err == nil {
			err = checkLeases(p.Name(), leases)
		}
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", p.Name(), err)
		}
		all = append(all, leases...)
	}
	return lease.Sort(lease.Filter(all, opts.Criteria), sortBy)
}

// checkLeases enforces the Provider contract on what a provider returned.
func checkLeases(name string, leases []lease.Lease) error {
	for _, l := range leases {
		if err := l.Validate(); err != nil {
			return fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
		}
		if l.Provider != name {
			return fmt.Errorf("%w: lease %q reports provider %q", providers.ErrBadResponse, l.ID, l.Provider)
		}
	}
	return nil
}

// selectProviders returns every provider when name is empty, otherwise only
// the provider with that name (or none).
func (s *Service) selectProviders(name string) []Provider {
	if name == "" {
		return s.providers
	}
	for _, p := range s.providers {
		if p.Name() == name {
			return []Provider{p}
		}
	}
	return nil
}

// names returns the providers' names, in the order they were given to New.
func (s *Service) names() []string {
	names := make([]string, 0, len(s.providers))
	for _, p := range s.providers {
		names = append(names, p.Name())
	}
	return names
}
