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
- **Index Management**: Automatically discovers and links new documentation nodes into the global navigation (`zensical.toml` / `mkdocs.yml`) and specialized index pages.

### 2. TUI & Ask Interface (Run-Time)

The run-time interface resides in the generated binary and provides:

- **TUI Browser**: A Bubbles-based terminal UI for navigating embedded markdown. Features include split-pane view, asynchronous background search, and sidebar resizing.
- **Chat Integration**: RAG (Retrieval-Augmented Generation) over the embedded assets — all documentation is injected into the system prompt for accurate, context-aware answers.
- **Streaming Responses**: When the selected provider supports streaming (Claude, OpenAI, Gemini), the AI answer appears in the TUI viewport as it is generated rather than after a full round-trip. `ProviderClaudeLocal` falls back to a non-streaming response.
- **AI Response Engine**: Specialized prompt engineering ensures high-quality Markdown responses with clear headings, lists, and consistent terminology.

### 3. Static Site Server (`docs serve`)

When the binary embeds a pre-built Material/Zensical static site (`assets/site`), the `docs serve` subcommand starts a local HTTP server for it.

- **Loopback by default**: the server binds `127.0.0.1` so the locally-served site is **not** reachable from the network. The startup log reports the address the listener actually bound to (including the resolved port when `--port 0` is used) rather than a hard-coded `localhost`.
- **Widening the bind**: pass `--host 0.0.0.0` (or another interface, e.g. `--host ::`) to expose the server on all interfaces. Only do this on a trusted network. The library equivalent is the additive `docs.WithHost` option to `docs.Serve`.
- **Flags**: `--port` / `-p` (default `8080`, `0` for a random port), `--host` (default `127.0.0.1`), and `--open` (auto-open the browser; skipped when `--port 0` since the bound port is not known to the caller).
- **Standard error path**: the command runs as `RunE` and flows through the recovery/timing/telemetry middleware chain like every other built-in, surfacing failures via the structured `ErrorHandler`.

## Man-page generation

In addition to Markdown docs and the TUI browser, `pkg/docs` renders
standards-compliant **roff man pages** directly from the live Cobra command
tree via `github.com/spf13/cobra/doc`. This is the artefact Linux packages
(`.deb`/`.rpm`) and Homebrew formulae install under `/usr/share/man`.

Pages are rendered from the live tree (never embedded in the binary), so they
cannot drift from the actual command set. The single library seam is:

```go
// pkg/docs/man.go
func GenerateManTree(root *cobra.Command, opts docs.ManOptions) error
```

`ManOptions` controls the output directory and `.TH` header policy:

| Field | Purpose |
| :--- | :--- |
| `Dir` | Output base; pages are written under `Dir/man<section>/<command-path>.1` |
| `Title` | Fixed `.TH` title; blank lets cobra derive a per-command title (recommended) |
| `Section` | Man section (default `1`) |
| `Source` / `Manual` | `.TH` footer (e.g. `gtb 0.21.0`, `GTB Manual`); always set so the cobra placeholder never ships |
| `Date` | `nil` suppresses the auto-gen trailer for byte-reproducible output (honours `SOURCE_DATE_EPOCH`); set to stamp a date |

Two thin command surfaces call this one seam:

- **`gtb generate man [--dir ./man] [--section] [--source] [--manual] [--date]`** —
  the build-time step for CI and packaging. Respects the generator's
  `--dry-run` (lists intended files instead of writing). A `just man` recipe
  wraps it.
- **`<tool> man [--dir]`** — a hidden, opt-in runtime command (default-off
  `props.ManCmd` feature). With `--dir` it writes the tree; without it, the
  tool's top-level page is printed to stdout for preview
  (`mytool man | man -l -`). Enable it with
  `props.SetFeatures(props.Enable(props.ManCmd))`.

Man-page generation is **deterministic** — there is no AI involvement, unlike
`generate docs`. Long descriptions come from each command's existing
`Long`/`Short`/`Example` fields.

## Integration Details

### Asset Structure

The system expects documentation files to be located at `assets/docs` within the provided `props.Assets` filesystem.

### Hierarchy & Navigation

When documenting commands, the system follows these placement rules:

- **Root Commands**: `docs/commands/[name].md`
- **Subcommands**: `docs/commands/[parent]/[child]/index.md`
- **Packages**: `docs/packages/[path]/[name].md`

This structure ensures that the documentation site remains organized and that navigation mirrors the actual CLI structure.

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
