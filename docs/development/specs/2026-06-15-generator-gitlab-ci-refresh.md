---
title: "Refresh the generator's GitLab CI to the phpboyscout/cicd component model"
description: "The gtb generate project GitLab skeleton still emits hand-written local job files (.gitlab/ci/{test,lint,release,pages}.yml) plus the releaser-pleaser component. go-tool-base's own .gitlab-ci.yml has since moved to the phpboyscout/cicd CI/CD components (go-lint, go-test, go-security, goreleaser, zensical-pages, renovate-self) with an MR-gate / tag-release / main-branch-releaser-pleaser / scheduled-renovate model. Refresh the scaffolded default GitLab CI to mirror that component model, decide the fate of the local job files, pin component versions and keep them current via the cicd renovate preset, and document the project prerequisites."
status: IMPLEMENTED
date: 2026-06-15
tags:
  - specification
  - generator
  - gitlab
  - ci
  - cicd
  - renovate
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Refresh the generator's GitLab CI to the phpboyscout/cicd component model

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-15

Status
:   IMPLEMENTED (2026-06-15 — scaffold now emits the phpboyscout/cicd component pipeline; the four local `.gitlab/ci/*.yml` job files removed; component versions pinned by a lockstep generator constant; `properties.ci.component_source` added to the manifest; renovate aligned to the cicd preset)

## Summary

When `gtb generate project` scaffolds a tool whose `ReleaseProvider` is GitLab,
it renders the `assets/skeleton-gitlab` tree (selected in
`internal/generator/skeleton.go` → `walkSkeletonAssets`). That tree's CI is a
hand-written, **local-job-file** pipeline: a top-level `.gitlab-ci.yml` that
`include:`s four local files —
`.gitlab/ci/{test,lint,release,pages}.yml` — plus the
`apricote/releaser-pleaser/run` component. Each local file is a bespoke job
(`unit-tests`, `golangci-lint`, `goreleaser`, `pages`) pinned to floating
`:latest` images.

go-tool-base's **own** `.gitlab-ci.yml` has since moved on. The repo-root
pipeline is now assembled almost entirely from **`gitlab.com/phpboyscout/cicd`
CI/CD components** — `go-lint`, `go-test`, `go-security`, `goreleaser`,
`zensical-pages`, and `renovate-self` — pinned to a tagged cicd release
(currently `@v0.10.5`), with the
`apricote/releaser-pleaser/run` component for the Release-MR machinery. The
quality gates live exclusively at the merge-request level; the release fan-out
(goreleaser + docs deploy) fires from the tag that releaser-pleaser cuts on
`main`; renovate runs on a schedule and keeps the component pins current via the
`gitlab>phpboyscout/cicd` renovate preset extended in `renovate.json`.

This spec proposes **refreshing the scaffolded default GitLab CI** so a newly
generated tool mirrors that component model instead of carrying the drifted
local-job-file version. It is a generator-asset and documentation change in
`internal/generator/assets/skeleton-gitlab/` only — no `pkg/` API change. It
deliberately defers the *mechanism* by which a user can replace the default CI
wholesale to the
[custom-partial-templates spec](2026-06-15-generator-custom-partial-templates.md);
**this** spec is only about what the embedded default emits.

## Motivation

The skeleton CI has drifted from the framework's own current practice, and a
scaffolding tool's default should track what the framework itself does:

- **Drift from the reference pipeline.** The framework moved its lint/test/
  security/release/pages/renovate stages onto reusable `phpboyscout/cicd`
  components; the generator still emits the older bespoke jobs. A tool scaffolded
  today gets a pipeline the framework no longer uses or maintains.
- **`:latest` image pinning.** Every local skeleton job pins a floating tag
  (`golang:latest`, `golangci/golangci-lint:latest`,
  `goreleaser/goreleaser:latest`, `python:3.x`). The component model pins exact
  component versions and lets renovate bump them deliberately — reproducible
  builds and an auditable upgrade trail instead of silent base-image drift.
- **Weaker gate semantics.** The skeleton's `goreleaser` job runs on
  `merge_request_event` *and* tags, and `pages` only on the default branch; the
  component model has a cleaner source-gated workflow (MR → gates; tag → release
  fan-out; main → releaser-pleaser; schedule → renovate) documented in the
  pipeline header. New tools should inherit the better-reasoned model.
