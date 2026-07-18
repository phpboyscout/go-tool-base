---
title: "Extract the transport server stack into go/transport"
description: "Phase 5 (final) of the transport-stack extraction plan: promote GTB's HTTP server, gRPC server and grpc-gateway — health handlers, secure server constructors, request/RPC authentication, security headers — into the standalone gitlab.com/phpboyscout/go/transport module, consuming go/controls + go/authn + go/transit + go/tls. This is a CLEAN BREAK: GTB keeps only the config.Containable adapters (which cannot leave the framework); the pure server API is no longer re-exported, so downstreams migrate to go/transport directly."
date: 2026-07-17
status: IMPLEMENTED
tags:
  - specification
  - transport
  - http
  - grpc
  - gateway
  - server
  - module-extraction
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract the transport server stack into `go/transport`

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-17

Status
:   IMPLEMENTED (2026-07-18) — shipped to main (cut-over MR !247); originally APPROVED (2026-07-17) — O1–O7 resolved. Phase 5 (final) of the transport-stack
    extraction plan. **This is a deliberate clean break, not a facade cut-over.**

## Summary

The last resident of GTB's transport layer is the **server stack**: the HTTP server,
the gRPC server, and the grpc-gateway. Phases 1–4 extracted everything a *client* needs
(`go/tls`, `go/authn`, `go/observability`, `go/transit`, `go/httpclient`,
`go/grpcclient`); the server bootstraps, health endpoints, request/RPC authentication and
security headers stayed behind because they legitimately depend on the heavier
`go/controls` lifecycle and on each other.

This spec promotes all three, **together**, into a single standalone module
**`gitlab.com/phpboyscout/go/transport`** with `http`, `grpc` and `gateway`
sub-packages (per the plan's O1/O3). It consumes `go/controls`, `go/authn`,
`go/transit`, `go/tls`, `go/grpcclient` and the gRPC/grpc-gateway SDKs.

Unlike Phases 2–4, **this is a clean break, not a facade cut-over** (O1). The pure
server API moves to `go/transport` and GTB does **not** re-export it via value aliases.
GTB retains only the `config.Containable` adapters — the framework-config glue that
cannot live in a config-free module — and those adapters call `go/transport` internally.
GTB's own internal callers of the pure API are repointed to `go/transport`; downstream
tools that imported the pure server symbols from `pkg/http`/`pkg/grpc`/`pkg/gateway`
migrate their imports to `go/transport`, guided by a migration note. This is a breaking
change, permitted under the pre-1.0 policy and shipped as a minor bump.

Completing this makes the transport stack fully modular and **ends the extraction
programme**: a tool can build a hardened HTTP/gRPC service from `go/transport` without
the GTB framework, and GTB itself becomes a consumer of its own extracted transport
layer through the thin config adapters it keeps.

## Problem statement / goals

- Ship `go/transport` v0.1.0: `http`, `grpc`, `gateway` sub-packages holding the pure
  server API — server constructors from typed `ServerSettings`, health/liveness/readiness
  handlers, `AuthMiddleware`/`AuthInterceptor`, security headers, `Start`/`Stop`/`Status`
  lifecycle glue, the gateway `New`/`Register`, and (O5) `DialLocal` +
  `TLSClientCredentials`.
- **Clean break in GTB** (O1): delete the re-exported pure API from
  `pkg/http`/`pkg/grpc`/`pkg/gateway`; keep **only** the `config.Containable` adapters,
  repointed at `go/transport`. Repoint GTB's internal callers to the module.
- Leave GTB green (`just ci`) — including the E2E controls/HTTP/gRPC scenarios — and
  keep the retained-adapter packages ≥90% under coverage-policy.
- `go/transport`'s dependency graph may contain `go/controls`, `go/authn`, `go/transit`,
  `go/tls`, `go/grpcclient` and the gRPC/gateway SDKs, but **not** `go-tool-base`,
  Viper/Cobra/Charm, or cloud SDKs. A `depfootprint` guard enforces this.
- Document the break: a migration note mapping every moved symbol
  `pkg/{http,grpc,gateway}.X` → `transport/{http,grpc,gateway}.X`.

**Non-goals**

- No behaviour change to the hardened server defaults (TLS, timeouts, health wiring,
  auth precedence, security headers).
- The GTB config-key schema and the `config.Containable` adapters stay in GTB — the
  module never imports the framework config container.
- No new middleware — that lives in `go/transit` (Phase 3) and is consumed.
- No change to the already-extracted client modules.

## What moves vs. what stays

### Moves into `go/transport` (and is removed from GTB)

**`transport/http`** (from `pkg/http`, server-side files):
- Health: `HealthHandler`, `LivenessHandler`, `ReadinessHandler` (consume
  `controls.HealthReporter`).
- Server: `ServerSettings{Port, MaxHeaderBytes}` (kept distinct — O2), `ServerOption` +
  `With*` (`WithConfigPrefix`, `WithPort`, `WithMaxHeaderBytes`, `WithReadTimeout`,
  `WithWriteTimeout`, `WithIdleTimeout`, `WithServerTLSConfig`), `NewServer`,
  `StartWithTLSPair`, `Stop`, `Status`, `RegisterOption`, `WithMiddleware`,
  `WithMaxRequestBodyBytes`, `MaxBytesMiddleware`, `Register`.
- Auth (O3): `AuthMiddleware` + its `AuthOption`s, `IdentityFromContext` (consume
  `go/authn`).
- Security headers: `SecurityHeadersMiddleware` + its options.

**`transport/grpc`** (from `pkg/grpc`, server-side files):
- Server: `ServerSettings{Port, Reflection}` (kept distinct — O2), `ServerOption` +
  `With*`, `NewServer`, `RegisterHealthService`, `Start`, `TLSServerCredentials`, `Stop`,
  `Status`, `RegisterOption`, `WithInterceptors`, `Register`.
- Auth (O3): `AuthInterceptor` + its options, `IdentityFromContext`.
- Client-dial helpers (O5): `DialLocal` (already a thin adapter over `go/grpcclient`) and
  `TLSClientCredentials` move here, keeping the grpc package self-contained.

**`transport/gateway`** (from `pkg/gateway`):
- `Settings`, `RegisterFunc`, `Option` + `With*` (`WithMuxOptions`, `WithDialOptions`,
  `WithMiddleware`), `New`, `Register`. Consumes `transport/http` (for `Chain`) + the
  grpc-gateway runtime + gRPC SDK, **not** `go/grpcclient` (O4).

### Stays in GTB (config adapters only — no facades)

- **The `config.Containable` adapters** (`config_adapter.go` in each package), repointed
  at `go/transport`: `ServerSettingsFromConfig`, `ObserveServerSettingsFromConfig`,
  `NewServerFromContainable`, `StartFromContainable`, `RegisterFromContainable`,
  `RateLimitConfigFromConfig`, `CircuitBreakerConfigFromConfig`,
  `DialLocalFromContainable`, and the gateway's `SettingsFromConfig` /
  `NewFromContainable` / `NewFromConfig` / `RegisterFromContainable` /
  `RegisterFromConfig`. These need `config.Containable` (a GTB type) and therefore
  cannot move; they remain the **recommended GTB-facing API** and call `go/transport`
  internally. They reference the module's `ServerSettings`/config types directly (no
  re-export alias).
