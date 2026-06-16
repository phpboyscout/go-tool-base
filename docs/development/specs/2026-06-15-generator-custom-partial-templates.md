---
title: "Generator custom/extensible template overlays (local folder or git repo, ref-pinned, manifest-tracked)"
description: "The generator renders only its embedded skeleton; users cannot extend it with their own templates. Add user-supplied custom template overlays — sourced from a local folder or a git repo (public over https/go-git, private over a configured forge with provider-aware auth), pinned to a git ref resolved to a commit SHA, tracked in .gtb/manifest.yaml. Every file in the source is rendered through the existing text/template engine to the identical relative path in the generated project: a new path adds a file; a path that also exists in the embedded skeleton is overwritten (user wins). A source-side gtb-template.yaml descriptor can suppress whole embedded forge-CI scaffolds, so a user CI file becomes the suitable ALTERNATIVE simply by living at the same path. Includes a security model for executing user/remote-fetched templates."
status: IMPLEMENTED
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

# Generator custom/extensible template overlays

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-15

Status
:   IMPLEMENTED (2026-06-16 — per-file overlay, `gtb-template.yaml` `replaces:` suppression, local + git (public/private forge) sources with XDG `@<sha>` cache and ref→SHA pinning, per-source hashes + offline-reproducible regenerate, the security containment/denylist/restricted-FuncMap/metadata-only-contract posture, `ValidateManifest` gating, the `--template` flag, and the `gtb template add/update/remove/list` group all landed; open questions resolved in review 2026-06-15)

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

This spec proposes **user-defined custom template overlays**, resolved in review
2026-06-15 to a single, simple core mechanism: a **per-file rendered overlay**.
A template source is a directory tree (local folder or git repo); the generator
**walks every file** in it and **renders each one through the existing
`text/template` engine and data contract** to the **identical relative path** in
the generated project. The effect is straightforward:

- A source path that does **not** exist in the embedded skeleton **adds** a file
  (an organisation `SECURITY.md`, `CODEOWNERS`, Dockerfile, …).
- A source path that **also** exists in the embedded `skeleton*` assets
  **overwrites** the embedded file — the user's deviation wins. This is how a
  user's CI file becomes the "suitable alternative" the maintainer wanted:
  it simply lives at the same path.

An overlay can only **add or overwrite**, never **delete**, an embedded file. To
let a source fully **replace** an embedded forge-CI scaffold (where the
replacement has fewer/differently-named files than the embedded tree), the source
carries a root **`gtb-template.yaml` descriptor** whose `replaces:` list names
the embedded scaffolds to **suppress before the overlay renders** (v1 named sets:
`gitlab-ci`, `github-ci`). Two reserved root meta files — `README.md` (the
human-facing explainer of the template set) and `gtb-template.yaml` (the
descriptor) — are **excluded from rendering**.

The consumer manifest is **minimal**: it records only provenance and pinning
(source type, location, the user-given `ref`, the **resolved commit SHA**, and
per-source output hashes). Suppression/replacement behaviour lives **with the
template set** (declared once by the author in `gtb-template.yaml`), so consuming
projects do not repeat it. `regenerate` reads the resolved SHA to reproduce the
same output deterministically, and the hash-protection / conflict model extends
cleanly to overlaid files.

The source may also point at "**a local folder OR a git repo** — and if the git
repo is private, **on one of the configured forges**", reusing the provider-aware
auth from [provider-aware-repo-auth](2026-06-12-provider-aware-repo-auth.md).

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
  CI but renders nothing in its place) plus manual authoring. A whole-scaffold
  replacement also needs to *remove* embedded files the alternative does not
  carry — which a pure add/overwrite overlay cannot do, hence the source-side
  `replaces:` descriptor.

