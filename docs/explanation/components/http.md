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
    re-export facade has been **removed**. Import the owning module directly:

    - **HTTP server** (`NewServer`, `Register`, `Start`/`Stop`, health handlers, `AuthMiddleware`, `SecurityHeadersMiddleware`, lifecycle glue) → [`gitlab.com/phpboyscout/go/transport/http`](https://transport.go.phpboyscout.uk) (alias `transporthttp`).
    - **Transport middleware** (`NewChain`, `Chain`, `LoggingMiddleware`, `RateLimitMiddleware`, `OTelMiddleware`, the client-middleware chain and its `With…` builders) → [`gitlab.com/phpboyscout/go/transit/http`](https://transit.go.phpboyscout.uk) (alias `transithttp`).
    - **HTTP client factory** (`NewClient`, `NewTransport`, the client `With…` options) → [`gitlab.com/phpboyscout/go/httpclient`](https://httpclient.go.phpboyscout.uk) (alias `httpclient`).

    `pkg/http` (alias `gtbhttp`) keeps **only** the `config.Reader` adapters: `ServerSettingsFromConfig`, `ObserveServerSettingsFromConfig`, `NewServerFromReader`, `StartFromReader`, `RegisterFromReader`, `RateLimitConfigFromConfig`, `CircuitBreakerConfigFromConfig`, with the GTB config-selection options `WithConfigPrefix`/`WithPort`. See the [facades-removed migration note](../../reference/migration/v0.x-facades-removed.md) and the [transport-extraction note](../../reference/migration/v0.x-transport-extracted.md). Behaviour is unchanged; only the import paths moved.

The `pkg/http` package provides hardened HTTP components for both server-side and client-side operations. It enforces secure TLS defaults, provides built-in observability endpoints, and mirrors the security posture required for production environments.

## Server Control

The HTTP server implementation integrates seamlessly with the `controls` lifecycle management.

### Features

- **Standardized Endpoints**: Automatically mounts `/healthz`, `/livez`, and `/readyz`.
- **Production Timeouts**: Pre-configured Read (5s), Write (10s), and Idle (120s) timeouts.
- **Secure TLS**: Enforces TLS 1.2 minimum with curated AEAD-based cipher suites and X25519 preference.

### TLS Configuration

TLS configuration cascades: transport-specific keys override the shared defaults:

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

By default `RegisterFromReader` reads server settings from the `server.http` config prefix. To run more than one HTTP server in the same process (for example a public API server plus an internal/admin server) pass a `ServerOption` so each reads its own config block or binds an explicit port. `RegisterFromReader` builds the server, wires its start/stop into the controller, and returns it in one call:

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

Need the server without controller integration? `NewServerFromReader` + `StartFromReader` split the same config-driven flow, and for config-free construction from explicit typed settings the core constructors live in the transport module: `transporthttp.NewServer(ctx, settings, handler)`, `transporthttp.StartWithTLSPair(slogLog, srv, tlsPair)`, `transporthttp.Stop(slogLog, srv)`. `ServerSettingsFromConfig(view, prefix)` resolves a `transporthttp.ServerSettings` from GTB config when you want to bridge the two.

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

GTB's own `ServerOption` (`gtbhttp`) has two values, and steers **config
resolution**:

- **`WithConfigPrefix(prefix string) ServerOption`**: Config prefix for port, TLS and max-header-bytes (default `server.http`).
- **`WithPort(port int) ServerOption`**: Explicit listen port, bypassing config lookup (highest precedence).

The `*FromReader` adapters accept these alongside the transport's own,
same-named `transporthttp.ServerOption` (`WithMaxHeaderBytes`,
`WithReadTimeout`/`WithWriteTimeout`/`WithIdleTimeout`,
`WithServerTLSConfig`), which are forwarded unchanged to the constructor. See
[go/transport/http](https://transport.go.phpboyscout.uk) for those.

### Bind address, invalid ports & option safety

The server reads a **bind address** from `<prefix>.host` (e.g. `server.http.host`).
It defaults to `""`: **all interfaces** (`0.0.0.0` / `[::]`), unchanged from prior
releases, so set it to `127.0.0.1` to restrict a listener (admin, metrics) to
loopback. The transport also exposes `transporthttp.WithHost` / `WithBindAddress`.
See the [bind-address migration note](../../reference/migration/v0.x-server-bind-address.md).

An **out-of-range explicit port** passed via `WithPort` (e.g. `70000`) is now a
hard error from `NewServerFromReader`/`RegisterFromReader`, not a silent ephemeral
(`:0`) bind. It is forwarded to the transport's validated port resolution. Pass
`WithPort(0)` explicitly if you genuinely want an OS-assigned ephemeral port.

An **unsupported option type** (a value that is neither a GTB `ServerOption` nor a
transport `ServerOption`) is rejected rather than silently dropped:
`NewServerFromReader` returns an error naming the offending type; `StartFromReader`
(no error return) logs a WARN.

### Functions

`pkg/http` (`gtbhttp`) exports only the config-key adapters below; construction,
start and stop (`NewServer`, `Register`, `StartWithTLSPair`, `Stop`) are
`transporthttp`'s own functions, taking explicit typed settings rather than a
`config.Reader` — see [go/transport/http](https://transport.go.phpboyscout.uk)
for those.

- **`ServerSettingsFromConfig(cfg config.Reader, prefix string) transporthttp.ServerSettings`**: GTB adapter helper for one-shot settings resolution. Empty prefix defaults to `server.http`.
- **`ObserveServerSettingsFromConfig(cfg config.Binder, prefix string, opts ...config.SectionBindingOption[ServerSettings]) (*config.ObservedSection[ServerSettings], error)`**: GTB adapter helper for reload-aware typed server settings.
- **`NewServerFromReader(ctx context.Context, cfg config.Reader, handler http.Handler, opts ...any) (*http.Server, error)`**: GTB adapter that resolves settings from config and delegates to `transporthttp.NewServer`.
- **`StartFromReader(cfg config.Reader, log logger.Logger, srv *http.Server, opts ...any) controls.StartFunc`**: GTB adapter that resolves settings and TLS from config, then delegates to `transporthttp.StartWithTLSPair`.
- **`RegisterFromReader(ctx context.Context, id string, controller controls.Controllable, cfg config.Reader, log logger.Logger, handler http.Handler, opts ...any) (*http.Server, error)`**: GTB adapter that reads the existing config structure and delegates to `transporthttp.Register`. The variadic accepts both GTB's `ServerOption` and the transport's own `ServerOption`/`RegisterOption` values. Health endpoints (`/healthz`, `/livez`, `/readyz`) are mounted outside any middleware chain.

**Drain semantics** (on the transport's `Stop`): it calls `srv.Shutdown(ctx)` to drain in-flight requests; if the shutdown context deadline expires (a handler outlives it), the server is **force-closed** via `srv.Close()` so a hung handler cannot leave the listener and connections open, mirroring the gRPC transport's graceful-then-force-stop behaviour. The per-request `BaseContext` is detached from the construction context with `context.WithoutCancel`, so cancelling the construction context at shutdown does **not** cancel already-accepted requests mid-drain.

### Middleware

> The HTTP server chain is one of four transport middleware surfaces. For the cross-cutting pattern (server/client × HTTP/gRPC), the resilience composition rules, and the config-prefix convention, see the [Transport Middleware & Resilience](../concepts/transport-middleware.md) concept.

The middleware chain API, `LoggingMiddleware` and `RateLimitMiddleware` live in
[`go/transit/http`](https://transit.go.phpboyscout.uk) (`transithttp`); the
server-side `SecurityHeadersMiddleware` and `AuthMiddleware` live in
[`go/transport/http`](https://transport.go.phpboyscout.uk) (`transporthttp`),
and `WithMiddleware` is a `transporthttp` `RegisterOption`. Full API, option
tables and config keys are on those modules' own reference pages; GTB's only
adapter here is `RateLimitConfigFromConfig(cfg config.Reader, prefix string)
transithttp.RateLimitConfig`, which reads
`<prefix>.ratelimit.{requests_per_second,burst,max_tracked_keys}` (prefix
defaults to `server.http`).

**Security headers are applied to GTB's own built-in surfaces by default.**
The interactive docs/OpenAPI handlers (the standalone `go/transport-openapi`
module's `Register`) and the documentation server (`pkg/docs.Serve`) wrap
their handlers with `SecurityHeadersMiddleware` automatically; customise via
`openapi.WithSecurityHeaderOptions(...)` or opt out with
`openapi.WithoutSecurityHeaders()`. It is **not** forced onto user-supplied
handlers.

Health endpoints (`/healthz`, `/livez`, `/readyz`) are always mounted
**outside** the `WithMiddleware` chain, so logging, rate limiting and auth
never gate probes.

`AuthMiddleware` wraps an [`go/authn`](https://authn.go.phpboyscout.uk)
verifier; GTB does not wire it itself (no default verifier, no config-key
schema for it). See the module's own docs for `WithAPIKeyHeader`,
`WithBearerVerifier`, `WithCookieVerifier`, `WithMTLSVerifier` and the
authorization seam.

### Usage Example

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/data", myDataHandler)

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

The hardened `http.Client` factory for outbound requests is the standalone,
framework-free module
[`gitlab.com/phpboyscout/go/httpclient`](https://httpclient.go.phpboyscout.uk),
imported as `httpclient`: `NewClient`, `NewTransport`, mandatory timeouts, the
HTTPS-downgrade redirect policy, and the client `With…` options. The
client-middleware chain (logging, bearer/basic auth, rate limiting), retry
with exponential backoff, and the circuit breaker live alongside the server
middleware in [`go/transit/http`](https://transit.go.phpboyscout.uk)
(`transithttp`); `httpclient.WithClientMiddleware`/`WithRetry` take those
types. GTB has no adapter over the client factory itself; the only client-side
config adapter that stays in `pkg/http` is
`CircuitBreakerConfigFromConfig(cfg config.Reader, prefix string)
transithttp.CircuitBreakerConfig`, reading
`<prefix>.circuitbreaker.{failure_threshold,cooldown,half_open_max_requests}`
(prefix defaults to `server.http`).

```go
client := httpclient.NewClient(
    httpclient.WithTimeout(30 * time.Second),
    httpclient.WithRetry(transithttp.DefaultRetryConfig()),
    httpclient.WithClientMiddleware(transithttp.NewClientChain(
        transithttp.WithCircuitBreaker(logger.ToSlog(props.Logger), gtbhttp.CircuitBreakerConfigFromConfig(view, "server.http")),
        transithttp.WithRequestLogging(logger.ToSlog(props.Logger)),
    )),
)
```
