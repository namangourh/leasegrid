package output

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Table renders leases as aligned columns, for people to read.
type Table struct {
	// Now is the time the EXPIRES column is relative to. Callers pass it in,
	// so tests can fix it and get the same output on every run.
	Now time.Time
}

// tableHeader names the columns, separated by tabs for tabwriter.
const tableHeader = "ID\tPROVIDER\tNAME\tSTATUS\tEXPIRES\tCOST/HOUR"

// Render writes one header line and one line per lease. An empty list writes
// nothing at all: the caller decides how to tell the user.
//
// The table is built in memory and written with a single Write, so a failing
// writer never leaves half a table behind.
func (t Table) Render(w io.Writer, leases []lease.Lease) error {
	if len(leases) == 0 {
		return nil
	}

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	// tw writes only to buf, and writes to a bytes.Buffer never fail, so the
	// errors below are always nil.
	_, _ = fmt.Fprintln(tw, tableHeader)
	for _, l := range leases {
		row := []string{
			cell(l.ID),
			cell(l.Provider),
			cell(l.Name),
			cell(string(l.Status)),
			t.expires(l.Expires),
			cell(cost(l.Cost)),
		}
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()

	_, err := w.Write(buf.Bytes())
	return err
}

// cell prepares provider-supplied text for a table cell. An empty value
// becomes "-". Non-printable characters, such as tabs, newlines and the
// escape character, become "?", so a lease name can't break the columns or
// send control sequences to the user's terminal.
func cell(s string) string {
	if s == "" {
		return "-"
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '?'
	}, s)
}

// expires formats an expiry as an absolute UTC time followed by how far it
// is from t.Now, for example "2026-10-31 14:00 UTC (in 3h12m)". An unknown
// (zero) expiry is "-".
func (t Table) expires(e time.Time) string {
	if e.IsZero() {
		return "-"
	}
	return e.UTC().Format("2006-01-02 15:04") + " UTC (" + relative(e.Sub(t.Now)) + ")"
}

// relative describes d, the time left until an expiry: "in 3h12m", or
// "expired 5m ago" when d is negative.
func relative(d time.Duration) string {
	if d < 0 {
		return "expired " + span(-d) + " ago"
	}
	return "in " + span(d)
}

// span formats a non-negative duration in its two largest units, rounded
// down to the minute: "2d4h", "3h12m", "45m", or "<1m".
func span(d time.Duration) string {
	const day = 24 * time.Hour
	days := d / day
	hours := d % day / time.Hour
	minutes := d % time.Hour / time.Minute
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return "<1m"
	}
}

// cost formats a per-hour cost, for example "4990 uakt". A cost with no
// denomination is "". Long IBC denoms are shortened (see shortDenom).
func cost(c lease.Cost) string {
	if c.Denom == "" {
		return ""
	}
	return strconv.FormatInt(c.PerHour, 10) + " " + shortDenom(c.Denom)
}

// shortDenom shortens an IBC denom, "ibc/" followed by a 64-character hash,
// to the hash's first and last four characters: "ibc/170C...F2F1". The full
// denom is still in the JSON output. Other denoms are returned unchanged.
func shortDenom(d string) string {
	hash, ok := strings.CutPrefix(d, "ibc/")
	if !ok || len(hash) <= 8 {
		return d
	}
	return "ibc/" + hash[:4] + "..." + hash[len(hash)-4:]
}
