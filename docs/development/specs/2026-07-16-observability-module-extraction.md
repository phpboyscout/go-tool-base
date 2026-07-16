---
title: "Extract the OTel observability group into a standalone go/observability module"
description: "Extract the OpenTelemetry logs/metrics/traces setup group (pkg/telemetry/otelcore + logs + metrics + tracing) out of go-tool-base into gitlab.com/phpboyscout/go/observability. The core is config-agnostic OTel plumbing (only the OTel SDK + cockroachdb/errors); the single GTB seam is otelcore's config-key Resolve adapter, which stays behind in a thin pkg/telemetry/otelcore facade. Third and final Phase 2 foundation. Confirms D3: the observability group extracts cleanly WITHOUT the root telemetry analytics/observability split."
date: 2026-07-16
status: APPROVED
tags:
  - specification
  - observability
  - telemetry
  - opentelemetry
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract the OTel observability group into a standalone `go/observability` module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-16

Status
:   APPROVED (O1–O4 resolved in review 2026-07-16)

## Summary

The `pkg/telemetry/otelcore` + `logs` + `metrics` + `tracing` group is GTB's
**service-observability** layer: hardened OpenTelemetry setup for OTLP logs, metrics,
and traces — endpoint/resource resolution, the typed per-signal `Settings`, and the
signal providers built on the OTel contrib SDK. It has clear reuse value for any Go
service that wants opinionated OTel wiring, independent of the rest of GTB.

This spec extracts it to `gitlab.com/phpboyscout/go/observability` following the
[module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) and is the
**third and final Phase 2 foundation** of the
[transport-stack extraction plan](2026-07-13-transport-stack-extraction-plan.md)
(after `tls` and `authn`).

The core is config-agnostic and works from typed `Settings` values; the single GTB
seam is `otelcore`'s config-key adapter (`Resolve` / `ObserveSettingsFromConfig`,
which read `config.Containable`). Per **O1 (resolved: full repoint)**, that adapter
**relocates into root `pkg/telemetry`** (package `telemetry`) rather than staying as a
facade package — a clean break: the in-tree `pkg/telemetry/otelcore` is deleted
outright and root-telemetry consumers import `go/observability/otelcore` directly. The
one material difference from `tls`/`authn`: the module **carries the OpenTelemetry
SDK**, so its depfootprint guard *allows* `go.opentelemetry.io` while still forbidding
go-tool-base, Viper/pflag/Cobra, Charm, and the cloud SDKs.

## D3 — observability extracts without the telemetry split (resolved: yes)

The [transport plan D3](2026-07-13-transport-stack-extraction-plan.md) and the
[telemetry-split spec](2026-07-12-telemetry-analytics-observability-split.md) raised a
concern: root `pkg/telemetry` mixes **product analytics** (consent-gated `Collector`,
spill, deletion, machine identity, PostHog/Datadog backends) with **observability**,
and the report says "do not extract root telemetry before splitting these."

Measured on the current tree, that concern **does not block this extraction**:

- The observability subpackages (`otelcore`/`logs`/`metrics`/`tracing`) import **zero**
  analytics code — no import of the root `pkg/telemetry` package. Their only GTB
  coupling is `pkg/config`, confined to `otelcore/config_adapter.go` (and its test);
  `viper`/`afero` appear only in tests.
- The dependency runs **analytics → observability**, not the reverse: root
  `pkg/telemetry/backend_otel.go` (the analytics OTel-log transport) consumes
  `otelcore`/`logs`. After extraction that becomes GTB-analytics → `go/observability`,
  a perfectly sound direction.

So the split spec's blocker is about extracting the **analytics root**, not the
observability group. Extracting `observability` **first** is clean and actually
*advances* the split: the observability concern physically leaves the package, leaving
root `pkg/telemetry` as analytics-plus-thin-wiring. **O4** asks whether to formally
reframe the split spec in light of this.

## Target module

- **Module path:** `gitlab.com/phpboyscout/go/observability` (per
  [naming convention](../../../MEMORY.md); the report's proposed name).
- **Package layout:** one module, subpackages **`otelcore`** (core), **`logs`**,
  **`metrics`**, **`tracing`** — mirroring today's layout (`logs`/`metrics`/`tracing`
  import `otelcore` internally).
- **Docs:** `observability.go.phpboyscout.uk` (light microsite — **O2**).
- **Local dir:** `~/workspace/phpboyscout/go/observability`.
- **Reference scaffold:** `~/workspace/phpboyscout/go/tls` (the facade sibling).

## What moves vs. what stays

