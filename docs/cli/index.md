---
title: CLI Overview
description: Overview of the GTB CLI, its core principles, and available generation tools.
date: 2026-02-16
tags: [cli, overview]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# CLI Overview

Welcome to the **gtb CLI**! 🚀

Our command-line tool is designed to be your best friend when building powerful, well-structured Go applications. It takes the heavy lifting out of boilerplate generation, allowing you to focus on what truly matters: **your unique logic**.

<video controls autoplay loop muted playsinline width="100%">
  <source src="../tapes/basic-demo.mp4" type="video/mp4">
</video>

## Why use the gtb CLI?

- **Consistency**: Ensures all your tools follow a standard, battle-tested structure.
- **Speed**: Go from an idea to a running command in seconds.
- **HIerarchy Made Easy**: Deeply nested commands? No problem. Our CLI handles the package structure and registration for you.
- **AI-Powered Autonomous Repair**: Convert scripts to Go and fix errors automatically using built-in coding agents.
- **Self-Documenting**: Every command you generate automatically gets its own documentation page.

## Core Philosophical Principles

1. **Explicit is better than implicit**: We use a [Manifest](manifest.md) (`.gtb/manifest.yaml`) so your project's structure is always clear and transparent.
2. **Encapsulated Logic**: We separate command boilerplate from your actual implementation, making your code cleaner and easier to test.
3. **Safety First**: Our templates use best practices like structured error handling and context propagation by default.

## Getting Started

To install the CLI tool, run:

```bash
go install github.com/phpboyscout/gtb@latest
```

Once installed, you can explore the available generation tools:

```bash
gtb generate --help
```

Or if you are working directly in this repository:

```bash
go run main.go generate --help
```

In the following sections, we'll dive into how to scaffold a new project, grow it with hierarchical commands, and keep it documented with our AI-powered tools. Let's build something amazing! ✨

- [Project Skeleton](skeleton.md): Scaffold your next big idea.
- [Generating Commands](command.md): Add functionality with ease.
- [AI Script Conversion](ai-conversion.md): Turn existing scripts into Go.
- [Generating Documentation](docs.md): Instant, AI-powered docs for commands and packages.
- [CLI Manifest](manifest.md): Understand your project structure.
- [MCP Server](mcp.md): Expose your tools via the Model Context Protocol.
