package akash

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
)

// providerName is this provider's Name and the Provider field of every lease
// it returns.
const providerName = "akash"

// converter turns Akash's per-block figures into LeaseGrid's units: money per
// hour, and wall-clock expiry times.
type converter struct {
	now       time.Time     // the moment expiry estimates count from
	blockTime time.Duration // average time between Akash blocks; must be > 0
}

// toLease converts one Akash lease. dep is the lease's deployment, and
// spendPerBlock is what all the deployment's open leases cost per block
// together. Both are nil for closed leases, which then get no name, resources
// or expiry. Every error wraps providers.ErrBadResponse.
func (c converter) toLease(l leaseDTO, dep *deploymentResponse, spendPerBlock *big.Rat) (lease.Lease, error) {
	id, err := leaseKey(l.ID)
	if err != nil {
		return lease.Lease{}, badResponse("lease: %v", err)
	}
	status, err := mapState(l.State)
	if err != nil {
		return lease.Lease{}, badResponse("lease %s: %v", id, err)
	}
	cost, err := c.cost(l.Price)
	if err != nil {
		return lease.Lease{}, badResponse("lease %s: price: %v", id, err)
	}

	out := lease.Lease{ID: id, Provider: providerName, Status: status, Cost: cost}
	if dep == nil {
		return out, nil
	}

	g, ok := findGroup(dep.Groups, l.ID.GSeq)
	if !ok {
		return lease.Lease{}, badResponse("lease %s: deployment has no group %d", id, l.ID.GSeq)
	}
	out.Name = g.GroupSpec.Name
	if out.Resources, err = totalResources(g.GroupSpec.Resources); err != nil {
		return lease.Lease{}, badResponse("lease %s: resources: %v", id, err)
	}
	out.Expires = c.expires(dep.EscrowAccount.State.Funds, l.Price.Denom, spendPerBlock)
	return out, nil
}

// badResponse wraps a formatted message with providers.ErrBadResponse. The
// details are formatted with %v, deliberately: they describe the problem,
// and callers only need to match the category.
func badResponse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", providers.ErrBadResponse, fmt.Sprintf(format, args...))
}

// leaseKey builds LeaseGrid's ID for an Akash lease, "dseq/gseq/oseq", which
// is unique among one owner's leases.
func leaseKey(id leaseID) (string, error) {
	if id.DSeq == "" {
		return "", errors.New("missing id.dseq")
	}
	dseq, err := strconv.ParseUint(id.DSeq, 10, 64)
	if err != nil {
		return "", errors.New("invalid id.dseq")
	}
	if id.GSeq == 0 {
		return "", errors.New("missing id.gseq")
	}
	if id.OSeq == 0 {
		return "", errors.New("missing id.oseq")
	}
	return fmt.Sprintf("%d/%d/%d", dseq, id.GSeq, id.OSeq), nil
}

// mapState converts an Akash lease state into a lease.Status.
func mapState(s string) (lease.Status, error) {
	switch s {
	case "active":
		return lease.StatusActive, nil
	case "closed":
		return lease.StatusClosed, nil
	case "insufficient_funds":
		return lease.StatusInsufficientFunds, nil
	case "":
		return "", errors.New("missing state")
	default:
		// %.32q quotes at most 32 characters, so a hostile response can't
		// make the error message arbitrarily long.
		return "", fmt.Errorf("unsupported state %.32q", s)
	}
}

// cost converts a per-block price into a per-hour Cost, rounded half up.
func (c converter) cost(price coin) (lease.Cost, error) {
	if price.Denom == "" {
		return lease.Cost{}, errors.New("missing denom")
	}
	perBlock, err := parseAmount(price.Amount)
	if err != nil {
		return lease.Cost{}, err
	}
	blocksPerHour := new(big.Rat).SetFrac64(int64(time.Hour), int64(c.blockTime))
	perHour, ok := roundToInt64(new(big.Rat).Mul(perBlock, blocksPerHour))
	if !ok {
		return lease.Cost{}, errors.New("amount out of range")
	}
	return lease.Cost{PerHour: perHour, Denom: price.Denom}, nil
}

