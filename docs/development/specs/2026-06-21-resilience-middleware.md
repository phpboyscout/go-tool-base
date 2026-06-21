---
title: "Resilience Middleware: Server-Side Rate Limiting and Client-Side Circuit Breaking"
description: "Round out the transport resilience story by adding a server-side token-bucket rate limiter (HTTP middleware + gRPC interceptor) and a client-side closed/open/half-open circuit breaker (HTTP ClientMiddleware + gRPC dial interceptor), composing into the existing Chain / InterceptorChain / ClientChain plumbing alongside the current retry/backoff."
date: 2026-06-21
status: DRAFT
tags:
  - specification
  - http
  - grpc
  - middleware
  - resilience
  - rate-limiting
  - circuit-breaker
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Resilience Middleware: Server-Side Rate Limiting and Client-Side Circuit Breaking

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   21 June 2026

Status
:   DRAFT

> Roadmap item **D2 (Resilience middleware)**. This spec is **DRAFT** and paused
> for human review. Per `CLAUDE.md` Step 0, do not begin implementation until the
> [Open Questions](#open-questions) are resolved or explicitly deferred.

---

## Overview

GTB's transports already carry half of a resilience story. On the client side,
`pkg/http` ships `WithRetry` / `RetryConfig` (exponential backoff + full jitter,
[2026-03-26-http-retry-backoff](2026-03-26-http-retry-backoff.md)) and a
`ClientChain` / `ClientMiddleware` `RoundTripper` pipeline
([2026-03-31-http-client-middleware](2026-03-31-http-client-middleware.md)) that
already includes a token-bucket **outbound** `WithRateLimit`. On the server side,
`pkg/http` has the `Chain` / `Middleware` handler pipeline and `pkg/grpc` has the
`InterceptorChain` / `Interceptor` pipeline
([2026-03-26-transport-logging-middleware](2026-03-26-transport-logging-middleware.md)),
both fronting hardened servers ([2026-03-24-secure-http-client](2026-03-24-secure-http-client.md)).

Two resilience primitives are conspicuously **missing**, and they are the two that
most directly complement what already exists:

1. **Server-side rate limiting.** Today a GTB-built management/API server has no
   first-class way to shed load. `WithRateLimit` exists only as *client* egress
   throttling; there is no ingress equivalent for the `Chain` or `InterceptorChain`.
   A downstream tool that exposes an HTTP or gRPC surface must hand-roll a limiter
   to protect itself — exactly the kind of boilerplate the middleware infrastructure
   was built to eliminate.

2. **Client-side circuit breaking.** Retry alone has a well-known failure mode:
   when a downstream is *hard* down (not transiently flapping), every caller keeps
   paying the full retry budget — backoff sleeps, connection attempts, and wasted
   latency — against a service that will not answer. The retry spec itself flags
   this in its *Future Considerations*: "A circuit breaker could wrap the retry
   transport to fail-fast when a downstream service is consistently unavailable,
   avoiding wasted retry attempts." This spec delivers that.

This spec adds, with **no new heavy dependency** (token-bucket already vendored via
`golang.org/x/time/rate`; the breaker is a small hand-rolled state machine):

| Concern | Side | HTTP surface | gRPC surface |
|---------|------|--------------|--------------|
| Rate limiting | **Server (ingress)** | `RateLimitMiddleware` (a `Middleware`) | `RateLimitInterceptor` (an `Interceptor`) |
| Circuit breaking | **Client (egress)** | `WithCircuitBreaker` (a `ClientMiddleware`) | `CircuitBreakerInterceptor` (a `grpc.DialOption` factory) |

All four plug into the **existing** chains. Nothing about the chain types changes.

### Scope clarification — what this is *not*

- **Not a caching layer.** A response/HTTP cache was **previously proposed and
  REJECTED** as a roadmap item. This spec deliberately stays clear of it: nothing
  here stores, keys, or serves responses. The breaker's open-state behaviour is to
  **fail fast** (return an error), never to serve a cached/stale body. See
  [Confirmation: no caching overlap](#confirmation-no-caching-overlap).
- **Not a replacement for retry.** Retry and the breaker are orthogonal layers that
  compose; see [Composition with retry](#composition-with-retry).
- **Not a distributed/coordinated limiter.** The rate limiter is per-process
  (per-server-instance), in-memory. Cluster-wide quota coordination (Redis token
  buckets, etc.) is explicitly out of scope and left to downstream tools.

---

## Decision Log — is this foundation-level or app-level?

> The shared roadmap brief asks each `D*` item to argue whether it belongs in GTB's
> foundation or is really an application concern, and to **say so plainly in the
> verdict if the case is weak.** Here is that argument.

**The case for foundation-level (strong):**

1. **It completes a story GTB already started, in GTB's own vocabulary.** GTB
   already owns `Chain`, `ClientChain`, `InterceptorChain`, `RetryConfig`, and an
   *egress* `WithRateLimit`. Rate limiting and circuit breaking are the two
   canonical resilience primitives that sit beside retry in every "stability
   patterns" treatment (Nygard's *Release It!*, the Polly/resilience4j/gobreaker
   ecosystems). Shipping retry but neither of the other two leaves the library at
   an awkward, incomplete altitude — a downstream tool gets retry for free but must
   reach outside GTB for the partner primitives.

2. **The integration points are GTB-internal types, not app types.** A breaker is a
   `ClientMiddleware`/`http.RoundTripper` and a `grpc.DialOption`; a limiter is a
   `Middleware`/`Interceptor`. These signatures are *GTB's*. An app cannot supply
   them as cleanly from outside without re-deriving the chain plumbing. The natural
   home for a `Middleware` that throttles is the package that defines `Middleware`.

3. **It removes a recurring, security-relevant footgun.** Ingress rate limiting is a
   denial-of-service mitigation. GTB has consistently absorbed transport-hardening
   concerns (body-size caps `DefaultMaxRequestBodyBytes` / `DefaultMaxGRPCMessageBytes`,
   TLS floors, redirect downgrade rejection). A self-protection limiter is the same
   class of concern — a foundation that hands you a server should hand you the means
   to stop it falling over.

4. **Consistency dividend.** Every GTB tool that adopts it gets identical limiter
   semantics, identical breaker state names, identical config keys, and identical
   logs/telemetry. That uniformity is precisely the value proposition of a base
   framework.

**The case against (weak, but recorded honestly):**

- *Policy* (the *rate*, the *failure threshold*) is undeniably app-specific. **But**
  GTB already ships configurable-policy primitives (`RetryConfig`, the egress
  limiter rate, body caps) without anyone calling those "app-level". GTB provides
  the *mechanism* and sane defaults; the app supplies the *numbers*. That division
  is the established pattern, not a new compromise.
- A determined team could vendor `gobreaker` and a `tollbooth`-style limiter
  directly. **But** they would then be writing the chain glue GTB is *for*, and
  losing the consistency dividend in (4).

**Verdict: foundation-level, and not a weak case.** This is the missing third of a
trio GTB already commits to two-thirds of, expressed entirely in GTB's own
middleware types, and it carries a self-protection (DoS) dimension consistent with
GTB's existing transport-hardening remit. It is admitted. The only genuinely
app-level part — the policy numbers — is delegated to config/options exactly as
`RetryConfig` already is. **Recommend: accept.**

### Confirmation: no caching overlap

The previously-**REJECTED** caching-layer item is confirmed **non-overlapping**:

- No type in this spec reads, writes, stores, or keys a response body.
- The circuit breaker's three states are `Closed` / `Open` / `HalfOpen`. In `Open`
  it returns a sentinel error (`ErrCircuitOpen`) **immediately** — it does **not**
  return a previously-seen response. There is no response store, no TTL, no
  conditional-request handling, no `Cache-Control` parsing anywhere in scope.
- The rate limiter rejects or admits; it never substitutes a stored answer.

No conflict with the rejected caching work exists or is introduced.

---

## Design Decisions

1. **Server-side rate limiting; client-side circuit breaking — as the brief
   directs, and as the topology demands.** A limiter protects *the thing receiving
   load*, so it belongs at ingress (server middleware/interceptor). A breaker
   protects *the caller from a sick callee*, so it belongs at egress (client
   middleware / dial interceptor). Putting either on the wrong side is a category
   error; this spec does not offer the inverted variants.

2. **Token-bucket for the limiter** (`golang.org/x/time/rate`), matching the
   **already-shipped** egress `WithRateLimit`. The dependency is already vendored
   and battle-tested; rolling our own leaky-bucket would add risk for no gain.
   Token-bucket gives a smooth steady-state rate plus a configurable burst, which is
   the right shape for API ingress.

3. **Classic three-state circuit breaker, hand-rolled, no new dependency.** The
   `Closed → Open → HalfOpen → Closed/Open` state machine is ~120 lines including
   the rolling failure counter. Pulling in `sony/gobreaker` for that is not
   justified given GTB's std-lib-leaning posture and the "avoid heavy deps if a
   small impl suffices" directive. The implementation lives in a small, fully-unit-
   tested internal type.

4. **Per-route *and* global, via composition rather than a config matrix.** Rather
   than build a route-pattern→policy table into the limiter, we expose the limiter
   as an ordinary `Middleware`/`Interceptor` and let the existing chain mechanics do
   per-route scoping. A *global* limiter is one entry in the server-wide `Chain`; a
   *per-route* limiter is the same constructor wrapped around a specific handler
   (HTTP) or selected by `info.FullMethod` (gRPC). See
   [Per-route vs global](#per-route-vs-global). This keeps the limiter a leaf
   primitive and avoids inventing a routing DSL GTB does not otherwise have.

5. **Config surface mirrors `RetryConfig`.** Each primitive takes a small config
   struct with a `Default*Config()` constructor returning sane values, exactly like
   `DefaultRetryConfig()`. Options are constructor arguments, not a second variadic
   layer, keeping the surface minimal.

6. **Limiter rejects with the protocol-correct "too many requests" signal.** HTTP →
   `429 Too Many Requests` with a `Retry-After` header (which the *client's* retry
   layer already honours — a pleasing closed loop). gRPC → `codes.ResourceExhausted`.

7. **Breaker fails fast with a typed sentinel, surfaced through the existing error
   stack.** `ErrCircuitOpen` is a `cockroachdb/errors` sentinel so callers can
   `errors.Is` it. On the gRPC side the open state returns
   `status.Error(codes.Unavailable, …)` so it is indistinguishable to the wire from
   a genuine downstream outage (which is the correct semantic).

8. **Observability via the existing logger; OTel-ready but not OTel-coupled.**
   Limiter rejections and breaker state transitions log through `logger.Logger`
   (the same dependency the logging middleware already takes). Metrics are listed as
   a future hook, mirroring how `pkg/grpc` keeps OTel in a separate `otel.go`
   ([2026-06-01-otel-observability](2026-06-01-otel-observability.md)) rather than
   threading it through every primitive.

---

## Public API

### `pkg/http` — server-side rate limit middleware

```go
// RateLimitConfig configures the server-side token-bucket rate limiter.
type RateLimitConfig struct {
    // RequestsPerSecond is the sustained fill rate of the token bucket.
    // Must be > 0. Default: 50.
    RequestsPerSecond float64

    // Burst is the bucket capacity — the maximum number of requests that may
    // be admitted in an instantaneous spike. Must be >= 1. Default: 100.
    Burst int

    // KeyFunc derives the limiter key for a request, enabling per-client
    // limiting. When nil, a single global bucket is used for all requests.
    // A common choice is to key on the client IP (see ClientIPKey).
    KeyFunc func(*http.Request) string

    // OnLimited is invoked when a request is rejected, before the 429 is
    // written. Optional; useful for metrics/telemetry. The default writes a
    // structured debug log via the logger passed to the constructor.
    OnLimited func(*http.Request)
}

// DefaultRateLimitConfig returns a RateLimitConfig suitable for a modest
// management/API server: 50 rps sustained, burst 100, single global bucket.
func DefaultRateLimitConfig() RateLimitConfig

// RateLimitMiddleware returns a Middleware that admits requests under a
// token-bucket limiter and rejects excess traffic with 429 Too Many Requests
// plus a Retry-After header. A nil/invalid config falls back to defaults.
//
// Because it is an ordinary Middleware it composes into any Chain and can be
// scoped globally (one entry in the server chain) or per-route (wrap a single
// handler). Per-client limiting is enabled by setting RateLimitConfig.KeyFunc.
func RateLimitMiddleware(log logger.Logger, cfg RateLimitConfig) Middleware

// ClientIPKey is a ready-made RateLimitConfig.KeyFunc that keys on the client
// IP, preferring the left-most X-Forwarded-For entry when present and falling
// back to RemoteAddr. It reuses the same client-IP derivation as the logging
// middleware for consistency.
func ClientIPKey(r *http.Request) string
```

### `pkg/http` — client-side circuit breaker middleware

```go
// CircuitState is the breaker's state.
type CircuitState int

const (
    // StateClosed admits all requests; failures are counted.
    StateClosed CircuitState = iota
    // StateOpen rejects all requests immediately with ErrCircuitOpen until the
    // cooldown elapses, then transitions to StateHalfOpen.
    StateOpen
    // StateHalfOpen admits a limited number of trial requests; success closes
    // the breaker, failure re-opens it.
    StateHalfOpen
)

// ErrCircuitOpen is returned (wrapped) by the breaker when it is open. Callers
// may test for it with errors.Is.
var ErrCircuitOpen = errors.New("http: circuit breaker is open")

// CircuitBreakerConfig configures the client-side breaker.
type CircuitBreakerConfig struct {
    // FailureThreshold is the number of consecutive failures (within Closed)
    // that trips the breaker open. Must be >= 1. Default: 5.
    FailureThreshold int

    // Cooldown is how long the breaker stays Open before allowing a trial.
    // Default: 30s.
    Cooldown time.Duration

    // HalfOpenMaxRequests is the number of trial requests allowed in HalfOpen.
    // The first success closes the breaker; any failure re-opens it.
    // Must be >= 1. Default: 1.
    HalfOpenMaxRequests int

    // IsFailure classifies a round-trip outcome as a failure for breaker
    // accounting. When nil, the default treats transport errors and 5xx
    // responses (>=500) as failures; 4xx and 2xx/3xx are successes. This means
    // a 429 (client rate-limited) does NOT trip the breaker — that is retry's
    // job, not the breaker's.
    IsFailure func(resp *http.Response, err error) bool

    // OnStateChange is invoked on every state transition. Optional; useful for
    // logging/telemetry. The constructor also logs transitions via logger.
    OnStateChange func(from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns: threshold 5, cooldown 30s,
// half-open trial 1, default 5xx/transport-error failure classification.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig

// WithCircuitBreaker returns a ClientMiddleware that fails fast while a
// downstream is consistently failing, avoiding wasted retry/backoff cycles.
// Place it OUTSIDE the retry transport (i.e. earlier in the ClientChain, or
// rely on the documented ordering) so the breaker sees the post-retry verdict.
func WithCircuitBreaker(log logger.Logger, cfg CircuitBreakerConfig) ClientMiddleware
```

### `pkg/grpc` — server-side rate limit interceptor

```go
// RateLimitConfig mirrors the HTTP server limiter for gRPC ingress.
type RateLimitConfig struct {
    RequestsPerSecond float64 // default 50
    Burst             int     // default 100

    // KeyFunc derives the limiter key from the RPC context (e.g. peer address
    // or a metadata value). When nil, a single global bucket is used.
    KeyFunc func(ctx context.Context, fullMethod string) string

    // OnLimited is invoked when an RPC is rejected. Optional.
    OnLimited func(ctx context.Context, fullMethod string)
}

func DefaultRateLimitConfig() RateLimitConfig

// RateLimitInterceptor returns an Interceptor (unary + stream) that admits
// RPCs under a token-bucket limiter and rejects excess with
// codes.ResourceExhausted. Composes into any InterceptorChain; per-method
// scoping is achieved via KeyFunc keying on fullMethod (or a method-filtering
// wrapper, analogous to the logging interceptor's WithPathFilter).
func RateLimitInterceptor(log logger.Logger, cfg RateLimitConfig) Interceptor

// PeerKey is a ready-made KeyFunc keying on the RPC peer address.
func PeerKey(ctx context.Context, fullMethod string) string
```

### `pkg/grpc` — client-side circuit breaker dial option

```go
// CircuitState / StateClosed / StateOpen / StateHalfOpen — same trio as HTTP,
// defined once in pkg/grpc for the gRPC side.

// CircuitBreakerConfig mirrors the HTTP breaker, with a gRPC-shaped failure
// classifier.
type CircuitBreakerConfig struct {
    FailureThreshold    int           // default 5
    Cooldown            time.Duration // default 30s
    HalfOpenMaxRequests int           // default 1

    // IsFailure classifies an RPC outcome. When nil, the default treats
    // Unavailable, DeadlineExceeded, and ResourceExhausted as failures and all
    // other codes (including OK) as successes.
    IsFailure func(err error) bool

    OnStateChange func(from, to CircuitState)
}

func DefaultCircuitBreakerConfig() CircuitBreakerConfig

// CircuitBreakerInterceptor returns a grpc.UnaryClientInterceptor that opens
// when a downstream is consistently failing and rejects calls with
// codes.Unavailable while open. Install it on a client connection via
// grpc.WithChainUnaryInterceptor.
//
// A streaming variant (CircuitBreakerStreamInterceptor) is provided for
// symmetry but only accounts for stream *establishment* failures, not mid-
// stream errors — see Open Questions.
func CircuitBreakerInterceptor(log logger.Logger, cfg CircuitBreakerConfig) grpc.UnaryClientInterceptor
func CircuitBreakerStreamInterceptor(log logger.Logger, cfg CircuitBreakerConfig) grpc.StreamClientInterceptor
```

> **gRPC asymmetry, deliberate.** The server breaker would be nonsensical (a server
> does not "break" against itself), and the client *rate limiter* is already the
> egress `golang.org/x/time/rate` story on the HTTP side and is rarely needed for
> gRPC clients. So gRPC ships **server rate-limit** + **client breaker**, matching
> the HTTP shape. No inverted variants are offered.

---

## Per-route vs global

The limiter is a leaf primitive; **scoping is composition**, not configuration.

**Global (server-wide):** one limiter entry in the chain protects everything.

```go
chain := gtbhttp.NewChain(
    gtbhttp.RecoveryMiddleware(l),
    gtbhttp.RateLimitMiddleware(l, gtbhttp.DefaultRateLimitConfig()), // global
    gtbhttp.LoggingMiddleware(l),
)
_, _ = gtbhttp.Register(ctx, "http", controller, cfg, l, mux, gtbhttp.WithMiddleware(chain))
```

**Per-route:** wrap the specific handler with its own limiter before mounting.

```go
mux := http.NewServeMux()
mux.Handle("/api/expensive",
    gtbhttp.RateLimitMiddleware(l, gtbhttp.RateLimitConfig{RequestsPerSecond: 2, Burst: 2}).
        Then(http.HandlerFunc(expensiveHandler)), // a single-middleware Then via NewChain
)
mux.HandleFunc("/api/cheap", cheapHandler) // unlimited
```

**Per-client:** set `KeyFunc` (e.g. `ClientIPKey`) so each key gets its own bucket;
buckets are stored in a bounded, lazily-evicted map (see
[Per-client bucket store](#per-client-bucket-store)).

The gRPC side mirrors this: a global limiter is one `Interceptor` in the
`InterceptorChain`; per-method limiting keys on `info.FullMethod` inside `KeyFunc`
(or wraps with a method filter analogous to the logging interceptor's path filter).

> Health endpoints (`/healthz`, `/livez`, `/readyz`) are **already** mounted outside
> the `WithMiddleware` chain by `Register` (see `pkg/http/server.go`), so a global
> limiter never throttles liveness/readiness probes. This is a load-bearing
> existing guarantee, not new work.

---

## Composition with retry

The breaker and retry are orthogonal and stack in a defined order. Within a
`NewClient`, the established wrapping is: base transport → `retryTransport` →
`clientChain` (see `NewClient` in `pkg/http/client.go`, lines ~128-141). Because the
breaker is a `ClientMiddleware` it lives in the `clientChain`, i.e. **outside** the
retry transport:

```
request → [circuit breaker] → [retry (backoff)] → [base transport] → network
```

This is the **correct** ordering and the whole point:

- The breaker sees the **final post-retry verdict** for a request. One logical call
  that exhausts its retry budget against a dead service counts as **one** breaker
  failure, not N.
- Once the breaker is `Open`, subsequent calls are rejected **before** entering the
  retry layer — so no backoff sleeps, no connection attempts are spent on a service
  known to be down. This is exactly the waste the retry spec's *Future
  Considerations* flagged.

```go
client := gtbhttp.NewClient(
    gtbhttp.WithRetry(gtbhttp.DefaultRetryConfig()),
    gtbhttp.WithClientMiddleware(gtbhttp.NewClientChain(
        gtbhttp.WithCircuitBreaker(l, gtbhttp.DefaultCircuitBreakerConfig()),
        gtbhttp.WithRequestLogging(l),
    )),
)
```

> **Ordering note for the spec author implementing this:** the current
> `WithClientMiddleware` doc comment says the chain "wraps the transport after retry
> … so that retry operates on the raw transport". Confirm during implementation that
> a breaker placed in the chain therefore sits *outside* retry as described, and
> tighten the doc comment if the wording is ambiguous. (Captured in Open Questions.)

---

## Internal implementation

### Token-bucket limiter (shared shape, two packages)

Each limiter holds either a single `*rate.Limiter` (global, `KeyFunc == nil`) or a
`*keyedLimiter` (per-key). On each request: `limiter.Allow()` — **non-blocking**,
unlike the egress `WithRateLimit` which uses `Wait` to *throttle* the caller. Ingress
must **reject**, not block, or a flood would simply queue and exhaust memory. `Allow()
== false` → write the rejection.

```go
// httpRateLimit (illustrative)
func RateLimitMiddleware(log logger.Logger, cfg RateLimitConfig) Middleware {
    cfg = cfg.normalized() // apply defaults, clamp
    store := newLimiterStore(cfg.RequestsPerSecond, cfg.Burst)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            key := ""
            if cfg.KeyFunc != nil {
                key = cfg.KeyFunc(r)
            }
            if !store.limiterFor(key).Allow() {
                if cfg.OnLimited != nil {
                    cfg.OnLimited(r)
                }
                log.Debug("request rate-limited", "path", r.URL.Path, "key", key)
                w.Header().Set("Retry-After", "1")
                http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### Per-client bucket store

`KeyFunc` introduces unbounded-key risk (an attacker rotating source IPs could
allocate a `*rate.Limiter` per IP and exhaust memory). The store is therefore
**bounded and evicting**: a mutex-guarded map capped at `maxTrackedKeys` (default
8192) with simple LRU/last-access eviction. When full, the least-recently-used key is
evicted (its bucket is recreated full on next sighting — acceptable, since eviction
only happens under key churn). This mirrors the defensive posture of the existing
body-size caps. **This memory-safety property is a required test.**

### Circuit breaker state machine

A mutex-guarded struct holding `state CircuitState`, `consecutiveFailures int`,
`openedAt time.Time`, and `halfOpenInFlight int`. The transitions:

- **Closed:** on each completed call, `IsFailure` increments or resets the counter;
  reaching `FailureThreshold` → `Open` (record `openedAt`).
- **Open:** every call rejected with `ErrCircuitOpen` until `time.Since(openedAt) >=
  Cooldown`; the next call after cooldown → `HalfOpen`.
- **HalfOpen:** admit up to `HalfOpenMaxRequests` trials; first success → `Closed`
  (reset counter); any failure → `Open` (reset `openedAt`). Trials beyond the cap
  while a trial is in flight are rejected with `ErrCircuitOpen`.

Time is injected (`now func() time.Time`, default `time.Now`) so cooldown transitions
are deterministically testable without sleeps — consistent with the project's
race-avoidance guidance (no package-level clock, dependency injected via struct
field). The HTTP variant adapts `RoundTrip`; the gRPC variant adapts the unary/stream
client interceptor signatures. The core state machine is a single shared internal
type to avoid two divergent implementations.

### gRPC limiter

`RateLimitInterceptor` returns an `Interceptor{Unary, Stream}`. Unary checks
`Allow()` before invoking `handler`; on rejection returns
`status.Error(codes.ResourceExhausted, "rate limit exceeded")`. Stream checks at
stream-open. Peer address via `peer.FromContext` (the logging interceptor already
does this, so the helper is reusable).

---

## Project structure

```
pkg/http/
    ratelimit.go            # NEW: RateLimitConfig, RateLimitMiddleware, ClientIPKey, limiterStore
    ratelimit_test.go       # NEW
    circuitbreaker.go       # NEW: CircuitBreakerConfig, WithCircuitBreaker, ErrCircuitOpen, breaker
    circuitbreaker_test.go  # NEW
    client_middleware.go    # UNCHANGED (breaker is a ClientMiddleware; no chain change)
    client.go               # UNCHANGED (ordering already correct)

pkg/grpc/
    ratelimit.go            # NEW: RateLimitConfig, RateLimitInterceptor, PeerKey
    ratelimit_test.go       # NEW
    circuitbreaker.go       # NEW: CircuitBreakerConfig, CircuitBreaker(Stream)Interceptor
    circuitbreaker_test.go  # NEW

internal/circuitbreaker/   # OPTION (see Open Questions): shared state machine
    breaker.go              # core Closed/Open/HalfOpen machine, transport-agnostic
    breaker_test.go
```

> The chain/interceptor types (`chain.go`, `client_middleware.go`,
> `pkg/grpc/chain.go`) are **untouched** — every new primitive is just another
> value of an existing middleware/interceptor type.

---

## Generator impact

**None for default scaffolding.** The generator does not prescribe a middleware
stack; consumers opt in, exactly as with the logging/recovery middleware. The
`docs/components/` examples should show the resilience middleware so scaffolded tools
discover it, but no template change is required. (If review wants the generated
server to ship a commented-out `RateLimitMiddleware` line as a discoverability hint,
that is a small, separate follow-up — flagged in Open Questions.)

---

## Error handling

- **Limiter:** never errors internally; rejection is a normal `429` /
  `ResourceExhausted` response, not a Go error. Invalid config is clamped to safe
  defaults by `normalized()` rather than rejected, so a misconfigured limiter can
  never *fail open into a panic* — it fails into the default policy and logs a warn.
- **Breaker:** open-state rejection returns `ErrCircuitOpen` (HTTP, `errors.Is`-able)
  or `codes.Unavailable` (gRPC). The breaker never swallows a downstream's real
  error in `Closed`/`HalfOpen` — it passes the real `resp, err` through and only
  *counts* it.
- All errors created/wrapped with `github.com/cockroachdb/errors` per project policy.

---

## Testing strategy

Table-driven, `t.Parallel()`, `logger.NewNoop()`, injected clock — no `time.Sleep`
for breaker timing. New `pkg/` code targets **≥90% coverage** per policy.

| Test | Scenario |
|------|----------|
| `TestRateLimit_AdmitsUnderRate` | requests within burst+rate all pass |
| `TestRateLimit_Rejects429` | excess request → 429 + Retry-After |
| `TestRateLimit_PerClientKey` | two IPs get independent buckets |
| `TestRateLimit_BucketStoreBounded` | key churn never exceeds maxTrackedKeys (memory-safety) |
| `TestRateLimit_GlobalNilKeyFunc` | single shared bucket when KeyFunc nil |
| `TestRateLimit_HealthEndpointsUnaffected` | /healthz never throttled (via Register) |
| `TestRateLimit_NonBlocking` | limiter uses Allow not Wait — rejected request returns promptly |
| `TestBreaker_OpensAtThreshold` | N consecutive failures → Open |
| `TestBreaker_OpenRejectsFast` | Open returns ErrCircuitOpen without calling next |
| `TestBreaker_HalfOpenAfterCooldown` | injected clock past cooldown → HalfOpen |
| `TestBreaker_HalfOpenSuccessCloses` | trial success → Closed, counter reset |
| `TestBreaker_HalfOpenFailureReopens` | trial failure → Open, openedAt reset |
| `TestBreaker_HalfOpenConcurrencyCap` | only HalfOpenMaxRequests trials admitted |
| `TestBreaker_DefaultIsFailure_5xxAndTransport` | 5xx + transport err count; 4xx/429 do not |
| `TestBreaker_ErrorsIsSentinel` | errors.Is(err, ErrCircuitOpen) holds |
| `TestBreaker_ComposesWithRetry` | one retry-exhausted call = one breaker failure |
| `TestGRPCRateLimit_Unary/Stream` | bufconn: ResourceExhausted on excess |
| `TestGRPCBreaker_Unary` | bufconn: Unavailable while open, recovers after cooldown |
| `TestBreaker_RaceUnderParallel` | `-race` with concurrent RoundTrips |

Concurrency tests run under `-race`; the breaker and bucket store must be race-clean
with no package-level mutable state (project mandate).

---

## Linting & verification

```bash
go build ./...
go test -race ./pkg/http/... ./pkg/grpc/...
golangci-lint run
just ci
```

No new `nolint` directives anticipated. No new third-party dependency
(`golang.org/x/time/rate` already vendored; breaker is hand-rolled).

---

## Documentation

- New sections in `docs/components/http.md` and `docs/components/grpc.md` (or
  equivalent) covering both primitives, the global/per-route/per-client recipes, and
  the retry-composition ordering diagram.
- Cross-reference from the retry-backoff component docs (closing its "circuit
  breaker" future-work note) and from `docs/concepts/` resilience overview if one
  exists.
- Godoc on every exported symbol; the breaker godoc must state the open-state
  fail-fast semantics and explicitly note it does **not** serve cached responses.

---

## Backwards compatibility

- **Purely additive.** No existing type, signature, or default changes. Chains,
  `RetryConfig`, and `NewClient` ordering are untouched.
- All four primitives are opt-in; a tool that does not add them sees identical
  behaviour to today.
- Pre-1.0 API note: even though breaking changes are currently permitted as a minor
  bump, none are needed here.

---

## Future considerations

- **OTel metrics:** limiter admit/reject counters and breaker state-transition gauges
  via the existing `otel.go` pattern — natural next step, deliberately out of this
  spec's scope to keep it focused.
- **Adaptive / concurrency limiting:** a Little's-law / AIMD adaptive limiter
  (à la Netflix `concurrency-limits`) as an alternative to fixed token-bucket.
- **Distributed limiter backend:** pluggable store interface so the bucket can live
  in Redis for cluster-wide quotas. The `limiterStore` is deliberately an internal
  interface so this could slot in without an API break.
- **Breaker bulkheading:** per-host breaker instances inside one client (keyed like
  the limiter store) so one bad host doesn't open the breaker for healthy hosts.
- **Config-driven policy:** read `RateLimitConfig` / `CircuitBreakerConfig` defaults
  from a config prefix (e.g. `server.http.ratelimit.*`) so operators can tune without
  recompiling — mirrors how server port/TLS are config-driven. Flagged below.

---

## Open Questions

1. **Shared internal breaker package?** The Closed/Open/HalfOpen machine is identical
   for HTTP and gRPC. Extract to `internal/circuitbreaker` (one tested core, two thin
   adapters) — or accept a small amount of duplication to keep each transport package
   self-contained? Recommendation: extract; it is genuinely shared logic with a
   transport-agnostic shape.

2. **Config-prefix integration now or later?** Should the four configs be readable
   from a config prefix (`server.http.ratelimit.requests_per_second`, etc.) in v1, so
   operators tune policy via config like they tune port/TLS — or ship code-only
   defaults first and add config binding as a follow-up? The brief's "minimal config
   surface" leaning suggests code-first; confirm.

3. **gRPC streaming breaker depth.** Should the stream client breaker account only for
   **stream-establishment** failures (proposed, simple) or also inspect per-message
   errors via a wrapped `ClientStream` (more complete, more code)? Proposed:
   establishment-only for v1, documented as such.

4. **Default rate values.** `50 rps / burst 100` is a guess for a "modest management
   server". Are these the right defaults, or should the limiter ship with **no
   default rate** (forcing the caller to choose) to avoid a surprising throttle on an
   unconfigured high-traffic server? Trade-off: safe-by-default vs least-surprise.

5. **Breaker default failure classification — is 429 a failure?** Proposed: **no** —
   a 429 means "you're being rate-limited", which is retry's domain, not a downstream
   health signal, so it should not trip the breaker. Confirm this is the desired
   default (it is the one encoded in `DefaultCircuitBreakerConfig`).

6. **`WithClientMiddleware` doc-comment ambiguity.** The existing comment frames the
   chain as wrapping "after retry". Confirm the implementer should tighten that wording
   so the breaker-outside-retry ordering is unambiguous, rather than leaving it implicit.

7. **Generator discoverability hint.** Should the scaffolded server include a
   commented-out `RateLimitMiddleware` line as a discoverability nudge, or is a docs
   mention sufficient? Proposed: docs only; no template change.

8. **E2E/BDD coverage.** Per `CLAUDE.md`, user-facing transport behaviour may warrant
   a Godog scenario (e.g. "given a server with a 2 rps limiter, when I send 5 rapid
   requests, then 3 receive 429"). Is an E2E scenario in scope for this item, or are
   unit + integration tests sufficient given there is no new CLI command? Proposed:
   one smoke-level CLI/transport BDD scenario for the limiter; breaker covered by unit
   tests only.

---

## Implementation phases

1. **HTTP server rate limiter** — `RateLimitConfig`, `normalized()`, `limiterStore`
   (bounded/evicting), `RateLimitMiddleware`, `ClientIPKey`; tests incl. memory-safety
   and health-endpoint-unaffected.
2. **HTTP client circuit breaker** — core state machine (shared per OQ1), `ErrCircuitOpen`,
   `WithCircuitBreaker`, injected clock; tests incl. retry-composition and `-race`.
3. **gRPC server rate limiter** — `RateLimitInterceptor`, `PeerKey`; bufconn tests.
4. **gRPC client circuit breaker** — unary + (establishment-only) stream interceptors;
   bufconn tests.
5. **Docs + (optional) BDD** — component docs, retry cross-reference, optional limiter
   Godog scenario; `/gtb-verify`.
