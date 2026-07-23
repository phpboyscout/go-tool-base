---
title: "Transport gateway: apply the middleware chain exactly once on the Register path"
description: "gateway.Register wraps the gateway handler with the middleware chain via newWithOptions and then also forwards the same chain to transporthttp.Register, which wraps it again — so every middleware runs twice per REST request on the managed-gateway path. Fix by building the raw mux on the Register path and letting transporthttp.WithMiddleware be the single application point, with a regression test that counts middleware invocations."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - transport
  - gateway
  - middleware
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Transport gateway: apply the middleware chain exactly once on the Register path

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH finding, transport section),
    [transport server stack extraction](2026-07-17-transport-server-stack.md) (where the gateway module landed),
    [transport logging middleware](2026-03-26-transport-logging-middleware.md) (one of the chains that now runs twice)

---

## 1. Problem

`go/transport/gateway.Register` applies the configured HTTP middleware chain **twice**:

1. `Register` (`go/transport/gateway/gateway.go:112-136`) calls `newWithOptions`
   at line 125, which — when `hasMiddleware` — wraps the gateway handler with
   `o.middleware.Then(handler)` (`gateway.go:94-96`).
2. It then *also* forwards `transporthttp.WithMiddleware(o.middleware)` at lines
   131-133 to `transporthttp.Register`, whose `register` wraps the same
   (already-wrapped) handler again with `rc.chain.Then(handler)`
   (`go/transport/http/server.go:413-415`).

The `WithMiddleware` doc comment (`gateway.go:54-60`) describes the intended
design: on the `New` path the chain wraps the returned handler directly; on the
`Register` path it is threaded to the managed server so health endpoints stay
outside the chain. The implementation does **both** on the Register path.

**Failure scenario.** Every middleware runs twice per REST request through a
managed gateway:

- A rate limiter consumes **two tokens per request** — effective capacity is
  halved and each request double-counts toward 429.
- Request logging emits two access-log lines per request.
- An OTel middleware opens a nested duplicate server span.
- An auth middleware verifies the credential twice; on failure the outer 401 is
  then processed again by the inner chain's writer path.

The existing test (`gateway_register_test.go`) only sets an idempotent header,
so it cannot detect the double application. GTB's
`pkg/gateway.RegisterFromConfig` (`pkg/gateway/config_adapter.go:161-183`)
forwards the resolved chain via `transportgateway.WithMiddleware`
(`config_adapter.go:97`), so every GTB tool using the managed gateway inherits
the bug.

## 2. Proposed change

On the `Register` path, build the **raw** mux and let
`transporthttp.WithMiddleware` be the single application point — exactly what
the `WithMiddleware` doc comment already promises:

```go
func Register(...) (*http.Server, error) {
    o := newOptions(opts)

    // Raw grpc-gateway mux — NO middleware applied here. The managed server
    // is the single application point, keeping /healthz etc. outside the chain.
    handler, err := o.serveMux(ctx, conn, register)
    if err != nil {
        return nil, err
    }

    httpOpts := []any{}
    if o.hasMiddleware {
        httpOpts = append(httpOpts, transporthttp.WithMiddleware(o.middleware))
    }

    return transporthttp.Register(ctx, id, controller, logger, handler, httpSettings, httpTLS, httpOpts...)
}
```

`serveMux` (`gateway.go:78-86`) already exists as the middleware-free
constructor; `Register` simply calls it instead of `newWithOptions`. The `New`
path is untouched — there the caller owns serving, so wrapping directly remains
correct. No public API changes; behaviour-only fix.

GTB's `pkg/gateway` needs no code change — it already threads the chain through
`transportgateway.WithMiddleware` and picks up the fix via a dependency bump.

## 3. Scope & release plan

1. **`go/transport`** — fix `gateway.Register`, add the regression test below.
   Release as a `fix(gateway)` patch.
2. **GTB** — bump the `go/transport` dependency (`fix(gateway)` or routine
   Renovate bump). No adapter changes.

No other modules are involved; `go/transit` is unaffected (the `Chain` type is
used correctly, it is the application count that was wrong).

## 4. Acceptance criteria

1. **Middleware-invocation-count regression test**: a `Register`-path test whose
   middleware increments a counter per request; a single REST request through
   the managed gateway must observe exactly **1** invocation (this test fails
   against the current code with 2). A non-idempotent middleware (e.g. one that
   appends to a slice or consumes a token) is used precisely because the
   existing header-setting test cannot detect double application.
2. Health endpoints (`/healthz`, `/livez`, `/readyz`) on the managed gateway
   server remain **outside** the chain (counter not incremented by probe
   requests) — the documented reason the Register path threads the chain to the
   server.
3. The `New` path still applies the chain exactly once (existing behaviour,
   asserted with the same counting technique).
4. GTB `pkg/gateway.RegisterFromConfig` E2E/adapter test confirms single
   application through the config-resolved chain after the dependency bump.
5. `WithMiddleware` doc comment re-verified against the implementation.

## 5. Open questions

1. Should `newWithOptions` be inlined into `New` (leaving `serveMux` +
   `New`/`Register` as the only entry points), or kept for symmetry? Removing it
   makes the "who applies the chain" decision visible at each call site.
2. Is a `fix` patch release sufficient, or does the behaviour change (rate
   limits effectively doubling back to configured capacity, one log line
   instead of two) warrant a release-note callout for downstreams that may have
   unknowingly compensated (e.g. halved their configured rate limits)?
