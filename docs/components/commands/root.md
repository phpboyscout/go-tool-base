---
title: Root Command
description: The entry point for the CLI, orchestrating service initialization and global flags.
date: 2026-02-16
tags: [components, commands, root, entrypoint]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Root Command

The Root command is the entry point for every GTB CLI. It orchestrates the primary lifecycle of the application, including service initialization and global feature registration.

## Usage

```bash
mytool [subcommand] [flags]
```

## Description

The root command provides the base structure for your tool. It manages the persistent state (flags, logging, config) for all subcommands. It does not perform a specific domain action on its own but ensures the environment is correctly set up before any subcommand runs.

## Built-in Commands

The root command automatically registers the following subcommands:

| Command | Description | Can be Disabled? |
| :--- | :--- | :--- |
| `version` | Display version information and check for updates | :material-close: No |
| `init` | Initialize tool configuration and setup | :material-check: Yes |
| `update` | Update the tool to the latest version | :material-check: Yes |
| `docs` | Interactive documentation browser with AI Q&A | :material-check: Yes |
| `mcp` | Expose tool as a Model Context Protocol server | :material-check: Yes |

!!! tip "Disabling Built-in Commands"
    Use the `Features` property to remove optional commands from your tool:
    ```go
    Tool: props.Tool{
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd), // Remove update command
            props.Disable(props.McpCmd),    // Remove MCP server
        ),
    }
    ```

## Global Flags

These flags are available to every subcommand:

| Flag | Description |
| :--- | :--- |
| `--config stringArray` | Path(s) to configuration files (default: `~/.mytool/config.yaml` and `/etc/mytool/config.yaml`). |
| `--debug` | Forces debug-level logging and enables detailed error stack traces. |
| `--ci` | Indicates the tool is running in a Continuous Integration environment (disables interactive update prompts). |

## Lifecycle Hooks (PersistentPreRunE)

Before any subcommand is executed, the root command performs the following automated steps:

1. **Flag Extraction**: Validates and parses the global flags.
2. **Configuration Loading**: Merges embedded assets with local filesystem configuration.
3. **Logging Setup**: Configures the global `props.Logger` level and format based on flags and config.
4. **Update Checking**: Optionally performs a background check for newer versions (unless `--ci` is set or the check was done in the last 24 hours).

## Implementation

The root command is implemented in `cmd/root/root.go` and created via the `root.NewCmdRoot(props)` entry point.

For more information on the dependency injection pattern used here, see the **[Props Documentation](../props.md)**.
