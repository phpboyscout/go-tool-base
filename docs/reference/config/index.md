---
title: Configuration Keys
description: Reference for the configuration keys the GTB framework reads, with types, defaults, and env-var mapping.
date: 2026-06-26
tags: [reference, config, configuration, keys, environment]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration Keys

This page lists the configuration keys the **framework itself** reads. Tools built
on GTB add their own keys on top. For how these are loaded and merged, see
[Sources & Precedence](../../explanation/components/config/sources-and-precedence.md).

**Precedence** (highest to lowest): CLI flags → environment variables → config
files (later files override earlier) → embedded defaults.

**Environment mapping.** Every key maps to an environment variable as
`<PREFIX>_<KEY>`, upper-cased with `.` replaced by `_` — e.g. with prefix
`MYTOOL`, `log.level` reads `MYTOOL_LOG_LEVEL`. The prefix is set by the tool
author. Keys ending in `.env` are different: their **value** is the *name* of an
environment variable that holds a credential (see [Credentials](#credentials)).

## Core

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `log.level` | string | `info` | Log verbosity — one of `debug`, `info`, `warn`, `error`. The one schema-required key. |

## Self-update

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `update.policy` | string | *(tool baseline)* | `disabled` (log only), `prompt` (ask), or `enabled` (block until updated). Empty defers to `props.Tool.UpdatePolicy`. |
| `update.check_interval` | duration | *(tool baseline)* | How often to check for updates (e.g. `24h`); `0` checks every run. Empty defers to the tool baseline, then `24h`. |
| `update.require_checksum` | bool | `false` | Require a verified checksum before applying an update. |
| `update.require_signature` | bool | `false` | Require a valid release signature before applying an update. |
| `update.require_external_crosscheck` | bool | `false` | Require an external cross-check of the release before applying. |

See [Configure Self-Updating](../../how-to/configure-self-updating.md) and
[Secure Releases](../../how-to/secure-releases.md).

## AI / chat

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ai.provider` | string | `""` | Active provider: `openai`, `anthropic`/`claude`, `gemini`, … |
| `ai.model` | string | *(provider default)* | Model id for the active provider. |
| `ai.max_tokens` | int | *(provider default)* | Max tokens per completion. |
| `ai.claude.local` | bool | `false` | Use the local `claude` CLI binary instead of the API. |

Provider credentials follow the credential pattern below
(`openai.api.*`, `anthropic.api.*`, `gemini.api.*`). See
[AI Provider Setup](../../how-to/ai-integration.md).

## Version control

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `github.url.api` | string | `https://api.github.com` | GitHub API base URL (set for GitHub Enterprise). |
| `github.url.upload` | string | `https://uploads.github.com` | GitHub upload URL. |
| `github.auth.env` | string | `GITHUB_TOKEN` | Name of the env var holding the GitHub token. |
| `github.ssh.key.env` | string | `GITHUB_KEY` | Name of the env var holding the SSH key. |

GitLab and other providers expose the analogous `<provider>.url.*` / `<provider>.auth.*`
keys. See the [VCS component](../../explanation/components/vcs/index.md).

## Telemetry

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `telemetry.enabled` | bool | `false` | Master opt-in for usage telemetry. |
| `telemetry.local_only` | bool | `false` | Collect locally but never transmit. |

See the [Telemetry reference](../cli/telemetry.md) and
[component docs](../../explanation/components/telemetry/index.md).

## Server

Read by tools that use `pkg/http`, `pkg/grpc`, or `pkg/controls`. Not present in a
plain CLI.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server.http.port` | int | — | HTTP listener port. |
| `server.http.tls.enabled` | bool | `false` | Enable TLS on the HTTP listener. |
| `server.http.max_header_bytes` | int | — | Max request-header size. |
| `server.grpc.port` | int | — | gRPC listener port. |
| `server.grpc.reflection` | bool | `false` | Enable gRPC server reflection. |
| `server.admin.port` | int | — | Admin/management listener port. |
| `server.admin.reflection` | bool | `false` | Enable reflection on the admin listener. |
| `server.tls.enabled` | bool | `false` | Enable TLS globally for the server. |

## Credentials

Secrets (AI API keys, VCS tokens) resolve through a fixed precedence so the literal
value need never be written to disk. For a provider/section, GTB checks, in order:

1. `<section>.api.env` (or `auth.env`) → read the named environment variable
2. `<section>.api.keychain` (or `auth.keychain`) → read the OS keychain
3. `<section>.api.key` (or `auth.value`) → the literal value in config (legacy)
4. a well-known fallback env var (e.g. `OPENAI_API_KEY`)

Literal mode is refused under `CI=true`. See the
[Credentials component](../../explanation/components/credentials.md) and
[Configure Credentials](../../how-to/configure-credentials.md).

## Related environment variables

A few env vars are read directly, independent of the config prefix:

| Variable | Effect |
|----------|--------|
| `CI` | When `true`, disables interactive prompts and refuses literal-credential mode. |
| `EDITOR` / `VISUAL` | Editor used by interactive flows (e.g. `config edit`). |
