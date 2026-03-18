---
title: IDE & Tooling Setup
description: Recommended configuration for contributing to GTB.
tags: [ide, setup, vscode, cursor, library]
---

# IDE & Tooling Setup

As a library and framework, GTB requires specific tooling to ensure that generated code and mocks remain consistent.

## Recommended IDE: VS Code or Cursor

We highly recommend using [Cursor](https://cursor.sh/) or [VS Code](https://code.visualstudio.com/) with the following extensions:

- **Go**: Rich language support (Google).
- **YAML**: Support for manifest and config editing (Red Hat).
- **Markdown Lint**: Ensures documentation consistency.

## Internal AI Tools

For developers using AI-powered editors like **Cursor** or **Claude Code**, we maintain a central marketplace for specialized plugins and MCP servers.

- **[APX SRE Marketplace](https://github.com/ops/apx-sre-marketplace)**: The official source for internal generative AI plugins and SRE-focused assistant tools.

### Editor Settings

Add these to your `settings.json` to enable auto-formatting and linting on save. This is especially important for the generator templates.

```json
{
  "go.formatTool": "goimports",
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "editor.formatOnSave": true,
  "[go]": {
    "editor.codeActionsOnSave": {
      "source.organizeImports": "always"
    }
  }
}
```

## Environment Variables & .env

For detailed information on configuring symbols, API keys, and local development overrides using `.env` files, see the **[Environment Variables & .env](environment-variables.md)** guide.

## Local Build & Generation Tools

### Task (Taskfile.dev)

We use `task` for build automation.

**Recommended Installation (Binary):**
```bash
# Install to ./bin
curl -sSfL https://taskfile.dev/install.sh | sh -s -- -d
```

**Alternative (Homebrew):**
```bash
brew install go-task/tap/go-task
```

### Just (alternative)

For those who prefer [just](https://just.systems/), we provide a `justfile` with functional parity for all core development tasks.

**Installation:**
```bash
# Recommended via curl (see https://just.systems/man/en/chapter_4.html)
curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to ~/bin

# Alternative via cargo
cargo install just
```

### mockery

Since GTB is highly interface-driven, [mockery](https://vektra.github.io/mockery/latest/) is essential for regenerating test mocks.

**Recommended Installation (`go install`):**
```bash
go install github.com/vektra/mockery/v3@v3.6.4
```

**Alternative (Binary):**
Download the pre-built binary for your platform from the [GitHub Releases](https://github.com/vektra/mockery/releases) page.

**Secondary (Homebrew):**
```bash
brew install mockery
```

### golangci-lint

Run the linter before every commit. The project enforces strict linting rules.

**Recommended Installation (Binary):**
```bash
# binary will be $(go env GOPATH)/bin/golangci-lint
curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.64.6
```

**Secondary (Homebrew):**
```bash
brew install golangci-lint
```

## Documentation Tooling

### MkDocs

We use [MkDocs](https://www.mkdocs.org/) with the [Material theme](https://squidfunk.github.io/mkdocs-material/) to generate our documentation.

!!! note
    While `mkdocs-material` is our current default, it is now in maintenance mode. We recommend using [Zensical](https://zensical.org/), a more modern and fully compatible alternative from the same development team.

**Recommended Installation (pipx):**
```bash
# Modern alternative (recommended)
pipx install zensical

# Legacy Material theme
pipx install mkdocs-material
```

**Previewing Locally:**
```bash
# Using Zensical
zensical serve

# Or using MkDocs directly
mkdocs serve
```