---
title: "Decouple root pkg/telemetry from pkg/props"
description: "Sever the last pkg/props import from root pkg/telemetry by relocating the shared telemetry value types (EventType, DeliveryMode and their constants) into a dependency-free leaf package that both pkg/props and pkg/telemetry import, so the collector no longer depends on GTB's DI container. Scoped to the type coupling only — not the larger product-analytics / observability split."
date: 2026-07-10
status: IMPLEMENTED
tags:
  - specification
  - telemetry
  - module-extraction
  - dependency-inversion
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude
    role: AI drafting assistant
---

# Decouple root pkg/telemetry from pkg/props

Authors
:   Matt Cockayne, Claude *(AI drafting assistant)*

Date
:   10 July 2026

Status
:   IMPLEMENTED

Builds on
:   [`2026-07-07-config-section-adapters-for-extraction.md`](2026-07-07-config-section-adapters-for-extraction.md),
    [`2026-07-07-slog-first-extraction-seams.md`](2026-07-07-slog-first-extraction-seams.md)

Related
:   [`2026-07-07-package-extraction-report.md`](../reports/2026-07-07-package-extraction-report.md)

---

## 0. Approved decisions (2026-07-11 review)

Approved for implementation as part of the `refactor/config-section-adapters`
migration. The open questions in §10 are resolved to their recommended answers:

- **Q1 — alias lifetime**: keep the `props.*` aliases (`EventType`,
  `DeliveryMode`, and the constants) **permanently**. Zero downstream cost, and
  `props` remains the natural lookup home for `TelemetryConfig`-adjacent
  constants. Revisit only at the eventual telemetry-module extraction spec.
- **Q2 — leaf location/name**: **`pkg/telemetrytypes`** (top-level sibling), so
  neither `props` nor `telemetry` appears to own the other's types.
- **Q3 — backend field updates**: covered transitively. `telemetry/posthog` and
  `telemetry/datadog` read `Event.Type` and compile unchanged; only their
  tests, which name the removed mirror constant, are repointed to
  `telemetrytypes`.
- **Q4 — scope**: **yes** — removing the `props` *type* coupling is the goal of
  this change. The larger product-analytics / observability split remains a
  separate, later effort (see §4 Non-goals).

## 1. Context

The config-section-adapters and slog-first work has decoupled every first-wave
extraction candidate (`chat`, `tls`, `http`, `grpc`, `gateway`, `vcs` +
providers + `repo`, `telemetry/otelcore`) from both `pkg/props` and
`pkg/logger`. A source-level guard (`test/architecture/boundary_test.go`) now
locks that in.

Root `pkg/telemetry` is the last extraction candidate still importing
`pkg/props` from its **core** (non-adapter) files. Its `pkg/logger` coupling is
already gone (it accepts `*slog.Logger`), and its **config** reads already live
in adapter files (`observability_config_adapter.go`, `datadir_config_adapter.go`).
What remains is a coupling to two `props` **value types**:

- `props.EventType` plus the six `Event*` constants
  (`EventCommandInvocation`, `EventCommandError`, `EventFeatureUsed`,
  `EventUpdateCheck`, `EventUpdateApplied`, `EventDeletionRequest`).
- `props.DeliveryMode` plus `DeliveryAtLeastOnce` / `DeliveryAtMostOnce`.

### 1.1 Why the coupling exists

`pkg/props` deliberately owns the `TelemetryCollector` **interface** so the DI
container can carry a `Collector` field without importing a concrete telemetry
implementation. `props.TelemetryConfig.Backend` and `.DeletionRequestor` are
typed `func(*Props) any` for the same reason — props stays implementation-
agnostic and never imports `pkg/telemetry` (which would drag in `pkg/http`,
`pkg/browser`, `pkg/redact`, and the OTel trees).

The direction of the dependency is therefore:

```
props defines TelemetryCollector.Track(eventType props.EventType, ...)
      ↓ (implements)
telemetry.Collector.Track(eventType props.EventType, ...)   ← must import props
```

