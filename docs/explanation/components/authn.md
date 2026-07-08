---
title: Authentication & Authorization
description: Opt-in credential verification (API-key, JWT/OIDC, mTLS) and a minimal authorization seam for the HTTP and gRPC transports.
date: 2026-06-24
tags: [components, authn, security, authentication, authorization, jwt, oidc, mtls, http, grpc]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Authentication & Authorization

The `pkg/authn` package answers the one question GTB's transports could not before:
*"who is calling, and are they allowed to?"* It provides transport-agnostic
**credential verification** (API key, JWT/OIDC bearer token, and mTLS client
certificate) plus a deliberately minimal **authorization** hook, wired into the
existing HTTP middleware chain and gRPC interceptor chain.

Two principles frame everything below:

- **100% opt-in.** A server that registers no auth middleware behaves exactly as
  it does today. Nothing is auth-gated implicitly, and nothing is read from config
  to silently turn auth on — you wire it explicitly.
- **Verification, not policy.** GTB verifies a credential and exposes the verified
  identity. It ships **no** user store, **no** RBAC/policy engine, and **no**
  OAuth2/OIDC login or token-issuance flow. Authorization is a single predicate
  you supply. This is the boundary that keeps auth a transport concern, not an
  application framework.

## At a glance

```go
// 1. Build a verifier (transport-agnostic).
keys, _ := authn.NewAPIKeyVerifier(
    authn.KeyEntry{Key: os.Getenv("CI_KEY"), Subject: "ci-runner"},
)

// 2. Wrap it as HTTP middleware and add it to a chain.
authMW, _ := gtbhttp.AuthMiddleware(
    gtbhttp.WithAPIKeyHeader("X-API-Key", keys),
    gtbhttp.WithAuthorize(authn.RequireScopes("api:write")),
    gtbhttp.WithAuthLogger(props.Logger),
)
chain := gtbhttp.NewChain(
    gtbhttp.SecurityHeadersMiddleware(),
    gtbhttp.LoggingMiddleware(props.Logger),
    authMW, // after logging, so failures are logged; before the handler
)
gtbhttp.RegisterFromContainable(ctx, "api", controller, props.Config, props.Logger, mux,
    gtbhttp.WithMiddleware(chain))

// 3. Read the verified identity in a handler.
id, ok := gtbhttp.IdentityFromContext(r.Context())
```

`/healthz`, `/livez`, `/readyz` are mounted outside the chain, so a global auth
middleware never gates liveness/readiness probes.

## Identity

A successful verification yields an `*authn.Identity`:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/authn](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/authn) for the full API definition.


Read it downstream with `IdentityFromContext(ctx)` — the **same** context key is
used by both transports, so a handler reads identity the same way over HTTP or
gRPC:

```go
id, ok := gtbhttp.IdentityFromContext(ctx) // or gtbgrpc.IdentityFromContext(ctx)
```

## Verifiers

`pkg/authn` ships three verifiers. The first two implement the `Verifier`
interface (a credential string in, an `Identity` out); mTLS implements
`CertVerifier` because a client certificate is a transport property, not a string.


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/authn](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/authn) for the full API definition.


### API key

```go
v, err := authn.NewAPIKeyVerifier(
    authn.KeyEntry{Key: "…", Subject: "ci-runner"},
    authn.KeyEntry{Key: "…", Subject: "admin"},
)
```

- **Constant-time comparison.** Keys are compared via a fixed-width SHA-256 digest
  with `crypto/subtle`, iterating **all** entries (no early return) — so neither
  match/no-match, key length, nor *which* key matched is timing-distinguishable.
- **Fail-closed construction.** An empty key set, or any entry with an empty key,
  is a construction error — the verifier never accepts "any key".
- Resolve the key material via [`pkg/credentials`](credentials.md); `pkg/authn`
  receives already-resolved strings and never logs them.

### JWT / OIDC

```go
v, err := authn.NewJWTVerifier(ctx, authn.JWTConfig{
    Issuer:    "https://issuer.example.com",
    Audiences: []string{"my-api"},          // any-of; empty disables the aud check
    JWKSURL:   "https://issuer.example.com/.well-known/jwks.json",
})

// …or discover the JWKS endpoint from the OIDC provider:
v, err := authn.NewJWTVerifier(ctx, authn.JWTConfig{Audiences: []string{"my-api"}},
    authn.WithOIDCDiscovery("https://issuer.example.com"))
```

Signing keys are fetched from the JWKS endpoint (or the OIDC-discovered one) and
cached. The verifier validates the signature and `iss` / `aud` (any-of) / `exp` /
`nbf`, tolerating small clock skew (`Leeway`, default 60s). `JWTConfig` fields:

