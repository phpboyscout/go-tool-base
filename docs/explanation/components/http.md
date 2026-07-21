---
title: HTTP
description: Secure-by-default HTTP server and client components.
date: 2026-03-24
tags: [components, http, networking, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# HTTP

!!! info "The server, client and middleware now live in standalone modules"
    Everything below `pkg/http`'s config adapters has moved out of GTB, and the earlier
    re-export facade has been **removed** — import the owning module directly:

    - **HTTP server** (`NewServer`, `Register`, `Start`/`Stop`, health handlers, `AuthMiddleware`, `SecurityHeadersMiddleware`, lifecycle glue) → [`gitlab.com/phpboyscout/go/transport/http`](https://transport.go.phpboyscout.uk) (alias `transporthttp`).
    - **Transport middleware** (`NewChain`, `Chain`, `LoggingMiddleware`, `RateLimitMiddleware`, `OTelMiddleware`, the client-middleware chain and its `With…` builders) → [`gitlab.com/phpboyscout/go/transit/http`](https://transit.go.phpboyscout.uk) (alias `transithttp`).
    - **HTTP client factory** (`NewClient`, `NewTransport`, the client `With…` options) → [`gitlab.com/phpboyscout/go/httpclient`](https://httpclient.go.phpboyscout.uk) (alias `httpclient`).

    `pkg/http` (alias `gtbhttp`) keeps **only** the `config.Reader` adapters — `ServerSettingsFromConfig`, `ObserveServerSettingsFromConfig`, `NewServerFromReader`, `StartFromReader`, `RegisterFromReader`, `RateLimitConfigFromConfig`, `CircuitBreakerConfigFromConfig`, with the GTB config-selection options `WithConfigPrefix`/`WithPort`. See the [facades-removed migration note](../../reference/migration/v0.x-facades-removed.md) and the [transport-extraction note](../../reference/migration/v0.x-transport-extracted.md). Behaviour is unchanged; only the import paths moved.

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

The core server constructors take package-owned typed settings. GTB config is
adapted at the framework boundary with `ServerSettingsFromConfig` for one-shot
construction or `ObserveServerSettingsFromConfig` for reload-aware snapshots.

By default `RegisterFromReader` reads server settings from the `server.http` config prefix. To run more than one HTTP server in the same process — for example a public API server plus an internal/admin server — pass a `ServerOption` so each reads its own config block or binds an explicit port. `RegisterFromReader` builds the server, wires its start/stop into the controller, and returns it in one call:

```go
view := props.Config.View() // pin one snapshot for the reads below

// Public API server on server.http.*
gtbhttp.RegisterFromReader(ctx, "public", controller, view, props.Logger, pubHandler)

// Internal admin server on its own config block (server.admin.* — port, tls.*,
// max_header_bytes), falling back to the shared server.port.
gtbhttp.RegisterFromReader(ctx, "admin", controller, view, props.Logger, admHandler,
    gtbhttp.WithConfigPrefix("server.admin"))

// ...or a fixed port with no config block at all — WithPort overrides config:
gtbhttp.RegisterFromReader(ctx, "debug", controller, view, props.Logger, dbgHandler,
    gtbhttp.WithPort(9090))
```

`WithConfigPrefix` threads the prefix through both construction and start, so the listen port and TLS settings stay consistent. When the resolved port is `0`, the OS assigns an ephemeral port and start logs the actually-bound address.

Need the server without controller integration? `NewServerFromReader` + `StartFromReader` split the same config-driven flow, and for config-free construction from explicit typed settings the core constructors live in the transport module — `transporthttp.NewServer(ctx, settings, handler)`, `transporthttp.StartWithTLSPair(slogLog, srv, tlsPair)`, `transporthttp.Stop(slogLog, srv)`. `ServerSettingsFromConfig(view, prefix)` resolves a `transporthttp.ServerSettings` from GTB config when you want to bridge the two.

### Observing Server Settings

For long-lived GTB composition, bind the config section once and pass the
observed snapshot through a package-owned settings source:

```go
settings, err := gtbhttp.ObserveServerSettingsFromConfig(
    props.Config,
    "server.http",
    config.WithSectionApply(func(change config.SectionChange[gtbhttp.ServerSettings]) error {
        props.Logger.Info("http server settings changed", "version", change.Version)
        return nil
    }),
)
if err != nil {
    return err
}

var source gtbhttp.ServerSettingsSource = settings
_ = source.Current()
```

`ObserveServerSettingsFromConfig` uses the same resolved config semantics as
`ServerSettingsFromConfig`, including the shared `server.port` fallback. The
binding rehydrates on successful config reloads, increments `Version()` only
when the typed `ServerSettings` snapshot changes, and exposes the latest
immutable value through `Current()`. Existing HTTP servers do not automatically
restart when the port changes; use the observed source for newly constructed
servers or explicit package-level reconfiguration logic.

### Server Options

`ServerOption` values configure `NewServer` and `Start`, and are also accepted by `Register`:

- **`WithConfigPrefix(prefix string) ServerOption`**: Config prefix for port, TLS and max-header-bytes (default `server.http`).
- **`WithPort(port int) ServerOption`**: Explicit listen port, bypassing config lookup (highest precedence).
- **`WithMaxHeaderBytes(n int) ServerOption`**: Overrides `<prefix>.max_header_bytes` and the 1 MB default.
- **`WithReadTimeout` / `WithWriteTimeout` / `WithIdleTimeout(d time.Duration) ServerOption`**: Override the built-in `http.Server` timeouts.
- **`WithServerTLSConfig(c *tls.Config) ServerOption`**: Replaces the default hardened `*tls.Config` on the constructed server (named distinctly from the client-side `WithTLSConfig`).

### Functions

- **`NewServer(ctx context.Context, settings ServerSettings, handler http.Handler, opts ...ServerOption) (*http.Server, error)`**: Returns a pre-configured `*http.Server` from typed settings.
- **`ServerSettingsFromConfig(cfg config.Reader, prefix string) ServerSettings`**: GTB adapter helper for one-shot settings resolution. Empty prefix defaults to `server.http`.
- **`ObserveServerSettingsFromConfig(cfg config.Binder, prefix string, opts ...config.SectionBindingOption[ServerSettings]) (*config.ObservedSection[ServerSettings], error)`**: GTB adapter helper for reload-aware typed server settings.
- **`StartWithTLSPair(logger *slog.Logger, srv *http.Server, tlsPair gtbtls.Pair) controls.StartFunc`**: Returns a controller start function. Pass `WithConfigPrefix` to match a server built on a custom prefix; it logs the bound listener address (so an ephemeral `:0` port surfaces resolved).
- **`Register(ctx context.Context, id string, controller controls.Controllable, logger *slog.Logger, handler http.Handler, settings ServerSettings, tlsPair gtbtls.Pair, opts ...any) (*http.Server, error)`**: Creates, configures, and registers the server with a `Controller`. The variadic accepts both `ServerOption` and `RegisterOption` values (mirroring `pkg/grpc.Register`). Health endpoints (`/healthz`, `/livez`, `/readyz`) are mounted outside any middleware chain.
- **`NewServerFromReader`, `StartFromReader`, `RegisterFromReader`**: GTB adapters that read the existing config structure through a `config.Reader` (typically `props.Config.View()`) and delegate to the typed constructors.
- **`Stop(logger *slog.Logger, srv *http.Server) controls.StopFunc`**: Returns a controller stop function. It calls `srv.Shutdown(ctx)` to drain in-flight requests; if the shutdown context deadline expires (a handler outlives it), the server is **force-closed** via `srv.Close()` so a hung handler cannot leave the listener and connections open. This mirrors the gRPC transport's graceful-then-force-stop behaviour.

**Drain semantics**: the server's per-request `BaseContext` is detached from the construction context with `context.WithoutCancel`. Cancelling the construction context (typically at shutdown) therefore does **not** cancel already-accepted requests mid-drain — `Shutdown` is left to drain them within its deadline. Context *values* on the construction context are still propagated to each request.

### Middleware Chaining

> The HTTP server chain is one of four transport middleware surfaces. For the cross-cutting pattern (server/client × HTTP/gRPC), the resilience composition rules, and the config-prefix convention, see the [Transport Middleware & Resilience](../concepts/transport-middleware.md) concept.

The alice-style middleware chaining API lives in [`gitlab.com/phpboyscout/go/transit/http`](https://transit.go.phpboyscout.uk) (import as `transithttp`); the server-side security-headers and auth middleware live in [`gitlab.com/phpboyscout/go/transport/http`](https://transport.go.phpboyscout.uk) (import as `transporthttp`), and `WithMiddleware` is a `transporthttp` `RegisterOption`. Middleware uses the standard `func(http.Handler) http.Handler` signature.

- **`transithttp.NewChain(middlewares ...Middleware) Chain`**: Creates a middleware chain. The first middleware is the outermost wrapper.
- **`(c Chain) Append(middlewares ...Middleware) Chain`**: Returns a new chain with additional middleware appended (immutable).
- **`(c Chain) Extend(other Chain) Chain`**: Composes two chains.
- **`(c Chain) Then(handler http.Handler) http.Handler`**: Applies the chain to a handler.
- **`(c Chain) ThenFunc(fn http.HandlerFunc) http.Handler`**: Convenience for `Then(http.HandlerFunc(fn))`.
- **`WithMiddleware(chain Chain) RegisterOption`**: Applies a middleware chain when using `Register`. Health endpoints are unaffected.

### Built-in Logging Middleware

`LoggingMiddleware` (in `transithttp`) logs each completed HTTP request with structured fields (method, path, status, latency, bytes, client IP, user agent).

- **`transithttp.LoggingMiddleware(logger *slog.Logger, opts ...LoggingOption) Middleware`**

**Format options** (`WithFormat`):

| Format | Description |
|--------|-------------|
| `FormatStructured` (default) | Structured key-value fields via `*slog.Logger` |
| `FormatCommon` | NCSA Common Log Format |
| `FormatCombined` | NCSA Combined Log Format (CLF + Referer + User-Agent) |
| `FormatJSON` | Single JSON object per request |

**Other options**: `WithLogLevel`, `WithoutLatency`, `WithoutUserAgent`, `WithPathFilter`, `WithHeaderFields`, `WithTrustedProxy`.

**Client IP and trusted proxies**: by default the logged `client_ip` is taken from the connection's `RemoteAddr`. The `X-Forwarded-For` and `X-Real-IP` headers are **ignored** because any direct client can forge them. Enable `WithTrustedProxy()` only when the server sits behind a trusted reverse proxy or load balancer that overwrites these headers; with it set, the left-most `X-Forwarded-For` entry (falling back to `X-Real-IP`) is used. Enabling it on a directly-exposed server lets clients spoof the recorded client IP.

### Built-in Security-Headers Middleware

`SecurityHeadersMiddleware` (in `transporthttp`) sets a conservative set of response security headers on every request. It returns a `transithttp.Middleware`, so it composes into any `transithttp.NewChain`/`transporthttp.WithMiddleware` pipeline.

- **`transporthttp.SecurityHeadersMiddleware(opts ...SecurityHeadersOption) transithttp.Middleware`**

**Default headers:**

| Header | Default value |
|--------|---------------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Content-Security-Policy` | `frame-ancestors 'none'` |
| `Referrer-Policy` | `no-referrer` |
| `Strict-Transport-Security` | *(off by default)* |

**Options:**

| Option | Effect |
|--------|--------|
| `WithContentTypeOptions(v)` | Override `X-Content-Type-Options` (empty omits the header). |
| `WithFrameOptions(v)` | Override `X-Frame-Options` (empty omits the header). |
| `WithReferrerPolicy(v)` | Override `Referrer-Policy` (empty omits the header). |
| `WithContentSecurityPolicy(p)` | Set a full CSP, replacing the frame-ancestors-only default. An empty value falls back to the default so the clickjacking control is never silently dropped. |
| `WithHSTS(maxAge, includeSubdomains, preload)` | Enable `Strict-Transport-Security`. **Off by default** — HSTS is only meaningful (and only safe) over TLS. A non-positive `maxAge` leaves it disabled. |

Headers are set **before** the wrapped handler runs, so a handler that writes its own response still emits them; a handler may override any value by setting its own.

**Applied to the built-in surfaces by default.** The interactive docs/OpenAPI handlers (`pkg/openapi.Register`) and the documentation server (`pkg/docs.Serve`) wrap their handlers with this middleware automatically — the docs UI serves a "try-it" console that benefits from `nosniff`/frame/referrer protections. Customise via `openapi.WithSecurityHeaderOptions(...)` or opt out with `openapi.WithoutSecurityHeaders()`. The middleware is **not** forced onto user-supplied handlers; add it to your own chain where you want it.

### Built-in Rate-Limit Middleware

`RateLimitMiddleware` protects a server from overload by admitting requests under a token-bucket limiter and rejecting excess traffic with **`429 Too Many Requests`** plus a `Retry-After` header (which a GTB client's retry layer honours — a pleasing closed loop). It is an ordinary `Middleware`, so it composes into any `Chain`.

- **`transithttp.RateLimitMiddleware(log *slog.Logger, cfg RateLimitConfig) Middleware`**
- **`transithttp.DefaultRateLimitConfig() RateLimitConfig`** — 50 rps sustained, burst 100, single global bucket
- **`transithttp.ClientIPKey(r *http.Request) string`** — a ready-made per-client `KeyFunc`
- **`gtbhttp.RateLimitConfigFromConfig(cfg config.Reader, prefix string) transithttp.RateLimitConfig`** — GTB config adapter that reads policy from config (stays in `pkg/http`)

**`RateLimitConfig` fields:**

| Field | Default | Description |
|-------|---------|-------------|
| `RequestsPerSecond` | 50 | Sustained token-bucket fill rate |
| `Burst` | 100 | Bucket capacity — max instantaneous spike admitted |
| `KeyFunc` | nil | Derives a per-client key; nil = one global bucket |
| `MaxTrackedKeys` | 8192 | Bound on the per-key bucket store (ignored when `KeyFunc` is nil) |
| `OnLimited` | nil | Optional callback invoked on each rejection (metrics/telemetry) |

Admission is **non-blocking** (`Allow`, not `Wait`): ingress must *reject* excess, never queue it, or a flood would exhaust memory.

**Scoping is composition, not configuration.** A *global* limiter is one entry in the server chain; a *per-route* limiter wraps a single handler; a *per-client* limiter sets `KeyFunc`. The per-key bucket store is **bounded and LRU-evicting** (capped at `MaxTrackedKeys`) so an attacker rotating source keys cannot exhaust memory.

> **Security note on `ClientIPKey`.** It keys on the connection's `RemoteAddr` and **deliberately ignores `X-Forwarded-For`/`X-Real-IP`** — those headers are spoofable by any direct client, so keying on them would let an attacker both evade their own bucket and churn the key store. A server behind a *trusted* reverse proxy that terminates XFF should supply its own `KeyFunc` that reads the proxy-set header.

Health endpoints (`/healthz`, `/livez`, `/readyz`) are mounted **outside** the `WithMiddleware` chain, so a global limiter never throttles liveness/readiness probes.

**Config keys** (`RateLimitConfigFromConfig`, prefix defaults to `server.http`):

| Key | Maps to |
|-----|---------|
| `server.http.ratelimit.requests_per_second` | `RequestsPerSecond` |
| `server.http.ratelimit.burst` | `Burst` |
| `server.http.ratelimit.max_tracked_keys` | `MaxTrackedKeys` |

Unset keys keep their defaults; the code-only fields (`KeyFunc`, `OnLimited`) are never read from config — wiring stays explicit.

### Built-in Authentication Middleware

`AuthMiddleware` authenticates (and optionally authorizes) each request from an [`pkg/authn`](authn.md) verifier — an API key, a JWT/OIDC bearer token, a session cookie, or an mTLS client certificate — storing the verified identity in the request context. It is an ordinary `Middleware`, so it composes into any `Chain`.

`AuthMiddleware` and its options live in `transporthttp`.

- **`transporthttp.AuthMiddleware(opts ...AuthOption) (transithttp.Middleware, error)`** — with no verifier configured it is a construction error (fail-closed)
- **`transporthttp.IdentityFromContext(ctx context.Context) (*authn.Identity, bool)`** — read the verified identity in a handler (same context key as the gRPC interceptor)

```go
keys, _ := authn.NewAPIKeyVerifier(authn.KeyEntry{Key: ciKey, Subject: "ci"})
authMW, _ := transporthttp.AuthMiddleware(
    transporthttp.WithAPIKeyHeader("X-API-Key", keys),
    transporthttp.WithAuthorize(authn.RequireScopes("api:write")),
    transporthttp.WithAuthLogger(logger.ToSlog(props.Logger)),
)
chain := transithttp.NewChain(transithttp.LoggingMiddleware(logger.ToSlog(props.Logger)), authMW)
```

Options: `WithBearerVerifier`, `WithAPIKeyHeader`, `WithCookieVerifier`, `WithMTLSVerifier`, `WithAuthorize`, `WithAuthLogger`, `WithAuthSkipper`. On failure it writes a generic `401` (with `WWW-Authenticate`) or `403` and logs the cause with the credential redacted — never disclosing why to the client. Health endpoints are outside the chain, so a global auth middleware never gates probes. **See [Authentication & Authorization](authn.md) for the full reference**, including the verifiers, the authorization seam, credential precedence, and the security model.

`WithCookieVerifier(name, v)` reads the credential from a named cookie. The cookie is an **ambient** credential — the browser sends it on every request, including `<img>`/`<audio>`/`<video>` sub-resource loads that cannot set an `Authorization` header — so it sits **below** the explicit header schemes in precedence (an explicit bearer or API-key header always wins; the cookie is consulted only when no header credential is presented). This makes a browser session authenticate sub-resources while leaving explicit API clients unaffected; typically paired with a token-in-URL bootstrap that sets the cookie on first load (Jupyter-style).

### Usage Example

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/data", myDataHandler)

// Build a middleware chain (transit/http)
chain := transithttp.NewChain(
    transithttp.LoggingMiddleware(logger.ToSlog(props.Logger),
        transithttp.WithFormat(transithttp.FormatCombined),
        transithttp.WithPathFilter("/healthz", "/livez", "/readyz"),
    ),
)

// Build settings via the GTB config adapter, then register on the transport server.
// Health endpoints stay outside the chain.
view := props.Config.View()
settings := gtbhttp.ServerSettingsFromConfig(view, "server.http")
tlsPair := gtbtls.Resolve(view, "server.http.tls")
srv, err := transporthttp.Register(ctx, "http-api", controller, logger.ToSlog(props.Logger), mux, settings, tlsPair,
    transporthttp.WithMiddleware(chain),
)
```

## Client Factory

The hardened `http.Client` factory for outbound requests lives in the standalone, framework-free module [`gitlab.com/phpboyscout/go/httpclient`](https://httpclient.go.phpboyscout.uk) (`v0.1.0`), imported as `httpclient`.

> The re-export facade has been **removed**: import `gitlab.com/phpboyscout/go/httpclient` directly for `NewClient`, `NewTransport` and the client `With…` options. The client-middleware chain, `RetryConfig` and the circuit breaker live alongside the server middleware in [`gitlab.com/phpboyscout/go/transit/http`](https://transit.go.phpboyscout.uk) (`transithttp`); `httpclient.WithClientMiddleware`/`WithRetry` take those `transithttp` types. `RateLimitConfigFromConfig`/`CircuitBreakerConfigFromConfig` remain GTB config adapters in `pkg/http`. See the [facades-removed](../../reference/migration/v0.x-facades-removed.md) and [httpclient-extraction](../../reference/migration/v0.x-httpclient-extracted.md) notes.

### Features

- **Mandatory Timeouts**: Default 30s timeout to prevent blocked goroutines.
- **Secure Transport**: Uses the same hardened TLS configuration as the server.
- **Scheme Protection**: Redirect policy rejects HTTPS-to-HTTP downgrades.
- **Connection Limits**: Pre-configured idle connection pooling and timeouts.

### Functions

- **`httpclient.NewClient(opts ...ClientOption) *http.Client`**: Returns a hardened HTTP client.
- **`httpclient.NewTransport(tlsCfg *tls.Config) *http.Transport`**: Returns a pre-configured secure transport for custom client needs.

### Options

All `httpclient` `ClientOption` values:

- `httpclient.WithTimeout(d time.Duration)`
- `httpclient.WithMaxRedirects(n int)`
- `httpclient.WithTLSConfig(cfg *tls.Config)`
- `httpclient.WithCertPool(pool *x509.CertPool)` — trusts a custom root CA pool (private CA / self-signed) while preserving the hardened TLS defaults; build the pool with `tls.CertPool`
- `httpclient.WithTransport(rt http.RoundTripper)`
- `httpclient.WithRetry(cfg transithttp.RetryConfig)` — enables automatic retry with exponential backoff (`RetryConfig` is a `transithttp` type)
- `httpclient.WithClientMiddleware(chain transithttp.ClientChain)` — applies a `transithttp` middleware chain to the transport

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

**Backoff strategy**: Full jitter — `uniform random in [0, min(cap, base × 2^attempt)]`. This reduces thundering-herd effects compared to fixed or equal-jitter backoff. The exponential term is clamped to `MaxBackoff` *before* the doubling so the computed delay can never overflow. Invalid `RetryConfig` values (negative `MaxRetries`, non-positive backoff durations, `MaxBackoff` below `InitialBackoff`) are normalised to safe defaults at client construction.

### Client Middleware Chain

The client supports composable `RoundTripper` middleware, mirroring the server-side chain pattern. The chain type and its built-in middleware live in `transithttp`; `httpclient.WithClientMiddleware` applies the assembled chain. Middleware wraps the transport — the first in the chain executes first on the request and last on the response.

```go
// transithttp.ClientMiddleware wraps an http.RoundTripper with additional behaviour.
type ClientMiddleware func(next http.RoundTripper) http.RoundTripper
```

**Chain API (`transithttp`):**

- **`transithttp.NewClientChain(middlewares ...ClientMiddleware) ClientChain`** — creates a chain
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
chain := transithttp.NewClientChain(
    transithttp.WithRequestLogging(logger.ToSlog(props.Logger)),
    transithttp.WithBearerToken(os.Getenv("API_TOKEN")),
    transithttp.WithRateLimit(10), // 10 requests per second
)

client := httpclient.NewClient(
    httpclient.WithTimeout(30 * time.Second),
    httpclient.WithClientMiddleware(chain),
)
```

The middleware chain is applied after retry wrapping, so retry operates on the raw transport (not on logged/authed requests), and a `ClientMiddleware` placed in the chain therefore sits **outside** the retry transport. Custom middleware can be written by implementing the `ClientMiddleware` function signature.

### Circuit Breaker

`WithCircuitBreaker` is a `ClientMiddleware` that **fails fast** while a downstream is consistently failing, avoiding wasted retry/backoff cycles against a service that will not answer. It is the partner primitive to retry: retry handles *transient* flakiness, the breaker handles a *hard* outage.

- **`transithttp.WithCircuitBreaker(log *slog.Logger, cfg CircuitBreakerConfig) ClientMiddleware`**
- **`transithttp.DefaultCircuitBreakerConfig() CircuitBreakerConfig`** — threshold 5, cooldown 30s, half-open trial 1
- **`transithttp.ErrCircuitOpen`** — sentinel error returned (wrapped) while open; test with `errors.Is`
- **`gtbhttp.CircuitBreakerConfigFromConfig(cfg config.Reader, prefix string) transithttp.CircuitBreakerConfig`** — GTB config adapter (stays in `pkg/http`)

**States:** `StateClosed` (admit all, count failures) → `StateOpen` (reject immediately with `ErrCircuitOpen` until the cooldown elapses) → `StateHalfOpen` (admit a bounded number of trials; the first success closes the breaker, any failure re-opens it).

**`CircuitBreakerConfig` fields:**

| Field | Default | Description |
|-------|---------|-------------|
| `FailureThreshold` | 5 | Consecutive failures (in Closed) that trip the breaker open |
| `Cooldown` | 30s | How long the breaker stays Open before a trial |
| `HalfOpenMaxRequests` | 1 | Trial requests admitted in HalfOpen |
| `IsFailure` | nil | Classifier; default = transport errors + 5xx are failures |
| `OnStateChange` | nil | Optional transition callback (metrics/telemetry) |

By default, **transport errors and 5xx responses count as failures; 4xx do not** — in particular a **429 does not trip the breaker** (that is the retry/backoff layer's concern, not a downstream-health signal).

**Ordering (composition with retry).** Place the breaker in the `ClientChain` so it sits outside the retry transport:

```
request → [circuit breaker] → [retry (backoff)] → [base transport] → network
```

The breaker sees the **final post-retry verdict**: one logical call that exhausts its retry budget against a dead service counts as **one** breaker failure, not N. Once Open, calls are rejected **before** entering the retry layer — so no backoff sleeps are spent on a service known to be down (exactly the waste the retry design flagged). The breaker **never** serves a cached response; an open breaker returns an error, never a stored body.

```go
client := httpclient.NewClient(
    httpclient.WithRetry(transithttp.DefaultRetryConfig()),
    httpclient.WithClientMiddleware(transithttp.NewClientChain(
        transithttp.WithCircuitBreaker(logger.ToSlog(props.Logger), transithttp.DefaultCircuitBreakerConfig()),
        transithttp.WithRequestLogging(logger.ToSlog(props.Logger)),
    )),
)
```

**Config keys** (`CircuitBreakerConfigFromConfig`, prefix defaults to `server.http`): `server.http.circuitbreaker.failure_threshold`, `.cooldown`, `.half_open_max_requests`.

**Retry-After support**: When a 429 or 503 response includes a `Retry-After` header (seconds or HTTP-date), that value is used as the delay instead of the computed backoff. The header value is **clamped to `MaxBackoff`**, so a hostile or misconfigured server cannot stall the client beyond the configured cap.

**Body rewind**: Request bodies are rewound via `GetBody` between attempts. Response bodies from failed attempts are drained and closed to allow connection reuse. A request whose body has already been consumed and that has **no `GetBody`** to rewind it (e.g. a streamed `io.Reader` body) is **not retried** — resending it would silently send an empty or partial body. A `nil` body (e.g. `GET`) is always safe to retry.

**Context cancellation**: If the request context is cancelled during a backoff wait, the retry loop exits immediately with the context error.

### Usage Example

```go
// Simple secure client
client := httpclient.NewClient()

// Client with automatic retry for transient failures
client := httpclient.NewClient(
    httpclient.WithTimeout(60*time.Second),
    httpclient.WithRetry(transithttp.DefaultRetryConfig()),
)

// Custom retry configuration
client := httpclient.NewClient(
    httpclient.WithRetry(transithttp.RetryConfig{
        MaxRetries:           5,
        InitialBackoff:       200 * time.Millisecond,
        MaxBackoff:           10 * time.Second,
        RetryableStatusCodes: []int{429, 502, 503, 504},
    }),
)

// Custom retry predicate
client := httpclient.NewClient(
    httpclient.WithRetry(transithttp.RetryConfig{
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

// Power user: custom client with secure transport (stdlib *http.Client + hardened transport)
customClient := &http.Client{
    Transport: httpclient.NewTransport(nil),
}
```
