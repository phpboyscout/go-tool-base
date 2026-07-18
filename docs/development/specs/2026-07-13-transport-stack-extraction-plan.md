---
title: "Transport stack & foundations — extraction sequence plan"
description: "The dependency-ordered plan for extracting the leaf utilities (redact, regexutil, browser), the transport foundations (tls, authn), and the transport stack (http/grpc/gateway) into standalone modules. Establishes the phased order forced by the dependency graph, the per-package extraction shape (repoint vs facade), and the client/server module split for the transport layer."
date: 2026-07-13
status: IMPLEMENTED
tags:
  - specification
  - module-extraction
  - transport
  - http
  - grpc
  - gateway
  - planning
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Transport stack & foundations — extraction sequence plan

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-13

Status
:   IMPLEMENTED (2026-07-18) — shipped to main; originally APPROVED (open questions resolved in review 2026-07-13; design points D1–D4 deferred to their phases)

## Summary

With `controls` extracted, the next natural target is the **transport stack**
(`pkg/http`, `pkg/grpc`, `pkg/gateway`). But the transport packages import several
other **not-yet-extracted** GTB packages, and a framework-free module cannot
import `go-tool-base` (the depfootprint guard forbids it). So the transport stack
cannot go first: its dependencies must extract ahead of it. This plan establishes
the **dependency-forced order**, the **shape** of each extraction (pure repoint vs
facade, per [playbook §11.1](2026-07-12-go-module-extraction-playbook.md#111-repoint-vs-facade-is-decided-by-adapter-ownership-not-surface-size)),
and the **client/server module split** for the transport layer.

This is a sequence/planning spec. Each package still gets its own extraction (a
short spec or a direct §5 application) when its turn comes; this document fixes
the order and the design decisions that span packages.

## The dependency graph (measured 2026-07-13)

```
gateway → http(server), grpc, tls, config, logger
http    → { client: tls, circuitbreaker, ratelimit }
          { server: authn, redact, tls, controls*, config, logger }
grpc    → authn, redact, tls, circuitbreaker, ratelimit, config, logger  (server-only)
```

`config` and `logger` stay in GTB — their coupling is already adapter-confined
(each transport package has a `config_adapter.go`; logging is the `*slog.Logger`
seam). `controls` is already external (`go/controls`). Everything else
(`redact`, `tls`, `authn`, `circuitbreaker`, `ratelimit`) must be resolved before
the transport stack can be framework-free.

### Per-package shape

| Package | LOC | Consumers | Shape | Notes |
|---|--:|--:|---|---|
| `redact` | 252 | 8 | **pure repoint** | 0 GTB imports; transport dep **and** widely used → extract first |
| `regexutil` | 145 | 2 | pure repoint | independent of transport; ease-10 warm-up |
| `browser` | 206 | 3 | pure repoint | independent of transport; ease-10 warm-up |
| `tls` | 215 | 8 | **facade** | imports `config`+`logger`; `config_adapter.go` stays in GTB |
| `authn` | 872 | 4 | pure repoint | 0 GTB imports |
| `circuitbreaker` (internal) | 202 | 2 | pure | client-side `RoundTripper`; promote with the client module |
| `ratelimit` (internal) | 84 | 2 | pure | server-side middleware; promote with the server module |
| `http` | 2611 | 17 | **facade** | split client/server — see below |
| `grpc` | 1715 | 2 | **facade** | server-only |
| `gateway` | 309 | 0 | **facade** | composes http+grpc |

## The client/server split & the module map (decided 2026-07-13)

Measured dependency footprints differ sharply between the client and server
halves of *both* transport packages:

- **Client** (`http`: `client.go`, `client_middleware.go`, `retry.go`, client-side
  circuit-breaker round-tripper; `grpc`: `CircuitBreakerInterceptor`/`…Stream…`
  client interceptors, `OTelClientHandler` dial option): depends only on **`tls`
  (client config) + the resilience/middleware primitives**. No `authn`, `redact`,
  or `controls`.
- **Server** (`http`: `server.go`, `auth.go`, `security_headers.go`; all of
  `grpc` server; `gateway`): depends on **`authn`, `redact`, `tls`, `controls`,
  `config`**.

Both `http` and `grpc` therefore split cleanly into a light client half and a
heavy server half. The resulting module map (client extracted separately per your
call; shared middleware standalone per **O1**; grpc client included per **O5**):

- **`go/transportmw`** *(standalone shared middleware — O1)* — the transport-neutral
  **resilience primitives** (`circuitbreaker`, `ratelimit`) plus the **shared
  middleware** used by both client and server: request/response chaining, logging
  (with `redact`), and OTel instrumentation. One module with `httpmw` (RoundTripper
  + `http.Handler` middleware) and `grpcmw` (unary/stream interceptors +
  dial-option/handler) sub-packages. Deps: `go/redact`, `go/tls`, OTel contrib SDK,
  `*slog`. Consumed by every client and server module below.
- **`go/httpclient`** *(client — O5)* — hardened `http.Client` factory wiring the
  `httpmw` round-trippers. Light: `go/transportmw` + `go/tls` + stdlib. Exactly what
  `go/chat` needed; a client consumer never inherits `controls`/`authn`/`gateway`.
- **`go/grpcclient`** *(client — O5, not deferred)* — grpc client interceptors and
  dial options (circuit-breaker/retry/OTel client instrumentation). We have
  in-scope consumers that dial grpc without the server stack. Light: `go/transportmw`
  + `go/tls`.
- **`go/transport`** *(server stack)* — the `http` server, `grpc` server, and
  `gateway`, with server auth, security headers, server rate-limiting, and the
  `controls` health integration. One module, sub-packages (`transport/http`,
  `transport/grpc`, `transport/gateway`) so `gateway → http/grpc` stays an internal
  import with no three-way version lockstep. Requires `go/transportmw`,
  `go/httpclient`/`go/grpcclient` (where it reuses client bits), `go/controls`,
  `go/authn`, `go/redact`, `go/tls`, `go/observability`.

**Why the client/mw split pays off:** a single heavy module would force
`controls`/`authn`/`gateway` into every client-only consumer's `go.mod` even under
dead-code elimination. Splitting keeps the client + middleware graphs genuinely
light, which is the common reuse case.

**Observability brought forward (O2).** Transport instruments via the OTel
**contrib SDK directly** (not `pkg/telemetry`), so `observability` is not a hard
dependency — but per your call it extracts **before** the transport layer, so
`go/transportmw` and `go/transport` can consume `go/observability` for
provider/exporter setup rather than re-inventing it, and observability lands as an
independently useful module sooner.

## Recommended sequence

**Phase 1 — leaf warm-ups (pure repoints, exercise playbook §11):**

1. **`redact`** → `go/redact`. First: it is a transport dependency **and** has the
   most consumers (8). Highest leverage; near-mechanical.
2. **`regexutil`** → `go/regexutil`, **`browser`** → `go/browser`. Independent of
   the transport stack; low-risk warm-ups that can slot in anytime (good parallel
   or filler work).

**Phase 2 — foundations:**

3. **`tls`** → `go/tls` (facade; `config_adapter.go` stays in GTB).
4. **`authn`** → `go/authn` (pure repoint). Independent of `tls`; either order.
5. **`observability`** → `go/observability` (the `telemetry/otelcore` + `logs`/
   `metrics`/`tracing` group; **brought forward, O2**). Independent of the
   analytics/telemetry split; extract the observability half now so the transport
   layer can consume it.

**Phase 3 — shared transport middleware (O1):**

6. **`go/transportmw`** — resilience primitives (`circuitbreaker`, `ratelimit`) +
   shared `httpmw`/`grpcmw` middleware. The clients and servers all sit on this.

**Phase 4 — clients (O5, not deferred):**

7. **`go/httpclient`** and **`go/grpcclient`** — the light client factories /
   interceptors. Either order; both consume `go/transportmw` + `go/tls`.

**Phase 5 — the server stack:**

8. **`go/transport`** — `http` server + `grpc` server + `gateway`, promoted
   together; config adapters stay in GTB; consumes every module above +
   `go/controls`.

Each phase updates the extraction-report readiness checklist and applies the
per-component §5 procedure. The order is dependency-sound at every step: a module
never imports a still-in-tree `go-tool-base/pkg/*` its depfootprint guard forbids.

## Resolved decisions (review 2026-07-13)

- **O1 — Shared middleware = its own standalone module.** `go/transportmw` holds
  the resilience primitives + the shared `httpmw`/`grpcmw` middleware; clients and
  servers depend on it (not folded into `httpclient`).
- **O2 — Observability brought forward.** Extract `go/observability` in Phase 2,
  before the transport layer, so the middleware/servers can consume it. (Not a hard
  dependency — transport uses the OTel contrib SDK directly — but sequenced early
  by choice and independently valuable.)
- **O3 — Naming.** `go/httpclient` + `go/grpcclient` + `go/transportmw` +
  `go/transport`.
- **O5 — grpc client not deferred.** `go/grpcclient` ships in Phase 4 alongside
  `go/httpclient`; there are in-scope consumers that dial grpc without the server.

## Remaining design points (resolve within the relevant phase)

1. **D1 — `transportmw` internal shape.** One module with `httpmw`/`grpcmw`
   sub-packages (proposed, mirrors the `go/transport` sub-package choice) vs. two
   modules. Confirm when reading the actual middleware files.
2. **D2 — Resilience-primitive home.** Keep `circuitbreaker`/`ratelimit` inside
   `go/transportmw` (proposed — their only consumers are transport middleware) vs.
   a separate `go/resilience`. Split only if a non-transport consumer appears.
3. **D3 — `observability` scope vs. the telemetry split.** Confirm the
   `otelcore`+`logs`/`metrics`/`tracing` group extracts cleanly without the root
   `pkg/telemetry` analytics/observability split — the report marks it "ready now",
   but verify config confinement at extraction time.
4. **D4 — Client factory vs. interceptor boundary.** Decide whether the client
   interceptors/round-trippers live in `go/transportmw` (proposed) with the client
   modules holding only the hardened factory + wiring, or whether the client
   modules own their interceptors.

## Acceptance criteria (for the plan)

- The sequence is dependency-sound: no module, at the point it is cut, imports a
  still-in-tree `go-tool-base/pkg/*` that its depfootprint guard forbids.
- Each phase leaves GTB green (`just ci`) consuming the newly-published modules.
- The client/server split is honoured: `go/httpclient`'s dependency graph contains
  no `controls`/`authn`/`gateway`.
