---
title: CLI Packages
description: Overview of the gtb CLI module's packages for contributors, covering the generator and agent systems.
date: 2026-02-16
tags: [components, cli, overview, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# CLI Packages

!!! warning "Not a supported API"
    The packages documented in this section live in the `cli/` module
    (`gitlab.com/phpboyscout/go-tool-base/cli`), under `cli/pkg/`, mirroring
    the `cmd/` + `pkg/cmd/` layout of a generated tool. They are importable by
    anything that requires the module, which every generated project does
    through its `tool` line, but they exist to build the `gtb` binary and are
    **not** a supported API: they change without notice and without a
    migration note.

    This documentation is provided for **contributors** to `gtb` to understand
    the core mechanics of the CLI generation and maintenance tools.

## Architecture Overview

The `cli/pkg` directory houses the business logic that powers the `gtb` CLI itself. While the framework's `pkg/` directory contains the re-usable libraries for *building* tools (props, config, etc.), `cli/pkg/` contains the logic for *generating* and *managing* them.

### Key Components

- **[Generator](generator.md)**: The engine room of the CLI. This package handles:

    -   **AST Manipulation**: Parsing and safely rewriting users' Go code to inject commands, flags, and imports without breaking existing logic.
    -   **Templating**: Rendering boilerplate code for new commands using `text/template`.
    -   **Manifest Management**: Reading and maintaining the `.gtb/manifest.yaml` source of truth.
    -   **Skeleton Generation**: Scaffolding complete new projects directory structures.

- **[Agent](agent.md)**: Defines the toolset and environment for the **Autonomous Repair Agent**. This package specifies what actions the AI can perform (build, test, lint, edit) during the self-healing code generation process.

- **[Commands](commands.md)**: Reference documentation for the internal CLI commands (`generate`, `regenerate`, `remove`) implemented in `cli/pkg/cmd`.

### Design Philosophy

The internal packages enable a "Code-First, Manifesto-Backed" approach:

1.  **Manifest-Driven**: The `manifest.yaml` is the source of truth for the CLI structure.
2.  **Verification-First**: Changes are verified by `golangci-lint` and test compilation before being finalized.
3.  **Non-Destructive**: The generator strives to preserve user edits in implementation files (`main.go`) while managing boilerplate (`cmd.go`) authoritatively.
