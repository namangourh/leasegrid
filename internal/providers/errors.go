// Package providers holds what every provider implementation shares: the
// error categories callers use to tell failures apart with errors.Is. The
// Provider interface itself belongs to its consumer, internal/app.
package providers

import "errors"

// Error categories. A provider wraps each failure with exactly one of them.
// Context cancellation and deadline errors are returned as they are, so
// errors.Is(err, context.DeadlineExceeded) keeps working.
var (
	// ErrUnauthorized means the provider refused access (HTTP 401 or 403).
	ErrUnauthorized = errors.New("access denied")

	// ErrNotFound means the endpoint doesn't exist (HTTP 404) or no longer
	// serves this API version (HTTP 501). Check the base URL and API version.
	ErrNotFound = errors.New("endpoint not found")

	// ErrRateLimited means too many requests (HTTP 429). Try again later.
	ErrRateLimited = errors.New("rate limited")

	// ErrUnavailable means the provider is down or unreachable (other HTTP
	// 5xx responses, connection failures).
	ErrUnavailable = errors.New("provider unavailable")

	// ErrBadResponse means the provider answered with something LeaseGrid
	// can't use: malformed JSON, missing or invalid fields, an unexpected
	// status code.
	ErrBadResponse = errors.New("bad response")
)