Because Go interface satisfaction requires the method's parameter type to match
**exactly**, `telemetry.Collector.Track` must name `props.EventType`, forcing the
`pkg/props` import. `telemetry` already keeps a **mirror** copy of `EventType`
and the constants (`pkg/telemetry/telemetry.go`) and converts at the boundary —
but the mirror does not remove the import, because the public `Track` signature
still names the props type. `DeliveryMode` is not mirrored at all: it appears
raw as a `NewCollector` parameter (`telemetry.go`), a `Collector` field, and in
spill retention logic (`spill.go`).

### 1.2 Precise coupling inventory

Non-test, non-adapter `pkg/props` usage in root telemetry:

| File | Line(s) | Symbol |
|------|---------|--------|
| `telemetry.go` | import + 84, 90, 99, 251 | `props.DeliveryMode`, `props.DeliveryAtLeastOnce` |
| `telemetry.go` | 183, 194, 206 | `props.EventType`, `props.EventCommandInvocation` |
| `spill.go` | import + 154, 164 | `props.DeliveryAtMostOnce`, `props.DeliveryAtLeastOnce` |

Downstream references that constrain any change (must keep compiling):

- **Interface satisfaction linchpin**: `pkg/cmd/root/root.go` —
  `var _ p.TelemetryCollector = (*telemetry.Collector)(nil)`.
- `props.TelemetryCollector.Track(eventType EventType, ...)` and
  `props.NoopCollector.Track` (`pkg/props/telemetry.go`).
- `props.TelemetryConfig.DeliveryMode` (a **tool-author-set field** — public
  API used by every downstream tool that customises delivery).
- Backends read `Event.Type` (`telemetry/posthog`, `telemetry/datadog`).
- Generated mock `mocks/pkg/props/TelemetryCollector.go` (regenerate).
- ~25 test call sites passing `props.Event*` / `props.Delivery*` constants.
- `internal/generator/` templates: **no** references — scaffolded tools do not
  emit `Track` / `EventType` / `DeliveryMode` code, so no generator changes.

## 2. Problem Statement

Root `pkg/telemetry` cannot be extracted as a standalone module while it imports
`pkg/props`, GTB's runtime DI container. The remaining coupling is purely two
shared value types plus their constants. We need those types to live somewhere
both `pkg/props` (which owns the collector interface and the tool-author config
field) and `pkg/telemetry` (which owns the runtime behaviour) can import, with
**neither package importing the other**.

## 3. Goals

- Remove every `pkg/props` import from non-adapter files of root
  `pkg/telemetry`.
- Preserve type safety on the `Track` boundary and the
  `TelemetryConfig.DeliveryMode` field (no regression to bare `string`).
- Keep the change **backward compatible for downstream tools** where feasible —
  code referencing `props.EventCommandInvocation`,
  `props.DeliveryAtLeastOnce`, etc. should continue to compile.
- Add root `pkg/telemetry` to the `boundary_test.go` guard once decoupled, so a
  future `pkg/props` re-import fails CI.
- Ship a `docs/migration/` note for the pre-1.0 audit trail.

## 4. Non-goals

- **The product-analytics / observability split.** The extraction report
  (`2026-07-07-package-extraction-report.md`) states root telemetry mixes
  product analytics, consent, deletion requests, and spill with observability
  concerns and "must not be extracted before splitting observability from
  product analytics." That split is a **separate, larger** effort. This spec
  only removes the `pkg/props` *type* coupling; it does **not** claim to make
  telemetry fully extractable on its own.
- Changing telemetry runtime behaviour, backends, spill semantics, consent
  flows, or the collector's method set.
- Touching the `telemetry/otelcore|logs|metrics|tracing` observability
  subpackages (already decoupled or out of scope here).

## 5. Design

### 5.1 Recommended approach — shared leaf types package (Approach A)

Introduce a dependency-free leaf package holding the shared value types and
constants:

```go
// Package <name> holds the telemetry value types shared between the DI
// container (pkg/props) and the collector implementation (pkg/telemetry),
// so neither has to import the other. It imports nothing from GTB.
package <name>

type EventType string

const (
    EventCommandInvocation EventType = "command.invocation"
    EventCommandError      EventType = "command.error"
    EventFeatureUsed       EventType = "feature.used"
    EventUpdateCheck       EventType = "update.check"
    EventUpdateApplied     EventType = "update.applied"
    EventDeletionRequest   EventType = "data.deletion_request"
)

type DeliveryMode string

const (
    DeliveryAtLeastOnce DeliveryMode = "at_least_once"
    DeliveryAtMostOnce  DeliveryMode = "at_most_once"
)
```

