// Package output renders leases for display. Every function writes to an
// io.Writer and returns any write error, so callers can report failures
// such as a closed pipe instead of silently losing output.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Format names an output format.
type Format string

// Supported formats.
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// ErrUnknownFormat is returned for a format name LeaseGrid doesn't support.
var ErrUnknownFormat = errors.New("unknown output format")

// Formatter renders leases in one output format.
type Formatter interface {
	Render(w io.Writer, leases []lease.Lease) error
}

// New returns the Formatter for f. now is the time the table shows expiries
// relative to; other formats ignore it.
func New(f Format, now time.Time) (Formatter, error) {
	switch f {
	case FormatTable:
		return Table{Now: now}, nil
	case FormatJSON:
		return JSON{}, nil
	default:
		return nil, fmt.Errorf("%w %q", ErrUnknownFormat, f)
	}
}

// ParseFormat converts a --format flag value into a Format.
func ParseFormat(s string) (Format, error) {
	f := Format(s)
	if _, err := New(f, time.Time{}); err != nil {
		return "", fmt.Errorf("%w (supported: table, json)", err)
	}
	return f, nil
}

// Write renders leases to w in format f, with now as the table's current
// time. It is shorthand for New followed by Render.
func Write(w io.Writer, f Format, now time.Time, leases []lease.Lease) error {
	formatter, err := New(f, now)
	if err != nil {
		return err
	}
	return formatter.Render(w, leases)
}

// JSON renders leases as JSON, for scripts.
type JSON struct{}

// Render writes leases as an indented JSON array followed by a newline.
// A nil slice is written as [] rather than null, which JSON consumers expect.
func (JSON) Render(w io.Writer, leases []lease.Lease) error {
	if leases == nil {
		leases = []lease.Lease{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(leases)
}
