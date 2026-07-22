---
title: "transport-metrics: a comprehensive Prometheus instrumentation module for the toolkit"
description: "A greenfield module gitlab.com/phpboyscout/go/transport-metrics providing comprehensive, cardinality-safe Prometheus instrumentation that complements (never conflicts with) the OTel go/observability modules. Originates in go-tool-base work item #2 (a scrapeable /metrics endpoint with Go runtime + process collectors) and expands to: HTTP/gRPC request instrumentation using route TEMPLATES (not raw URLs) for cardinality safety; RED/USE domain + operational helpers for DBs, caches, circuit breakers, queues, and a generic business-logic wrapper; buildinfo-driven labelling; OpenMetrics exemplars (metric->trace linking); and both attach-to-existing-mux and standalone-metrics-server exposition. Architected cycle-free: a lean core (go/transit + prometheus/client_golang only) with opt-in subpackages that each isolate a heavy dependency (gRPC, the standalone server's go/transport, OTel trace extraction)."
date: 2026-07-22
status: DRAFT
tags:
  - specification
  - transport-metrics
  - prometheus
  - observability
  - instrumentation
  - greenfield
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# `transport-metrics`: a comprehensive Prometheus instrumentation module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-22

Status
:   IN PROGRESS (2026-07-22). Greenfield; cycle-free architecture confirmed by a spike
    (§3). All open questions resolved with the maintainer: Q1 `go/transit`-only core
    (spike); Q2 hook + warn-if-unguarded; Q3 comprehensive, **phased**; Q4
    `metrics/server` on **`go/transport`**; Q5 private registry; Q6 `r.Pattern` route
    label; Q7 OTel back-port as a separate follow-up spec. **Building Phase 1
    (v0.1.0)** per §9.

