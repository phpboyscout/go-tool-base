---
title: Generating Documentation
description: How to generate and maintain documentation for your CLI commands and packages using AI.
date: 2026-02-16
tags: [cli, documentation, generator, ai]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generating Documentation

### Intelligent Documentation with AI

The `generate docs` command is your secret weapon for maintaining world-class documentation with zero effort. By leveraging advanced AI, it analyzes your Go source code and produces comprehensive, high-quality Markdown pages that are ready to be served via MkDocs.

Whether you're documenting a complex command hierarchy or a critical library package, this tool ensures your documentation is always accurate, insightful, and beautifully formatted. 📚✨

## Core Features

### 1. Portable Doc Generation 🚀

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

### 2. Command Documentation 🕹️

This command:

- **Agentic Inspection**: Uses AI tools to explore subcommands and referenced types autonomously.
- **Intelligent Formatting**: Produces structured Markdown with frontmatter, usage examples, and flag tables.
- **Smart Indexing**: Updates `docs/commands/index.md` and your site navigation (`zensical.toml` / `mkdocs.yml`) automatically!

### 2. Package Documentation 📦

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
    The `--path` flag (path to project root) is **required**. You must also provide exactly one of `--command` or `--package` — they are mutually exclusive.

!!! note "Deprecated Flag"
    The `--source` flag is deprecated. Use `--command` instead.

### 3. Iterative Refinement 🔄

The AI documentation generator isn't a one-and-done tool. It respects your manual edits!

If a documentation page already exists, the AI:

1. **Reads Existing Content**: Uses your manual tweaks as context.
2. **Preserves Customizations**: Merges new technical details with your hand-written sections.
3. **Maintains Authorship**: Appends the AI model to the `authors` list while preserving existing human authors.

## Advanced Usage

### Persistent AI Configuration

You can easily switch between AI providers or models using persistent flags:

```bash
go run main.go generate docs --command "az/login" --provider openai --model "gpt-4"
```

!!! tip
    Use the `--provider` and `--model` flags on the root `generate` command to set your preferences once for all subsequent generation tasks.

### Hierarchical Resolving

The tool intelligently resolves command paths. You can specify a deeply nested command, and the generator will find the correct source code and place the documentation in the matching folder structure.

```bash
go run main.go generate docs --command "az/keyvault/get"
```

## Why Automated Documentation?

Keeping documentation in sync with code is traditionally a painful, manual process. `generate docs` transforms this into a delightful experience:

- **Single Source of Truth**: Your code is the source; the docs are the reflection.
- **Low Friction**: Generate docs as part of your development workflow.
- **High Quality**: AI-driven summaries often provide insights that manual writing might miss.

Focus on building great software, and let `gtb` handle the story of how to use it! 🚀