| Field | Default | Meaning |
|-------|---------|---------|
| `Issuer` | — (required) | Expected `iss` |
| `Audiences` | none | Acceptable `aud` values (any-of); empty disables the check |
| `JWKSURL` | — | JWKS endpoint (HTTPS); resolved by `WithOIDCDiscovery` |
| `Leeway` | 60s | Clock-skew tolerance on exp/nbf/iat |
| `RefreshInterval` | 15m | How often the JWKS is refetched |
| `AllowedAlgorithms` | RS/ES 256/384/512 | Accepted `alg` values |
| `HTTPClient` | timeout client | Client used to fetch the JWKS / OIDC doc (HTTPS) |

**`WithOIDCDiscovery` is discovery only** — it fetches
`/.well-known/openid-configuration` to resolve the JWKS endpoint and validate the
issuer. There is no login flow, no token endpoint use, no token issuance. GTB
*verifies* tokens; it never mints, refreshes, or brokers them.

> **Why JWKS is foundation-level, not the rejected `pkg/cache`.** The JWKS cache
> is single-purpose and hard-bounded — it caches exactly one kind of thing for one
> purpose. It mirrors the accepted **WKD** pattern GTB already uses to resolve
> signing keys from a well-known HTTPS location. See [Security model](#security-model).

### mTLS (client certificate)

```go
v := authn.NewMTLSVerifier()                               // Subject from CN, then DNS SAN, then URI SAN
v := authn.NewMTLSVerifier(authn.WithCertSubject(myFunc))  // custom subject derivation
```

mTLS derives identity from the **already-verified** client-certificate chain. The
cryptographic verification is the TLS stack's job — configure the server for
`RequireAndVerifyClientCert` with a `ClientCAs` pool (see [`pkg/tls`](tls.md)).
The verifier only extracts the subject.

## Authorization

Authorization is one tool-supplied predicate, run **after** successful
verification:

```go
type AuthorizeFunc func(ctx context.Context, id *Identity) bool
```

Returning `false` yields `403` / `PermissionDenied`. Two convenience constructors
cover the common cases:

```go
authn.RequireScopes("api:read", "api:write") // Identity must carry all named scopes
authn.RequireClaim("role", "admin")          // a verified claim must equal a value
```

This is the **entire** authorization surface — there is no built-in role model or
policy DSL. But `AuthorizeFunc` is deliberately a *policy-model-shaped hole*: it
receives the resolved `Identity` **and** a context carrying the request route, so
you can implement arbitrary external policy (a role map, an OPA call, …) without
GTB shipping the engine:

```go
policy := func(ctx context.Context, id *authn.Identity) bool {
    meta, _ := authn.RequestMetadataFromContext(ctx) // {Method, Path}
    if strings.HasPrefix(meta.Path, "/admin") {
        return authn.RequireScopes("admin")(ctx, id)
    }
    return id != nil
}
```

## HTTP — `AuthMiddleware`

```go
authMW, err := gtbhttp.AuthMiddleware(opts ...gtbhttp.AuthOption)
```

| Option | Effect |
|--------|--------|
| `WithBearerVerifier(v)` | Verify `Authorization: Bearer <token>` with `v` |
| `WithAPIKeyHeader(header, v)` | Verify the named header (e.g. `X-API-Key`) with `v` |
| `WithMTLSVerifier(v)` | Authenticate from the verified client cert when no header credential is presented |
| `WithAuthorize(fn)` | Run an authorization predicate after verification |
| `WithAuthLogger(l)` | Logger for redacted server-side failure logging |
| `WithAuthSkipper(pred)` | Skip auth for matching requests (e.g. a public sub-path or OPTIONS preflight) |

With **no** verifier configured, construction fails (fail-closed — never a silent
pass-through). The middleware is an ordinary `Middleware`, so it composes into any
`Chain` and reads the verified identity via `gtbhttp.IdentityFromContext`.

## gRPC — `AuthInterceptor`

```go
authIC, err := gtbgrpc.AuthInterceptor(opts ...gtbgrpc.GRPCAuthOption)
chain := gtbgrpc.NewInterceptorChain(gtbgrpc.LoggingInterceptor(log), authIC)
gtbgrpc.Register(ctx, "api", controller, cfg, log, gtbgrpc.WithInterceptors(chain))
```

| Option | Effect |
|--------|--------|
| `WithGRPCBearerVerifier(v)` | Verify the `authorization` metadata (`Bearer <token>`) |
| `WithGRPCAPIKeyMetadata(key, v)` | Verify the named metadata key |
| `WithGRPCMTLSVerifier(v)` | Authenticate from the peer's verified client cert |
| `WithGRPCAuthorize(fn)` | Authorization predicate |
| `WithGRPCAuthLogger(l)` | Redacted failure logger |
| `WithGRPCMethodSkipper(pred)` | Skip **additional** methods (the standard health/reflection services are already auto-skipped) |

It returns a paired unary + stream `Interceptor`; the stream check runs once at
stream open. **The standard health (`/grpc.health.v1.Health/*`) and reflection
(`/grpc.reflection.v1*`) services are auto-skipped** so k8s gRPC probes keep
working without you having to remember a skipper — a custom skipper *adds* to that
set, it does not replace it.

## Credential precedence & failure surfacing

**Precedence (fail-closed on ambiguity).** A bearer token and an API-key header/
metadata presented *together* are rejected as ambiguous — GTB never silently picks
one. Otherwise the presented header/metadata scheme is used; the **cookie**
(`WithCookieVerifier`, HTTP only) is an *ambient* credential consulted only when no
explicit header scheme is presented (an explicit `Authorization`/API-key header
always wins — the auto-sent cookie never overrides it); **mTLS authenticates only
when no header *or* cookie credential is presented**.

**Non-leaky failures.** On a missing/invalid credential the request gets a generic
`401` (HTTP, with `WWW-Authenticate`) or `codes.Unauthenticated` (gRPC); a
verified-but-unauthorized identity gets `403` / `codes.PermissionDenied`. The
client is told *nothing* about why — the specific cause (expired, bad signature,
unknown kid, wrong audience) is logged once at WARN with the credential
[redacted](redact.md). The handler/RPC never runs on failure.

| Condition | HTTP | gRPC |
|-----------|------|------|
| No verifier configured | construction error (fail-closed) | construction error |
| Missing / malformed credential | `401` + `WWW-Authenticate`, `{"error":"unauthorized"}` | `Unauthenticated` |
| Both bearer and API-key presented | `401` (ambiguous) | `Unauthenticated` |
| Bad signature / `alg:none` / HMAC / unknown kid / wrong iss·aud / expired | `401`, cause logged WARN | `Unauthenticated` |
| Verified but `AuthorizeFunc` false | `403`, `{"error":"forbidden"}` | `PermissionDenied` |
| Health / reflection (gRPC) | n/a | auto-skipped, reachable unauthenticated |

## Configuration convention

Auth is **never** activated from config — you wire verifiers explicitly. When a
tool reads auth parameters from config, the suggested (tool-owned) keys are:

| Suggested key | Meaning |
|---------------|---------|
| `server.auth.jwt.issuer` | expected `iss` |
| `server.auth.jwt.audiences` | acceptable `aud` list |
| `server.auth.jwt.jwks_url` *or* `.oidc_issuer` | key source |
| `server.auth.apikeys` | key→subject entries (resolved via [`pkg/credentials`](credentials.md)) |

These are a documented convention your tool parses and passes to the verifiers —
not magic keys a GTB server reads on its own.

## Security model

The verifiers centralise security primitives that are easy to get subtly wrong:

- **Constant-time API-key comparison** over fixed-width digests, iterating all
  entries — no timing oracle.
- **Algorithm-confusion defence.** `alg:none` is always rejected; HMAC (`HS*`) is
  rejected whenever a JWKS is configured, so an attacker cannot have the public key
  treated as an HMAC secret. Only the configured asymmetric algorithms are
  accepted.
- **Bounded JWKS fetching.** HTTPS-only endpoint, a capped response body and key
  count, a fetch timeout, and a single-flight, rate-limited refresh — so a stream
  of unknown-`kid` tokens cannot hammer the JWKS endpoint. On a refresh failure a
  still-valid cached key is used (fail-static, not fail-open).
- **No credential ever crosses back to the client** or reaches a log unredacted.

**Out of scope by design:** any user/identity store; a role/permission model or
policy DSL; session/cookie management; OAuth2/OIDC *login* or token issuance/
refresh. Those are application concerns. The authorization knob is a single
predicate — see [Authorization](#authorization).

## See also

- [How to Verify Requests (API Keys & JWT/OIDC)](../../how-to/verify-requests-with-authn.md) — the task recipe for wiring verifiers into a server
- [HTTP](http.md) — the server middleware chain `AuthMiddleware` plugs into
- [gRPC](grpc.md) — the interceptor chain `AuthInterceptor` plugs into
- [TLS](tls.md) — configure client-certificate verification for mTLS
- [credentials](credentials.md) — resolve API keys / tokens without hardcoding
- [redact](redact.md) — the redaction applied to server-side auth failure logs
