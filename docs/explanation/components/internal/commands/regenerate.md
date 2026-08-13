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

**The rebuild reconciles; it does not replace.** The scan is authoritative for
everything it can read out of `cmd.go` — structure, nesting, descriptions,
flags, hooks, argument contracts — and the existing manifest keeps the
per-command state no generated file carries:

- `hashes`, the drift-detection baseline `doctor` reads;
- `hidden`, which is written into `cmd.go` but not read back out;
- `protected`, which is manifest-only and reaches no generated file at all.

A command the scan finds but the manifest did not know about starts with none of
these, which is what a newly-discovered command should have.

Descriptions survive the trip intact, including multi-line ones. A value spanning
several lines is written back as a YAML block scalar rather than a plain scalar
with escaped newlines — a plain scalar does not interpret `\n`, so escaping there
would put a literal backslash-n into the command's help output.

Feature-state handling depends on whether a manifest already exists:

- **Manifest present** (the common case): `properties.features` is **preserved**
  from the existing manifest rather than re-derived. When a manifest is on disk
  it stays authoritative for author-set posture, and only the fields the root
  `cmd.go` is the source of truth for (name, description, release source) are
  refreshed.
- **From scratch** (`.gtb/` deleted, no manifest): feature state is **fully
  re-derived** from in-tree source. The built-in features come from the root
  command's `props.SetFeatures(...)` literal via the shared
  `templates.FeatureCatalogue`, and the scaffold-only `keychain` feature — which
  has no `FeatureID` and so never appears in that literal — is recovered from
  the presence of `cmd/<name>/keychain.go` (`recoverNonLiteralProperties`).

So the manifest is a convenience, not the only record: a from-scratch rebuild
reconstructs the full feature set (keychain included) from the source tree. See
the reconstruction guarantee in
[Regeneration & Synchronization](../../../concepts/regeneration.md#the-manifest-is-recoverable-not-precious).

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

**A regeneration converges: running it twice changes nothing the second time.**
The manifest tracks hashes in two places — project-level files under
`hashes:`, and each command's files under that command's `hashes:` — and both
are re-read from disk once post-processing (`go mod tidy`, `golangci-lint
run --fix`) has finished. A file is therefore recorded with the bytes that
actually landed, not the bytes as first rendered, so a later pass reformatting
gtb's own output does not read as developer drift on the next run.

Files you have declared yours are exempt: a path kept at the conflict prompt, or
covered by a `.gtb/ignore` rule, keeps its previously stored hash rather than
adopting what is on disk. That is deliberate — adopting it would silently make
your edit the new baseline and overwrite it without asking next time.

**Regenerating inside a git worktree is safe.** It used not to be: the
post-generation `golangci-lint --fix` could rewrite files in the *original*
checkout, silently, while the generated output landed correctly in the worktree.

The cause is upstream. golangci-lint caches findings against absolute paths and
returns them from a later run in a different directory, so run from a worktree
of a repo that has been linted elsewhere, `--fix` writes to the paths in the
cache — files in the other checkout. Nothing in the generator misresolved
anything; the leaked files were simply whatever lint had analysed there before.

gtb therefore gives golangci-lint a cache keyed to the directory being linted. A
worktree cannot inherit entries belonging to another checkout, and repeat runs
in the same directory still hit a warm cache. If you have set
`GOLANGCI_LINT_CACHE` yourself it is left alone — that is an explicit choice —
but be aware it reopens the behaviour above.

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
