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

**The Intelligent Application Lifecycle Framework for Go.**

Modern CLI tools and DevOps workflows demand more than basic flag parsing. GTB works as a "batteries-included" micro-framework, providing a standardized foundation for building mission-critical tools with built-in agentic workflows, embedded documentation, and zero-config service management.

<video controls autoplay loop muted playsinline width="100%">
  <source src="tapes/basic-demo.mp4" type="video/mp4">
</video>

## Why GTB?

Before diving into code, we highly recommend reading our positioning guides to understand if GTB is the right fit for your next project:

- **[What is GTB?](why-gtb.md)** — Core philosophy, "IS / IS NOT" framing, and the 8 key advantages.
- **[Framework Comparison](comparison.md)** — Direct comparisons with Cobra, Viper, urfave/cli, and web frameworks.
- **[Coming from other Ecosystems?](coming-from-other-ecosystems.md)** — A translation guide for developers migrating from PHP (Laravel), Ruby (Rails), or Python (Django).

## Overview

GTB accelerates development by providing a standardized Dependency Injection (`Props`) container pre-wired with essential features. It includes multi-source configuration, automatic version checking, structured logging, and an AI service layer—allowing you to focus entirely on your unique business logic.

## Key Features

- **🤖 AI Agentic Workflows**: Integrated support for Claude, Gemini, and OpenAI to power autonomous ReAct-style loops against your code.
- **🔌 Model Context Protocol (MCP)**: Expose your CLI commands automatically as MCP tools for external AI agents.
- **📕 Integrated TUI Docs**: An interactive documentation browser with AI Q&A (`docs ask`) embedded directly in your tool.
- **📦 Auto Updates & Lifecycle**: Zero-config version syncing and self-update capabilities via GitHub/GitLab releases.
- **🚀 Scaffold & Generate**: Get a CLI tool running in seconds with the skeleton generator.
- **⚙️ Configuration Management**: Seamless config merging from embedded assets, YAML, and Env vars.
- **📝 Structured Logging & Errors**: Charm-powered logging with stack-traced, context-aware error handling.

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
