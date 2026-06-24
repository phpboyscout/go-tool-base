---
title: "Credentials Taxonomy & Architecture"
description: "The conceptual storage modes, trust model, and consumer architecture for GTB credentials."
date: 2026-06-24
tags: [concepts, security, credentials]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Credentials Architecture

`pkg/credentials` provides a shared taxonomy for how GTB — and tools built on GTB — persist user-supplied secrets. This document explains the architectural trust models and storage modes. For the API reference, see [Credentials API](../components/credentials.md).

## Storage Modes

Three `Mode` values are supported:

| Mode | Value | What gets written to config | Where the secret lives |
|------|-------|-----------------------------|-----------------------|
| `ModeEnvVar` | `"env"` | The **name** of an env var (`GITHUB_TOKEN`, `ANTHROPIC_API_KEY`, …) | Process environment, shell profile, or CI secret injection |
| `ModeKeychain` | `"keychain"` | A `<service>/<account>` reference | OS keychain (macOS Keychain, Linux Secret Service, Windows Credential Manager) — **only available when the `pkg/credentials/keychain` subpackage is imported** |
| `ModeLiteral` | `"literal"` | The secret itself | The config file |

`ModeEnvVar` is the recommended default and the only mode permitted under `CI=true`. `ModeLiteral` is supported for backward compatibility and throwaway environments; the setup wizard refuses it under CI and the doctor `credentials.no-literal` check warns on its presence.

## Consumers

| Subsystem | Relationship to `pkg/credentials` |
|-----------|-----------------------------------|
| `pkg/setup/ai` | Storage-mode selector uses `AvailableModes()` gated on `Probe()`; the chosen mode decides whether `<provider>.api.env`, `<provider>.api.keychain`, or `<provider>.api.key` is written. Keychain mode also writes the secret via `credentials.Store` — it never hits the config file. |
| `pkg/setup/github` | CI refusal for `ModeLiteral`; falls back to manual PAT entry when the OAuth flow cannot launch a browser. |
| `pkg/chat` | `resolveAPIKey` walks five steps: direct → `<provider>.api.env` (ref) → `<provider>.api.keychain` (lookup) → `<provider>.api.key` (literal) → well-known env fallback. Env-aware via Viper's `AutomaticEnv`. |
| `pkg/vcs` | `ResolveToken` walks `auth.env` → `auth.keychain` → `auth.value` → fallback env. Used by GitHub, GitLab, Gitea, and the direct provider. |
| `pkg/vcs/bitbucket` | `resolveCredentials` walks `bitbucket.<field>.env` → shared `bitbucket.keychain` JSON blob (`{username, app_password}`) → literals → well-known env. A corrupt keychain blob aborts resolution rather than silently falling back to a stale literal. |
| `pkg/cmd/doctor` | `credentials.no-literal` check scans for `ModeLiteral`-style config values and warns. |
| `pkg/cmd/config` | The masker recognises `auth`/`username`/`password`/`api` mid-path segments so `config get`/`config list` render literal secrets as `****<tail>`. |

## Trust Model

| Deployment | Recommended mode |
|-----------|-------------------|
| Local dev | Env-var reference (shell profile, `direnv`) or keychain |
| CI/CD pipelines | Env-var reference, populated by the CI platform's secret injection |
| Containerised / Kubernetes | External secret injection (Kubernetes Secrets, CSI) mounted as env vars |
| Throwaway / air-gapped | Literal value in config, accepting the plaintext-on-disk risk |
| Regulated / compliance-audited | Env-var reference only; do **not** import `pkg/credentials/keychain` in the tool's `main` |

Full trust-model guidance is in [`docs/development/security-decisions.md`](../../development/security-decisions.md#h-1-2026-04-02-audit-plaintext-credentials-in-config-files).
