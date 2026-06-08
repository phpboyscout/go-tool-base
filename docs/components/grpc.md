---
title: gRPC
description: Secure-by-default gRPC server components.
date: 2026-03-24
tags: [components, grpc, networking, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# gRPC

The `pkg/grpc` package provides a standard gRPC server implementation that integrates with the `controls` package for lifecycle management and observability.

## Features

- **Standard Observability**: Implements the standard gRPC Health Checking Protocol.
- **Named Probes**: Supports `liveness` and `readiness` service names for orchestrator integration.
- **Reflection**: Built-in support for gRPC reflection (enabled by default).

## Functions

- **`NewServer(cfg config.Containable, opts ...any) (*grpc.Server, error)`**: Returns a new `*grpc.Server` with reflection registered. The variadic accepts both `ServerOption` values (e.g. `WithConfigPrefix`, which selects the config block the reflection flag is read from) and `grpc.ServerOption` values.
- **`Start(cfg config.Containable, logger logger.Logger, srv *grpc.Server, opts ...ServerOption) controls.StartFunc`**: Returns a controller start function. Pass `WithConfigPrefix`/`WithPort` to target a custom server; it logs the bound listener address (so an ephemeral `:0` port surfaces resolved).
- **`RegisterHealthService(srv *grpc.Server, controller controls.Controllable)`**: Wires the gRPC health service to the controller status.
- **`Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, opts ...any) (*grpc.Server, error)`**: Creates a server, registers the health service, adds it to the controller, and returns the server instance. Accepts `ServerOption`, `RegisterOption` and `grpc.ServerOption` values.

### Server Options

`ServerOption` values select the config block (and port) a gRPC server uses, so you can run more than one gRPC server in a process. They are accepted by `NewServer`, `Start`, `DialLocal` and `Register`:

- **`WithConfigPrefix(prefix string) ServerOption`**: Config prefix for port, reflection and TLS (default `server.grpc`). The keys become `<prefix>.port`, `<prefix>.reflection`, `<prefix>.tls.*`.
- **`WithPort(port int) ServerOption`**: Explicit listen/dial port, bypassing config lookup (overrides `<prefix>.port` and the `server.port` fallback).

```go
// A second gRPC server on its own config block (server.internal.*):
srv, _ := gtbgrpc.Register(ctx, "internal", controller, props.Config, props.Logger,
    gtbgrpc.WithConfigPrefix("server.internal"))

// ...and a gateway/in-process client dialling that same server:
conn, _ := gtbgrpc.DialLocal(props.Config, gtbgrpc.WithConfigPrefix("server.internal"))
```

## Interceptor Chaining

The package provides an interceptor chaining API for composing gRPC unary and stream interceptors.

- **`NewInterceptorChain(interceptors ...Interceptor) InterceptorChain`**: Creates a chain from paired unary/stream interceptors.
- **`(c InterceptorChain) Append(interceptors ...Interceptor) InterceptorChain`**: Returns a new chain with additional interceptors (immutable).
- **`(c InterceptorChain) ServerOptions() []grpc.ServerOption`**: Returns `grpc.ChainUnaryInterceptor` and `grpc.ChainStreamInterceptor` options.
- **`WithInterceptors(chain InterceptorChain) RegisterOption`**: Applies an interceptor chain when using `Register`.

## Built-in Logging Interceptor

`LoggingInterceptor` logs each completed RPC with structured fields (method, status code, latency, RPC type).

- **`LoggingInterceptor(logger logger.Logger, opts ...GRPCLoggingOption) Interceptor`**

**Options**: `WithGRPCLogLevel`, `WithoutGRPCLatency`, `WithGRPCPathFilter`.

## TLS

The gRPC server supports TLS using the shared hardened configuration from [`pkg/tls`](tls.md) (TLS 1.2 minimum, curated AEAD cipher suites, X25519 curve preference). The TLS listener advertises HTTP/2 via ALPN (`h2`); without it, grpc-go 1.67+ clients — including the [gateway](gateway.md) — refuse the connection with "missing selected ALPN property". The `Register`/`Start` path sets this for you.

### Configuration

TLS configuration cascades — transport-specific keys override the shared defaults:

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
creds, err := gtbgrpc.TLSServerCredentials("/path/to/cert.pem", "/path/to/key.pem")
if err != nil {
    return err
}

srv := grpc.NewServer(grpc.Creds(creds))
```

This uses the same shared hardened TLS config from [`pkg/tls`](tls.md) as the automatic setup. (`credentials.NewTLS` advertises `h2` itself, so no explicit ALPN is needed on this path.)

### Client Credentials and Local Dialling

The package also provides the client side, used for example by the [gateway](gateway.md) when it dials the gRPC server over a self-signed or private-CA certificate:

- **`TLSClientCredentials(caFiles ...string) (credentials.TransportCredentials, error)`**: client transport credentials trusting the given CA/cert files — the mirror of `TLSServerCredentials`. With no files it trusts the system roots.
- **`DialLocal(cfg config.Containable, opts ...any) (*grpc.ClientConn, error)`**: dials the gRPC server described by `cfg` over the loopback interface, with transport security that matches the server's own config (`<prefix>.tls` cascading to `server.tls`). The variadic accepts both `ServerOption` values (e.g. `WithConfigPrefix` to dial a non-default server) and `grpc.DialOption` values. Intended for in-process callers such as the gateway, so they connect without re-deriving the endpoint or credentials by hand.

```go
// Connect to the local gRPC server with matching transport security in one call.
conn, err := gtbgrpc.DialLocal(props.Config)
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
| `ConfigKeyPort` | `server.grpc.port` | Deprecated — prefer `WithConfigPrefix` / `<prefix>.port`. |
| `ConfigKeyReflection` | `server.grpc.reflection` | Deprecated — prefer `WithConfigPrefix`. |
| `ConfigTLSPrefix` | `server.grpc.tls` | Deprecated — the TLS prefix is `<prefix>.tls`. |

## Usage Example

```go
// Build an interceptor chain with logging
chain := gtbgrpc.NewInterceptorChain(
    gtbgrpc.LoggingInterceptor(props.Logger,
        gtbgrpc.WithGRPCPathFilter("/grpc.health.v1.Health/Check"),
    ),
)

// Register with interceptors
srv, err := gtbgrpc.Register(ctx, "grpc-api", controller, props.Config, props.Logger,
    gtbgrpc.WithInterceptors(chain),
)
if err != nil {
    return err
}

// Register your custom services
pb.RegisterMyServiceServer(srv, &myServiceImpl{})
```
