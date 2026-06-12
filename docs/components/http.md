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

### TLS Configuration

TLS configuration cascades — transport-specific keys override the shared defaults:

| Key | Shared Default | HTTP Override |
|-----|---------------|--------------|
| Enabled | `server.tls.enabled` | `server.http.tls.enabled` |
| Certificate | `server.tls.cert` | `server.http.tls.cert` |
| Private key | `server.tls.key` | `server.http.tls.key` |

To use the same certificate for both HTTP and gRPC, configure the shared keys only:

```yaml
server:
  tls:
    enabled: true
    cert: /etc/certs/server.crt
    key: /etc/certs/server.key
```

When TLS is enabled, the server uses `ServeTLS` with the shared hardened config from [`pkg/tls`](tls.md) (TLS 1.2+, curated AEAD ciphers, X25519). When disabled, it uses plain `Serve`.

TLS configuration and resolution live in [`pkg/tls`](tls.md) (`gtbtls.DefaultConfig`, `gtbtls.Resolve`, the typed `gtbtls.Pair`, plus the `CertPool`/`ClientConfig` client helpers), shared across the HTTP, gRPC and gateway transports.

### Running a Second HTTP Server

By default a server reads its port and TLS from the `server.http` config prefix. To run more than one HTTP server in the same process — for example a public API server plus an internal/admin server — pass a `ServerOption` so each server reads its own config block or binds an explicit port.

Through the controller-integrated `Register` (a single `WithConfigPrefix` threads the prefix to both construction and start):

```go
// Reads server.gateway.port and server.gateway.tls.* (falling back to server.tls.*)
srv, err := gtbhttp.Register(ctx, "gateway", controller, props.Config, props.Logger, handler,
    gtbhttp.WithConfigPrefix("server.gateway"),
)
```

Or constructing standalone with `NewServer` + `Start` — pass the **same** prefix to both so the listen port and TLS settings stay consistent:

```go
// Public API server on server.http.*
pub, _ := gtbhttp.NewServer(ctx, props.Config, pubHandler)
controller.Register("public",
    controls.WithStart(gtbhttp.Start(props.Config, props.Logger, pub)),
    controls.WithStop(gtbhttp.Stop(props.Logger, pub)))

// Internal admin server on its own config block (server.admin.*)
adm, _ := gtbhttp.NewServer(ctx, props.Config, admHandler, gtbhttp.WithConfigPrefix("server.admin"))
controller.Register("admin",
    controls.WithStart(gtbhttp.Start(props.Config, props.Logger, adm, gtbhttp.WithConfigPrefix("server.admin"))),
    controls.WithStop(gtbhttp.Stop(props.Logger, adm)))

// ...or a fixed port with no config block at all:
dbg, _ := gtbhttp.NewServer(ctx, props.Config, dbgHandler, gtbhttp.WithPort(9090))
```

The prefix governs `<prefix>.port`, `<prefix>.tls.*` and `<prefix>.max_header_bytes`; the shared `server.port` remains the fallback. `WithPort` overrides config entirely. When the resolved port is `0`, the OS assigns an ephemeral port and `Start` logs the actually-bound address.

### Server Options

`ServerOption` values configure `NewServer` and `Start`, and are also accepted by `Register`:

- **`WithConfigPrefix(prefix string) ServerOption`**: Config prefix for port, TLS and max-header-bytes (default `server.http`).
- **`WithPort(port int) ServerOption`**: Explicit listen port, bypassing config lookup (highest precedence).
- **`WithMaxHeaderBytes(n int) ServerOption`**: Overrides `<prefix>.max_header_bytes` and the 1 MB default.
- **`WithReadTimeout` / `WithWriteTimeout` / `WithIdleTimeout(d time.Duration) ServerOption`**: Override the built-in `http.Server` timeouts.
- **`WithServerTLSConfig(c *tls.Config) ServerOption`**: Replaces the default hardened `*tls.Config` on the constructed server (named distinctly from the client-side `WithTLSConfig`).

### Functions

- **`NewServer(ctx context.Context, cfg config.Containable, handler http.Handler, opts ...ServerOption) (*http.Server, error)`**: Returns a pre-configured `*http.Server`. With no options it reads the `server.http` prefix.
- **`Start(cfg config.Containable, logger logger.Logger, srv *http.Server, opts ...ServerOption) controls.StartFunc`**: Returns a controller start function. Pass `WithConfigPrefix` to match a server built on a custom prefix; it logs the bound listener address (so an ephemeral `:0` port surfaces resolved).
- **`Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, handler http.Handler, opts ...any) (*http.Server, error)`**: Creates, configures, and registers the server with a `Controller`. The variadic accepts both `ServerOption` and `RegisterOption` values (mirroring `pkg/grpc.Register`). Health endpoints (`/healthz`, `/livez`, `/readyz`) are mounted outside any middleware chain.

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
- `WithCertPool(pool *x509.CertPool)` — trusts a custom root CA pool (private CA / self-signed) while preserving the hardened TLS defaults; build the pool with `tls.CertPool`
- `WithTransport(rt http.RoundTripper)`
- `WithRetry(cfg RetryConfig)` — enables automatic retry with exponential backoff
- `WithClientMiddleware(chain ClientChain)` — applies a middleware chain to the transport

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

### Client Middleware Chain

The client supports composable `RoundTripper` middleware, mirroring the server-side chain pattern. Middleware wraps the transport — the first in the chain executes first on the request and last on the response.

```go
// ClientMiddleware wraps an http.RoundTripper with additional behaviour.
type ClientMiddleware func(next http.RoundTripper) http.RoundTripper
```

**Chain API:**

- **`NewClientChain(middlewares ...ClientMiddleware) ClientChain`** — creates a chain
- **`(c ClientChain) Append(middlewares ...ClientMiddleware) ClientChain`** — returns a new chain with additional middleware (immutable)
- **`(c ClientChain) Then(rt http.RoundTripper) http.RoundTripper`** — applies the chain to a transport

**Built-in middleware:**

| Middleware | Description |
|-----------|-------------|
| `WithRequestLogging(log)` | Logs method, URL, status code, and duration at debug level. Headers and body are NOT logged (security). |
| `WithBearerToken(token)` | Injects `Authorization: Bearer {token}`. Sent only to the first host the client addresses, so a cross-host redirect cannot capture the token. |
| `WithBasicAuth(user, pass)` | Injects `Authorization: Basic {base64}`. Host-pinned like `WithBearerToken` — not re-sent across a cross-host redirect. |
| `WithRateLimit(rps)` | Token bucket rate limiting. Blocks until a token is available or the request context is cancelled. |

**Usage example:**

```go
chain := gtbhttp.NewClientChain(
    gtbhttp.WithRequestLogging(props.Logger),
    gtbhttp.WithBearerToken(os.Getenv("API_TOKEN")),
    gtbhttp.WithRateLimit(10), // 10 requests per second
)

client := gtbhttp.NewClient(
    gtbhttp.WithTimeout(30 * time.Second),
    gtbhttp.WithClientMiddleware(chain),
)
```

The middleware chain is applied after retry wrapping, so retry operates on the raw transport (not on logged/authed requests). Custom middleware can be written by implementing the `ClientMiddleware` function signature.

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
