---
title: "HTTP Retry with Exponential Backoff Specification"
description: "Add a retry mechanism with exponential backoff and jitter to pkg/http for transient failure recovery, configurable via ClientOption."
date: 2026-03-26
status: IMPLEMENTED
tags:
  - specification
  - http
  - retry
  - resilience
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# HTTP Retry with Exponential Backoff Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   26 March 2026

Status
:   IMPLEMENTED

---

## Overview

The `pkg/http` package provides a secure HTTP client with sensible defaults (TLS 1.2+, redirect policy, timeouts) but has no retry mechanism. When the update command runs behind corporate proxies or over unreliable networks, transient failures such as `context deadline exceeded`, HTTP 429, 502, 503, and 504 responses cause immediate failure with no recovery attempt.

This specification adds an opt-in retry mechanism with exponential backoff, jitter, configurable retry limits, and context-aware cancellation. The mechanism is implemented as a `http.RoundTripper` decorator so it composes cleanly with the existing `ClientOption` pattern and works transparently for all HTTP requests made through the client.

---

## Design Decisions

**RoundTripper decorator**: Implement retry as a wrapping `http.RoundTripper` rather than patching `NewClient` internals. This keeps the retry logic isolated, testable, and composable with custom transports set via `WithTransport`.

**Opt-in via ClientOption**: Retry is not enabled by default. Consumers who need it use `WithRetry(...)`. This avoids unexpected behaviour changes for existing callers and prevents double-retries when callers already have their own retry logic.

**Exponential backoff with full jitter**: Use the "full jitter" strategy (`[0, min(cap, base * 2^attempt)]`) as recommended by AWS architecture best practices. Full jitter reduces thundering-herd effects compared to equal jitter or no jitter.

**Retryable conditions are configurable**: The default set of retryable status codes (429, 502, 503, 504) and network errors (timeouts, connection resets) covers common transient failures. Consumers can override this with a custom `RetryPolicy` function.

**Request body buffering**: For requests with a body, the body must be re-readable across retries. The retry transport buffers the body on first read and resets it for each attempt. Requests with `GetBody` set (as produced by `http.NewRequest`) are handled natively.

**Respect Retry-After header**: When a 429 or 503 response includes a `Retry-After` header, use that value as the delay floor for the next attempt rather than the computed backoff.

---

## Public API Changes

### New ClientOption: `WithRetry`

```go
// RetryConfig configures the retry behaviour of the HTTP client.
type RetryConfig struct {
    // MaxRetries is the maximum number of retry attempts. Zero means no retries.
    MaxRetries int
    // InitialBackoff is the base delay before the first retry. Default: 500ms.
    InitialBackoff time.Duration
    // MaxBackoff caps the computed delay. Default: 30s.
    MaxBackoff time.Duration
    // RetryableStatusCodes defines which HTTP status codes trigger a retry.
    // Default: []int{429, 502, 503, 504}.
    RetryableStatusCodes []int
    // ShouldRetry is an optional custom predicate. When set, it replaces the
    // default status-code and network-error checks. The attempt count (0-based)
    // and either the response or the transport error are provided.
    ShouldRetry func(attempt int, resp *http.Response, err error) bool
}

// WithRetry enables automatic retry with exponential backoff for transient failures.
func WithRetry(cfg RetryConfig) ClientOption
```

### Default Retry Configuration

```go
// DefaultRetryConfig returns a RetryConfig suitable for most use cases.
func DefaultRetryConfig() RetryConfig {
    return RetryConfig{
        MaxRetries:           3,
        InitialBackoff:       500 * time.Millisecond,
        MaxBackoff:           30 * time.Second,
        RetryableStatusCodes: []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
    }
}
```

### Usage

```go
client := http.NewClient(
    http.WithTimeout(60*time.Second),
    http.WithRetry(http.DefaultRetryConfig()),
)
```

---

## Internal Implementation

### retryTransport

```go
// retryTransport wraps an http.RoundTripper with retry logic.
type retryTransport struct {
    next http.RoundTripper
    cfg  RetryConfig
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    var (
        resp *http.Response
        err  error
    )

    for attempt := range t.cfg.MaxRetries + 1 {
        if attempt > 0 {
            delay := t.computeDelay(attempt, resp)

            select {
            case <-req.Context().Done():
                return nil, req.Context().Err()
            case <-time.After(delay):
            }

            // Reset request body for retry
            if req.GetBody != nil {
                req.Body, err = req.GetBody()
                if err != nil {
                    return nil, errors.Wrap(err, "failed to reset request body for retry")
                }
            }
        }

        resp, err = t.next.RoundTrip(req)
        if !t.shouldRetry(attempt, resp, err) {
            break
        }

        // Drain and close response body before retry to reuse connection
        if resp != nil {
            _, _ = io.Copy(io.Discard, resp.Body)
            _ = resp.Body.Close()
        }
    }

    return resp, err
}
```

### Backoff Computation

