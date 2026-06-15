---
title: "Improve the generated project's default README.md"
description: "The generator's scaffolded README.md is a four-section stub (title, one-line, go install, --help). Replace the embedded default with a richer, GENERIC starter README that orients an engineer arriving at a freshly generated project: what it is, prerequisites, install, build & run via the scaffold's own just recipes, a Develop section (project layout, the .gtb/manifest.yaml + generate/regenerate model, config & env prefix), Documentation, Releasing, Contributing/conventions, and links into the GTB docs. Every command and path it references must actually exist in the scaffold; user-influenced values keep the existing escapeMarkdown/escapeMarkdownCodeBlock pipes; a custom-overlay can still override it."
status: IMPLEMENTED
date: 2026-06-15
tags:
  - specification
  - generator
  - readme
  - templates
  - docs
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Improve the generated project's default README.md

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-15

Status
:   IMPLEMENTED (open questions resolved in review 2026-06-15; embedded default README template rewritten and tested 2026-06-15 — install path corrected to `cmd/<name>`, "go deeper" links standardised on the published GTB docs site `https://phpboyscout.gitlab.io/go-tool-base/`)

## Summary

`gtb generate project` scaffolds a complete Go CLI tool — a `justfile` task
runner, `.golangci.yaml`, `.pre-commit-config.yaml`, `zensical.toml` docs, a
`.goreleaser.yaml`, provider-specific CI, the `pkg/cmd/...` command tree, and a
`.gtb/manifest.yaml` recording the generator state — but the `README.md` it
emits is a four-section **stub**: a `# {{ .Name }}` title, a one-line "built with
gtb" sentence, an `Installation` block (`go install {{ .Repo }}@latest`), and a
`Usage` block (`{{ .Name }} --help`). That is almost no help to an engineer who
clones the freshly-generated repo and wants to know what it is, how to build and
run it, how to develop on it, or where to go next.

