---
title: gtb template
description: Manage custom template-overlay sources on a generated project — add, update, remove, and list the sources recorded in .gtb/manifest.yaml.
date: 2026-06-16
tags: [cli, generator, templates, overlay]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `gtb template`

Manage [custom template-overlay sources](../custom-templates.md) on a generated project. Each mutating verb edits the project's `.gtb/manifest.yaml` and regenerates, so the overlay (and any `gtb-template.yaml` `replaces:` suppression) is applied or undone.

The manifest-edit + `gtb regenerate` path is the source of truth; these subcommands are ergonomics over it.

## Subcommands

| Command | Purpose |
|---------|---------|
| `gtb template add <src>@<ref> [--name <handle>]` | Append a source, pin it (resolve a git ref to its commit SHA, or fingerprint a local folder), and regenerate. A remote source prompts a one-time trust confirmation (skipped under `--ci`). A rejected overlay rolls the manifest back. |
| `gtb template update <name>` | Re-resolve a git source's `ref` to a new commit and regenerate. The **only** pin-advancing path. |
| `gtb template remove <name>` | Remove a source and regenerate, restoring any embedded scaffold a `replaces:` had suppressed. |
| `gtb template list` | Show recorded sources, refs, and resolved commits. |

All accept `-p, --path` (project root, defaults to workspace detection from CWD).

## Examples

```bash
# Add a local house-templates folder.
gtb template add ./house-templates --name house

# Add a forge repo pinned to a tag (prompts to trust the remote source).
gtb template add acme/gtb-templates@v1.2.0 --name acme

# Advance the acme source to the newest commit on its ref.
gtb template update acme

# List everything.
gtb template list

# Remove the house source (restores any scaffold it replaced).
gtb template remove house
```

## Security

Custom templates execute `text/template` content GTB did not author and are treated as **trusted input with a bounded blast radius**, not sandboxed code. Adding a source is the trust decision; the SHA pin records exactly what was trusted. Hard controls (write-path containment, a protected-path denylist, a restricted FuncMap, a metadata-only data contract, inert fetch) apply on every render. See [Template Security](../../development/template-security.md#custom-template-overlays-a-different-threat-model).
