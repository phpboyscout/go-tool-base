---
title: How to Verify Requests (API Keys & JWT/OIDC)
description: Authenticate HTTP requests with API keys or JWT/OIDC using pkg/authn and the AuthMiddleware.
date: 2026-06-26
tags: [how-to, authn, security, http, jwt, oidc, api-key]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Verify Requests (API Keys & JWT/OIDC)

When your tool exposes an HTTP service, you usually need to authenticate callers.
The standalone [`go/authn`](https://authn.go.phpboyscout.uk) module provides the
verifiers (API key, JWT/OIDC, mTLS) and `pkg/http` wraps them in a fail-closed
`AuthMiddleware`. This guide shows the common setups. For the threat model and
verification internals, see the [Auth component](../explanation/components/authn.md)
and the [module docs](https://authn.go.phpboyscout.uk).

## Authenticate with API keys

Build a verifier from one or more `KeyEntry` secrets, then read them from a header:

```go
import (
    "gitlab.com/phpboyscout/go/authn"
    gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
)

verifier, err := authn.NewAPIKeyVerifier(
    authn.KeyEntry{Key: os.Getenv("CI_TOKEN"), Subject: "ci-runner"},
    authn.KeyEntry{Key: os.Getenv("ADMIN_TOKEN"), Subject: "admin"},
)
if err != nil {
    return err
}

auth, err := gtbhttp.AuthMiddleware(
    gtbhttp.WithAPIKeyHeader("X-API-Key", verifier),
)
if err != nil {
    return err
}

// AuthMiddleware is a func(http.Handler) http.Handler — wrap your mux:
http.ListenAndServe(":8080", auth(mux))
```

Keys are compared in constant time. Requests without a valid key get `401`.

## Authenticate with JWT / OIDC

Point a JWT verifier at your issuer. With `WithOIDCDiscovery` the JWKS endpoint is
resolved from `/.well-known/openid-configuration`; otherwise set `JWKSURL` yourself.

```go
verifier, err := authn.NewJWTVerifier(ctx,
    authn.JWTConfig{
        Issuer:    "https://issuer.example.com",
        Audiences: []string{"my-api"}, // any-of; empty disables the aud check
        // Leeway, RefreshInterval, AllowedAlgorithms have sane defaults.
    },
    authn.WithOIDCDiscovery("https://issuer.example.com"),
)
if err != nil {
    return err
}

auth, err := gtbhttp.AuthMiddleware(
    gtbhttp.WithBearerVerifier(verifier), // reads "Authorization: Bearer <token>"
)
if err != nil {
    return err
}
http.ListenAndServe(":8080", auth(mux))
```

The verifier rejects `alg:none`, rejects HMAC algorithms whenever a JWKS is
configured (algorithm-confusion defence), and tolerates small clock skew
(`Leeway`, default 60s). Signing keys are fetched over HTTPS and cached.

## Read the caller's identity in a handler

After authentication, the verified `*authn.Identity` is on the request context:

```go
func handler(w http.ResponseWriter, r *http.Request) {
    id, ok := gtbhttp.IdentityFromContext(r.Context())
    if !ok {
        http.Error(w, "unauthenticated", http.StatusUnauthorized)
        return
    }
    // id.Subject — the principal; id.Method — "apikey" | "jwt" | "mtls"
    // id.Claims  — verified JWT claims (nil for API key); id.Scopes — parsed scopes
    fmt.Fprintf(w, "hello %s\n", id.Subject)
}
```

## Authorize, not just authenticate

Authentication proves *who* the caller is; authorization decides *what they may
do*. Pass an `AuthorizeFunc` to gate on claims or scopes — it runs after a
successful verify, and a `false` result yields `403`:

```go
requireAdmin := authn.AuthorizeFunc(func(_ context.Context, id *authn.Identity) bool {
    return slices.Contains(id.Scopes, "admin")
})

auth, err := gtbhttp.AuthMiddleware(
    gtbhttp.WithBearerVerifier(verifier),
    gtbhttp.WithAuthorize(requireAdmin),
)
```

## Notes

- **Fail-closed.** `AuthMiddleware` returns an error if you pass no verifier — it
  never silently allows unauthenticated traffic.
- **Combine methods.** Pass several `With…Verifier` options to accept a bearer
  token *or* an API key; the first that matches the request wins.
- **mTLS.** For client-certificate auth, use `WithMTLSVerifier(authn.NewMTLSVerifier())`
  alongside TLS-terminating server config.

## Related

- [Auth component](../explanation/components/authn.md) — threat model, JWKS caching, the `AuthorizeFunc` policy seam
- [Use middleware](use-middleware.md) — how middleware composes on the server
- [Add security headers](security-headers.md)
