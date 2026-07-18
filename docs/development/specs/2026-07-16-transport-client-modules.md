---
title: "Extract the transport client factories into go/httpclient and go/grpcclient"
description: "Phase 4 of the transport-stack extraction plan: lift GTB's hardened HTTP client factory (pkg/http/client.go) into the standalone gitlab.com/phpboyscout/go/httpclient module, and the gRPC client dial factory into gitlab.com/phpboyscout/go/grpcclient — two light modules that wire the go/transit middleware and go/tls onto stdlib net/http and the gRPC SDK, so a client-only consumer never inherits the server stack (controls/authn/gateway)."
date: 2026-07-16
status: IMPLEMENTED
tags:
  - specification
  - httpclient
  - grpcclient
  - transport
  - http
  - grpc
  - module-extraction
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract the transport client factories into `go/httpclient` and `go/grpcclient`

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-16

Status
:   IMPLEMENTED (2026-07-18) — shipped to main; originally APPROVED (2026-07-16) — O1–O5 resolved; O3 = `grpcclient.Target` type.

## Summary

**Phase 4** of the
[transport-stack extraction plan](2026-07-13-transport-stack-extraction-plan.md): extract
the two *light* transport **client** factories that sit on the now-published
[`go/transit`](2026-07-16-transit-module-extraction.md) middleware and
[`go/tls`](2026-07-16-tls-module-extraction.md):

1. **`go/httpclient`** — the hardened `*http.Client` factory currently in
   `pkg/http/client.go` (`NewClient`, the `ClientOption` set, `NewTransport`).
2. **`go/grpcclient`** — the gRPC client **dial factory** (`DialLocal`/`dialLocal` in
   `pkg/grpc/server.go`), composing `go/transit`'s client interceptors, `go/tls`
   credentials, and the gRPC SDK.

The value of the client/server split (plan O5) is that a client-only consumer — and
GTB has many: `pkg/chat`, `pkg/telemetry` (+`datadog`/`posthog`), `pkg/vcs/*`,
`pkg/setup` — pulls in only `net/http`/gRPC + `transit` + `tls`, never `controls`,
`authn`, or the gateway. Those stay behind for Phase 5's `go/transport` server stack.

## Problem statement

`pkg/http/client.go` and the gRPC dial helpers are framework-free in spirit (they wire
stdlib/SDK transports with hardened defaults) but currently live inside the GTB
`pkg/http` / `pkg/grpc` packages, which also carry the server bootstraps, config
adapters, and (until Phase 5) `controls` integration. Any external project — or an
extracted sibling like `go/chat` — that just wants "a secure HTTP client with the GTB
retry/breaker/logging middleware" cannot get it without the whole framework. Phase 3
already extracted the middleware; the client factories are the thin, high-reuse layer
that composes it.

## Goals & non-goals

**Goals**

- Ship `go/httpclient` v0.1.0: `NewClient(...ClientOption)` and `NewTransport` with the
  existing hardened TLS/redirect/timeout defaults, wiring `transit`'s
  `NewRetryTransport` and `ClientChain`.
- Ship `go/grpcclient` v0.1.0: a gRPC client dial factory wiring `transit`'s client
  interceptors + `go/tls` credentials.
- Keep GTB call sites unchanged via a **facade** (see O2): `pkg/http`/`pkg/grpc`
  re-export the client factories.
- Both modules pass a `depfootprint` guard forbidding `go-tool-base`, `controls`,
  `authn`, the gateway, Viper/Cobra/Charm, and cloud SDKs.

**Non-goals**

- No behaviour change to the hardened defaults (TLS 1.2 floor, redirect downgrade
  refusal, connection limits).
- The **server** stack (`http.Server`, `grpc.Server`, gateway, `ServerSettings`,
  security headers, server rate-limiting, `controls` health) stays in GTB → Phase 5.
- The GTB config-key adapters (`*FromContainable`) stay in GTB.
- No new middleware — that all lives in `go/transit`.

## What moves vs. what stays

### Moves into `go/httpclient`

