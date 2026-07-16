---
title: "Split product analytics from observability in pkg/telemetry"
description: "REFRAMED 2026-07-16: observability's core has been extracted to go/observability, and the two concerns are already import-separated, so this spec narrows to the remaining product-analytics extraction (the Collector, spill, deletion, machine identity, vendor posthog/datadog backends → an analytics module + backend modules). Analytics is blocked on pkg/http (needs go/httpclient, Phase 4) and pkg/osinfo, so it is re-sequenced after the transport stack — not next."
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
:   REFRAMED 2026-07-16 (observability core extracted; scope narrowed to the analytics remainder — see §0)

Builds on
:   [`2026-07-10-telemetry-props-decoupling.md`](2026-07-10-telemetry-props-decoupling.md),
    [`2026-07-07-config-section-adapters-for-extraction.md`](2026-07-07-config-section-adapters-for-extraction.md),
    [`2026-06-01-otel-observability.md`](2026-06-01-otel-observability.md)

Related
:   [`2026-07-07-package-extraction-report.md`](../reports/2026-07-07-package-extraction-report.md)

---

## 0. Reframe (2026-07-16) — observability extracted; this spec is now analytics-only

This spec was written **before** the observability extraction and framed an *in-tree
split* as the prerequisite to extracting either concern. That work has since happened
by a different route, so the spec is reframed:

- **Observability's core is already extracted.** `otelcore` + `logs` + `metrics` +
  `tracing` now live in the standalone
  [`gitlab.com/phpboyscout/go/observability`](https://gitlab.com/phpboyscout/go/observability)
  `v0.1.0` (Phase 2, spec `2026-07-16-observability-module-extraction.md`, a full
  repoint). What remains in root `pkg/telemetry` for observability is only the thin GTB
  **wiring** — `Setup`/`SetupFromProps`, `ObservabilitySettings`, and the relocated
  config-key adapter (`observability_otel_config.go`) — which consumes the module. That
  wiring is GTB composition and is **not** a further module candidate.
- **The two concerns are already import-separated.** Measured on the current tree, the
  observability wiring files import **no** analytics symbol (`Collector`, `Backend`,
  spill, machine identity, deletion), and vice versa. The `test/architecture`
  no-cross-import guard this spec proposed (§5.1/§6) is therefore **effectively already
  satisfied** — the split-as-prerequisite is done. No separate in-tree-split effort is
  needed.
- **Only the analytics concern remains to extract.** The live target is root
  `pkg/telemetry`'s **product-analytics** product — the `Collector`, buffering/spill,
  deletion, machine identity, data-dir, and the vendor backends — into an analytics
  module (+ `posthog`/`datadog` backend modules). Everything below now reads as the
  plan for **that** extraction.

### 0.1 Analytics is blocked — it is NOT the next extraction

The analytics concern's current GTB coupling (production imports), post the
`browser`/`redact`/`controls` extractions:

| Import | Count | Status |
|---|--:|---|
| `pkg/telemetrytypes` | 5 | shared types — move with analytics or keep as a shared dep |
| `pkg/telemetry` (self) | 5 | intra-package (posthog/datadog → the `Backend` interface) |
| **`pkg/http`** | **4** | **blocker** — analytics ships events over the hardened HTTP client; needs **`go/httpclient`** (Phase 4) or the transport extraction first |
| `pkg/props` | 2 | GTB composition — handled via `*FromProps` adapters, not a blocker |
| `pkg/logger` | 2 | the `*slog.Logger` seam |
| **`pkg/osinfo`** | **1** | **blocker** — machine identity; needs `osinfo` extracted or the dep severed |

So analytics extraction is **gated behind Phase 4 (`go/httpclient`)** at minimum (so the
backends can dial via `go/httpclient` instead of `pkg/http`), plus resolving `pkg/osinfo`.
It is **re-sequenced after the transport stack**, not next. The immediate next step in
the extraction program is **Phase 3 (`go/transportmw`)** per the
[transport-stack plan](2026-07-13-transport-stack-extraction-plan.md).

The remainder of this document (the app-metadata/consent seam, redaction-at-ingestion,
the backend contract, and the OTel-log-backend-as-analytics-transport classification)
stands as the design for the eventual analytics extraction, to be picked up once its
blockers clear.

---

## 1. Context (historical — pre-reframe)

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

## 8. Sequencing (reframed 2026-07-16)

1. ~~In-tree split + guard + seams~~ — **not needed**: the concerns are already
   import-separated (§0).
2. ~~Observability extraction~~ — **DONE**: `go/observability` v0.1.0 (the OTel
   subpackages moved; GTB consumes it via `SetupFromProps` + the relocated config
   adapter).
3. **Transport stack first** — Phase 3 `go/transportmw`, Phase 4 `go/httpclient` +
   `go/grpcclient` (per the transport plan). Analytics extraction depends on
   `go/httpclient` (its backends ship events over HTTP), so it waits for Phase 4.
4. **Resolve `pkg/osinfo`** — extract it or sever the machine-identity dependency.
5. **Analytics contracts** (separate spec): finalise the app-metadata/consent
   interface (§5.3), classify the OTel-log backend as an analytics transport (§5.3),
   confirm redaction-at-ingestion (§5.4), then extract the analytics `Collector` into
   an analytics module — dialling `go/httpclient`, not `pkg/http`.
6. **Backend modules** (separate spec): move `posthog` / `datadog` as optional adapter
   modules once analytics contracts are stable, each accepting a standard `*http.Client`
   (or `go/httpclient`).

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
