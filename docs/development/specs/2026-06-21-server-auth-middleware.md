---
title: "Server-Side Authentication & Authorization Middleware"
description: "Opt-in JWT/OIDC token verification and API-key gating for the HTTP and gRPC transports, plugged into the existing middleware/interceptor chains. Composable building blocks, not a mandated auth model — GTB ships the transports and TLS; this secures them via the accepted middleware extension point."
date: 2026-06-21
status: DRAFT
tags:
  - specification
  - http
  - grpc
  - middleware
  - security
  - authentication
  - authorization
  - jwt
  - oidc
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Server-Side Authentication & Authorization Middleware

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   21 June 2026

Status
:   DRAFT

> **This is a DRAFT for human review.** It does not begin implementation. See
> [Open Questions](#open-questions) and [Feasibility Verdict](#feasibility-verdict)
> before approving. The feasibility verdict is **FEASIBLE-WITH-CAVEATS** — the
> JWKS/OIDC surface is the part most in tension with the "foundation, not
> application framework" principle, and one phasing option defers it.

---

## Problem Statement

GTB ships first-class server transports — `pkg/http` (an `*http.Server` with a
`Chain`/`Middleware` system, security headers, request-size limits, structured
logging) and `pkg/grpc` (a `*grpc.Server` with an `InterceptorChain`, TLS,
health, logging). Both attach to services through `pkg/controls` and share a
hardened `pkg/tls` config. The grpc-gateway in `pkg/gateway/` re-exposes the
gRPC services as REST.

What GTB provides today is **transport-level confidentiality and integrity**
(TLS) and **request hygiene** (size caps, security headers, reflection gating).
What it does **not** provide is any way to answer *"who is calling, and are they
allowed to?"* on the server side. A tool author who stands up an HTTP or gRPC
management/API surface with GTB has to hand-roll:

- bearer-token (JWT/OIDC) verification — fetching and caching a JWKS document,
  validating signature, `iss`/`aud`/`exp`/`nbf`, and surfacing the claims;
- API-key gating — reading a shared secret and comparing it **in constant time**
  (the naïve `==` is a timing-oracle footgun that is easy to get wrong);
- mapping an auth failure onto the right wire status (HTTP 401 vs 403; gRPC
  `Unauthenticated` vs `PermissionDenied`) without leaking *why* it failed.

Each of these is a small-surface, easy-to-get-subtly-wrong security primitive.
Re-implementing them per tool is exactly the duplication GTB exists to remove —
the same argument that justified `SecurityHeadersMiddleware`, `MaxBytesMiddleware`,
the constant-time-adjacent TLS hardening, and `pkg/redact`.

This spec proposes **opt-in** auth middleware (HTTP) and interceptors (gRPC)
that plug into the **existing** `Chain`/`InterceptorChain`, with no change to the
default behaviour of any server. A server that registers no auth middleware
behaves exactly as it does today.

## Scope & Decision-Log Justification (read this first)

The [feature decision log](../feature-decisions.md) **rejected** an event bus,
task queues, a DB/ORM abstraction, and **distributed tracing**, under the
principle *"a foundation for tools, not an application framework."* Server-side
auth must be measured against that same bar honestly, because "auth" can easily
sprawl into application territory (user stores, RBAC policy engines, session
management, login flows).

**Why opt-in transport auth middleware IS foundation-level:**

1. **It secures a transport GTB already ships.** GTB owns the `*http.Server` and
   `*grpc.Server` lifecycle, their TLS, their size limits, their security
   headers, and their reflection gating. Verifying the credential on an inbound
   request is the *same layer* as those — it is request hygiene at the edge, not
   business logic. TLS answers "is the channel private?"; this answers "is the
   caller authenticated?" — both are properties of the transport, not the
   application.
2. **The extension point is already an accepted GTB pattern.** `Chain`/
   `Middleware` (HTTP) and `InterceptorChain`/`Interceptor` (gRPC) exist
   *specifically* so cross-cutting concerns compose without touching handlers.
   The command-middleware spec explicitly names *"authentication validation"* as
   a motivating use case for the analogous pattern. Auth middleware is the
   canonical thing these chains are for — adding one more composable middleware
   is not new machinery.
3. **The primitives are security footguns, not features.** Constant-time key
   comparison, JWKS signature verification, `iss`/`aud`/`exp` checks, and
   "fail closed without leaking the reason" are precisely the kind of
   easy-to-get-wrong security plumbing GTB centralises elsewhere
   (`pkg/redact`, `pkg/tls`, `pkg/browser` scheme allowlist,
   `pkg/regexutil` ReDoS bounds). Each tool re-implementing them is a
   net security regression for the ecosystem.
4. **It ships no policy and no identity model.** The middleware *verifies a
   credential and exposes the verified claims*. It does **not** decide what a
   principal may do (no RBAC engine, no policy DSL), store users, manage
   sessions, or run a login flow. The authorization knob offered is deliberately
   minimal: a claims-predicate callback the tool author supplies. That is the
   boundary that keeps this on the foundation side.

**Where it would cross the line into "application framework" (and is therefore
OUT of scope):** a user/identity store; a role/permission model or policy DSL
(Casbin/OPA-style); session or cookie management; OAuth2/OIDC *login* flows
(authorization-code, PKCE, token issuance/refresh — GTB verifies tokens, it does
not mint them); multi-tenant org models. These are application concerns and are
explicitly rejected here, consistent with the decision log.

**The honest tension:** JWKS fetch-and-cache pulls in an HTTP-client-with-TTL-cache
(the decision log rejected a generic `pkg/cache`) and an OIDC discovery notion
that edges toward "service framework." This is the single most debatable part of
the proposal and is the reason the verdict is FEASIBLE-WITH-CAVEATS rather than
FEASIBLE, and why Phase 2 (JWT/OIDC) is **separable** from Phase 1 (API key) so
it can be deferred or rejected independently. See
[Feasibility Verdict](#feasibility-verdict).

## Goals & Non-Goals

### Goals

1. A new `pkg/authn` package providing transport-agnostic credential
   verification primitives:
   - **API-key** verification with **constant-time** comparison
     (`crypto/subtle.ConstantTimeCompare`), supporting multiple valid keys and
     a key→identity label map.
   - **JWT/OIDC bearer-token** verification: JWKS fetch + cache (TTL + bounded),
     signature validation, and `iss`/`aud`/`exp`/`nbf` checks, exposing the
     verified claims.
2. Thin transport adapters that wrap a verifier as the existing extension types:
   - `pkg/http`: an `AuthMiddleware(...) Middleware` that slots into `Chain`.
   - `pkg/grpc`: an `AuthInterceptor(...) Interceptor` (unary + stream) that
     slots into `InterceptorChain`.
3. A standard place to read the verified identity downstream: store it in the
   request `context.Context` and provide `IdentityFromContext(ctx)`. The gRPC
   side uses the same context key so handlers are transport-uniform.
4. Correct, non-leaky failure surfacing via `pkg/errorhandling` semantics:
   HTTP `401`/`403`, gRPC `codes.Unauthenticated`/`codes.PermissionDenied`,
   with `WWW-Authenticate` on 401 and **no disclosure** of *why* validation
   failed to the client (the detail is logged server-side, redacted).
5. **100% opt-in, zero default behaviour change.** No server gains auth unless
   the tool author adds the middleware/interceptor to a chain. The generator
   scaffolds it commented-out, not active.
6. A minimal, callback-based **authorization** hook (`RequireClaims`,
   `AuthorizeFunc`) — *not* a policy engine.

### Non-Goals

- **No identity/user store, no RBAC/policy engine, no policy DSL.** Authorization
  is a tool-supplied predicate over verified claims, nothing more.
- **No OAuth2/OIDC login or token issuance flow.** GTB *verifies* tokens; it
  never mints, refreshes, or brokers them. No authorization-code/PKCE flow.
- **No session/cookie management.** Bearer/`Authorization`-header and API-key
  header credentials only; no server-side session state.
- **No mutual-TLS client-cert auth in this spec.** mTLS is a transport/`pkg/tls`
  concern already flagged as future work in the server-hardening spec; it may
  layer in later as a third verifier but is out of scope here to keep the surface
  small. *(Open Question 4.)*
- **No new generic cache package.** The JWKS cache is a single-purpose,
  internal, bounded cache inside `pkg/authn` — it does not resurrect the rejected
  `pkg/cache`.
- **No rate limiting / brute-force lockout.** Orthogonal; a separate concern.
- **No change** to `controls`, health endpoints, the gateway's wiring, or TLS.

## Background: existing extension points (anchors)

| Anchor | Type / symbol | Role |
|--------|---------------|------|
| `pkg/http/chain.go` | `Middleware func(http.Handler) http.Handler`, `Chain`, `NewChain`, `Then` | HTTP middleware composition (auth slots in here) |
| `pkg/http/server.go` | `ServerOption`, `RegisterOption`, `WithMiddleware(Chain)`, `Register(... opts ...any)` | how a chain reaches a server; health endpoints mount **outside** the chain |
| `pkg/http/security_headers.go` | `SecurityHeadersOption`, `SecurityHeadersMiddleware(...)` | the canonical pattern this spec mirrors (opt-in security middleware) |
| `pkg/grpc/chain.go` | `Interceptor{Unary,Stream}`, `InterceptorChain`, `ServerOptions()` | gRPC interceptor composition (auth slots in here) |
| `pkg/grpc/server.go` | `RegisterOption`, `WithInterceptors(InterceptorChain)`, `Register(... opts ...any)` | how a chain reaches a gRPC server |
| `pkg/grpc/logging.go` | `LoggingInterceptor` (unary+stream pair) | the structural template for a paired interceptor |
| `pkg/gateway/gateway.go` | `Register`, `New` | gateway is just an `http.Handler`/server — auth middleware applies to it like any HTTP server |
| `pkg/tls/tls.go` | `DefaultConfig`, `Resolve`, `Pair` | transport security GTB already owns (auth is the next layer up) |
| `pkg/controls` | `Controllable`, `Register` | transports attach to services here; **unchanged** |
| `pkg/errorhandling` | `WithUserHint`, `cockroachdb/errors` | server-side error wrapping/hints |
| `pkg/redact` | `String`, `IsSensitiveHeaderKey` | redact tokens before they reach a log |

**Key existing invariant to preserve:** in `pkg/http.Register`, the health
endpoints (`/healthz`, `/livez`, `/readyz`) are mounted **outside** the
middleware chain (`server.go` mounts them on the mux directly; only `/` is
wrapped by `rc.chain.Then(handler)`). Auth must therefore **never** gate the
health probes — they remain reachable unauthenticated by construction, which is
the correct behaviour for k8s liveness/readiness. This spec relies on that
existing structure and does not change it.

## Design

### 1. `pkg/authn` — transport-agnostic verifiers

A small package of pure verification logic with no HTTP/gRPC imports, so it is
trivially unit-testable and reusable from both transports (and by tools
directly).

```go
package authn

// Identity is the verified outcome of a successful authentication. It carries
// the minimum a downstream handler needs to make authorization decisions.
type Identity struct {
    // Subject is the principal identifier: the "sub" claim for JWT, or the
    // configured label for an API key.
    Subject string
    // Method records which verifier authenticated the request ("apikey", "jwt").
    Method string
    // Claims holds verified JWT claims (nil for API-key auth). Read-only.
    Claims map[string]any
    // Scopes is the parsed "scope"/"scp" claim, when present, for convenience.
    Scopes []string
}

// Verifier authenticates a single credential string (the raw bearer token or
// API key, already extracted from the transport). It returns the verified
// Identity, or an error. Implementations MUST NOT leak the reason for failure
// in the returned error's *user-facing* hint; the error message is for the
// server log only (callers redact + map it to a generic wire status).
type Verifier interface {
    Verify(ctx context.Context, credential string) (*Identity, error)
}
```

Two concrete verifiers:

#### 1a. API-key verifier (Phase 1 — ships first, independently)

```go
// KeyEntry pairs a valid key with the Subject label recorded on success.
type KeyEntry struct {
    Key     string // the shared secret
    Subject string // identity label, e.g. "ci-runner", "admin"
}

// NewAPIKeyVerifier returns a Verifier that accepts any of the given keys.
// Comparison is constant-time (crypto/subtle) over a fixed-length digest of the
// presented credential against each entry, so neither match/no-match nor key
// length is timing-distinguishable. An empty key set is a construction error
// (fail-closed: never accept "any key").
func NewAPIKeyVerifier(entries ...KeyEntry) (Verifier, error)
```

**Constant-time detail (load-bearing):** comparing variable-length user input
against secrets with `subtle.ConstantTimeCompare` is itself length-leaking
(it short-circuits on unequal lengths). The verifier therefore compares a
**fixed-width SHA-256 digest** of the presented credential against the
pre-computed digest of each configured key
(`subtle.ConstantTimeCompare(sha256(presented), storedDigest) == 1`), iterating
**all** entries (no early return on first match) so the number of comparisons
does not depend on which key matched. This mirrors the defensive posture of
`pkg/tls`/`pkg/redact`. Keys are sourced via `pkg/credentials` resolution by the
tool author; `pkg/authn` receives already-resolved strings and never logs them.

#### 1b. JWT/OIDC verifier (Phase 2 — separable; see Feasibility)

Built on `github.com/golang-jwt/jwt/v5` (already present in `go.sum` as an
indirect dependency — promoting it to a direct dependency, no new module added
beyond the JWKS fetch helper).

```go
type JWTConfig struct {
    // Issuer is the required "iss". Verification fails if it does not match.
    Issuer string
    // Audiences are the acceptable "aud" values (any-of). Empty = no aud check
    // (logged as a warning at construction; not recommended).
    Audiences []string
    // JWKSURL is the JSON Web Key Set endpoint. When set, signing keys are
    // fetched and cached from here. Mutually informative with Issuer for OIDC
    // (see WithOIDCDiscovery).
    JWKSURL string
    // Leeway tolerates small clock skew on exp/nbf/iat (default 60s).
    Leeway time.Duration
    // RefreshInterval bounds how often the JWKS is refetched (default 15m);
    // a kid miss triggers at most one out-of-band refresh, rate-limited.
    RefreshInterval time.Duration
    // AllowedAlgorithms restricts accepted "alg" values (default: RS256, ES256,
    // RS384/512, ES384/512). "none" is ALWAYS rejected. HMAC ("HS*") is rejected
    // when a JWKS is configured (alg-confusion defence).
    AllowedAlgorithms []string
    // HTTPClient is the client used to fetch the JWKS. Defaults to a client
    // built from pkg/http's hardened client; MUST be HTTPS (validated).
    HTTPClient *http.Client
}

func NewJWTVerifier(ctx context.Context, cfg JWTConfig) (Verifier, error)

// WithOIDCDiscovery resolves JWKSURL (and validates Issuer) from the provider's
// /.well-known/openid-configuration document at construction time. This is the
// ONLY OIDC affordance: discovery of the JWKS endpoint. No login flow, no token
// endpoint use. The discovery URL must be HTTPS.
func WithOIDCDiscovery(issuerURL string) JWTOption
```

The JWKS cache is an **internal, bounded, single-purpose** struct in `pkg/authn`
(`jwksCache`): it holds a `map[kid]crypto.PublicKey`, a fetch timestamp, a
`sync.RWMutex`, a hard cap on key count and document size, and a single-flight
refresh guard. It is **not** a general cache and does not re-open the rejected
`pkg/cache` proposal — it caches exactly one kind of thing for exactly one
purpose, like the version-check cache patterns already in the codebase.

JWKS-fetch hardening: HTTPS-only URL (rejects `http://`, validated like
`chat.ValidateBaseURL` / `pkg/browser`), bounded response body
(`http.MaxBytesReader`-style cap), bounded key count, and a fetch timeout. This
reuses the project's established "bound everything that crosses a trust
boundary" posture.

### 2. Authorization hook (minimal, callback-based)

```go
// AuthorizeFunc decides whether an authenticated Identity may proceed. It runs
// AFTER successful verification. Returning false yields 403 / PermissionDenied.
// This is the ENTIRE authorization surface: tool authors implement policy in
// Go. GTB ships no RBAC engine and no policy DSL.
type AuthorizeFunc func(ctx context.Context, id *Identity) bool

// RequireScopes is a convenience AuthorizeFunc requiring all named scopes.
func RequireScopes(scopes ...string) AuthorizeFunc

// RequireClaim is a convenience AuthorizeFunc requiring claim == value.
func RequireClaim(name string, value any) AuthorizeFunc
```

### 3. HTTP adapter — `pkg/http`

A new `auth.go` in `pkg/http` exposing a `Middleware` factory that composes into
the existing `Chain`, mirroring `SecurityHeadersMiddleware`'s option style.

```go
// AuthOption configures AuthMiddleware.
type AuthOption func(*authConfig)

// WithBearerVerifier extracts a token from "Authorization: Bearer <token>" and
// verifies it with v.
func WithBearerVerifier(v authn.Verifier) AuthOption

// WithAPIKeyHeader extracts the credential from the named header (e.g.
// "X-API-Key") and verifies it with v. May be combined with a bearer verifier;
// the first scheme that supplies a credential is used (bearer takes precedence).
func WithAPIKeyHeader(header string, v authn.Verifier) AuthOption

// WithAuthorize installs an authorization predicate run after verification.
func WithAuthorize(fn authn.AuthorizeFunc) AuthOption

// WithAuthLogger sets the logger for redacted server-side auth failure logging.
func WithAuthLogger(l logger.Logger) AuthOption

// WithAuthSkipper skips auth for requests matching pred (e.g. an OPTIONS
// preflight, or a public sub-path). Health endpoints are already outside the
// chain and need no skipper.
func WithAuthSkipper(pred func(*http.Request) bool) AuthOption

// AuthMiddleware returns a Middleware that authenticates (and optionally
// authorizes) each request, storing the verified Identity in the request
// context on success. With no verifier configured it is a construction error
// (fail-closed; never a silent pass-through).
func AuthMiddleware(opts ...AuthOption) (Middleware, error)

// IdentityFromContext returns the verified Identity, if any, set by
// AuthMiddleware. The same key is shared with the gRPC interceptor.
func IdentityFromContext(ctx context.Context) (*authn.Identity, bool)
```

Wiring (composes with everything that exists today — no new server plumbing):

```go
authMW, _ := http.AuthMiddleware(
    http.WithBearerVerifier(jwtV),
    http.WithAuthorize(authn.RequireScopes("api:write")),
    http.WithAuthLogger(log),
)
chain := http.NewChain(
    http.SecurityHeadersMiddleware(),
    http.LoggingMiddleware(log),
    authMW, // <- new, after logging so failures are logged, before the handler
)
http.Register(ctx, "api", ctrl, cfg, log, handler, http.WithMiddleware(chain))
// /healthz, /livez, /readyz remain unauthenticated by construction.
```

**Failure surfacing (HTTP):** on a missing/invalid credential the middleware
writes `401 Unauthorized` with `WWW-Authenticate: Bearer` (and/or the API-key
scheme) and a generic JSON body `{"error":"unauthorized"}` — **no reason**. On a
verified-but-unauthorized identity it writes `403 Forbidden`,
`{"error":"forbidden"}`. The *specific* cause (expired, bad signature, unknown
kid, wrong aud) is logged once at WARN via the auth logger with the token
**redacted** through `pkg/redact`. The handler is never invoked on failure.

### 4. gRPC adapter — `pkg/grpc`

A new `auth.go` in `pkg/grpc` producing a paired `Interceptor`, structurally
identical to `LoggingInterceptor` (`Interceptor{Unary, Stream}`), composing into
the existing `InterceptorChain`.

```go
// GRPCAuthOption configures AuthInterceptor.
type GRPCAuthOption func(*grpcAuthConfig)

// WithGRPCBearerVerifier extracts the token from the "authorization" metadata
// ("Bearer <token>") and verifies it.
func WithGRPCBearerVerifier(v authn.Verifier) GRPCAuthOption

// WithGRPCAPIKeyMetadata extracts the credential from the named metadata key.
func WithGRPCAPIKeyMetadata(key string, v authn.Verifier) GRPCAuthOption

func WithGRPCAuthorize(fn authn.AuthorizeFunc) GRPCAuthOption
func WithGRPCAuthLogger(l logger.Logger) GRPCAuthOption

// WithGRPCMethodSkipper skips auth for matching full method names (e.g. the
// health and reflection services).
func WithGRPCMethodSkipper(pred func(fullMethod string) bool) GRPCAuthOption

// AuthInterceptor returns an Interceptor (unary + stream) that authenticates
// each RPC and stores the Identity in the RPC context. With no verifier it is a
// construction error.
func AuthInterceptor(opts ...GRPCAuthOption) (Interceptor, error)
```

Wiring:

```go
authIC, _ := grpc.AuthInterceptor(grpc.WithGRPCBearerVerifier(jwtV))
chain := grpc.NewInterceptorChain(
    grpc.LoggingInterceptor(log),
    authIC,
)
grpc.Register(ctx, "api", ctrl, cfg, log, grpc.WithInterceptors(chain))
```

**Health-service note:** unlike HTTP (where health is outside the chain), gRPC
interceptors run for *all* services including `grpc.health.v1.Health` and
reflection. The default `AuthInterceptor` therefore **auto-skips** the standard
health (`/grpc.health.v1.Health/*`) and reflection
(`/grpc.reflection.v1.*`) method prefixes so k8s gRPC health probes keep working
without the tool author having to remember a skipper. This default-skip set is
documented and overridable via `WithGRPCMethodSkipper`. *(Open Question 2.)*

**Failure surfacing (gRPC):** missing/invalid credential →
`status.Error(codes.Unauthenticated, "unauthenticated")`; authenticated-but-denied
→ `codes.PermissionDenied`. Generic message only; the specific cause is logged
WARN with the token redacted. For streams, the check runs once at stream open
before the handler is invoked.

### 5. Identity context key (shared)

Both adapters store `*authn.Identity` under one unexported context key defined in
`pkg/authn`, with `authn.IdentityFromContext(ctx)` as the canonical accessor;
`http.IdentityFromContext` / `grpc.IdentityFromContext` are thin re-exports so a
handler reads identity the same way regardless of transport.

## Data Models

No persisted models. New exported types: `authn.Identity`, `authn.Verifier`,
`authn.KeyEntry`, `authn.JWTConfig`, `authn.AuthorizeFunc`; option types and the
two adapter factories. Config keys (read by the tool author wiring the verifier,
**not** auto-read by any server — auth is never config-activated implicitly):

| Suggested key (tool-owned) | Meaning |
|----------------------------|---------|
| `server.auth.jwt.issuer` | expected `iss` |
| `server.auth.jwt.audiences` | acceptable `aud` list |
| `server.auth.jwt.jwks_url` *or* `.oidc_issuer` | key source |
| `server.auth.apikeys` | key→subject entries (resolved via `pkg/credentials`) |

These keys are a **documented convention**, parsed by the tool, not magic keys a
GTB server reads on its own. *(Open Question 3: ship a `WithAuthFromConfig(cfg)`
convenience that reads this convention, or leave all wiring explicit?)*

## Error Cases

| Condition | Behaviour |
|-----------|-----------|
| No verifier configured on `AuthMiddleware`/`AuthInterceptor` | construction error — fail-closed, never a silent open server |
| Empty API-key set | construction error |
| Missing credential | 401 / `Unauthenticated`, generic body, `WWW-Authenticate` (HTTP) |
| Malformed `Authorization` header (no `Bearer ` prefix) | 401 / `Unauthenticated` |
| Bad signature / unknown kid / `alg:none` / HMAC-with-JWKS | 401 / `Unauthenticated`; cause logged WARN, token redacted |
| `iss`/`aud` mismatch, `exp`/`nbf` outside leeway | 401 / `Unauthenticated` |
| JWKS fetch fails / endpoint down | 401 / `Unauthenticated` (fail-closed); error logged; cached keys used if still within validity |
| JWKS URL not HTTPS, oversized, too many keys | construction/refresh error (rejected) |
| Verified but `AuthorizeFunc` returns false | 403 / `PermissionDenied`, generic body |
| Health/reflection method (gRPC) | skipped by default; reachable unauthenticated |

All client-facing responses are **generic**; specific causes never cross the
wire. Server logs carry the detail with credentials redacted via `pkg/redact`.

## Testing Strategy

Table-driven, `t.Parallel()`, `logger.NewNoop()`. New `pkg/` code ≥90% coverage.
No package-level mocking hooks — the JWKS `*http.Client` and a clock function are
injected via fields/options (per the project's race-avoidance rule), so token
expiry and JWKS responses are deterministic.

`pkg/authn`:

- API-key: accepts each configured key; rejects unknown; rejects empty set at
  construction; a benchmark/structural assertion that all entries are iterated
  (no early-return) and comparison uses fixed-width digests.
- JWT: valid token (RS256/ES256) passes; expired/`nbf`-future fail (with leeway
  boundary cases); wrong `iss`/`aud` fail; `alg:none` and HMAC-with-JWKS
  rejected; unknown `kid` triggers exactly one bounded refresh then fails;
  oversized/non-HTTPS JWKS rejected; OIDC discovery resolves `jwks_url` from a
  fake `/.well-known/openid-configuration` (`httptest.Server`).
- JWKS cache: concurrent verifies hit the cache (single-flight) under `-race`.

`pkg/http` / `pkg/grpc` adapters:

- success stores `Identity` in context, handler/RPC sees it via
  `IdentityFromContext`;
- failure yields the right status, generic body, `WWW-Authenticate` (HTTP), and
  a single redacted WARN log (assert the token does not appear);
- `WithAuthorize` false → 403 / `PermissionDenied`;
- HTTP: health endpoints (outside the chain) reachable without a credential;
- gRPC: default skip of health/reflection verified; custom skipper honoured;
- stream interceptor checks once at open.

### E2E BDD (Godog) assessment

Per the [Godog BDD strategy](2026-03-28-godog-bdd-strategy.md): server auth is a
**user-facing transport behaviour** with clear Given/When/Then shape ("Given a
server requiring a bearer token / When I call without one / Then I get 401").
A **small** smoke-level set of E2E scenarios (HTTP 401/200, gRPC
Unauthenticated/OK, health-probe-stays-open) is warranted under the existing
controls/CLI E2E harness, gated `INT_TEST_E2E`. Unit tests carry the
verification-matrix burden; BDD covers the wired-end-to-end happy/sad path only.

## Implementation Phases

Phases 1 and 2 are **independently shippable** and independently approvable —
deliberately so, given the feasibility tension on JWKS/OIDC.

1. **`pkg/authn` core + API-key verifier + adapters (foundation-clean).**
   `Identity`, `Verifier`, `AuthorizeFunc`, `RequireScopes`/`RequireClaim`,
   constant-time `APIKeyVerifier`, context key + accessor; HTTP `AuthMiddleware`
   and gRPC `AuthInterceptor`; failure surfacing + redacted logging; tests;
   docs. **No new direct dependency, no network fetching.** This phase alone is
   an unambiguous foundation-level win.
2. **JWT/OIDC verifier (the debatable phase).** Promote `golang-jwt/jwt/v5` to a
   direct dependency; `JWTVerifier`, bounded `jwksCache`, HTTPS-only fetch,
   OIDC-discovery-of-JWKS-only; tests. **Gate on the Open-Question-1 decision** —
   if rejected, Phase 1 still ships and tools bring their own JWT verifier
   implementing `authn.Verifier`.
3. **Generator + docs.** Scaffold a **commented-out** auth chain entry in the
   generated server wiring (opt-in, never active by default); new
   `docs/components/authn.md`; update `docs/components/http.md`,
   `docs/components/grpc.md`, `docs/components/gateway.md`; cross-link the
   threat model from `docs/development/security.md`; add a decision-log entry
   recording the foundation-vs-application reasoning.

## Open Questions

**Resolve these with Matt before implementation — do not start coding until each
is answered or explicitly deferred.**

1. **JWT/OIDC in scope, or API-key only?** Phase 1 (API-key) is an
   unambiguous foundation win. Phase 2 (JWKS fetch+cache, OIDC discovery) is the
   part in genuine tension with the `pkg/cache`/distributed-tracing rejections.
   Options: (a) ship both; (b) ship Phase 1 now, defer Phase 2 to a separate
   spec/decision; (c) ship Phase 1 + define the `Verifier` interface only, and
   leave **all** JWT to tool authors (GTB ships no JWKS code at all). The author
   leans **(b)**: land the clearly-foundation API-key + adapters + interface,
   and decide JWKS on its own merits.
2. **gRPC default skip set.** Auto-skip health + reflection (proposed), or
   require the tool author to opt out explicitly via `WithGRPCMethodSkipper`?
   Auto-skip is safer (health probes don't silently break) but is an implicit
   policy. HTTP needs no equivalent because health is already outside the chain.
3. **`WithAuthFromConfig(cfg)` convenience?** Provide a helper that reads the
   `server.auth.*` convention keys and constructs verifiers, or keep all wiring
   explicit in tool code (more boilerplate, zero magic)? Explicit wiring is more
   in keeping with "no implicit config activation"; a convenience helper trades
   that for ergonomics.
4. **mTLS as a third verifier later?** Out of scope here. Confirm it belongs in a
   future `pkg/tls`/transport spec rather than `pkg/authn` (client-cert identity
   is a transport property, not a bearer credential).
5. **Authorization surface ceiling.** Is `AuthorizeFunc` + `RequireScopes`/
   `RequireClaim` the agreed maximum? Anything richer (role maps, policy
   evaluation) is explicitly an application concern and stays out — confirm we
   hold that line.
6. **Multiple-scheme precedence.** When both a bearer token and an API-key header
   are present, bearer-wins is proposed. Confirm, or prefer "reject ambiguous"
   (fail-closed on two credentials)?

## Resolutions (open questions confirmed with user 2026-06-21)

1. **JWT/OIDC scope** — RESOLVED: **ship BOTH** Phase 1 (API-key verifier +
   HTTP/gRPC adapters + `Verifier` interface) **and** Phase 2 (JWKS-fetch+cache
   OIDC verifier). Rationale (overrides the draft's "defer Phase 2"): GTB already
   offers the **WKD** pattern for signing-key resolution — fetching keys from a
   well-known HTTPS location with caching — so a JWKS verifier is the same accepted
   shape, not the rejected general `pkg/cache`. Include the tooling to help engineers
   do enterprise/SSO auth. The JWKS cache stays single-purpose and hard-capped, OIDC
   *discovery* only (no login/issuance flow).
2. **gRPC default skip set** — RESOLVED: **auto-skip health + reflection** methods
   (safe default so probes don't silently break); `WithGRPCMethodSkipper` overrides.
3. **`WithAuthFromConfig`** — RESOLVED: **explicit wiring only, no helper.** Auth is
   never config-activated — consistent with "no implicit config activation" and
   appropriate for a security boundary.
4. **mTLS verifier** — RESOLVED: **include an mTLS / client-cert verifier in
   `pkg/authn` now**, as a third `Verifier` alongside API-key and JWT/OIDC. (Departs
   from the draft's "defer to a future tls spec" — broader scope accepted.)
5. **Authorization ceiling + extension seam** — RESOLVED: **no built-in policy
   model** (no role maps / policy engine / RBAC-ABAC DSL — that stays an application
   concern). `AuthorizeFunc` + `RequireScopes`/`RequireClaim` is the shipped ceiling.
   **BUT** `AuthorizeFunc` must be deliberately designed as a *policy-model-shaped
   hole*: it receives enough context (resolved identity, scopes, claims, and request
   metadata) for a user to implement arbitrary custom authorization externally
   without GTB shipping the engine. Design the seam for that explicitly.
6. **Multiple-scheme precedence** — RESOLVED: **reject ambiguous / fail closed** when
   both a bearer token and an API-key header are present (no silent credential
   selection). (Departs from the draft's bearer-wins proposal.)

> Note: the Feasibility Verdict below predates these resolutions. Q1 deliberately
> adopts both phases (with the WKD-precedent rationale), so the verdict's "gate/defer
> Phase 2" recommendation is superseded; the philosophy tension is resolved in favour
> of shipping JWKS, justified by the analogous accepted WKD pattern.

## Feasibility Verdict

**FEASIBLE-WITH-CAVEATS.**

- **Phase 1 (API-key verifier + HTTP/gRPC adapters + `Verifier` interface):**
  clearly **FEASIBLE and foundation-aligned.** It secures transports GTB already
  owns, via the middleware/interceptor extension points that are an accepted GTB
  pattern explicitly intended for auth, and it ships no policy, identity model,
  or network I/O. It centralises a genuine security footgun (constant-time key
  comparison + non-leaky failure surfacing). Recommend proceeding.

- **Phase 2 (JWT/OIDC with JWKS fetch+cache):** **FEASIBLE-WITH-CAVEATS.** This
  is the honest tension. JWKS-fetch-and-cache reintroduces an
  HTTP-client-with-TTL-cache shape close to the rejected `pkg/cache`, and "OIDC"
  edges toward service-framework territory. The mitigations that keep it on the
  foundation side: it is a **single-purpose bounded cache** (one key type, one
  purpose, hard caps) not a general cache; it does **OIDC discovery only** (no
  login/issuance flow); and it is **strictly separable** from Phase 1. The
  recommendation is to **gate Phase 2 on an explicit decision (Open Question 1)**
  and, if there is any doubt, **defer it** — Phase 1 plus the `Verifier`
  interface lets tools supply their own JWT verification with zero loss of the
  foundation value. **Do not treat Phase 2 as automatically approved by
  approving this spec.**

Net: approve Phase 1 as foundation-level; treat Phase 2 as a deliberate,
separately-ratified decision rather than a default.
