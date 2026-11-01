// Package demo provides built-in providers that serve fixed sample leases,
// so LeaseGrid can be tried without a network connection or an account.
// Enable them in the config file under the names NameA and NameB.
package demo

import (
	"context"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Names of the demo providers, as used in the config file.
const (
	NameA = "demo-a"
	NameB = "demo-b"
)

// ibcDenom is a denom in the IBC format real Akash prices can use, so the
// demo shows how long denoms are displayed.
const ibcDenom = "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"

// sample is a lease template. Its expiry is an offset from the current time,
// so the demo always shows leases that are about to expire.
type sample struct {
	lease     lease.Lease   // everything except Provider and Expires
	expiresIn time.Duration // 0 means the expiry is unknown
}

// samples holds each demo provider's leases. Between them they cover every
// status, an unknown expiry, an expired lease, two leases with the same
// expiry, and the same lease ID under both providers.
var samples = map[string][]sample{
	NameA: {
		{expiresIn: 3 * time.Hour, lease: lease.Lease{
			ID: "1001/1/1", Name: "web", Status: lease.StatusActive,
			Cost:      lease.Cost{PerHour: 4990, Denom: ibcDenom},
			Resources: lease.Resources{CPUMillis: 500, MemoryBytes: 512 << 20, StorageBytes: 1 << 30},
		}},
		{expiresIn: 10 * 24 * time.Hour, lease: lease.Lease{
			ID: "1002/1/1", Name: "api", Status: lease.StatusActive,
			Cost:      lease.Cost{PerHour: 1800, Denom: "uakt"},
			Resources: lease.Resources{CPUMillis: 1000, MemoryBytes: 1 << 30, StorageBytes: 5 << 30},
		}},
		{lease: lease.Lease{
			ID: "1003/1/1", Name: "batch", Status: lease.StatusClosed,
			Cost: lease.Cost{PerHour: 1200, Denom: "uakt"},
		}},
	},
	NameB: {
		{expiresIn: -2 * time.Hour, lease: lease.Lease{
			ID: "1001/1/1", Name: "worker", Status: lease.StatusInsufficientFunds,
			Cost:      lease.Cost{PerHour: 900, Denom: "uakt"},
			Resources: lease.Resources{CPUMillis: 250, MemoryBytes: 256 << 20, StorageBytes: 1 << 30},
		}},
		{expiresIn: 3 * time.Hour, lease: lease.Lease{
			ID: "2001/1/1", Name: "gpu", Status: lease.StatusActive,
			Cost:      lease.Cost{PerHour: 36000, Denom: "uakt"},
			Resources: lease.Resources{CPUMillis: 4000, MemoryBytes: 16 << 30, StorageBytes: 100 << 30, GPUs: 1},
		}},
	},
}

// Provider serves one demo provider's sample leases.
type Provider struct {
	name    string
	samples []sample
	now     func() time.Time
}

// New returns the demo provider called name, using now as its clock (nil
// means time.Now). It reports false if name is not a demo provider.
func New(name string, now func() time.Time) (*Provider, bool) {
	s, ok := samples[name]
	if !ok {
		return nil, false
	}
	if now == nil {
		now = time.Now
	}
	return &Provider{name: name, samples: s, now: now}, true
}

// Name returns the provider's name, e.g. "demo-a".
func (p *Provider) Name() string {
	return p.name
}

// ListLeases returns the sample leases, with expiries computed from the
// current time. If ctx is already done it returns ctx's error instead.
func (p *Provider) ListLeases(ctx context.Context) ([]lease.Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := p.now()
	leases := make([]lease.Lease, 0, len(p.samples))
	for _, s := range p.samples {
		l := s.lease // a copy: Lease has no pointer, slice or map fields
		l.Provider = p.name
		if s.expiresIn != 0 {
			l.Expires = now.Add(s.expiresIn)
		}
		leases = append(leases, l)
	}
	return leases, nil
}
