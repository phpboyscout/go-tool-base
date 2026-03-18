---
title: Components
description: Overview of the reusable library components available in the pkg directory.
date: 2026-02-16
tags: [components, overview, libraries]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Components

The `pkg` directory contains the reusable library components that power `gtb` applications. These packages are designed to be modular, testable, and strictly typed.

## Core Components

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Props](props.md)** | `pkg/props` | The dependency injection container. Holds global state like configuration, logger, and filesystem interfaces. |
| **[Config](config.md)** | `pkg/config` | Robust configuration management wrapping generic Viper usage with type safety and interface-based testability. |
| **[Commands](commands/index.md)** | `cmd/` | Built-in Cobra commands for configuration (`init`), updates (`version`, `update`), interactive browser (`docs`), and agentic workflows (`mcp`). |
| **[Error Handling](error-handling.md)** | `pkg/errorhandling` | Centralized error reporting and formatting, ensuring consistent exit codes and log output. |

## Advanced Features

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Controls](controls.md)** | `pkg/controls` | Service orchestration and lifecycle management for long-running processes (e.g., servers, watchers). |
| **[Setup](setup/index.md)** | `pkg/setup` | bootstrapping logic for tool initialization, including GitHub authentication and self-updates. |
| **[Version Control](version-control.md)** | `pkg/vcs` | Abstractions for git operations and GitHub API interactions, handling enterprise auth complexity. |
| **[Chat](chat.md)** | `pkg/chat` | Multi-provider AI client (OpenAI, Anthropic, Gemini) for building intelligent features. |
| **[Docs](docs.md)** | `pkg/docs` | Logic for the interactive TUI documentation browser. |
| **[Forms](forms.md)** | `pkg/forms` | Multi-step interactive CLI form helpers with Escape-to-go-back navigation, built on charmbracelet/huh. |
| **[Utils](utils.md)** | `pkg/utils` | General-purpose utility functions for path resolution and system checks. |

## Testing Support

| Component | Package | Description |
| :--- | :--- | :--- |
| **[Mocks](mocks.md)** | `pkg/mocks` | Auto-generated Mockery definitions for all core interfaces to simplify unit testing. |

## Internal Development

- **[Internal Packages](internal/index.md)**: Documentation for the private `internal/` packages that power the CLI generator itself. (Contributors Only)
