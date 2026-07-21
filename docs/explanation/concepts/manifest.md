---
title: The Manifest 🧠
description: Detailed explanation of the manifest.yaml file, its schema, and its role in project structure.
date: 2026-02-16
tags: [cli, manifest, configuration, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# The Manifest 🧠

Every great tool needs a brain, and for your CLI, that brain is the **Manifest**.

Located at `.gtb/manifest.yaml`, this file is the single source of truth for your project's command structure. It keeps track of every command, every flag, and every description, ensuring that your generated code stays consistent and recoverable.

## Why do we need a manifest?

In traditional code generation, once you write boilerplate, it's often "fire and forget." If you wanted to add a flag later, you'd have to manually edit the Cobra code, risking bugs or inconsistencies.

The Manifest changes that game. By storing the *intent* of your CLI (what commands exist, what flags they have) separately from the *implementation* (the Go code), we can:

1.  **Regenerate Boilerplate**: Update `cmd.go` files automatically without touching your custom logic in `main.go`.
2.  **Ensure Consistency**: Keep descriptions and flag types synchronized across your tool.
3.  **Simplify Evolution**: Add new features like flags or assets commands via the CLI, knowing the generator will handle the wiring for you.

## The Schema

The manifest is a simple YAML file. Here's what it looks like:

```yaml
properties:
  name: my-cli-tool
release_source:
  type: github
  owner: my-org
  repo: my-repo
version:
  gtb: v1.0.0
commands:
  - name: root
    description: The root command
    withAssets: true
  - name: server
    description: Manage the server
    longDescription: |
      The server command allows you to start, stop, and configure
      the backend server for the application.
    flags:
      - name: port
        type: int
        description: Port to listen on
      - name: verbose
        type: bool
        description: Enable verbose logging
    commands:
      - name: start
        description: Start the server
```

### Key Fields

- **properties**: Global project settings (name, repo, host, description, features, help channel).
- **release_source**: Where the tool's releases are hosted. `type` is `github` or `gitlab`; `owner` is the organisation or user; `repo` is the repository name.
- **version**: Records the GTB version used to generate the project (`gtb: vX.Y.Z`).
- **commands**: A recursive list of all commands in your tool.
    - **name**: The command name (e.g., `server`).
    - **description**: The short description used in help text.
    - **longDescription**: A detailed explanation of the command.
    - **withAssets**: Whether the command bundles static assets.
    - **aliases**: A list of alternative names for the command.
    - **hidden**: Whether the command is hidden from help text.
    - **args**: Positional argument validation (e.g., `ExactArgs(1)`, `ArbitraryArgs`).
    - **hash**: SHA256 hash of the generated `cmd.go` content (used for change detection).
    - **protected**: Whether the command is write-protected (preventing overwrite).
    - **persistentPreRun**: Whether to generate a persistent pre-run hook.
    - **preRun**: Whether to generate a pre-run hook.
    - **withInitializer**: Whether to generate a config Initializer for this command (`init.go` + `Init<Name>` stub in `main.go`).
    - **mutuallyExclusive**: A list of flag groups where only one flag can be set (e.g., `[["json", "yaml"]]`).
    - **requiredTogether**: A list of flag groups that must be set together.
    - **flags**: A list of flags associated with the command.
        - **name**: The flag name (e.g., `port`).
        - **type**: The data type (`string`, `int`, `bool`, `float64`, `stringSlice`, `intSlice`).
        - **description**: Help text for the flag.
        - **shorthand**: A single-letter shorthand for the flag (e.g., `p`).
        - **default**: The default value for the flag.
        - **required**: Whether the flag is required.
        - **persistent**: Whether the flag is persistent (inherited by subcommands).
        - **hidden**: Whether the flag is hidden from help text.
    - **commands**: Nested subcommands (e.g., `start` under `server`).

### Properties: the recorded project posture

`properties` records more than the tool's name and repo. It is where GTB keeps
the author-set posture that has no other home — the settings a human chose that
the generated Go source does not fully encode:

- **features**: The built-in feature toggles (e.g. `ai`, `config`, `telemetry`,
    plus the scaffold-only `keychain`). Only entries that differ from the
    framework default are recorded, so the block stays minimal.
- **signing**: The self-update signing posture (backend, key id/region, public
    key path, enforcement flags).
- **templates**: Custom template-overlay provenance and pins — `{name, type,
    location, ref, resolved, fingerprint, hashes}` per source.
- **telemetry**: Endpoint configuration for the analytics/observability
    surfaces.
- **ci**: The CI component source override (`ci.component_source`) when a
    downstream repoints the include base away from the default.
- **module_published**: Whether the project is a published module.
- **docs_layout**: Which documentation tree shape (Diátaxis vs the legacy flat
    layout) the project uses.

Per-command, the manifest also records **`mcp_enabled`** — whether a command is
exposed over MCP — so that gating round-trips through both regeneration
directions.

### Why `provenance.go` exists

A handful of those recorded properties — the **signing posture**, the
**template-overlay pins**, and **`module_published`** — appear *nowhere else* in
the generated Go source. They are not encoded in `cmd.go`, `main.go`, or any
other artefact the scanner can read. This is a problem for recovery: the whole
point of the manifest is that it can be rebuilt from in-tree source after
`.gtb/` is deleted (see [Regeneration & Synchronization](regeneration.md)), and
you cannot rebuild what was never written down.

To close that gap the generator emits `pkg/cmd/root/provenance.go`: a tracked,
**inert** Go file (it compiles, `package root`, but has no runtime effect)
carrying those facts as structured comment annotations:

```go
// Code generated by gtb. DO NOT EDIT.
//
// Generation provenance not present elsewhere in the generated source, so
// `gtb regenerate manifest` can reconstruct it from scratch. No runtime effect.
//
// gtb:signing enabled=true backend=aws-kms key_id=alias/my-key ...
// gtb:template name=corp-overlay type=git location=... resolved=<sha> ...
// gtb:module published=true

package root
```

Because it is committed in-tree, it survives a `.gtb/` deletion. When
`regenerate manifest` rebuilds the manifest from scratch, it reads these
`// gtb:signing` / `// gtb:template` / `// gtb:module` lines back to recover the
signing block, the template sources, and `module_published`. The file is emitted
**only when there is something to record** and removed otherwise, so a plain
project without signing, overlays, or a published module carries no extra file.

## Working with the Manifest

### Automatic Updates ✨

You rarely need to edit this file manually! The `generate` commands handle it for you:

- `generate command`: Adds new entries to the `commands` list.
- `generate add-flag`: Updates the `flags` list for a specific command.

### Manual Edits 🛠️

If you need to make a quick text change—like fixing a typo in a description—you can edit `manifest.yaml` directly.

!!! warning "Manual Updates"
    Changes made manually to the manifest won't be reflected in your Go code until you run a regeneration command. For structural changes, always rely on `generate add-flag` to ensure your code stays in sync!
