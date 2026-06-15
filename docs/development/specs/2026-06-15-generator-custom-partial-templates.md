---
title: "Generator custom/extensible partial templates (local folder or git repo, ref-pinned, manifest-tracked)"
description: "The generator renders only its embedded skeleton; users cannot extend it with their own templates. Add user-supplied custom partial template sets — sourced from a local folder or a git repo (public over https/go-git, private over a configured forge with provider-aware auth), pinned to a git ref resolved to a commit SHA, tracked in .gtb/manifest.yaml, rendered alongside the embedded scaffold, and able to act as a suitable ALTERNATIVE that overrides the embedded CI files. Includes a security model for executing user/remote-fetched templates."
status: DRAFT
date: 2026-06-15
tags:
  - specification
  - generator
  - templates
  - vcs
  - repo
  - security
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Generator custom/extensible partial templates

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-15

Status
:   DRAFT

## Summary

`gtb generate project` renders a fixed set of embedded assets. The Go files come
from `dave/jennifer` (`generateSkeletonGoFiles`); the rest come from three
embedded trees walked by `generateSkeletonTemplateFiles` →
`walkSkeletonAssets`: the common skeleton (`assets/skeleton`), and one of two
**provider-specific CI trees** selected by `ReleaseProvider`
(`assets/skeleton-github` → `.github/workflows/*`, or `assets/skeleton-gitlab` →
`.gitlab-ci.yml` + `.gitlab/ci/*`). Every file is a `text/template` rendered
against `skeletonTemplateData`, hash-tracked in `.gtb/manifest.yaml`, and
subject to the `.gtb/ignore` rules. There is **no way for an operator to extend
this set** with their own templates — to add an organisation-standard
`SECURITY.md`, a house-style issue template, a bespoke Dockerfile, or to replace
the framework CI with their own pipeline — without forking GTB or hand-editing
every generated tree after the fact (which then trips the hash-conflict prompt
on regenerate).

This spec proposes **user-defined custom partial template sets**. Per the
maintainer's framing, users can:

> 1. "define their own custom **partial templates to iterate over**" — additional
>    template files rendered alongside the embedded scaffold, **tracked in the
>    manifest** along with the **git ref used (branch, tag, or commit)**.
> 2. have them "**rendered alongside the embedded content**", and — "**for the CI
>    files that exist in the embedded assets** (gitlab `.gitlab-ci.yml`/`.gitlab/ci`,
>    github `.github/workflows`) **a user template set can be a suitable
>    ALTERNATIVE**" (replace/override the embedded CI).
> 3. point a template source at "**a local folder OR a git repo** — and if the git
>    repo is private, **on one of the configured forges**" (reuse the
>    provider-aware auth from
>    [provider-aware-repo-auth](2026-06-12-provider-aware-repo-auth.md)).

A new `properties.templates` manifest block records each source (type, location,
ref, the **resolved commit SHA**, target scope, and per-file hashes), so
`regenerate` reproduces the same output deterministically and so the
hash-protection / conflict model extends cleanly to user-overridden files.