- **Maintenance surface.** Four hand-written job files are four things to keep
  current in the generator. Delegating to versioned components shrinks the
  embedded surface to a pinned `include:` list plus a renovate preset that bumps
  it.
- **Consistency for downstream operators.** A team running several gtb-generated
  tools benefits from every tool's CI being the same small component include, not
  N divergent copies of hand-written jobs.

## Current vs target

### Current (local-job-file model)

`assets/skeleton-gitlab/.gitlab-ci.yml`:

```yaml
include:
  - local: .gitlab/ci/test.yml
  - local: .gitlab/ci/lint.yml
  - local: .gitlab/ci/release.yml
  - local: .gitlab/ci/pages.yml
  - component: $CI_SERVER_FQDN/apricote/releaser-pleaser/run@v0.8.0
    inputs:
      token: $RELEASER_PLEASER_TOKEN
      branch: main
      stage: release
stages: [test, lint, release, pages]
```

with four local files:

| File | Job | Image (floating) | Notable |
|------|-----|------------------|---------|
| `.gitlab/ci/test.yml` | `unit-tests` | `golang:latest` | race + cobertura coverage |
| `.gitlab/ci/lint.yml` | `golangci-lint` | `golangci/golangci-lint:latest` | MR + default branch |
| `.gitlab/ci/release.yml` | `goreleaser` | `goreleaser/goreleaser:latest` | runs on tag **and** MR (snapshot) |
| `.gitlab/ci/pages.yml` | `pages` | `python:3.x` | zensical build on default branch |

There is **no** security/govulncheck stage, **no** scheduled renovate job in the
pipeline (the skeleton ships a `renovate.json5` but nothing invokes renovate),
**no** source-gated `workflow:` block, and the CI files are **static**
`text/template`s (no manifest values are interpolated into them today).

### Target (cicd component model — mirroring go-tool-base's own `.gitlab-ci.yml`)

A single `.gitlab-ci.yml` whose `include:` list is the `phpboyscout/cicd`
components plus releaser-pleaser, mirroring the repo root:

| Component | Gate | Inputs the reference uses |
|-----------|------|---------------------------|
| `phpboyscout/cicd/go-lint@vX.Y.Z` | MR | (golangci-lint image bundles its own Go) |
| `phpboyscout/cicd/go-test@vX.Y.Z` | MR | `image`, `enable_e2e` |
| `phpboyscout/cicd/go-security@vX.Y.Z` | MR | `govulncheck_image` |
| `phpboyscout/cicd/goreleaser@vX.Y.Z` | tag `v*` | `gotoolchain` (override when image lags go.mod) |
| `phpboyscout/cicd/zensical-pages@vX.Y.Z` | main / tag | — |
| `apricote/releaser-pleaser/run@vA.B.C` | main | `token`, `branch`, `stage` |
| `phpboyscout/cicd/renovate-self@vX.Y.Z` | schedule | `repositories` |

plus the source-gated `workflow:` rules (MR primary; suppress duplicate branch
pipelines when an MR is open; tag → release; main → releaser-pleaser;
schedule → renovate), `GIT_DEPTH: "0"`, and a `default.tags` runner selection.

The header comment carries forward the **project-setting prerequisites**:
pipelines-must-succeed, fast-forward + squash merges, and the
`RELEASER_PLEASER_TOKEN` access-token requirement.

The net change from the operator's point of view: same four functional
gates (lint/test/release/pages) **plus** a security stage and a working
scheduled-renovate run, expressed as a short pinned component list instead of
four hand-written jobs.