The framework already owns every primitive this needs: a `text/template` render
pipeline with a documented data struct (`skeletonTemplateData`); a
hash/conflict/ignore model; a manifest that records generation inputs; and a
**provider-aware git layer** (`pkg/vcs/repo`, from the 0.17.0 work) that can
clone a private repo from any configured forge at a ref. The overlay plugs into
all of it. The missing pieces are a **template-source abstraction** (walk +
render to mirrored paths), the **`gtb-template.yaml` descriptor** for
whole-scaffold suppression, a **minimal manifest representation** (provenance +
pin), and — critically — a **trust/security posture** for running non-framework
template content.

## Design

### D1 — The overlay: walk-and-render every source file to its mirrored path

The core mechanism is a **per-file rendered overlay**. A template source is a
directory tree. The generator **walks every file** in the source (exactly as
`walkSkeletonAssets` already walks the embedded trees) and **renders each file
through the existing `text/template` engine against the data contract**
([the data contract](#the-data-contract)), writing the result to the **identical
relative path** in the generated project:

- A source file at `SECURITY.md` → `SECURITY.md` in the output.
- A source file at `.github/workflows/ci.yml` → `.github/workflows/ci.yml`.

Two outcomes follow from the mirrored relative path, with **no mode flag and no
scope concept**:

- **Add.** If the mirrored output path does **not** exist in the embedded
  `skeleton*` assets, the overlay **adds** the file.
- **Overwrite (user wins).** If the mirrored output path **also** exists in the
  embedded assets, the overlay **overwrites** the embedded file — the operator's
  deviation wins ([D6](#d6--collision-policy-user-wins-multi-source-layering)).
  This is the entire "suitable alternative" mechanism: an operator who wants a
  different `.gitlab-ci.yml` ships one at `.gitlab-ci.yml` in their source.

**Two reserved root meta files are excluded from rendering** and never emitted:

- **`README.md`** (at the source root) — the human-facing explainer of the
  template set, for people browsing the source repo.
- **`gtb-template.yaml`** (at the source root) — the descriptor
  ([D3](#d3--whole-scaffold-replacement-the-gtb-templateyaml-descriptor)).

There is **no per-command / per-feature iteration** and **no filename-token
expansion** (`__command__`). "Iterate over" means *walk the tree and render each
file once*, nothing more. Go `text/template`'s own `{{ template "name" . }}`
includes remain available within a single file's render, but the generator emits
exactly the walked files (minus the two reserved meta files); the overlay does
not synthesise additional outputs from a collection.

### D2 — The minimal consumer manifest block (provenance + pinning only)

A new optional `properties.templates` block under `ManifestProperties` (parallel
to `Telemetry`, `Signing`, `Help`) records **only provenance and the pin** — the
*behaviour* (what a source replaces, which contract it targets) lives with the
template set in `gtb-template.yaml`, not in the consumer manifest:

```yaml
properties:
  name: mytool
  # …
  templates:
    - type: git                                  # "git" | "local"
      location: gitlab.com/acme/gtb-templates    # forge repo path / URL (git) OR a filesystem path (local)
      ref: v1.2.0                                # the ref the USER asked for: branch | tag | commit (recorded verbatim)
      resolved: 9f3c1a2…                         # the commit SHA `ref` resolved to at generate time — the pin (git sources)
      hashes:                                    # per-source rendered-output hashes (self-contained; D5)
        SECURITY.md: 5f2a…
        .gitlab-ci.yml: 1c8e…
```

Recorded fields and why:

- `type` — `git` or `local` ([D4](#d4--source-types-local-folder-or-git-repo-private-via-forge)).
- `location` — for `git`, the **forge repo path** (`org/repo`, nested GitLab
  groups supported, mirroring `splitRepoPath`) or a full clone URL; for `local`,
  a filesystem path (validated, [Security model](#security-model)).
- `ref` — the branch/tag/commit the user **specified**, recorded verbatim
  (provenance: "what did I ask for?").
- `resolved` — the **commit SHA** `ref` resolved to at generate time: the pin
  that makes regenerate reproducible
  ([D7](#d7--ref-pinning-record-both-the-ref-and-the-resolved-sha)). Empty for
  `local` sources (no SHA); a local source records a content fingerprint instead
  so drift can be warned on
  ([D7](#d7--ref-pinning-record-both-the-ref-and-the-resolved-sha)).
- `hashes` — per-rendered-file hashes so the existing conflict/refresh machinery
  ([`hash.go`](#d8--hash-protection-conflict-and-ignore-interaction)) extends to
  overlaid output with no new mechanism.

Forge selection for a private `git` source is derived from the `location` host
and the tool's configured forges — there is **no per-source `forge`/`private`
toggle in the manifest**; auth resolution follows
[D4](#d4--source-types-local-folder-or-git-repo-private-via-forge). A
corresponding `SkeletonConfig.Templates []TemplateSource` field carries the
sources through generation; the wizard/flags populate it and
`writeSkeletonManifest` persists it.

### D3 — Whole-scaffold replacement: the `gtb-template.yaml` descriptor

An overlay can only **add or overwrite**, never **delete**, an embedded file. A
source that wants to **fully replace** an embedded forge-CI scaffold — where the
replacement has fewer or differently-named files than the embedded tree, so a
pure overlay would leave stranded embedded files — declares that intent in a
**source-side descriptor at the source root**:

```yaml
# gtb-template.yaml (at the source root; reserved meta file, NOT rendered)
contract: 1                 # data-contract version this set targets (D9 / the data contract)
description: "ACME house CI + extra scaffolding"
replaces:                   # embedded scaffolds this set supersedes — SUPPRESSED before the overlay renders
  - gitlab-ci               # alias for assets/skeleton-gitlab (the whole .gitlab/ + .gitlab-ci.yml + renovate.json5)
  # - github-ci             # alias for assets/skeleton-github (the whole .github/)
```

- **GTB maintains a small alias → embedded-paths map.** The **named suppressible
  set for v1 is `gitlab-ci` and `github-ci`** (extensible later). `gitlab-ci`
  maps to the whole `assets/skeleton-gitlab` tree (`.gitlab-ci.yml`,
  `.gitlab/ci/**`, `renovate.json5`, …); `github-ci` maps to the whole
  `assets/skeleton-github` tree (`.github/**`).
- **`replaces:` suppresses before the overlay renders.** When a source declares
  `replaces: [gitlab-ci]`, the embedded GitLab CI tree is dropped from the walk
  **before** the overlay's files render, so the source provides the *complete* CI
  with no stranded embedded files left behind.
- **Suppression intent lives with the template set.** The author declares
  `replaces:` once in `gtb-template.yaml`; consuming projects do **not** repeat it
  in their manifests. The consumer manifest stays provenance-only
  ([D2](#d2--the-minimal-consumer-manifest-block-provenance--pinning-only)).
- **Absent descriptor / empty `replaces` ⇒ pure overlay.** Nothing is suppressed;
  the source only adds and overwrites by mirrored path.
- The `contract:` field declares the data-contract version the set targets
  ([the data contract](#the-data-contract)); GTB rejects unknown versions.

### D4 — Source types: local folder, or git repo (private via forge)

Both source types ship in **v1**:

- **`local`** — read directly from the filesystem path via the generator's
  `afero.Fs`, exactly like embedded assets but from a real directory. The path
  is validated (no traversal outside an allowed root; see
  [Security model](#security-model)). No network, no cache.

- **`git`** — cloned via **`pkg/vcs/repo`**, reusing the provider-aware
  clone/auth delivered by
  [provider-aware-repo-auth](2026-06-12-provider-aware-repo-auth.md):
  - **Public** sources clone over plain `https`/go-git with **no credential**
    (the spec's "missing auth is non-fatal for public repos").
  - **Private** sources resolve auth from the **configured forge subtree** —
    `resolveForge` selects the forge for the `location` host, `<forge>.auth` /
    `<forge>.ssh` carry the credential config, and `vcs.ResolveToken` resolves it
    (env-ref → keychain → literal). **No second auth path** — exactly the
    mechanism the provider-aware-repo-auth spec generalised for clone/push,
    applied to template fetch.
  - The clone is **shallow** at the resolved SHA where the forge supports it
    (`WithShallowClone` / `WithSingleBranch` already exist), then `resolved` is
    the checked-out commit
    ([D7](#d7--ref-pinning-record-both-the-ref-and-the-resolved-sha)).

The forge URL for a `git` source is built from `location` + the forge's host
exactly as the git-init spec builds its push URL, preserving nested GitLab group
paths.

### D5 — Hash storage: per-source `hashes` map

Each rendered overlay file's SHA256 is recorded in the **source's own `hashes`
map** (keyed by output relative path) under its `templates` entry, so a source's
footprint is **self-contained** and the source can be removed cleanly without
disturbing other entries or the top-level `Manifest.Hashes`. This is the storage
shape the conflict/refresh machinery reads
([D8](#d8--hash-protection-conflict-and-ignore-interaction)).

### D6 — Collision policy: user wins; multi-source layering

- **Overlay vs embedded asset = overwrite (user wins), never an error.** A source
  file whose mirrored path also exists in the embedded assets simply replaces the
  embedded file. This is the designed behaviour, not a conflict to refuse.
- **Multiple user sources layer in manifest order.** The render order is
  **embedded base → source 1 → source 2 → …** (manifest list order), with
  **last writer wins** for any shared output path. Each overwrite emits an
  **info/debug log line** naming the overridden path and the winning source — it
  is **not** a hard error, so composing sources is permitted and deterministic.
- **`replaces:` runs first.** Any `gtb-template.yaml` `replaces:` suppression
  ([D3](#d3--whole-scaffold-replacement-the-gtb-templateyaml-descriptor)) is
  applied **before** the overlay layers, so a replacing source starts from a clean
  embedded baseline for that scaffold.

The winner for every output path is therefore computable from the manifest list
order alone, independent of map iteration order — the contract `regenerate`
relies on.

### D7 — Ref pinning: record both the ref and the resolved SHA

The maintainer's requirement is "tracked in the manifest along with the git ref
used (branch, tag, or commit)". Recording *only* a branch/tag is **not
reproducible** — `main` moves. So:

- The operator specifies a `ref` (branch, tag, or full/abbrev commit), recorded
  verbatim in `ref`.
- At generate time the generator **resolves `ref` to the concrete commit SHA**
  and records it in `resolved`. This SHA is the **pin**.
- **`regenerate` checks out `resolved`, not `ref`** — so a regenerate a year
  later reproduces byte-identical template content even though `main` has moved.
- An explicit `gtb template update <…>` (or a manifest-driven refresh,
  [CLI surface](#cli-surface)) re-resolves `ref` → a new `resolved` and
  re-renders, surfacing the diff through the normal hash-conflict flow. This is
  the *only* path that advances the pin.

`local` sources have no SHA; `resolved` is empty and reproducibility is "whatever
is on disk now". A `local` source **records a content fingerprint** of the tree
at generate time, and `regenerate` **warns** if the on-disk source has drifted
since (no pin is possible, so drift is surfaced rather than enforced).

### D8 — Hash protection, conflict, and ignore interaction

Overlay output participates in the **existing** machinery, with no new mechanism
invented:

- Each rendered overlay file's SHA256 is recorded under the source's own `hashes`
  map ([D5](#d5--hash-storage-per-source-hashes-map)).
- On regenerate, an overlay output file modified by the operator since last
  generation triggers the **same `checkSkeletonConflict` / `promptOverwrite`
  flow** as embedded files — manual edits are protected identically.
- `runSkeletonPostProcessing` (go mod tidy / golangci-lint) and the subsequent
  `refreshProjectFileHashes` must also refresh overlay-file hashes, so
  post-processing edits to an overlaid Go file don't read as a user customisation
  next run.
- **`.gtb/ignore`** rules apply to overlay output paths exactly as to embedded
  ones (`rules.IsIgnored(relPath)` in the walk).
- For a `replaces:`-suppressed embedded scaffold
  ([D3](#d3--whole-scaffold-replacement-the-gtb-templateyaml-descriptor)), the
  suppressed embedded files are **not** hashed (they were never written); only the
  overlay's output is tracked. Removing the replacing source on a later regenerate
  restores the embedded scaffold (it re-enters the walk) — a clean, reversible
  swap.

### D9 — Fetch, caching, and offline/regenerate behaviour

- **Cache location.** `git` sources are cloned into a per-source, SHA-pinned cache
  under the **user XDG cache dir**
  (`$XDG_CACHE_HOME/gtb/templates/<host>/<owner>/<repo>@<sha>/`), keyed by the
  resolved SHA so a pin is immutable and shareable across projects. The cache is
  **never** the source of truth for output — it is only the input the renderer
  reads.
- **Offline / regenerate.** Because regenerate targets `resolved` (a SHA), a warm
  cache means **regenerate works fully offline**. An **offline cold cache for a
  `replaces`/overwriting source errors clearly** — GTB **never silently restores
  a suppressed embedded scaffold** (that would change output silently); instead it
  errors and tells the operator to restore connectivity or remove the source.
- **Integrity.** The resolved SHA is itself a content hash of the git tree; a
  fetched cache entry whose checked-out HEAD ≠ `resolved` is rejected.

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

1. **Path traversal on write.** A source file whose relative path is crafted to
   render to `../../etc/...` or an absolute path, escaping the project tree (the
   same sink class `getCommandPath` / `ValidateCommandName` already guard for
   command generation).
2. **Overwriting framework/security-critical files.** An overlay source silently
   shadowing `go.mod`, `.gtb/manifest.yaml`, or the signing trust keys —
   supply-chain injection into the generated tool. (Overwriting an ordinary
   embedded skeleton file is *intended* behaviour, user-wins; the denylist below
   carves out the security-critical paths that may **never** be overwritten.)
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
   ([D7](#d7--ref-pinning-record-both-the-ref-and-the-resolved-sha)).

### Posture and guardrails

- **Trusted-source posture (resolved default, not a sandbox).** Custom templates
  are treated as **operator-trusted input**, *not* sandboxed arbitrary code —
  analogous to a `Makefile`, a `Dockerfile`, or a git pre-commit hook the operator
  chose to run. The operator is responsible for the provenance of a source they
  add (the SHA pin gives them the means to vet a specific commit). The framework's
  job is to make the **blast radius bounded and the provenance explicit**, not to
  run hostile templates safely. A true sandbox is explicitly **out of scope**; a
  config-driven **host allowlist** is an optional later add, not v1
  ([O3](#open-questions)). The v1 guardrails are the hard controls below plus a
  loud first-use confirmation for remote sources.
- **Write-path containment (hard, always on).** Every rendered output path is
  resolved and checked (`filepath.Abs` + `filepath.Rel`) to sit **strictly under
  the project root**, reusing the AI-doc-tool / `getCommandPath` containment
  pattern. A path that escapes is a fatal error for that file.
- **Protected-path denylist (hard).** Overlay output may **never** write
  `.gtb/**` (manifest/ignore), `internal/trustkeys/**` (signing anchors), or
  `go.mod`/`go.sum` — even though the overlay otherwise overwrites freely. A
  `replaces:` suppression cannot reach these paths either; they are denied
  unconditionally.
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
  every `templates` entry (source type, location character class, ref/SHA shape)
  so a tampered manifest cannot drive a fetch or write outside the rules —
  mirroring how it already validates `Commands` and `Signing`. The source-side
  `gtb-template.yaml` is likewise validated (known `contract:` version, `replaces:`
  aliases restricted to the maintained set). Invalid entries are **skipped, not
  fatal**, on the regenerate path (consistent with
  [template-security.md](../development/template-security.md)).
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

Overlay templates render against a **documented, stable, metadata-only and
secret-free** subset of `skeletonTemplateData`, **versioned** so a source can
declare the contract version it targets via the `contract:` field of
`gtb-template.yaml`. GTB **rejects unknown contract versions**. Exposed fields
(all already public project metadata): `Name`, `Description`, `Repo`, `Host`,
`Org`, `RepoName`, `ModulePath`, `ReleaseProvider`, `GoVersion`,
`GoToolBaseVersion`, `EnvPrefix`, `EnabledFeatures`, `DisabledFeatures`,
`Private`, and the help/telemetry/signing *presence/shape* (not secrets).
**Explicitly excluded:** any resolved credential, env var, absolute host path, or
forge token. The contract is documented in `docs/components/` and frozen under the
pre-1.0 visibility rules; additive fields are safe, removals are a
contract-version bump.

## Regeneration & reproducibility

- The **SHA pin** (`resolved`, [D7](#d7--ref-pinning-record-both-the-ref-and-the-resolved-sha))
  is the cornerstone: regenerate fetches/reads `resolved`, never the moving
  `ref`, so output is byte-stable across time. A warm cache makes it fully
  offline; an offline cold cache for a `replaces`/overwriting source **errors
  clearly** rather than silently restoring the suppressed embedded scaffold
  ([D9](#d9--fetch-caching-and-offlineregenerate-behaviour)).
- **Deterministic layering** ([D6](#d6--collision-policy-user-wins-multi-source-layering)):
  embedded base → sources in manifest order; any `replaces:` suppression applies
  first; **last writer wins** for a shared path, with an info/debug log per
  override. The winner for every path is computable from the manifest list order
  alone, independent of map iteration order.
- **Hash protection** ([D8](#d8--hash-protection-conflict-and-ignore-interaction))
  extends unchanged: per-source `hashes`, `checkSkeletonConflict` on regenerate,
  `refreshProjectFileHashes` after post-processing.
- **Reversibility:** removing a source (or `gtb template remove`) drops its
  tracked output and **restores any embedded scaffold a `replaces:` had
  suppressed** (the embedded files simply re-enter the walk on the next
  regenerate).
- **`remove` (project removal)** treats overlay output like any other tracked file
  — no new behaviour, but the verification plan asserts it.

## CLI surface

Three entry points, all resolving to the same manifest representation:

- **Add at generate time:**
  `gtb generate project … --template <src>@<ref>` — repeatable; `<src>` is a
  forge path / URL / local path, `@<ref>` optional (defaults to the source's
  default branch, then pinned to its resolved SHA). The wizard offers an
  interactive "add a template source?" step. No `--mode`/`--scope` flags exist —
  the overlay needs none, and whole-scaffold suppression is declared in the
  source's `gtb-template.yaml`, not on the command line.
- **Manage on an existing project** (manifest-editing subcommands):
  - `gtb template add <src>@<ref> [--name …]` — appends a source, resolves the
    pin, renders, refreshes the manifest.
  - `gtb template update <name>` — re-resolves `ref` → new `resolved`, re-renders
    (the only pin-advancing path,
    [D7](#d7--ref-pinning-record-both-the-ref-and-the-resolved-sha)).
  - `gtb template remove <name>` — removes the source and its tracked output
    (restoring any embedded scaffold a `replaces:` had suppressed).
  - `gtb template list` — shows sources, refs, and resolved SHAs.
- **Pure manifest edit + `regenerate`** — hand-adding a `templates:` entry and
  running `regenerate` is a supported path; it goes through `ValidateManifest`
  ([Security model](#security-model)).

`gtb template …` is a new command group, but the manifest-edit + regenerate path
is the source of truth; the subcommands are ergonomics over it.

## Open questions

All open questions were resolved in the maintainer review of **2026-06-15**; the
Design, Security, CLI, and Regeneration sections above reflect the resolutions.
They are retained here with their resolutions for the record.

1. **O1 — "Partials to iterate over" scope.**
   *Resolved (2026-06-15):* **Per-file overlay (Reading A).** "Iterate over" means
   **walk the source tree and render each file once** to its mirrored relative
   path — not per-element iteration. The reserved root `README.md` and
   `gtb-template.yaml` are excluded from rendering. **Reading B** (per-command /
   per-feature iteration, `__command__` filename expansion, `.Command`/`.Feature`
   data fields) is **out of scope**.
2. **O2 — Override granularity.**
   *Resolved (2026-06-15):* Override granularity is **per-file by mirrored path —
   the overlay *is* the override** (a source file at a path that exists in the
   embedded assets overwrites it). For **whole embedded forge-CI suppression**,
   the source-side `gtb-template.yaml` `replaces:` list supersedes a named scaffold
   (`gitlab-ci`, `github-ci`). The earlier augment/override-**mode** and
   `ci`/`docs`/`meta` **region-scope** concepts are **removed** in favour of this.
3. **O3 — Security posture: trusted-source vs sandbox.**
   *Resolved (2026-06-15):* **Trusted-source posture, not a sandbox.** v1 controls:
   write-path containment under the project root, a protected-path denylist
   (`.gtb/**`, `internal/trustkeys/**`, `go.mod`), a restricted FuncMap (no
   file/exec/env/network), a metadata-only secret-free data contract, inert
   fetch/clone, `ValidateManifest` gating, and a first-use remote-source
   confirmation prompt. A config-driven **host allowlist is an optional later add,
   not v1**.
4. **O4 — Public-only vs private-forge in v1.**
   *Resolved (2026-06-15):* **Both** public (https/go-git) and private
   (provider-aware forge auth via `resolveForge` / `vcs.ResolveToken`) `git`
   sources ship in v1, **plus** local-folder sources.
5. **O5 — Local-source drift.**
   *Resolved (2026-06-15):* A `local` source **records a content fingerprint**;
   `regenerate` **warns** on drift (no SHA pin is possible for local).
6. **O6 — Hash storage shape.**
   *Resolved (2026-06-15):* **Per-source `hashes` map** under each `templates`
   entry (self-contained; clean removal), not folded into the top-level
   `Manifest.Hashes`.
7. **O7 — Cache location & offline policy.**
   *Resolved (2026-06-15):* Cache in the **XDG user cache** keyed by `@<sha>`
   (`$XDG_CACHE_HOME/gtb/templates/…@<sha>`). An **offline cold cache for a
   `replaces`/overwriting source errors clearly** — GTB **never silently restores**
   the suppressed embedded scaffold.
8. **O8 — CLI shape.**
   *Resolved (2026-06-15):* A **`--template <src>@<ref>` flag** on
   `generate project` **plus** a **`gtb template {add,update,remove,list}`** group
   **plus** the manifest-edit + `regenerate` path. No `--mode`/`--scope` flags.
9. **O9 — Data contract surface.**
   *Resolved (2026-06-15):* The data contract is **versioned** (the `contract:`
   field in `gtb-template.yaml`) and **metadata-only / secret-free**; GTB **rejects
   unknown contract versions**.
10. **O10 — Collision policy.**
    *Resolved (2026-06-15):* A per-file overlay collision with an embedded asset is
    **overwrite (user wins)**, not an error. Multiple user sources **layer in
    manifest order** (embedded base → source 1 → source 2 → …), **last writer
    wins**, with an info/debug log per override — not a hard error.

## Verification plan

1. **Unit — overlay add.** A `local` source adds a `SECURITY.md` at a path with no
   embedded counterpart; it renders against the data contract, lands at the
   mirrored path, and is hash-tracked under the source's `hashes`.
2. **Unit — overlay overwrite (user wins).** A source file whose mirrored path
   **also** exists in the embedded assets overwrites the embedded file; an
   info/debug log names the override; the source's content is the final output.
3. **Unit — whole-scaffold `replaces`.** A source carrying
   `gtb-template.yaml` with `replaces: [gitlab-ci]` (and `github-ci`) **suppresses**
   the embedded provider CI tree **before** the overlay renders; the suppressed
   embedded files are absent and untracked; the source's CI is the only CI present.
4. **Unit — multi-source layering.** Two sources sharing an output path resolve by
   **manifest order, last writer wins**, deterministically (independent of map
   iteration order); no hard error.
5. **Unit — write containment.** A source file rendering to `../escape` or an
   absolute path is rejected; no write occurs outside the tree.
6. **Unit — protected denylist.** A source attempting to write `.gtb/**`,
   `internal/trustkeys/**`, or `go.mod` is refused even though the overlay
   otherwise overwrites freely.
7. **Unit — restricted FuncMap.** Templates cannot reach a file/exec/env/network
   helper; the registered FuncMap exposes only escape + pure-format helpers.
8. **Unit — data contract is metadata-only.** No resolved credential / token /
   env / absolute path is reachable from an overlay template's context; an unknown
   `contract:` version is rejected.
9. **Unit — ref pin reproducibility.** Two renders against the same `resolved`
   SHA produce identical output; advancing `ref` only happens via
   `template update` and re-resolves the pin.
10. **Unit — local-source drift.** A `local` source records a content fingerprint;
    `regenerate` warns when the on-disk source has changed since generation.
11. **Unit — regenerate conflict.** A user-edited overlay output file triggers the
    same `checkSkeletonConflict`/`promptOverwrite` flow as embedded files; post-
    processing refreshes overlay hashes.
12. **Unit — reversibility.** Removing a source whose `gtb-template.yaml` had a
    `replaces:` restores the suppressed embedded scaffold on regenerate; removing a
    pure-overlay source drops only its files.
13. **Unit — ignore interaction.** `.gtb/ignore` suppresses an overlay output path
    exactly as it does an embedded one.
14. **Unit — manifest validation.** A tampered `templates` entry (bad type,
    traversal `location`, malformed ref) is rejected/skipped by `ValidateManifest`;
    a malformed `gtb-template.yaml` (unknown contract, unknown `replaces:` alias)
    is rejected.
15. **Integration (`INT_TEST_VCS=1`)** — a **public** `git` source clones at a ref
    and pins the SHA; a **private** source resolves auth from the configured forge
    subtree (token/ssh), against a local bare remote — extends
    `repo_integration_test.go`.
16. **Integration — offline regenerate** from a warm SHA-keyed cache renders with
    no network; a cold cache + no network for a `replaces`/overwriting source
    **errors clearly** ([D9](#d9--fetch-caching-and-offlineregenerate-behaviour)).
17. **E2E (Godog)** — `gtb template add … && regenerate` and `gtb template remove`
    user workflows (new CLI command group ⇒ Gherkin required).
18. **Docs** — `docs/components/generator` (the `templates` manifest block, the
    overlay add/overwrite model, the `gtb-template.yaml` descriptor and `replaces:`
    aliases, the data contract), a new threat-model section cross-referenced from
    `docs/development/template-security.md`, and a how-to for authoring a template
    source.

## Out of scope

- **Sandboxed execution of untrusted templates.** The posture is trusted-source
  with bounded blast radius ([O3](#open-questions)); a true sandbox (WASM,
  process isolation) is a separate, much larger capability.
- **A template *registry* / discovery service.** Sources are addressed by
  forge path / URL / local path; no central index, ratings, or signing of
  template repos (template-repo *signing* is a plausible later hardening, not v1).
- **Per-command / per-feature iteration (Reading B).** `__command__` filename
  expansion and per-element `.Command`/`.Feature` data fields are out of scope;
  the overlay walks and renders each file once (resolved O1).
- **Non-`text/template` engines** (Jinja, Handlebars, Go `html/template`). v1 is
  `text/template`, matching the embedded pipeline.
- **Overriding the Jennifer-generated Go files** (`main.go`, `cmd.go`,
  `version.go`). The overlay extends the *template-rendered* surface; the
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
  third generator spec; the embedded CI that an overlay can overwrite (or a
  `replaces: [gitlab-ci]` descriptor can suppress wholesale) is the subject of that
  refresh, so the two must agree on the `gitlab-ci` alias → embedded-paths map.
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
