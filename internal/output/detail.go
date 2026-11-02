package output

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Detail renders one lease as labelled lines, for people to read.
type Detail struct {
	// Now is the time the expiry is relative to, as in Table.
	Now time.Time
}

// Render writes one "Label:  value" line per field, always the same lines in
// the same order. A value the provider didn't give is "-". Unlike the table,
// the cost keeps its full denom, and resources are shown in readable units.
//
// The view is built in memory and written with a single Write, so a failing
// writer never leaves half a view behind.
func (d Detail) Render(w io.Writer, l lease.Lease) error {
	cpu, memory, storage, gpus := "-", "-", "-", "-"
	if r := l.Resources; r != (lease.Resources{}) {
		cpu = strconv.FormatFloat(float64(r.CPUMillis)/1000, 'f', -1, 64)
		memory = size(r.MemoryBytes)
		storage = size(r.StorageBytes)
		gpus = strconv.FormatInt(r.GPUs, 10)
	}

	fields := []struct{ label, value string }{
		{"ID", cell(l.ID)},
		{"Provider", cell(l.Provider)},
		{"Name", cell(l.Name)},
		{"Status", cell(string(l.Status))},
		{"Expires", expires(l.Expires, d.Now)},
		{"Cost/hour", cell(fullCost(l.Cost))},
		{"CPU", cpu},
		{"Memory", memory},
		{"Storage", storage},
		{"GPUs", gpus},
	}
	var buf bytes.Buffer
	for _, f := range fields {
		// The widest label, "Cost/hour:", is 10 characters; two spaces follow it.
		fmt.Fprintf(&buf, "%-12s%s\n", f.label+":", f.value)
	}

	_, err := w.Write(buf.Bytes())
	return err
}

// fullCost formats a per-hour cost like cost does, but keeps the full denom.
func fullCost(c lease.Cost) string {
	if c.Denom == "" {
		return ""
	}
	return strconv.FormatInt(c.PerHour, 10) + " " + c.Denom
}

// size formats a byte count in binary units with at most one decimal place,
// for example "512 MiB" or "1.5 GiB". Counts under 1 KiB are in bytes.
func size(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	// An int64 is at most 8 EiB, so the loop never runs past "EiB".
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	v, i := float64(n)/unit, 0
	for v >= unit {
		v /= unit
		i++
	}
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0") + " " + units[i]
}
