// Package akash lists leases from the Akash Network through its public REST
// API. Lease data on Akash is public, so the provider needs only the owner's
// wallet address, never a key or other credential.
//
// The Provider satisfies app.Provider implicitly; this package doesn't import
// app.
package akash

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Defaults for Config fields left at zero.
const (
	DefaultTimeout   = 30 * time.Second
	DefaultBlockTime = 6 * time.Second
)

// API paths, as served by api.akashnet.net in October 2026. The two modules
// are on different versions; if either starts returning 404 or 501, Akash has
// moved to a new version.
const (
	leasesPath     = "/akash/market/v1beta5/leases/list"
	deploymentPath = "/akash/deployment/v1beta4/deployments/info"
)

// Paging and size limits, so a huge or misbehaving API can't make a call run
// forever or use unbounded memory.
const (
	pageLimit       = 100     // leases per page
	maxPages        = 20      // so at most 2,000 leases
	maxResponseSize = 8 << 20 // 8 MiB per response
)

// ErrTooManyPages is returned when the owner has more leases than LeaseGrid
// pages through.
var ErrTooManyPages = errors.New("too many pages of leases")

// Config configures a Provider. Only BaseURL and Owner are required.
type Config struct {
	// BaseURL is the REST endpoint, e.g. "https://api.akashnet.net".
	// It must use https.
	BaseURL string

	// Owner is the wallet address whose leases are listed, e.g. "akash1...".
	Owner string

	// HTTPClient sends the requests. nil means a new http.Client; timeouts
	// come from Timeout, not from the client.
	HTTPClient *http.Client

	// Timeout bounds a whole ListLeases call, every request included.
	// Zero means DefaultTimeout.
	Timeout time.Duration

	// BlockTime is the average time between Akash blocks, used to turn
	// per-block prices and escrow funds into hours. Zero means
	// DefaultBlockTime.
	BlockTime time.Duration

	// Now returns the current time, the start of expiry estimates.
	// nil means time.Now.
	Now func() time.Time
}

// Provider lists one owner's Akash leases. Create it with New.
type Provider struct {
	baseURL   *url.URL
	owner     string
	client    *http.Client
	timeout   time.Duration
	blockTime time.Duration
	now       func() time.Time

	// Limits, as fields so tests in this package can lower them.
	maxPages int
	maxBytes int64
}

// New validates cfg and returns a Provider. It makes no network calls.
func New(cfg Config) (*Provider, error) {
	if !strings.HasPrefix(cfg.Owner, "akash1") {
		return nil, errors.New("akash: owner must be an akash1... wallet address")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("akash: base URL must be an https URL with a host")
	}
	if cfg.Timeout < 0 || cfg.BlockTime < 0 {
		return nil, errors.New("akash: timeout and block time must not be negative")
	}

	p := &Provider{
		baseURL:   u,
		owner:     cfg.Owner,
		client:    cfg.HTTPClient,
		timeout:   cfg.Timeout,
		blockTime: cfg.BlockTime,
		now:       cfg.Now,
		maxPages:  maxPages,
		maxBytes:  maxResponseSize,
	}
	if p.client == nil {
		p.client = &http.Client{}
	}
	if p.timeout == 0 {
		p.timeout = DefaultTimeout
	}
	if p.blockTime == 0 {
		p.blockTime = DefaultBlockTime
	}
	if p.now == nil {
		p.now = time.Now
	}
	return p, nil
}

// Name returns "akash".
func (p *Provider) Name() string { return providerName }

// ListLeases returns all of the owner's leases, in the order Akash lists
// them. Closed leases have no name, resources or expiry: fetching their
// deployments would cost one request each, for history that's rarely needed.
func (p *Provider) ListLeases(ctx context.Context) ([]lease.Lease, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	dtos, err := p.fetchLeases(ctx)
	if err != nil {
		return nil, err
	}

	conv := converter{now: p.now(), blockTime: p.blockTime}
	deployments := make(map[string]*deploymentResponse)
	leases := make([]lease.Lease, 0, len(dtos))
	for _, d := range dtos {
		// Check the ID before using its dseq in a request.
		if _, err := leaseKey(d.ID); err != nil {
			return nil, badResponse("lease: %v", err)
		}

		var dep *deploymentResponse
		var spend *big.Rat
		if d.State != "closed" {
			if dep, err = p.deployment(ctx, deployments, d.ID.DSeq); err != nil {
				return nil, err
			}
			if spend, err = spendPerBlock(dtos, d.ID.DSeq, d.Price.Denom); err != nil {
				return nil, badResponse("deployment %s: price: %v", d.ID.DSeq, err)
			}
		}

		l, err := conv.toLease(d, dep, spend)
		if err != nil {
			return nil, err
		}
		leases = append(leases, l)
	}
	return leases, nil
}

// fetchLeases reads every page of the owner's lease list, up to maxPages.
func (p *Provider) fetchLeases(ctx context.Context) ([]leaseDTO, error) {
	var all []leaseDTO
	key := ""
	for range p.maxPages {
		q := url.Values{}
		q.Set("filters.owner", p.owner)
		q.Set("pagination.limit", strconv.Itoa(pageLimit))
		if key != "" {
			q.Set("pagination.key", key)
		}

		var page leaseListResponse
		if err := p.getJSON(ctx, "lease list", leasesPath, q, &page); err != nil {
			return nil, err
		}
		for _, e := range page.Leases {
			all = append(all, e.Lease)
		}
		if page.Pagination.NextKey == "" {
			return all, nil
		}
		key = page.Pagination.NextKey
	}
	return nil, fmt.Errorf("lease list: %w: more than %d pages of %d", ErrTooManyPages, p.maxPages, pageLimit)
}

// deployment returns the info for deployment dseq, fetching it at most once
// per ListLeases call: a deployment with several leases is requested once.
func (p *Provider) deployment(ctx context.Context, cache map[string]*deploymentResponse, dseq string) (*deploymentResponse, error) {
	if dep, ok := cache[dseq]; ok {
		return dep, nil
	}
	q := url.Values{}
	q.Set("id.owner", p.owner)
	q.Set("id.dseq", dseq)

	var dep deploymentResponse
	if err := p.getJSON(ctx, "deployment info", deploymentPath, q, &dep); err != nil {
		return nil, err
	}
	cache[dseq] = &dep
	return &dep, nil
}
