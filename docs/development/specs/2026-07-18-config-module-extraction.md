---
title: "Extract pkg/config into a standalone go/config module"
description: "Extract the Viper-based configuration container (hierarchical merge, hot-reload Observable, typed sections, struct validation, the Containable/Container/Observable API) out of go-tool-base into gitlab.com/phpboyscout/go/config. The core is already framework-free and slog-first (its only logging seam is a nil-safe *slog.Logger and it has zero go-tool-base imports), so this is a clean-break git-mv extraction with a direct caller repoint — but the blast radius is large (~19 internal consumer packages) and, unlike every prior extraction, this module legitimately carries the full Viper stack. Extracting config is the prerequisite for a clean go/vcs / go/releases."
date: 2026-07-18
status: IMPLEMENTED
tags:
  - specification
  - config
  - viper
  - module-extraction
  - reuse
  - foundational
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/config` into a standalone `go/config` module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-18

Status
:   IMPLEMENTED (2026-07-18 — go/config v0.1.0 published; GTB cut over)

Related
:   [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5 procedure, §10/§11 findings),
    [credentials extraction](2026-07-18-credentials-module-extraction.md) (the immediately-prior clean-break repoint, IMPLEMENTED),
    [config-section-adapters spec](2026-07-07-config-section-adapters-for-extraction.md) (the decoupling groundwork, IMPLEMENTED),
    [extraction report](../reports/2026-07-07-package-extraction-report.md)

---

## Summary

`pkg/config` is GTB's configuration backbone: a Viper wrapper providing a
hierarchical-merge `Container`, the `Containable` accessor interface, hot-reload via
the `Observable` interface, typed configuration sections (`Section[T]` /
`ObservedSection[T]` with `UnmarshalKey`/observe semantics), and struct-tag
validation (`Schema` / `ValidateStruct`). Nearly every GTB subsystem reads its
configuration through `config.Containable`.

This spec extracts it to `gitlab.com/phpboyscout/go/config` following the
[module-extraction playbook](2026-07-12-go-module-extraction-playbook.md). Like
`credentials`/`authn`/`controls`, the **core is already framework-free and
slog-first**: production `pkg/config` imports **zero** go-tool-base packages, and its
only logging seam is a nil-safe `*slog.Logger` (default `slog.New(slog.DiscardHandler)`).
There is no `Props`, `Assets`, or `pkg/logger` coupling — configuration files are
read through an injected `afero.Fs`. This is therefore a **clean-break, git-mv
extraction with a direct caller repoint** — no facade, no decoupling work.

Two things make it different from every prior extraction, and both are the point of
this spec:

1. **Blast radius.** ~19 internal consumer packages repoint in a single cut-over —
   the largest so far — including `pkg/props` (the DI core) and the `*_config_adapter.go`
   files that stayed in GTB during the transport extractions.
2. **It is the viper-ful module.** Every prior module's `depfootprint_test.go`
   *forbids* Viper/pflag/afero. `go/config` is the one module that legitimately
   *carries* the full Viper stack — it is the wrapper. Its guard forbids the
   framework/TUI/OTel/cloud set but explicitly allows the Viper ecosystem.

## Motivation

- **Unblocks the VCS/release stack.** `pkg/vcs` (and its `config_adapter.go`) reads
  config through `Containable`. A clean `go/vcs` / `go/releases` — the highest
  aggregate-value extraction remaining — cannot be framework-free until `config` is
  a module. Config is the gating prerequisite.
- **Foundational reuse.** A hardened Viper container with hot-reload, typed sections,
  and struct validation is broadly useful to any Go service, independent of GTB.
- **Completes the adapter story.** The transport extractions deliberately left the
  `config.Containable → typed struct` adapters in GTB. Making `config` a module turns
  those adapters into ordinary `go/config` consumers, finishing the separation the
  config-section-adapters work began.

## Target module

- **Module path:** `gitlab.com/phpboyscout/go/config` (bare name, `go` subgroup).
- **Package name:** stays `config`.
- **Docs microsite:** `config.go.phpboyscout.uk` (core module ⇒ full Diátaxis site).
- **Local dir:** `~/workspace/phpboyscout/go/config`.

## Current structure & coupling

Production source ~2146 LOC (5660 incl. tests). Core files:

| File | LOC | Role |
|---|---|---|
| `container.go` | 641 | `Container` + `Containable` interface + hot-reload watcher |
| `unmarshal.go` | 279 | typed `Section[T]` unmarshal/bind |
| `observed_section.go` | 266 | `ObservedSection[T]` hot-reload sections |
| `validate.go` | 238 | `Schema`/`FieldSchema` struct validation |
| `config.go` | 219 | `LoadFilesContainer*`, `NewContainerFromViper`, resolver viper |
| `schema.go` | 176 | schema types + `ValidationResult` |
| `load.go` | 112 | env/embedded loaders (`LoadEnv`) |
| `options.go` | 102 | `ContainerOption`, `WithLogger`, … |
| `validate_generic.go` | 64 | generic `ValidateStruct[T]` |
| `observer.go` / `doc.go` | 19 / 30 | `Observable` interface, package doc |

**External dependencies:** the Viper stack — `github.com/spf13/viper`, `spf13/afero`,
`spf13/pflag`, `spf13/cast`, `github.com/fsnotify/fsnotify`,
`github.com/go-viper/mapstructure/v2`, `github.com/subosito/gotenv` — plus
`github.com/cockroachdb/errors`. Logging seam: `*slog.Logger`. **Zero go-tool-base
imports in production code** (verified).

**Public API surface:** `Container`, `Containable` (~28 methods), `Observable`,
`Observer`, `Section[T]`, `ObservedSection[T]`, `SectionChange[T]`,
`SectionBindingConfig[T]`/`SectionBindingOption[T]`, `Schema`, `FieldSchema`,
`SchemaOption`, `ValidationError`, `ValidationResult`, `ValidateStruct[T]`,
`ContainerOption`, `WithLogger`, `LoadFilesContainer`/`LoadFilesContainerWithSchema`/
`NewFilesContainer`, `NewContainerFromViper`, `LoadEnv`, `ErrNoFilesFound`,
`DefaultReloadDebounce`. The API is unchanged by this extraction.

### Consumers to repoint (~19 packages)

`pkg/props` (+`propstest`), `pkg/cmd/root`, `pkg/cmd/config`, `pkg/chat`,
`pkg/tls`, `pkg/http`, `pkg/grpc`, `pkg/gateway`, `pkg/vcs` (+`repo`),
`pkg/telemetry`, `pkg/setup` (+`ai`/`github`/`bitbucket`/`telemetry`),
`internal/generator` (+`templates`). Plus the generated mocks under
`mocks/pkg/{config,props,setup}`. The generator **templates** emit `config` imports,
so the scaffold-template strings repoint too (scaffolded projects then `go mod tidy`
in `go/config`, as they already do for `go/chat`/`go/credentials`).

## Seams and decoupling

**None required.** `pkg/config` is already slog-first (nil-safe `*slog.Logger`),
takes an injected `afero.Fs` for all file access (no embedded-asset or `Props`
coupling), and owns typed config structs rather than reaching into GTB. This is the
cleanest possible core — the work is entirely in the *repoint*, not decoupling.

The one guard that differs from every prior module is the **depfootprint** (see
Open question 2): `go/config`'s guard forbids `go-tool-base`, `charmbracelet`/
`charm.land`, OpenTelemetry, and cloud SDKs — but **allows** `spf13/viper`,
`spf13/afero`, `spf13/pflag`, `spf13/cast`, `fsnotify`, `go-viper/mapstructure`, and
`subosito/gotenv`, which are intrinsic to a Viper wrapper.

## Migration procedure

Follows playbook §5 in the clean-repoint shape (`credentials`/`authn` precedent):

1. **Bootstrap** `phpboyscout/go/config` to playbook §3 (cicd v0.23.0 components,
   `.golangci.yaml` local-prefix, the viper-allowing `depfootprint_test.go`,
   `requirements-lock.txt`, `enable_e2e: false`, README header). No goreleaser.
2. **Move code** — the 11 production files above + their pure tests. No import
   rewrites beyond the module path; no branded literals to change.
3. **Tests + guards** — carry all `*_test.go` (they are pure: verify none import
   other GTB packages before moving — `internal/testutil` usages must be reclassified
   per playbook §11.2), keep **≥90% coverage**, add the viper-allowing depfootprint
   guard. Mocks: see Open question 3.
4. **Docs (Diátaxis) + Pages** — author `config.go.phpboyscout.uk`: getting-started,
   how-tos (load & merge config, typed sections, hot-reload with `Observable`, struct
   validation, bind CLI flags), an explanation of the precedence model + hot-reload
   safety (last-known-good on validation failure). Reference → pkg.go.dev.
5. **Cut `v0.1.0`** via the Release-MR flow (human-gated).
6. **GTB cut-over (single MR):** add the dependency; **delete `pkg/config`**; repoint
   all ~19 consumers + the generator template strings **with no aliases**; regenerate
   `mocks/pkg/config` from `go/config` (Open question 3); run `just ci`; add a
   `docs/reference/migration/v0.x-config-extracted.md` note.
7. **GTB docs cross-reference** the microsite (component page → stub; sweep `docs/`
   for the old path per §11.6).

## Test redistribution

The `*_test.go` files are expected to be pure (package-under-test + stdlib +
testify/viper/afero). `config_integration_test.go` and any test importing
`internal/testutil` must be classified per playbook §11.2 — a pure test moves to the
module (vendoring any `internal/testutil` helper it needs), a cross-GTB-coupled one
stays in GTB. Build the master `Test*`/`Example*` inventory and diff it after the
move so nothing silently vanishes (§10.3).

## Acceptance criteria

- `go/config` resolves and builds under the new path; `depfootprint_test.go` green,
  asserting no go-tool-base/TUI/OTel/cloud deps while allowing the Viper stack.
- Module coverage ≥90%; docs live at `config.go.phpboyscout.uk` with a working cert.
- GTB builds and `just ci` is green consuming `go/config@v0.1.0`; `pkg/config` is
  deleted; `mocks/pkg/config` regenerated from the module; scaffolded projects
  (`gtb generate`) import `go/config` and build.
- Migration note added; component page cross-references the microsite.

## Resolutions (confirmed with user 2026-07-18)

- **R1 — clean break, no facade.** Repoint every consumer directly to `go/config`;
  delete `pkg/config`. Matches `credentials`/`authn`.
- **R2 — depfootprint: no Viper restriction.** `go/config` is the module the
  viper-forbidding rule was *created for*; once Viper lives here that rule is defunct
  for this module. The guard forbids only `go-tool-base`, `charmbracelet`/`charm.land`,
  OpenTelemetry, and cloud SDKs — it makes **no assertion about the Viper stack**,
  which is the module's reason to exist.
- **R3 — ship importable mocks with the module.** The module **publishes** a mock
  package (mockery-generated mocks of `Containable`/`Observable`/`EmbeddedFileReader`)
  as part of `go/config`, and GTB's tests **consume** it. GTB's `mocks/pkg/config` is
  **deleted** and its test call sites repoint to the module's mock package. This is a
  first for the toolkit (prior modules kept mocks `_test`-internal); it means
  `testify/mock` + `objx` enter `go/config`'s module graph — acceptable, and the
  depfootprint guard is scoped to the core packages so the mock subpackage's testify
  dependency is not flagged.
- **R4 — one-shot cut-over.** All ~19 consumer packages in a single clean-break MR;
  the repoint is a mechanical, unchanged-API substitution, so staging would only add
  a long-lived half-migrated tree.
- **R5 — carry `Load` over (it is generic).** `Load`/`LoadEnv`/`LoadEmbed` are all
  generic (paths + injected `afero.Fs`/stdlib `fs.FS` + options); nothing GTB-specific
  is in their logic. The only GTB-ism is the `ErrNoFilesFound` message wording ("run
  init … `--config` flag"), which is **de-branded** to tool-agnostic phrasing during
  the move (cf. the credentials probe de-brand). Nothing else stays in GTB.

## Open questions

_All resolved — see Resolutions above._

## Status

IMPLEMENTED (2026-07-18). All open questions resolved (R1–R5). Delivered:
`go/config v0.1.0` published (repo, DNS/Pages at `config.go.phpboyscout.uk`, landing
card, release) with the published `configmock` package (R3), the de-branded
`ErrNoFilesFound` message (R5), and a depfootprint guard that makes no Viper assertion
(R2). GTB cut over clean-break in one shot (R4): `pkg/config` and `mocks/pkg/config`
deleted, ~85 files and 26 mock consumers repointed, generator templates updated, the
six-page component subsection collapsed to a GTB-integration page pointing at the
microsite, and a [migration note](../../reference/migration/v0.x-config-extracted.md)
added.

A stale claim in `docs/how-to/validate-component-config.md` — that `Validate` is
deliberately kept off the `Containable` interface — was corrected during the cut-over;
it had been superseded by the
[generic-validation spec](2026-05-29-config-generic-validation.md) and contradicted the
code.
