package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
	"github.com/namangourh/leasegrid/internal/summary"
)

const statusIBC = "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"

// statusDemo returns a dashboard like the demo providers give: five leases,
// sorted by expiry, and the summary of them with a 7-day window.
func statusDemo() output.Status {
	return output.Status{
		Now:    tableNow,
		Window: 7 * 24 * time.Hour,
		Summary: summary.Summary{
			Total: 5, Active: 3, InsufficientFunds: 1, Closed: 1, ExpiringSoon: 2,
			ByProvider: map[string]int{"demo-b": 2, "demo-a": 3},
			SpendPerHour: []lease.Cost{
				{PerHour: 4990, Denom: statusIBC},
				{PerHour: 37800, Denom: "uakt"},
			},
		},
		Leases: []lease.Lease{
			{ID: "1001/1/1", Provider: "demo-b", Name: "worker", Status: lease.StatusInsufficientFunds,
				Expires: tableNow.Add(-2 * time.Hour), Cost: lease.Cost{PerHour: 900, Denom: "uakt"}},
			{ID: "2001/1/1", Provider: "demo-b", Name: "gpu", Status: lease.StatusActive,
				Expires: tableNow.Add(45 * time.Minute), Cost: lease.Cost{PerHour: 36000, Denom: "uakt"}},
			{ID: "1001/1/1", Provider: "demo-a", Name: "web", Status: lease.StatusActive,
				Expires: tableNow.Add(3*time.Hour + 12*time.Minute), Cost: lease.Cost{PerHour: 4990, Denom: statusIBC}},
			{ID: "1002/1/1", Provider: "demo-a", Name: "api", Status: lease.StatusActive,
				Expires: tableNow.Add(10*24*time.Hour + 4*time.Hour), Cost: lease.Cost{PerHour: 1800, Denom: "uakt"}},
			{ID: "1003/1/1", Provider: "demo-a", Name: "batch", Status: lease.StatusClosed,
				Cost: lease.Cost{PerHour: 1200, Denom: "uakt"}},
		},
	}
}

// statusEmpty returns the dashboard for no leases, with the summary
// summary.Calculate gives for them.
func statusEmpty() output.Status {
	return output.Status{
		Now:     tableNow,
		Window:  24 * time.Hour,
		Summary: summary.Summary{ByProvider: map[string]int{}, SpendPerHour: []lease.Cost{}},
	}
}

func TestParseStatusFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    output.Format
		wantErr error
	}{
		{in: "table", want: output.FormatTable},
		{in: "json", want: output.FormatJSON},
		{in: "yaml", wantErr: output.ErrUnknownFormat}, // list has YAML; status doesn't
		{in: "JSON", wantErr: output.ErrUnknownFormat},
		{in: "", wantErr: output.ErrUnknownFormat},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := output.ParseStatusFormat(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseStatusFormat(%q) error = %v, want %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseStatusFormat(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	_, err := output.ParseStatusFormat("yaml")
	if want := `unknown output format "yaml" (supported: table, json)`; err == nil || err.Error() != want {
		t.Errorf("ParseStatusFormat(%q) error = %v, want %q", "yaml", err, want)
	}
}

func TestStatusTextExact(t *testing.T) {
	want := `Leases:         5 (3 active, 1 insufficient_funds, 1 closed)
Expiring soon:  2 within 7d0h
Active spend:   4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1 per hour
                37800 uakt per hour
By provider:    demo-a 3, demo-b 2

ID        PROVIDER  NAME    STATUS              EXPIRES                                  COST/HOUR
1001/1/1  demo-b    worker  insufficient_funds  2026-10-20 10:00 UTC (expired 2h0m ago)  900 uakt
2001/1/1  demo-b    gpu     active              2026-10-20 12:45 UTC (in 45m)            36000 uakt
1001/1/1  demo-a    web     active              2026-10-20 15:12 UTC (in 3h12m)          4990 ibc/170C...F2F1
1002/1/1  demo-a    api     active              2026-10-30 16:00 UTC (in 10d4h)          1800 uakt
1003/1/1  demo-a    batch   closed              -                                        1200 uakt
`

	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatTable, statusDemo()); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("WriteStatus() output =\n%s\nwant\n%s", got, want)
	}
}

func TestStatusTextEmpty(t *testing.T) {
	want := `Leases:         0 (0 active, 0 insufficient_funds, 0 closed)
Expiring soon:  0 within 1d0h
Active spend:   -
By provider:    -

No leases found.
`

	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatTable, statusEmpty()); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("WriteStatus() output =\n%s\nwant\n%s", got, want)
	}
}

func TestStatusCounts(t *testing.T) {
	// Every count differs, so a count shown in the wrong place is caught.
	st := statusEmpty()
	st.Summary.Total, st.Summary.Active, st.Summary.InsufficientFunds = 9, 4, 3
	st.Summary.Closed, st.Summary.ExpiringSoon = 2, 1

	var text bytes.Buffer
	if err := output.WriteStatus(&text, output.FormatTable, st); err != nil {
		t.Fatalf("WriteStatus(table) error = %v", err)
	}
	if want := "Leases:         9 (4 active, 3 insufficient_funds, 2 closed)\nExpiring soon:  1 within 1d0h\n"; !strings.HasPrefix(text.String(), want) {
		t.Errorf("WriteStatus(table) output =\n%s\nwant it to start with\n%s", text.String(), want)
	}

	var js bytes.Buffer
	if err := output.WriteStatus(&js, output.FormatJSON, st); err != nil {
		t.Fatalf("WriteStatus(json) error = %v", err)
	}
	want := `"total": 9,
  "active": 4,
  "insufficient_funds": 3,
  "closed": 2,
  "expiring_soon": 1,`
	if !strings.Contains(js.String(), want) {
		t.Errorf("WriteStatus(json) output =\n%s\nwant it to contain\n%s", js.String(), want)
	}
}

