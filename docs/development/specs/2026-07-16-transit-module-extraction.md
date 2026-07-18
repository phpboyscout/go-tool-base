---
title: "Extract the shared transport middleware & resilience into go/transit"
description: "Extract GTB's transport-neutral middleware layer — the circuit-breaker and rate-limiter primitives plus the shared HTTP and gRPC middleware (request/response chaining, logging with redaction, OTel instrumentation, client round-trippers, and server interceptors) — out of pkg/http, pkg/grpc, and internal/ into the standalone gitlab.com/phpboyscout/go/transit module. Phase 3 of the transport-stack extraction plan; the layer every transport client and server sits on."
date: 2026-07-16
status: IMPLEMENTED
tags:
  - specification
  - transit
  - transport
  - middleware
  - resilience
  - http
  - grpc
  - module-extraction
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract the shared transport middleware & resilience into `go/transit`

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-16

Status
:   IMPLEMENTED (2026-07-18) — shipped to main; originally APPROVED (O1–O4 resolved in review 2026-07-16)

## Summary

`go/transit` is **Phase 3** of the
[transport-stack extraction plan](2026-07-13-transport-stack-extraction-plan.md) — the
shared, transport-neutral middleware layer that every transport client and server sits
on. It bundles two things GTB currently spreads across `pkg/http`, `pkg/grpc`, and
`internal/`:

1. **Resilience primitives** — the transport-neutral circuit breaker
   (`internal/circuitbreaker`) and rate limiter (`internal/ratelimit`).
2. **Shared HTTP & gRPC middleware** — request/response chaining, logging (with
   `go/redact`), OTel instrumentation, the client-side round-trippers / client
   interceptors (retry, circuit-break), and the server-side handlers / server
   interceptors (rate-limit, logging).

Extracting it now (after the Phase 2 foundations `tls`/`authn`/`observability`) is what
lets Phase 4's light clients (`go/httpclient`, `go/grpcclient`) and Phase 5's server
stack (`go/transport`) all sit on one shared middleware module rather than each
re-implementing it.

**The layer is already remarkably clean.** Measured on the current tree, the candidate
files import **no** `pkg/config`, `pkg/logger`, `pkg/authn`, or `pkg/tls`. Their only
non-stdlib dependencies are `go/redact` (already a module), the gRPC SDK, the OTel
contrib instrumentation, `golang.org/x/time/rate`, and `cockroachdb/errors` — plus the
two `internal/` primitives, which **promote into this module**. So `transit` is close
to a lift-and-shift, with the *configuration* of the middleware (thresholds, limits)
staying behind in GTB.

## Target module

- **Module path:** `gitlab.com/phpboyscout/go/transit` (name chosen 2026-07-16 to
  replace the working title `transportmw`).
- **Package layout (D1):** one module with sub-packages
  - `transit/http` — `http.Handler` middleware + `http.RoundTripper` client middleware
    (chain, logging, OTel, retry, circuit-break, rate-limit).
  - `transit/grpc` — unary/stream **client** interceptors + **server** interceptors +
    the OTel stats handler / dial options (chain, logging, OTel, circuit-break,
    rate-limit).
  - `transit/resilience` — the transport-neutral circuit-breaker and rate-limiter
    primitives (**D2**: kept here, not a separate `go/resilience`).
- **Docs:** `transit.go.phpboyscout.uk` (microsite scope — **O3**).
- **Local dir:** `~/workspace/phpboyscout/go/transit`.
- **Reference scaffold:** `~/workspace/phpboyscout/go/observability` (the OTel-carrying
  sibling — its depfootprint guard shape is the closest precedent).

## What moves vs. what stays

### Moves into `go/transit`

- **Resilience primitives:** `internal/circuitbreaker/breaker.go`,
  `internal/ratelimit/store.go` (+ tests) → `transit/resilience`. Zero GTB imports
  today.
- **HTTP middleware** (from `pkg/http`): `chain.go`, `logging.go` (uses `go/redact`),
  `otel.go`, `client_middleware.go`, `retry.go`, `circuitbreaker.go`, `ratelimit.go`
  (+ tests) → `transit/http`.
- **gRPC middleware** (from `pkg/grpc`): `chain.go`, `logging.go`, `otel.go`,
  `circuitbreaker.go`, `ratelimit.go` (+ tests) → `transit/grpc`.

### Stays in GTB

- **The resilience/middleware *configuration*.** `pkg/http/config_adapter.go` and
  `pkg/grpc/config_adapter.go` read GTB config for rate-limit and circuit-breaker
  settings; they stay (they read `config.Containable`). They will configure `transit`'s
  primitives from typed settings.
- **The transport servers and clients themselves** — `pkg/http/server.go` /
  `client.go`, all of `pkg/grpc` server, `pkg/gateway` — which are Phase 4/5 targets.
  Until then they remain in GTB and **consume `transit`** for the middleware they wire.
- **Server-auth & security-headers** (`pkg/http/auth.go`, `security_headers.go`,
  `pkg/grpc` auth) — these depend on `go/authn` and are server-stack concerns (Phase 5),
  not shared middleware.

## Cut-over shape (O1)

Unlike the Phase 2 extractions, this carves a **layer out of packages that stay**
(`pkg/http`, `pkg/grpc` remain in GTB until Phase 5). Two options for how GTB then
consumes it:

