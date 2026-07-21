---
title: Commands Overview
description: Overview of built-in commands available in all GTB applications.
date: 2026-02-16
tags: [components, commands, overview]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Commands Overview

GTB provides a set of essential built-in commands that are automatically included in every CLI tool. These commands provide core functionality for configuration management, version checking, self-updating, interactive documentation, and AI agent integration.

## Available Commands

| Command | Purpose |
| :--- | :--- |
| **[Root](root.md)** | Application entry point and service orchestration. |
| **[Init](init.md)** | Tool configuration and environment setup. |
| **[Config](config.md)** | Programmatic config access for CI and scripted setup. (opt-in) |
| **[Version](version.md)** | Version display and update checking. |
| **[Update](update.md)** | Automated binary updates and migration. |
| **[Docs](docs.md)** | Interactive TUI documentation browser, plus roff man-page generation. |
| **Man** | Hidden, opt-in roff man-page emitter for packaging/preview. See [Docs › Man-page generation](../explanation/components/docs.md#man-page-generation). (opt-in) |
| **[Doctor](doctor.md)** | Environment and configuration health checks, plus `doctor report` — a redacted, paste-ready support bundle. |
| **[Changelog](changelog.md)** | Embedded changelog display. |
| **[MCP](mcp.md)** | AI agent integration (Model Context Protocol). |
| **[Telemetry](telemetry.md)** | Opt-in usage telemetry status and management. (opt-in) |

## Framework-developer commands (`gtb`)

These commands are part of the `gtb` binary itself — the tooling you use to *build*
a CLI on GTB. They are not shipped in your generated tool.

| Command | Purpose |
| :--- | :--- |
| **[generate](generate.md)** | Scaffold projects, commands, flags, docs, and man pages. |
| **[regenerate](regenerate.md)** | Rebuild a project from its manifest, or the manifest from source. |
| **[remove](remove.md)** | Remove a generated command from a project. |
| **[keys](keys.md)** | Generate, mint, and publish OpenPGP signing keys. |
| **[sign](sign.md)** | Produce an OpenPGP detached signature for a file. |
| **[enable / disable](enable-disable.md)** | Toggle capabilities (signing, MCP, features) on a project. |
| **[template](template.md)** | Manage custom template-overlay sources. |

---

## Feature flags

Built-in commands are registered automatically by `root.NewCmdRoot`. Each is gated
by a feature-flag constant in `props`; the default-enabled set ships in every tool,
and the opt-in set must be turned on explicitly.

| Constant | Command | Default |
|----------|---------|---------|
| `props.UpdateCmd` | `update` | enabled |
| `props.InitCmd` | `init` | enabled |
| `props.McpCmd` | `mcp` | enabled |
| `props.DocsCmd` | `docs` | enabled |
| `props.DoctorCmd` | `doctor` | enabled |
| `props.ChangelogCmd` | `changelog` | enabled |
| `props.AiCmd` | AI config in `init` | opt-in |
| `props.ConfigCmd` | `config` | opt-in |
| `props.TelemetryCmd` | `telemetry` | opt-in |
| `props.ManCmd` | `man` | opt-in |

The `version` command is always registered and cannot be disabled.

To toggle these (via `props.SetFeatures(props.Enable(…)/props.Disable(…))`) see
[Configuring Built-in Features](../../how-to/builtin-features.md); to add your own
commands see [Adding Custom Commands](../../how-to/custom-commands.md).
