package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/summary"
)

// Status is everything the status dashboard shows.
type Status struct {
	// Now is the time the summary was calculated at. Expiries in the table
	// are relative to it, as in Table.
	Now time.Time

	// Window is the "expiring soon" window the summary was calculated with.
	Window time.Duration

	// Summary comes from summary.Calculate, so its map and slice are never nil.
	Summary summary.Summary

	// Leases are the leases the summary describes, in display order.
	Leases []lease.Lease
}

// ParseStatusFormat converts the status command's --format flag value into a
// Format. The dashboard has a table and a JSON view, but no YAML view.
func ParseStatusFormat(s string) (Format, error) {
	f := Format(s)
	if f != FormatTable && f != FormatJSON {
		return "", fmt.Errorf("%w %q (supported: table, json)", ErrUnknownFormat, f)
	}
	return f, nil
}

// WriteStatus renders the status dashboard to w in format f.
func WriteStatus(w io.Writer, f Format, st Status) error {
	switch f {
	case FormatTable:
		return statusText(w, st)
	case FormatJSON:
		return statusJSON(w, st)
	default:
		return fmt.Errorf("%w %q", ErrUnknownFormat, f)
	}
}

// statusText writes the dashboard for people to read: the summary, a blank
// line, then the leases in the same table as list. With no leases, the
// summary still shows its zero counts and the table is a "No leases found."
// line.
//
// The dashboard is built in memory and written with a single Write, so a
// failing writer never leaves half a dashboard behind.
func statusText(w io.Writer, st Status) error {
	s := st.Summary

	spend := []string{"-"}
	if len(s.SpendPerHour) > 0 {
		spend = nil
		for _, c := range s.SpendPerHour {
			spend = append(spend, cell(fullCost(c))+" per hour")
		}
	}
	providers := "-"
	if len(s.ByProvider) > 0 {
		var counts []string
		for _, name := range slices.Sorted(maps.Keys(s.ByProvider)) {
			counts = append(counts, cell(name)+" "+strconv.Itoa(s.ByProvider[name]))
		}
		providers = strings.Join(counts, ", ")
	}

	var buf bytes.Buffer
	line := func(label, value string) {
		// The widest label, "Expiring soon:", is 14 characters; two spaces follow it.
		fmt.Fprintf(&buf, "%-16s%s\n", label, value)
	}
	line("Leases:", fmt.Sprintf("%d (%d active, %d insufficient_funds, %d closed)",
		s.Total, s.Active, s.InsufficientFunds, s.Closed))
	line("Expiring soon:", fmt.Sprintf("%d within %s", s.ExpiringSoon, span(st.Window)))
	line("Active spend:", spend[0])
	for _, v := range spend[1:] {
		line("", v)
	}
	line("By provider:", providers)
	buf.WriteString("\n")

	if len(st.Leases) == 0 {
		buf.WriteString("No leases found.\n")
	} else {
		// Writes to a bytes.Buffer never fail, so the error is always nil.
		_ = Table{Now: st.Now}.Render(&buf, st.Leases)
	}

	_, err := w.Write(buf.Bytes())
	return err
}

// statusJSONView is the JSON form of the dashboard. Its field names are part
// of LeaseGrid's output contract: scripts depend on them, so they don't
// change.
type statusJSONView struct {
	AsOf              time.Time      `json:"as_of"`
	ExpiringWithin    string         `json:"expiring_within"`
	Total             int            `json:"total"`
	Active            int            `json:"active"`
	InsufficientFunds int            `json:"insufficient_funds"`
	Closed            int            `json:"closed"`
	ExpiringSoon      int            `json:"expiring_soon"`
	ByProvider        map[string]int `json:"by_provider"`
	SpendPerHour      []lease.Cost   `json:"spend_per_hour"`
	Leases            []lease.Lease  `json:"leases"`
}

// statusJSON writes the dashboard as one indented JSON object followed by a
// newline, for scripts. Spend totals are exact integers in each denom's
// smallest unit. No leases is an empty "leases" array, never null.
func statusJSON(w io.Writer, st Status) error {
	s := st.Summary
	view := statusJSONView{
		AsOf:              st.Now,
		ExpiringWithin:    st.Window.String(),
		Total:             s.Total,
		Active:            s.Active,
		InsufficientFunds: s.InsufficientFunds,
		Closed:            s.Closed,
		ExpiringSoon:      s.ExpiringSoon,
		ByProvider:        s.ByProvider,
		SpendPerHour:      s.SpendPerHour,
		Leases:            st.Leases,
	}
	if view.Leases == nil {
		view.Leases = []lease.Lease{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(view)
}