```go
func (t *retryTransport) computeDelay(attempt int, resp *http.Response) time.Duration {
    // Check Retry-After header
    if resp != nil {
        if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 {
            return ra
        }
    }

    // Exponential backoff: base * 2^attempt
    backoff := t.cfg.InitialBackoff * (1 << uint(attempt-1))
    if backoff > t.cfg.MaxBackoff {
        backoff = t.cfg.MaxBackoff
    }

    // Full jitter: uniform random in [0, backoff]
    jitter := time.Duration(rand.Int64N(int64(backoff) + 1))

    return jitter
}
```

### Integration with NewClient

The `WithRetry` option stores the `RetryConfig` on `clientConfig`. In `NewClient`, after the base transport is resolved, the retry transport wraps it:

```go
func NewClient(opts ...ClientOption) *http.Client {
    cfg := defaultClientConfig()
    for _, opt := range opts {
        opt(cfg)
    }

    var transport http.RoundTripper
    if cfg.transport != nil {
        transport = cfg.transport
    } else {
        transport = NewTransport(cfg.tlsConfig)
    }

    if cfg.retry != nil {
        transport = &retryTransport{next: transport, cfg: *cfg.retry}
    }

    return &http.Client{
        Timeout:       cfg.timeout,
        Transport:     transport,
        CheckRedirect: redirectPolicy(cfg.maxRedirects),
    }
}
```

---

## Project Structure

```
pkg/http/
├── client.go          <- MODIFIED: RetryConfig on clientConfig, WithRetry option, wrapping in NewClient
├── retry.go           <- NEW: retryTransport, computeDelay, parseRetryAfter, default shouldRetry
├── retry_test.go      <- NEW: retry tests
├── tls.go             <- UNCHANGED
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestRetryTransport_NoRetryOnSuccess` | 200 response on first attempt, no retry |
| `TestRetryTransport_RetriesOn503` | 503 then 200, verifies single retry and success |
| `TestRetryTransport_RetriesOn429WithRetryAfter` | 429 with Retry-After header, verifies delay respects header |
| `TestRetryTransport_ExhaustsMaxRetries` | 503 on all attempts, returns final 503 response |
| `TestRetryTransport_ContextCancelled` | Context cancelled during backoff wait, returns context error |
| `TestRetryTransport_NetworkError` | Connection reset on first attempt, retries and succeeds |
| `TestRetryTransport_BodyRewind` | POST with body, retried, body correctly re-sent |
| `TestRetryTransport_NoRetryOn4xx` | 400/401/403/404 not retried |
| `TestRetryTransport_CustomShouldRetry` | Custom predicate overrides default logic |
| `TestRetryTransport_BackoffJitter` | Verify delay is within expected bounds across multiple attempts |
| `TestRetryTransport_MaxBackoffCap` | High attempt count does not exceed MaxBackoff |
| `TestDefaultRetryConfig` | Default values are correct |
| `TestWithRetry_Integration` | NewClient with WithRetry, full round-trip through httptest.Server |

### Test Helpers

```go
// countingHandler returns an http.Handler that responds with the given status
// codes in sequence, then 200 for all subsequent requests.
func countingHandler(t *testing.T, statusCodes ...int) http.Handler
```

### Coverage

- Target: 90%+ for `pkg/http/` including retry paths.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for `RetryConfig`, `WithRetry`, and `DefaultRetryConfig`.
- Godoc on `retryTransport` explaining the wrapping strategy and body-rewind behaviour.
- Update `docs/components/http.md` (if it exists) with retry usage examples.

---

## Backwards Compatibility

- **No breaking changes**. Retry is opt-in via `WithRetry`. Existing callers that do not use this option see identical behaviour.
- `WithRetry` composes with `WithTransport` -- the retry transport wraps whatever base transport is configured.

---

## Future Considerations

- **Circuit breaker**: A circuit breaker could wrap the retry transport to fail-fast when a downstream service is consistently unavailable, avoiding wasted retry attempts.
- **Per-request retry override**: Allow individual requests to opt out of retry via a context value.
- **Metrics/observability**: Emit retry attempt counts and delays for monitoring integration.

---

## Implementation Phases

### Phase 1 -- Types and Configuration
1. Add `RetryConfig` struct and `DefaultRetryConfig()` to `pkg/http/retry.go`
2. Add `WithRetry` ClientOption
3. Add `retry *RetryConfig` field to `clientConfig`

### Phase 2 -- Retry Transport
1. Implement `retryTransport` with `RoundTrip`
2. Implement `computeDelay` with exponential backoff and full jitter
3. Implement `parseRetryAfter` for Retry-After header support
4. Implement default `shouldRetry` checking status codes and network errors
5. Wire retry transport wrapping into `NewClient`

### Phase 3 -- Tests
1. Create `countingHandler` test helper
2. Add unit tests for `retryTransport` covering all scenarios
3. Add integration test via `httptest.Server`
4. Run with race detector

---

## Verification

```bash
go build ./...
go test -race ./pkg/http/...
go test ./...
golangci-lint run --fix

# Verify retry transport exists
grep -n 'retryTransport' pkg/http/retry.go

# Verify WithRetry option
grep -n 'WithRetry' pkg/http/client.go
```
