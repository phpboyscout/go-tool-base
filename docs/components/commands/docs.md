---
title: Docs Command
description: Launch the interactive terminal documentation browser with AI-powered Q&A.
date: 2026-02-16
tags: [components, commands, docs, tui]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Docs Command

The `docs` command launches an interactive terminal-based documentation browser.

## Usage

```bash
mytool docs [flags]
```

## Description

Launches a TUI for browsing embedded project documentation. It features a split-pane layout, asynchronous background search, and an AI-powered Q&A assistant. When the AI provider supports streaming (Claude, OpenAI, Gemini), answers appear progressively in the content viewport as the model generates them.

## Flags

| Flag | Description | Default |
| :--- | :--- | :--- |
| `--provider` | AI provider to use (`openai`, `claude`, `gemini`) | Auto-detected |

## TUI Keybindings

| Key | Action |
| :--- | :--- |
| `q` | Quit the browser |
| `Tab` | Toggle sidebar visibility |
| `s` | Open search input |
| `?` | Open AI Q&A input |
| `Esc` | Focus sidebar / Close search |
| `Enter` | Select item / Focus content |

## Docs Serve Subcommand

The `docs serve` subcommand starts a local HTTP server to preview the documentation as a static site.

**Usage:**
```bash
mytool docs serve [flags]
```

**Description:**
Start a local HTTP server and serve the documentation as a Material-styled static site. By default, it automatically opens the local URL in your default browser.

**Flags:**
| Name | Description | Default |
| :--- | :--- | :--- |
| `-p, --port` | Port to listen on (0 for random) | `8080` |
| `--open` | Automatically open the browser | `true` |

> [!NOTE]
> The `serve` command is only available if the static site assets have been pre-built and embedded into the binary.

## Docs Ask Subcommand

The `docs ask` subcommand allows you to query the documentation directly from the command line without launching the TUI.

**Usage:**
```bash
mytool docs ask "How do I configure logging?"
```

**Aliases:** `?`

**Description:**
Ask a question about the documentation and receive an AI-generated answer rendered in the terminal. This is useful for quick lookups or when running in non-interactive environments.

When `--no-style` is set and the provider supports streaming, the answer is printed to stdout as each token arrives. In styled mode (default), the full response is collected and rendered as formatted Markdown at the end.

**Flags:**

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-n, --no-style` | Disable markdown styling; enables live token streaming to stdout | `false` |
| `--provider` | AI provider to use (`openai`, `claude`, `gemini`) | Inherited from parent |

**Examples:**
```bash
# Ask a question with styled output (full response rendered at completion)
mytool docs ask "What is the Props container?"

# Ask without markdown formatting — streams tokens live as they arrive
mytool docs ask --no-style "List all available commands"

# Pipe streamed output
mytool docs ask --no-style "List all available commands" | grep init

# Use a specific AI provider
mytool docs ask --provider claude "Explain the configuration system"
```

## Implementation

The docs command is implemented in `cmd/docs/docs.go` and utilizes the `pkg/docs` library for TUI rendering and search.
