package akash

import (
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
)

// Sanitized test identities: obviously fake, never real addresses.
const (
	testOwner = "akash1testowner0000000000000000000000000000"
	testHost  = "akash1testhost00000000000000000000000000000"
)

// testNow is the fixed "current time" all expiry estimates count from.
var testNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

var testConverter = converter{now: testNow, blockTime: 6 * time.Second}

// akashLease builds an Akash lease record for deployment dseq, group 1, order 1.
func akashLease(dseq, state, denom, amount string) leaseDTO {
	return leaseDTO{
		ID:    leaseID{Owner: testOwner, DSeq: dseq, GSeq: 1, OSeq: 1, Provider: testHost},
		State: state,
		Price: coin{Denom: denom, Amount: amount},
	}
}

// deployment builds a deployment with one group (gseq 1) and escrow funds.
func deployment(name string, funds coin, rs ...groupResource) *deploymentResponse {
	return &deploymentResponse{
		Groups:        []group{{ID: groupID{GSeq: 1}, GroupSpec: groupSpec{Name: name, Resources: rs}}},
		EscrowAccount: escrowAccount{State: escrowState{Funds: []coin{funds}}},
	}
}

// profile builds a resource profile: CPU millicores, memory bytes, GPUs,
// instance count, and any number of storage volumes in bytes.
func profile(cpu, mem, gpu string, count uint32, storage ...string) groupResource {
	r := groupResource{Count: count}
	r.Resource.CPU.Units.Val = cpu
	r.Resource.Memory.Quantity.Val = mem
	r.Resource.GPU.Units.Val = gpu
	for _, s := range storage {
		r.Resource.Storage = append(r.Resource.Storage, storageUnit{Quantity: quantity{Val: s}})
	}
	return r
}

// rat parses a decimal for test inputs, failing the test on bad input.
func rat(t *testing.T, s string) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		t.Fatalf("bad test decimal %q", s)
	}
	return r
}

// sameLease compares leases, using time.Time.Equal for Expires: == on
// time.Time also compares internal details, so equal instants can differ.
func sameLease(a, b lease.Lease) bool {
	ae, be := a.Expires, b.Expires
	a.Expires, b.Expires = time.Time{}, time.Time{}
	return a == b && ae.Equal(be)
}

