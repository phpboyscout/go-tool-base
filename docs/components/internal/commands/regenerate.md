---
title: Regenerate Command
description: Internal command for synchronizing project state with the manifest file.
date: 2026-02-16
tags: [components, internal, commands, regenerate]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Regenerate Command

The `regenerate` command group is used to synchronize the project state with the `manifest.yaml` Source of Truth.

## Help Output (`regenerate --help`)

```text
Regenerate project components from manifest or rebuild the manifest from existing source code.

Usage:
  gtb regenerate [command]

Available Commands:
  manifest    Regenerate manifest from source code
  project     Regenerate project from manifest

Flags:
  -h, --help   help for regenerate

Global Flags:
      --ci                   flag to indicate the tools is running in a CI environment
      --config stringArray   config files to use (default [/home/mcockayne/.gtb/config.yaml,/etc/gtb/config.yaml])
      --debug                forces debug log output
```

## Subcommands

### Manifest

Scans the filesystem for existing commands and updates `manifest.yaml`. Use this if you have manually added files or if the manifest is out of sync.

The scan walks the full `pkg/cmd/<parent>/<child>/…` tree and recognises both
cobra's `parent.AddCommand(child…)` and the GTB `setup.Command` wrapper's
`parent.Register(child…)`, so **nested subcommands are preserved** in
`commands[].commands` rather than dropped as orphans.

Each command's `Short`/`Long` are read out of its generated `cmd.go` even when
the cobra literal is wrapped in the `setup.Wrap("name", &cobra.Command{…})`
middleware helper, so descriptions survive the rebuild. (Without this, a
following `regenerate project` would render the blanked descriptions back into
every `cmd.go`, wiping the help text.)

`properties.features` is **preserved** from the existing manifest rather than
re-derived from the generated root command: feature state (including the
scaffold-only `keychain` feature and the opt-ins) is author configuration that
the root cannot losslessly express, so the manifest remains its source of truth.

**Help (`regenerate manifest --help`):**

```text
Scan the project for cobra.Command definitions and rebuild the manifest.yaml file.

Usage:
  gtb regenerate manifest [flags]

Flags:
  -h, --help          help for manifest
  -p, --path string   Path to project root (default ".")
```

### Project

Re-renders all `cmd.go` boilerplate files based on the structure defined in `manifest.yaml`. This is non-destructive to `main.go` files unless `--force` is used.

**Operator-owned seed files are never overwritten.** Files gtb scaffolds once and
then hands to the developer — the init assets it seeds (`pkg/cmd/**/assets/**`,
e.g. the `init/config.yaml` you fill in), the project `README.md`, the docs
landing page (`docs/index.md`), and the `justfile` — are preserved on every
`regenerate project`, **even under `--overwrite allow`**. Framework-structural
files (the generated `cmd.go`, CI pipelines, `.goreleaser.yaml`) remain
gtb-managed and continue to update. Use `--dry-run` to preview the blast radius.

**Help (`regenerate project --help`):**

```text
Regenerate all command registration files (cmd.go) based on the manifest.yaml.
Does not overwrite implementation files (main.go) unless --force is provided.

Usage:
  gtb regenerate project [flags]

Flags:
      --force         Overwrite existing main.go implementation files
  -h, --help          help for project
  -p, --path string   Path to project root (default ".")
```
