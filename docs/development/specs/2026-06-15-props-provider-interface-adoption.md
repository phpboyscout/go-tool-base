---
title: "Provider-interface adoption — make GTB eat its own dogfood"
description: "pkg/props defines nine narrow provider interfaces (LoggerProvider, ConfigProvider, FileSystemProvider, …) so a leaf function can declare only the dependencies it needs, but nothing internal consumes them — every signature takes the whole *props.Props. This spec analyses the impact of adopting the interfaces in leaf-package signatures (backward-compatible, since *Props satisfies each) and proposes a phased rollout behind the advisory apidiff job, or the alternative of documenting them as downstream-convenience-only."
status: IMPLEMENTED
date: 2026-06-15
tags:
  - specification
  - props
  - architecture
  - idiomatic-go
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Provider-interface adoption

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-15

Status
:   IMPLEMENTED (open questions resolved in review 2026-06-15)

## Summary

`pkg/props` defines **nine narrow provider interfaces** so a leaf package or function can declare *only* the capabilities it actually needs (the interface-segregation principle), with `*props.Props` satisfying every one of them via compile-time checks. The documented intent — see the `Props` doc comment ("consider whether a narrow interface … would suffice") and the architecture section of `CLAUDE.md` — is that consumers narrow their dependency surface. In practice **nothing internal does**: every non-generated signature in the codebase takes the concrete `*props.Props`. The provider interfaces are dead API, and the codebase does not eat its own dogfood.

This spec quantifies the adoption impact across the codebase and proposes how to close the drift: either **(A)** adopt the interfaces in leaf-package signatures, phased package-by-package behind the advisory `apidiff` job, or **(B)** keep `*Props` everywhere and re-document the interfaces as a downstream-convenience surface only. The recommendation, the threshold rules, and the genuine open decisions are laid out for maintainer sign-off.

Finding addressed (from `docs/development/reports/codebase-audit-2026-06-12.md`, tabled in §3.5 Improvements, detailed in §3.4 Idiomatic Go):

- `props-provider-interfaces-unused` — **Medium, idiomatic-go / documented-architecture drift**

## Motivation

GTB sells downstream authors on the narrow-interface pattern: a function that only logs should take a `LoggerProvider`, not the whole container, so its dependency contract is honest and it is trivially testable with a one-method fake. The framework defines the interfaces, wires the compile-time satisfaction checks, and documents the convention — then ignores it everywhere. That is a credibility gap: the pattern we recommend to downstreams is one we do not follow ourselves.

Adopting the interfaces internally would (1) make each function's dependency contract self-documenting (the signature states exactly what it touches), (2) improve unit-test ergonomics (narrow fakes instead of a fully-wired Props), and (3) turn the dead interfaces into living, exercised API that downstreams can trust. The cost is a wide-but-mechanical signature churn and a long list of advisory `apidiff` entries — which this spec scopes precisely so the maintainer can weigh it.

## Impact analysis

### The interfaces that exist today

`pkg/props/interfaces.go` defines nine interfaces — seven single-capability and two composites — each backed by a getter method on `*Props` and a compile-time satisfaction check:

| Interface | Method(s) | Backing `Props` field |
|---|---|---|
| `LoggerProvider` | `GetLogger() logger.Logger` | `Logger` |
| `ConfigProvider` | `GetConfig() config.Containable` | `Config` |
| `FileSystemProvider` | `GetFS() afero.Fs` | `FS` |
| `AssetProvider` | `GetAssets() Assets` | `Assets` |
| `VersionProvider` | `GetVersion() version.Version` | `Version` |
| `ErrorHandlerProvider` | `GetErrorHandler() errorhandling.ErrorHandler` | `ErrorHandler` |
| `ToolMetadataProvider` | `GetTool() Tool` | `Tool` |
| `LoggingConfigProvider` | `LoggerProvider` + `ConfigProvider` | (composite) |
| `CoreProvider` | `LoggerProvider` + `ConfigProvider` + `FileSystemProvider` | (composite) |

Two observations from the source:

1. **The getters exist but the fields are exported.** Internal code reaches the dependency directly as `p.Logger`, `p.Config`, `p.FS`, … — *never* through `p.GetLogger()` etc. Adopting a narrow-interface parameter therefore also forces those call sites to switch from field access to the getter method (the interface exposes only the method, not the field). This is part of the churn, not a free rename.
2. **There is no provider for `Collector`.** `Props` has eight fields (`Tool`, `Logger`, `Config`, `Assets`, `FS`, `Version`, `ErrorHandler`, `Collector`), but only seven have a getter/provider — `TelemetryCollector` has neither. Any site that needs *only* the collector (we found two) cannot narrow today without first adding a `TelemetryProvider` interface + `GetCollector()` getter.

