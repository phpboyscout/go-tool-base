---
title: Installation
description: Installation instructions for GTB and private module configuration.
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
- Access to the Git repository (github.com)
- Properly configured Go environment for private modules

!!! info "Go Version in Generated Projects"
    While GTB itself requires Go 1.21+, the `generate skeleton` command creates projects configured for Go 1.24+ to take advantage of the latest [tool directive](https://go.dev/doc/modules/managing-dependencies#tools) features.

## Private Module Configuration

Since GTB is hosted on a private Git server, you'll need to configure your Go environment:

```bash
# Configure Git to use authentication for private repositories
git config --global --replace-all url."https://${GITHUB_USERNAME}:${GITHUB_TOKEN}@github.com".insteadOf "https://github.com"

# Set GOPRIVATE to include the private module path
go env -w GOPRIVATE=github.com
```

- `GITHUB_USERNAME` - Your GitHub username
- `GITHUB_TOKEN` - Your GitHub personal access token with appropriate permissions

## CLI Installation

The recommended way to install the `gtb` CLI is using our pre-built release binaries. This ensures you have all necessary assets (like documentation) which are omitted by source-based builds.

### Linux/macOS

```bash
curl -sSL -H "Authorization: Bearer ${GITHUB_TOKEN}" -H "Accept: application/vnd.github.v3.raw" "https://github.com/ptps/gtb/raw/main/install.sh" | bash
```

!!! note
    The script installs the binary to `$HOME/.local/bin`. Ensure this directory is in your `$PATH`.

### Windows (PowerShell)

```powershell
$env:GITHUB_TOKEN = "your_token_here"
irm "https://github.com/ptps/gtb/raw/main/install.ps1" -Headers @{Authorization = "Bearer $env:GITHUB_TOKEN"; Accept = "application/vnd.github.v3.raw"} | iex
```

### From Source (go install)

While less recommended because gitignored assets (like the TUI documentation) will be missing, you can still install from source:

```bash
go install github.com/phpboyscout/gtb@latest
```

Ensuring your `$GOPATH/bin` is in your `$PATH`, you can then use the `gtb` command directly.

## Adding the Library to Your Project

### Method 1: Go Modules (Recommended)

Add GTB to your project using Go modules:

```bash
go mod init your-tool-name
go get github.com/phpboyscout/gtb
```

### Method 2: Direct Import

Add the import to your Go files and run `go mod tidy`:

```go
import "github.com/phpboyscout/gtb/pkg/cmd/root"
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
    "time"

    "github.com/phpboyscout/gtb/pkg/cmd/root"
    "github.com/phpboyscout/gtb/pkg/props"
    "github.com/phpboyscout/gtb/pkg/version"
    "github.com/charmbracelet/log"
    "github.com/spf13/afero"
)

//go:embed assets/*
var assets embed.FS

func main() {
    logger := log.NewWithOptions(os.Stderr, log.Options{
        ReportTimestamp: true,
        TimeFormat:      time.Kitchen,
        Level:          log.InfoLevel,
    })

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
        Logger:  logger,
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

- Verify your `GOPRIVATE` setting includes `github.com`
- Ensure your GitHub token has access to the repository

**Build failures:**

- Check that you're using Go 1.21 or later
- Run `go mod tidy` to resolve dependencies

**Authentication errors:**

- Verify your Git configuration for the private repository
- Test access with: `git clone https://github.com/phpboyscout/gtb.git`