From `pkg/http/client.go`: `ClientOption`, `clientConfig`, `defaultClientConfig`,
`WithTimeout`, `WithMaxRedirects`, `WithTLSConfig`, `WithTransport`, `WithCertPool`,
`WithRetry`, `WithClientMiddleware`, `NewTransport`, `NewClient`, `redirectPolicy`, and
the client timeout/limit constants. `WithRetry`/`WithClientMiddleware` import
`go/transit` directly (`RetryConfig`, `ClientChain`, `NewRetryTransport`); the TLS
options import `go/tls` (`DefaultConfig`).

### Moves into `go/grpcclient`

From `pkg/grpc/server.go`: the `dialLocal` credential/endpoint logic, generalised behind
the new `grpcclient.Target` type + `Dial(Target, ...opts)` entry point (O3). The gRPC
**client interceptors** (`CircuitBreakerInterceptor`, `OTelClientHandler`, …) already
live in `go/transit/grpc` and are consumed, not moved.

### Stays in GTB

- `pkg/http`: the server (`server.go`), config adapters (`config_adapter.go`), request
  auth (`auth.go`), security headers, and the middleware re-exports from Phase 3.
- `pkg/grpc`: the server (`server.go` minus the dial factory), config adapters
  (`DialLocalFromContainable`), auth. `DialLocal(ServerSettings, ...)` stays as a **thin
  adapter** mapping GTB's `ServerSettings` onto the new `grpcclient` dial factory (O3).

## Cut-over shape

**O2 — facade, both modules.** `pkg/http/client.go` becomes a re-export of
`go/httpclient` (type alias `ClientOption`, function-value re-exports of
`NewClient`/`NewTransport`/`With*`), mirroring the Phase 3 `reexport.go` pattern, so the
~10 internal `NewClient` consumers and the `pkg/chat` adapter are untouched. The gRPC
side keeps `DialLocal` as a GTB adapter over `grpcclient`.

> **Facade coverage note (learned in Phase 3):** re-export functions as
> `var NewClient = httpclient.NewClient` **value aliases**, not wrapper funcs — value
> aliases are not countable statements, so they do not drag the GTB package under the
> 90% `coverage-policy` gate (a wrapper-func facade dropped `pkg/tls` to 78.3% and
> tripped the policy). Only re-type a signature when an external option type forces it.

## Public API (unchanged from today)

```go
// go/httpclient
func NewClient(opts ...ClientOption) *http.Client
func NewTransport(tlsCfg *tls.Config) *http.Transport
type ClientOption func(*clientConfig)
func WithTimeout(d time.Duration) ClientOption
func WithMaxRedirects(n int) ClientOption
func WithTLSConfig(cfg *tls.Config) ClientOption
func WithTransport(rt http.RoundTripper) ClientOption
func WithCertPool(pool *x509.CertPool) ClientOption
func WithRetry(cfg transit.RetryConfig) ClientOption          // transit type
func WithClientMiddleware(chain transit.ClientChain) ClientOption

// go/grpcclient  (O3 resolved: first-class client Target, decoupled from ServerSettings)
type Target struct {
	Host string      // dial host; empty ⇒ loopback for a local server
	Port int         // dial port
	TLS  tls.Pair    // go/tls Pair; disabled ⇒ insecure loopback creds
}
func Dial(t Target, opts ...grpc.DialOption) (*grpc.ClientConn, error)
```

`Dial` builds `credentials.TransportCredentials` from `t.TLS` (via `go/tls`), assembles
the endpoint from `Host`/`Port`, and applies the caller's dial options (into which
`transit`'s `OTelClientHandler` / circuit-breaker client interceptors are passed). GTB's
`DialLocal(settings ServerSettings, pair tls.Pair, …)` becomes a thin adapter that maps
`ServerSettings.Port` (+ loopback host) and the pair onto a `grpcclient.Target`.

## Data models

`clientConfig` (unexported) is unchanged: `timeout`, `maxRedirects`, `tlsConfig`,
`transport`, `retry *transit.RetryConfig`, `clientChain *transit.ClientChain`. No config
structs are introduced (client factories take functional options, not config sections).

## Error cases

- `redirectPolicy` continues to reject HTTPS→HTTP downgrades and cap redirect count.
- `WithCertPool`/`ClientConfig` surface CA-file read/parse errors from `go/tls`.
- gRPC `Dial` surfaces credential and target-parse errors from the SDK; no behaviour
  change from today's `dialLocal`.

## Testing strategy

