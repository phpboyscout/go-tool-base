---
title: "Split product analytics from observability in pkg/telemetry"
description: "Separate root pkg/telemetry's two intertwined concerns — consent-gated product analytics (event collection, buffering/spill, deletion requests, machine identity, vendor backends) and service observability (OTel logs/metrics/traces setup) — into independently extractable seams. This is the prerequisite boundary cleanup identified by the extraction report before either concern can become a standalone module (observability first; product analytics later)."
date: 2026-07-12
status: DRAFT
tags:
  - specification
  - telemetry
  - observability
  - module-extraction
  - dependency-inversion
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Split product analytics from observability in pkg/telemetry

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   12 July 2026

Status
:   DRAFT

Builds on
:   [`2026-07-10-telemetry-props-decoupling.md`](2026-07-10-telemetry-props-decoupling.md),
    [`2026-07-07-config-section-adapters-for-extraction.md`](2026-07-07-config-section-adapters-for-extraction.md),
    [`2026-06-01-otel-observability.md`](2026-06-01-otel-observability.md)

Related
:   [`2026-07-07-package-extraction-report.md`](../reports/2026-07-07-package-extraction-report.md)

---

## 1. Context

`pkg/telemetry` grew into two distinct products that happen to share a package:

1. **Product analytics** — GTB's opt-in, informed-consent CLI usage pipeline: the
   `Collector`, event buffering and on-disk spill, redaction at ingestion,
   machine identity, deletion-request handling, data-directory resolution, and
   the vendor backends (`posthog`, `datadog`, the OTel-log backend).
2. **Observability** — service-side, implied-consent OpenTelemetry setup for
   long-running GTB services, built on the reusable `otelcore` / `logs` /
   `metrics` / `tracing` subpackages and exposed to GTB through
   `ObservabilitySettings` and the `observability_config_adapter.go` /
   `datadir_config_adapter.go` seams.

The [`2026-06-01-otel-observability.md`](2026-06-01-otel-observability.md) spec
deliberately co-located these under one package with **two consent models**, and
that was the right call at the time. The
[package extraction report](../reports/2026-07-07-package-extraction-report.md)
now flags the coupling as the blocker for extraction:

> Do not extract root telemetry before splitting observability from product
> analytics.

The [`2026-07-10-telemetry-props-decoupling.md`](2026-07-10-telemetry-props-decoupling.md)
spec severed the last `pkg/props` *type* import from root telemetry (relocating
`EventType` / `DeliveryMode` into `pkg/telemetrytypes`) and explicitly scoped
itself to **type coupling only**, naming this larger split as the follow-on
effort. The [config-section-adapters](2026-07-07-config-section-adapters-for-extraction.md)
spec likewise confined root telemetry's config reads to adapter files and named
this split as the remaining architectural work. This spec is that work.

## 2. Problem statement

The two concerns are entangled at the package level in ways that prevent either
from being extracted cleanly:

- **Shared package, divergent audiences.** Analytics serves the *vendor*
  (informed consent, off by default); observability serves the *operator*
  (implied consent, on when a service opts in). A single import surface forces
  every consumer of one to link the other.
- **Backends bridge both.** The vendor backends were designed as analytics
  sinks but the OTel-log backend blurs the line between an analytics transport
  and an observability signal.
- **Different extraction destinations.** The report proposes
  `gitlab.com/phpboyscout/observability` for the OTel subpackages, while product
  analytics is a lower-priority, heavier-coupling candidate (it imports
  `pkg/browser`, `pkg/controls`, `pkg/http`, `pkg/osinfo`, `pkg/redact`). They
  cannot share an extraction timeline.
- **Config seams already diverge.** Observability config resolves through
  `ObservabilitySettings` + override-precedence detection; analytics config
  resolves through consent/source-aware `IsSet`/`Has` checks. These are two
  different adapter shapes living in one package.

## 3. Goals

- Establish a clean **intra-package boundary** between the analytics concern and
  the observability concern, so each can later move to its own module without a
  second round of boundary surgery.