> **Note — overlays out of scope for the default.** The repo-root pipeline also
> carries a `goreleaser:` overlay (AWS-KMS release signing) and an advisory
> `apidiff:` job. Those are **framework-internal** (signing infra, pre-1.0 API
> tracking) and are **not** scaffolded into a generated tool. The generated
> default is the component includes + workflow + prerequisites only — see
> [D1](#d1--the-new-scaffolded-gitlab-ciyml).

## Design decisions

### D1 — The new scaffolded `.gitlab-ci.yml`

The skeleton's `.gitlab-ci.yml` is rewritten to the component-include shape
above: the `phpboyscout/cicd` components for lint/test/security/release/pages/
renovate, the `apricote/releaser-pleaser/run` component, the source-gated
`workflow:` block, `stages`, `GIT_DEPTH: "0"`, and `default.tags`. Because the
cicd components are **public and reusable downstream** (resolved [O1](#open-questions)),
the scaffold `include`s the **absolute `gitlab.com/phpboyscout/cicd/...@vX.Y.Z`
path directly**, mirroring the framework — with the include base sourced from a
configurable `ci.component_source` (default `gitlab.com/phpboyscout/cicd`) so a
mirrored/self-hosted downstream can repoint it ([D4](#d4-templated-vs-static)).
The header comment block (gate model + project-setting prerequisites) is carried
forward, adapted to the generated tool (its own repository path in the renovate
`repositories` input — see [D4](#d4-templated-vs-static)).

The framework-internal overlays (AWS-KMS `goreleaser:` signing, advisory
`apidiff:`) are **excluded** from the scaffold. A generated tool gets a clean
component pipeline; if a downstream wants signing, that is its own opt-in (and
relates to the separate signing generator feature, not this CI refresh).

### D2 — Fate of the local `.gitlab/ci/*.yml` files

The four local job files are **removed** from the skeleton tree. With every job
now sourced from a versioned component, keeping bespoke copies would (a)
reintroduce the drift this spec exists to fix and (b) confuse operators about
which definition is authoritative.

Removing them means `walkSkeletonAssets` no longer renders those paths; the
generated tree simply has no `.gitlab/ci/` job directory. (The skeleton's
`.gitlab/CODEOWNERS`, which is templated and unrelated to the job files, is
retained.) Per [O3](#open-questions) (resolved 2026-06-15), **all four files are
removed cleanly** — no commented-out override stubs are kept, since the
[custom-partial-templates](2026-06-15-generator-custom-partial-templates.md)
mechanism is the sanctioned way to override with a bespoke job.

### D3 — Component version pinning + renovate currency

The scaffolded `.gitlab-ci.yml` pins **exact** component versions
(`@vX.Y.Z`, never a floating major) exactly as the reference does — the
releaser-pleaser component in particular "does not support floating tags." Two
things keep those pins current for the **downstream** project:

1. The scaffolded `renovate.json5` extends the **cicd preset**
   (`gitlab>phpboyscout/cicd`, as the framework's own `renovate.json` does). The
   preset carries the custom manager that recognises and auto-bumps
   `gitlab.com/phpboyscout/cicd/*@vX.Y.Z` component pins, so the downstream
   tool's renovate run proposes component bumps as MRs. **This requires the
   generated renovate config to actually extend the cicd preset and enable the
   `gitlabci`/component manager** — the current skeleton `renovate.json5` extends
   only `config:recommended` and enables `gomod` + `pre-commit`; it does **not**
   extend the cicd preset, so as written it would never bump the component pins.
   Aligning the scaffolded renovate config with the cicd-preset model is part of
   this change (see [D5](#d5-renovate-config-alignment)).
2. The `renovate-self` component is included in the pipeline (schedule-gated), so
   the downstream tool's own scheduled pipeline actually *runs* renovate — the
   skeleton today ships a config but never runs it.

**Default versions at scaffold time** and **how the generator stays in sync as
cicd releases** are resolved ([O2](#open-questions)) to the **lockstep generator
constant**: the scaffold pins the same versions the framework's `.gitlab-ci.yml`
currently uses (a constant in the generator, e.g. a `cicdComponentVersion`
defaulted to the current pin), bumped in lockstep when the framework upgrades
cicd. This couples the generator's release cadence to cicd's, which is accepted;
the scaffolded renovate then keeps the pins current for the downstream tool.

### D4 — Templated vs static

The current skeleton CI files are **static** (no `{{ }}` interpolation). The
component model needs a small amount of templating from the manifest:

- **Renovate `repositories` input** — the reference hardcodes
  `'["phpboyscout/go-tool-base"]'`; the scaffold must template the **generated
  tool's** repository path from the manifest (`Org`/`RepoName`, the GitLab group
  path), so renovate-self targets the right project.
- **releaser-pleaser `branch`** — defaults to `main`; templated only if the
  generator grows a configurable default branch (it relates to the default-branch
  decision in the [git-init spec](2026-06-15-generator-git-initialisation.md)).
- **Component source** — the include base is templated from a configurable
  `ci.component_source` (generator input / manifest value) defaulting to
  `gitlab.com/phpboyscout/cicd` and overridable, so a mirrored/self-hosted
  downstream can repoint it (resolved [O1](#open-questions)).
- **Component versions** — interpolated from the generator's pinned constant per
  [D3](#d3-component-version-pinning--renovate-currency) (the lockstep constant,
  resolved [O2](#open-questions)), rather than hardcoded in the asset, so a
  single bump updates all pins.
- **`enable_e2e`** — scaffolded **`false`** (or omitted, taking the component
  default): a freshly generated tool has no E2E suite (resolved
  [O4](#open-questions)).
- **Image / `gotoolchain` overrides** — the reference overrides the go-test/
  go-security images and the goreleaser `gotoolchain` to clear specific stdlib
  advisories and a goreleaser-image/go.mod toolchain skew. These overrides are
  **point-in-time workarounds**, not steady state. The scaffold emits **no
  overrides** and leans on the component defaults, letting renovate/cicd carry
  the toolchain rather than baking the framework's transient pins into every
  generated tool (resolved [O5](#open-questions)).

Everything else (the `workflow:` rules, `stages`, `GIT_DEPTH`, the component
list structure, releaser-pleaser `token: $RELEASER_PLEASER_TOKEN` / `stage`) is
**fixed** in the asset.

### D5 — Renovate config alignment

The scaffolded `renovate.json5` is updated to extend the cicd preset
(`gitlab>phpboyscout/cicd`) so the component-pin auto-bump manager is active, and
to set `platform: gitlab` for a GitLab-scaffolded tool. Per [O6](#open-questions)
(resolved 2026-06-15) the config is **aligned with the framework**: extend the
preset for component currency (resolvable since the components and preset are
public), keeping a **minimal gomod grouping only where it adds value**. The
`renovate.json5` already lives only in the `skeleton-gitlab` tree, so this change
is GitLab-scoped.

### D6 — Prerequisites documentation

The generated `.gitlab-ci.yml` header (and the generator's user-facing docs)
must carry forward and update the **project-setting prerequisites** the current
skeleton header already documents, matching the reference pipeline's header:

- **`RELEASER_PLEASER_TOKEN`** — a project access token with **Maintainer** role
  and `api`, `read_repository`, `write_repository` scopes, set as a CI/CD
  variable.
- **Merge method: fast-forward + squash** — so the tested commit is what lands on
  `main`.
- **Pipelines must succeed** — so the MR gates actually block merge.
- Note that the components are version-pinned and renovate keeps them current via
  the cicd preset.

This is a doc-and-comment carry-forward, not new behaviour, but it is load-bearing
for the generated pipeline to actually function.

## Open questions

1. **O1 — Downstream component visibility / source (load-bearing).** The cicd
   components live at `gitlab.com/phpboyscout/cicd/*`. A generated tool is owned
   by **some other user/group**, not phpboyscout. Are these components **public
   and reusable by any GitLab project** (so a downstream tool can `include` them
   directly by the absolute `gitlab.com/phpboyscout/cicd/...@vX.Y.Z` path), or
   are they **gtb-internal / private to phpboyscout**? This determines the
   correct scaffolded reference:
   - **Public** → scaffold the absolute `gitlab.com/phpboyscout/cicd/...` path
     directly (simplest; mirrors the framework verbatim).
   - **Private/internal** → the scaffold **cannot** reference them; options are
     a configurable **component source** (a generator input / manifest value for
     the component project path, defaulting to phpboyscout's but overridable), a
     `$CI_SERVER_FQDN`-relative path (only works if a mirror exists on the
     downstream's own instance), or **falling back to keeping the local job
     files** for downstreams that cannot reach the components.

     *This could not be confirmed from the repo.* The releaser-pleaser component
     is already referenced `$CI_SERVER_FQDN`-relative (instance-local), which
     suggests components are expected to resolve on the **downstream's own GitLab
     instance** — implying either phpboyscout/cicd must be public-and-mirrorable
     or the path must be configurable. **This is the gating decision for the
     whole spec** and must be resolved before implementation; the rest of the
     design (templated component source vs hardcoded path) hangs off it.

   *Resolved (2026-06-15):* The `gitlab.com/phpboyscout/cicd/*` components are
   **PUBLIC and fully reusable by any downstream GitLab project**
   (maintainer-confirmed). The "private/internal" branch above is therefore
   resolved to **public → absolute path**: the scaffold `include`s the absolute
   `gitlab.com/phpboyscout/cicd/...@vX.Y.Z` path directly, mirroring the
   framework. **Additionally**, the generator exposes a **configurable component
   source** — a generator input / manifest value (`ci.component_source`)
   defaulting to `gitlab.com/phpboyscout/cicd` and overridable — so a
   self-hosted or mirrored downstream can repoint the include base. The
   `$CI_SERVER_FQDN`-relative concern only governs the releaser-pleaser
   component ([O7](#open-questions)), which stays instance-local.
2. **O2 — Default pinned versions + sync mechanism.** What versions does the
   scaffold pin at generation time, and how does the generator stay current as
   cicd releases? In lockstep with the framework's own `.gitlab-ci.yml` pin (a
   generator constant bumped when the framework upgrades), templated from a
   manifest value, or resolved at scaffold time? ([D3](#d3-component-version-pinning--renovate-currency))

   *Resolved (2026-06-15):* **Lockstep generator constant.** The scaffold pins
   the same component versions the framework's own `.gitlab-ci.yml` pins (a
   generator constant bumped when the framework upgrades cicd); the scaffolded
   renovate then keeps those pins current downstream.
3. **O3 — Local job files: remove vs keep as override stubs.** Remove the four
   `.gitlab/ci/*.yml` entirely ([D2](#d2--fate-of-the-local-gitlabciyml-files)),
   or keep one or more as commented-out optional override examples? Clean removal
   is the recommendation given the custom-template override path exists.

   *Resolved (2026-06-15):* **Remove all four** local `.gitlab/ci/*.yml` files.
   Bespoke CI is covered by the custom-template overlay path
   ([custom-partial-templates](2026-06-15-generator-custom-partial-templates.md)),
   so no commented-out stubs are kept.
4. **O4 — `enable_e2e` default.** Scaffold `enable_e2e: false` / omit it (a fresh
   tool has no E2E suite), or `true` to match the framework and prompt the
   operator to add scenarios? ([D4](#d4-templated-vs-static))

   *Resolved (2026-06-15):* **`false` / omit** (take the component default). A
   freshly generated tool has no E2E suite, so E2E is not enabled by default.
5. **O5 — Image / `gotoolchain` overrides.** Scaffold **no** overrides (lean on
   component defaults, the steady-state recommendation), or carry the framework's
   current advisory/toolchain overrides into every generated tool? The overrides
   are point-in-time workarounds in the reference. ([D4](#d4-templated-vs-static))

   *Resolved (2026-06-15):* **No** image / `gotoolchain` overrides. Lean on the
   component defaults; the framework's overrides are point-in-time advisory
   workarounds and must not be baked into every generated tool.
6. **O6 — Renovate config scope.** Keep the skeleton's explicit gomod grouping
   rules alongside the cicd preset, or lean entirely on the preset as the
   framework's `renovate.json` does? And does extending the cicd preset reintroduce
   the visibility question from [O1](#open-questions) (is the *preset* itself
   `gitlab>phpboyscout/cicd` resolvable by a downstream project)?
   ([D5](#d5-renovate-config-alignment))

   *Resolved (2026-06-15):* **Align renovate with the framework** — extend the
   `gitlab>phpboyscout/cicd` preset so the component-pin auto-bump manager is
   active downstream; keep a minimal gomod grouping only where it adds value. The
   preset-resolvability twin of [O1](#open-questions) is resolved **YES**: the
   components and the preset are public, so a downstream project can extend the
   preset directly.
7. **O7 — releaser-pleaser version.** The reference pins
   `apricote/releaser-pleaser/run@v0.8.0` `$CI_SERVER_FQDN`-relative. Carry that
   exact pin forward, and keep it `$CI_SERVER_FQDN`-relative (instance-local) or
   absolute?

   *Resolved (2026-06-15):* Carry **`apricote/releaser-pleaser/run@v0.8.0`**
   forward, **`$CI_SERVER_FQDN`-relative** (instance-local), exactly as the
   framework references it.
8. **O8 — GitHub parity.** The maintainer asked specifically for GitLab. Confirm
   the GitHub skeleton CI (`skeleton-github/.github/workflows`) is a **separate
   follow-up** (or explicitly out of scope) — GitHub has no analogue to the cicd
   GitLab components, so parity would be a distinct reusable-workflow design.

   *Resolved (2026-06-15):* GitHub parity is **OUT of scope** here — a separate
   follow-up. GitHub has no cicd-component analogue; reusable-workflows would be
   a distinct design.

## Verification plan

1. **Scaffold a GitLab tool.** `just build && go run ./cmd/gtb generate project
   -p tmp` with a GitLab `ReleaseProvider`; confirm `tmp/.gitlab-ci.yml` renders
   to the component-include shape, references each expected `phpboyscout/cicd`
   component plus releaser-pleaser, carries the `workflow:` rules and the
   prerequisites header, and that `tmp/.gitlab/ci/` is absent (per
   [D2](#d2--fate-of-the-local-gitlabciyml-files)); delete `tmp/`.
2. **Templated values land correctly.** The renovate `repositories` input and any
   component-version interpolation resolve to the generated tool's values
   (org/repo path, pinned versions), with no leftover `{{ }}` or
   `phpboyscout/go-tool-base` literals. Cover GitLab nested group paths.
3. **YAML / CI validity.** The rendered `.gitlab-ci.yml` is valid YAML; where the
   tooling is available, `glab ci lint` (or the GitLab CI lint API) accepts it.
   Note that component resolution may require network/instance access, so the
   lint step must degrade gracefully (syntactic validation always; full lint when
   `glab` + connectivity are present), gated like the other integration checks.
4. **Renovate config.** The rendered `renovate.json5` extends the cicd preset and
   would bump the component pins (validate against the renovate schema; assert the
   preset extend is present).
5. **Generator unit/golden tests.** Update the generator's skeleton golden/render
   tests so the new asset content is asserted, and add a regression that the old
   `.gitlab/ci/*.yml` paths are no longer emitted.
6. **Docs.** Update `docs/components/` (generator) and any generated-CI how-to to
   describe the component model, the prerequisites, and the renovate-driven
   currency; cross-reference the reference `.gitlab-ci.yml`.

## Out of scope

- **GitHub CI** (`skeleton-github/.github/workflows`). This spec is GitLab-only;
  GitHub parity is a separate follow-up ([O8](#open-questions)) — there is no
  GitHub analogue to the cicd GitLab components.
- **The custom-template override mechanism.** Letting an operator replace the CI
  wholesale with their own template set is the
  [custom-partial-templates spec](2026-06-15-generator-custom-partial-templates.md)'s
  job; **this** spec only refreshes the embedded **default**.
- **Framework-internal overlays.** The repo-root pipeline's AWS-KMS release-signing
  `goreleaser:` overlay and advisory `apidiff:` job are not scaffolded into
  generated tools ([D1](#d1--the-new-scaffolded-gitlab-ciyml)).
- **Forge-API repository / CI-variable provisioning.** The generator does not
  create the `RELEASER_PLEASER_TOKEN` variable or configure the project's merge
  method on the operator's behalf — these are documented prerequisites
  ([D6](#d6--prerequisites-documentation)), not automated steps.
- **Changing `regenerate`/`remove` semantics.** Refreshing the embedded asset
  naturally flows into a `regenerate` re-render of the CI files; no new
  regenerate behaviour is introduced here.

## Related

- [Generator custom/extensible partial templates](2026-06-15-generator-custom-partial-templates.md)
  — the sanctioned mechanism for an operator to **override** the embedded CI with
  their own template set; this spec refreshes the **default** that that mechanism
  overrides.
- [Generator git initialisation & initial commit](2026-06-15-generator-git-initialisation.md)
  — the default-branch decision that the scaffolded releaser-pleaser `branch`
  input and the `workflow:`/`main` gating depend on.
- `.gitlab-ci.yml` (repo root) — go-tool-base's own pipeline; the **reference
  model** this refresh mirrors (component list, inputs, `workflow:` rules,
  prerequisites header).
- `renovate.json` (repo root) — the cicd-preset renovate config the scaffolded
  `renovate.json5` is aligned with ([D5](#d5-renovate-config-alignment)).
- `internal/generator/assets/skeleton-gitlab/.gitlab-ci.yml` and
  `.gitlab/ci/{test,lint,release,pages}.yml` — the local-job-file assets this
  spec rewrites/removes.
- `internal/generator/assets/skeleton-gitlab/renovate.json5` — the scaffolded
  renovate config updated to extend the cicd preset.
- `internal/generator/skeleton.go` — `walkSkeletonAssets` and the
  `ReleaseProvider`-based selection of the `skeleton-gitlab` tree; where the
  templated component-source/version values would be wired.
