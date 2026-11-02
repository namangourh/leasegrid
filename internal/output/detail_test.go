package output_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
)

func TestDetailExact(t *testing.T) {
	l := lease.Lease{
		ID:        "1001/1/1",
		Provider:  "demo-a",
		Name:      "web",
		Status:    lease.StatusActive,
		Expires:   tableNow.Add(3*time.Hour + 12*time.Minute + 59*time.Second),
		Cost:      lease.Cost{PerHour: 4990, Denom: "ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1"},
		Resources: lease.Resources{CPUMillis: 500, MemoryBytes: 512 << 20, StorageBytes: 1 << 30},
	}
	want := `ID:         1001/1/1
Provider:   demo-a
Name:       web
Status:     active
Expires:    2026-10-20 15:12 UTC (in 3h12m)
Cost/hour:  4990 ibc/170C677610AC31DF0904FFE09CD3B5C657492170E7E52372E48756B71E56F2F1
CPU:        0.5
Memory:     512 MiB
Storage:    1 GiB
GPUs:       0
`

	var buf bytes.Buffer
	if err := (output.Detail{Now: tableNow}).Render(&buf, l); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("Render() output =\n%s\nwant\n%s", got, want)
	}
}

func TestDetailMissingValues(t *testing.T) {
	// No name, expiry, cost or resources: every one of those lines is "-".
	l := lease.Lease{ID: "1003/1/1", Provider: "demo-a", Status: lease.StatusClosed}
	want := `ID:         1003/1/1
Provider:   demo-a
Name:       -
Status:     closed
Expires:    -
Cost/hour:  -
CPU:        -
Memory:     -
Storage:    -
GPUs:       -
`

	var buf bytes.Buffer
	if err := (output.Detail{Now: tableNow}).Render(&buf, l); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := buf.String(); got != want {
		t.Errorf("Render() output =\n%s\nwant\n%s", got, want)
	}
}

func TestDetailResources(t *testing.T) {
	tests := []struct {
		name      string
		resources lease.Resources
		want      string // the CPU, Memory, Storage and GPUs lines
	}{
		{
			name:      "whole units",
			resources: lease.Resources{CPUMillis: 4000, MemoryBytes: 16 << 30, StorageBytes: 100 << 30, GPUs: 1},
			want:      "CPU:        4\nMemory:     16 GiB\nStorage:    100 GiB\nGPUs:       1\n",
		},
		{
			name:      "fractions",
			resources: lease.Resources{CPUMillis: 250, MemoryBytes: 1536 << 20, StorageBytes: 1<<40 + 1<<39},
			want:      "CPU:        0.25\nMemory:     1.5 GiB\nStorage:    1.5 TiB\nGPUs:       0\n",
		},
		{
			// Some values are set, so a zero is a real zero, not a missing value.
			name:      "small and zero values",
			resources: lease.Resources{CPUMillis: 1, MemoryBytes: 1023, StorageBytes: 0},
			want:      "CPU:        0.001\nMemory:     1023 B\nStorage:    0 B\nGPUs:       0\n",
		},
		{
			name:      "first binary units",
			resources: lease.Resources{CPUMillis: 1000, MemoryBytes: 1 << 10, StorageBytes: 1 << 20},
			want:      "CPU:        1\nMemory:     1 KiB\nStorage:    1 MiB\nGPUs:       0\n",
		},
		{
			name:      "rounded to one decimal place",
			resources: lease.Resources{CPUMillis: 1500, MemoryBytes: 1000 << 20, StorageBytes: 1<<30 + 1<<20},
			want:      "CPU:        1.5\nMemory:     1000 MiB\nStorage:    1 GiB\nGPUs:       0\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lease.Lease{ID: "1", Provider: "a", Status: lease.StatusActive, Resources: tt.resources}
			var buf bytes.Buffer
			if err := (output.Detail{Now: tableNow}).Render(&buf, l); err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got := buf.String(); !strings.HasSuffix(got, tt.want) {
				t.Errorf("Render() output =\n%s\nwant it to end with\n%s", got, tt.want)
			}
		})
	}
}

func TestDetailReplacesControlCharacters(t *testing.T) {
	// The name and the denom come from the provider; either could hold a
	// newline that fakes an extra line or an escape sequence for the terminal.
	l := lease.Lease{
		ID: "1", Provider: "akash", Name: "evil\nGPUs:       99", Status: lease.StatusActive,
		Cost: lease.Cost{PerHour: 1, Denom: "uakt\x1b[2J"},
	}

	var buf bytes.Buffer
	if err := (output.Detail{Now: tableNow}).Render(&buf, l); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "Name:       evil?GPUs:       99\n") || !strings.Contains(got, "Cost/hour:  1 uakt?[2J\n") {
		t.Errorf("Render() output = %q, want the name and denom with control characters replaced", got)
	}
	if strings.Contains(got, "\x1b") || strings.Count(got, "\n") != 10 {
		t.Errorf("Render() output = %q, want no escapes and exactly ten lines", got)
	}
}

func TestDetailReturnsWriterError(t *testing.T) {
	l := lease.Lease{ID: "1", Provider: "a", Status: lease.StatusActive}
	err := (output.Detail{Now: tableNow}).Render(failingWriter{}, l)
	if !errors.Is(err, errWrite) {
		t.Fatalf("Render() error = %v, want %v", err, errWrite)
	}
}
