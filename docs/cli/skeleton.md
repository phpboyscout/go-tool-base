---
title: Generating a CLI Skeleton
description: Guide to scaffolding a new CLI project with the generate skeleton command.
date: 2026-02-16
tags: [cli, generator, scaffolding, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generating a CLI Skeleton

The journey of a thousand miles begins with a single step—and for your new tool, that step is `generate skeleton`. 🛠️

Scaffolding a project from scratch can be tedious. `generate skeleton` fast-tracks this process by setting up a robust, industry-standard project structure that's ready for high-scale development.

<video controls autoplay loop muted playsinline width="100%">
  <source src="../../tapes/basic-demo.mp4" type="video/mp4">
</video>

## What's included in the box?

When you run `generate skeleton`, we set up a complete, working CLI project:

**Project Core**
: A clean `main.go` and a `root` command in `pkg/cmd/root`.

**Modern Tooling**
: A `go.mod` file using the latest **Go 1.24+ tool directives**, keeping your dependencies clean and isolated.

**CI/CD Readiness**
: GitHub Actions workflows for testing, linting, releases, and documentation.

**Standard Layout**
: A `pkg/` directory for your logic and a `docs/` directory for your users.

**The Manifest**
: A `.gtb/manifest.yaml` file that acts as the brain of your project, tracking your command hierarchy.


### Project Structure Summary

```text
my-awesome-tool/
├── .github/workflows/          # CI/CD: Automated testing, linting, and release
├── .gtb/manifest.yaml          # The Brain: Tracks your command hierarchy
├── cmd/my-awesome-tool/main.go # Entry Point: The main function of your tool
├── pkg/cmd/root/cmd.go         # The Root: Setup and registration of all commands
├── docs/                       # Documentation: Your project's documentation site
├── go.mod                      # Dependencies: Uses Go 1.24+ tool directives
└── README.md                   # Welcome: Basic info about your new tool
```

### The Generated Root Command

The `pkg/cmd/root/cmd.go` file initializes the `Props` container, configures logging, and returns both the root command and the props for use by the `Execute()` wrapper.

#### Annotated Example: `pkg/cmd/root/cmd.go`

```go
package root

import (
    "embed"
    "os"

    gtbRoot "github.com/phpboyscout/gtb/pkg/cmd/root"
    "github.com/phpboyscout/gtb/pkg/errorhandling"
    "github.com/phpboyscout/gtb/pkg/logger"
    "github.com/phpboyscout/gtb/pkg/props"
    "github.com/phpboyscout/gtb/pkg/version"

    "github.com/spf13/afero"
    "github.com/spf13/cobra"
)

//go:embed assets/*
var assets embed.FS

// NewCmdRoot constructs the root command and props for this tool.
// It returns both so that main.go can pass them to pkgRoot.Execute().
func NewCmdRoot(v version.Info) (*cobra.Command, *props.Props) {
    l := logger.NewCharm(os.Stderr,
        logger.WithTimestamp(),
        logger.WithLevel(logger.InfoLevel),
    )

    p := &props.Props{
        Assets: props.NewAssets(props.AssetMap{"root": &assets}),
        FS:     afero.NewOsFs(),
        Logger: l,
        Tool: props.Tool{
            Name:    "my-tool",
            Summary: "A summary of my tool",
            ReleaseSource: props.ReleaseSource{
                Type:  "github",
                Owner: "my-org",
                Repo:  "my-repo",
            },
        },
        Version: v,
    }

    // Optionally configure a help/support channel shown in error messages:
    // p.Tool.Help = errorhandling.SlackHelp{Team: "My Team", Channel: "#support"}
    // p.Tool.Help = errorhandling.TeamsHelp{Team: "My Team", Channel: "Support"}

    p.ErrorHandler = errorhandling.New(l, p.Tool.Help)

    rootCmd := gtbRoot.NewCmdRoot(p)

    // Subcommands are registered here by the generator:
    // rootCmd.AddCommand(mysubcmd.NewCmdMySub(p))

    return rootCmd, p
}
```

### The Tool Entry Point

The `cmd/my-awesome-tool/main.go` uses `pkgRoot.Execute` to run the command and route all errors through `ErrorHandler`:

```go
package main

import (
    "my-awesome-tool/internal/version"
    "my-awesome-tool/pkg/cmd/root"

    pkgRoot "github.com/phpboyscout/gtb/pkg/cmd/root"
)

func main() {
    rootCmd, p := root.NewCmdRoot(version.Get())
    pkgRoot.Execute(rootCmd, p)
}
```

`pkgRoot.Execute` silences Cobra's own error printing and routes any error returned from `RunE` through `ErrorHandler.Check` at fatal level. There is no need for an `os.Exit` call in `main.go`.

## How to run it

Navigate to the directory where you want your project to live and run:

```bash
gtb generate cli \
  --name "my-awesome-tool" \
  --repo "my-github-org/my-awesome-tool-repo" \
  --git-backend github \
  --help-type slack \
  --slack-channel "#help" \
  --slack-team "My Team"
```

### Interactive Multi-Stage Form

You don't have to remember all the flags! If you run it without `--name` and `--repo`, the CLI will guide you through a three-stage interactive form:

**Stage 1 — Project Setup**
: Name, Description, Destination Path, Features, Git Backend (GitHub/GitLab), Help Channel (Slack/Teams/None).

**Stage 2 — Git Repository**
: Git Host (pre-filled from your backend selection, editable for self-hosted instances) and Repository in `org/repo` format.

**Stage 3 — Help Channel** *(skipped if None selected)*
: Slack Channel + Slack Team, or Teams Channel + Teams Team, depending on your Stage 1 selection.

Press **Escape** at any stage to go back to the previous one. Press **Ctrl+C** to cancel.

### Available Flags

| Flag | Short | Description | Default |
| :--- | :--- | :--- | :--- |
| `--name` | `-n` | Name of your CLI tool | — |
| `--repo` | `-r` | Repository in `org/repo` format | — |
| `--git-backend` | | Git backend (`github` or `gitlab`) | `github` |
| `--host` | | Git host (overrides backend default, for self-hosted instances) | — |
| `--description` | `-d` | Short description of the tool | `A tool built with gtb` |
| `--path` | `-p` | Destination path for the generated project | `.` |
| `--features` | `-f` | Features to enable (`init`, `update`, `mcp`, `docs`) | all four |
| `--help-type` | | Help channel type (`slack`, `teams`, or `none`) | `none` |
| `--slack-channel` | | Slack channel (e.g. `#my-team-help`) | — |
| `--slack-team` | | Slack team name (e.g. `My Team`) | — |
| `--teams-channel` | | Microsoft Teams channel | — |
| `--teams-team` | | Microsoft Teams team name | — |

!!! tip
    The `--host` flag is only needed when using a self-hosted GitHub Enterprise or GitLab instance. For public `github.com` or `gitlab.com`, the correct host is set automatically from `--git-backend`.

## Help Channel Configuration

The skeleton generator supports two built-in help channel types, which populate the `Tool.Help` field in the generated root command:

**Slack** — users are directed to a Slack channel in error messages:
```
For assistance, contact My Team via Slack channel #support
```

**Microsoft Teams** — users are directed to a Teams channel:
```
For assistance, contact My Team via Microsoft Teams channel Support
```

Both use the `errorhandling.HelpConfig` interface, so you can also provide a custom implementation.

## Next Steps

Once your skeleton is generated, your project is ready to grow! Head over to the [Command Generation](command.md) guide to see how to add functionality.