Related
:   [go-tool-base work item #2](https://gitlab.com/phpboyscout/go-tool-base/-/work_items/2) (the originating request),
    [transport-openapi extraction](2026-07-22-transport-openapi-companion-extraction.md) (companion pattern),
    [output extraction](2026-07-21-output-module-extraction-redesign.md) (the core + opt-in-subpackage pattern this reuses),
    [observability extraction](../reports/2026-07-07-package-extraction-report.md) (the OTel modules this complements)

---

## 1. Context & positioning

[Work item #2](https://gitlab.com/phpboyscout/go-tool-base/-/work_items/2) asks for a
scrapeable `/metrics` endpoint (Go runtime + process collectors, optional `pprof`)
for GTB tools running a long-lived HTTP service — a zero-infra way to watch heap,
goroutines, GC and uptime and catch leaks with `go tool pprof`. That is the seed.

The maintainer expanded the intent: this should be a **comprehensive, Prometheus-
native instrumentation module** for the whole toolkit, covering request, resource,
and domain metrics — not just a process endpoint.

**Positioning (the guiding constraint):** it is the **pull/scrape (Prometheus)**
counterpart to the existing **push (OTel) `go/observability`** modules, and must
**complement, never conflict with** them:

| | `go/observability` (exists) | `go/transport-metrics` (this) |
|---|---|---|
| Model | OTel, **push** (OTLP to a collector) | Prometheus, **pull** (scrape `/metrics`) |
| Best for | fleets with an OTLP pipeline | local/single-binary tools; classic Prometheus stacks |
| Trace link | OTel-native | **exemplars** on metric samples (§4.7) |

A tool may use **both** (e.g. OTLP in prod, `/metrics` locally). The RED/USE helper
*ergonomics* designed here (§4.5) are candidate **upgrades to the OTel modules**
later, so the two families feel the same — tracked as a follow-up (§9), not built
here.

## 2. Module & naming

| Concern | Value |
|---|---|
| Module | `gitlab.com/phpboyscout/go/transport-metrics` |
| Core package | `package metrics` |
| Opt-in subpackages | `metrics/grpc`, `metrics/server`, `metrics/otel` (§3.2) |
| Docs microsite | `transport-metrics.go.phpboyscout.uk` |

## 3. Architecture (cycle-free — spike result)

### 3.1 No dependency cycle

**Spike finding:** `go/transport`'s `NewServer(ctx, settings, handler http.Handler,
opts...)` takes a **caller-owned mux/handler** (health/liveness/readiness are already
mounted this way). So the **tool** owns the mux, mounts metrics onto it, and passes
it to the server:

```
tool ─mount→ metrics.Register(mux, metrics.WithMiddleware(transporthttp.AuthMiddleware(…)))
tool ─mount→ …other handlers…
tool ─pass mux→ transport.NewServer(ctx, settings, mux, …)
```

`go/transport` **never** imports `transport-metrics`; the tool does all wiring.
Request instrumentation is the same shape: the module *produces* middleware that the
tool applies through `go/transport`'s existing `WithMiddleware(transithttp.Chain)`.
Therefore the **core depends on `go/transit` only** (the `Middleware`/`Chain` type),
never `go/transport`. A cycle can only arise if `go/transport` were made to depend on
this module (a built-in `WithMetrics()` option) — which we deliberately do **not**
do (it would also drag Prometheus into transport core). Any such convenience lives in
a GTB/tool adapter.

### 3.2 Core + opt-in subpackages (the `output/cobra` pattern)

Each heavy dependency is isolated in a subpackage so importing the core never pulls
it. `depfootprint_test.go` guards the **root** package.

| Package | Purpose | Extra deps beyond core |
|---|---|---|
| `metrics` (core) | registry, collectors (Go/process/buildinfo), `/metrics` handler, **HTTP** request middleware, RED/USE helpers, business wrapper, exemplar plumbing (pluggable trace source) | `prometheus/client_golang`, `go/transit`, `cockroachdb/errors` |
| `metrics/grpc` | gRPC unary+stream server interceptors | `google.golang.org/grpc` |
| `metrics/server` | **standalone** metrics HTTP server (own port/listener, lifecycle, TLS) | `go/transport` (+`go/controls`, `go/tls` transitively) |
| `metrics/otel` | OTel exemplar trace-ID source (reads span context) | `go.opentelemetry.io/otel/trace` |

The exemplar trace source is **pluggable** (`WithExemplarSource(func(context.Context)
(traceID string, ok bool))`) so the core needs no OTel dep; `metrics/otel` provides
the ready-made OTel extractor for tools that want it.

## 4. Feature areas

### 4.1 Exposition — attach OR standalone

- **Attach (core):** `Register(mux *http.ServeMux, opts...) error` mounts `GET
  /metrics` (promhttp, OpenMetrics-capable) and, with `WithPprof()`, `net/http/pprof`
  — every handler wrapped by `WithMiddleware` (§4.8).
- **Standalone (`metrics/server`):** stand up a dedicated metrics server on its own
  address (typically loopback or an internal port), built on `go/transport` for
  consistent lifecycle/TLS/graceful-shutdown, serving the same registry. A tool
  chooses one or both.

### 4.2 Process / runtime / buildinfo collectors (work item #2)

Registered by default on a **private** `*prometheus.Registry` (not the global
default): `collectors.NewGoCollector()`, `collectors.NewProcessCollector(...)`, and a
**buildinfo** collector (§4.4). `WithoutRuntimeCollectors()` opts out.

### 4.3 HTTP request instrumentation — cardinality-safe

Middleware (`transithttp.Middleware`) recording the RED signals per request:
requests-total (counter), request-duration (histogram), in-flight (gauge). **Labels
use the route *template*, never the raw URL** — the module reads the matched pattern
from **`http.Request.Pattern`** (Go 1.22+ `ServeMux`) so `/users/{id}` is one series,
not one per user id. A `WithRouteLabelFunc(func(*http.Request) string)` override
supports non-stdlib routers; a bounded fallback (`WithUnmatchedRouteLabel`, default
`"<other>"`) prevents an unmatched flood. Method and status are naturally bounded;
optional normalisation caps status to class (`2xx`) where desired.

### 4.4 buildinfo-driven labelling

A `build_info` gauge (value 1) with labels sourced from `runtime/debug.ReadBuildInfo()`
+ ldflags (version, revision/commit, go version, and optional tool name). Exposed so
dashboards join any metric to the build. A shared `ConstLabels`/`WithBuildLabels`
helper stamps version/revision onto other metrics where wanted, making tagging
consistent across a tool's whole metric surface.

### 4.5 RED/USE domain & operational helpers

Reusable instrument sets so downstreams instrument common resources without
hand-rolling Prometheus each time:

- **RED** (request-driven — services, handlers, business ops): rate, errors,
  duration.
- **USE** (resources — pools, caches, queues): utilization, saturation, errors.
- Ready-made helpers for **databases** (query duration/errors, pool
  in-use/idle/wait), **caches** (hits/misses/evictions/size), **circuit breakers**
  (state gauge, trips, short-circuits), **queues** (depth, enqueue/dequeue rate,
  wait/processing latency).
- **Business-logic wrapper** — a generic RED recorder:
  `Observe(ctx, name, func() error)` / `ObserveResult[T](ctx, name, func()(T,error))`
  timing the op, counting errors, and (with exemplars on) tagging the duration
  sample with the current trace id.

These are Prometheus-native here; the same shapes are the candidate OTel upgrade
(§9).

### 4.6 gRPC instrumentation (`metrics/grpc`)

Unary + stream **server interceptors** recording RED per RPC — the full method name
is naturally low-cardinality, status from the gRPC code. Client interceptors are a
possible later addition.

### 4.7 Exemplars

OpenMetrics **exemplars** on counters/histograms link a metric sample to a trace id
(so a latency spike in a dashboard jumps to the trace). The trace-id source is
**pluggable** (`WithExemplarSource`), keeping OTel out of the core; `metrics/otel`
ships the OTel span-context extractor. Exposition negotiates OpenMetrics so scrapers
that support exemplars receive them. This is the deliberate, non-conflicting bridge
to the trace world `go/observability` already serves.

### 4.8 Auth guard, custom collectors (work item #2 reqs 3 & 5)

- **Guard (req 3):** `WithMiddleware(mw ...transithttp.Middleware)` wraps every
  mounted handler; docs lead with the guarded posture (auth middleware **or** loopback
  bind). Per Q2, `Register` **logs a warning when no middleware is supplied** rather
  than hard-failing (loopback-only tools are legitimate).
- **Custom collectors (req 5):** `WithRegistry(*prometheus.Registry)` (caller owns) or
  `WithCollectors(...prometheus.Collector)`.

## 5. Cardinality safety (a first-class concern)

High cardinality is the way a metrics system takes down its own scrape target. The
module bakes in defaults that resist it: route **templates** not raw paths (§4.3),
bounded label helpers, status-class normalisation, a capped `<other>` bucket for
unmatched routes, and docs that call out every label whose value space the *caller*
controls. Any helper that accepts a caller-supplied label name documents the
cardinality contract.

## 6. Dependencies & footprint guard

Core `go.mod`: `github.com/prometheus/client_golang`,
`gitlab.com/phpboyscout/go/transit v0.1.0`, `github.com/cockroachdb/errors v1.14.0`.
Subpackages add grpc / go/transport / otel respectively. `depfootprint_test.go`
(root pkg) forbids `go-tool-base`, Viper/pflag/Cobra, cloud SDKs, **and
`google.golang.org/grpc`, `go/transport`, `go.opentelemetry.io`** (those belong only
to the subpackages). Prometheus is allowed (the module's reason to exist).

## 7. Non-conflict guarantees with `go/observability`

- Distinct module, distinct package names; a tool may import both with no symbol or
  registry clash (this owns its own `*prometheus.Registry`; observability owns the
  OTel meter).
- No shared global state; exemplars *read* trace context via a pluggable source
  rather than depending on the OTel SDK in the core.
- Docs state the boundary explicitly (§1 table) so users pick pull vs push
  deliberately, or run both.

## 8. Open questions

- **Q1 — Core dependency. → `go/transit` only (RESOLVED by spike, §3).** Confirmed
  cycle-free; `go/transport` lives only in `metrics/server`.
- **Q2 — Auth guard. → Hook + docs + warn-if-unguarded (RESOLVED).**
- **Q3 — Scope. → Comprehensive, incl. request instrumentation (RESOLVED); phased
  (§9).**
- **Q4 — Standalone server engine. → `metrics/server` on `go/transport`** (RESOLVED):
  consistent lifecycle/TLS with the toolkit; it is an opt-in subpackage, so only
  standalone-server users pull that graph.
- **Q5 — Registry default. → private `*prometheus.Registry`** (RESOLVED), `WithRegistry`
  to override.
- **Q6 — Route-label source. → `http.Request.Pattern`** (Go 1.22+ ServeMux) with a
  `WithRouteLabelFunc` override for non-stdlib routers (RESOLVED).
- **Q7 — OTel upgrade. → separate follow-up spec** (RESOLVED): back-port the RED/USE
  ergonomics to `go/observability` later; not scoped here.

## 9. Phased delivery

Comprehensive, but shipped in reviewable minors (each independently useful, each
holding ≥90% coverage):

- **v0.1.0 — Endpoint & runtime** (satisfies work item #2 + buildinfo + standalone):
  core registry (private, Go/process/**buildinfo** collectors), `Register` attach +
  `WithPprof`, `WithMiddleware` guard, custom-collector hooks, and the
  `metrics/server` standalone server. This is the shippable answer to #2.
- **v0.2.0 — Request instrumentation:** HTTP middleware (route-template labels,
  cardinality controls) + `metrics/grpc` interceptors + exemplar plumbing
  (`WithExemplarSource`) and `metrics/otel`.
- **v0.3.0 — RED/USE helpers:** database / cache / circuit-breaker / queue instrument
  sets + the generic business-logic `Observe` wrapper.
- **Follow-up (separate spec):** back-port RED/USE ergonomics to `go/observability`
  (Q7); optional gRPC/HTTP **client** instrumentation.

Each phase is a normal Release-MR minor on the module; downstreams (krites first)
adopt incrementally.

## 10. Acceptance criteria (per phase)

1. Module builds; resolves from the proxy; `depfootprint_test.go` green (core free of
   grpc/transport/otel/go-tool-base/viper/cobra/cloud-SDK; subpackages carry their
   own deps); ≥90% coverage each phase.
2. **v0.1.0:** `/metrics` serves Go+process+buildinfo; `WithPprof` guarded; standalone
   `metrics/server` runs and stops cleanly; auth-guard hook + warning; custom
   collectors. Work item #2's five requirements demonstrably met.
3. **v0.2.0:** HTTP/gRPC RED metrics with **route-template** labels proven
   low-cardinality by test; exemplars emitted under OpenMetrics with a pluggable
   source.
4. **v0.3.0:** DB/cache/breaker/queue helpers + business wrapper, each with a worked
   example.
5. Dedicated microsite documents the pull-vs-push boundary and the cardinality
   contract; README states the guarded-endpoint requirement.