### How many sites take `*props.Props`

Counting non-test, non-mock references:

- **179** total `props.Props` token occurrences across the tree (`grep -rn "props\.Props" --include=*.go | grep -v _test.go | grep -v /mocks/`).
- **~139** of those are in `func` signatures (parameters or constructors).
- Generated/template noise: a large slice lives in `internal/generator/templates/` and `internal/generator/stubs.go` as *string literals* that emit `*props.Props` into scaffolded downstream code — these are not real signatures and are **out of scope** for narrowing (changing them would alter what we scaffold for users; see Out of scope).

Excluding tests, mocks, and the generator's emitted-code string literals leaves roughly **45 real source files** with a live `*props.Props` parameter or field. Distribution by package (files referencing `props.Props`, non-test, non-mock):

| Package | Files |
|---|---|
| `pkg/chat` | 5 |
| `internal/cmd/generate` | 5 |
| `pkg/setup` (+ `ai`/`github`/`bitbucket`/`telemetry` subpkgs) | ~9 |
| `internal/generator` (+ `verifier`/`templates`) | ~6 |
| `internal/cmd/keys` | 4 |
| `internal/cmd/regenerate` | 3 |
| `pkg/cmd/docs` | 3 |
| `pkg/telemetry` (+ `pkg/cmd/telemetry`) | 4 |
| `pkg/vcs/repo` | 2 |
| `internal/cmd/{enable,remove,sign,disable,root}` | ~7 |
| `cmd/e2e` | 2 |
| others (`pkg/docs`, `pkg/cmd/changelog`, `internal/cmd/resolve.go`) | 3 |

### Which sites actually use a narrow slice

For every non-generated file with a `*props.Props` parameter, we enumerated the distinct `Props` fields actually accessed (`p.Logger`, `p.Config`, `p.FS`, …). The result categorises cleanly:

**Category (a) — single-provider candidates (uses exactly one field).** Strong candidates for a one-method interface:

| Site(s) | Field used | Narrow type |
|---|---|---|
| `pkg/telemetry/datadir.go` (`ResolveDataDir`, `configDataDir`) | `Config` | `ConfigProvider` |
| `pkg/docs/ask.go` (`ResolveProvider`) | `Config` | `ConfigProvider` |
| `cmd/e2e/config_probe.go` | `Config` | `ConfigProvider` |
| `pkg/chat/claude_local.go` (`newClaudeLocal`) | `Logger` | `LoggerProvider` |
| `internal/cmd/disable/disable.go` | `Logger` | `LoggerProvider` |
| `internal/cmd/generate/command.go` | `Logger` | `LoggerProvider` |
| `internal/cmd/keys/{generate,mint,wkd}.go` | `Logger` | `LoggerProvider` |
| `internal/cmd/sign/sign.go` | `Logger` | `LoggerProvider` |
| `pkg/setup/telemetry/telemetry.go` | `Tool` | `ToolMetadataProvider` |
| `pkg/setup/middleware_builtin.go` | `Collector` | (needs new `TelemetryProvider`) |

→ **~12 files**, several with multiple functions, are single-field. The `Logger`-only and `Config`-only clusters dominate.

**Category (b) — two/three-provider candidates.** Map cleanly onto the existing composites or a small multi-param signature:

| Site(s) | Fields | Fits |
|---|---|---|
| `pkg/chat/{claude,client,gemini,openai}.go`, `internal/cmd/enable/signing.go` | `Config`+`Logger` | `LoggingConfigProvider` |
| `internal/cmd/resolve.go`, `internal/cmd/generate/flag.go`, `internal/generator/ast_extract.go`, `internal/generator/verifier/{agent,legacy}.go` | `FS`+`Logger` | two params, or a `CoreProvider` minus config |
| `pkg/cmd/changelog/changelog.go` | `Assets`+`Logger` | two params |
| `pkg/cmd/docs/docs.go`, `pkg/props/propstest` | `Assets`+`Tool` | two params |
| `internal/cmd/root/root.go` | `ErrorHandler`+`Tool` | two params |
| `pkg/setup/init.go` | `Assets`+`FS`+`Logger` | three params, or `CoreProvider`-ish |
| `internal/generator/commands.go` | `Config`+`FS`+`Logger` | `CoreProvider` |
| `pkg/cmd/telemetry/checks.go` | `Collector`+`Config`+`Tool` | needs new `TelemetryProvider` |

