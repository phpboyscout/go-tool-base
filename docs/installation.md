---
title: Installation
description: Installation instructions for GTB.
date: 2026-02-16
tags: [installation, setup, configuration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
hide:
  - navigation
---

# Installation

GTB is a Go library designed to be imported into your CLI tool projects. There are several ways to add it to your project depending on your development workflow.

## Prerequisites

Before using GTB, ensure you have:

- **Go 1.21 or later** installed (generated projects may target newer versions like Go 1.24+)

!!! info "Go Version in Generated Projects"
    While GTB itself requires Go 1.21+, the `generate skeleton` command creates projects configured for Go 1.24+ to take advantage of the latest [tool directive](https://go.dev/doc/modules/managing-dependencies#tools) features.

## CLI Installation

The recommended way to install the `gtb` CLI is using our pre-built release binaries. This ensures you have all necessary assets (like documentation) which are omitted by source-based builds.

### Linux/macOS

```bash
curl -sSL "https://github.com/phpboyscout/go-tool-base/raw/main/install.sh" | bash
```

!!! note
    The script installs the binary to `$HOME/.local/bin`. Ensure this directory is in your `$PATH`.

### Windows (PowerShell)

```powershell
irm "https://github.com/phpboyscout/go-tool-base/raw/main/install.ps1" | iex
```

### Token Permissions

The install scripts accept an optional `GITHUB_TOKEN` environment variable. For installing releases from this public repository, **no token is required** — anonymous access is sufficient and subject only to GitHub's 60 req/hr rate limit.

If you choose to supply a token (for example to raise that rate limit or when running inside a CI pipeline that always sets `GITHUB_TOKEN`), the minimum permissions are:

- **Fine-grained PAT:** `contents: read` on `phpboyscout/go-tool-base`.
- **Classic PAT:** the `public_repo` scope is sufficient.

The install scripts will emit a warning if they detect a token with broader scopes (`repo`, `admin:*`, `delete_repo`, `workflow`) because those are unnecessary for installation and increase the blast radius if the token is ever leaked from shell history, environment dumps, or a misconfigured CI log.

### From Source (go install)

While less recommended because gitignored assets (like the TUI documentation) will be missing, you can still install from source:

```bash
go install github.com/phpboyscout/go-tool-base@latest
```

Ensuring your `$GOPATH/bin` is in your `$PATH`, you can then use the `gtb` command directly.

## Adding the Library to Your Project

### Method 1: Go Modules (Recommended)

Add GTB to your project using Go modules:

```bash
go mod init your-tool-name
go get github.com/phpboyscout/go-tool-base
```

### Method 2: Direct Import

Add the import to your Go files and run `go mod tidy`:

```go
import "github.com/phpboyscout/go-tool-base/pkg/cmd/root"
```

Then run:
```bash
go mod tidy
```

## Project Structure

When starting a new CLI tool project, we recommend this structure:

```
your-tool/
├── cmd/
│   └── main.go              # Main entry point
├── pkg/
│   └── cmd/
│       ├── root/            # Root command setup
│       │   ├── assets/      # Embedded assets (configs, etc.)
│       │   ├── main.go      # Root command setup
│       └── custom/          # Your custom commands
├── go.mod
├── go.sum
└── README.md
```

## Minimal Example

Create a minimal CLI tool to verify installation:

**main.go:**
```go
package main

import (
    "embed"
    "os"

    "github.com/phpboyscout/go-tool-base/pkg/cmd/root"
    "github.com/phpboyscout/go-tool-base/pkg/logger"
    "github.com/phpboyscout/go-tool-base/pkg/props"
    "github.com/phpboyscout/go-tool-base/pkg/version"
    "github.com/spf13/afero"
)

//go:embed assets/*
var assets embed.FS

func main() {
    l := logger.NewCharm(os.Stderr,
        logger.WithTimestamp(),
        logger.WithLevel(logger.InfoLevel),
    )

    props := &props.Props{
        Tool: props.Tool{
            Name:        "example-tool",
            Summary:     "An example CLI tool",
            Description: "Demonstrates GTB usage",
            GitHub: props.GitHub{
                Org:  "your-org",
                Repo: "example-tool",
            },
        },
        Logger:  l,
        Assets:  props.NewAssets(&assets),
        FS:      afero.NewOsFs(),
        Version: version.NewInfo("0.1.0", "", ""),
    }

    rootCmd := root.NewCmdRoot(props)

    // Add your custom commands here
    // rootCmd.AddCommand(yourCustomCommand)

    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

**Build and test:**
```bash
go build -o example-tool .
./example-tool --help
./example-tool version
```

## Verification

After installation, you should be able to:

1. **Build successfully**: `go build .` should complete without errors
2. **Run basic commands**: Your tool should respond to `--help`, `version`, etc.
3. **Access built-in functionality**: Commands like `init`, `version`, and `update` should be available

## Next Steps

Once installation is complete:

1. **Read the [Getting Started Guide](getting-started.md)** for a detailed tutorial

- **[Components Documentation](components/props.md)** to understand the architecture

## Troubleshooting

### Common Issues

**Import errors:**

- Ensure your Go environment is set up correctly (`go env GOPATH`)
- Run `go mod tidy` to resolve dependencies

**Build failures:**

- Check that you're using Go 1.21 or later
- Run `go mod tidy` to resolve dependencies
