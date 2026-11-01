package output_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
)

func TestYAMLExact(t *testing.T) {
	leases := []lease.Lease{
		{
			ID:        "1001/1/1",
			Provider:  "akash",
			Name:      "web",
			Status:    lease.StatusActive,
			Expires:   time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC),
			Cost:      lease.Cost{PerHour: 3000, Denom: "uakt"},
			Resources: lease.Resources{CPUMillis: 500, MemoryBytes: 536870912, StorageBytes: 1073741824},
		},
		// Unknown expiry and no cost or resources, so those keys are left
		// out. Unquoted, this ID and name would read as a number and a boolean.
		{ID: "1001", Provider: "akash", Name: "true", Status: lease.StatusClosed},
	}
	want := `- id: 1001/1/1
  provider: akash
  name: web
  status: active
  expires: 2026-11-14T12:00:00Z
  cost:
    per_hour: 3000
    denom: uakt
  resources:
    cpu_millis: 500
    memory_bytes: 536870912
    storage_bytes: 1073741824
    gpus: 0
- id: "1001"
  provider: akash
  name: "true"
  status: closed
`
	var buf bytes.Buffer
	if err := (output.YAML{}).Render(&buf, leases); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("Render() output =\n%s\nwant\n%s", got, want)
	}
}

// TestYAMLRoundTrip checks that YAML output decodes back into the same
// leases, including text that YAML could mistake for another type and text
// with control characters, which must be escaped rather than written raw.
func TestYAMLRoundTrip(t *testing.T) {
	want := []lease.Lease{
		{
			ID:       "1001/1/1",
			Provider: "akash",
			Name:     "web",
			Status:   lease.StatusActive,
			Expires:  time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC),
			Cost:     lease.Cost{PerHour: 4990, Denom: "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"},
		},
		{ID: "1002", Provider: "akash", Name: "no", Status: lease.StatusClosed},
		{ID: "x", Provider: "akash", Name: "evil\x1b[31m\nname", Status: lease.StatusActive},
	}

	var buf bytes.Buffer
	if err := (output.YAML{}).Render(&buf, want); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.ContainsRune(buf.String(), '\x1b') {
		t.Errorf("output contains a raw escape character:\n%q", buf.String())
	}

	var got []lease.Lease
	if err := yaml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, buf.String())
	}
	if !slices.Equal(got, want) {
		t.Errorf("decoded = %v, want %v", got, want)
	}
}

func TestYAMLEmpty(t *testing.T) {
	for _, leases := range [][]lease.Lease{nil, {}} {
		var buf bytes.Buffer
		if err := (output.YAML{}).Render(&buf, leases); err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		if got := buf.String(); got != "[]\n" {
			t.Errorf("Render(%v) = %q, want %q", leases, got, "[]\n")
		}
	}
}
