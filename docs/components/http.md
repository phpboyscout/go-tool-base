---
title: HTTP
description: Secure-by-default HTTP server and client components.
date: 2026-03-24
tags: [components, http, networking, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# HTTP

The `pkg/http` package provides hardened HTTP components for both server-side and client-side operations. It enforces secure TLS defaults, provides built-in observability endpoints, and mirrors the security posture required for production environments.

## Server Control

The HTTP server implementation integrates seamlessly with the `controls` lifecycle management.

### Features

- **Standardized Endpoints**: Automatically mounts `/healthz`, `/livez`, and `/readyz`.
- **Production Timeouts**: Pre-configured Read (5s), Write (10s), and Idle (120s) timeouts.
- **Secure TLS**: Enforces TLS 1.2 minimum with curated AEAD-based cipher suites and X25519 preference.

### Functions

- **`NewServer(ctx context.Context, cfg config.Containable, handler http.Handler) (*http.Server, error)`**: Returns a pre-configured `*http.Server`.
- **`Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, handler http.Handler, opts ...RegisterOption) (*http.Server, error)`**: Creates, configures, and registers the server with a `Controller`. Health endpoints (`/healthz`, `/livez`, `/readyz`) are mounted outside any middleware chain.

### Middleware Chaining

The package provides an alice-style middleware chaining API. Middleware uses the standard `func(http.Handler) http.Handler` signature.

- **`NewChain(middlewares ...Middleware) Chain`**: Creates a middleware chain. The first middleware is the outermost wrapper.
- **`(c Chain) Append(middlewares ...Middleware) Chain`**: Returns a new chain with additional middleware appended (immutable).
- **`(c Chain) Extend(other Chain) Chain`**: Composes two chains.
- **`(c Chain) Then(handler http.Handler) http.Handler`**: Applies the chain to a handler.
- **`(c Chain) ThenFunc(fn http.HandlerFunc) http.Handler`**: Convenience for `Then(http.HandlerFunc(fn))`.
- **`WithMiddleware(chain Chain) RegisterOption`**: Applies a middleware chain when using `Register`. Health endpoints are unaffected.

### Built-in Logging Middleware

`LoggingMiddleware` logs each completed HTTP request with structured fields (method, path, status, latency, bytes, client IP, user agent).

- **`LoggingMiddleware(logger logger.Logger, opts ...LoggingOption) Middleware`**

**Format options** (`WithFormat`):

| Format | Description |
|--------|-------------|
| `FormatStructured` (default) | Structured key-value fields via `logger.Logger` |
| `FormatCommon` | NCSA Common Log Format |
| `FormatCombined` | NCSA Combined Log Format (CLF + Referer + User-Agent) |
| `FormatJSON` | Single JSON object per request |

**Other options**: `WithLogLevel`, `WithoutLatency`, `WithoutUserAgent`, `WithPathFilter`, `WithHeaderFields`.

### Usage Example

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/data", myDataHandler)

// Build a middleware chain
chain := gtbhttp.NewChain(
    gtbhttp.LoggingMiddleware(props.Logger,
        gtbhttp.WithFormat(gtbhttp.FormatCombined),
        gtbhttp.WithPathFilter("/healthz", "/livez", "/readyz"),
    ),
)

// Register with middleware — health endpoints stay outside the chain
srv, err := gtbhttp.Register(ctx, "http-api", controller, props.Config, props.Logger, mux,
    gtbhttp.WithMiddleware(chain),
)
```

## Client Factory

The `pkg/http` package provides a factory for creating hardened `http.Client` instances for outbound requests.

### Features

- **Mandatory Timeouts**: Default 30s timeout to prevent blocked goroutines.
- **Secure Transport**: Uses the same hardened TLS configuration as the server.
- **Scheme Protection**: Redirect policy rejects HTTPS-to-HTTP downgrades.
- **Connection Limits**: Pre-configured idle connection pooling and timeouts.

### Functions

- **`NewClient(opts ...ClientOption) *http.Client`**: Returns a hardened HTTP client.
- **`NewTransport(tlsCfg *tls.Config) *http.Transport`**: Returns a pre-configured secure transport for custom client needs.

### Options

- `WithTimeout(d time.Duration)`
- `WithMaxRedirects(n int)`
- `WithTLSConfig(cfg *tls.Config)`
- `WithTransport(rt http.RoundTripper)`
- `WithRetry(cfg RetryConfig)` — enables automatic retry with exponential backoff

### Retry with Exponential Backoff

The client supports opt-in retry for transient failures via `WithRetry`. Retry is implemented as a `http.RoundTripper` decorator, so it composes cleanly with custom transports set via `WithTransport`.

**`RetryConfig` fields:**

| Field | Default | Description |
|-------|---------|-------------|
| `MaxRetries` | 3 | Maximum number of retry attempts (0 = no retries) |
| `InitialBackoff` | 500ms | Base delay before the first retry |
| `MaxBackoff` | 30s | Cap on computed delay |
| `RetryableStatusCodes` | 429, 502, 503, 504 | HTTP status codes that trigger a retry |
| `ShouldRetry` | nil | Optional custom predicate replacing default logic |

**Backoff strategy**: Full jitter — `uniform random in [0, min(cap, base × 2^attempt)]`. This reduces thundering-herd effects compared to fixed or equal-jitter backoff.

**Retry-After support**: When a 429 or 503 response includes a `Retry-After` header (seconds or HTTP-date), that value is used as the delay instead of the computed backoff.

**Body rewind**: Request bodies are rewound via `GetBody` between attempts. Response bodies from failed attempts are drained and closed to allow connection reuse.

**Context cancellation**: If the request context is cancelled during a backoff wait, the retry loop exits immediately with the context error.

### Usage Example

```go
// Simple secure client
client := http.NewClient()

// Client with automatic retry for transient failures
client := http.NewClient(
    http.WithTimeout(60*time.Second),
    http.WithRetry(http.DefaultRetryConfig()),
)

// Custom retry configuration
client := http.NewClient(
    http.WithRetry(http.RetryConfig{
        MaxRetries:           5,
        InitialBackoff:       200 * time.Millisecond,
        MaxBackoff:           10 * time.Second,
        RetryableStatusCodes: []int{429, 502, 503, 504},
    }),
)

// Custom retry predicate
client := http.NewClient(
    http.WithRetry(http.RetryConfig{
        MaxRetries:     3,
        InitialBackoff: 500 * time.Millisecond,
        MaxBackoff:     30 * time.Second,
        ShouldRetry: func(attempt int, resp *http.Response, err error) bool {
            if err != nil {
                return true // retry all errors
            }
            return resp != nil && resp.StatusCode >= 500
        },
    }),
)

// Power user: custom client with secure transport
customClient := &http.Client{
    Transport: http.NewTransport(nil),
}
```
