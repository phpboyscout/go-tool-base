---
title: Generating Documentation
description: How to generate and maintain documentation for your CLI commands and packages using AI.
date: 2026-02-16
tags: [cli, documentation, generator, ai]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generating Documentation

The `generate docs` command is your secret weapon for maintaining world-class documentation with zero effort: it analyzes your Go source code with AI and produces comprehensive Markdown pages ready to serve. 📚✨

## Generate docs

Document a **command** or a **package** — exactly one of `--command` / `--package`:

```bash
# A command (by manifest path):
go run main.go generate docs --command "az/login"

# A library package:
go run main.go generate docs --package "pkg/utils"
```

The sections below cover the generator's capabilities in detail.

## Documentation layout (Diátaxis)

New projects scaffold a [Diátaxis](https://diataxis.fr/)-structured `docs/` tree, and `generate docs` places each page in the matching quadrant (for *why* GTB generates into this structure, see [Documentation Layout](../../explanation/concepts/documentation-layout.md)):

| Content | Quadrant | Path |
| :--- | :--- | :--- |
| CLI commands | Reference | `docs/reference/cli/<command>.md` (a leaf), or `docs/reference/cli/<command>/index.md` (a command with subcommands) |
| Library packages | Explanation | `docs/explanation/components/<package>.md` |

The layout is recorded as `docs_layout: diataxis` in `.gtb/manifest.yaml`. Projects generated before this feature default to the legacy **flat** layout (`docs/commands/`, `docs/packages/`); run `regenerate project --force` to migrate them — it moves existing pages into the quadrant tree (preserving your content), updates the manifest, and removes the old trees.

!!! note "No AI? Still structured"
    With `--agentless` (or no AI provider configured), `generate docs` writes deterministic boilerplate — a reference-shaped command page (description, usage, flags/subcommands tables, `--help` pointer) or an explanation skeleton for packages — so the docset is coherent without an API call.

## Core Features

### Portable Doc Generation 🚀

Documentation builds are handled by a portable Go generator. When called from a nested package (like `internal/cmd/root`), use the following pattern:

```go
//go:generate go tool docs --project-root ../../.. --target-dir pkg/cmd/root/assets
```

This tool:
- **Dual Content Sync**: Simultaneously synchronizes raw markdown for the TUI and builds a static site for `docs serve`.
- **Auto-Detection**: Automatically uses `zensical` (preferred) or `mkdocs` if available.
- **Configurable**:
    - `--project-root`: Point to your project sources (e.g., where `zensical.toml` or `mkdocs.yml` lives).
    - `--target-dir`: Specify where `assets/docs` and `assets/site` should be generated.
    - `--config-file`: Path to the site config file relative to the project root (default: `mkdocs.yml`).

### Command Documentation 🕹️

This command:

- **Agentic Inspection**: Uses AI tools to explore subcommands and referenced types autonomously.
- **Intelligent Formatting**: Produces structured Markdown with frontmatter, usage examples, and flag tables.
- **Smart Indexing**: Updates the CLI reference index (`docs/reference/cli/index.md`) automatically. zensical projects derive site navigation from the docs tree; legacy `mkdocs.yml` projects get their nav list rewritten.

### Package Documentation 📦

For developers building libraries, the `--package` flag generates **explanation-oriented** package docs (the Diátaxis explanation quadrant) — understanding, not an exhaustive API dump:

```bash
go run main.go generate docs --path . --package "pkg/utils"
```

The page lands in the `docs/explanation/components/` hierarchy and includes:

- A high-level overview of what the package is for and the problem it solves.
- The main exported types and their roles, described narratively.
- A short usage sketch synthesized from your code.
- An **API Reference** pointer — by default a local `go doc ./pkg/utils` hint, so a private or unpublished module never gets a dead link.

!!! tip "Published modules: link pkg.go.dev"
    Pass `--public-api` (or set `module_published: true` in `.gtb/manifest.yaml`) when your module is published. The API reference then links the package's `pkg.go.dev` page — the canonical home for the full Go API reference — instead of the `go doc` hint.

!!! warning "Required Flags"
    You must provide exactly one of `--command` or `--package` (the deprecated `--source` is the third member of the same one-required group) — they are mutually exclusive. The `--path` flag (project root) is optional and defaults to the current directory (`.`).

!!! note "Deprecated Flag"
    The `--source` flag is deprecated. Use `--command` instead.

### Iterative Refinement 🔄

The AI documentation generator isn't a one-and-done tool. It respects your manual edits!

If a documentation page already exists, the AI:

1. **Reads Existing Content**: Uses your manual tweaks as context.
2. **Preserves Customizations**: Merges new technical details with your hand-written sections.
3. **Maintains Authorship**: Authorship is **additive** — the AI model is appended to the `authors` list as a *co-author*, and your existing human author(s) are always preserved, never replaced.

!!! tip "Frontmatter-first output"
    Generated pages are guaranteed to begin with the YAML frontmatter fence (`---`). Any conversational preamble the model emits ahead of the frontmatter is stripped before the file is written, so static-site generators always parse the frontmatter. If a response contains no frontmatter at all, `generate docs` falls back to deterministic boilerplate rather than committing a broken page.

### Suppressing AI Attribution 🚫🤖

By default the generated frontmatter credits the AI model as an additive co-author. If your project's policy is to keep **no** AI/model attribution in committed docs, pass `--no-ai-attribution`:

```bash
go run main.go generate docs --command "az/login" --no-ai-attribution
```

This flips the generation prompt so the `authors:` field carries your project's human author(s) only — the model is instructed to add no AI, model, assistant, or tool identity. Existing human authors are still preserved.

## Advanced Usage

### Persistent AI Configuration

You can easily switch between AI providers or models using persistent flags:

```bash
go run main.go generate docs --command "az/login" --provider openai --model "gpt-5.4"
```

!!! tip
    Use the `--provider` and `--model` flags on the root `generate` command to set your preferences once for all subsequent generation tasks.

### Hierarchical Resolving

The tool intelligently resolves command paths. You can specify a deeply nested command, and the generator will find the correct source code and place the documentation in the matching folder structure.

```bash
go run main.go generate docs --command "az/keyvault/get"
```

## Why Automated Documentation?

Your code is the single source of truth and the docs are its reflection — generate them as part of your workflow and keep them in sync for free. For the design and how the generator fits the framework, see the [Docs component](../../explanation/components/docs.md).

Focus on building great software, and let `gtb` handle the story of how to use it! 🚀