### Moves into `go/observability` (the config-agnostic core + pure tests)

`otelcore` core — everything **except** the config adapter:

- `config.go` (`Config`, `SignalConfig`, `Settings`, `SignalOverrides`,
  `ResolveSettings`, `Root`, the `Signal*` consts), `endpoint.go` (`ParseEndpoint`),
  `resource.go` (`Resource`), `doc.go`, and their pure tests. These import only the
  OTel SDK + `cockroachdb/errors`.

The signal subpackages whole: `logs/`, `metrics/`, `tracing/` (each `*.go` +
`exporter_error_test.go` + `*_test.go`). They import `otelcore` (intra-module) and the
OTel exporter/SDK packages — no GTB imports.

### Stays in GTB (the config-key adapter, relocated — O1: full repoint)

There is **no** in-tree `otelcore` package after this change. The config-key adapter
**moves up into root `pkg/telemetry`** (package `telemetry`):

- `otelcore/config_adapter.go` → a new `pkg/telemetry/observability_otel_config.go`
  (package `telemetry`) holding `Resolve(cfg config.Containable, signal string) Settings`
  and `ObserveSettingsFromConfig(...)`, now referencing the module types via
  `obs "gitlab.com/phpboyscout/go/observability/otelcore"` (`obs.Settings`,
  `obs.Config`, `obs.SignalConfig`, `obs.SignalOverrides`, `obs.ResolveSettings`,
  `obs.Root`). Its test moves alongside.
- Root-telemetry callers of the adapter are already in package `telemetry`, so
  `otelcore.Resolve(...)` becomes the local `Resolve(...)` (or a lightly-renamed
  helper to avoid collisions); every other `otelcore.*` reference becomes `obs.*`
  against the module.

## Consumer surface & cut-over strategy

The **only** GTB consumers of the group are within root `pkg/telemetry` itself:
`observability.go`, `observability_config_adapter.go`, `observability_settings.go`,
and `backend_otel.go`. The transport packages do **not** consume it — they instrument
via the OTel contrib SDK directly (per the transport plan).

Used symbols: `otelcore.Settings` (10×), `otelcore.Resolve` (3×, the adapter that
stays), `otelcore.Root` (2×), `otelcore.Resource` (2×), `otelcore.ParseEndpoint` (1×),
`otelcore.Signal{Logs,Metrics,Tracing}` (1× each); plus `logs`/`metrics`/`tracing`
imports in `observability.go` (+ `backend_otel.go` uses `logs`).

Cut-over shape (**O1 — resolved: full repoint, no facade**):

- Delete `pkg/telemetry/otelcore/` entirely; import `go/observability/otelcore` as
  `obs` in root `pkg/telemetry`. Repoint the ~18 `otelcore.*` references
  (`Settings`×10, `Root`×2, `Resource`×2, `ParseEndpoint`, the `Signal*` consts) to
  `obs.*`; the adapter `Resolve`/`ObserveSettingsFromConfig` relocate into package
  `telemetry` (above) and their call sites become local calls.
- **`logs`/`metrics`/`tracing` → repoint**: repoint the imports in
  `observability.go`/`backend_otel.go` from `…/pkg/telemetry/{logs,metrics,tracing}` to
  `…/go/observability/{logs,metrics,tracing}` and delete the in-tree subpackages.
- A clean break (Matt's call): no shim/facade package lingers under `pkg/telemetry`.

## Repository framework (playbook §3)

Bootstrap from the `go/tls` scaffold, with **OTel allowed**:

- **CI**: `go-lint`, `go-test` (`enable_e2e: false`), `go-security`,
  `releaser-pleaser`, `zensical-pages`, `renovate-self` — all `@v0.22.0`.
- `depfootprint_test.go`: **allow** `go.opentelemetry.io/*` (and its transitive
  `google.golang.org/grpc`, `google.golang.org/protobuf`, etc.); **forbid**
  `go-tool-base`, `spf13/viper`, `spf13/pflag`, `spf13/cobra`, `charmbracelet`,
  `aws-sdk-go`, `cloud.google.com/go`, `Azure/azure-sdk`. This is the key guard
  difference from the leaf/`tls`/`authn` modules.
- **OTel version lockstep**: pin the exact OTel versions GTB uses today
  (`otel/sdk@v1.44.0`, `exporters/otlp/*` at `v0.20.0`/`v1.44.0`,
  `contrib/bridges/otelslog@v0.19.0`, `semconv v1.26.0`). GTB and the module must
  track OTel together — note this in the compatibility section and let grouped
  Renovate keep them aligned (**O3**).
- `.golangci.yaml` v2, local prefix `gitlab.com/phpboyscout/go/observability`; ≥90%
  coverage; `cockroachdb/errors`; `LICENSE`, README, `requirements-lock.txt`.

## Documentation (playbook §4)

- Author `observability.go.phpboyscout.uk` (light microsite — **O2**) from the group's
  `doc.go` + GTB's observability docs, de-GTB-ed (the config-key `Resolve` cascade
  stays in GTB docs; the module docs cover OTel setup, endpoint/resource resolution,
  and the typed `Settings`). Reference → pkg.go.dev.
