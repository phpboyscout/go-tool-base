---
title: Getting Started
description: Getting started guide for GTB, covering CLI scaffolding and manual integration.
date: 2026-02-16
tags: [getting-started, guide, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
hide:
  - navigation
---

# Getting Started

GTB provides two primary ways to get started. Choose the route that best fits your workflow:

| Route | Description | Best For... |
| :--- | :--- | :--- |
| **[Route A: CLI Generator](#route-a-the-fast-track-cli-scaffolding)** | Use our specialized CLI tool to scaffold your project in seconds. | New projects and rapid prototyping. |
| **[Route B: Manual Integration](#route-b-manual-integration-manual-step-by-step)** | Manually integrate `gtb` as a library into an existing project. | Complex existing tools and custom layouts. |

---

## Route A: The Fast-Track (CLI Scaffolding)

This is the recommended path for most users. Our generator handles the boilerplate, directory structure, and registration logic for you.

### 1. Install Global Generator
Ensure you have the `gtb` CLI installed:
```bash
go install github.com/phpboyscout/gtb/pkg/cmd/gtb@latest
```

### 2. Scaffold Your Project
Run the skeleton generator to create your new tool:
```bash
gtb generate skeleton --name my-awesome-tool --github-org your-org
```

### 3. Add Custom Commands
Navigate to your project and use the command generator:
```bash
cd my-awesome-tool
gtb generate command --name hello
```

---

## Route B: Manual Integration (Manual Step-by-Step)

If you have an existing project or prefer total control, follow these steps to integrate `gtb` as a library.

### 1. Project Initialization
Create a directory and initialize your module:
```bash
mkdir my-awesome-tool
cd my-awesome-tool
go mod init github.com/your-org/my-awesome-tool
mkdir -p cmd/tool
```

### 2. Basic Tool Structure
Create your `main.go` entry point.

**cmd/tool/main.go:**
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
        Level:           log.InfoLevel,
    })

    props := &props.Props{
        Tool: props.Tool{
            Name: "my-awesome-tool",
            GitHub: props.GitHub{Org: "your-org", Repo: "my-awesome-tool"},
        },
        Logger:  logger,
        Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
        FS:      afero.NewOsFs(),
        Version: version.NewInfo("1.0.0", "dev", time.Now().Format(time.RFC3339)),
    }

    rootCmd := root.NewCmdRoot(props)
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

### 3. Provide Default Assets
Create the assets directory and a default configuration file.

```bash
mkdir cmd/tool/assets
```

**cmd/tool/assets/config.yaml:**
```yaml
log:
  level: info
vcs:
  provider: github # Options: github, gitlab
github:
  url:
    api: https://github.com/api/v3
gitlab:
  url:
    api: https://gitlab.com/api/v4
```

---

## Next Steps

Regardless of which route you choose, you now have a CLI tool powered by GTB. Explore the rest of the documentation to unlock its full potential:

- **[Core Concepts](concepts/index.md)**: Deep dive into theory, props, and precedence.
- **[How-to Guides](how-to/index.md)**: Practical instructions for adding commands and testing.
- **[Component Reference](components/index.md)**: Detailed API documentation for every framework package.
