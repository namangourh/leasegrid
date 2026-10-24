package akash

import (
	"os"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
)

// TestLiveAkash queries the real Akash API. It is skipped unless
// LEASEGRID_AKASH_LIVE_OWNER holds an akash1... address, so CI and ordinary
// test runs never touch the network. Akash lease data is public, so no
// credential is needed or read.
func TestLiveAkash(t *testing.T) {
	owner := os.Getenv("LEASEGRID_AKASH_LIVE_OWNER")
	if owner == "" {
		t.Skip("set LEASEGRID_AKASH_LIVE_OWNER to run against the real Akash API")
	}
	baseURL := os.Getenv("LEASEGRID_AKASH_LIVE_URL")
	if baseURL == "" {
		baseURL = "https://api.akashnet.net"
	}

	p, err := New(Config{BaseURL: baseURL, Owner: owner, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	leases, err := p.ListLeases(t.Context())
	if err != nil {
		t.Fatalf("ListLeases() error = %v", err)
	}

	t.Logf("%d leases for %s", len(leases), owner)
	byStatus := make(map[lease.Status]int)
	shown := 0
	for i, l := range leases {
		if err := l.Validate(); err != nil {
			t.Errorf("lease %d is invalid: %v", i, err)
		}
		byStatus[l.Status]++
		// Closed leases carry little detail, so show open ones.
		if l.Status != lease.StatusClosed && shown < 5 {
			t.Logf("%+v", l)
			shown++
		}
	}
	t.Logf("leases by status: %v", byStatus)
}

// Command To Run Test
// $owner = (Invoke-RestMethod "https://api.akashnet.net/akash/market/v1beta5/leases/list?filters.state=active&pagination.limit=1").leases[0].lease.id.owner
// $env:LEASEGRID_AKASH_LIVE_OWNER = $owner
// go test -count=1 -v -run TestLiveAkash ./internal/providers/akash/
// Remove-Item Env:LEASEGRID_AKASH_LIVE_OWNER
// go test -count=1 -v -run TestLiveAkash ./internal/providers/akash/
