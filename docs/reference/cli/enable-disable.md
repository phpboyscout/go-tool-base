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
in the manifest, it **survives `gtb regenerate project`**.

With **no feature argument**, an interactive multi-select of the candidate
features (those not already in the target state) is shown. In a
non-interactive session or under CI there is nothing to prompt with, so the
command errors (`a feature name is required in non-interactive/CI mode`) rather
than printing state.

## `signing`

`enable signing` wires consumer-side release-signature verification (and the
GoReleaser signing block when `--key-id` is set); `disable signing` removes it.

| Flag | Default | Description |
|------|---------|-------------|
| `--email` | — | Release WKD email (`external_key_email`); enables the external trust-anchor leg. |
| `--key-source` | `both` | Trust-anchor source: `embedded`, `external`, or `both`. |
| `--require-signature` | `false` | Fail updates closed when no valid signature is present (flip only once a signed release has shipped). |
| `--require-external-crosscheck` | `false` | Fail closed when the external (WKD) resolver is unreachable. |
| `--key-id` | — | Signing key id/ARN/alias (or PEM path) the release pipeline signs with. |
| `--backend` | *(aws-kms when `--key-id` set)* | `gtb sign` backend for the release pipeline. |
| `--kms-region` | `eu-west-2` | AWS region for the `aws-kms` backend. |
| `--public-key` | *(embedded signing-key)* | Path to the embedded public key the signature identifies. |

See [Secure Releases](../../how-to/secure-releases.md).

## `mcp`

`enable mcp` enables the MCP feature, or exposes specific commands as MCP tools;
`disable mcp` disables it, or withholds specific commands. Pass `command-path`
arguments to scope to individual commands. See
[Expose an MCP Server](../../how-to/framework-cli/expose-mcp-server.md).

> Run any command with `--help` for the complete, authoritative flag set.