// expires estimates when a deployment's escrow runs out: its funds in denom,
// divided by what its leases spend per block, times the block time. It
// returns the zero time (unknown) when there are no matching funds, nothing
// is being spent, or the estimate doesn't fit in a time.Duration.
func (c converter) expires(funds []coin, denom string, spendPerBlock *big.Rat) time.Time {
	if spendPerBlock == nil || spendPerBlock.Sign() == 0 {
		return time.Time{}
	}
	for _, f := range funds {
		if f.Denom != denom {
			continue
		}
		amount, err := parseAmount(f.Amount)
		if err != nil {
			return time.Time{}
		}
		blocks := new(big.Rat).Quo(amount, spendPerBlock)
		nanos, ok := roundToInt64(blocks.Mul(blocks, new(big.Rat).SetInt64(int64(c.blockTime))))
		if !ok {
			return time.Time{}
		}
		return c.now.Add(time.Duration(nanos))
	}
	return time.Time{}
}

// spendPerBlock sums the per-block prices, in denom, of deployment dseq's
// leases that aren't closed: together they drain the deployment's escrow.
func spendPerBlock(leases []leaseDTO, dseq, denom string) (*big.Rat, error) {
	total := new(big.Rat)
	for _, l := range leases {
		if l.ID.DSeq != dseq || l.State == "closed" || l.Price.Denom != denom {
			continue
		}
		p, err := parseAmount(l.Price.Amount)
		if err != nil {
			return nil, err
		}
		total.Add(total, p)
	}
	return total, nil
}

// findGroup returns the deployment group with sequence number gseq.
func findGroup(groups []group, gseq uint32) (group, bool) {
	for _, g := range groups {
		if g.ID.GSeq == gseq {
			return g, true
		}
	}
	return group{}, false
}

// totalResources adds up a group's resources: each profile's units times its
// count.
func totalResources(rs []groupResource) (lease.Resources, error) {
	var total lease.Resources
	for _, r := range rs {
		cpu, err := parseQuantity(r.Resource.CPU.Units)
		if err != nil {
			return lease.Resources{}, err
		}
		mem, err := parseQuantity(r.Resource.Memory.Quantity)
		if err != nil {
			return lease.Resources{}, err
		}
		gpu, err := parseQuantity(r.Resource.GPU.Units)
		if err != nil {
			return lease.Resources{}, err
		}
		var storage int64
		for _, s := range r.Resource.Storage {
			v, err := parseQuantity(s.Quantity)
			if err != nil {
				return lease.Resources{}, err
			}
			storage += v
		}

		count := int64(r.Count)
		total.CPUMillis += cpu * count
		total.MemoryBytes += mem * count
		total.StorageBytes += storage * count
		total.GPUs += gpu * count
	}
	return total, nil
}

// parseQuantity parses an integer sent as a string. An omitted value means 0.
func parseQuantity(q quantity) (int64, error) {
	if q.Val == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(q.Val, 10, 64)
	if err != nil || v < 0 {
		return 0, errors.New("invalid quantity")
	}
	return v, nil
}

// parseAmount parses a Cosmos decimal amount such as "4.826603000000000000"
// exactly, as a fraction. float64 would round it.
func parseAmount(s string) (*big.Rat, error) {
	if s == "" {
		return nil, errors.New("missing amount")
	}
	if !isDecimal(s) {
		return nil, errors.New("invalid amount")
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, errors.New("invalid amount")
	}
	return r, nil
}

// isDecimal reports whether s looks like a Cosmos decimal: at most 80 digits
// with at most one dot. big.Rat would also accept exponents and fractions,
// but an input like "1e999999999" would make it compute an enormous number,
// so anything else is rejected before parsing.
func isDecimal(s string) bool {
	if len(s) > 80 {
		return false
	}
	digits, dots := 0, 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.':
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}

// roundToInt64 rounds a non-negative r to the nearest integer, halves up. It
// reports false if the result doesn't fit in an int64.
func roundToInt64(r *big.Rat) (int64, bool) {
	// floor((2*num + den) / (2*den)) rounds half up for non-negative values.
	num := new(big.Int).Mul(r.Num(), big.NewInt(2))
	num.Add(num, r.Denom())
	den := new(big.Int).Mul(r.Denom(), big.NewInt(2))
	q := num.Quo(num, den)
	if !q.IsInt64() {
		return 0, false
	}
	return q.Int64(), true
}
