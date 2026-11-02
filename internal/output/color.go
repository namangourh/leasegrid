package output

import (
	"io"
	"os"
	"time"

	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/summary"
)

// ColorMode says whether the status dashboard's text view may use color.
// JSON never has color, whatever the mode.
type ColorMode int

const (
	// ColorAuto colors output only when it goes to a terminal and the
	// NO_COLOR environment variable is empty or unset. It is the zero value,
	// so output to a file, a pipe or a test buffer stays plain.
	ColorAuto ColorMode = iota

	// ColorAlways colors output wherever it goes.
	ColorAlways

	// ColorNever never colors output.
	ColorNever
)

// enabled reports whether output written to w should be colored.
func (m ColorMode) enabled(w io.Writer) bool {
	switch m {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}
	// See https://no-color.org: any non-empty value turns color off.
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	// A terminal is a character device; files and pipes aren't.
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// ANSI escape codes for the text view's colors. Every color code is five
// bytes long: tabwriter counts the codes as part of a cell's width, so a
// column stays aligned only if every cell in it, the header included, is
// wrapped in a code of the same length.
const (
	ansiReset   = "\x1b[0m"
	ansiDefault = "\x1b[39m" // the terminal's normal color
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiRed     = "\x1b[31m"
	ansiGray    = "\x1b[90m"
)

// statusColor returns the color for a lease status: green for active, red
// for insufficient_funds, gray for closed.
func statusColor(s lease.Status) string {
	switch s {
	case lease.StatusActive:
		return ansiGreen
	case lease.StatusInsufficientFunds:
		return ansiRed
	case lease.StatusClosed:
		return ansiGray
	default:
		return ansiDefault
	}
}

// expiryColor returns the color for an active lease's expiry: red once it
// has passed, yellow while it is expiring soon. Other expiries keep the
// normal color: a closed lease's old expiry is no cause for alarm.
func expiryColor(l lease.Lease, now time.Time, window time.Duration) string {
	switch {
	case l.Status != lease.StatusActive || l.Expires.IsZero():
		return ansiDefault
	case !l.Expires.After(now):
		return ansiRed
	case summary.ExpiringSoon(l, now, window):
		return ansiYellow
	default:
		return ansiDefault
	}
}