Both `pkg/props` and `pkg/telemetry` import this leaf. The dependency graph
becomes:

```
        <leaf types>
        ↑          ↑
   pkg/props   pkg/telemetry     (siblings; no edge between them)
```

- `pkg/props`: `TelemetryCollector.Track(eventType <leaf>.EventType, ...)`,
  `NoopCollector.Track`, and `TelemetryConfig.DeliveryMode <leaf>.DeliveryMode`.
- `pkg/telemetry`: `Collector.Track(eventType <leaf>.EventType, ...)`,
  `NewCollector(..., deliveryMode <leaf>.DeliveryMode, ...)`, `Event.Type`,
  spill comparisons — all against the leaf type. The internal mirror in
  `telemetry.go` is deleted.
- The `var _ p.TelemetryCollector = (*telemetry.Collector)(nil)` assertion in
  `root.go` holds because both sides now name the identical leaf type.

#### 5.1.1 Backward-compatibility via type aliases (recommended)

To avoid breaking downstream tools that reference `props.EventCommandInvocation`
or set `props.DeliveryAtLeastOnce`, `pkg/props` keeps the names as **aliases**:

```go
// pkg/props/telemetry.go
type EventType = <leaf>.EventType         // alias, not a new type
type DeliveryMode = <leaf>.DeliveryMode

const (
    EventCommandInvocation = <leaf>.EventCommandInvocation
    // ... etc
    DeliveryAtLeastOnce = <leaf>.DeliveryAtLeastOnce
    DeliveryAtMostOnce  = <leaf>.DeliveryAtMostOnce
)
```

Because a type alias is identity, `props.EventType` **is**
`<leaf>.EventType`; downstream code compiles unchanged, and telemetry's
leaf-typed `Track` satisfies `props.TelemetryCollector`. The only mechanical
edits are: delete telemetry's mirror, repoint telemetry's internal references to
the leaf, regenerate the mock, and update the in-repo test call sites (which may
also keep using the `props.*` aliases if we choose). This reduces the change
from a hard public-API break to a near-transparent relocation.

> **Open question Q1** — do we keep the `props.*` aliases permanently, or mark
> them `// Deprecated:` and point downstream at the leaf? Recommendation: keep
> them (zero downstream cost; props is the natural place a tool author looks for
> `TelemetryConfig`-adjacent constants), and revisit at the telemetry-module
> extraction spec.

### 5.2 Where does the leaf live? (Open question Q2)

Candidates:

1. **`pkg/telemetry/<subpkg>`** (e.g. `pkg/telemetry/telemetrytype`) — reads
   naturally, travels with telemetry on extraction. But then `pkg/props` (which
   stays in GTB) imports a subpackage under the to-be-extracted tree; after
   extraction that becomes `import <extracted-module>/telemetrytype`. Acceptable
   (GTB → extracted lib is a fine direction) but means props tracks the module
   path.
2. **Top-level `pkg/telemetrytypes`** — neutral sibling of both; clearest
   ownership story; one more top-level package.
3. **Reuse an existing leaf** — none currently fits (would overload an unrelated
   package). Not recommended.

Recommendation: **option 2** (`pkg/telemetrytypes` or similar), so ownership is
unambiguous and neither `props` nor `telemetry` appears to "own" the other's
types. Final name to be confirmed at review.

### 5.3 Rejected alternatives

- **Approach B — plain-`string` interface.** Change `Track(eventType string,
  ...)` on both sides and keep `telemetry.EventType` internal only. Simplest
  (no new package) but drops compile-time enum safety at the interface boundary
  and at every call site, and pushes `string(...)` conversions onto callers.
  Rejected: type-safety regression for a foundational surface.
- **Approach C — invert (props imports telemetry).** Move the interface and
  types into `pkg/telemetry`; `pkg/props` imports it. Rejected: `pkg/props` is a
  low-level, widely-imported package deliberately kept implementation-agnostic
  (hence the `func(*Props) any` backend hooks). Importing telemetry would pull
  `pkg/http`, `pkg/browser`, `pkg/redact`, and the OTel dependency trees into
  every consumer of props.