This is a substantial, security-sensitive capability: it makes the generator
execute **template content that GTB did not author** — potentially fetched from
a remote git repository — which bypasses the existing escape-at-known-sites
model (`template_escape.go`) because the *template author*, not GTB, controls
the output. The [Security model](#security-model) section is the load-bearing
part of this spec.

It is the largest of three related generator specs; see
[Related](#related) for the [git-initialisation](2026-06-15-generator-git-initialisation.md)
and [gitlab-ci-refresh](2026-06-15-generator-gitlab-ci-refresh.md) work.

## Motivation

The embedded skeleton encodes *the framework's* opinions. Real adopting
organisations have **their own** opinions that the scaffold cannot anticipate:

- **House files the framework will never ship** — `SECURITY.md`, `CONTRIBUTING.md`
  in a specific format, a corporate `LICENSE` header, `.editorconfig`, a
  Dockerfile, a Helm chart, issue/PR templates, a `CODEOWNERS` with the org's
  teams.
- **A different CI posture.** The embedded GitHub/GitLab CI is GTB-shaped
  (releaser-pleaser + GoReleaser). An org that runs Jenkins, Buildkite,
  CircleCI, or a bespoke internal GitLab CI include wants to **replace** the
  framework CI wholesale — the maintainer's "suitable alternative" requirement.
  Today the only escape hatch is `.gtb/ignore` (which *suppresses* the embedded
  CI but renders nothing in its place) plus manual authoring.
- **Iteration over project structure.** Per the "partials to iterate over"
  framing, an org may want a template rendered **per command** (e.g. a runbook
  stub per generated command) or **per enabled feature**, not just once.

The framework already owns every primitive this needs: a `text/template` render
pipeline with a documented data struct (`skeletonTemplateData`); a
hash/conflict/ignore model; a manifest that records generation inputs; and a
**provider-aware git layer** (`pkg/vcs/repo`, from the 0.17.0 work) that can
clone a private repo from any configured forge at a ref. Custom templates plug
into all of it. The missing piece is a **template-source abstraction**, a
**manifest representation**, and — critically — a **trust/security posture** for
running non-framework template content.

## Design

### D1 — The `properties.templates` manifest block

A new optional block under `ManifestProperties` (parallel to `Telemetry`,
`Signing`, `Help`), a **list** of template sources so multiple can compose:

```yaml
properties:
  name: mytool
  # …
  templates:
    - name: org-standard         # stable identifier (used in logs, ignore scoping, regenerate)
      source:
        type: git                # "git" | "local"
        location: org/dev-templates   # forge repo path (git) OR a filesystem path (local)
        forge: gitlab            # optional; selects the forge auth subtree for a private git source
        private: true            # requires auth (D5)
        subdir: gtb/             # optional path within the source to treat as the template root
      ref: main                  # the ref the USER asked for (branch | tag | commit) — recorded verbatim
      resolved: 9f3c1a2…         # the commit SHA `ref` resolved to at generate time (D4) — the pin
      mode: augment              # "augment" (add files) | "override" (D3)
      scope:                     # optional; restricts where this set may write (D3)
        - ci
      hashes:                    # per-source rendered-output hashes, exactly like Manifest.Hashes
        SECURITY.md: 5f2a…
        .gitlab-ci.yml: 1c8e…
```

Recorded fields and why:

- `name` — a stable identifier the operator chooses; scopes log lines, becomes
  addressable in `.gtb/ignore` (e.g. `templates/org-standard/**`), and keys the
  source on regenerate.
- `source.type` — `git` or `local` ([D5](#d5--source-types-local-folder-or-git-repo-private-via-forge)).
- `source.location` — for `git`, the **forge repo path** (`org/repo`, nested
  GitLab groups supported, mirroring `splitRepoPath`) or a full clone URL; for
  `local`, a filesystem path (validated, [Security model](#security-model)).
- `source.forge` / `source.private` — drive provider-aware auth ([D5](#d5--source-types-local-folder-or-git-repo-private-via-forge)).
- `source.subdir` — optional template root within the source, so a repo can hold
  templates alongside other content.
- `ref` — the branch/tag/commit the user **specified**, recorded verbatim.
- `resolved` — the **commit SHA** `ref` resolved to at generate time: the pin
  that makes regenerate reproducible ([D4](#d4--ref-pinning-record-both-the-ref-and-the-resolved-sha)).
- `mode` + `scope` — augment vs override and where the set may write
  ([D3](#d3--render-alongside-augment-vs-override-the-ci-alternative)).
- `hashes` — per-rendered-file hashes so the existing conflict/refresh machinery
  ([`hash.go`](#d6--hash-protection-conflict-and-ignore-interaction)) extends to
  custom output with no new mechanism.

A corresponding `SkeletonConfig.Templates []TemplateSource` field carries the
sources through generation; the wizard/flags populate it and
`writeSkeletonManifest` persists it.

### D2 — "Partials to iterate over": interpretation (flag as open question)

The phrase **"custom partial templates to iterate over"** is ambiguous and
admits two readings; both are *useful*, and they are not mutually exclusive.
This spec designs for both layers but flags the precise scope as
[O1](#open-questions) for the maintainer to confirm.

- **Reading A — a set of template *files* rendered once each.** The source is a
  directory tree; every file is rendered against the data contract and written
  to the mirrored relative path (a `SECURITY.md` at the source root → `SECURITY.md`
  in the output). "Iterate over" = walk the tree, exactly as `walkSkeletonAssets`
  already does for the embedded trees. **This is the primary, must-have reading**
  and the minimum viable feature.

- **Reading B — templates rendered *per project element*** (per command, per
  enabled feature). "Iterate over" = the *generator* iterates a collection and
  renders the template once per element, expanding a filename token. Proposed
  convention: a path token (e.g. `__command__`) in the source filename triggers
  per-command expansion, with the element exposed in the data context (a
  `.Command` / `.Feature` field added to the data struct). Example:
  `docs/runbooks/__command__.md` → one file per generated command. This is the
  richer reading of "iterate" and the bigger design + data-contract commitment.

Additionally, Go `text/template`'s own `{{ template "name" . }}` **partials/
includes** are a third, orthogonal axis: a source may ship shared partial files
(by convention, a `_partials/` dir or files prefixed `_`) that are parsed into
the template namespace but **not** emitted as standalone output, so the user's
own templates can `{{ template }}`-include them. This is cheap to support (parse
the whole set into one `*template.Template`, skip `_`-prefixed files at the
emit step) and is almost certainly part of what "partial templates" means.

**Recommendation:** ship **Reading A + Go partials** in v1 (covers the
overwhelming common case and the literal word "partial"), and treat **Reading B
(per-element iteration)** as a fast-follow gated on confirming the iteration
collection(s) and the data-contract additions. The spec proceeds on that basis;
[O1](#open-questions) asks the maintainer to confirm.

### D3 — Render alongside (augment) vs override; the CI ALTERNATIVE

Each source has a `mode`:

- **`augment`** (default) — files are **added**. A source file maps to its
  mirrored relative output path. If a source file's output path **collides with
  an embedded asset**, that is a **conflict**; default policy is to **refuse and
  error** (the operator must either rename, switch the source to `override`, or
  `.gtb/ignore` the embedded file) — fail-closed so an `augment` source cannot
  silently shadow framework files. Collisions *between two augment sources* are
  likewise an error (deterministic, order-independent).

- **`override`** — the source is declared a **suitable alternative** for a
  region of the embedded output and **replaces** it. This is the mechanism for
  the maintainer's CI requirement: an `override` source scoped to `ci`
  **suppresses the embedded provider CI tree entirely** (the
  `assets/skeleton-github` / `assets/skeleton-gitlab` walk is skipped for the
  scoped paths) and renders the user's CI in its place. Override is the
  generalisation of `.gtb/ignore` + "render something instead".

**`scope`** bounds where an `override` (or `augment`) source may write, as a set
of named regions and/or path globs. Proposed named regions, derived from the
embedded structure:

| Region | Embedded paths it covers |
|--------|--------------------------|
| `ci` | `.github/workflows/**` (github) **or** `.gitlab-ci.yml` + `.gitlab/ci/**` (gitlab) — provider-resolved |
| `docs` | the docs/zensical scaffold |
| `meta` | `CODEOWNERS`, `renovate.json5`, `README.md`, `LICENSE`, … |

An `override` with no `scope` defaulting to "anything it writes" is **rejected**:
override must be explicit about what it replaces, so it cannot accidentally
shadow the whole tree.

**Precedence (deterministic, recorded order):** embedded assets render first;
then template sources render **in manifest list order**; an `override` source
removes the embedded contributions in its scope **before** its own files render;
two sources may not both write the same output path (error). The final winner
for any path is therefore unambiguous and reproducible from the manifest alone.
This ordering is the contract `regenerate` relies on.

### D4 — Ref pinning: record both the ref and the resolved SHA

The maintainer's requirement is "tracked in the manifest along with the git ref
used (branch, tag, or commit)". Recording *only* a branch/tag is **not
reproducible** — `main` moves. So:

- The operator specifies a `ref` (branch, tag, or full/abbrev commit). It is
  recorded verbatim in `ref` (provenance: "what did I ask for?").
- At generate time the generator **resolves `ref` to the concrete commit SHA**
  and records it in `resolved`. This SHA is the **pin**.
- **`regenerate` checks out `resolved`, not `ref`** — so a regenerate a year
  later reproduces byte-identical template content even though `main` has moved.
- An explicit `gtb generate project --template-update <name>` (or a
  manifest-driven refresh, [D8](#d8--cli-surface)) re-resolves `ref` → a new
  `resolved` and re-renders, surfacing the diff through the normal hash-conflict
  flow. This is the *only* path that advances the pin.

`local` sources have no SHA; their reproducibility is "whatever is on disk now"
and `resolved` is empty. The manifest may optionally record a content
fingerprint of the local tree at generate time so `regenerate` can warn if the
local source drifted — flagged as [O5](#open-questions).

### D5 — Source types: local folder, or git repo (private via forge)

A source is fetched by type:

- **`local`** — read directly from the filesystem path via the generator's
  `afero.Fs`, exactly like embedded assets but from a real directory. The path
  is validated (no traversal outside an allowed root; see
  [Security model](#security-model)). No network, no cache.

- **`git`** — cloned via **`pkg/vcs/repo`**, reusing the provider-aware
  clone/auth delivered by
  [provider-aware-repo-auth](2026-06-12-provider-aware-repo-auth.md):
  - **Public** sources clone over plain `https`/go-git with **no credential**
    (the spec's D2 "missing auth is non-fatal for public repos").
  - **Private** sources (`source.private: true`) resolve auth from the
    **configured forge subtree** — `source.forge` (defaulting to the tool's own
    `ReleaseSource.Type`, overridable by `vcs.provider`) selects `<forge>.auth` /
    `<forge>.ssh`, and `vcs.ResolveToken` resolves the credential
    (env-ref → keychain → literal). **No second auth path** — this is exactly the
    mechanism the spec generalised for clone/push, applied to template fetch.
  - The clone is **shallow** at the resolved SHA where the forge supports it
    (`WithShallowClone` / `WithSingleBranch` already exist), then `resolved` is
    the checked-out commit ([D4](#d4--ref-pinning-record-both-the-ref-and-the-resolved-sha)).

The forge URL for a `git` source is built from `location` + the forge's host
exactly as the git-init spec builds its push URL, preserving nested GitLab group
paths.

### D6 — Hash protection, conflict, and ignore interaction

Custom-template output participates in the **existing** machinery, with no new
mechanism invented:

- Each rendered custom file's SHA256 is recorded — proposed under the source's
  own `hashes` map (keyed by output relative path), so a source's footprint is
  self-contained and a source can be removed cleanly. (Alternative: fold into the
  top-level `Manifest.Hashes`; [O6](#open-questions).)
- On regenerate, a custom output file modified by the operator since last
  generation triggers the **same `checkSkeletonConflict` / `promptOverwrite`
  flow** as embedded files — manual edits are protected identically.
- `runSkeletonPostProcessing` (go mod tidy / golangci-lint) and the subsequent
  `refreshProjectFileHashes` must also refresh custom-file hashes, so post-
  processing edits to a custom Go file don't read as a user customisation next
  run.
- **`.gtb/ignore`** rules apply to custom output paths exactly as to embedded
  ones (`rules.IsIgnored(relPath)` in the walk), and the `name`-scoped form
  (`templates/<name>/**`) lets an operator suppress a whole source.
- For an **`override`** source, the embedded file it replaces is **not** hashed
  (it was never written); only the override's output is tracked. Removing the
  override source on a later regenerate restores the embedded file (it re-enters
  the walk) — a clean, reversible swap.

### D7 — Fetch, caching, and offline/regenerate behaviour

- **Cache location.** `git` sources are cloned into a per-source, ref-pinned
  cache under the **user cache dir** (e.g.
  `$XDG_CACHE_HOME/gtb/templates/<host>/<owner>/<repo>@<sha>/`), keyed by the
  resolved SHA so a pin is immutable and shareable across projects. A project-
  local cache under `.gtb/` is the alternative ([O7](#open-questions)); the
  user-cache default keeps generated trees clean and avoids committing fetched
  template sources. The cache is **never** the source of truth for output — it is
  only the input the renderer reads.
- **Offline / regenerate.** Because regenerate targets `resolved` (a SHA), a
  warm cache means **regenerate works fully offline**. A cold cache on a private
  source with no network/credential **fails that source non-fatally** (warning +
  the embedded fallback for an `override` source is *not* auto-restored — that
  would change output silently; instead regenerate errors clearly and tells the
  operator to restore connectivity or remove the source). Exact offline policy is
  [O7](#open-questions).
- **Integrity.** The resolved SHA is itself a content hash of the git tree; a
  fetched cache entry whose checked-out HEAD ≠ `resolved` is rejected.

### D8 — CLI surface

Proposed surface (confirm shape in [O8](#open-questions)):

- **Add at generate time:**
  `gtb generate project … --template <src>@<ref> [--template-mode override] [--template-scope ci]`
  — repeatable; `<src>` is a forge path / URL / local path, `@<ref>` optional
  (defaults to the source's default branch, then pinned). The wizard offers an
  interactive "add a template source?" step.
- **Manage on an existing project (manifest-editing subcommands):**
  - `gtb template add <src>@<ref> [--mode …] [--scope …] [--name …]` — appends a
    source, resolves the pin, renders, refreshes the manifest.
  - `gtb template update <name>` — re-resolves `ref` → new `resolved`, re-renders
    (the only pin-advancing path, [D4](#d4--ref-pinning-record-both-the-ref-and-the-resolved-sha)).
  - `gtb template remove <name>` — removes the source and its tracked output
    (restoring any embedded file an `override` had replaced).
  - `gtb template list` — shows sources, refs, resolved SHAs, modes, scopes.
- **Pure manifest edit + `regenerate`** must also work — hand-adding a
  `templates:` entry and running `regenerate` is a supported path (it goes
  through `ValidateManifest`, [Security model](#security-model)).

`gtb template …` is a new command group, but the manifest-edit + regenerate path
is the source of truth; the subcommands are ergonomics over it.

## Security model

This is the **riskiest** part of the feature and is treated as a first-class
section. Rendering custom templates means the generator **parses and executes
`text/template` content GTB did not author**, and for `git` sources that content
arrives **over the network from a repository the framework does not control**.
This fundamentally differs from the existing model documented in
[template-security.md](../development/template-security.md), where GTB authors
every template and escapes *its own* user-supplied **field values** at known
sites. Here the **template author controls the output directly**, so the
escape-at-known-sites perimeter does **not** protect the output of a custom
template.

### Threat model

What a malicious or compromised template source can attempt:

1. **Path traversal on write.** A source file or a per-element filename token
   (`__command__`) crafted to render to `../../etc/...` or an absolute path,
   escaping the project tree (the same sink class `getCommandPath` /
   `ValidateCommandName` already guard for command generation).
2. **Overwriting framework/security-critical files.** An `augment` source
   silently shadowing `go.mod`, `.gtb/manifest.yaml`, the signing trust keys, or
   CI — supply-chain injection into the generated tool.
3. **Information disclosure via the data contract.** A template exfiltrating
   whatever the data context exposes. The contract **must not** carry secrets
   (tokens, resolved credentials, absolute host paths, env) — only the
   already-public project metadata.
4. **`text/template` execution surface.** `text/template` does **not** execute
   arbitrary Go, but a template can: call any function registered in the
   `FuncMap` (so a custom FuncMap must expose nothing dangerous — no file read,
   no exec, no network); trigger pathological expansion / huge output (resource
   exhaustion); and emit content that is **itself** an injection into the
   *downstream* build (a malicious `.gitlab-ci.yml` or `justfile` that runs
   attacker code when CI/`just` runs).
5. **Clone-time code execution.** A git source whose checkout runs hooks or
   `.gitattributes` filters, or whose submodules pull from attacker hosts.
   Fetching must be inert: no hook execution, no submodule recursion by default,
   no filter/clean smudge.
6. **Supply-chain drift.** A branch/tag `ref` silently changing under the
   operator between runs — addressed by the SHA pin
   ([D4](#d4--ref-pinning-record-both-the-ref-and-the-resolved-sha)).

### Posture and guardrails

- **Trusted-source posture (proposed default).** Custom templates are treated as
  **operator-trusted input**, *not* sandboxed arbitrary code — analogous to a
  `Makefile`, a `Dockerfile`, or a git pre-commit hook the operator chose to
  run. The operator is responsible for the provenance of a source they add (the
  SHA pin gives them the means to vet a specific commit). The framework's job is
  to make the **blast radius bounded and the provenance explicit**, not to run
  hostile templates safely. The alternative — a true sandbox — is
  [O3](#open-questions); the recommendation is trusted-source + the hard
  guardrails below, with a loud first-use confirmation for remote sources.
- **Write-path containment (hard, always on).** Every rendered output path is
  resolved and checked (`filepath.Abs` + `filepath.Rel`) to sit **strictly under
  the project root**, reusing the AI-doc-tool / `getCommandPath` containment
  pattern. A path that escapes is a fatal error for that file. Per-element
  filename tokens are validated through the same class as `ValidateCommandName`.
- **Protected-path denylist (hard).** Custom output may **never** write
  `.gtb/**` (manifest/ignore), `internal/trustkeys/**` (signing anchors), or
  `go.mod`/`go.sum` — even in `override` mode. Override's `scope` is an
  **allowlist** of regions, never a way to reach these.
- **No dangerous FuncMap.** Custom templates render with a **restricted FuncMap**
  — the existing escape helpers plus pure string/format helpers, and **nothing**
  that reads files, runs commands, opens network connections, or reads the
  environment. (`text/template` has no such builtins; the risk is only what GTB
  *adds*.)
- **Data contract is metadata-only** (see [D-contract](#the-data-contract));
  resolved tokens/credentials are never placed in the context.
- **Inert fetch.** Clones disable hooks, do not recurse submodules by default,
  and do not run filters; the cache is read-only input. Length/size bounds cap a
  pathological source.
- **Validation at the manifest gate.** `ValidateManifest` is extended to validate
  every `templates` entry (source type, location character class, forge name,
  ref/SHA shape, mode/scope) so a tampered manifest cannot drive a fetch or write
  outside the rules — mirroring how it already validates `Commands` and
  `Signing`. Invalid entries are **skipped, not fatal**, on the regenerate path
  (consistent with [template-security.md](../development/template-security.md)).
- **Explicit-trust confirmation.** Adding a **remote** source for the first time
  prompts an interactive confirmation naming the host/owner/repo and the resolved
  SHA (suppressible with `--yes`/non-interactive for CI). Adding a source is the
  trust decision; the pin records exactly what was trusted.

### Output escaping reality

Because the template author controls output, GTB **cannot** guarantee a custom
template's output is well-formed YAML/Markdown/etc. — the escape helpers protect
GTB's *own* field interpolation, not a third party's whole-file template. This is
stated plainly in the docs: **a custom template's output correctness is the
template author's responsibility.** GTB's guarantees are confined to *where* the
output may land (containment + denylist) and *what data* the template may see
(metadata-only contract), not the bytes it emits.

### The data contract

Custom templates render against a **documented, stable, metadata-only** subset
of `skeletonTemplateData` — versioned so a source can declare the contract
version it expects. Proposed exposed fields (all already public project
metadata): `Name`, `Description`, `Repo`, `Host`, `Org`, `RepoName`,
`ModulePath`, `ReleaseProvider`, `GoVersion`, `GoToolBaseVersion`, `EnvPrefix`,
`EnabledFeatures`, `DisabledFeatures`, `Private`, and the help/telemetry/signing
*presence/shape* (not secrets). For Reading B
([D2](#d2--partials-to-iterate-over-interpretation-flag-as-open-question)) a
per-iteration `.Command` / `.Feature` is added. **Explicitly excluded:** any
resolved credential, env var, absolute host path, or forge token. The contract
is documented in `docs/components/` and frozen under the pre-1.0 visibility
rules; additive fields are safe, removals are a contract-version bump.

## Regeneration & reproducibility

- The **SHA pin** (`resolved`, [D4](#d4--ref-pinning-record-both-the-ref-and-the-resolved-sha))
  is the cornerstone: regenerate fetches/reads `resolved`, never the moving
  `ref`, so output is byte-stable across time. A warm cache makes it fully
  offline.
- **Deterministic precedence** ([D3](#d3--render-alongside-augment-vs-override-the-ci-alternative)):
  embedded → sources in manifest order; override removes-then-renders within its
  scope; cross-source path collisions are an error. The winner for every path is
  computable from the manifest alone, independent of map iteration order.
- **Hash protection** ([D6](#d6--hash-protection-conflict-and-ignore-interaction))
  extends unchanged: per-source `hashes`, `checkSkeletonConflict` on regenerate,
  `refreshProjectFileHashes` after post-processing.
- **Reversibility:** removing a source (or `gtb template remove`) drops its
  tracked output and **restores any embedded file an override replaced** (the
  embedded file simply re-enters the walk on the next regenerate).
- **`remove` (project removal)** treats custom output like any other tracked file
  — no new behaviour, but the verification plan asserts it.

## CLI surface

See [D8](#d8--cli-surface). Summary: `--template <src>@<ref>` (+ `--template-mode`,
`--template-scope`) at `generate project`; a `gtb template {add,update,remove,list}`
group for existing projects; and a hand-edit-`templates:` + `regenerate` path
that goes through `ValidateManifest`.

## Open questions

1. **O1 — "Partials to iterate over" scope.** Confirm the
   [D2](#d2--partials-to-iterate-over-interpretation-flag-as-open-question)
   interpretation: ship **Reading A (per-file render) + Go `{{ template }}`
   partials** in v1, defer **Reading B (per-command / per-feature iteration with
   `__command__` filename expansion + `.Command`/`.Feature` data fields)** to a
   fast-follow? Or is per-element iteration a v1 must-have? If so, which
   collections (commands, features, both)?
2. **O2 — Override granularity.** Is `override` at the **region/scope** level
   (`ci`, `docs`, `meta`) the right grain, or should override be **per-file**
   (a source file overrides exactly the embedded file at the same path)? Region
   scoping is safer for the CI "alternative"; per-file is finer but easier to get
   wrong.
3. **O3 — Security posture: trusted-source vs sandbox.** Confirm the
   **trusted-source** posture (operator owns provenance; GTB bounds the blast
   radius via containment + denylist + metadata-only contract + inert fetch +
   restricted FuncMap), rather than attempting to *sandbox* arbitrary template
   execution. Is the first-use remote-source confirmation prompt sufficient, or
   is an allowlist of permitted template hosts (config-driven) wanted?
4. **O4 — Public-only vs private-forge in v1.** Ship **both** public
   (https/go-git) and private (provider-aware forge auth) `git` sources in v1, or
   start **public-only** and add private-forge fetch in a follow-up? Private
   reuses existing auth so the marginal cost is low, but it widens the v1 test
   surface.
5. **O5 — Local-source drift.** Should a `local` source record a content
   fingerprint so `regenerate` can **warn** when the on-disk source changed since
   last generation (no SHA pin is possible for local), or is "local is whatever
   is on disk" acceptable with no drift detection?
6. **O6 — Hash storage shape.** Per-source `hashes` map under each `templates`
   entry (self-contained, clean removal — proposed), or fold custom-output hashes
   into the top-level `Manifest.Hashes` (one map, but harder to attribute/remove)?
7. **O7 — Cache location & offline policy.** User cache dir keyed by SHA
   (`$XDG_CACHE_HOME/gtb/templates/…@<sha>`, proposed) vs a project-local `.gtb/`
   cache. And: on a cold cache with no network for an `override` source, **error
   clearly** (proposed — never silently restore the embedded fallback) vs
   **auto-restore the embedded file with a warning**?
8. **O8 — CLI shape.** Confirm the surface in [D8](#d8--cli-surface): a
   `--template <src>@<ref>` flag on `generate project` **plus** a
   `gtb template {add,update,remove,list}` group **plus** the manifest-edit +
   regenerate path. Is the dedicated command group worth it, or is flag +
   manifest-edit enough for v1?
9. **O9 — Data contract surface.** Confirm the metadata-only field set in
   [the data contract](#the-data-contract) and that it is **versioned** (a source
   declares the contract version it targets). Any field an org would plausibly
   need that is *not* a secret and is missing?
10. **O10 — Augment collision policy.** Confirm that an `augment` source whose
    output path collides with an embedded asset is a **hard error** (fail-closed,
    proposed) rather than a silent override — forcing the operator to use
    `override` + `scope` explicitly when they mean to replace.

## Verification plan

1. **Unit — local augment.** A `local` source adds a `SECURITY.md`; it renders
   against the data contract, lands at the mirrored path, and is hash-tracked
   under the source's `hashes`.
2. **Unit — CI override.** An `override` source scoped to `ci` **suppresses** the
   embedded provider CI tree (github *and* gitlab cases) and renders the user CI
   in its place; the embedded CI files are absent and untracked.
3. **Unit — augment collision is fatal** ([O10](#open-questions)); cross-source
   path collision is fatal; precedence is manifest-order-deterministic.
4. **Unit — write containment.** A source file / `__command__` token rendering to
   `../escape` or an absolute path is rejected, no write occurs outside the tree.
5. **Unit — protected denylist.** A source attempting to write `.gtb/**`,
   `internal/trustkeys/**`, or `go.mod` is refused even in `override` mode.
6. **Unit — restricted FuncMap.** Templates cannot reach a file/exec/env/network
   helper; the registered FuncMap exposes only escape + pure-format helpers.
7. **Unit — data contract is metadata-only.** No resolved credential / token /
   env / absolute path is reachable from a custom template's context.
8. **Unit — ref pin reproducibility.** Two renders against the same `resolved`
   SHA produce identical output; advancing `ref` only happens via
   `template update` and re-resolves the pin.
9. **Unit — regenerate conflict.** A user-edited custom output file triggers the
   same `checkSkeletonConflict`/`promptOverwrite` flow as embedded files; post-
   processing refreshes custom hashes.
10. **Unit — reversibility.** Removing an `override` source restores the embedded
    file on regenerate; removing an `augment` source drops only its files.
11. **Unit — ignore interaction.** `.gtb/ignore` with `templates/<name>/**`
    suppresses a whole source; per-file ignore suppresses a single custom file.
12. **Unit — manifest validation.** A tampered `templates` entry (bad type,
    traversal `location`, malformed ref) is rejected/skipped by `ValidateManifest`.
13. **Integration (`INT_TEST_VCS=1`)** — a **public** `git` source clones at a ref
    and pins the SHA; a **private** source resolves auth from the configured forge
    subtree (token/ssh), against a local bare remote — extends
    `repo_integration_test.go`.
14. **Integration — offline regenerate** from a warm SHA-keyed cache renders with
    no network; cold cache + no network errors clearly ([O7](#open-questions)).
15. **E2E (Godog)** — `gtb template add … && regenerate` and `gtb template remove`
    user workflows (new CLI command group ⇒ Gherkin required).
16. **Docs** — `docs/components/generator` (the templates block, modes, scopes,
    data contract), a new threat-model section cross-referenced from
    `docs/development/template-security.md`, and a how-to for authoring a template
    source.

## Out of scope

- **Sandboxed execution of untrusted templates.** The posture is trusted-source
  with bounded blast radius ([O3](#open-questions)); a true sandbox (WASM,
  process isolation) is a separate, much larger capability.
- **A template *registry* / discovery service.** Sources are addressed by
  forge path / URL / local path; no central index, ratings, or signing of
  template repos (template-repo *signing* is a plausible later hardening, not v1).
- **Non-`text/template` engines** (Jinja, Handlebars, Go `html/template`). v1 is
  `text/template`, matching the embedded pipeline.
- **Overriding the Jennifer-generated Go files** (`main.go`, `cmd.go`,
  `version.go`). Custom templates extend the *template-rendered* surface; the
  AST-generated Go core is not user-overridable in v1.
- **Mutating `regenerate`/`remove` semantics** beyond extending the existing
  hash/conflict/containment machinery to custom output.
- **Re-implementing git auth.** Auth is whatever
  [provider-aware-repo-auth](2026-06-12-provider-aware-repo-auth.md) /
  `pkg/vcs/repo` already resolves.

## Related

- [Generator git initialisation & initial commit](2026-06-15-generator-git-initialisation.md)
  — the sibling generator spec on this branch; shares the `pkg/vcs/repo` reuse and
  the forge-URL derivation patterns.
- [Generator GitLab CI refresh](2026-06-15-generator-gitlab-ci-refresh.md) — the
  third generator spec; the embedded CI that custom `override` sources can replace
  is the subject of that refresh, so the two must agree on the CI scope boundary.
- [Provider-aware repository auth](2026-06-12-provider-aware-repo-auth.md) — the
  forge-aware clone/auth (`resolveForge`, `<forge>.auth`/`<forge>.ssh`,
  `vcs.ResolveToken`, non-fatal public clone) the `git` source fetch reuses.
- [Template Security](../development/template-security.md) — the existing
  escape-at-known-sites model this feature deliberately steps *outside*; the
  custom-template threat model extends it.
- [Generator template escaping](2026-04-02-generator-template-escaping.md) and
  [Generator validation perimeter](2026-06-12-generator-validation-perimeter.md)
  — the escape helpers / `ValidateManifest` gate the custom-template path hooks
  into.
- `internal/generator/skeleton.go` — `generateSkeletonTemplateFiles` /
  `walkSkeletonAssets` / `skeletonTemplateData`, the render pipeline custom
  sources plug into.
- `internal/generator/manifest.go` — `ManifestProperties` where `templates`
  slots in; `internal/generator/hash.go` and `internal/cmd/regenerate/` for the
  hash/conflict machinery; `internal/generator/ignore.go` for `.gtb/ignore`.
- `pkg/vcs/repo/repo.go` — `Clone`/`Opener` roles and `NewRepo`'s provider-aware
  auth used to fetch a `git` source at the pinned SHA.
