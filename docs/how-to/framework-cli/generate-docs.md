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
- **Smart Indexing**: Updates `docs/commands/index.md` and your site navigation (`zensical.toml` / `mkdocs.yml`) automatically!

### Package Documentation 📦

For developers building libraries, the `--package` flag is a game-changer:

```bash
go run main.go generate docs --path . --package "pkg/utils"
```

This creates "Developer Documentation" specifically tailored for Go packages, including:

- High-level architecture overviews.
- Exported type and function documentation.
- Usage examples synthesized from your code.
- Automatic inclusion in the `docs/packages/` hierarchy.

!!! warning "Required Flags"
    You must provide exactly one of `--command` or `--package` (the deprecated `--source` is the third member of the same one-required group) — they are mutually exclusive. The `--path` flag (project root) is optional and defaults to the current directory (`.`).

!!! note "Deprecated Flag"
    The `--source` flag is deprecated. Use `--command` instead.

### Iterative Refinement 🔄

The AI documentation generator isn't a one-and-done tool. It respects your manual edits!

If a documentation page already exists, the AI:

1. **Reads Existing Content**: Uses your manual tweaks as context.
2. **Preserves Customizations**: Merges new technical details with your hand-written sections.
3. **Maintains Authorship**: Appends the AI model to the `authors` list while preserving existing human authors.

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
