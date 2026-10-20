package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    output.Format
		wantErr error
	}{
		{in: "json", want: output.FormatJSON},
		{in: "yaml", wantErr: output.ErrUnknownFormat},
		{in: "JSON", wantErr: output.ErrUnknownFormat}, // names are case-sensitive
		{in: "", wantErr: output.ErrUnknownFormat},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := output.ParseFormat(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseFormat(%q) error = %v, want %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWriteJSONExact(t *testing.T) {
	leases := []lease.Lease{{ID: "a1", Provider: "akash", Name: "web", Status: lease.StatusActive}}
	want := `[
  {
    "id": "a1",
    "provider": "akash",
    "name": "web",
    "status": "active"
  }
]
`
	var buf bytes.Buffer
	if err := output.Write(&buf, output.FormatJSON, leases); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("Write() output =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		leases []lease.Lease
		want   []lease.Lease
	}{
		{
			name: "two leases",
			leases: []lease.Lease{
				{ID: "a1", Provider: "akash", Name: "web", Status: lease.StatusActive},
				{ID: "a2", Provider: "akash", Name: "db", Status: lease.StatusClosed},
			},
			want: []lease.Lease{
				{ID: "a1", Provider: "akash", Name: "web", Status: lease.StatusActive},
				{ID: "a2", Provider: "akash", Name: "db", Status: lease.StatusClosed},
			},
		},
		{name: "nil becomes empty array", leases: nil, want: []lease.Lease{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := output.Write(&buf, output.FormatJSON, tt.leases); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			var got []lease.Lease
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
			}
			if got == nil {
				t.Fatalf("output decoded to null, want an array:\n%s", buf.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("decoded = %v, want %v", got, tt.want)
			}
		})
	}
}

// failingWriter simulates a closed pipe or full disk.
type failingWriter struct{}

var errWrite = errors.New("write failed")

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestWriteReturnsWriterError(t *testing.T) {
	err := output.Write(failingWriter{}, output.FormatJSON, nil)
	if !errors.Is(err, errWrite) {
		t.Fatalf("Write() error = %v, want %v", err, errWrite)
	}
}

func TestWriteUnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := output.Write(&buf, output.Format("yaml"), nil)
	if !errors.Is(err, output.ErrUnknownFormat) {
		t.Fatalf("Write() error = %v, want %v", err, output.ErrUnknownFormat)
	}
	if buf.Len() != 0 {
		t.Errorf("Write() wrote %q for an unknown format, want nothing", buf.String())
	}
}
