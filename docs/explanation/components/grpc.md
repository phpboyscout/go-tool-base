---
title: gRPC
description: Secure-by-default gRPC server components.
date: 2026-03-24
tags: [components, grpc, networking, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# gRPC

!!! info "The server, interceptors and dial factory now live in standalone modules"
    The hardened **gRPC server** (`NewServer`, `Register`, `Start`, health service, `AuthInterceptor`, TLS credentials, `DialLocal`, lifecycle glue) has been extracted to [`gitlab.com/phpboyscout/go/transport/grpc`](https://transport.go.phpboyscout.uk) (alias `transportgrpc`); the **interceptors and resilience primitives** (`NewInterceptorChain`, `LoggingInterceptor`, `RateLimitInterceptor`, `OTelStatsHandler`, the circuit breaker) to [`gitlab.com/phpboyscout/go/transit/grpc`](https://transit.go.phpboyscout.uk) (alias `transitgrpc`); and the **dial factory** behind `DialLocal` to [`gitlab.com/phpboyscout/go/grpcclient`](https://grpcclient.go.phpboyscout.uk) (alias `grpcclient`). This was a **clean break**: `pkg/grpc` (alias `gtbgrpc`) keeps only the `config.Reader` adapters (`NewServerFromReader`, `StartFromReader`, `RegisterFromReader`, `DialLocalFromReader`, `RateLimitConfigFromConfig`, `CircuitBreakerConfigFromConfig`, with `WithConfigPrefix`/`WithPort`). The earlier re-export facade has been removed. Import the owning module directly. See the [facades-removed migration note](../../reference/migration/v0.x-facades-removed.md) and the [transport-extraction note](../../reference/migration/v0.x-transport-extracted.md). Behaviour is unchanged; only the import paths moved.

The `pkg/grpc` package provides a standard gRPC server implementation that integrates with the `controls` package for lifecycle management and observability.

## Features

- **Standard Observability**: Implements the standard gRPC Health Checking Protocol.
- **Named Probes**: Supports `liveness` and `readiness` service names for orchestrator integration.
- **Reflection**: Built-in support for gRPC reflection (enabled by default).

## Functions

`pkg/grpc` (`gtbgrpc`) exports only the config-key adapters below; construction,
start, health-service wiring and registration (`NewServer`, `Start`,
`RegisterHealthService`, `Register`) are `transportgrpc`'s own functions,
taking explicit typed settings rather than a `config.Reader` — see
[go/transport/grpc](https://transport.go.phpboyscout.uk) for those.

- **`ServerSettingsFromConfig(cfg config.Reader, prefix string) ServerSettings`**: GTB adapter helper for one-shot settings resolution. Empty prefix defaults to `server.grpc`.
- **`ObserveServerSettingsFromConfig(cfg config.Binder, prefix string, opts ...config.SectionBindingOption[ServerSettings]) (*config.ObservedSection[ServerSettings], error)`**: GTB adapter helper for reload-aware typed server settings.
- **`NewServerFromReader(cfg config.Reader, opts ...any) (*grpc.Server, error)`**: GTB adapter that resolves `ServerSettings` from existing config and delegates to `transportgrpc.NewServer`.
- **`StartFromReader(cfg config.Reader, logger logger.Logger, srv *grpc.Server, opts ...any) controls.StartFunc`**: GTB adapter that resolves settings and TLS from existing config, then delegates to `transportgrpc.Start`.
- **`DialLocalFromReader(cfg config.Reader, opts ...any) (*grpc.ClientConn, error)`**: see [Client Credentials and Local Dialling](#client-credentials-and-local-dialling).
- **`RegisterFromReader(ctx context.Context, id string, controller controls.Controllable, cfg config.Reader, logger logger.Logger, opts ...any) (*grpc.Server, error)`**: GTB adapter that reads the existing config structure and delegates to `transportgrpc.Register`.

### Server Options

GTB's own `ServerOption` (`gtbgrpc`) selects the config block (and port) a
gRPC server uses, so you can run more than one gRPC server in a process. The
`*FromReader` adapters accept these alongside `transportgrpc.ServerOption` and
`grpc.ServerOption` values:

- **`WithConfigPrefix(prefix string) ServerOption`**: Config prefix for port, reflection and TLS (default `server.grpc`). The keys become `<prefix>.port`, `<prefix>.host`, `<prefix>.reflection`, `<prefix>.tls.*`.
- **`WithPort(port int) ServerOption`**: Explicit listen/dial port, bypassing config lookup (overrides `<prefix>.port` and the `server.port` fallback).

The server also reads a **bind address** from `<prefix>.host` (e.g. `server.grpc.host`),
defaulting to `""`: **all interfaces**, unchanged from prior releases; set it to
`127.0.0.1` to restrict a listener to loopback (`transportgrpc.WithHost` /
`WithBindAddress` at the transport layer). An **unsupported option type** passed to
`StartFromReader` is logged as a WARN rather than silently dropped; the
error-returning constructors (`NewServer`/`Register`/`DialLocal`) reject it. See the
[bind-address migration note](../../reference/migration/v0.x-server-bind-address.md).

```go
// A second gRPC server on its own config block (server.internal.*):
srv, _ := gtbgrpc.RegisterFromReader(ctx, "internal", controller, props.Config.View(), props.Logger,
    gtbgrpc.WithConfigPrefix("server.internal"))

// ...and a gateway/in-process client dialling that same server:
conn, _ := gtbgrpc.DialLocalFromReader(props.Config.View(), gtbgrpc.WithConfigPrefix("server.internal"))
```

### Observing Server Settings

For long-lived GTB composition, bind the config section once and pass the
observed snapshot through a package-owned settings source:

```go
settings, err := gtbgrpc.ObserveServerSettingsFromConfig(
    props.Config,
    "server.grpc",
    config.WithSectionApply(func(change config.SectionChange[gtbgrpc.ServerSettings]) error {
        props.Logger.Info("grpc server settings changed", "version", change.Version)
        return nil
    }),
)
if err != nil {
    return err
}

var source gtbgrpc.ServerSettingsSource = settings
_ = source.Current()
```

`ObserveServerSettingsFromConfig` uses the same resolved config semantics as
`ServerSettingsFromConfig`, including the shared `server.port` fallback. The
binding rehydrates on successful config reloads, increments `Version()` only
when the typed `ServerSettings` snapshot changes, and exposes the latest
immutable value through `Current()`. Existing gRPC servers do not automatically
restart when the port changes; use the observed source for newly constructed
servers or explicit package-level reconfiguration logic.

## Interceptors

> Interceptors are gRPC's expression of the same chain pattern used by HTTP middleware. For the unified transport story: server/client × HTTP/gRPC, the resilience composition rules, and the config-prefix convention. See the [Transport Middleware & Resilience](../concepts/transport-middleware.md) concept.

The interceptor chain API, `LoggingInterceptor` and `RateLimitInterceptor` live
in [`go/transit/grpc`](https://transit.go.phpboyscout.uk) (`transitgrpc`); the
server-side `AuthInterceptor` and the client-side circuit breaker
interceptors live in [`go/transport/grpc`](https://transport.go.phpboyscout.uk)
(`transportgrpc`), and `WithInterceptors` is a `transportgrpc` `RegisterOption`.
Full API, option tables and config keys are on those modules' own reference
pages; GTB's only adapters here are the two config-key mappings below (both
stay in `pkg/grpc`, prefix defaults to `server.grpc`):

- **`RateLimitConfigFromConfig(cfg config.Reader, prefix string) transitgrpc.RateLimitConfig`**: `<prefix>.ratelimit.{requests_per_second,burst,max_tracked_keys}`
- **`CircuitBreakerConfigFromConfig(cfg config.Reader, prefix string) transitgrpc.CircuitBreakerConfig`**: `<prefix>.circuitbreaker.{failure_threshold,cooldown,half_open_max_requests}`

The standard health (`/grpc.health.v1.Health/*`) and reflection
(`/grpc.reflection.v1*`) services are **auto-skipped** by the auth and
rate-limit interceptors so probes keep working; a custom skipper adds to that
set.

`AuthInterceptor` wraps an [`go/authn`](https://authn.go.phpboyscout.uk)
verifier; GTB does not wire it itself. Usage:

```go
authIC, _ := transportgrpc.AuthInterceptor(transportgrpc.WithGRPCBearerVerifier(jwtVerifier))
chain := transitgrpc.NewInterceptorChain(transitgrpc.LoggingInterceptor(log), authIC)
gtbgrpc.RegisterFromReader(ctx, "api", controller, cfg, log, transportgrpc.WithInterceptors(chain))
```

## TLS

The gRPC server supports TLS using the shared hardened configuration from [`pkg/tls`](tls.md) (TLS 1.2 minimum, curated AEAD cipher suites, X25519 curve preference). The TLS listener advertises HTTP/2 via ALPN (`h2`); without it, grpc-go 1.67+ clients (including the [gateway](gateway.md)) refuse the connection with "missing selected ALPN property". The `Register`/`Start` path sets this for you.

### Configuration

TLS configuration cascades: transport-specific keys override the shared defaults:

| Key | Shared Default | gRPC Override |
|-----|---------------|---------------|
| Enabled | `server.tls.enabled` | `server.grpc.tls.enabled` |
| Certificate | `server.tls.cert` | `server.grpc.tls.cert` |
| Private key | `server.tls.key` | `server.grpc.tls.key` |

To use the same certificate for both HTTP and gRPC, configure the shared keys only:

```yaml
server:
  tls:
    enabled: true
    cert: /etc/certs/server.crt
    key: /etc/certs/server.key
```

To use different certificates per transport:

```yaml
server:
  tls:
    enabled: true
    cert: /etc/certs/http.crt
    key: /etc/certs/http.key
  grpc:
    tls:
      cert: /etc/certs/grpc.crt
      key: /etc/certs/grpc.key
```

### Direct Credential Construction

For cases where you need to pass TLS credentials directly to `grpc.NewServer` (e.g. when not using the `Register` helper):

```go
creds, err := transportgrpc.TLSServerCredentials("/path/to/cert.pem", "/path/to/key.pem")
if err != nil {
    return err
}

srv := grpc.NewServer(grpc.Creds(creds))
```

This uses the same shared hardened TLS config from [`pkg/tls`](tls.md) as the automatic setup. (`credentials.NewTLS` advertises `h2` itself, so no explicit ALPN is needed on this path.)

### Client Credentials and Local Dialling

The package also provides the client side, used for example by the [gateway](gateway.md) when it dials the gRPC server over a self-signed or private-CA certificate:

> The dial factory has been extracted to the standalone, framework-free module [`gitlab.com/phpboyscout/go/grpcclient`](https://grpcclient.go.phpboyscout.uk) (`v0.1.0`), which exposes `grpcclient.Dial(target grpcclient.Target, ...)`. The `transportgrpc.DialLocal` helper maps `ServerSettings` + the TLS pair onto a `grpcclient.Target` and calls it; the GTB `gtbgrpc.DialLocalFromReader` config adapter derives that from `config.Reader`. The re-export facade has been removed. Import the owning module directly. See the [facades-removed](../../reference/migration/v0.x-facades-removed.md) and [grpcclient-extraction](../../reference/migration/v0.x-grpcclient-extracted.md) notes.

- **`transportgrpc.TLSClientCredentials(caFiles ...string) (credentials.TransportCredentials, error)`**: client transport credentials trusting the given CA/cert files: the mirror of `TLSServerCredentials`. With no files it trusts the system roots.
- **`transportgrpc.DialLocal(settings ServerSettings, tlsPair gtbtls.Pair, opts ...any) (*grpc.ClientConn, error)`**: dials the local gRPC server from explicit typed settings and TLS information (a thin adapter over `grpcclient.Dial`).
- **`gtbgrpc.DialLocalFromReader(cfg config.Reader, opts ...any) (*grpc.ClientConn, error)`**: GTB config adapter that dials the gRPC server described by `cfg` over the loopback interface, with transport security that matches the server's own config (`<prefix>.tls` cascading to `server.tls`). The variadic accepts both `ServerOption` values (e.g. `WithConfigPrefix` to dial a non-default server) and `grpc.DialOption` values. Intended for in-process callers such as the gateway, so they connect without re-deriving the endpoint or credentials by hand.

```go
// Connect to the local gRPC server with matching transport security in one call.
conn, err := gtbgrpc.DialLocalFromReader(props.Config.View())
if err != nil {
    return err
}
```

### Config Keys

`DefaultConfigPrefix` (`server.grpc`) is the default config block; `WithConfigPrefix` derives `<prefix>.port`, `<prefix>.reflection` and `<prefix>.tls.*` from it. The shared `ConfigKeySharedPort` (`server.port`) is the fallback when the per-server port is unset.

| Constant | Key | Notes |
|----------|-----|-------|
| `DefaultConfigPrefix` | `server.grpc` | Default prefix; override with `WithConfigPrefix`. |
| `ConfigKeySharedPort` | `server.port` | Fallback when the per-server port is unset. |

## Usage Example

```go
// Build an interceptor chain with logging (transit/grpc)
chain := transitgrpc.NewInterceptorChain(
    transitgrpc.LoggingInterceptor(logger.ToSlog(props.Logger),
        transitgrpc.WithGRPCPathFilter("/grpc.health.v1.Health/Check"),
    ),
)

// Register with interceptors via the GTB config adapter; WithInterceptors is a transport/grpc option.
srv, err := gtbgrpc.RegisterFromReader(ctx, "grpc-api", controller, props.Config.View(), props.Logger,
    transportgrpc.WithInterceptors(chain),
)
if err != nil {
    return err
}

// Register your custom services
pb.RegisterMyServiceServer(srv, &myServiceImpl{})
```
