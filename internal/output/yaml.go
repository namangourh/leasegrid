package output

import (
	"bytes"
	"io"

	"go.yaml.in/yaml/v3"

	"github.com/namangourh/leasegrid/internal/lease"
)

// YAML renders leases as YAML, for people and tools that prefer it to JSON.
// Field names are the same as in the JSON output.
type YAML struct{}

// Render writes leases as a YAML list. A nil or empty list is written as [],
// the same as the JSON output. Unlike encoding/json, the yaml package already
// writes a nil slice as [], so no conversion is needed.
//
// The document is built in memory and written with a single Write, so a
// failing writer never leaves half a document behind.
func (YAML) Render(w io.Writer, leases []lease.Lease) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(leases); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	_, err := w.Write(buf.Bytes())
	return err
}