→ **~14 files**. The `Config`+`Logger` cluster is the single biggest win and maps onto the *already-defined* `LoggingConfigProvider`.

**Category (c) — genuinely need most of Props (leave as `*Props`).** Four-or-more fields, or constructors that fan dependencies into many sub-components:

| Site | Fields |
|---|---|
| `pkg/vcs/repo/repo.go` (`NewRepo`, auth helpers) | `Config`+`FS`+`Logger`+`Tool` |
| `pkg/setup/update.go` | `Config`+`FS`+`Logger`+`Tool`+`Version` |
| `pkg/telemetry/observability.go` (`Setup`) | `Config`+`Logger`+`Tool`+`Version` |
| `pkg/cmd/telemetry/telemetry.go` | `Collector`+`Config`+`FS`+`Logger`+`Tool` |
| `pkg/setup/ai/ai.go` | `Assets`+`Config`+`FS`+`Logger`+`Tool` |
| `pkg/setup/github/{github,ssh}.go`, `pkg/setup/bitbucket/bitbucket.go` | `FS`+`Logger`+`Tool` (±`Assets`) |
| `internal/generator/generator.go` (`New`) | `FS`+`Logger`+`Version` |

→ **~9 files** stay on `*Props` (or, at most, a 3-param signature where it reads cleanly).

**Headline split:** of the ~35 narrowing-eligible source files, roughly **12 are single-provider**, **14 are two/three-provider**, and **9 genuinely need `*Props`**. Roughly **three-quarters of eligible sites could narrow** — a large but bounded surface.

### Backward-compatibility and `apidiff` assessment

This is the load-bearing risk, and it must be stated honestly:

- **Source-compatible for `*Props` callers.** Because `*props.Props` satisfies every provider interface, changing a parameter from `*props.Props` to `props.LoggerProvider` (etc.) keeps compiling for every caller that passes a `*Props` — which is *all* internal callers and the overwhelming majority of downstream ones. No caller edits are required at the pass-the-argument site.
- **It is still an exported-signature change.** `apidiff` will report each narrowed exported function/constructor as an incompatible signature change, because the parameter's static type changed. With ~35 files and many of them exposing multiple exported symbols, this is **dozens of advisory `apidiff` entries**. Per the pre-1.0 policy (`CLAUDE.md` → API Stability), the `apidiff` CI job is **advisory / `allow_failure: true`**, so this does not block — but it produces a large, noisy diff a reviewer must scan and confirm as intentional. Concentrating the churn per-package per-MR keeps each diff reviewable.
- **A genuine break exists for non-`*Props` callers.** Any downstream that passed something *other* than a `*Props` (e.g. their own concrete type, or — for a return-value/struct-field change — code that relied on the concrete type) would break. For *parameters* this is unlikely (downstreams pass the Props they were handed). For **struct fields and return types** the risk is higher and the benefit lower — narrowing a stored field changes the struct's exported shape and can ripple. Recommendation: **narrow parameters of free functions and methods first; leave exported struct fields and return types as `*Props`** unless there is a specific reason.
- **Pre-1.0 lever.** Even in the worst case this ships as a **minor** bump, not a major — the project explicitly permits API churn pre-1.0. So the back-compat question is really "is the advisory-diff noise and the field→getter churn worth the dogfooding payoff?", not "can we afford a break?".

### Test impact

Net positive, and de-risked by recent work. `pkg/props/propstest.New` now returns a fully-wired `*props.Props` that satisfies every provider interface, so existing tests that build a Props and call a narrowed function **keep working unchanged** (they just pass their `*Props` where a `LoggerProvider` is now expected). New or refactored tests for narrowed functions gain the option of a one-method fake instead of a full Props — better isolation, no over-wiring. There is no test-rewrite tax for adoption; the upside is opt-in.

## Design decisions

> These are **proposals for review**, not yet ratified. Each is paired with an open question where the maintainer's call is genuinely needed.

### D1 — Adopt in leaf packages; recommend Option A over Option B

Prefer **adopting** the interfaces (Option A) over re-documenting them as downstream-only (Option B). The interfaces already exist with compile-time checks and doc-comment intent; deleting that intent (Option B) is the lower-effort path but cements the dogfooding gap. Adoption is mechanical and pre-1.0-cheap. (See O1.)

### D2 — A ≤2-field threshold for narrowing

Narrow a signature to a provider interface (or to ≤2 narrow params) **only when the function body touches ≤2 of the Props fields**. At 3 fields, narrow *only* if a defined composite (`CoreProvider`) fits exactly; otherwise keep `*Props`. At 4+ fields, always keep `*Props`. This keeps signatures short and avoids the "five narrow params" anti-pattern that is strictly worse than the container. (See O2.)

