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

### Usage Example

```go
// Simple secure client
client := http.NewClient()

// Custom secure client for an SDK
githubClient := github.NewClient(http.NewClient(http.WithTimeout(10 * time.Second)))

// Power user: custom client with secure transport
customClient := &http.Client{
    Transport: http.NewTransport(nil),
}
```
