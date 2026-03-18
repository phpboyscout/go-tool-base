---
title: Go Tool Base (GTB)
description: Overview of GTB, a library for building CLI tools in Go.
date: 2026-02-16
tags: [overview, introduction]
authors: [Matt Cockayne <matt@phpboyscout.com>]
hide:
  - navigation
---

# Go Tool Base (GTB)

A comprehensive Go library providing base components for building command-line tools with common functionality like initialization, configuration management, version control, and auto-updates.

<video controls autoplay loop muted playsinline width="100%">
  <source src="tapes/basic-demo.mp4" type="video/mp4">
</video>

## Overview

GTB is designed to accelerate the development of CLI tools by providing a standardized foundation with essential features that most command-line applications need. It includes robust configuration management, automatic version checking and updates, error handling, and a structured approach to building extensible CLI applications.

## Key Features

- **🚀 Quick Setup**: Get a CLI tool running in minutes with built-in commands
- **🤖 AI Autonomous Repair**: Convert scripts to Go and fix errors automatically using built-in agents
- **⚙️ Configuration Management**: Flexible config loading from files and embedded resources
- **🔄 Auto Updates**: Built-in version checking and self-update functionality
- **📝 Structured Logging**: Integrated logging with configurable levels and formats
- **💡 Integrated Documentation**: Embed your docs directly in the CLI with an interactive TUI browser and AI Q&A
- **🧪 Testable**: Comprehensive interfaces and mocking support for unit testing
- **🔧 Extensible**: Easy to add custom commands and functionality

## Built-in Commands

Every CLI tool built with GTB automatically includes the following commands with a cli tool:

- **`init`** - Initialize tool configuration and setup
- **`version`** - Display version information and check for updates
- **`update`** - Update the tool to the latest version
- **`docs`** - Interactive documentation browser with AI capabilities
- **`mcp`** - Expose your tool's capabilities via the Model Context Protocol

## Quick Start

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
        TimeFormat:     time.Kitchen,
        Level:          log.InfoLevel,
    })

    props := &props.Props{
        Tool: props.Tool{
            Name:        "mytool",
            Summary:     "My awesome CLI tool",
            Description: "A tool that does amazing things",
            GitHub: props.GitHub{
                Org:  "myorg",
                Repo: "mytool",
            },
        },
        Logger:  logger,
        Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
        FS:      afero.NewOsFs(),
        Version: version.NewInfo("1.0.0", "", ""),
    }

    rootCmd := root.NewCmdRoot(props)
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

## [Architecture](concepts/architecture.md)

Learn about the high-level system design, command registry, and execution flow in our **[Concepts](concepts/index.md)** section.

## Getting Started

Ready to build your own CLI tool? We provide two clear paths:

- **The Fast Track**: Use the [Generator CLI](how-to/new-cli.md) to scaffold a project in seconds.
- **Manual Integration**: Follow the [Step-by-Step Guide](getting-started.md#route-b-manual-integration-manual-step-by-step) to integrate the library manually.

Check out our **[How-to Guides](how-to/index.md)** for more practical instructions.