### D3 — Composite vs multiple params for the 2-field case

For the dominant `Config`+`Logger` cluster, use the existing **`LoggingConfigProvider`** composite (one named type reads better than two params and is already defined). For other 2-field combinations with no matching composite (e.g. `FS`+`Logger`, `Assets`+`Tool`), prefer **two narrow params** over minting a new composite per combination — proliferating composites is its own smell. (See O2, O4.)

### D4 — Free functions and methods first; constructors and struct fields last

Narrow **free functions and methods** first (lowest blast radius — parameters only). Treat **constructors** as category-(c)-leaning: a constructor that fans a Props into several sub-components legitimately needs many fields and should usually keep `*Props`; narrow a constructor only if it genuinely uses ≤2 fields. **Do not narrow exported struct fields or return types** in this effort. (See O3.)

### D5 — Add a `TelemetryProvider` if (and only if) we narrow the collector-only sites

Two sites use only `Collector`. To narrow them we add a `TelemetryProvider` interface (`GetCollector() TelemetryCollector`), a `GetCollector` getter on `*Props`, and a satisfaction check — a small, additive, non-breaking change. Resolved: this is **in scope** (O5) — the collector-only sites are narrowed and the missing provider is added for consistency with the other seven fields. (See O5.)

### D6 — Phased, package-by-package, behind the advisory apidiff job

Roll out one package per MR, lowest-risk first, so each `apidiff` advisory diff is small and reviewable. Do **not** do a single tree-wide MR. (See Phased rollout plan.)

## Phased rollout plan

Each phase is an independent MR; lint + tests + the advisory `apidiff` job run per MR. Ordered lowest-risk → highest, fully abortable after any phase.

1. **Phase 0 — groundwork.** Add `TelemetryProvider` + `GetCollector()` + satisfaction check (D5, resolved in scope per O5). Pure addition, zero diff to existing signatures.
2. **Phase 1 — `Logger`-only cluster** (`internal/cmd/{disable,generate,keys,sign}`, `pkg/chat/claude_local.go`). All single-field `LoggerProvider`. Lowest risk, biggest count, establishes the field→getter pattern.
3. **Phase 2 — `Config`-only cluster** (`pkg/telemetry/datadir.go`, `pkg/docs/ask.go`). Single-field `ConfigProvider`.
4. **Phase 3 — `Config`+`Logger` cluster** (`pkg/chat/{claude,client,gemini,openai}.go`, `internal/cmd/enable/signing.go`) via `LoggingConfigProvider`. The single biggest readability win.
5. **Phase 4 — `FS`+`Logger` and other 2-field clusters** (`internal/cmd/resolve.go`, `internal/generator/{ast_extract,verifier}`, `internal/cmd/generate/flag.go`, `pkg/cmd/changelog`, `pkg/cmd/docs/docs.go`, `internal/cmd/root/root.go`).
6. **Phase 5 — 3-field composite fits** (`internal/generator/commands.go` via `CoreProvider`; `pkg/setup/init.go`, `pkg/cmd/telemetry/checks.go` if a composite reads cleanly).
7. **Phase 6 — docs.** Update `docs/concepts/`/`docs/components/` to show the now-live convention. The **generator templates** keep emitting `*props.Props` for *new* scaffolded commands (O8 resolved — Out of scope; see below).

Category-(c) packages (`pkg/vcs/repo`, `pkg/setup/update.go`, `pkg/telemetry/observability.go`, `pkg/setup/ai`, generator `New`) are explicitly **not** in the plan; they keep `*Props`.

## Open questions