func TestStatusTextReplacesControlCharacters(t *testing.T) {
	// Provider names, lease names and denoms all come from outside; any of
	// them could hold a newline that fakes a line or an escape sequence.
	st := statusEmpty()
	st.Summary = summary.Summary{
		Total: 1, Active: 1,
		ByProvider:   map[string]int{"evil\nActive spend:   0 uakt": 1},
		SpendPerHour: []lease.Cost{{PerHour: 1, Denom: "uakt\x1b[2J"}},
	}
	st.Leases = []lease.Lease{{ID: "1", Provider: "evil\nActive spend:   0 uakt", Name: "a\x1b[31m", Status: lease.StatusActive}}

	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatTable, st); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "Active spend:   1 uakt?[2J per hour\n") ||
		!strings.Contains(got, "By provider:    evil?Active spend:   0 uakt 1\n") {
		t.Errorf("WriteStatus() output = %q, want the denom and provider with control characters replaced", got)
	}
	if strings.Contains(got, "\x1b") || strings.Count(got, "\n") != 7 {
		t.Errorf("WriteStatus() output = %q, want no escapes and exactly seven lines", got)
	}
}

func TestStatusJSONExact(t *testing.T) {
	st := statusDemo()
	st.Leases = st.Leases[:2]
	want := `{
  "as_of": "2026-10-20T12:00:00Z",
  "expiring_within": "168h0m0s",
  "total": 5,
  "active": 3,
  "insufficient_funds": 1,
  "closed": 1,
  "expiring_soon": 2,
  "by_provider": {
    "demo-a": 3,
    "demo-b": 2
  },
  "spend_per_hour": [
    {
      "per_hour": 4990,
      "denom": "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"
    },
    {
      "per_hour": 37800,
      "denom": "uakt"
    }
  ],
  "leases": [
    {
      "id": "1001/1/1",
      "provider": "demo-b",
      "name": "worker",
      "status": "insufficient_funds",
      "expires": "2026-10-20T10:00:00Z",
      "cost": {
        "per_hour": 900,
        "denom": "uakt"
      }
    },
    {
      "id": "2001/1/1",
      "provider": "demo-b",
      "name": "gpu",
      "status": "active",
      "expires": "2026-10-20T12:45:00Z",
      "cost": {
        "per_hour": 36000,
        "denom": "uakt"
      }
    }
  ]
}
`

	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatJSON, st); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("WriteStatus() output =\n%s\nwant\n%s", got, want)
	}
}

func TestStatusJSONEmpty(t *testing.T) {
	// Scripts can rely on every field being present, with {} and [] rather
	// than null when there is nothing to report.
	want := `{
  "as_of": "2026-10-20T12:00:00Z",
  "expiring_within": "24h0m0s",
  "total": 0,
  "active": 0,
  "insufficient_funds": 0,
  "closed": 0,
  "expiring_soon": 0,
  "by_provider": {},
  "spend_per_hour": [],
  "leases": []
}
`

	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatJSON, statusEmpty()); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("WriteStatus() output =\n%s\nwant\n%s", got, want)
	}
}

func TestStatusJSONLargeAmounts(t *testing.T) {
	// The largest total an int64 holds is written digit for digit: no
	// rounding through float64, no exponent.
	st := statusEmpty()
	st.Summary.SpendPerHour = []lease.Cost{{PerHour: math.MaxInt64, Denom: "uakt"}}

	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatJSON, st); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	if !strings.Contains(buf.String(), `"per_hour": 9223372036854775807,`) {
		t.Errorf("WriteStatus() output =\n%s\nwant per_hour 9223372036854775807", buf.String())
	}

	var got struct {
		SpendPerHour []lease.Cost `json:"spend_per_hour"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(got.SpendPerHour) != 1 || got.SpendPerHour[0].PerHour != math.MaxInt64 {
		t.Errorf("decoded spend_per_hour = %+v, want %d", got.SpendPerHour, int64(math.MaxInt64))
	}
}

func TestStatusJSONHasNoEscapeCharacters(t *testing.T) {
	// JSON is for scripts: a raw escape character from provider data must not
	// reach a terminal that displays it. encoding/json writes it as \u001b.
	st := statusEmpty()
	st.Leases = []lease.Lease{{ID: "1", Provider: "a", Name: "a\x1b[31m", Status: lease.StatusActive}}

	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatJSON, st); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	if got := buf.String(); strings.Contains(got, "\x1b") || !strings.Contains(got, `"name": "a\u001b[31m"`) {
		t.Errorf("WriteStatus() output = %q, want the escape character written as \\u001b", got)
	}
}

func TestWriteStatusReturnsWriterError(t *testing.T) {
	for _, f := range []output.Format{output.FormatTable, output.FormatJSON} {
		t.Run(string(f), func(t *testing.T) {
			err := output.WriteStatus(failingWriter{}, f, statusDemo())
			if !errors.Is(err, errWrite) {
				t.Fatalf("WriteStatus() error = %v, want %v", err, errWrite)
			}
		})
	}
}

func TestWriteStatusUnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := output.WriteStatus(&buf, output.FormatYAML, statusDemo())
	if !errors.Is(err, output.ErrUnknownFormat) {
		t.Fatalf("WriteStatus() error = %v, want %v", err, output.ErrUnknownFormat)
	}
	if buf.Len() != 0 {
		t.Errorf("WriteStatus() wrote %q, want nothing", buf.String())
	}
}
