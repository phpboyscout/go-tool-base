---
title: Getting Started
description: The two ways into GTB: scaffold a project with the generator, or wire the library into an existing tool by hand.
date: 2026-08-02
tags: [getting-started, guide, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
hide:
  - navigation
---

# Getting Started

There are two ways in, and the choice is mostly about whether you are starting
fresh.

| Route | What it gives you | Best for |
| :--- | :--- | :--- |
| **[Generate a project](#generate-a-project)** | A complete, releasable project (command tree, CI, docs site, manifest) in one command. | New tools, and prototyping. |
| **[Wire the library by hand](#wire-the-library-by-hand)** | GTB's root command, Props and built-in commands inside a layout you already own. | Existing tools, and unusual layouts. |

Both need the `gtb` CLI or the library installed first. See
[Installation](installation.md).

If you would rather be walked through it,
[Build your first CLI](tutorials/build-your-first-cli.md) is the same ground at
tutorial pace, with the expected output at each step.

## Generate a project

The generator handles the boilerplate, the directory structure and the
registration wiring.

### 1. Scaffold

```bash
gtb generate project \
  --name my-awesome-tool \
  --repo your-org/my-awesome-tool \
  --env-prefix MY_AWESOME_TOOL \
  --path ./my-awesome-tool
```

`gtb generate cli` and `gtb generate skeleton` are aliases for the same command.
Run it without `--name`/`--repo` in an interactive terminal and it launches a
guided wizard instead.

Set `--env-prefix`. Without it the generated tool has no environment-variable
configuration layer, because an unprefixed layer would let any process on the
machine reconfigure your tool.

Pick your feature set with `--features`, `init`, `update`, `mcp`, `docs`,
`doctor`, `changelog`, `keychain`, `ai`, `config`, `telemetry`, `man`, and a
forge to configure credentials for (`github`, `gitlab`, `gitea`, `bitbucket`).
The default set is the first seven. Note that `--features` *replaces* that
default rather than adding to it, so name every feature you want. Everything
except `keychain` can be added or removed later. Full list in the
[generate reference](reference/cli/generate.md#features).

### 2. Build and initialise

```bash
cd my-awesome-tool
just build            # or: go build -o bin/my-awesome-tool ./cmd/my-awesome-tool
./bin/my-awesome-tool init
```

The first build downloads a substantial dependency tree; give it a few minutes.
`init` writes `~/.my-awesome-tool/config.yaml` from the template the tool ships
with. Until it exists, most commands stop with `no config file found`.

### 3. Add a command

```bash
gtb generate command --name hello --short "Say hello"
```

That writes `pkg/cmd/hello/cmd.go` (generated boilerplate, do not edit) and
`pkg/cmd/hello/main.go` (yours), registers the command in the root tree, records
it in `.gtb/manifest.yaml`, and writes a reference page under
`docs/reference/cli/`.

Your logic goes in `main.go`. Regeneration never rewrites it. See
[Regenerating components](how-to/framework-cli/regenerate-components.md) for the
three mechanisms that protect your edits, and the one flag (`--force`) that does
not.

## Wire the library by hand

Use this when you have an existing project or a layout the generator would fight.
You give up the manifest, regeneration and the scaffolded CI; you keep Props, the
root command, the built-in commands and the configuration store.

### 1. Create the module

```bash
mkdir my-awesome-tool
cd my-awesome-tool
go mod init github.com/your-org/my-awesome-tool
mkdir -p cmd/tool/assets
```

### 2. Write the entry point

**`cmd/tool/main.go`:**

```go
package main

import (
	"embed"
	"os"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

//go:embed assets/*
var assets embed.FS

func main() {
	l := logger.NewCharm(os.Stderr,
		logger.WithTimestamp(true),
		logger.WithLevel(logger.InfoLevel),
	)

	p, err := props.New(
		props.Tool{
			Name:      "my-awesome-tool",
			Summary:   "My awesome CLI tool",
			EnvPrefix: "MY_AWESOME_TOOL",
			ReleaseSource: props.ReleaseSource{
				Type:  "github",
				Owner: "your-org",
				Repo:  "my-awesome-tool",
			},
		},
		l,
		afero.NewOsFs(),
		props.WithAssets(props.NewAssets(props.AssetMap{"root": &assets})),
		props.WithVersion(version.NewInfo("0.1.0", "", "")),
	)
	if err != nil {
		l.Error("failed to build props", "error", err)
		os.Exit(1)
	}

	root.Execute(root.NewCmdRoot(p), p)
}
```

`root.Execute` is not the same as Cobra's `rootCmd.Execute()`. It adds the
signal-aware context (SIGINT/SIGTERM cancel the command context, a second signal
force-exits, an interrupted run exits 128+signum), the shared error path, and
the telemetry flush. See the
[root command reference](reference/cli/root.md#signal-handling-and-exit-codes).

### 3. Provide embedded defaults

**`cmd/tool/assets/config.yaml`**, the lowest-precedence configuration layer,
and required for the `//go:embed` directive to have something to match:

```yaml
log:
  level: info
```

Defaults belong here and nowhere else. `default:` struct tags are treated as
hint text and never applied.

If your tool talks to a forge, add its block too, the framework's own defaults
already supply the GitHub URLs, so you only need what differs:

```yaml
vcs:
  provider: gitlab
gitlab:
  url:
    api: https://gitlab.example.com/api/v4
  auth:
    env: GITLAB_TOKEN
```

### 4. Build

```bash
go build -o bin/my-awesome-tool ./cmd/tool
./bin/my-awesome-tool --help
```

## Which built-in commands you get

Both routes give the same set, controlled by `props.Tool.Features`. `version` is
always present; the rest can be switched off:

```go
Tool: props.Tool{
	Features: props.SetFeatures(
		props.Disable(props.UpdateCmd),
		props.Disable(props.McpCmd),
	),
}
```

`config` and `telemetry` are opt-in and off by default. See
[Built-in features](how-to/builtin-features.md) and the
[commands overview](reference/cli/index.md).

## What neither route does for you

- **Neither writes your business logic.** The generator writes a stub that
  returns `ErrNotImplemented`.
- **Neither configures a forge or an AI provider.** Those are `init github`,
  `init ai` and friends, run against a tool that already builds.
- **Hand-wiring gives up regeneration permanently.** There is no supported path
  from a hand-wired project to a manifest-driven one; the generator expects to
  have written the layout it manages.
- **The generator does not merge into an existing project.** Point `--path` at a
  fresh directory.

## Next steps

- **[Build your first CLI](tutorials/build-your-first-cli.md)**: the same
  ground, step by step, with expected output.
- **[How-to guides](how-to/index.md)**: adding flags, binding them to config,
  testing, nested subcommands.
- **[Configuration keys](reference/config/index.md)**: every key, its default,
  and what happens when it is wrong.
- **[Concepts](explanation/concepts/index.md)**: Props, precedence, and the
  architecture behind them.
