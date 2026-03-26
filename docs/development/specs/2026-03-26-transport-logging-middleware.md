---
title: "Transport Middleware and Logging Specification"
description: "Middleware chaining for HTTP handlers and gRPC interceptors, with a built-in request logging middleware using the GTB logger interface."
date: 2026-03-26
status: DRAFT
tags:
  - specification
  - http
  - grpc
  - logging
  - middleware
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Opus 4.6
    role: AI drafting assistant
---

# Transport Middleware and Logging Specification

Authors
:   Matt Cockayne, Claude Opus 4.6 *(AI drafting assistant)*

Date
:   26 March 2026

Status
:   DRAFT

---

## Overview

The HTTP and gRPC server packages (`pkg/http`, `pkg/grpc`) currently have no middleware infrastructure. Consumers who want to add cross-cutting concerns (logging, recovery, auth, metrics) must manually compose handler wrappers or gRPC interceptor chains — a pattern that quickly becomes unwieldy as the number of concerns grows.

This spec introduces two things:

1. **Middleware chaining** — a lightweight, composable mechanism (inspired by [justinas/alice](https://github.com/justinas/alice)) for declaring and applying ordered middleware stacks for both HTTP and gRPC, without pulling in an external dependency.
2. **Built-in request logging middleware** — the first middleware shipped with the framework, providing per-request structured logging using `logger.Logger`.

### Motivation

The recent shutdown debugging effort revealed that the lack of request-level logging makes it difficult to reason about in-flight connections during graceful shutdown. More broadly, the absence of any middleware infrastructure means every consumer reinvents handler composition. A minimal chaining API solves both problems.

### Terminology

| Term | Definition |
|------|-----------|
| **HTTP Middleware** | A function with signature `func(http.Handler) http.Handler` — the standard Go middleware convention. |
| **gRPC Interceptor** | A `grpc.UnaryServerInterceptor` or `grpc.StreamServerInterceptor` — the standard gRPC middleware convention. |
| **Chain** | An ordered collection of middleware/interceptors that composes into a single wrapper. |
| **Transport** | Either HTTP or gRPC — the two server transports GTB supports. |

### Design Decisions

1. **No external dependency**: The chaining mechanism is trivial to implement (~30 lines per transport). No need for `justinas/alice` as a dependency — we adopt its ergonomics, not its code.
2. **Standard signatures**: HTTP middleware uses `func(http.Handler) http.Handler`. gRPC uses the native interceptor types. No custom abstractions that fight the ecosystem.
3. **Separate chains per transport**: HTTP and gRPC have different middleware signatures. Each gets its own `Chain` type rather than a shared abstraction.
4. **Opt-in composition**: Chains are built explicitly by the consumer. The `Register` convenience functions gain an optional `WithMiddleware`/`WithInterceptors` option so consumers can declare their stack at registration time.
5. **Logging middleware is built-in but not default**: Shipped with the framework as a ready-to-use middleware, but not wired in unless the consumer includes it in their chain.

---

## Public API

### Middleware Chaining

#### Package `pkg/http`

```go
// Middleware is the standard Go HTTP middleware signature.
type Middleware func(http.Handler) http.Handler

// Chain composes zero or more Middleware into a single Middleware.
// Middleware is applied left-to-right: the first middleware in the list is
// the outermost wrapper (first to see the request, last to see the response).
//
//   chain := gtbhttp.Chain(recovery, logging, auth)
//   handler := chain.Then(mux)
type Chain struct {
    middlewares []Middleware
}

// NewChain creates a new middleware chain from the given middleware functions.
func NewChain(middlewares ...Middleware) Chain

// Append returns a new Chain with additional middleware appended.
// The original chain is not modified.
func (c Chain) Append(middlewares ...Middleware) Chain

// Extend returns a new Chain that applies c's middleware first, then other's.
func (c Chain) Extend(other Chain) Chain

// Then applies the middleware chain to the given handler and returns
// the resulting http.Handler.
//
// If handler is nil, http.DefaultServeMux is used.
func (c Chain) Then(handler http.Handler) http.Handler

// ThenFunc is a convenience for Then(http.HandlerFunc(fn)).
func (c Chain) ThenFunc(fn http.HandlerFunc) http.Handler
```

#### Package `pkg/grpc`

```go
// InterceptorChain composes zero or more gRPC interceptors into ordered
// slices suitable for grpc.ChainUnaryInterceptor and grpc.ChainStreamInterceptor.
type InterceptorChain struct {
    unary  []grpc.UnaryServerInterceptor
    stream []grpc.StreamServerInterceptor
}

// NewInterceptorChain creates a new interceptor chain.
// Each Interceptor argument provides a unary interceptor, a stream interceptor,
// or both. Nil entries are silently skipped.
func NewInterceptorChain(interceptors ...Interceptor) InterceptorChain

// Interceptor groups a paired unary and stream interceptor.
// Either field may be nil if the interceptor only applies to one RPC type.
type Interceptor struct {
    Unary  grpc.UnaryServerInterceptor
    Stream grpc.StreamServerInterceptor
}

// Append returns a new InterceptorChain with additional interceptors appended.
func (c InterceptorChain) Append(interceptors ...Interceptor) InterceptorChain

// ServerOptions returns grpc.ServerOption values that install the chain.
// This is the primary integration point — pass the result to grpc.NewServer
// or to gtbgrpc.NewServer's variadic options.
//
//   chain := gtbgrpc.NewInterceptorChain(logging, recovery)
//   srv, _ := gtbgrpc.NewServer(cfg, chain.ServerOptions()...)
func (c InterceptorChain) ServerOptions() []grpc.ServerOption
```

### Registration Integration

Both `Register` functions gain optional configuration for middleware:

#### `pkg/http`

```go
// RegisterOption configures optional behaviour for HTTP server registration.
type RegisterOption func(*registerConfig)

// WithMiddleware sets the middleware chain applied to the handler before
// it is passed to the HTTP server. Health endpoints (/healthz, /livez,
// /readyz) are mounted outside the chain and are never affected by middleware.
func WithMiddleware(chain Chain) RegisterOption

// Register signature gains variadic options:
func Register(ctx context.Context, id string, controller controls.Controllable,
    cfg config.Containable, logger logger.Logger, handler http.Handler,
    opts ...RegisterOption) (*http.Server, error)
```

#### `pkg/grpc`

```go
// RegisterOption configures optional behaviour for gRPC server registration.
type RegisterOption func(*registerConfig)

// WithInterceptors prepends the given interceptor chain before any
// grpc.ServerOption interceptors passed via the variadic opts.
func WithInterceptors(chain InterceptorChain) RegisterOption

// Register signature gains RegisterOption alongside existing grpc.ServerOption:
func Register(ctx context.Context, id string, controller controls.Controllable,
    cfg config.Containable, logger logger.Logger,
    opts ...any) (*grpc.Server, error)
// opts accepts both grpc.ServerOption and RegisterOption values.
```

---

### Built-in Logging Middleware

#### Package `pkg/http`

```go
// LoggingMiddleware returns an HTTP Middleware that logs each completed request.
func LoggingMiddleware(logger logger.Logger, opts ...LoggingOption) Middleware
```

#### Package `pkg/grpc`

```go
// LoggingInterceptor returns an Interceptor (unary + stream) that logs
// each completed RPC.
func LoggingInterceptor(logger logger.Logger, opts ...LoggingOption) Interceptor
```

#### Logging Options

Options are defined in each transport package but follow the same naming and semantics.

```go
// LoggingOption configures transport logging behaviour.
type LoggingOption func(*loggingConfig)

// WithLogLevel sets the log level for successful requests.
// Defaults to logger.InfoLevel. Errors always log at logger.ErrorLevel.
func WithLogLevel(level logger.Level) LoggingOption

// WithoutLatency disables the "latency" field.
func WithoutLatency() LoggingOption

// WithoutUserAgent disables the "user_agent" field (HTTP only).
func WithoutUserAgent() LoggingOption

// WithPathFilter excludes requests matching the given paths from logging.
// Useful for suppressing noisy health-check endpoints.
//
//   WithPathFilter("/healthz", "/livez", "/readyz")
func WithPathFilter(paths ...string) LoggingOption

// WithHeaderFields logs the specified request header values as fields.
// Header names are normalised to lowercase. Values are truncated to 256 bytes.
//
//   WithHeaderFields("x-request-id", "x-forwarded-for")
func WithHeaderFields(headers ...string) LoggingOption
```

### Log Fields

Each request log contains structured fields. All fields are optional and controllable via options.

#### HTTP

| Field | Type | Example | Description |
|-------|------|---------|-------------|
| `method` | string | `GET` | HTTP method |
| `path` | string | `/api/health` | Request path (without query string) |
| `status` | int | `200` | Response status code |
| `latency` | duration | `12.3ms` | Time from handler entry to response write |
| `bytes` | int | `1024` | Response body size in bytes |
| `client_ip` | string | `10.0.0.1` | Client IP from `RemoteAddr` or `X-Forwarded-For` |
| `user_agent` | string | `curl/8.0` | `User-Agent` header value |
| `request_id` | string | `abc-123` | From header if `WithHeaderFields` configured |

#### gRPC

| Field | Type | Example | Description |
|-------|------|---------|-------------|
| `method` | string | `/pkg.Service/DoThing` | Full gRPC method name |
| `code` | string | `OK` | gRPC status code name |
| `latency` | duration | `5.1ms` | Time from handler entry to response |
| `type` | string | `unary` / `stream` | RPC type |
| `peer` | string | `10.0.0.1:54321` | Peer address from transport credentials |

---

### Usage Examples

#### HTTP — composing a middleware stack

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/data", dataHandler)

// Build a middleware chain
chain := gtbhttp.NewChain(
    gtbhttp.RecoveryMiddleware(l),  // outermost — catches panics from everything below
    gtbhttp.LoggingMiddleware(l,
        gtbhttp.WithPathFilter("/healthz", "/livez", "/readyz"),
        gtbhttp.WithHeaderFields("x-request-id"),
    ),
    authMiddleware,                  // application-specific
)

// Option A: apply manually
srv, _ := gtbhttp.NewServer(ctx, cfg, chain.Then(mux))

// Option B: apply via Register
_, _ = gtbhttp.Register(ctx, "http", controller, cfg, l, mux,
    gtbhttp.WithMiddleware(chain),
)
```

#### HTTP — extending chains

```go
// Base chain shared across all services
base := gtbhttp.NewChain(
    gtbhttp.RecoveryMiddleware(l),
    gtbhttp.LoggingMiddleware(l),
)

// Admin routes get additional auth
admin := base.Append(adminAuthMiddleware)

adminHandler := admin.Then(adminMux)
publicHandler := base.Then(publicMux)
```

#### gRPC — composing interceptors

```go
chain := gtbgrpc.NewInterceptorChain(
    gtbgrpc.LoggingInterceptor(l,
        gtbgrpc.WithPathFilter("/grpc.health.v1.Health/Check"),
    ),
    gtbgrpc.Interceptor{Unary: authUnaryInterceptor},  // unary-only
)

// Option A: apply via ServerOptions
srv, _ := gtbgrpc.NewServer(cfg, chain.ServerOptions()...)

// Option B: apply via Register
srv, _ := gtbgrpc.Register(ctx, "grpc", controller, cfg, l,
    gtbgrpc.WithInterceptors(chain),
)
```

---

## Internal Implementation

### HTTP Chain

The `Chain` type is a simple slice of `Middleware`. `Then` applies them in reverse order so the first middleware in the list is the outermost wrapper:

```go
func (c Chain) Then(h http.Handler) http.Handler {
    for i := len(c.middlewares) - 1; i >= 0; i-- {
        h = c.middlewares[i](h)
    }
    return h
}
```

`Append` and `Extend` return new slices — chains are immutable after creation.

### gRPC InterceptorChain

Maintains two parallel slices (`unary` and `stream`). `ServerOptions` returns:

```go
func (c InterceptorChain) ServerOptions() []grpc.ServerOption {
    var opts []grpc.ServerOption
    if len(c.unary) > 0 {
        opts = append(opts, grpc.ChainUnaryInterceptor(c.unary...))
    }
    if len(c.stream) > 0 {
        opts = append(opts, grpc.ChainStreamInterceptor(c.stream...))
    }
    return opts
}
```

### HTTP `loggingConfig`

```go
type loggingConfig struct {
    level        logger.Level
    logLatency   bool
    logUserAgent bool
    pathFilter   map[string]struct{}
    headerFields []string
}
```

The middleware wraps `http.ResponseWriter` with a thin interceptor that captures `statusCode` and `bytesWritten` via `WriteHeader` and `Write` overrides. After the inner handler returns, a single structured log call is emitted.

### Response Writer Wrapper (HTTP)

```go
type responseLogger struct {
    http.ResponseWriter
    statusCode   int
    bytesWritten int
    wroteHeader  bool
}
```

Must implement `http.Flusher` and `http.Hijacker` if the underlying writer supports them, to avoid breaking WebSocket upgrades or SSE.

### gRPC `loggingConfig`

Shares the same shape as the HTTP config. The unary interceptor wraps the handler call; the stream interceptor wraps `grpc.ServerStream` to capture completion. Both extract the method name from `info.FullMethod` and the peer address from `peer.FromContext`.

---

## Project Structure

```
pkg/http/
    chain.go            # Chain type + NewChain, Append, Extend, Then
    chain_test.go
    logging.go          # LoggingMiddleware + options + responseLogger
    logging_test.go
    options.go          # RegisterOption, WithMiddleware
    options_test.go

pkg/grpc/
    chain.go            # InterceptorChain + NewInterceptorChain, Append, ServerOptions
    chain_test.go
    logging.go          # LoggingInterceptor + options
    logging_test.go
    options.go          # RegisterOption, WithInterceptors
    options_test.go
```

No new packages. Middleware infrastructure lives alongside the server code it wraps.

---

## Generator Impact

None. The generator scaffolds server registration but does not prescribe middleware. Consumers add middleware explicitly.

---

## Error Handling

- Chain types do not produce errors. A nil middleware or nil interceptor is silently skipped.
- The logging middleware itself does not produce errors. If the underlying handler panics, the panic propagates as normal (recovery is the responsibility of a separate recovery middleware).
- Failed requests (5xx) are logged at `logger.ErrorLevel` regardless of the configured level. 4xx requests use the configured level (default `Info`).

---

## Testing Strategy

### Unit Tests

- **Chain (HTTP)**: Verify ordering — first middleware is outermost. Verify `Append` returns a new chain (immutability). Verify `Then(nil)` uses `DefaultServeMux`. Verify `ThenFunc` convenience.
- **Chain (gRPC)**: Verify `ServerOptions` produces correct `ChainUnaryInterceptor`/`ChainStreamInterceptor` options. Verify nil interceptors are skipped.
- **Logging (HTTP)**: Use `httptest.NewRecorder` with a known handler. Assert log output contains expected fields (method, path, status, latency). Verify path filtering suppresses output. Verify header field extraction.
- **Logging (gRPC)**: Use `bufconn` with a test service. Assert log output for unary and streaming RPCs. Verify method filtering and peer extraction.
- **Options**: Each option has a dedicated test verifying its effect on log output.
- **Register integration**: Verify `WithMiddleware`/`WithInterceptors` apply the chain correctly and that health endpoints remain unaffected.

### Integration Tests

- Wire middleware through `Register` into a full controller lifecycle. Verify logs appear during normal operation and during graceful shutdown with in-flight requests.

### Coverage Target

90% for all new files.

---

## Migration & Compatibility

- **No breaking changes**. All additions are additive and opt-in.
- Existing `Register` function signatures gain variadic options but remain backwards-compatible — zero options produces identical behaviour to today.
- Existing consumers who manually wrap handlers continue to work unchanged.
- The `Chain` and `InterceptorChain` types are transport-specific to allow future transport-specific extensions without coupling.

---

## Future Considerations

- **Recovery middleware**: A `RecoveryMiddleware(logger)` that catches panics and converts them to 500/INTERNAL errors. Likely the next built-in middleware after logging.
- **Request ID middleware**: Generates `X-Request-Id` if not present, which the logging middleware picks up via `WithHeaderFields`.
- **Metrics extraction**: The same `responseLogger` wrapper could feed latency histograms to a metrics middleware. Keep interfaces clean so they can compose.
- **Sampling**: A `WithSampler(rate float64)` logging option could be added later without changing the core interface.
- **Body logging**: Intentionally excluded for v1 (security and performance). Could be added behind a `WithBodyLogging(maxBytes int)` option for debug use.
- **Conditional chains**: A `Chain.If(condition bool, middleware...)` method for conditionally including middleware based on config flags.

---

## Implementation Phases

### Phase 1: HTTP Middleware Chaining

1. Implement `Chain` type with `NewChain`, `Append`, `Extend`, `Then`, `ThenFunc`.
2. Implement `RegisterOption` and `WithMiddleware` for `Register`.
3. Unit tests for chain ordering, immutability, and nil handling.

### Phase 2: gRPC Interceptor Chaining

1. Implement `InterceptorChain` type with `NewInterceptorChain`, `Append`, `ServerOptions`.
2. Implement `Interceptor` type and `RegisterOption`/`WithInterceptors`.
3. Unit tests for interceptor chain composition and `ServerOptions` output.

### Phase 3: HTTP Logging Middleware

1. Implement `responseLogger` wrapper with status/bytes capture.
2. Implement `LoggingMiddleware` with default fields.
3. Implement options: `WithLogLevel`, `WithoutLatency`, `WithPathFilter`.
4. Unit tests with `httptest`.

### Phase 4: gRPC Logging Interceptor

1. Implement unary interceptor with method/code/latency fields.
2. Implement stream interceptor wrapping `grpc.ServerStream`.
3. Implement options: `WithLogLevel`, `WithoutLatency`, `WithPathFilter`.
4. Unit tests with `bufconn`.

### Phase 5: Extended Options and Integration

1. `WithHeaderFields` (HTTP) and `WithoutUserAgent` (HTTP).
2. Peer address extraction (gRPC).
3. Client IP extraction with `X-Forwarded-For` support (HTTP).
4. Integration tests wired through the controller lifecycle.

---

## Open Questions

1. **Register API for gRPC**: The current `Register` accepts `...grpc.ServerOption`. Adding `RegisterOption` alongside requires either a mixed variadic (`...any` with type-switching) or a separate options parameter. The spec proposes `...any` for simplicity — is this acceptable, or should we use a separate `RegisterWithOptions` function to keep type safety?
2. **Should 4xx responses log at `Warn` level by default?** Currently proposed as `Info`. Some teams prefer `Warn` for client errors to surface them more visibly.
3. **gRPC metadata logging**: Should there be a `WithMetadataFields` option analogous to `WithHeaderFields`, or is this too niche for v1?
4. **Health endpoint exclusion**: The spec proposes that `WithMiddleware` in `Register` mounts health endpoints outside the chain. Should consumers be able to opt health endpoints into the middleware chain (e.g. for access logging on health checks)?
