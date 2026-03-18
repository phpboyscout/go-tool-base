---
title: Configuring Built-in Features
description: Configuring, enabling, and disabling built-in features like updates and MCP.
date: 2026-02-16
tags: [how-to, configuration, features, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuring Built-in Features

GTB comes with several powerful features **enabled by default**. These include:
- **`version`**: Semantic versioning and build info.
- **`update`**: Automatic self-updates via GitHub releases.
- **`init`**: Interactive configuration bootstrapping.
- **`mcp`**: The Model Context Protocol server for AI agents.
- **`docs`**: The integrated TUI documentation browser and AI assistant.

You can selectively disable these features or enable additional opt-in features in your `main.go` using the `props.Tool` configuration.

## Disabling Features

If you don't need certain functionality (like self-updates or the MCP server), you can disable it:

```go
props := &props.Props{
    Tool: props.Tool{
        Name: "mytool",
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd), // Disable self-updates
            props.Disable(props.McpCmd),    // Disable MCP server
        ),
    },
}
```

## Enabling Opt-in Features

Some features, like the AI provider setup in the `init` command, are disabled by default to keep the footprint small:

```go
props := &props.Props{
        Features: props.SetFeatures(
            props.Enable(props.AiCmd), // Enables 'init ai'
        ),
}
```

## Configuring Version checking

The `version` and `update` commands rely on GitHub releases. Ensure you have the `GitHub` property set correctly in your `Props`:

```go
GitHub: props.GitHub{
    Org:  "my-org",
    Repo: "my-tool",
},
```

---

## Feature Reference Table

The following table provides a complete reference for all `FeatureCmd` constants, showing their default state, affected commands, and typical use cases:

| Constant | Default State | Affected Commands | Use Case |
| :--- | :--- | :--- | :--- |
| `props.UpdateCmd` | **Enabled** | `update` | Disable if you manage updates externally (package managers, CI/CD) |
| `props.InitCmd` | **Enabled** | `init`, `init github` | Disable if your tool has no configuration requirements |
| `props.McpCmd` | **Enabled** | `mcp`, `mcp serve`, `mcp tools` | Disable if AI agent integration is not needed |
| `props.DocsCmd` | **Enabled** | `docs`, `docs serve`, `docs ask` | Disable if you don't embed documentation |
| `props.AiCmd` | **Opt-in** | `init ai` | Enable to allow users to configure AI providers |

!!! tip "Enable vs Disable Semantics"

    - **Disable**: Use this to turn OFF default features your tool doesn't need
    - **Enable**: Use this to turn ON optional features your tool wants to provide
    
    Most tools should only need to customize a few options. The defaults are designed to work well for typical CLI applications.

---

## Feature Combinations

Here are some common configuration patterns:

### Minimal Tool (No Auto-Updates or AI)

For simple, lightweight utilities:

```go
props := &props.Props{
    Tool: props.Tool{
        Name: "mytool",
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd),
            props.Disable(props.McpCmd),
        ),
    },
}
```

### AI-Powered Tool

For tools that leverage AI features:

```go
props := &props.Props{
    Tool: props.Tool{
        Name: "mytool",
        Features: props.SetFeatures(
            props.Enable(props.AiCmd),  // Enable AI provider setup in 'init'
        ),
    },
}
```

### Documentation-Only Tool

For tools focused on documentation delivery:

```go
props := &props.Props{
    Tool: props.Tool{
        Name: "mydocs",
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd),
            props.Disable(props.InitCmd),
            props.Disable(props.McpCmd),
            props.Disable(props.DocsCmd), // Disable docs if not needed, otherwise it's enabled by default
        ),
    },
}
```

### Air-Gapped Environment

For tools deployed in restricted environments without internet access:

```go
props := &props.Props{
    Tool: props.Tool{
        Name: "mytool",
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd),  // No GitHub access for updates
            props.Disable(props.McpCmd),     // No AI agent connectivity
        ),
    },
}
```

---

## Runtime Checks

You can check at runtime whether a feature is enabled or disabled:

```go
func isFeatureEnabled(p *props.Props, feature props.FeatureCmd) bool {
    // Smart default logic is handled internally by gtb
    return p.Tool.IsEnabled(feature)
}
```

!!! warning "Feature Registration"
    The feature mechanism affects command registration at startup. Changing these values after `root.NewCmdRoot()` has been called will have no effect.