- **TDD, ≥90% coverage per module.** Port the existing `client_test.go` client-factory
  tests into `go/httpclient` (they already drive `NewClient`/`NewTransport` via
  `httptest`), plus the redirect-policy and cert-pool cases. Port the `dialLocal`
  coverage into `go/grpcclient`.
- **GTB keeps facade wiring tests** — the `TestWithRetry_Integration` /
  `TestWithClientMiddleware_Integration` style checks that GTB's re-exported `NewClient`
  still installs the transit middleware end-to-end.
- **BDD (Godog): not warranted.** These are library factories with no CLI surface,
  multi-step workflow, or service lifecycle — standard Go tests suffice, per the
  [BDD suitability assessment](2026-03-28-godog-bdd-strategy.md). (Phase 5's server
  stack, by contrast, will warrant E2E scenarios.)
- Each module carries a `depfootprint_test.go` guard (see Goals).

## Documentation

Following Diátaxis with the project's marketing carve-out:

- **New microsites** `httpclient.go.phpboyscout.uk` and `grpcclient.go.phpboyscout.uk`
  (zensical, DNS-only CNAME + `pages_access_level=public`, per the Phase 2/3 playbook),
  each with: index, a getting-started **tutorial**, a how-to (compose client
  middleware / dial a service), and an explanation (hardened defaults & the client
  factory model). Reference → pkg.go.dev.
- **Tutorials** are authored as **phpboyscout.uk blog posts** and linked from the
  microsite Tutorial section (project deviation from pure Diátaxis).
- **GTB docs**: migration notes `docs/reference/migration/v0.x-httpclient-extracted.md`
  and `…-grpcclient-extracted.md` (non-breaking — facade); update
  `docs/explanation/components/http.md` / `grpc.md` and tick the extraction report.
- **Landing card** on `phpboyscout/go/landing` for each module.

## Implementation phases

1. **`go/httpclient` first** (higher reuse, no server coupling): scaffold module,
   move `client.go`, wire `transit` + `tls`, port tests to ≥90%, depfootprint guard,
   publish v0.1.0, facade cut-over in `pkg/http`, docs + landing.
2. **`go/grpcclient` second** (once O3 resolved): scaffold, introduce the dial factory,
   make GTB `DialLocal` an adapter, port tests, publish v0.1.0, docs + landing.
3. Update the extraction-report readiness checklist after each.

## Resolved decisions (review 2026-07-16)

- **O1 — one combined spec.** Both Phase-4 modules are specified here; implemented and
  published sequentially (`httpclient` then `grpcclient`).
- **O2 — facade for `pkg/http`** (and a GTB adapter for the gRPC dial). Given ~10
  internal `NewClient` consumers plus the `pkg/chat` adapter, re-export
  `go/httpclient` from `pkg/http` (value-alias functions) rather than repointing every
  consumer — zero call-site churn, and it keeps the coverage-policy green.
- **O3 — a first-class `grpcclient.Target`** (host/port/`tls.Pair`), decoupled from the
  server-side `ServerSettings`. `grpcclient` ships in Phase 4; GTB's `DialLocal` becomes
  a thin `ServerSettings → Target` adapter. (Chosen over a plain SDK-shaped `Dial` and
  over deferring to Phase 5.)
- **O4 — plan names** `httpclient` / `grpcclient` (bare, matching `go/redact` etc.),
  docs at `httpclient.go.phpboyscout.uk` / `grpcclient.go.phpboyscout.uk`.
- **O5 — `go/tls` client surface is sufficient.** `DefaultConfig`/`CertPool`/
  `ClientConfig` cover `WithTLSConfig`/`WithCertPool` and the transport defaults; no
  GTB-only TLS glue leaks into the client modules. To be re-confirmed at implementation
  by the depfootprint guard.

## Acceptance criteria

- `go/httpclient` and (pending O3) `go/grpcclient` published at v0.1.0, framework-free
  (depfootprint green), ≥90% coverage, microsites live.
- GTB builds and all tests pass with `pkg/http`/`pkg/grpc` re-exporting the modules;
  the ~10 `NewClient` consumers and `pkg/chat` adapter are unchanged.
- `coverage-policy` stays green (value-alias facade).
- Extraction report + migration notes + landing cards updated.
