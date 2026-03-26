---
title: Commands Overview
description: Overview of built-in commands available in all GTB applications.
date: 2026-02-16
tags: [components, commands, overview]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Commands Overview

GTB provides a set of essential built-in commands that are automatically included in every CLI tool. These commands provide core functionality for configuration management, version checking, self-updating, interactive documentation, and AI agent integration.

## Available Commands

| Command | Purpose |
| :--- | :--- |
| **[Root](root.md)** | Application entry point and service orchestration. |
| **[Init](init.md)** | Tool configuration and environment setup. |
| **[Version](version.md)** | Version display and update checking. |
| **[Update](update.md)** | Automated binary updates and migration. |
| **[Docs](docs.md)** | Interactive TUI documentation browser. |
| **[Doctor](doctor.md)** | Environment and configuration health checks. |
| **[MCP](mcp.md)** | AI agent integration (Model Context Protocol). |

---

## Command Integration

### Automatic Registration

All built-in commands are automatically registered when you create a root command:

```go
package main

import (
    "embed"
    "github.com/phpboyscout/go-tool-base/pkg/cmd/root"
    "github.com/phpboyscout/go-tool-base/pkg/props"
)

//go:embed assets/*
var assets embed.FS

func main() {
    props := &props.Props{
        Tool: props.Tool{
            Name: "mytool",
            // ... other configuration
        },
        // ... other props
    }

    // Initialize props...
    props.Assets = props.NewAssets(&assets)

    // Create root command. Built-in commands (init, version, update, docs, mcp)
    // are automatically registered unless explicitly disabled.
    rootCmd := root.NewCmdRoot(props)
    rootCmd.Execute()
}
```

### Disabling Commands

You can disable specific built-in commands by configuring the `Features` field in `props.Tool`:

```go
props := &props.Props{
    Tool: props.Tool{
        Name: "mytool",
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd), // Disable the update command
            props.Disable(props.InitCmd),   // Disable the init command
            props.Disable(props.McpCmd),    // Disable the MCP command
        ),
    },
}
```

**Available disable options:**

- `props.UpdateCmd`: Disables the `update` command.
- `props.InitCmd`: Disables the `init` command.
- `props.McpCmd`: Disables the `mcp` command.
- `props.DocsCmd`: Disables the `docs` command.
- `props.DoctorCmd`: Disables the `doctor` command.

Note: The `version` command cannot be disabled as it's essential for troubleshooting.

### Enabling Optional Commands

Some features are opt-in, such as the AI provider configuration:

```go
props := &props.Props{
    Tool: props.Tool{
        Features: props.SetFeatures(
            props.Enable(props.AiCmd), // Enable AI provider configuration in 'init'
        ),
    },
}
```

---

## Custom Commands

You can easily add your own custom commands alongside the built-in ones by passing them to `NewCmdRoot`:

```go
customCmd := newCustomCommand(props)
rootCmd := root.NewCmdRoot(props, customCmd)
```

See the **[Development Guide](../../development/index.md)** for more details on implementing custom commands.
