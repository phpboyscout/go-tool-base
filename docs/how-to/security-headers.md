---
title: Add HTTP Security Headers
description: How to implement HTTP security headers for tools built on GTB using the middleware chain.
date: 2026-04-02
tags: [how-to, http, security, middleware, headers]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Add HTTP Security Headers

GTB's HTTP server does not set security headers by default. This is a deliberate design choice: different tools have different security requirements. A CLI tool serving a local management API has very different header needs than a service exposed to the internet behind a reverse proxy. Imposing a fixed set of headers at the framework level would either be too restrictive for some tools or give a false sense of security to others.

Instead, GTB provides the `pkg/http` middleware chain so tool authors can compose exactly the headers their deployment requires.

## 1. Write a Security Headers Middleware

Create a middleware function that sets the headers your tool needs:

```go
package mymiddleware

import "net/http"

// SecurityHeaders returns an HTTP middleware that sets common security headers.
// Adjust the values to match your tool's requirements.
func SecurityHeaders() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Prevent MIME-type sniffing
            w.Header().Set("X-Content-Type-Options", "nosniff")

            // Prevent clickjacking
            w.Header().Set("X-Frame-Options", "DENY")

            // Enforce HTTPS (only set this if your service terminates TLS)
            w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")

            // Basic CSP — restrict resources to same origin
            w.Header().Set("Content-Security-Policy", "default-src 'self'")

            // Disable browser features that are not needed
            w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

            // Control referrer information
            w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

            next.ServeHTTP(w, r)
        })
    }
}
```

!!! warning "HSTS Requires TLS"
    Only set `Strict-Transport-Security` if your service terminates TLS directly or you are certain an upstream proxy handles it. Setting HSTS on a plaintext HTTP service can lock browsers out if the domain later serves HTTPS with a valid certificate.

## 2. Apply the Middleware via the Chain

Use `http.NewChain` to compose your security middleware with any other middleware (logging, recovery, etc.), then pass the chain to `http.Register`:

```go
import (
    gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
    "github.com/phpboyscout/go-tool-base/pkg/controls"
)

func registerHTTPServer(ctx context.Context, controller controls.Controllable, cfg config.Containable, l logger.Logger, handler http.Handler) error {
    chain := gtbhttp.NewChain(
        SecurityHeaders(),
        gtbhttp.LoggingMiddleware(l),
    )

    _, err := gtbhttp.Register(ctx, "http", controller, cfg, l, handler,
        gtbhttp.WithMiddleware(chain),
    )

    return err
}
```

!!! important "Health Endpoints Are Outside the Chain"
    The `/healthz`, `/livez`, and `/readyz` endpoints are mounted outside the middleware chain by `http.Register`. Security headers set via `WithMiddleware` do not apply to health endpoints. If you need headers on health endpoints as well, wrap the entire `http.Server` handler separately.

## 3. Choose Headers for Your Deployment

Not every header is appropriate for every tool. Use this table as a starting point:

| Header | When to Use | When to Skip |
|--------|-------------|--------------|
| `X-Content-Type-Options: nosniff` | Always | Never |
| `X-Frame-Options: DENY` | Services that serve HTML or are reachable from browsers | Pure API services with no browser clients |
| `Strict-Transport-Security` | Services that terminate TLS or sit behind a TLS-terminating proxy that strips/re-adds the header | Local-only management APIs, plaintext services |
| `Content-Security-Policy` | Services that serve HTML content | JSON-only APIs (harmless but unnecessary) |
| `Permissions-Policy` | Services with browser-facing endpoints | CLI-only tools |
| `Referrer-Policy` | Services that serve HTML with outbound links | API-only services |

## 4. Validate Your Headers

Use the OWASP Secure Headers Project to check that your header configuration meets current best practices:

- [OWASP Secure Headers Project](https://owasp.org/www-project-secure-headers/)
- [Security Headers Scanner](https://securityheaders.com/) — test a deployed endpoint

For local testing, inspect response headers with `curl`:

```bash
curl -sI https://localhost:8080/your-endpoint | grep -iE "x-content|x-frame|strict-transport|content-security"
```

## Summary

GTB delegates security header decisions to tool authors because there is no single correct set of headers for all tools. Use the `pkg/http` middleware chain to compose the headers appropriate for your deployment, and validate them against OWASP guidance before going to production.
