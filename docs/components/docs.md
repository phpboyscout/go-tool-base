---
title: Docs
description: Architecture of the documentation system, including the generation engine and TUI browser.
date: 2026-02-16
tags: [components, docs, documentation, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Docs

The `docs` component provides a terminal-based documentation browser and an AI-powered Q&A interface for your CLI tool.

## System Architecture

The `docs` component is built on top of the `generator` package and `pkg/chat` to provide a seamless documentation lifecycle. It consists of two primary subsystems:

### 1. Generation Engine (Build-Time)

The generation engine is invoked via `generate docs`. It uses an agentic AI loop to:

- **Parse Source**: Extracts metadata from command registration and implementation files.
- **Resolve Hierarchy**: Maps CLI command structures (e.g., `parent/child`) to matching filesystem structures (`docs/commands/parent/child/node.md`).
- **Index Management**: Automatically discovers and links new documentation nodes into the global navigation (`mkdocs.yml`) and specialized index pages.

### 2. TUI & Ask Interface (Run-Time)

The run-time interface resides in the generated binary and provides:

- **TUI Browser**: A Bubbles-based terminal UI for navigating embedded markdown. Features include split-pane view, asynchronous background search, and sidebar resizing.
- **Chat Integration**: High-level unmarshaling of natural language queries into RAG (Retrieval-Augmented Generation) context using the embedded assets.
- **AI Response Engine**: Specialized prompt engineering ensures high-quality, structured Markdown responses with clear hierarchies, headings, and lists.

## Integration Details

### Asset Structure

The system expects documentation files to be located at `assets/docs` within the provided `props.Assets` filesystem.

### Hierarchy & Navigation

When documenting commands, the system follows these placement rules:

- **Root Commands**: `docs/commands/[name].md`
- **Subcommands**: `docs/commands/[parent]/[child]/index.md`
- **Packages**: `docs/packages/[path]/[name].md`

This structure ensures that MkDocs remains organized and that navigation mirrors the actual CLI structure.

### Example Implementation

In your internal `cmd/root` package:

```go
package root

import (
	"embed"
    // ...
)

//go:generate sh -c "mkdir -p assets/docs && cp -r ../../../docs/* assets/docs/"
//go:embed assets/*
var assets embed.FS

func NewCmdRoot(...) *cobra.Command {
    p := &props.Props{
        Assets: props.NewAssets(props.AssetMap{"root": &assets}),
    }

    // Initialize the library root command
    rootCmd := root.NewCmdRoot(p)
    // ...
}
```

## Disabling Features

You can selectively disable the `docs` functionality in your tool configuration:

```go
props.Tool{
    Features: props.SetFeatures(
        props.Disable(props.DocsCmd),
    ),
}
```

!!! note
    Disabling the command removes the `docs` entry from your CLI but does not remove the embedded assets from your binary unless you also remove the `go:embed` directives.
