---
title: "Keep the project generator's baked-in version pins current via Renovate"
description: "The gtb generate scaffold bakes tool/component version pins into places no Renovate manager reads — Go source constants (CICDComponentVersion, ReleaserPleaserComponentVersion in internal/generator/generator.go) and static skeleton assets (the pre-commit hook revs, the GitHub-workflow python-version). gtb's own renovate.json only extended the two cicd presets, so those pins froze: the cicd component pin sat at v0.10.5 while the framework itself ran v0.33.0, and the pre-commit revs sat at the exact stale values ticket #6 reported (golangci-lint v2.8.0, pre-commit-hooks v5.0.0). Every scaffold was therefore born stale until its own Renovate first ran. Bump the four stale pins to head and add Renovate customManagers (plus enable the pre-commit manager) so the template pins self-update the same way live projects do."
status: IMPLEMENTED
date: 2026-07-28
tags:
  - specification
  - generator
  - renovate
  - dependency-management
  - maintenance
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Keep the project generator's baked-in version pins current via Renovate

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-28

Status
:   IMPLEMENTED (2026-07-28). Four stale pins bumped to head; Renovate
    customManagers added to `renovate.json5` (converted from `renovate.json`)
    and the pre-commit manager enabled. Config validated against
    renovate@43.285.6; a scratch `gtb generate project` confirms all pins
    render current; generator suite green.

Related
:   [generator GitLab CI refresh](2026-06-15-generator-gitlab-ci-refresh.md) (introduced the cicd component pins this tracks),
    [config family version re-pin](2026-07-23-config-family-version-repin.md) (same failure mode — human-gated propagation stalls without a mechanism),
    ticket #6 (the pre-commit index oscillation this bump resolves at the template root)

---

## 1. Problem

`gtb generate project` bakes third-party version pins into the scaffold from
two kinds of source, and **neither is reachable by any built-in Renovate
manager**:

1. **Go source constants** in `internal/generator/generator.go`:
   - `CICDComponentVersion` — the `phpboyscout/cicd` component tag stamped into
     every scaffolded `.gitlab-ci.yml` (`go-lint`, `go-test`, `go-security`,
     `goreleaser`, `zensical-pages`, `renovate-self`).
   - `ReleaserPleaserComponentVersion` — the `apricote/releaser-pleaser`
     component/action tag.
2. **Static skeleton assets** under `internal/generator/assets/`:
   - the pre-commit hook `rev:` pins in `skeleton/.pre-commit-config.yaml`
     (golangci-lint, pre-commit-hooks);
   - the `python-version` input in the `skeleton-github` GitHub workflows.

gtb's `renovate.json` extended only `gitlab>phpboyscout/cicd:go` and
`:library`. The base preset carries a customManager that bumps
`component: gitlab.com/phpboyscout/cicd/*@vX.Y.Z` **in a literal
`.gitlab-ci.yml`** — but the generator emits that pin through a Go-template
placeholder fed by a constant, so nothing matched it. The pre-commit manager is
off by default and was never enabled here.

**Consequence.** The pins froze at whatever value they were last hand-edited to
and drifted arbitrarily far from head:

| Pin | Source | Was | Head |
|-----|--------|-----|------|
| cicd components | `generator.go` const | **v0.10.5** | v0.33.0 |
| releaser-pleaser | `generator.go` const | **v0.8.0** | v0.9.0 |
| golangci-lint (pre-commit) | `skeleton/.pre-commit-config.yaml` | **v2.8.0** | v2.12.2 |
| pre-commit-hooks | `skeleton/.pre-commit-config.yaml` | **v5.0.0** | v6.0.0 |

The last two are the exact values ticket **#6** reported as the source of its
pre-commit index oscillation — fixing them at the template closes that at the
root. The `go` version in the generated `go.mod` is **not** affected: it is
resolved dynamically from the building toolchain (`runtime.Version()`), and CI
uses `go-version: stable`, so both track head without a pin. The
`skeleton-github` workflow `uses:` pins are already kept current by Renovate's
built-in github-actions manager (it matches the nested `.github/workflows/`
path even on a GitLab-hosted repo) — the only gap there is the plain
`python-version` value, which is not a `uses:` reference.

## 2. Change

**A. Bump the four stale pins to head** (`generator.go` consts →
v0.33.0 / v0.9.0; `skeleton/.pre-commit-config.yaml` → v2.12.2 / v6.0.0) so a
scaffold generated today is current immediately, not on its first Renovate run.

**B. Make gtb's Renovate keep the template pins current.** Convert
`renovate.json` → `renovate.json5` (fleet convention; lets the intent be
commented) and:

- **Enable the pre-commit manager** (`"pre-commit": { enabled: true }`). It is
  off by default; enabling it bumps both this repo's own
  `.pre-commit-config.yaml` and the generator's skeleton copy. No
  `enabledManagers` allow-list is set, so gomod / gitlabci / github-actions
  stay active.
- **Add four `customManagers`:**
  1. the `.gitlab-ci.yml` cicd-component tracker **re-declared from the base
     preset** — Renovate *replaces* rather than concatenates the
     `customManagers` array from an extended preset, so this repo's own
     six-component pipeline would otherwise lose its tracker;
  2. `CICDComponentVersion` in `generator.go` → `gitlab-tags` `phpboyscout/cicd`;
  3. `ReleaserPleaserComponentVersion` in `generator.go` → `github-releases`
     `apricote/releaser-pleaser`;
  4. `python-version` in the `skeleton-github` workflows → `docker` `python`.

The cicd-const tracker inherits the base preset's first-party automerge rule
(`matchPackageNames: ["phpboyscout/cicd"]`), so a scaffold-pin bump auto-merges
on green exactly like the pipeline-pin bump — zero human toil to stay current,
which is the point.

## 3. Acceptance criteria

- [x] `generator.go` consts at v0.33.0 / v0.9.0; skeleton pre-commit revs at
      v2.12.2 / v6.0.0.
- [x] `renovate.json5` validates against current Renovate; the four
      customManagers present plus the re-declared cicd-pipeline tracker.
- [x] `gtb generate project` renders all four pins at head (verified on a
      scratch scaffold).
- [x] Generator test suite green; no fixture asserted an old value.
- [ ] After merge, Renovate opens bump MRs when any tracked source advances
      (observed once in the wild — e.g. the next cicd or golangci-lint release).

## 4. Notes

- **Preset drift risk.** The re-declared `.gitlab-ci.yml` tracker (customManager
  #1) mirrors `gitlab>phpboyscout/cicd` `default.json`. If that preset's tracker
  regex changes, this copy must follow. It is a small, stable, core pattern; the
  comment in `renovate.json5` flags the dependency.
- **Why not read the version from a tracked file instead of a const?** A larger
  refactor (source the cicd tag from gtb's own `.gitlab-ci.yml` at generation
  time) would remove the const entirely, but couples generation to a parsed file
  and is out of scope here. The customManager keeps the const honest at far lower
  cost.