1. **O1 — Adopt or document-only?** Option A (adopt the interfaces in leaf signatures — recommended) vs Option B (keep `*Props` everywhere and re-document the interfaces as a downstream-convenience surface only, closing the finding by amending the docs). *Drafting recommendation: A.* This is the one decision that gates the entire spec. *Resolved (2026-06-15):* **Option A — ADOPT** the provider interfaces internally (dogfood them). Not document-only.
2. **O2 — The narrowing threshold.** Is "narrow only when the body uses ≤2 fields" the right line? Or stricter (single-field only) / looser (≤3 with a composite)? (D2) *Resolved (2026-06-15):* narrow a function/method to provider-interface params **only when it uses ≤2 Props fields**; if it needs >2, keep `*props.Props`.
3. **O3 — Constructors.** Should constructors ever narrow, or is `*Props` the standing rule for any `New*`? And confirm exported struct fields / return types stay `*Props` (recommended). (D4) *Resolved (2026-06-15):* constructors stay on `*props.Props` (they build many-field state); narrow only free functions and methods. Exported struct fields and return types stay `*Props`.
4. **O4 — Composites policy.** Use the two existing composites only, or are we willing to add more (and where's the line before composite-proliferation is worse than two params)? (D3) *Resolved (2026-06-15):* reuse the existing composites (`LoggingConfigProvider`, `CoreProvider`) only; do **not** mint a new composite per combination — anything needing >2 providers stays `*Props` (consistent with O2).
5. **O5 — `TelemetryProvider`.** Add the missing collector provider (and getter) so collector-only sites can narrow, or leave them on `*Props`? (D5) *Resolved (2026-06-15):* **ADD** a new `TelemetryProvider` interface (a getter for the `Collector`) so collector-only sites can narrow too — closing the one field that lacks a provider, for consistency. Add a `GetCollector()`-equivalent getter to `Props` if one does not already exist, as part of implementation.
6. **O6 — Field access vs getters.** Adoption forces narrowed sites from `p.Logger` to `p.GetLogger()`. Are we comfortable with the mixed style during rollout (some files getter-based, some field-based), or do we want a convention note so it doesn't read as accidental inconsistency? *Resolved (2026-06-15):* narrowed code accesses dependencies via the getters (`p.GetLogger()`, etc.), not the exported fields, since the interfaces expose getters.
7. **O7 — How far?** Full rollout through Phase 5, or stop after the high-value Logger/Config phases (1–3) and leave the rest as "narrow opportunistically when touched"? The advisory-diff noise scales with how far we push. *Resolved (2026-06-15):* be **comprehensive but pragmatic** — adopt across **all** eligible sites (not just a Logger/Config first phase then stop), bounded by the O2 ≤2-field threshold. The phased per-package rollout stays for safety, but the end-state target is full adoption of every eligible site.
8. **O8 — Generator templates.** Should newly *scaffolded* commands be generated with narrowed signatures (teaching downstreams the pattern by example), or keep emitting `*props.Props` for simplicity? (Currently Out of scope.) *Resolved (2026-06-15):* generated/scaffolded command output stays on `*props.Props` (keeps downstream-author ergonomics simple); the narrowing is an internal-GTB dogfooding exercise, **not** pushed into the generator templates.

## Verification plan

1. **Compiles unchanged for callers** — after each phase, the whole tree builds with no caller-site edits at the pass-the-argument points (proving the `*Props`-satisfies-interface back-compat claim).
2. **Tests green, unchanged** — existing tests using `propstest.New` (returning `*Props`) pass against the narrowed signatures without modification.
3. **`apidiff` advisory reviewed** — each MR's `apidiff` job output is read and confirmed as *only* the intended parameter-type narrowings, no unintended breaks (e.g. an accidental return-type or struct-field change).
4. **Narrow-fake demonstration** — at least one narrowed function gains a unit test driven by a one-method fake (not a full Props) to prove the testability payoff is real.
5. **Lint** — `golangci-lint` clean; the now-consumed interfaces remove any dead-code/`deadcode` signal on the provider types.
6. **Docs accuracy** — `docs/concepts/`/`docs/components/` examples match real adopted signatures (cross-referenced to code per the After-Implementation rule).

## Out of scope

- **Generator template / scaffold output.** The `*props.Props` strings emitted by `internal/generator/templates/` and `stubs.go` into downstream scaffolds stay as-is (O8 resolved: keep emitting `*props.Props`); changing them alters what every new tool is generated with.
- **Category-(c) packages.** `pkg/vcs/repo`, `pkg/setup/update.go`, `pkg/telemetry/observability.go`, `pkg/setup/ai`, and the generator `New` constructor keep `*Props`.
- **Narrowing exported struct fields and return types.** Parameters only (D4).
- **Removing the exported `Props` fields in favour of getters.** The fields stay public; this spec only adds getter *usage* at narrowed sites.
- **Any `RepoLike` interface split** (`repolike-kitchen-sink-weak-typing`) — that is a separate Phase 14 spec.

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.5 Improvements (finding tabled) / §3.4 Idiomatic Go (finding detail) — `props-provider-interfaces-unused`
- [Plan 3 — improvements](../plans/2026-06-12-audit-plan-3-improvements.md) Phase 15 — `pkg/props` provider-interface adoption
- `pkg/props/interfaces.go`, `pkg/props/props.go`, `pkg/props/propstest`
- `CLAUDE.md` → API Stability (pre-1.0) — the advisory, non-blocking `apidiff` job
- `docs/about/api-stability.md`
