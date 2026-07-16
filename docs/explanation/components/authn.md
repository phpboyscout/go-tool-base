---
title: Authentication & Authorization
description: go-tool-base consumes the standalone go/authn module (API-key, JWT/OIDC, mTLS verifiers) and wires it into the HTTP AuthMiddleware and gRPC auth interceptor.
date: 2026-06-24
tags: [components, authn, security, authentication, authorization, jwt, oidc, mtls, http, grpc]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Authentication & Authorization

The request-authentication primitives have been **extracted into the standalone
[`gitlab.com/phpboyscout/go/authn`](https://gitlab.com/phpboyscout/go/authn)
module** (framework-free — only `cockroachdb/errors` and `golang-jwt/jwt/v5`). The
full documentation — the `Verifier`/`CertVerifier` API, the API-key/JWT-OIDC/mTLS
verifiers, the `AuthorizeFunc` seam, and the **security model** — now lives at:

> **[authn.go.phpboyscout.uk](https://authn.go.phpboyscout.uk)**

`authn` is framework-free, so go-tool-base consumes it **directly** (no adapter):
callers import `gitlab.com/phpboyscout/go/authn` and use `authn.Verifier`,
`authn.NewAPIKeyVerifier`, `authn.NewJWTVerifier`, `authn.NewMTLSVerifier`,
`authn.Identity`, and the `AuthorizeFunc` combinators as before. See the
[migration note](../../reference/migration/v0.x-authn-extracted.md) for the
import-path change.

## How go-tool-base uses it

GTB wires the module's verifiers into both server transports; the wiring is a GTB
concern and stays in the framework:

- **HTTP** — `pkg/http`'s fail-closed `AuthMiddleware` wraps a verifier via
  `WithAPIKeyHeader` / `WithBearerVerifier` / `WithMTLSVerifier`, gates with
  `WithAuthorize`, and exposes the verified `*authn.Identity` through
  `gtbhttp.IdentityFromContext`.
- **gRPC** — `pkg/grpc`'s auth interceptor applies the same verifiers to the RPC
  metadata / peer certificate and puts the `Identity` on the RPC context.

For the end-to-end setup — API keys, JWT/OIDC, mTLS, and authorization — see
[How to verify requests](../../how-to/verify-requests-with-authn.md). The verifiers'
threat model (fail-closed, leak-nothing, JWT hardening) is documented on the
[module microsite](https://authn.go.phpboyscout.uk/explanation/security-model/).
