package akash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/namangourh/leasegrid/internal/providers"
)

// getJSON sends GET path?q and decodes the JSON response into out. what names
// the request in errors, e.g. "lease list".
//
// Every failure wraps a providers category, or the context's error. Errors
// never include the request URL (other APIs carry keys in query strings) or
// the response body (it could be huge, or hostile).
func (p *Provider) getJSON(ctx context.Context, what, path string, q url.Values, out any) error {
	u := *p.baseURL // a copy: the shared base URL is never modified
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("%s: build request: %w", what, redactURL(err))
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s: %w", what, ctxErr) // timed out or cancelled
		}
		return fmt.Errorf("%w: %s: %v", providers.ErrUnavailable, what, redactURL(err))
	}
	// Closing the body releases the connection. Nothing useful can be done
	// if closing fails, so the error is discarded.
	defer func() { _ = resp.Body.Close() }()

	if category := statusCategory(resp.StatusCode); category != nil {
		return fmt.Errorf("%w: %s returned HTTP %d", category, what, resp.StatusCode)
	}

	// Read one byte more than allowed, to detect an oversized body without
	// reading all of it.
	data, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s: %w", what, ctxErr)
		}
		return fmt.Errorf("%w: %s: read body: %v", providers.ErrUnavailable, what, err)
	}
	if int64(len(data)) > p.maxBytes {
		return fmt.Errorf("%w: %s: response larger than %d bytes", providers.ErrBadResponse, what, p.maxBytes)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %s: invalid JSON: %v", providers.ErrBadResponse, what, err)
	}
	return nil
}

// statusCategory maps an HTTP status code to a providers error category, or
// nil for success.
func statusCategory(code int) error {
	switch {
	case code >= 200 && code <= 299:
		return nil
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		return providers.ErrUnauthorized
	case code == http.StatusNotFound, code == http.StatusNotImplemented:
		return providers.ErrNotFound
	case code == http.StatusTooManyRequests:
		return providers.ErrRateLimited
	case code >= 500:
		return providers.ErrUnavailable
	default:
		return providers.ErrBadResponse
	}
}

// redactURL removes the request URL from a transport error. *url.Error
// formats as `Get "https://...?query": cause`; only the cause is kept.
func redactURL(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err
	}
	return err
}