- Make the observability concern depend only on the OTel subpackages and
  explicit options — no dependency on the analytics `Collector`, spill, machine
  identity, or deletion machinery.
- Make the analytics concern depend on explicit app-metadata / consent
  interfaces rather than reaching into observability setup.
- Preserve **both existing consent models** and every current `telemetry.*`
  config key, precedence, and reload semantic. This is a refactor, not a
  behaviour change.
- Keep the vendor backends as optional adapters attached to the analytics
  concern, with the OTel-log backend's role explicitly classified.
- Leave GTB wiring (`pkg/props` adapters, data-dir resolution, consent prompts,
  config persistence) in the GTB composition layer, matching the pattern the
  config-section-adapters spec established.
- Produce a **sequencing plan**: observability extractable first, analytics
  later, backends after analytics contracts.

## 4. Non-goals

- **Extracting either concern into a module in this spec.** This spec delivers
  the in-tree split only. The actual `gitlab.com/phpboyscout/observability`
  module and any analytics module are separate follow-up specs that consume the
  seams defined here. This keeps the diff reviewable and the risk contained.
- **Changing consent behaviour, config keys, or the on-disk spill format.**
- **Reworking the `otelcore` / `logs` / `metrics` / `tracing` subpackages.**
  They are already the clean observability core; this spec organises *root*
  telemetry around them, it does not touch them beyond import adjustments.
- **Touching `pkg/telemetrytypes`** (already resolved upstream).

## 5. Design

### 5.1 Two internal concerns, one package (for now)

Root `pkg/telemetry` is reorganised so its files, types, and constructors group
unambiguously under one of two concerns, with **no import edges from
observability code to analytics code** (and vice versa) inside the package. The
split is by responsibility, established first as file/type grouping and a
boundary guard, so that a later `git mv` into a module is close to mechanical.

Analytics concern (the `Collector` product):

- `telemetry.go`, `backend.go`, `backend_otel.go`, `deletion.go`, `spill.go`,
  `machine*.go`, `datadir.go`, `datadir_config_adapter.go`
- vendor backends under `posthog/`, `datadog/`

Observability concern (service OTel setup):

- `observability.go`, `observability_settings.go`,
  `observability_config_adapter.go`
- depends on `otelcore` / `logs` / `metrics` / `tracing` only

### 5.2 Package layout options

Two candidate shapes; the spec review must choose one (see §11 Open Questions):

**Option A — subpackage split now.** Introduce `pkg/telemetry/analytics` and
`pkg/telemetry/observability` (or promote observability to a top-level
`pkg/observability`) in this spec, moving files immediately. Pro: the module
boundary is real and guard-enforced today; extraction becomes `git mv` of a
whole directory. Con: a larger, more disruptive diff now, and every downstream
import of `pkg/telemetry` symbols shifts (pre-1.0, acceptable, but noisy).

**Option B — grouping + guard now, physical move at extraction.** Keep one
package, enforce the no-cross-import rule with a `test/architecture` guard over
file-level symbol groups, and defer the directory move to each concern's
extraction spec. Pro: minimal churn now, downstream imports unchanged. Con: the
boundary is convention-plus-test rather than a hard package wall until
extraction.

Given the pre-1.0 posture and that the config-section-adapters spec already
proved the "co-located adapter + guard test" approach, **Option B is the
recommended default**, with Option A reserved for the observability half if it
extracts imminently.

### 5.3 Dependency-inverted seams

Whichever layout is chosen, the following seams are introduced so neither
concern reaches into GTB or into the other:

- **App metadata / consent interface** for analytics — replaces any remaining
  implicit reliance on GTB-owned values (tool name, version, machine ID, data
  dir, consent state) with a small explicit interface populated by a GTB
  adapter (`*FromProps`). Extends the `telemetrytypes` decoupling.
