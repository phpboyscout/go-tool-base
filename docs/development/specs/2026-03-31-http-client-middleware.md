---
title: "HTTP Client Middleware Chain"
description: "RoundTripper middleware chain for the HTTP client, enabling composable request logging, auth injection, and rate limiting."
date: 2026-03-31
status: DRAFT
tags:
  - specification
  - http
  - client
  - middleware
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# HTTP Client Middleware Chain

Authors
:   Matt Cockayne

Date
:   31 March 2026

Status
:   DRAFT

---

## Overview

The HTTP server has a composable middleware chain (`NewChain`, `Append`, `Then`). The HTTP client has retry support but no equivalent middleware pattern. Tool authors making API calls commonly need: request logging, auth header injection, rate limiting, and response caching — each as composable, reusable layers.

This spec adds a `RoundTripper` middleware chain to `pkg/http` that mirrors the server-side pattern, allowing tool authors to compose client behaviours without subclassing or wrapping `http.Client` manually.

---

## Design

### `ClientMiddleware` Type

```go
// ClientMiddleware wraps an http.RoundTripper with additional behaviour.
type ClientMiddleware func(next http.RoundTripper) http.RoundTripper
```

### `ClientChain` Type

```go
// ClientChain composes ClientMiddleware in order. The first middleware
// is the outermost wrapper (executes first on request, last on response).
type ClientChain struct {
    middlewares []ClientMiddleware
}

func NewClientChain(middlewares ...ClientMiddleware) ClientChain
func (c ClientChain) Append(middlewares ...ClientMiddleware) ClientChain
func (c ClientChain) Then(rt http.RoundTripper) http.RoundTripper
```

### Integration with `NewClient`

```go
// WithClientMiddleware applies a middleware chain to the client's transport.
func WithClientMiddleware(chain ClientChain) ClientOption
```

### Built-in Client Middleware

#### Request Logging

```go
// WithRequestLogging logs each outbound request and response at the specified level.
func WithRequestLogging(log logger.Logger) ClientMiddleware
```

Logs: method, URL, status code, duration. Headers and body are NOT logged (security).

#### Auth Header Injection

```go
// WithBearerToken injects an Authorization: Bearer header on every request.
func WithBearerToken(token string) ClientMiddleware

// WithBasicAuth injects an Authorization: Basic header on every request.
func WithBasicAuth(username, password string) ClientMiddleware
```

#### Rate Limiting

```go
// WithRateLimit limits outbound requests to the specified rate.
func WithRateLimit(requestsPerSecond float64) ClientMiddleware
```

Uses a token bucket algorithm. Blocks until a token is available or the request context is cancelled.

---

## Usage Example

```go
chain := gtbhttp.NewClientChain(
    gtbhttp.WithRequestLogging(props.Logger),
    gtbhttp.WithBearerToken(os.Getenv("API_TOKEN")),
    gtbhttp.WithRateLimit(10),
)

client := gtbhttp.NewClient(
    gtbhttp.WithTimeout(30 * time.Second),
    gtbhttp.WithClientMiddleware(chain),
)
```

---

## Open Questions

1. Should retry (`WithRetry`) be refactored as a `ClientMiddleware` for consistency, or remain a separate `ClientOption`?
