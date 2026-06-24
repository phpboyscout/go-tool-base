---
title: Internal Packages
description: Overview of internal packages for contributors, covering the generator and agent systems.
date: 2026-02-16
tags: [components, internal, overview, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Internal Packages

!!! warning "Internal Use Only"
    The packages documented in this section reside in the `internal/` directory. By Go convention, they are **not accessible** to external consumers of this library.

    This documentation is provided solely for **contributors** to `gtb` to understand the core mechanics of the CLI generation and maintenance tools. These interfaces are subject to change without notice.

## Architecture Overview

The `internal` directory houses the business logic that powers the `gtb` CLI itself. While the root `pkg/` directory contains the re-usable libraries for *building* tools (props, config, etc.), `internal/` contains the logic for *generating* and *managing* them.

### Key Components

- **[Generator](generator.md)**: The engine room of the CLI. This package handles:

    -   **AST Manipulation**: Parsing and safely rewriting users' Go code to inject commands, flags, and imports without breaking existing logic.
    -   **Templating**: Rendering boilerplate code for new commands using `text/template`.
    -   **Manifest Management**: Reading and maintaining the `.gtb/manifest.yaml` source of truth.
    -   **Skeleton Generation**: Scaffolding complete new projects directory structures.

- **[Agent](agent.md)**: Defines the toolset and environment for the **Autonomous Repair Agent**. This package specifies what actions the AI can perform (build, test, lint, edit) during the self-healing code generation process.

- **[Commands](commands.md)**: Reference documentation for the internal CLI commands (`generate`, `regenerate`, `remove`) implemented in `internal/cmd`.

### Design Philosophy

The internal packages enable a "Code-First, Manifesto-Backed" approach:

1.  **Manifest-Driven**: The `manifest.yaml` is the source of truth for the CLI structure.
2.  **Verification-First**: Changes are verified by `golangci-lint` and test compilation before being finalized.
3.  **Non-Destructive**: The generator strives to preserve user edits in implementation files (`main.go`) while managing boilerplate (`cmd.go`) authoritatively.