## 6. Public API Plan

Pre-1.0 (`v0.x`): breaking changes ship as a minor/patch with a migration note,
no `BREAKING CHANGE:` footer (per AGENTS.md § API Stability). With the alias
strategy (§5.1.1) the net downstream break is expected to be **zero**; the
change is nonetheless documented as an API-surface relocation.

- **New**: leaf package with `EventType`, `DeliveryMode`, and constants.
- **`pkg/props`**: `EventType` / `DeliveryMode` become aliases to the leaf;
  constants re-exported; `TelemetryCollector` and `TelemetryConfig` unchanged in
  spelling. No behavioural change.
- **`pkg/telemetry`**: internal mirror removed; `Track` / `NewCollector` /
  `Event.Type` / spill now reference the leaf type (identical values). No
  `pkg/props` import remains in core files.
- **`mocks/pkg/props/TelemetryCollector.go`**: regenerated. Per
  `project_mockery_churn`, regenerate then keep only this package's mock and
  discard the unrelated import-order churn.

## 7. TDD / BDD Strategy

- Unit: existing `pkg/telemetry` tests continue to pass unchanged (values are
  identical); update only where a test names `props.EventType` as a *distinct*
  type in a table (e.g. the `props.EventType`↔mirror mapping table in
  `telemetry_test.go`, which becomes redundant and should be simplified).
- Architecture guard: add `pkg/telemetry` to `firstWaveRoots` in
  `boundary_test.go` (its `*_adapter.go` files remain exempt and legitimately
  keep importing props/config). This is the executable acceptance test for the
  decoupling. Confirm the guard *fails* before the change and *passes* after.
- No new BDD scenarios — no user-facing CLI or lifecycle behaviour changes.

## 8. Documentation

- `docs/migration/`: relocation note (types moved to leaf; `props` aliases
  retained; no action required for downstream tools).
- `docs/components/`: update the telemetry component doc's type references and
  note the leaf package as the shared-types home.
- Update `2026-07-07-config-section-adapters-for-extraction.md` remaining-work
  item 2 and the handover note to point at this spec.

## 9. Implementation Phases

1. **Leaf package** — create it with the types + constants and a focused test.
2. **props aliases** — repoint `pkg/props` to the leaf via aliases; regenerate
   the `TelemetryCollector` mock (keep only that package's mock).
3. **telemetry core** — delete the mirror; repoint `telemetry.go` / `spill.go`
   to the leaf; drop the `pkg/props` import. Update in-package tests.
4. **Guard + docs** — add `pkg/telemetry` to `boundary_test.go`; write the
   migration note and component-doc updates.
5. **Verify** — `go build ./...`; `go test $(go list ./... | grep -v
   internal/agent)`; `golangci-lint run`; `go test ./test/architecture/...`.

## 10. Open Questions

- **Q1** — Keep the `props.*` aliases permanently, or deprecate them toward the
  leaf? (Recommendation: keep; see §5.1.1.)
- **Q2** — Leaf package location and name: `pkg/telemetrytypes` (recommended)
  vs. `pkg/telemetry/telemetrytype`. (§5.2)
- **Q3** — Should this spec also fold in the trivial `Event.Type` field-type
  update in the backends, or are those covered transitively? (They compile
  unchanged under the alias strategy; listed for completeness.)
- **Q4** — Scope confirmation: is decoupling the props *types* sufficient for
  this MR's goal, with the full analytics/observability split tracked
  separately? (This spec assumes **yes** — see §4 Non-goals.)

## 11. Acceptance Criteria

- No non-adapter file under `pkg/telemetry` imports `pkg/props`.
- `pkg/telemetry` added to `boundary_test.go` `firstWaveRoots`; the guard
  passes.
- `go build ./...` and the full test suite (minus the environmental
  `internal/agent` build-stamp test) pass; `golangci-lint run` is clean.
- Downstream references to `props.Event*` / `props.Delivery*` /
  `props.TelemetryConfig.DeliveryMode` still compile (alias strategy).
- Migration note and component-doc updates landed in the same change.