- **Observability options** — observability setup already takes
  `ObservabilitySettings`; confirm it needs nothing from the analytics side and
  that `ObservabilitySettingsFromProps` / `SetupFromProps` remain the only GTB
  adapters.
- **Backend contract** — the analytics `Backend` interface stays the injection
  point for `posthog` / `datadog`; classify the OTel-log backend as an analytics
  transport (not an observability signal) so it travels with the analytics
  concern.

### 5.4 Redaction boundary

Confirm redaction happens at **event ingestion** in the analytics concern before
any value reaches a backend, so a future standalone analytics module is safe by
construction (the report calls this out explicitly). Add a test asserting the
ingestion boundary redacts.

## 6. Testing strategy

- **Import-boundary guard** (`test/architecture`): assert no observability
  symbol/file imports an analytics symbol/file and vice versa, mirroring the
  existing `pkg/props`/`pkg/logger` guard from the config-section-adapters spec.
- **Behaviour-preservation tests**: existing `telemetry_test.go`,
  `observability_test.go`, `backend_test.go`, `spill_test.go`, and the
  `observability_e2e_test.go` suites must pass unchanged (aside from import
  paths) — the split is behaviour-neutral.
- **Redaction-at-ingestion test** as per §5.4.
- **Consent-model tests**: assert informed-consent (analytics, off by default)
  and implied-consent (observability, service opt-in) paths are independently
  exercisable after the split.
- No new BDD scenarios: no user-visible CLI workflow changes. Record in the
  implementation notes that unit/integration coverage is sufficient.

## 7. Migration & compatibility

- Pre-1.0: import-path churn from a subpackage split (Option A) or symbol moves
  is an acceptable minor break; add `docs/migration/` notes for any moved
  exported symbol.
- Preserve every `telemetry.*` config key, precedence, and reload semantic.
- `pkg/props` retains any aliases needed so downstream tools are unaffected,
  consistent with the telemetrytypes approach.

## 8. Sequencing (post-split)

1. **This spec**: in-tree split + guard + seams.
2. **Observability extraction** (separate spec): move the observability concern
   + OTel subpackages into `gitlab.com/phpboyscout/observability`; GTB consumes
   it via `ObservabilitySettingsFromProps`.
3. **Analytics contracts** (separate spec): finalise app-metadata/consent
   interfaces, then extract the analytics `Collector`.
4. **Backend modules** (separate spec): move `posthog` / `datadog` as optional
   adapter modules once analytics contracts are stable, accepting a standard
   `*http.Client`.

## 9. Future considerations

- A shared `gitlab.com/phpboyscout/observability` could become the instrumentation
  dependency for extracted `http` / `grpc` modules.
- The analytics module may warrant a `gtb-`-prefixed name if it stays
  GTB-flavoured (consent UX, deletion requests) rather than fully generic.

## 10. Implementation phases

- **Phase 1** — Classify every root-telemetry file/type under exactly one
  concern; write the `test/architecture` boundary guard (failing first).
- **Phase 2** — Introduce the app-metadata/consent seam for analytics; move any
  residual GTB reach-through behind `*FromProps` adapters.
- **Phase 3** — Sever cross-concern imports until the guard passes; classify the
  OTel-log backend; add the redaction-at-ingestion test.
- **Phase 4** — (Option A only) physically split into subpackages and update
  imports; otherwise finalise grouping.
- **Phase 5** — Docs: update the telemetry component pages, `doc.go`, the
  extraction report, and add migration notes for any moved symbols.

## 11. Open questions

1. **Layout: Option A (subpackage split now) vs Option B (grouping + guard,
   move at extraction).** Recommended B; confirm.
2. **Observability destination**: promote to a top-level `pkg/observability`
   before extraction, or keep it under `pkg/telemetry/observability` until the
   module move?
3. **OTel-log backend classification**: analytics transport (travels with the
   `Collector`) or an observability signal? Recommended: analytics transport.
4. **Analytics module naming**: generic vs `gtb-`-prefixed — defer to the
   analytics extraction spec, or decide now to shape the seam?
