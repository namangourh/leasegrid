package output_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
)

var tableNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func TestTableExact(t *testing.T) {
	ibc := "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"
	leases := []lease.Lease{
		{ID: "1001/1/1", Provider: "demo-b", Name: "worker", Status: lease.StatusInsufficientFunds,
			Expires: tableNow.Add(-2 * time.Hour), Cost: lease.Cost{PerHour: 900, Denom: "uakt"}},
		{ID: "1001/1/1", Provider: "demo-a", Name: "web", Status: lease.StatusActive,
			Expires: tableNow.Add(3*time.Hour + 12*time.Minute + 59*time.Second), Cost: lease.Cost{PerHour: 4990, Denom: ibc}},
		{ID: "2001/1/1", Provider: "demo-b", Name: "gpu", Status: lease.StatusActive,
			Expires: tableNow.Add(45 * time.Minute), Cost: lease.Cost{PerHour: 36000, Denom: "uakt"}},
		{ID: "1002/1/1", Provider: "demo-a", Name: "api", Status: lease.StatusActive,
			Expires: tableNow.Add(10*24*time.Hour + 4*time.Hour), Cost: lease.Cost{PerHour: 1800, Denom: "uakt"}},
		{ID: "x", Provider: "demo-a", Status: lease.StatusActive, Expires: tableNow.Add(30 * time.Second)}, // no name, no cost
		{ID: "1003/1/1", Provider: "demo-a", Name: "batch", Status: lease.StatusClosed,
			Cost: lease.Cost{PerHour: 1200, Denom: "uakt"}}, // unknown expiry
	}
	want := `ID        PROVIDER  NAME    STATUS              EXPIRES                                  COST/HOUR
1001/1/1  demo-b    worker  insufficient_funds  2026-10-20 10:00 UTC (expired 2h0m ago)  900 uakt
1001/1/1  demo-a    web     active              2026-10-20 15:12 UTC (in 3h12m)          4990 ibc/170C...F2F1
2001/1/1  demo-b    gpu     active              2026-10-20 12:45 UTC (in 45m)            36000 uakt
1002/1/1  demo-a    api     active              2026-10-30 16:00 UTC (in 10d4h)          1800 uakt
x         demo-a    -       active              2026-10-20 12:00 UTC (in <1m)            -
1003/1/1  demo-a    batch   closed              -                                        1200 uakt
`

	var buf bytes.Buffer
	if err := (output.Table{Now: tableNow}).Render(&buf, leases); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("Render() output =\n%s\nwant\n%s", got, want)
	}
}

func TestTableEmptyWritesNothing(t *testing.T) {
	for _, leases := range [][]lease.Lease{nil, {}} {
		var buf bytes.Buffer
		if err := (output.Table{Now: tableNow}).Render(&buf, leases); err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		if buf.Len() != 0 {
			t.Errorf("Render(%v) wrote %q, want nothing", leases, buf.String())
		}
	}
}

func TestTableReplacesControlCharacters(t *testing.T) {
	// A provider could return a name containing a tab, a newline or a
	// terminal escape sequence (\x1b[31m turns text red).
	leases := []lease.Lease{{ID: "1", Provider: "akash", Name: "evil\t\x1b[31m\nname", Status: lease.StatusActive}}

	var buf bytes.Buffer
	if err := (output.Table{Now: tableNow}).Render(&buf, leases); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "evil??[31m?name") {
		t.Errorf("Render() output = %q, want the name with control characters replaced", got)
	}
	if strings.ContainsAny(got, "\t\x1b") || strings.Count(got, "\n") != 2 {
		t.Errorf("Render() output = %q, want no tabs or escapes and exactly two lines", got)
	}
}
