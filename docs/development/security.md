---
title: "Secrets & Configuration Model"
description: "Architecture and deployment guide for managing secrets and environment-specific configuration in GTB."
date: 2026-03-24
tags: [development, security, configuration, secrets]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Secrets & Configuration Model

GTB uses a layered configuration model powered by the extracted
[go/config](https://config.go.phpboyscout.uk) Store. Precedence is the order
layers are declared, so the resolution below is a property of one constructor
call rather than an emergent merge. This approach allows tools to remain
environment-agnostic while supporting secure secrets management in both
development and production.

## Configuration Priority

Configuration keys resolve in the following priority order (highest to lowest):

1. **Changed CLI Flags**: e.g., `--server-port 9090` (`server.port` by the
   hyphen-to-dot convention). Only flags the user actually set contribute — a
   flag at its default never overrides configuration.
2. **Environment Variables**: e.g., `MYTOOL_SERVER_PORT=9090` under the tool's
   `EnvPrefix`.
3. **Project-local `.<tool>.yaml`**: discovered by walking up from the working
   directory (a repo-root convention like `.editorconfig`). Suppressed entirely
   when `--config` names files explicitly.
4. **Config Files**: the `--config` paths if given (repeatable; later files
   win), otherwise `~/.<tool>/config.yaml` then `/etc/<tool>/config.yaml`.
5. **The tool's explicit embedded config assets** (`ConfigPaths`).
6. **Embedded Defaults**: `assets/config.yaml` merged across every registered
   asset bundle — the framework baseline, enabled features, and the tool's
   own. Defaults always apply: a key omitted from your file resolves to the
   shipped default rather than a zero value.

`config get`/`doctor` can explain any resolved value's provenance — the store
records which layer supplied every key.

## Environment Variable Mapping

Environment variables reach configuration only under the tool's `EnvPrefix` —
an unprefixed variable on a shared runner cannot silently reconfigure a tool.
The name after the prefix maps onto the configuration key, resolved against
the keys the lower layers define:

| Config Key | Environment Variable (prefix `MYTOOL`) |
| :--- | :--- |
| `server.http.port` | `MYTOOL_SERVER_HTTP_PORT` |
| `github.auth.value` | `MYTOOL_GITHUB_AUTH_VALUE` |
| `log.level` | `MYTOOL_LOG_LEVEL` |

Well-known unprefixed variables such as `GITHUB_TOKEN` are honoured by the
credential resolution chain (see Credential Storage below), not by the
configuration layer.

## Deployment Models

### Development Environment

In local development, secrets (like API keys or database passwords) are typically stored in local configuration files (`config.yaml` or `.env` files).

- **Key Practice**: Ensure local config files containing secrets are added to your `.gitignore`.
- **Threat Model**: Local file secrets are equivalent to environment variables in your shell profile—they are secure provided the local machine is not compromised.

### Production (Containers/Kubernetes)

In production, secrets should **never** be committed to version control, baked into container images, or passed as build arguments. They are runtime dependencies provided by the deployment platform.

#### 1. Kubernetes Secrets
Mount secrets as volumes containing configuration files:

```yaml
volumeMounts:
- name: config-volume
  mountPath: /etc/mytool
volumes:
- name: config-volume
  secret:
    secretName: mytool-config
```

#### 2. Secret Managers (Vault, AWS Secrets Manager)
Use CSI drivers or external secrets operators to inject secrets as files or environment variables directly into the application's environment.

#### 3. Environment Variable Injection
Inject secrets directly as environment variables. This is the simplest method for cloud platforms like Heroku, AWS Lambda, or simple Docker Compose setups.

## Core Principles

1. **Secrets are Runtime Dependencies**: They belong to the environment, not the application code.
2. **Standard Config Paths**: GTB provides the abstraction (Viper) and conventional paths. The deployment platform provides the storage mechanism.
3. **Secure Defaults**: GTB defaults to secure settings (e.g., gRPC reflection disabled) and requires explicit opt-in for development conveniences.

## Server-Side Authentication

When a tool exposes an HTTP or gRPC management/API surface, route caller
authentication through [`go/authn`](../explanation/components/authn.md) rather than
hand-rolling it. The package centralises the easy-to-get-wrong primitives:
constant-time API-key comparison, JWT/OIDC verification with an algorithm-confusion
defence (`alg:none` and HMAC-with-JWKS rejected), a bounded single-purpose JWKS
cache, mTLS client-certificate identity, and non-leaky failure surfacing (a
generic `401`/`403` or `Unauthenticated`/`PermissionDenied`, with the cause logged
[redacted](../explanation/components/redact.md)). It is opt-in and ships no policy engine or
token-issuance flow — authorization is a single tool-supplied predicate. See the
[Authentication & Authorization](../explanation/components/authn.md) component reference and
its security model.

## Opening External URLs

All URL-opening (browser or mail-client invocation) routes through [`go/browser`](../explanation/components/browser.md). The package enforces a scheme allowlist (`https`, `http`, `mailto`), an 8 KiB length bound, and control-character rejection before the URL reaches the platform handler. Direct use of `github.com/cli/browser.OpenURL` or `exec.Command` with `open`/`xdg-open`/`rundll32` is forbidden by convention — new call sites must use `pkg/browser.OpenURL`.

Callers that construct `mailto:` URLs from user-influenced data must additionally `url.QueryEscape` every parameter value to prevent header injection. See the `EmailDeletionRequestor` implementation in `pkg/telemetry/deletion.go` for the canonical pattern, and its test suite for the caller-contract assertion.
