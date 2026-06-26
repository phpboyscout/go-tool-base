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
| **[Config](config.md)** | Programmatic config access for CI and scripted setup. (opt-in) |
| **[Version](version.md)** | Version display and update checking. |
| **[Update](update.md)** | Automated binary updates and migration. |
| **[Docs](docs.md)** | Interactive TUI documentation browser, plus roff man-page generation. |
| **Man** | Hidden, opt-in roff man-page emitter for packaging/preview. See [Docs › Man-page generation](docs.md#man-page-generation). (opt-in) |
| **[Doctor](doctor.md)** | Environment and configuration health checks, plus `doctor report` — a redacted, paste-ready support bundle. |
| **[Changelog](changelog.md)** | Embedded changelog display. |
| **[MCP](mcp.md)** | AI agent integration (Model Context Protocol). |
| **[Telemetry](telemetry.md)** | Opt-in usage telemetry status and management. (opt-in) |

## Framework-developer commands (`gtb`)

These commands are part of the `gtb` binary itself — the tooling you use to *build*
a CLI on GTB. They are not shipped in your generated tool.

| Command | Purpose |
| :--- | :--- |
| **[generate](generate.md)** | Scaffold projects, commands, flags, docs, and man pages. |
| **[regenerate](regenerate.md)** | Rebuild a project from its manifest, or the manifest from source. |
| **[remove](remove.md)** | Remove a generated command from a project. |
| **[keys](keys.md)** | Generate, mint, and publish OpenPGP signing keys. |
| **[sign](sign.md)** | Produce an OpenPGP detached signature for a file. |
| **[enable / disable](enable-disable.md)** | Toggle capabilities (signing, MCP, features) on a project. |
| **[template](template.md)** | Manage custom template-overlay sources. |

---

## Command Integration

### Automatic Registration

All built-in commands are automatically registered when you create a root command:

```go
package main

import (
    "embed"
    "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
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

    // Create root command. Built-in commands (init, version, update, docs,
    // doctor, changelog, mcp) are automatically registered unless explicitly
    // disabled.
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
- `props.ChangelogCmd`: Disables the `changelog` command.
- `props.ConfigCmd`: Disables the `config` command (note: already disabled by default).

Note: The `version` command cannot be disabled as it's essential for troubleshooting.

### Enabling Optional Commands

Some commands are opt-in and disabled by default. Enable them via `props.SetFeatures`:

```go
props := &props.Props{
    Tool: props.Tool{
        Features: props.SetFeatures(
            props.Enable(props.AiCmd),    // Enable AI provider configuration in 'init'
            props.Enable(props.ConfigCmd), // Enable programmatic config access
        ),
    },
}
```

**Available opt-in commands:**

- `props.AiCmd`: Enables AI provider configuration during `init`.
- `props.ConfigCmd`: Enables the `config get/set/list/validate` command group.
- `props.TelemetryCmd`: Enables the opt-in usage telemetry management commands.
- `props.ManCmd`: Enables the hidden roff man-page emitter command for packaging scripts.

---

## Custom Commands

You can easily add your own custom commands alongside the built-in ones by passing them to `NewCmdRoot`:

```go
customCmd := newCustomCommand(props)
rootCmd := root.NewCmdRoot(props, customCmd)
```

See the **[Development Guide](../../development/index.md)** for more details on implementing custom commands.