func TestToLease(t *testing.T) {
	tests := []struct {
		name         string
		in           leaseDTO
		dep          *deploymentResponse
		spend        string // per-block spend of the deployment; "" for closed leases
		want         lease.Lease
		wantExpiring bool // expires within 24 hours of testNow
	}{
		{
			name:  "active lease",
			in:    akashLease("1001", "active", "uact", "5.000000000000000000"),
			dep:   deployment("web", coin{Denom: "uact", Amount: "1800000.000000000000000000"}, profile("500", "536870912", "0", 1, "1073741824")),
			spend: "5",
			want: lease.Lease{
				ID: "1001/1/1", Provider: "akash", Name: "web", Status: lease.StatusActive,
				Expires:   testNow.Add(600 * time.Hour), // 1,800,000 / 5 = 360,000 blocks x 6 s
				Cost:      lease.Cost{PerHour: 3000, Denom: "uact"},
				Resources: lease.Resources{CPUMillis: 500, MemoryBytes: 536870912, StorageBytes: 1073741824},
			},
		},
		{
			name:  "expiring lease",
			in:    akashLease("1002", "active", "uakt", "10"),
			dep:   deployment("worker", coin{Denom: "uakt", Amount: "6000"}, profile("1000", "1073741824", "1", 2, "1073741824", "5368709120")),
			spend: "10",
			want: lease.Lease{
				ID: "1002/1/1", Provider: "akash", Name: "worker", Status: lease.StatusActive,
				Expires:   testNow.Add(time.Hour), // 6,000 / 10 = 600 blocks x 6 s
				Cost:      lease.Cost{PerHour: 6000, Denom: "uakt"},
				Resources: lease.Resources{CPUMillis: 2000, MemoryBytes: 2147483648, StorageBytes: 12884901888, GPUs: 2},
			},
			wantExpiring: true,
		},
		{
			name: "closed lease",
			in:   akashLease("900", "closed", "uakt", "2.5"),
			want: lease.Lease{
				ID: "900/1/1", Provider: "akash", Status: lease.StatusClosed,
				Cost: lease.Cost{PerHour: 1500, Denom: "uakt"},
			},
		},
		{
			name:  "insufficient funds",
			in:    akashLease("1003", "insufficient_funds", "uakt", "1"),
			dep:   deployment("batch", coin{Denom: "uakt", Amount: "0"}, profile("250", "268435456", "0", 1, "536870912")),
			spend: "1",
			want: lease.Lease{
				ID: "1003/1/1", Provider: "akash", Name: "batch", Status: lease.StatusInsufficientFunds,
				Expires:   testNow, // no funds left
				Cost:      lease.Cost{PerHour: 600, Denom: "uakt"},
				Resources: lease.Resources{CPUMillis: 250, MemoryBytes: 268435456, StorageBytes: 536870912},
			},
			wantExpiring: true,
		},
		{
			name:  "funds in another denomination give no estimate",
			in:    akashLease("1004", "active", "uakt", "1"),
			dep:   deployment("api", coin{Denom: "uact", Amount: "1000"}, profile("100", "1", "0", 1)),
			spend: "1",
			want: lease.Lease{
				ID: "1004/1/1", Provider: "akash", Name: "api", Status: lease.StatusActive,
				Cost:      lease.Cost{PerHour: 600, Denom: "uakt"},
				Resources: lease.Resources{CPUMillis: 100, MemoryBytes: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var spend *big.Rat
			if tt.spend != "" {
				spend = rat(t, tt.spend)
			}

			got, err := testConverter.toLease(tt.in, tt.dep, spend)
			if err != nil {
				t.Fatalf("toLease() error = %v", err)
			}
			if !sameLease(got, tt.want) {
				t.Errorf("toLease() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("mapped lease is invalid: %v", err)
			}
			if expiring := got.ExpiresWithin(testNow, 24*time.Hour); expiring != tt.wantExpiring {
				t.Errorf("ExpiresWithin(24h) = %v, want %v", expiring, tt.wantExpiring)
			}
		})
	}
}

func TestToLeaseErrors(t *testing.T) {
	withID := func(id leaseID) leaseDTO {
		l := akashLease("1", "active", "uakt", "1")
		l.ID = id
		return l
	}
	otherGroup := deployment("web", coin{Denom: "uakt", Amount: "1"})
	otherGroup.Groups[0].ID.GSeq = 2

	tests := []struct {
		name    string
		in      leaseDTO
		dep     *deploymentResponse
		wantMsg string
	}{
		{name: "missing dseq", in: withID(leaseID{GSeq: 1, OSeq: 1}), wantMsg: "missing id.dseq"},
		{name: "non-numeric dseq", in: withID(leaseID{DSeq: "abc", GSeq: 1, OSeq: 1}), wantMsg: "invalid id.dseq"},
		{name: "missing gseq", in: withID(leaseID{DSeq: "1", OSeq: 1}), wantMsg: "missing id.gseq"},
		{name: "missing oseq", in: withID(leaseID{DSeq: "1", GSeq: 1}), wantMsg: "missing id.oseq"},
		{name: "missing state", in: akashLease("1", "", "uakt", "1"), wantMsg: "missing state"},
		{name: "unsupported state", in: akashLease("1", "pending", "uakt", "1"), wantMsg: `unsupported state "pending"`},
		{name: "missing denom", in: akashLease("1", "active", "", "1"), wantMsg: "missing denom"},
		{name: "missing amount", in: akashLease("1", "active", "uakt", ""), wantMsg: "missing amount"},
		{name: "exponent amount", in: akashLease("1", "active", "uakt", "1e999999999"), wantMsg: "invalid amount"},
		{name: "group not found", in: akashLease("1", "active", "uakt", "1"), dep: otherGroup, wantMsg: "deployment has no group 1"},
		{
			name:    "invalid resource quantity",
			in:      akashLease("1", "active", "uakt", "1"),
			dep:     deployment("web", coin{Denom: "uakt", Amount: "1"}, profile("lots", "1", "0", 1)),
			wantMsg: "invalid quantity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testConverter.toLease(tt.in, tt.dep, big.NewRat(1, 1))
			if !errors.Is(err, providers.ErrBadResponse) {
				t.Fatalf("toLease() error = %v, want providers.ErrBadResponse", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("toLease() error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

// TestErrorLengthIsBounded checks that a hostile value can't produce a huge
// error message.
func TestErrorLengthIsBounded(t *testing.T) {
	_, err := testConverter.toLease(akashLease("1", strings.Repeat("x", 10000), "uakt", "1"), nil, nil)
	if err == nil || len(err.Error()) > 150 {
		t.Fatalf("toLease() error has length %d, want an error of at most 150 bytes", len(err.Error()))
	}
}

func TestCostRounding(t *testing.T) {
	tests := []struct {
		perBlock string
		want     int64 // per hour, at 600 blocks per hour
	}{
		{perBlock: "5", want: 3000},
		{perBlock: "4.826603000000000000", want: 2896}, // 2895.9618
		{perBlock: "0.0025", want: 2},                  // exactly 1.5: halves round up
		{perBlock: "0.000833333333333333", want: 0},    // 0.4999999999999998
		{perBlock: "0.000834", want: 1},                // 0.5004
		{perBlock: "0", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.perBlock, func(t *testing.T) {
			got, err := testConverter.cost(coin{Denom: "uakt", Amount: tt.perBlock})
			if err != nil {
				t.Fatalf("cost() error = %v", err)
			}
			if got.PerHour != tt.want {
				t.Errorf("cost(%s).PerHour = %d, want %d", tt.perBlock, got.PerHour, tt.want)
			}
		})
	}
}

func TestParseAmount(t *testing.T) {
	valid := []string{"0", "15760388", "4.826603000000000000", "0.5"}
	for _, s := range valid {
		if _, err := parseAmount(s); err != nil {
			t.Errorf("parseAmount(%q) error = %v, want nil", s, err)
		}
	}

	invalid := []string{"", "abc", "-1", "1e9", "1/2", "1.2.3", ".", strings.Repeat("9", 81)}
	for _, s := range invalid {
		if _, err := parseAmount(s); err == nil {
			t.Errorf("parseAmount(%q) error = nil, want an error", s)
		}
	}
}

func TestExpiresUnknown(t *testing.T) {
	funds := []coin{{Denom: "uakt", Amount: "1000"}}

	tests := []struct {
		name  string
		funds []coin
		spend *big.Rat
	}{
		{name: "nothing spent", funds: funds, spend: new(big.Rat)},
		{name: "no spend given", funds: funds, spend: nil},
		{name: "invalid funds", funds: []coin{{Denom: "uakt", Amount: "lots"}}, spend: big.NewRat(1, 1)},
		{name: "beyond time.Duration", funds: []coin{{Denom: "uakt", Amount: strings.Repeat("9", 40)}}, spend: big.NewRat(1, 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := testConverter.expires(tt.funds, "uakt", tt.spend); !got.IsZero() {
				t.Errorf("expires() = %v, want the zero time (unknown)", got)
			}
		})
	}
}

func TestSpendPerBlock(t *testing.T) {
	second := akashLease("1001", "active", "uact", "2.5")
	second.ID.GSeq = 2 // another group of the same deployment

	leases := []leaseDTO{
		akashLease("1001", "active", "uact", "5"),
		second,
		akashLease("1001", "closed", "uact", "100"), // closed: no longer spending
		akashLease("1001", "active", "uakt", "3"),   // other denomination
		akashLease("1002", "active", "uact", "7"),   // other deployment
	}

	got, err := spendPerBlock(leases, "1001", "uact")
	if err != nil {
		t.Fatalf("spendPerBlock() error = %v", err)
	}
	if want := big.NewRat(15, 2); got.Cmp(want) != 0 { // 5 + 2.5
		t.Errorf("spendPerBlock() = %s, want %s", got.FloatString(2), want.FloatString(2))
	}

	bad := []leaseDTO{akashLease("1001", "active", "uact", "oops")}
	if _, err := spendPerBlock(bad, "1001", "uact"); err == nil {
		t.Error("spendPerBlock() with an invalid price: error = nil, want an error")
	}
}