- **O1a — facade (recommended):** `pkg/http` / `pkg/grpc` keep re-exporting the moved
  middleware symbols (`http.NewChain`, `http.LoggingMiddleware`, `http.OTelMiddleware`,
  the grpc interceptors, …) as thin aliases over `transit`. **Zero churn** for GTB's
  own `server.go`/`client.go`/`gateway` wiring **and** for downstream tools and the
  docs examples that call `http.OTelMiddleware` etc. Matches the `tls` facade; the
  aliases evaporate in Phase 5 when the servers/clients themselves move to `transit`.
- **O1b — full repoint:** repoint every middleware reference in `pkg/http`/`pkg/grpc`/
  `pkg/gateway` (and downstream) to `transit` directly, deleting the symbols from the
  transport packages. Cleaner break, but churns the transport packages we rewrite in
  Phase 5 anyway, and breaks downstream `http.OTelMiddleware`-style call sites now
  rather than at Phase 5.

Because the transport packages persist through Phase 5 and have external callers, the
facade (O1a) keeps this phase minimal and low-risk; the clean break happens naturally
when the servers/clients extract. (This is the opposite recommendation from
`observability`, where the only consumer was internal — here there are downstream
callers.)

## Repository framework (playbook §3)

Bootstrap from the `go/observability` scaffold, with **gRPC + OTel allowed**:

- `depfootprint_test.go`: **allow** `google.golang.org/grpc`, `go.opentelemetry.io/*`,
  `golang.org/x/time`; **forbid** `go-tool-base`, `spf13/viper`, `spf13/pflag`,
  `spf13/cobra`, `charmbracelet`, and the cloud SDKs. `go/redact` is an allowed
  sibling-module dependency.
- OTel + gRPC versions pinned in lockstep with GTB (and with `go/observability`); watch
  the `google.golang.org/genproto` split ambiguity (pin `googleapis/api`+`rpc` + grpc,
  as `go/observability` does).
- CI `@v0.22.0`; `.golangci.yaml` v2, local prefix `gitlab.com/phpboyscout/go/transit`;
  ≥90% coverage (the middleware suites are already thorough); `cockroachdb/errors`;
  `*slog.Logger` seam for logging middleware.

## Documentation (playbook §4)

- Author `transit.go.phpboyscout.uk` (scope per **O3**): overview, a how-to (compose a
  client round-tripper chain + a server handler chain), and an explanation of the
  resilience model (circuit-breaker states, rate-limit token bucket) and the
  client/server middleware split.
- Pages custom domain + Cloudflare DNS (DNS-only) + `pages_access_level=public` +
  `pages_primary_domain=https://…` per the `.go` learnings.
- GTB side: the transport-middleware concept/component pages link out to the microsite
  and keep the GTB config-cascade + chain-ordering story; migration note; report tick.

## Sequencing within the transport stack

`transit` unblocks the rest of the transport plan:

1. **This spec** — `transit` (resilience + shared middleware).
2. **Phase 4** — `go/httpclient` + `go/grpcclient`: the light hardened client factories,
   wiring `transit/http` / `transit/grpc` client middleware + `go/tls`.
3. **Phase 5** — `go/transport`: the `http` server + `grpc` server + `gateway`, wiring
   `transit` server middleware + `go/authn` + `go/observability` + `go/controls`.

## Resolved decisions (review 2026-07-16)

1. **O1 — Facade (O1a).** `pkg/http` / `pkg/grpc` re-export `transit`'s middleware as
   thin aliases, so GTB's server/client/gateway wiring **and** downstream callers (which
   use `http.OTelMiddleware`, `http.NewChain`, etc.) are unchanged. The aliases evaporate
   in Phase 5 when the servers/clients themselves move to `transit`. (Opposite of
   `observability`'s full-repoint, because here the transport packages persist and have
   external callers.)
2. **O2 — One `transit/resilience` sub-package** holding both the circuit breaker and
   the rate limiter (they are small and always travel together).
3. **O3 — Light microsite** at `transit.go.phpboyscout.uk`: overview + a compose-a-chain
   how-to + a resilience-model / client-server-split explanation. Reference → pkg.go.dev.
4. **O4 — `transit` owns both halves.** It holds the client round-trippers / client
   interceptors **and** the server handlers / server interceptors; the Phase 4 client
   modules (`go/httpclient`, `go/grpcclient`) hold only the hardened factory + wiring.
   The measured client/server file split makes a single shared home clean and keeps the
   resilience primitives in one place.

## Acceptance criteria

- `go/transit` builds with `transit/http`, `transit/grpc`, `transit/resilience`
  sub-packages; `depfootprint_test.go` passes (gRPC/OTel/redact allowed; no
  go-tool-base/framework/cloud-SDK); ≥90% coverage; race/lint/vet/govulncheck green;
  `v0.1.0` published and proxy-resolvable.
- GTB consumes `go/transit v0.1.0`; `internal/circuitbreaker` + `internal/ratelimit`
  and the moved middleware files are gone from GTB; per O1, either the `pkg/http`/
  `pkg/grpc` facades re-export it (no churn) or all references repoint; `just ci` green
  (modulo the known `internal/agent` test).
- Docs live per O3; migration note added; extraction-report checklist ticks `transit`;
  the transport-stack plan's Phase 3 is marked done.
