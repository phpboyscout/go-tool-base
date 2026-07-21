---
title: Transport Middleware & Resilience
description: Middleware and interceptor chains as the single extension point for cross-cutting transport concerns across HTTP and gRPC, server and client.
date: 2026-06-23
tags: [concepts, middleware, http, grpc, resilience, rate-limiting, circuit-breaker]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Transport Middleware & Resilience

GTB owns the `*http.Server`, the `*grpc.Server`, their outbound clients, and the
grpc-gateway that bridges them. Every cross-cutting concern that wraps a request
on the way in or out — logging, security headers, authentication, rate limiting,
retry, circuit breaking, OpenTelemetry — is expressed as **one pattern**: a
composable middleware (HTTP) or interceptor (gRPC) added to a chain at
registration time. Learn the pattern once and it applies to all four transport
surfaces.

!!! info "The middleware lives in `go/transit`; the servers in `go/transport`"
    The HTTP/gRPC middleware and interceptors described here — logging, OpenTelemetry,
    circuit breaking, rate limiting, client retry — plus the circuit-breaker and
    rate-limiter primitives, live in the standalone, framework-free module
    **[`gitlab.com/phpboyscout/go/transit`](https://transit.go.phpboyscout.uk)** (`v0.1.0`),
    under its `transit/http` and `transit/grpc` subpackages. The hardened servers that
    apply them (`Register`, `WithMiddleware`, `SecurityHeadersMiddleware`, `AuthMiddleware`,
    `AuthInterceptor`) live in **[`gitlab.com/phpboyscout/go/transport`](https://transport.go.phpboyscout.uk)**,
    and the outbound clients in `go/httpclient` / `go/grpcclient`. GTB's `pkg/http` and
    `pkg/grpc` keep only the `config.Reader` adapters — the earlier re-export facade has
    been removed, so import the owning module directly. See the
    [facades-removed migration note](../../reference/migration/v0.x-facades-removed.md).

!!! note "This is the transport sibling of [Command Middleware](../components/setup/middleware.md)"
    [Command Middleware](../components/setup/middleware.md) covers the cobra **CLI** chain —
    cross-cutting concerns for command execution (`RunE`). *This* doc covers the
    **HTTP/gRPC transport** chains. They share a philosophy (a wrapping chain of
    composable units) but are entirely separate systems with separate types. If
    you searched for "middleware" and want CLI commands, you want the other page.

## One pattern, four surfaces

| Surface | Unit type | Chain type | Applied via | Reference |
|---------|-----------|-----------|-------------|-----------|
| HTTP server (ingress) | `transithttp.Middleware` | `transithttp.Chain` | `transporthttp.WithMiddleware(chain)` on `transporthttp.Register` | [http.md](../components/http.md) |
| HTTP client (egress) | `transithttp.ClientMiddleware` (a `RoundTripper` decorator) | `transithttp.ClientChain` | `httpclient.WithClientMiddleware(chain)` on `httpclient.NewClient` | [http.md](../components/http.md) |
| gRPC server (ingress) | `transitgrpc.Interceptor` (unary + stream) | `transitgrpc.InterceptorChain` | `transportgrpc.WithInterceptors(chain)` on `transportgrpc.Register` | [grpc.md](../components/grpc.md) |
| gRPC client (egress) | `grpc.UnaryClientInterceptor` / `StreamClientInterceptor` | dial options | `grpc.WithChainUnaryInterceptor(...)` on `grpcclient.Dial` | [grpc.md](../components/grpc.md) |

The `transithttp` / `transitgrpc` aliases are `gitlab.com/phpboyscout/go/transit/http` and
`.../transit/grpc`; `transporthttp` / `transportgrpc` are the `go/transport` server subpackages;
`httpclient` / `grpcclient` are the standalone client modules. GTB's `pkg/http` / `pkg/grpc`
contribute only the `config.Reader` adapters (`RateLimitConfigFromConfig`, `RegisterFromReader`, …).

## The shared shape

Every chain follows the same rules, so muscle memory transfers between transports:

- **Immutable composition.** `NewChain(...)` / `NewInterceptorChain(...)` build a
  chain; `Append`/`Extend` return a *new* chain rather than mutating. The first
  unit in the list is the outermost wrapper — first to see the request, last to
  see the response.
- **`With*` builders.** Each concern ships as a `With*`/`*Middleware` constructor
  returning a unit value (`SecurityHeadersMiddleware()`, `RateLimitMiddleware(...)`,
  `WithCircuitBreaker(...)`, `LoggingInterceptor(...)`). You assemble the policy by
  listing the units you want.
- **Applied at registration.** Chains reach a server through a single
  `RegisterOption`; nothing is wired implicitly. A server that registers no chain
  behaves exactly as it does today — **all middleware is opt-in**.
- **Health endpoints sit outside the chain.** `http.Register` mounts `/healthz`,
  `/livez`, `/readyz` *outside* `WithMiddleware`, so a global rate limiter or auth
  middleware never gates liveness/readiness probes. The gRPC limiter/auth
  equivalents skip the health and reflection services by the same principle.

## The cross-cutting concerns catalogue

| Concern | Side | Unit | Where documented |
|---------|------|------|-------------------|
| Structured request logging | server + client | `LoggingMiddleware` / `WithRequestLogging` / `LoggingInterceptor` | [http.md](../components/http.md), [grpc.md](../components/grpc.md) |
| Security response headers | HTTP server | `SecurityHeadersMiddleware` | [http.md](../components/http.md) |
| OpenTelemetry tracing/metrics | server + client | `OTelMiddleware` / `OTelStatsHandler` | [observability.md](../components/observability.md) |
| Credential injection | HTTP client | `WithBearerToken` / `WithBasicAuth` (host-pinned) | [http.md](../components/http.md) |
| **Rate limiting** | server (ingress) | `RateLimitMiddleware` / `RateLimitInterceptor` | [http.md](../components/http.md), [grpc.md](../components/grpc.md) |
| **Retry with backoff** | HTTP client | `WithRetry` | [http.md](../components/http.md) |
| **Circuit breaking** | client (egress) | `WithCircuitBreaker` / `CircuitBreakerInterceptor` | [http.md](../components/http.md), [grpc.md](../components/grpc.md) |

The first four are observability/identity concerns; the last three are the
**resilience** primitives, and their *composition* has rules worth internalising.

## Resilience composition rules

Rate limiting, retry, and circuit breaking are three primitives that complete
each other. Get the topology and ordering right and they form a closed loop;
get them wrong and they fight.

**Topology — limiter at ingress, breaker at egress.** A *rate limiter* protects
the thing receiving load, so it lives on the **server** (ingress). A *circuit
breaker* protects the caller from a sick callee, so it lives on the **client**
(egress). GTB does not offer the inverted variants — putting either on the wrong
side is a category error.

**Ordering — the breaker sits outside retry.** Place the breaker in the client
chain so the layering is:

```
request → [circuit breaker] → [retry (backoff)] → [base transport] → network
```

This is load-bearing:

- The breaker sees the **final post-retry verdict**. One logical call that
  exhausts its retry budget against a dead service counts as **one** breaker
  failure, not N.
- Once the breaker is **Open**, calls are rejected *before* entering the retry
  layer — no backoff sleeps are spent on a service already known to be down.

**The closed loop — 429 / Retry-After.** A server-side rate limiter rejects with
`429 Too Many Requests` + `Retry-After`; the client's retry layer *honours* that
header (clamped to its `MaxBackoff`). Ingress back-pressure and egress retry
cooperate without any shared state.

**A rate-limit signal is not a health signal.** By deliberate default, a **429
does not trip the breaker** (HTTP), and neither does gRPC **`ResourceExhausted`**
— they mean "slow down", which is retry's domain, not "the downstream is
unhealthy". This keeps a server's own rate limiter from tripping its callers'
circuit breakers. Both are overridable via the `IsFailure` classifier.

**The breaker never caches.** An open breaker returns an error (`ErrCircuitOpen`
on HTTP, `codes.Unavailable` on gRPC) — it never serves a stored response. GTB
ships no response cache; resilience and caching are separate stories.

## Configuration shape

The resilience primitives mirror the [TLS](../components/tls.md) cascade:
GTB supplies the *mechanism* and sane defaults; the operator supplies the
*policy numbers* via config, under a per-transport prefix.

| Prefix | Read by |
|--------|---------|
| `server.http.ratelimit.*` | `http.RateLimitConfigFromConfig` |
| `server.http.circuitbreaker.*` | `http.CircuitBreakerConfigFromConfig` |
| `server.grpc.ratelimit.*` | `grpc.RateLimitConfigFromConfig` |
| `server.grpc.circuitbreaker.*` | `grpc.CircuitBreakerConfigFromConfig` |

Only the **policy numbers** are config-driven. Code-only fields — `KeyFunc`,
`OnLimited`, `OnStateChange`, `IsFailure` — are never read from config; wiring
stays explicit, consistent with "no implicit config activation".

## The gateway is just an `http.Handler`

The [grpc-gateway](../components/gateway.md) re-exposes gRPC services as REST. Its
output is an ordinary `http.Handler`, so the **HTTP server chain applies to it
like any other handler**. Pass `gateway.WithMiddleware(chain)` to either entry
point: on `gateway.Register` the chain wraps the REST routes while the health
endpoints stay outside it (as with `http.Register`); on `gateway.New` it wraps
the returned handler for mounting on your own server.

## See also

- [HTTP Transport](../components/http.md) — full reference for every server and
  client middleware, retry, and the circuit breaker
- [gRPC Transport](../components/grpc.md) — interceptors, the rate-limit
  interceptor, and the client circuit breaker
- [Observability](../components/observability.md) — OTel as a chain entry
- [TLS](../components/tls.md) — the *other* shared transport concern
- [Service Orchestration](service-orchestration.md) — how these servers attach to
  the controller lifecycle
- [Command Middleware](../components/setup/middleware.md) — the CLI sibling of this pattern
- [Functional Options](functional-options.md) — the option pattern the `With*`
  builders are built on
