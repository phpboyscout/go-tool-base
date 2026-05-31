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

- **`NewServer(cfg config.Containable, opt ...grpc.ServerOption) (*grpc.Server, error)`**: Returns a new `*grpc.Server` with reflection registered.
- **`RegisterHealthService(srv *grpc.Server, controller controls.Controllable)`**: Wires the gRPC health service to the controller status.
- **`Register(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, opts ...any) (*grpc.Server, error)`**: Creates a server, registers the health service, adds it to the controller, and returns the server instance. Accepts both `grpc.ServerOption` and `RegisterOption` values.

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
- **`DialLocal(cfg config.Containable, opts ...grpc.DialOption) (*grpc.ClientConn, error)`**: dials the gRPC server described by `cfg` over the loopback interface, with transport security that matches the server's own config (`server.grpc.tls` cascading to `server.tls`). Intended for in-process callers such as the gateway, so they connect without re-deriving the endpoint or credentials by hand.

```go
// Connect to the local gRPC server with matching transport security in one call.
conn, err := gtbgrpc.DialLocal(props.Config)
if err != nil {
    return err
}
```

### Config Keys

Exported constants for the gRPC server's config keys (prefer these over bare strings):

| Constant | Key |
|----------|-----|
| `ConfigKeyPort` | `server.grpc.port` |
| `ConfigKeyReflection` | `server.grpc.reflection` |
| `ConfigKeySharedPort` | `server.port` (fallback when the port is unset) |
| `ConfigTLSPrefix` | `server.grpc.tls` |

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