- The **GTB config-key schema** and default assets.
- **No value-alias re-exports of the pure API.** `pkg/http`/`pkg/grpc`/`pkg/gateway`
  shrink to the adapter set above (plus whatever GTB-only glue genuinely remains). A GTB
  or downstream caller that wants the pure constructors imports `go/transport` directly.

## Public API after the break

The `config.Containable` adapters keep their names/signatures (bar referencing the
module's `ServerSettings` type). The pure constructors are **no longer available from
`pkg/*`** — they live at `go/transport`:

```go
// go/transport/http (pure)
func NewServer(ctx context.Context, s ServerSettings, h http.Handler, opts ...ServerOption) (*http.Server, error)

// pkg/http (GTB — adapter retained, pure NewServer NOT re-exported)
func NewServerFromContainable(ctx context.Context, cfg config.Containable, h http.Handler, opts ...transporthttp.ServerOption) (*http.Server, error) {
    s := ServerSettingsFromConfig(cfg, prefix)     // config-key derivation stays in GTB
    return transporthttp.NewServer(ctx, s, h, opts...)
}

// Migration for a downstream that called the pure API directly:
//   - gtbhttp.NewServer(ctx, settings, h)
//   + transporthttp.NewServer(ctx, settings, h)
```

## Module layout & dependencies

- One module, `gitlab.com/phpboyscout/go/transport`, sub-packages `http`, `grpc`,
  `gateway` (mirrors `go/transit`). `gateway` consumes `http` and the gRPC SDK.
- Dependency graph (allowed): `go/controls`, `go/authn`, `go/transit`, `go/tls`,
  `go/grpcclient`, gRPC SDK, grpc-gateway runtime, `cockroachdb/errors`. Forbidden
  (depfootprint): `go-tool-base`, Viper/Cobra/Charm, cloud SDKs.
- This is the one extracted module that legitimately carries `go/controls` — it is the
  server-lifecycle stack, sequenced last for exactly this reason.

## Cut-over shape (single combined MR — O6)

Ship the whole GTB-side repoint as **one MR** (O6):

1. Move the pure symbols into `go/transport`; publish v0.1.0.
2. In GTB, **delete** the moved server code from `pkg/http`/`pkg/grpc`/`pkg/gateway`,
   leaving the `config.Containable` adapters, and repoint those adapters + every internal
   GTB caller (`pkg/cmd/*`, `internal/generator` templates, generated scaffolds, E2E test
   binary) at `go/transport`.
3. Update `go.mod`, run `just ci` green (unit + race + lint + coverage-policy + E2E).
4. Migration notes mapping every moved symbol; mark the extraction programme complete.

Because there are no re-export wrappers, the coverage-policy risk of Phase 3 does not
arise here; the retained adapter packages are covered by the existing GTB adapter tests.

## Resolved (review 2026-07-17)

- **O1 — Clean break, not a facade.** Eliminate the value-alias re-exports; GTB keeps
  only the `config.Containable` adapters (which cannot leave the framework). Downstreams
  migrate to `go/transport`; document it thoroughly. *(Matt.)*
- **O2 — `ServerSettings` kept distinct** per sub-package (`http{Port, MaxHeaderBytes}`,
  `grpc{Port, Reflection}`) — no unified type. *(Matt.)*
- **O3 — Auth moves with its transport** into `transport/http` and `transport/grpc`.
  *(Recommended; implied by the clean break.)*
- **O4 — `gateway` needs only the grpc-gateway runtime + gRPC SDK**, not `go/grpcclient`;
  the dial happens in GTB's retained `NewFromContainable` adapter. *(Recommended.)*
- **O5 — `DialLocal` + `TLSClientCredentials` move to `transport/grpc`**, keeping the
  grpc server package self-contained; GTB's `DialLocalFromContainable` adapter stays.
  *(Matt leaned "move"; confirmed.)*
- **O6 — Single combined cut-over MR.** *(Matt.)*
- **O7 — Naming & docs.** `go/transport`, docs at `transport.go.phpboyscout.uk`, same
  publish playbook (public repo, DNS-only CNAME, Diátaxis microsite, landing card).
  *(Plan O3; confirmed.)*

## Open questions

None outstanding. Implementation-time detail: confirm whether any residual GTB-only glue
beyond the config adapters must stay in `pkg/*` (e.g. a config-derived `ServerSettings`
type reference) — resolve while cutting over, keeping the clean-break intent (no pure-API
re-exports).

## Testing strategy

- Pure server logic (constructors, health handlers, auth, security headers, gateway,
  `DialLocal`, `TLSClientCredentials`) is unit-tested inside `go/transport` — lift the
  existing `pkg/*` server tests, ≥90% (target 100% on new files).
- **GTB keeps adapter tests**: the `*FromContainable` adapters (which own the config-key
  derivation) are tested in GTB against a real `config.Containable`.
- **BDD (Godog):** the server stack **does** warrant E2E scenarios (service lifecycle,
  health/readiness, graceful shutdown) — the existing `features/` controls/HTTP/gRPC
  scenarios exercise GTB's retained adapters end-to-end and must stay green through the
  break.
- `depfootprint_test.go` guard in the module (forbid go-tool-base/Viper/Cobra/Charm/
  cloud; allow controls/authn/transit/tls/grpcclient/grpc/gateway).

## Documentation

- New microsite `transport.go.phpboyscout.uk` (zensical, Diátaxis): index,
  getting-started (stand up a health-checked HTTP+gRPC service), how-to (add auth /
  security headers / a gateway), explanation (the server/client split & the lifecycle
  seam). Reference → pkg.go.dev.
- **Migration note** `docs/reference/migration/v0.x-transport-extracted.md` — this one is
  a *real* migration (breaking): a symbol-by-symbol map
  `pkg/{http,grpc,gateway}.X → transport/{http,grpc,gateway}.X`, with the note that the
  `*FromContainable`/`*FromConfig` adapters remain in GTB. Update
  `docs/explanation/components/{http,grpc,gateway}.md` and tick the extraction report
  (**transport stack complete → extraction programme done**).

## Implementation phases

1. Assemble `go/transport` (http → grpc → gateway sub-packages), lift server tests,
   depfootprint + docs; build green locally (checkpoint before repo/DNS).
2. Publish: public repo, DNS-only domain, release v0.1.0 (human-gated), landing card.
3. GTB clean-break cut-over (single MR): delete moved code, keep + repoint the config
   adapters, repoint internal callers, `just ci` green; migration notes + docs; mark the
   extraction programme complete.
