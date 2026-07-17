---
title: "Remove the extraction facade re-export surfaces (clean break)"
description: "Follow-up cleanup to the transport-stack extraction programme: delete the pure re-export surfaces GTB retained over the extracted modules (the go/httpclient client facade, the go/transit HTTP+gRPC middleware re-exports, and the go/chat re-exports), repointing GTB's own code to import the modules directly. The genuine framework glue — config.Containable/Props adapters, tls.Resolve, chat's config schema and provider blank-imports — stays. A breaking change for downstream tools, shipped with a migration note."
date: 2026-07-17
status: APPROVED
tags:
  - specification
  - refactor
  - facades
  - extraction
  - breaking
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Remove the extraction facade re-export surfaces (clean break)

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-17

Status
:   APPROVED (2026-07-17) — scope chosen: **full clean break** of the removable
    re-export surfaces.

## Summary

The transport-stack extraction (six modules, five phases) is complete. Along the way GTB
retained **facades** in `pkg/*` that re-export the extracted modules so call sites stayed
unchanged. A [facade audit](../reports/2026-07-07-package-extraction-report.md) found that
most retained packages are **load-bearing glue** (config adapters, `tls.Resolve`, chat's
config schema and provider blank-imports) that cannot leave the framework — but each also
carries a **pure re-export surface** that only exists to keep call sites writing
`gtbhttp.NewClient`, `gtbhttp.NewChain`, `chat.New`, etc.

This spec removes those pure re-export surfaces (D1) and repoints GTB's own consumers to
import the modules directly. It is a **breaking change** for downstream tools built on
GTB (they repoint their imports too), shipped with a migration note. The genuine glue
stays; the packages do **not** disappear.

## Scope — what gets deleted (D1)

| File | Re-exports | Source module |
|------|-----------|---------------|
| `pkg/http/client.go` | `NewClient`, `NewTransport`, `ClientOption`, `WithTimeout`/`WithMaxRedirects`/`WithTLSConfig`/`WithTransport`/`WithCertPool`/`WithRetry`/`WithClientMiddleware` | `go/httpclient` |
| `pkg/http/reexport.go` | the ~43 `go/transit/http` middleware symbols (`Chain`, `Middleware`, `NewChain`, `LoggingMiddleware`, `RateLimitConfig`, `CircuitBreakerConfig`, `RetryConfig`, `ClientChain`, …) | `go/transit/http` |
| `pkg/grpc/reexport.go` | the ~26 `go/transit/grpc` interceptor symbols (`Interceptor`, `InterceptorChain`, `NewInterceptorChain`, `LoggingInterceptor`, `OTelStatsHandler`, `RateLimitConfig`, `CircuitBreakerConfig`, …) | `go/transit/grpc` |
| `pkg/chat/reexport.go` | the ~96 `go/chat` symbols (`ChatClient`, `Config`, `New`, `Tool`, `Provider*`, `DefaultModel*`, sentinel errors, …) | `go/chat` |

## What stays (glue — D2)

- **The `config.Containable` adapters** in `pkg/http`/`pkg/grpc`/`pkg/gateway`
  (`*FromContainable`, `ServerSettingsFromConfig`, `RateLimitConfigFromConfig`, …) and
  their GTB config-selection options (`WithConfigPrefix`/`WithPort`). Their signatures
  currently name the re-exported alias types (`RateLimitConfig`, `CircuitBreakerConfig`);
  those are **repointed to the module type directly** (`transithttp.RateLimitConfig`,
  `transitgrpc.CircuitBreakerConfig`).
- **`pkg/chat` glue**: `constants.go` (the GTB config-key schema), `config_adapter.go`
  (`SettingsFromProps`, `NewFromProps`, `NewWithFallbackFromProps`), `adapter_helpers.go`,
  and `providers.go` (the provider module **blank-imports** — without these no provider
  registers). These are repointed to name `go/chat` types directly.
- **`pkg/tls`** (its re-exports are load-bearing for `Resolve`'s signature and used very
  widely) and **`pkg/props/telemetry.go`** (core DI container) are **out of scope** —
  KEEP as-is. (Audit verdict: not worth the churn.)

## Internal repoints (non-breaking half of the work)

Before/with the deletions, GTB's own code repoints to the modules:

- **`go/httpclient`** — ~12 files that use `gtbhttp.NewClient`/`WithTimeout`/… :
  `pkg/telemetry/{backend,deletion}.go`, `pkg/telemetry/{posthog,datadog}/*.go`,
  `pkg/vcs/{github/client,github/release,gitea/release,gitlab/release,direct/provider,bitbucket/release}.go`,
  `pkg/setup/update.go`, `pkg/chat/config_adapter.go`.
- **`go/transit/http`+`/grpc`** — the config adapters (return/param types) plus the
  test-only consumers (`pkg/gateway/*_test.go`, `pkg/http/example_test.go`,
  `pkg/gateway/helpers_test.go`, `test/e2e/steps`, `test/integration/controls`).
- **`go/chat`** — ~7 files that use only the re-exports: `internal/generator/{ai,generator}.go`,
  `internal/generator/verifier/{verifier,agent,legacy}.go`, `internal/agent/{tools,query}.go`,
  plus the chat glue files (`config_adapter.go`, `adapter_helpers.go`) that name `go/chat`
  types. Import alias: `gochat "gitlab.com/phpboyscout/go/chat"` where a file also imports
  the GTB `pkg/chat` glue.

## Decisions

- **D1 — Full clean break** of the four re-export surfaces above. *(Matt.)*
- **D2 — Keep the glue** (config adapters, chat schema/adapters/providers, `pkg/tls`,
  `pkg/props/telemetry.go`); repoint retained signatures to name module types directly.
- **D3 — Ship as one MR** (mirrors the transport clean break), leaving `just ci` green,
  with a migration note. *(Proposed — see O1.)*

## Open questions

- **O1 — one MR or a short series?** The change spans `pkg/http`, `pkg/grpc`, `pkg/chat`
  and ~19 consumer files. Recommend **one MR** (the deletions and repoints must land
  together to stay green), reviewed as a single coherent break. Confirm.

## Testing strategy

- `just ci` green: unit + race + lint + coverage-policy + E2E. The config adapters keep
  their existing tests (only the named types change); the moved-consumer packages keep
  their tests (only imports change).
- No new behaviour — this is a pure import/re-export refactor. Assertions are unchanged.

## Documentation

- Migration note `docs/reference/migration/v0.x-facades-removed.md`: a per-surface map
  (`gtbhttp.NewClient` → `httpclient.NewClient`, `gtbhttp.NewChain` →
  `transithttp.NewChain`, `chat.New` → `chat.New` from `go/chat`, …), noting that the
  `*FromContainable` / `*FromProps` adapters and `pkg/tls` are unchanged.
- Update `pkg/http`/`pkg/grpc`/`pkg/chat` package docs to drop the "re-exports the
  module" language.

## Implementation phases

1. Repoint GTB-internal consumers to the modules (`go/httpclient`, `go/transit/*`,
   `go/chat`) and repoint the retained adapters' signatures to module types.
2. Delete the four re-export files.
3. `just ci` green; migration note + doc updates.
