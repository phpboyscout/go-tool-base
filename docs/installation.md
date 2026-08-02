---
title: Installation
description: Install the gtb CLI, add the go-tool-base library to a project, and verify both with a minimal tool that compiles.
date: 2026-08-02
tags: [installation, setup, configuration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
hide:
  - navigation
---

# Installation

GTB is two things: a Go library you import into a CLI project, and a `gtb`
automation CLI that scaffolds and regenerates those projects. You can use either
without the other, though the generator is the fastest route in.

## Prerequisites

- **Go 1.26.5 or later.** That is the `go` directive in `go.mod`; an older
  toolchain refuses to build the module. Generated projects default to the
  toolchain you generate with, overridable with `--go-version`.

## Install the gtb CLI

The recommended route is a pre-built release binary. A source build omits the
gitignored assets the embedded documentation browser needs, so `gtb docs` is
degraded on one.

### Linux and macOS

```bash
curl -sSL "https://gitlab.com/phpboyscout/go-tool-base/-/raw/main/install.sh" | bash
```

The script installs to `$HOME/.local/bin`. Make sure that is on your `$PATH`.

### Windows (PowerShell)

```powershell
irm "https://gitlab.com/phpboyscout/go-tool-base/-/raw/main/install.ps1" | iex
```

### From source

```bash
go install gitlab.com/phpboyscout/go-tool-base/cmd/gtb@latest
```

The `cmd/gtb` suffix matters — the module root is a library and has no `main`
package, so `go install gitlab.com/phpboyscout/go-tool-base@latest` fails.
Ensure `$GOPATH/bin` is on your `$PATH`.

### Does the installer need a token?

No. Releases are published to `gitlab.com/phpboyscout/go-tool-base`, and the
install scripts resolve the latest release from the GitLab API anonymously.

Both scripts accept an optional `GITLAB_TOKEN`, useful behind a private mirror or
to avoid anonymous rate limits in CI. When set it is sent as a `PRIVATE-TOKEN`
header. A token with the `read_api` scope, or a project access token with the
Reporter role, is enough; nothing broader is needed to download a release asset.

## Add the library to a project

```bash
go mod init your-tool-name
go get gitlab.com/phpboyscout/go-tool-base
```

Or add the import and let the toolchain resolve it:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
```

```bash
go mod tidy
```

## A minimal tool that compiles

This is the smallest program that produces a working GTB CLI. It builds against
v0.35.0.

**`main.go`:**

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
			Name:        "example-tool",
			Summary:     "An example CLI tool",
			Description: "Demonstrates GTB usage",
			EnvPrefix:   "EXAMPLE_TOOL",
			ReleaseSource: props.ReleaseSource{
				Type:  "github",
				Owner: "your-org",
				Repo:  "example-tool",
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

	rootCmd := root.NewCmdRoot(p)

	// Add your own commands here:
	// rootCmd.Register(mycmd.NewCmdMine(p))

	root.Execute(rootCmd, p)
}
```

**`assets/config.yaml`** — the embedded defaults layer. The `//go:embed assets/*`
directive requires the directory to exist and be non-empty:

```yaml
log:
  level: info
```

Four details are load-bearing:

- **`props.New` rather than a struct literal.** It applies the framework defaults
  — a no-op telemetry collector, an error handler built from your logger, an
  empty version — and returns an error if `Tool.Name`, the logger or the
  filesystem is missing, instead of panicking later. A struct literal still
  works, but then those defaults are yours to set.
- **`props.NewAssets` takes an `AssetMap`,** not a bare `*embed.FS`. The key
  names the bundle; later bundles override earlier ones.
- **`ReleaseSource`, not a `GitHub` field.** `props.Tool` carries
  `ReleaseSource{Type, Host, Owner, Repo, Private, Params}`, where `Type` is
  `github`, `gitlab`, `bitbucket`, `gitea`, `codeberg` or `direct`.
- **`root.Execute(rootCmd, p)`, not `rootCmd.Execute()`.** `root.Execute` adds
  the signal-aware context, the shared error path and the telemetry flush.
  Calling Cobra's `Execute` directly skips all three.

Setting `EnvPrefix` is optional but worth doing: without one, the tool has no
environment-variable configuration layer at all.

**Build and run:**

```bash
go build -o example-tool .
./example-tool --help
./example-tool version
```

`--help` lists the built-in commands your feature set enables — `init`,
`version`, `update`, `docs`, `doctor`, `changelog`, `mcp` — none of which you
wrote.

## Verifying an install

1. `gtb version` prints a version, a commit and a build date.
2. `go build .` completes in your project.
3. `./your-tool --help` lists the built-in commands.
4. `./your-tool version` prints your version.

Most commands stop with `no config file found` until you run `your-tool init`.
That is expected: the tool will not guess at configuration it does not have. See
[Auto-initialise config](how-to/auto-initialise-config.md) to change that.

## Common problems

**`module gitlab.com/phpboyscout/go-tool-base@latest found (v0.35.0), but does
not contain package gitlab.com/phpboyscout/go-tool-base`.** The module root has
no `main` package. Install
`gitlab.com/phpboyscout/go-tool-base/cmd/gtb@latest` instead.

**`go.mod requires go >= 1.26.5`.** Your toolchain is older than the module
requires. Upgrade Go, or pin an older GTB release.

**`pattern assets/*: no matching files found`.** The `//go:embed` directive needs
the directory to exist and contain at least one file. Create
`assets/config.yaml`.

**`props: Tool.Name is required`** from `props.New`. The three required fields
are `Tool.Name`, the logger and the filesystem; the rest are defaulted.

## Next steps

- **[Build your first CLI](tutorials/build-your-first-cli.md)** — the guided
  route, using the generator rather than hand-wiring.
- **[Getting started](getting-started.md)** — the two integration routes side by
  side.
- **[Props](explanation/components/props.md)** — what the container carries and
  why every command receives one.
- **[Configuration keys](reference/config/index.md)** — every key, its default,
  and what happens when it is wrong.
