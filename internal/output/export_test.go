package output

import "io"

// ColorEnabled lets the tests in package output_test check ColorMode.enabled.
// Files ending in _test.go are compiled only by go test, so it is never part
// of the real package.
func ColorEnabled(m ColorMode, w io.Writer) bool { return m.enabled(w) }
