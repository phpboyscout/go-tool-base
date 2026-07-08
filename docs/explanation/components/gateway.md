---
title: Gateway
description: grpc-gateway as a first-class transport for serving generated REST handlers.
date: 2026-05-31
tags: [components, gateway, grpc, http, networking]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Gateway

The `pkg/gateway` package makes a [grpc-gateway](https://github.com/grpc-ecosystem/grpc-gateway) a first-class transport. It dials the local gRPC server — matching the server's own transport security — and serves the generated REST handlers, either mounted on an existing HTTP server or as its own controller-managed HTTP server.

This gives you a JSON/REST surface over an existing gRPC service without standing up a separate, hand-written translation layer. The only gateway-specific code you write is a single registration function.

## The RegisterFunc

The caller supplies a `RegisterFunc` that wires the generated gateway handlers onto the mux using a client connection to the gRPC server. This is the only gateway-specific code a caller writes, and it is typically a one-liner calling the generated `RegisterXServiceHandler`.

```go
// RegisterFunc registers the generated gateway handlers onto the mux, using a
// client connection to the gRPC server.
type RegisterFunc func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error
```

A realistic implementation:

```go
register := func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
    return widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
}
```

## Functions

- **`New(ctx context.Context, conn *grpc.ClientConn, register RegisterFunc, opts ...Option) (http.Handler, error)`**: Builds a grpc-gateway handler ready to mount on an existing HTTP server from an already prepared gRPC client connection.
- **`SettingsFromConfig(cfg config.Containable) Settings`**: GTB adapter helper that composes typed HTTP settings/TLS from `server.gateway.*` and typed gRPC dial settings/TLS from `server.grpc.*`.
- **`ObserveSettingsFromConfig(cfg config.Containable, opts ...config.SectionBindingOption[Settings]) (*config.ObservedSection[Settings], error)`**: GTB adapter helper for reload-aware typed gateway transport settings.
- **`NewFromConfig(ctx context.Context, cfg config.Containable, register RegisterFunc, opts ...Option) (http.Handler, error)`**: GTB adapter that resolves typed gRPC dial settings from existing config, then delegates to `New`.
- **`NewFromContainable(ctx context.Context, cfg config.Containable, register RegisterFunc, opts ...Option) (http.Handler, error)`**: GTB adapter that dials the local gRPC server from existing config, then delegates to `New`.
- **`Register(ctx context.Context, id string, controller controls.Controllable, logger logger.Logger, conn *grpc.ClientConn, httpSettings gtbhttp.ServerSettings, httpTLS gtbtls.Pair, register RegisterFunc, opts ...Option) (*http.Server, error)`**: Runs the gateway as its own controller-managed HTTP server from explicit typed HTTP settings and a prepared gRPC connection.
- **`RegisterFromConfig(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, register RegisterFunc, opts ...Option) (*http.Server, error)`**: GTB adapter that resolves typed HTTP/gRPC transport settings from existing config, then delegates to the typed registration path.
- **`RegisterFromContainable(ctx context.Context, id string, controller controls.Controllable, cfg config.Containable, logger logger.Logger, register RegisterFunc, opts ...Option) (*http.Server, error)`**: GTB adapter that reads the existing `server.gateway` config block and delegates to the typed HTTP registration path.

## Middleware on the REST surface

The gateway handler is an ordinary `http.Handler`, so the
[HTTP server middleware chain](http.md#middleware-chaining) — logging, security
headers, rate limiting, auth — applies to it like any other handler. Pass
`WithMiddleware` to either entry point:

```go
chain := gtbhttp.NewChain(
    gtbhttp.SecurityHeadersMiddleware(),
    gtbhttp.RateLimitMiddleware(log, gtbhttp.DefaultRateLimitConfig()),
)

// Managed server (RegisterFromContainable): the chain wraps the REST routes; health endpoints
// (/healthz, /livez, /readyz) stay OUTSIDE it, exactly as with http.Register.
srv, _ := gateway.RegisterFromContainable(ctx, "gateway", controller, cfg, log, registerFn,
    gateway.WithMiddleware(chain))

// Mount-on-existing-server (NewFromContainable): the chain wraps the returned handler directly.
handler, _ := gateway.NewFromContainable(ctx, cfg, registerFn, gateway.WithMiddleware(chain))
```

- **`WithMiddleware(chain http.Chain) Option`**: wraps the gateway's REST surface
  with an HTTP middleware chain. On the `RegisterFromContainable` path it is threaded to the
  managed server so health probes remain unauthenticated/unthrottled; on the
  `NewFromContainable` path it wraps the returned handler.

See the [Transport Middleware & Resilience](../concepts/transport-middleware.md)
concept for the full pattern.

## Configuration

When run via `RegisterFromConfig` or `RegisterFromContainable`, the gateway
server reads its own config block:

```go
const ConfigPrefix = "server.gateway"
```

Port and TLS come from `server.gateway.*`, and TLS falls back to the shared `server.tls` defaults (following the same cascade as the other transports).

```yaml
server:
  gateway:
    port: 8081
  tls:
    enabled: true
    cert: /etc/certs/server.crt
    key: /etc/certs/server.key
```

With the shared `server.tls` keys set and no `server.gateway.tls` override, the gateway server uses the same certificate as the rest of the stack.

### Observing Gateway Settings

Gateway settings are a composition of the HTTP server it owns and the gRPC
server it dials. Bind that composition once when GTB code needs a reload-aware
source:

```go
settings, err := gateway.ObserveSettingsFromConfig(
    props.Config,
    config.WithSectionApply(func(change config.SectionChange[gateway.Settings]) error {
        props.Logger.Info("gateway settings changed", "version", change.Version)
        return nil
    }),
)
if err != nil {
    return err
}

var source gateway.SettingsSource = settings
_ = source.Current()
```

The source rehydrates `server.gateway.*`, `server.gateway.tls.*`,
`server.grpc.*`, and `server.grpc.tls.*` on successful config reloads. Existing
gateway servers and gRPC client connections do not automatically restart or
redial; use the observed source for new construction or explicit package-level
reconfiguration logic.

## Options

| Option | Description |
|--------|-------------|
| `WithMuxOptions(opts ...runtime.ServeMuxOption)` | Passes `runtime.ServeMuxOption` values to the gateway mux (e.g. a custom error handler or header matcher). |
| `WithDialOptions(opts ...grpc.DialOption)` | Passes extra `grpc.DialOption` values to the connection the gateway opens to the gRPC server. Transport security is set automatically. |
| `WithMiddleware(chain http.Chain)` | Wraps the REST surface with an HTTP middleware chain. On `Register` the chain wraps the routes while health endpoints stay outside it; on `New` it wraps the returned handler. See [Middleware on the REST surface](#middleware-on-the-rest-surface). |

## Usage Example: mounted on an existing server

Use `NewFromContainable` when the REST handlers should share an origin with other routes and GTB config should prepare the local gRPC connection for you — for example, serving the API alongside the OpenAPI docs on a single mux.

```go
register := func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
    return widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
}

gw, err := gateway.NewFromContainable(ctx, props.Config, register)
if err != nil {
    return err
}

mux := http.NewServeMux()
mux.Handle("/v1/", gw)            // REST handlers (annotations are /v1/...; do not strip the prefix)
mux.Handle("/docs/", docsHandler) // OpenAPI docs, same origin

srv, err := gtbhttp.RegisterFromContainable(ctx, "http-api", controller, props.Config, props.Logger, mux)
if err != nil {
    return err
}
```

## Usage Example: as its own server

Use `RegisterFromContainable` to stand the gateway up as a controller-managed HTTP server on the `server.gateway` config block, peer to the gRPC and HTTP servers.

```go
register := func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
    return widgetv1.RegisterWidgetServiceHandler(ctx, mux, conn)
}

srv, err := gateway.RegisterFromContainable(ctx, "gateway", controller, props.Config, props.Logger, register)
if err != nil {
    return err
}
```

## Dependencies

The package builds on the rest of the web-service stack:

- **`pkg/grpc`** — `DialLocal` opens the connection to the local gRPC server with matching transport security.
- **`pkg/http`** — `Register` and `WithConfigPrefix` host the gateway as a controller-managed server when using `Register`.
- **grpc-gateway/v2 `runtime`** — provides the `ServeMux` and the generated handler registration.
