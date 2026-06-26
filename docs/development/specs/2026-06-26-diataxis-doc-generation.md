---
title: "Diátaxis-Structured Documentation Generation"
description: "Make the gtb doc generator scaffold and emit documentation in the Diátaxis structure (tutorials / how-to / reference / explanation) by default — quadrant-aware output paths, prompts, skeleton tree, and nav — so tools built on GTB get well-structured docs the way go-tool-base's own docs now are. Neutral by default: no off-site, public-module, or marketing assumptions."
date: 2026-06-26
status: APPROVED
tags:
  - specification
  - generator
  - documentation
  - diataxis
  - templates
  - ai
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Diátaxis-Structured Documentation Generation

## Context

go-tool-base's own documentation was restructured to follow the
[Diátaxis](https://diataxis.fr/) framework (with the hybrid docsite/marketing
concessions captured in the `diataxis-docs` skill). The framework's *generator*,
however, still scaffolds and emits docs in a pre-Diátaxis layout. A newly
generated tool gets:

- a bare `docs/index.md` skeleton (`internal/generator/assets/skeleton/docs/`);
- AI-generated **command** docs written to `docs/commands/<parent>/<name>/index.md`
  (`prepareDocsContext`, `internal/generator/docs.go`);
- AI-generated **package** docs written to `docs/packages/<relPath>/index.md`;
- nav generated from that flat tree (`GenerateNavFromFS`).

So tools built on GTB do **not** inherit the structure GTB now models for itself.
This spec makes the Diátaxis layout the generator's default — **neutrally**: it
must not assume off-site tutorials, a publicly published module, or a marketing
site. Those are project-specific choices the generated tool's author opts into.

## Goals

1. A generated project scaffolds a Diátaxis docs tree: `how-to/`, `reference/`,
   `explanation/`, and a `tutorials/` index, with a neutral landing and a working
   nav.
2. `generate docs` places generated content in the correct quadrant by default:
   command docs → `reference/cli/`, package docs → `explanation/components/`.
3. The doc-gen prompts produce **quadrant-appropriate** content (reference = dry
   facts; explanation = narrative/architecture).
4. The behaviour is consistent with the `diataxis-docs` skill.

## Non-goals

- Re-authoring or auto-migrating an existing downstream project's docs (except via
  an explicit `--force`, below).
- Changing the TUI docs browser or `docs serve` rendering.
- Generating tutorial *content* to a guaranteed-correct standard (best-effort
  only, below).

## Current behaviour (baseline)

| Surface | Today | File |
|---|---|---|
| Skeleton docs | single `docs/index.md` | `internal/generator/assets/skeleton/docs/` |
| Command doc output | `docs/commands/<parent>/<name>/index.md` | `prepareDocsContext` |
| Package doc output | `docs/packages/<relPath>/index.md` | `prepareDocsContext` |
| AI prompts | `commandDocumentationSystemPrompt`, `packageDocumentationSystemPrompt` | `internal/generator/docs.go` |
| Nav | generated from the flat tree | `GenerateNavFromFS` (`pkg/docs`) |

## Resolved design decisions

All six open questions raised during review are resolved:

1. **Hard default, no flag.** The Diátaxis layout is unconditional for new
   projects; there is no `--docs-layout` flat escape hatch. The old flat layout is
   not a maintained option.
2. **Leaf-flat / parent-subsection.** A command with no subcommands →
   `reference/cli/<path>/<name>.md`. A command with subcommands →
   `reference/cli/<path>/<name>/index.md`, its children beside it as
   `…/<name>/<child>.md`. This maps 1:1 to the generator's per-command output and
   makes command groups expandable nav subsections.
3. **Leave existing projects as-is; `--force` migrates.** The manifest records the
   layout (`docs_layout: diataxis`) and the generating gtb version; both are used
   to detect an existing project's layout. By default `regenerate` writes to the
   project's existing layout (no surprise moves, no duplicate files, no lost
   customizations). `regenerate project --force` performs the migration: re-emit
   docs in the Diátaxis layout, remove the old `docs/commands/` + `docs/packages/`
   trees and stale nav, and stamp `docs_layout: diataxis`. Ship a migration note in
   `docs/reference/migration/`.
