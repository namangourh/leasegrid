// Package output renders leases for display. Every function writes to an
// io.Writer and returns any write error, so callers can report failures
// such as a closed pipe instead of silently losing output.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/namangourh/leasegrid/internal/lease"
)

// Format names an output format.
type Format string

// Supported formats. Table output comes in a later slice.
const (
	FormatJSON Format = "json"
)

// ErrUnknownFormat is returned for a format name LeaseGrid doesn't support.
var ErrUnknownFormat = errors.New("unknown output format")

// ParseFormat converts a --format flag value into a Format.
func ParseFormat(s string) (Format, error) {
	switch f := Format(s); f {
	case FormatJSON:
		return f, nil
	default:
		return "", fmt.Errorf("%w %q (supported: json)", ErrUnknownFormat, s)
	}
}

// Write renders leases to w in format f.
func Write(w io.Writer, f Format, leases []lease.Lease) error {
	switch f {
	case FormatJSON:
		return writeJSON(w, leases)
	default:
		return fmt.Errorf("%w %q", ErrUnknownFormat, f)
	}
}

// writeJSON writes leases as an indented JSON array followed by a newline.
// A nil slice is written as [] rather than null, which JSON consumers expect.
func writeJSON(w io.Writer, leases []lease.Lease) error {
	if leases == nil {
		leases = []lease.Lease{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(leases)
}