This spec proposes replacing the embedded default
`internal/generator/assets/skeleton/README.md` with a **richer, generic starter
README** that orients a newcomer. It is deliberately **not** about the author's
actual product (the generator cannot know that) — it is a solid generic
jumping-off point that the author fills in. The hard constraint is **accuracy**:
every command (`just test`, `just lint`, `just docs-serve`, …) and every path
(`pkg/cmd/...`, `.gtb/manifest.yaml`, …) it references must actually exist in
the scaffold the generator ships. This is documentation-only at the template
level — no generator code logic changes are required beyond the template text
itself (and, if approved, one additive field rename/exposure note in
[D5](#d5--template-data-fields-already-available)).

## Motivation

The current template (verbatim) is:

```markdown
# {{ .Name }}

{{ .Name | escapeMarkdown }} is a tool built with [gtb](https://gitlab.com/phpboyscout/go-tool-base).

## Installation

​```bash
go install {{ .Repo | escapeMarkdownCodeBlock }}@latest
​```

## Usage

​```bash
{{ .Name | escapeMarkdownCodeBlock }} --help
​```
```

The README is the first file most engineers open. For a scaffolding tool whose
whole value proposition is "a complete, opinionated, batteries-included Go CLI
base", shipping a near-empty README undersells the scaffold and leaves the
developer to *discover* the `justfile`, the regeneration model, the env-prefix
config behaviour, and the docs site on their own. The scaffold already contains
everything a good "how to develop on this" section needs — it is simply not
surfaced. A better default README is pure DX upside and costs only template
text.

## Proposed README structure

The replacement template is organised as the sections below. Each section lists
the **template data fields** it consumes (all already present on
`skeletonTemplateData` unless flagged NEW — see
[D5](#d5--template-data-fields-already-available)) and is grounded in files the
scaffold actually ships.

1. **Title + one-liner + "built on gtb" note.**
   `# {{ .Name }}` then `{{ .Description | escapeMarkdown }}` as the lead
   sentence, followed by a "Built on [gtb](…)" note. Uses `.Name`,
   `.Description`. (The current template uses a hard-coded "is a tool built
   with gtb" sentence; switching the lead line to `.Description` makes the very
   first line useful, since the generator already defaults `.Description` to
   `"<name> utility"` when the operator gives none.)

2. **What is this? (placeholder).** A short, clearly-marked
   `<!-- TODO: describe … -->` / blockquote placeholder the author replaces with
   real product context. Generic prose around it explains it is a CLI built on
   the GTB framework. No fields (static), or echoes `.Description`.

3. **Prerequisites.** Go toolchain version (`.GoVersion`, already resolved by
   `resolveGoVersion` and present in the data), plus the optional dev tools the
   scaffold's recipes call (`just`, `golangci-lint`, `pre-commit`, `zensical`,
   `goreleaser`) noted as needed only for development. Uses `.GoVersion`.

4. **Install.** `go install {{ .ModulePath | escapeMarkdownCodeBlock }}/cmd/{{ .Name }}@latest`
   (the binary lives at `cmd/<name>/` per `generateSkeletonGoFiles`), with the
   existing `go install {{ .Repo }}@latest` form noted as the module-root
   alternative. Uses `.ModulePath` / `.Repo` / `.Name`. (NEW accuracy note: the
   current README installs `{{ .Repo }}@latest`, but the `main` package is under
   `cmd/<name>`; the corrected install path is part of this change.)

5. **Build & run.** The scaffold's **own** `just` recipes — `just` / `just build`
   (default recipe → `bin/{{ .Name }}`), then `./bin/{{ .Name }} --help`. Uses
   `.Name`. All recipe names verified against
   `internal/generator/assets/skeleton/justfile` (`build`, `install`, `snapshot`).

6. **Develop.** The largest section, grounded entirely in shipped files:
   - **Clone & verify**: `just test`, `just test-race`, `just lint`,
     `just check` (pre-commit), `just ci` — all real recipes.
   - **Project layout**: where commands live (`pkg/cmd/root/`, `cmd/{{ .Name }}/`),
     `internal/version/`, `pkg/cmd/root/assets/init/config.yaml`. Uses `.Name`.
   - **Regeneration model**: the `.gtb/manifest.yaml` contract and the
     `gtb generate command` / `gtb regenerate` flow, plus the
     **do-not-hand-edit generated files** caveat (hash-tracked; a manual edit is
     detected as a conflict on the next run). No fields (static).
   - **Config & env prefix**: configuration precedence and the
     `{{ .EnvPrefix }}` environment-variable prefix for overrides. Uses
     `.EnvPrefix`.
   - **Enabled built-ins**: a brief list of the active built-in commands rendered
     from `.EnabledFeatures`, via a small inline key→readable-name mapping in the
     template (`ai`→"AI chat", `config`→"Config management",
     `telemetry`→"Telemetry", `docs`→"Embedded docs", …). No large new computed
     data-struct field is required (resolved O2).

7. **Documentation.** The bundled `zensical.toml` docs site and `docs/` — author
   serves locally with `just docs-serve` (real recipe → `zensical serve`). Uses
   no fields (static), references the same `.Name`/`.Description` the
   `zensical.toml` already templates.

8. **Releasing (brief).** One short paragraph: releases run through the
   scaffolded CI (`.goreleaser.yaml` + the provider CI under `.github/` or
   `.gitlab/`), conventional-commit driven, with a link out to the deeper GTB
   release how-to rather than reproducing it. Uses `.ReleaseProvider` to pick
   GitHub-vs-GitLab phrasing/links (already in the data). This section ships by
   default as a brief paragraph plus a `.ReleaseProvider`-driven link out, not
   reproduced CI detail (resolved O3).

9. **Contributing / conventions.** Conventional Commits, run `just ci` before a
   PR, and the regen-owned-files caveat restated. Static.

10. **Links / "go deeper".** A short list of GTB documentation links (framework
    README, generator/regenerate concepts, config & env-prefix, testing,
    releasing) so a developer can dig in. Static links standardised on GTB's
    published documentation site (the zensical-pages docs site), falling back to
    the `gitlab.com/phpboyscout/go-tool-base` repo where a page isn't published;
    the exact site URL is read from the framework's `zensical.toml`/pages config
    (resolved O4).

### Template data fields already available

Every field the structure above needs is **already** on `skeletonTemplateData`
(`internal/generator/regenerate.go`) and passed to the skeleton text templates
via `generateSkeletonTemplateFiles` / `walkSkeletonAssets`:

| Field | Use in README |
|-------|---------------|
| `.Name` | Title, binary name, paths |
| `.Description` | Lead line, "what is this" |
| `.Repo` | Module-root install alternative |
| `.ModulePath` | `host/org/repo` install path |
| `.Host`, `.Org`, `.RepoName` | Links, ownership phrasing |
| `.GoVersion` | Prerequisites (Go version) |
| `.EnvPrefix` | Config / env-prefix section |
| `.EnabledFeatures`, `.DisabledFeatures` | "Enabled built-ins" list (via inline key→name map; resolved O2) |
| `.ReleaseProvider` | GitHub-vs-GitLab Releasing phrasing |

**No NEW data-struct field is required** — the richer README is achievable with
the existing contract. This is a deliberate scoping win: the change is
template-text-only. The single accuracy correction (install path under
`cmd/<name>`) is expressible with existing fields (`.ModulePath`, `.Name`). The
"Enabled built-ins" list (resolved O2) is rendered with a small inline
key→readable-name map over `.EnabledFeatures` in the template — no large new
computed field. A convenience `.BinaryPath`-style field is deliberately not
added (purely cosmetic — derivable inline as `cmd/{{ .Name }}`).

## Design decisions

### D1 — Templated, not static

The README stays a `text/template` asset rendered by
`renderAndHashSkeletonTemplate` like every other skeleton file, so per-project
values (`.Name`, `.Description`, `.GoVersion`, `.EnvPrefix`, `.ModulePath`) are
substituted at generation time. We do **not** ship a static README and patch it
post-hoc; the existing template pipeline already does exactly what we need.

### D2 — Accuracy constraint (only reference what the scaffold ships)

Every command and path the README mentions **must exist in the generated tree**.
The recipe names are taken verbatim from
`internal/generator/assets/skeleton/justfile` (`build`, `tidy`, `generate`,
`lint`, `lint-fix`, `test`, `test-race`, `test-integration`, `coverage`,
`bench`, `check`, `mocks`, `vuln`, `deadcode`, `install`, `docs-serve`, `ci`,
`snapshot`, `cleanup`). Paths are taken from `generateSkeletonGoFiles` /
`generateSkeletonTemplateFiles` (`cmd/<name>/main.go`, `pkg/cmd/root/...`,
`pkg/cmd/root/assets/init/config.yaml`, `internal/version/`, `.gtb/manifest.yaml`,
`docs/`, `zensical.toml`). The verification plan asserts this. The
`just` recipes always exist regardless of feature flags, so the template stays
**branch-free**: it simply avoids referencing feature-gated *tool subcommands* in
the README, and no `{{ if … }}` guards against `.EnabledFeatures` /
`.DisabledFeatures` are needed (resolved O5).

### D3 — Escaping (unchanged discipline)

User-influenced values keep the existing escaping pipes already used in the
template and its siblings: `| escapeMarkdown` for prose interpolation
(`.Description`, `.Name` in sentences) and `| escapeMarkdownCodeBlock` inside
fenced code blocks (`.ModulePath`, `.Repo`, `.Name` in `go install` / shell
lines), per `internal/generator/template_escape.go` and
`docs/development/template-security.md`. No new user-facing field class is
introduced, so no new escaper is needed. User values are **not** interpolated
into markdown table cells — they stay in prose/code-fence contexts already
covered by the existing escapers — so no dedicated pipe-guarding cell escaper is
required (resolved O6).

### D4 — Regeneration / hash interaction

The README participates in the normal manifest hash-tracking flow
(`renderAndHashSkeletonTemplate` → `collectedHashes` → `.gtb/manifest.yaml`).
Consequences, all already true of every templated skeleton file and simply
inherited here:

- On `gtb regenerate`, an **unmodified** README is re-rendered from the new
  template; a README the author has **edited** is detected as a conflict
  (stored-hash mismatch) and protected by the overwrite prompt
  (`checkSkeletonConflict`). This is the correct behaviour for a
  "fill-this-in" starter — the author's product description survives
  regeneration.
- Because the README is meant to be edited, the conflict-on-regenerate is the
  *expected* steady state, not a bug. The docs update for this change should say
  so explicitly (the README is yours to edit; regenerate will ask before
  overwriting).

### D5 — Custom overlay can override this default

This README is the **embedded DEFAULT**. Per the
[custom-partial-templates spec](2026-06-15-generator-custom-partial-templates.md),
a user overlay (local folder or ref-pinned git repo) whose tree contains a
`README.md` at the same relative path **overwrites** this embedded default
(user-wins is the overlay contract). So an organisation that wants a house-style
README ships one in its overlay and it simply replaces this file — no special
casing required. This spec only defines the **default**; the override mechanism
is owned by the overlay spec. The two compose cleanly because both render
through the same `text/template` engine with the same data, so an overlay README
can use the very same fields documented in
[§ fields already available](#template-data-fields-already-available).

### D6 — Generic, never product-specific

The README must remain a **generic** starter. It describes the *framework-shaped*
project (a GTB CLI: commands under `pkg/cmd`, the manifest/regen model, the
justfile tooling) and leaves a clearly-marked placeholder for the author's
product narrative. It must not invent product features, badges pointing at
non-existent CI, or commands that do not exist. "Generic + accurate" is the whole
design centre.

## Open questions

1. **O1 — Placeholder vs prose balance.** How much explicit "fill-this-in"
   placeholder (`<!-- TODO -->` / blockquote) versus framework prose? A heavy
   placeholder is honest but looks unfinished; heavy prose risks reading as
   product copy the author must delete. Proposed: one clearly-marked "What is
   this?" placeholder block, everything else accurate framework prose.

   *Resolved (2026-06-15):* one clearly-marked "What is this?" placeholder block
   for the author's product blurb; everything else is accurate framework prose.

2. **O2 — Expose `.EnabledFeatures` as a human list?** All raw fields needed are
   already exposed. Should we additionally render a friendly "Enabled built-ins"
   list (mapping `ai`/`config`/`telemetry`/`docs`/… to readable names) — which
   may warrant a small NEW computed field or a template helper — or omit the
   feature list from the README entirely? Default leaning: omit for v1
   (keep template-text-only), revisit if requested.

   *Resolved (2026-06-15):* **include** a brief "Enabled built-ins" list rendered
   from `.EnabledFeatures`, kept simple — a small inline key→readable-name mapping
   in the template (e.g. `ai`→"AI chat", `config`→"Config management",
   `telemetry`→"Telemetry", `docs`→"Embedded docs", …); no large new computed
   data-struct field is required. This is the one spot worth a touch more than
   pure static text, because it directly serves the "better jumping-off point"
   goal.

3. **O3 — Ship a Releasing/CI section by default?** Releasing detail is
   provider-specific and largely covered by the
   [gitlab-ci-refresh spec](2026-06-15-generator-gitlab-ci-refresh.md) and the
   GitHub workflow scaffold. Should the README carry a brief Releasing paragraph
   (with a `.ReleaseProvider`-driven link out) or just a single "see CI" line to
   avoid drift? Proposed: a brief paragraph + link, not reproduced detail.

   *Resolved (2026-06-15):* a **brief Releasing paragraph + a
   `.ReleaseProvider`-driven link out**, not reproduced CI detail (avoids drift
   with the gitlab-ci-refresh spec).

4. **O4 — Doc-link targets.** Which GTB doc URLs does the "go deeper" list point
   at, and are they stable enough to hard-code? (Framework README, generator /
   regenerate concept page, config & env-prefix, testing, releasing.) Need the
   canonical published doc base URL (the scaffold's `zensical.toml` /
   `docs/index.md` currently reference both
   `gitlab.com/phpboyscout/go-tool-base` and a `pages.gitlab.com/...` host —
   pick one).

   *Resolved (2026-06-15):* standardise the "go deeper" links on **GTB's
   published documentation site (the zensical-pages docs site), falling back to
   the `gitlab.com/phpboyscout/go-tool-base` repo** where a page isn't published.
   Resolve the scaffold's current two-host inconsistency in favour of the
   published docs site. The implementation reads the exact site URL from the
   framework's `zensical.toml`/pages config; the canonical target is the
   published GTB docs site, with the repo as fallback.

5. **O5 — Conditionalise feature-gated references?** Should mentions of
   feature-gated tooling (e.g. `just docs-serve`/docs when the `docs` built-in
   is disabled) be wrapped in `{{ if … }}` against `.DisabledFeatures`, or is the
   default scaffold (docs on) enough that we keep the template branch-free for
   readability? Proposed: keep branch-free unless a referenced command can
   genuinely be absent.

   *Resolved (2026-06-15):* **keep the template branch-free** — the `just`
   recipes always exist regardless of feature flags; simply avoid referencing
   feature-gated *tool subcommands* in the README, so no `{{ if … }}` is needed.

6. **O6 — Markdown-table escaping.** If user values (`.Name`, `.Description`)
   appear in a markdown table, do we need a cell-safe escaper (pipe-guarding) or
   should the template simply avoid interpolating user values into table cells?
   Proposed: avoid user values in tables; keep them in prose/code-fence contexts
   already covered by existing escapers.

   *Resolved (2026-06-15):* **avoid user values in tables** — keep `.Name` /
   `.Description` in prose/code-fence contexts already covered by the existing
   `escapeMarkdown` / `escapeMarkdownCodeBlock` pipes; no dedicated cell escaper
   is introduced.

7. **O7 — Correct the install path.** Confirm switching the install example to
   `cmd/<name>` (the actual `main` package location) — the current
   `go install {{ .Repo }}@latest` points at the module root, which has no
   `main`. This is an accuracy fix bundled into the README rework.

   *Resolved (2026-06-15):* **yes, correct it** to
   `go install <module>/cmd/<name>@latest` (the actual `main` package location);
   the current module-root path has no `main`. Bundled into the README rework.

## Verification plan

1. **Scaffold renders valid markdown.** `gtb generate project` into a temp dir
   produces a `README.md` that parses as valid markdown (no unbalanced fences,
   no unrendered `{{ … }}`), via a lightweight markdown lint / template-residue
   check in the generator tests.
2. **Every referenced command exists.** A test cross-checks each `just <recipe>`
   the README mentions against the recipes actually defined in the rendered
   `justfile`, and each referenced path against the generated tree — failing if
   the README names a recipe or path that does not exist (the accuracy
   constraint, [D2](#d2--accuracy-constraint-only-reference-what-the-scaffold-ships)).
3. **Field substitution.** The rendered README contains the expected `.Name`,
   `.Description`, `.GoVersion`, `.EnvPrefix`, and `.ModulePath` values and no
   leftover template delimiters.
4. **Escaping.** A project name/description containing markdown metacharacters
   renders without breaking layout or injecting markup (exercises the
   `escapeMarkdown` / `escapeMarkdownCodeBlock` pipes), mirroring existing
   template-security tests.
5. **Regenerate round-trips.** `gtb regenerate` on an **unmodified** generated
   project re-renders the README with no spurious conflict; an **edited** README
   is detected as a conflict and protected by the overwrite prompt
   ([D4](#d4--regeneration--hash-interaction)).
6. **Overlay override.** With an overlay supplying its own `README.md`
   (per the custom-partial-templates spec), the overlay README wins over the
   embedded default ([D5](#d5--custom-overlay-can-override-this-default)).
7. **Docs.** Update `docs/components/` (generator) / the generator how-to to note
   the richer default README, the edit-it-then-regenerate behaviour, and the
   overlay override path.

## Out of scope

- **The author's actual product content.** The README ships a generic starter
  and a placeholder; populating real product narrative is the author's job.
- **GitHub/GitLab CI specifics.** The provider CI scaffolds and their refresh are
  owned by the [gitlab-ci-refresh spec](2026-06-15-generator-gitlab-ci-refresh.md)
  and the GitHub workflow assets; the README only links out to them.
- **The overlay override mechanism itself.** Owned by the
  [custom-partial-templates spec](2026-06-15-generator-custom-partial-templates.md);
  this spec only defines the embedded default that an overlay may replace.
- **Restructuring `docs/index.md` / the zensical site.** This spec touches the
  repository `README.md` template only, not the bundled docs site content.
- **Adding badges** (build/coverage/version). Badges point at CI/registries that
  may not exist for a brand-new repo and would violate the accuracy constraint;
  deferred.

## Related

- [Generator custom/extensible template overlays](2026-06-15-generator-custom-partial-templates.md)
  — the overlay mechanism by which a user-supplied `README.md` overrides this
  embedded default ([D5](#d5--custom-overlay-can-override-this-default)).
- [Refresh the generator's GitLab CI to the phpboyscout/cicd component model](2026-06-15-generator-gitlab-ci-refresh.md)
  — owns the CI detail the README's Releasing section links out to rather than
  reproduces.
- `internal/generator/assets/skeleton/README.md` — the stub template this spec
  replaces.
- `internal/generator/assets/skeleton/justfile` — the source of truth for every
  `just` recipe the README may reference.
- `internal/generator/regenerate.go` — `skeletonTemplateData`, the field
  contract the README template renders against.
- `internal/generator/skeleton.go` — `generateSkeletonGoFiles` /
  `generateSkeletonTemplateFiles` / `renderAndHashSkeletonTemplate`, the render
  and hash-tracking pipeline and the source of truth for generated paths.
- `internal/generator/template_escape.go` and
  `docs/development/template-security.md` — the escaping discipline the README
  must follow.
