package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
)

var (
	// ErrInvalidLeaseID is returned by FindLease for an ID that can't match
	// any lease, such as an empty one.
	ErrInvalidLeaseID = errors.New("invalid lease ID")

	// ErrLeaseNotFound is returned by FindLease when no searched provider has
	// a lease with the ID. Its text completes a message such as
	// `lease "1001/1/1" not found (searched: akash)`.
	ErrLeaseNotFound = errors.New("not found")
)

// AmbiguousLeaseError is returned by FindLease when leases from more than one
// provider have the ID. Lease IDs are unique only within one provider.
type AmbiguousLeaseError struct {
	ID        string
	Providers []string // the providers that have the ID, sorted by name
}

func (e *AmbiguousLeaseError) Error() string {
	return fmt.Sprintf("lease %q is ambiguous (found in: %s)", e.ID, strings.Join(e.Providers, ", "))
}

// FindLease returns the lease with exactly this ID. If provider is set, only
// that provider is searched; otherwise all of them are. It gets leases from
// ListLeases, so provider selection, contract checks and error wrapping work
// as they do for listing, and ctx reaches every provider call.
//
// If no searched provider has the ID, the error matches ErrLeaseNotFound. If
// several providers have it, the error is an *AmbiguousLeaseError: FindLease
// never picks one of them.
func (s *Service) FindLease(ctx context.Context, id, provider string) (lease.Lease, error) {
	if id == "" {
		return lease.Lease{}, fmt.Errorf("%w: it must not be empty", ErrInvalidLeaseID)
	}
	all, err := s.ListLeases(ctx, ListOptions{Criteria: lease.Criteria{Provider: provider}, SortBy: lease.SortByProvider})
	if err != nil {
		return lease.Lease{}, err
	}

	// Leases are sorted by provider, so a provider that returned the ID twice
	// shows up as two matches in a row.
	var matches []lease.Lease
	for _, l := range all {
		if l.ID != id {
			continue
		}
		if len(matches) > 0 && matches[len(matches)-1].Provider == l.Provider {
			return lease.Lease{}, fmt.Errorf("provider %s: %w: lease %q appears more than once", l.Provider, providers.ErrBadResponse, id)
		}
		matches = append(matches, l)
	}

	switch len(matches) {
	case 0:
		searched := s.names()
		if provider != "" {
			searched = []string{provider}
		}
		return lease.Lease{}, fmt.Errorf("lease %q %w (searched: %s)", id, ErrLeaseNotFound, strings.Join(searched, ", "))
	case 1:
		return matches[0], nil
	default:
		ambiguous := &AmbiguousLeaseError{ID: id}
		for _, m := range matches {
			ambiguous.Providers = append(ambiguous.Providers, m.Provider)
		}
		return lease.Lease{}, ambiguous
	}
}
