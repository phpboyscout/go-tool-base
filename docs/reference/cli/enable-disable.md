---
title: enable / disable Commands
description: Framework-developer commands to toggle capabilities (signing, MCP, features) on a generated project.
date: 2026-06-26
tags: [reference, commands, enable, disable, features, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `enable` / `disable` Commands

`gtb enable` and `gtb disable` toggle capabilities on a generated project, either
named features, or the dedicated `signing` and `mcp` capabilities. Part of the
**framework-developer** CLI. For runtime feature flags in your tool's own code,
see [Configuring Built-in Features](../../how-to/builtin-features.md).

## Usage

```bash
gtb enable [feature...] [flags]
gtb enable signing [flags]
gtb enable mcp [command-path...] [flags]

gtb disable [feature...] [flags]
gtb disable signing [flags]
gtb disable mcp [command-path...] [flags]
```

All commands accept `--path, -p` (default `.`) for the project root.

## `enable` / `disable` `[feature...]`

Enable or disable named built-in features on the project. Pass one or more
feature names to toggle several at once (e.g. `gtb enable ai config telemetry`).
Each toggle flips `properties.features` in `.gtb/manifest.yaml` and re-renders
the generated root command's `props.SetFeatures(...)`. Because the change lives
in the manifest, it **survives `gtb regenerate project`**. The same command
runs the derived-file sync `regenerate` runs (the root command, the adapter
files, the signing files, the derived fields), so the tree builds without a
regenerate afterwards. The links `keychain` and `mcp` are toggleable too, and
for them the file is the toggle: `disable <id>` removes `cmd/<name>/<id>.go`
and `enable <id>` writes it back; nothing is rendered into `SetFeatures`. A
project without `mcp.go` ships without `go/mcp` and the MCP SDK (unless its
`claude-local` or `codex-local` chat provider brings the SDK in through
`go/chat-mcptools`, the tool bridge those providers need, and
`chat.tool_bridge` is not `false`).

With **no feature argument**, an interactive multi-select of the candidate
features (those not already in the target state) is shown. In a
non-interactive session or under CI there is nothing to prompt with, so the
command errors (`a feature name is required in non-interactive/CI mode`) rather
than printing state.

## `signing`

`enable signing` wires consumer-side release-signature verification; `disable
signing` removes it. With `--key-id` it also wires the release pipeline: the
GoReleaser `signs:` block, and in the file that runs GoReleaser
(`.gitlab-ci.yml` or `.github/workflows/goreleaser.yaml`) the `sigillum` install on GitHub, the
`aws-kms` OIDC credentials, and `--skip=sign` on GitHub builds that aren't tags.
The `aws-kms` backend reads the signer role from a `SIGNER_ROLE_ARN` CI variable,
which you set. A CI file you've edited, listed in `.gtb/ignore`, or take from a
template source is left as it is with a warning. An existing `signs:` block is
updated if it's gtb's own and the file is unedited; otherwise you get the block to
paste.

| Flag | Default | Description |
|------|---------|-------------|
| `--email` | — | Release WKD email (`external_key_email`); enables the external trust-anchor leg. |
| `--key-source` | `both` | Trust-anchor source: `embedded`, `external`, or `both`. |
| `--require-signature` | `false` | Fail updates closed when no valid signature is present (flip only once a signed release has shipped). |
| `--require-external-crosscheck` | `false` | Fail closed when the external (WKD) resolver is unreachable. |
| `--key-id` | — | Signing key id/ARN/alias (or PEM path) the release pipeline signs with. |
| `--backend` | *(aws-kms when `--key-id` set)* | Signing backend for the release pipeline: `aws-kms` or `local`, the two `sigillum` links. |
| `--kms-region` | `eu-west-2` | AWS region for the `aws-kms` backend. |
| `--public-key` | *(embedded signing-key)* | Path to the embedded public key the signature identifies. |

See [Secure Releases](../../how-to/secure-releases.md).

## `mcp`

`enable mcp` enables the MCP feature, or exposes specific commands as MCP tools;
`disable mcp` disables it, or withholds specific commands. Pass `command-path`
arguments to scope to individual commands. See
[Expose an MCP Server](../../how-to/framework-cli/expose-mcp-server.md).

What a path does depends on where its command comes from:

| The path names | `disable mcp <path>` | `enable mcp <path>` |
|---|---|---|
| A command gtb generated | Records `mcp_enabled: false` and re-renders its `cmd.go` with `setup.ExcludeFromMCP`. | Records `mcp_enabled: true` and re-renders its `cmd.go` with `setup.IncludeInMCP`. |
| Anything else: a built-in such as `config/trust`, or an attached command such as `sign` | Puts the path on `properties.mcp.withhold` and re-renders the root. | Takes the path off the list. |

```bash
gtb disable mcp sign config/trust   # withheld by path, from the next build
gtb enable mcp sign                 # back on the surface
```

For a path on the list:

- It covers the command and everything beneath it, and no marker overrules it.
- `enable mcp` on a path under a listed command is refused and names that
  command. Expose the listed command instead.
- `enable mcp` on a path that is neither generated nor listed changes nothing
  and says so: the command is already exposed.
- gtb cannot see a built-in or attached command from the project, so it does
  not check that the path exists. `disable mcp` names the paths it listed and
  tells you to confirm with `<tool> mcp tools` after the build. A listed path
  that names no command stops the built tool's `mcp start`, `mcp stream` and
  `mcp tools`.
- A path is command names separated by `/`, each of lowercase letters, digits,
  `-` or `_` and starting with a letter. Anything else is refused and nothing
  is written.
- Several paths in one call are all checked before any is written. The whole
  withhold list is then written before any generated command is re-rendered, so
  a failure while re-rendering leaves the list and the commands before it
  changed. Run the command again once the cause is fixed.
- The list survives `gtb disable mcp` with no path, a regenerate and a wizard
  revisit. `gtb set` and `gtb unset` refuse `mcp.withhold`; read the list in
  `.gtb/manifest.yaml`.

> Run any command with `--help` for the complete, authoritative flag set.