- GitLab Pages custom domain + Cloudflare DNS **DNS-only** + `pages_access_level=public`
  + `pages_primary_domain=https://…` (per the `.go` learnings).
- GTB side: the observability component page keeps the config-adapter + `telemetry.*`
  key-cascade story and links out; migration note in `docs/reference/migration/`;
  extraction-report checklist ticked.

## Migration procedure (playbook §5)

1. Bootstrap `phpboyscout/go/observability` repo; trivial-commit CI green; Pages
   domain + DNS.
2. `git mv` the `otelcore` core (minus `config_adapter.go`) + `logs`/`metrics`/`tracing`
   + pure tests; add the OTel-allowing `depfootprint_test.go`; confirm ≥90%;
   race/lint/vet/govulncheck green (watch OTel-transitive OSV advisories).
3. Author the microsite; wire Pages + DNS.
4. Cut `v0.1.0` via the Release-MR flow (Matt merges).
5. **GTB cut-over (single MR):** add `go/observability v0.1.0`; reduce
   `pkg/telemetry/otelcore` to the facade (`config_adapter.go` + a `reexport.go`);
   delete the moved core + the in-tree `logs`/`metrics`/`tracing`; repoint their
   imports in `observability.go`/`backend_otel.go`; `just ci` green (modulo the known
   `internal/agent` buildvcs test); migration note; component-page stub; report tick.
6. Downstream hand-off via the migration note (we do not edit downstream repos).

## Resolved decisions (review 2026-07-16)

1. **O1 — Cut-over shape: full repoint, no facade.** Delete `pkg/telemetry/otelcore/`
   outright; root `pkg/telemetry` imports `go/observability/otelcore` directly and the
   config-key adapter relocates into package `telemetry`. A clean break — no lingering
   facade/shim — consistent with GTB's pre-1.0 "prefer a clean break over a shim"
   stance. `logs`/`metrics`/`tracing` likewise repoint-and-delete.
2. **O2 — Docs: light microsite** at `observability.go.phpboyscout.uk` (family
   consistency with `tls`/`authn`): overview + a how-to (wire OTLP logs/metrics/traces)
   + an explanation of endpoint resolution and the `OTEL_EXPORTER_OTLP_*` env fallback.
   Reference → pkg.go.dev.
3. **O3 — OTel version policy: pin + lockstep.** Pin the module to GTB's exact OTel
   versions (`otel/sdk@v1.44.0`, `exporters/otlp/*` `v0.20.0`/`v1.44.0`,
   `contrib/bridges/otelslog@v0.19.0`, `semconv v1.26.0`); grouped Renovate keeps GTB
   and the module aligned; a README compatibility line documents the coupling.
4. **O4 — Proceed observability-first, then reframe the split spec.** This extraction
   *is* the observability half of the analytics/observability split, done physically.
   After it lands, **revise the DRAFT
   [telemetry-split spec](2026-07-12-telemetry-analytics-observability-split.md)** to
   cover only the remaining **analytics** concern (root `pkg/telemetry` → an analytics
   module + PostHog/Datadog backend modules); its proposed in-tree split is largely
   moot once observability physically leaves.

## Acceptance criteria

- `go/observability` builds under `gitlab.com/phpboyscout/go/observability` with
  `otelcore`/`logs`/`metrics`/`tracing` subpackages; `depfootprint_test.go` passes
  (OTel allowed; no go-tool-base/framework/cloud-SDK); ≥90% coverage; race/lint/vet
  green; `v0.1.0` published and proxy-resolvable.
- GTB consumes `go/observability v0.1.0`; `pkg/telemetry/otelcore` is the facade;
  `logs`/`metrics`/`tracing` deleted and repointed; **no root-telemetry `otelcore.*`
  reference changed**; `just ci` green (modulo the known `internal/agent` test).
- Docs live per O2; migration note added; extraction-report checklist ticks
  `observability`; the telemetry-split spec is reframed per O4.
