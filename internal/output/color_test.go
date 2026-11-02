package output_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/output"
)

// closeAtEnd closes c when the test ends. Nothing was written that a failed
// close could lose, so the error is ignored.
func closeAtEnd(t *testing.T, c io.Closer) {
	t.Cleanup(func() { _ = c.Close() })
}

func TestColorModeEnabled(t *testing.T) {
	// os.DevNull is a character device on Linux, macOS and Windows, so it
	// stands in for a terminal.
	terminal, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	closeAtEnd(t, terminal)
	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	closeAtEnd(t, file)
	pr, pipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	closeAtEnd(t, pr)
	closeAtEnd(t, pipe)

	tests := []struct {
		name    string
		mode    output.ColorMode
		w       io.Writer
		noColor string // the NO_COLOR environment variable
		want    bool
	}{
		{name: "auto, terminal", mode: output.ColorAuto, w: terminal, want: true},
		{name: "auto, terminal, NO_COLOR set", mode: output.ColorAuto, w: terminal, noColor: "1", want: false},
		{name: "auto, file", mode: output.ColorAuto, w: file, want: false},
		{name: "auto, pipe", mode: output.ColorAuto, w: pipe, want: false},
		{name: "auto, buffer", mode: output.ColorAuto, w: &bytes.Buffer{}, want: false},
		{name: "never, terminal", mode: output.ColorNever, w: terminal, want: false},
		{name: "always, buffer", mode: output.ColorAlways, w: &bytes.Buffer{}, want: true},
		// Choosing color explicitly overrides NO_COLOR, as no-color.org advises.
		{name: "always, NO_COLOR set", mode: output.ColorAlways, w: terminal, noColor: "1", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// An empty NO_COLOR counts as unset, so "" also checks that rule.
			t.Setenv("NO_COLOR", tt.noColor)
			if got := output.ColorEnabled(tt.mode, tt.w); got != tt.want {
				t.Errorf("ColorEnabled(%v) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

// ansiCodes matches the color escape sequences the text view writes.
var ansiCodes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// renderStatus writes st as text with the given color mode.
func renderStatus(t *testing.T, st output.Status, mode output.ColorMode) string {
	t.Helper()
	st.Color = mode
	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatTable, st); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	return buf.String()
}

func TestStatusTextColor(t *testing.T) {
	st := statusDemo()
	st.Leases = append([]lease.Lease{
		{ID: "3001/1/1", Provider: "demo-b", Name: "old", Status: lease.StatusActive, Expires: tableNow.Add(-time.Hour)},
		{ID: "3002/1/1", Provider: "demo-b", Name: "new", Status: lease.StatusActive},
	}, st.Leases...)

	plain := renderStatus(t, st, output.ColorNever)
	colored := renderStatus(t, st, output.ColorAlways)

	if strings.Contains(plain, "\x1b") {
		t.Errorf("ColorNever output = %q, want no escape codes", plain)
	}
	// Without its codes, the colored dashboard is exactly the plain one, so
	// the columns still line up and nothing but color was added.
	if got := ansiCodes.ReplaceAllString(colored, ""); got != plain {
		t.Errorf("colored output without its codes =\n%s\nwant\n%s", got, plain)
	}

	// The STATUS and EXPIRES cells of each row, found by the NAME column.
	rows := []struct{ name, status, expires string }{
		{"NAME", "\x1b[39mSTATUS\x1b[0m", "\x1b[39mEXPIRES\x1b[0m"}, // the header, so columns align
		{"old", "\x1b[32mactive\x1b[0m", "\x1b[31m2026-10-20 11:00 UTC (expired 1h0m ago)\x1b[0m"},
		{"new", "\x1b[32mactive\x1b[0m", "\x1b[39m-\x1b[0m"}, // unknown expiry: nothing to warn about
		{"gpu", "\x1b[32mactive\x1b[0m", "\x1b[33m2026-10-20 12:45 UTC (in 45m)\x1b[0m"},
		{"web", "\x1b[32mactive\x1b[0m", "\x1b[33m2026-10-20 15:12 UTC (in 3h12m)\x1b[0m"},
		{"api", "\x1b[32mactive\x1b[0m", "\x1b[39m2026-10-30 16:00 UTC (in 10d4h)\x1b[0m"}, // after the window
		// Only active leases' expiries are colored.
		{"worker", "\x1b[31minsufficient_funds\x1b[0m", "\x1b[39m2026-10-20 10:00 UTC (expired 2h0m ago)\x1b[0m"},
		{"batch", "\x1b[90mclosed\x1b[0m", "\x1b[39m-\x1b[0m"},
	}
	lines := strings.Split(colored, "\n")
	for _, r := range rows {
		i := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "  "+r.name+"  ") })
		if i < 0 {
			t.Errorf("colored output = %q, want a row named %q", colored, r.name)
			continue
		}
		if !strings.Contains(lines[i], r.status) || !strings.Contains(lines[i], r.expires) {
			t.Errorf("row %q = %q, want it to contain %q and %q", r.name, lines[i], r.status, r.expires)
		}
	}

	// Only the table is colored: the summary above it stays plain.
	summaryLines, _, _ := strings.Cut(colored, "\n\n")
	if strings.Contains(summaryLines, "\x1b") {
		t.Errorf("summary = %q, want no escape codes", summaryLines)
	}
}

func TestStatusTextColorUsesWindow(t *testing.T) {
	// With a 1-hour window, the lease 3h12m away is no longer expiring soon.
	st := statusDemo()
	st.Window = time.Hour
	colored := renderStatus(t, st, output.ColorAlways)

	for _, want := range []string{
		"\x1b[33m2026-10-20 12:45 UTC (in 45m)\x1b[0m",
		"\x1b[39m2026-10-20 15:12 UTC (in 3h12m)\x1b[0m",
	} {
		if !strings.Contains(colored, want) {
			t.Errorf("colored output = %q, want it to contain %q", colored, want)
		}
	}
}

func TestStatusTextColorUnknownStatus(t *testing.T) {
	// lease.Validate rejects other statuses, but the table must not invent
	// a meaning for one: it keeps the normal color.
	st := statusDemo()
	st.Leases = []lease.Lease{{ID: "1", Provider: "a", Status: "pending", Expires: tableNow.Add(time.Hour)}}
	colored := renderStatus(t, st, output.ColorAlways)

	for _, want := range []string{"\x1b[39mpending\x1b[0m", "\x1b[39m2026-10-20 13:00 UTC (in 1h0m)\x1b[0m"} {
		if !strings.Contains(colored, want) {
			t.Errorf("colored output = %q, want it to contain %q", colored, want)
		}
	}
}

func TestStatusJSONNeverColored(t *testing.T) {
	st := statusDemo()
	st.Color = output.ColorAlways
	var buf bytes.Buffer
	if err := output.WriteStatus(&buf, output.FormatJSON, st); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	if strings.Contains(buf.String(), "\x1b") || strings.Contains(buf.String(), `\u001b`) {
		t.Errorf("WriteStatus(json) output = %q, want no escape codes", buf.String())
	}
}