4. **Neutral, no assumptions.**
   - *Tutorials:* the scaffolded `tutorials/index.md` is **neutral** — it explains
     the quadrant and offers both paths (write tutorials locally **or** curate
     links to externally-hosted ones), with no off-site assumption. A **best-effort**
     tutorial-generation pass may be offered, but gated/caveated (learning content
     has a high correctness bar the generator can't guarantee).
   - *Reference (code API):* **do not assume the module is public.** pkg.go.dev only
     resolves for published modules, so by **default the API reference is stubbed**
     (narrative + a "run `go doc` locally" hint, **no** pkg.go.dev link). An
     **opt-in** (`--public-api` flag and/or a manifest `module_published: true`
     property) enables pkg.go.dev links for genuinely public modules.
5. **Structure never depends on AI.** With `--agentless` / no AI, still scaffold
   the full quadrant tree and emit the **deterministic** boilerplate we can build
   from generate-time info (usage + flags/subcommands tables from the manifest /
   cobra tree). Explanation pages with no AI become stubs (purpose line + TODO). AI
   only *enriches* prose.
6. **Neutral landing, marketing is opt-in.** The scaffold leaves a
   "marketing-shaped hole" for the author to fill — it does **not** fill it. The
   landing `index.md` is a plain, useful docs home (overview + quadrant links), and
   there is **no** `about/` section by default. Teams running a marketing site dress
   up the landing and add `about/` themselves (the `diataxis-docs` skill documents
   how).

## Design

### 1. Skeleton Diátaxis tree (neutral)

Replace the bare `skeleton/docs/index.md` with:

```
docs/
  index.md                 # neutral docs landing: overview + links into the quadrants
  getting-started.md       # universal on-ramp
  how-to/index.md
  reference/
    index.md
    cli/index.md
    config/index.md
  explanation/
    index.md
    components/index.md
    concepts/index.md
  tutorials/index.md        # neutral: write locally, or curate links out
```

Each `index.md` is a short populated stub explaining what the quadrant is for, so
the site is coherent before any `generate docs` run. No `about/`; the landing is
neutral (easy to make marketing, but not marketing by default).

### 2. Quadrant-aware output paths

In `prepareDocsContext`, by the project's recorded `docs_layout`:

- **diataxis** (new default): command → `docs/reference/cli/<path>/<name>.md`
  (leaf) or `…/<name>/index.md` (has children); package →
  `docs/explanation/components/<relPath>.md`.
- **flat** (existing projects): unchanged, unless `--force` migrates.

### 3. Quadrant-aware prompts (public-module-aware)

Update `commandDocumentationSystemPrompt` and `packageDocumentationSystemPrompt`:

- **Command (reference):** austere, complete reference — usage, a flags table,
  subcommands, args, a `--help` pointer. No rationale, no tutorials. (Deterministic
  even without AI.)
- **Package (explanation):** narrative/architecture — purpose, key types and how
  they fit, a usage sketch. **API-reference deferral is conditional:** with
  `--public-api`/`module_published`, link pkg.go.dev; otherwise stub the API
  reference (no dead links) and suggest `go doc` locally.
- Both embed the relevant `diataxis-docs` principles so the model stays in-quadrant.

### 4. Nav generation

`GenerateNavFromFS` (and any nav written to `zensical.toml`) reflects the quadrant
tree and renders subsection groups (a directory with `index.md` + topic pages
becomes an expandable group, as on go-tool-base today).

### 5. Consistency with the skill

The generator's prompts and scaffold are the executable counterpart of the
`diataxis-docs` skill. Factor shared structural rules where practical so the two
don't drift.

## Implementation plan (TDD)

1. **Manifest layout field** — add `docs_layout` (+ read the generating gtb
   version); default existing projects to `flat`, new to `diataxis`. Tests.
2. **Skeleton tree** — add the neutral quadrant scaffold; test `generate project`
   produces the tree + a valid nav.
3. **Output-path mapping** — `prepareDocsContext` honours `docs_layout` and the
   leaf-flat/parent-subsection rule; tests for command/package/nested.
4. **`--force` migration** — re-emit in Diátaxis, remove old trees + stale nav,
   stamp the manifest; test the before/after.
5. **Prompts** — quadrant-appropriate output + conditional pkg.go.dev via
   `--public-api`/`module_published`; assert on prompt content.
6. **Agentless boilerplate** — deterministic flags/subcommands tables + stubs with
   no AI; tests.
7. **Nav** — quadrant tree + subsections; tests.
8. **Docs + skill** — update the framework-cli how-tos and generator explanation;
   add the migration note; cross-check the `diataxis-docs` skill.

## Affected files

- `internal/generator/assets/skeleton/docs/**` (new neutral tree)
- `internal/generator/docs.go` (`prepareDocsContext`, prompt constants, layout)
- `pkg/docs/docs.go` (`GenerateNavFromFS`) + nav writing
- `internal/cmd/generate/docs.go` and `regenerate` (`--force`, `--public-api`)
- manifest schema (`docs_layout`, optional `module_published`)
- `docs/how-to/framework-cli/*`, the generator explanation, and a
  `docs/reference/migration/` note

## Testing strategy

Unit tests for layout detection, path mapping, prompt content, agentless
boilerplate, and nav generation. An integration test runs `generate project` +
`generate docs` into a temp dir and asserts the quadrant tree, output locations,
and a building nav; plus a `--force` migration test. ≥90% coverage on new `pkg/`
code (nav helper changes). E2E (Godog) optional for the scaffold flow.

## Risks

- **LLM nondeterminism** — assert on the *prompt*, not generated prose; keep the
  agentless/boilerplate path well-covered.
- **Migration data-loss** — `--force` deletes the old trees, so it must preserve
  any hand-written content it can detect, and the migration note must warn to
  commit first.
- **Tutorial accuracy** — best-effort generation is explicitly caveated; never
  present generated tutorials as verified.
