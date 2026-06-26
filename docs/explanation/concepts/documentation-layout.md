---
title: Documentation Layout (Diátaxis)
description: Why GTB scaffolds and generates documentation in the Diátaxis structure, how content maps to the four quadrants, and how to opt in or out.
date: 2026-06-26
tags: [concepts, documentation, diataxis, generator, layout]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Documentation Layout (Diátaxis)

When `gtb` scaffolds a project and generates documentation for it, the output is
organised according to [Diátaxis](https://diataxis.fr/) — a systematic way of
structuring technical documentation around what the reader is actually trying to
do. This page explains *what* that structure is and *why* GTB generates into it,
so you understand the shape of your tool's `docs/` tree rather than just following
the commands that produce it.

## What Diátaxis is

Diátaxis observes that documentation serves four distinct needs, and that mixing
them on one page serves none of them well. It separates content into four
quadrants:

| Quadrant | Orientation | Answers | In your `docs/` tree |
| :--- | :--- | :--- | :--- |
| **Tutorials** | Learning | "Teach me, start to finish." | `docs/tutorials/` |
| **How-to guides** | Tasks | "How do I accomplish X?" | `docs/how-to/` |
| **Reference** | Information | "What exactly is X?" | `docs/reference/` |
| **Explanation** | Understanding | "Why does X work this way?" | `docs/explanation/` |

The value is in the separation. A reference page can be terse and exhaustive
because it is not also trying to teach; a tutorial can take its time because it is
not also trying to be a complete reference. A reader who knows which question they
are asking knows exactly where to look.

## Why GTB generates into it

GTB builds *many* tools from one framework, and generated documentation is only
useful if it is consistent and lands in a predictable place. Generating into the
Diátaxis structure gives three concrete benefits:

- **Each generated artefact has a correct home.** A CLI command is *reference*
  material — terse, factual, exhaustive. A package overview is *explanation* —
  narrative, about purpose and design. Putting them in the same flat bucket
  (`docs/commands/`, `docs/packages/`) blurs that distinction; the quadrants make
  it explicit.
- **Consistency across every tool.** Anyone who has read the docs for one
  GTB-based tool can navigate any other, because the top-level shape is the same —
  the same property that GTB already enforces for the [project filesystem
  layout](project-structure.md).
- **It composes with the static site.** The scaffolded `zensical.toml` derives the
  site navigation directly from the `docs/` tree, so the quadrant directories
  become the navigation sections with no extra wiring.

## How content maps to the quadrants

`gtb generate docs` (and the documentation step of `generate command`) places each
generated page in the quadrant that matches its kind:

| Generated content | Quadrant | Path |
| :--- | :--- | :--- |
| CLI commands | Reference | `docs/reference/cli/<command>.md` (a leaf), or `docs/reference/cli/<command>/index.md` (a command that has subcommands) |
| Library packages | Explanation | `docs/explanation/components/<package>.md` |

The leaf-vs-subsection rule keeps a command and its children together: a leaf
command is a single flat file, while a command with subcommands becomes a section
whose `index.md` sits beside the child pages.

The generator deliberately fills only the two quadrants it can author truthfully
from your code — **reference** (what your commands are) and the
**explanation of components** (what your packages do). The remaining quadrants are
scaffolded as neutral stubs and left to you:

- **Tutorials** and **how-to guides** are yours to write; the generator never
  invents a learning path it cannot verify. (GTB's own tutorials live off-site on
  the project blog, and your tool is free to do the same — the scaffolded stubs are
  neutral about where the long-form learning content ultimately lives.)
- The **landing page** is a marketing-shaped hole for you to fill, not something
  the generator fakes.

## Package API reference: pkg.go.dev vs `go doc`

The generated package page is *explanation*, not an API dump — it describes the
package's purpose and key types narratively, then points at the full API reference
rather than pasting it. Where it points depends on whether your module is
published:

- **Published** (you pass `--public-api`, or set `module_published: true` in the
  manifest): the page links the package's `pkg.go.dev` page — the canonical home
  for the full Go API reference.
- **Unpublished / private** (the default): the page suggests `go doc` locally, so a
  module with no registry page never gets a dead link.

`--public-api` is persisted to the manifest the first time you use it, so the
choice survives future regenerations.

## Opting in, and the `docs_layout` setting

The layout is recorded as `docs_layout` in `.gtb/manifest.yaml`:

- **`diataxis`** — the default for newly scaffolded projects.
- **`flat`** — the legacy layout (`docs/commands/`, `docs/packages/`), kept for
  projects generated before this became the default. Projects with no
  `docs_layout` recorded are treated as flat.

A flat-layout project keeps its layout on every ordinary `regenerate` — GTB never
moves a downstream author's docs tree out from under them. To migrate, run
`regenerate project --force`, which relocates existing pages into the quadrant
tree (preserving your hand-written content), stamps `docs_layout: diataxis`, and
removes the old trees. See
[Regenerating Components](../../how-to/framework-cli/regenerate-components.md)
for the procedure.

## See also

- [Generating Documentation](../../how-to/framework-cli/generate-docs.md) — the how-to for `generate docs`.
- [Integrated Documentation](../components/docs.md) — the docs engine, TUI browser, and `docs serve`.
- [Diátaxis](https://diataxis.fr/) — the framework itself, by Daniele Procida.
