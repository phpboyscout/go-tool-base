---
title: "Secrets & Configuration Model"
description: "Architecture and deployment guide for managing secrets and environment-specific configuration in GTB."
date: 2026-03-24
tags: [development, security, configuration, viper, secrets]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Secrets & Configuration Model

GTB uses a layered configuration model powered by **Viper**. This approach allows tools to remain environment-agnostic while supporting secure secrets management in both development and production.

## Configuration Priority

Viper resolves configuration keys using the following priority order (highest to lowest):

1. **Explicit CLI Flags**: e.g., `--server.port 9090`
2. **Environment Variables**: e.g., `SERVER_PORT=9090`
3. **Config Files**: e.g., `config.yaml`
4. **Internal Defaults**: hardcoded fallback values

## Environment Variable Mapping

GTB automatically binds environment variables to configuration keys. Dot-separated paths are converted to upper-case, underscore-separated environment variables:

| Config Key | Environment Variable |
| :--- | :--- |
| `server.http.port` | `SERVER_HTTP_PORT` |
| `github.token` | `GITHUB_TOKEN` |
| `log.level` | `LOG_LEVEL` |

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

## Opening External URLs

All URL-opening (browser or mail-client invocation) routes through [`pkg/browser`](../components/browser.md). The package enforces a scheme allowlist (`https`, `http`, `mailto`), an 8 KiB length bound, and control-character rejection before the URL reaches the platform handler. Direct use of `github.com/cli/browser.OpenURL` or `exec.Command` with `open`/`xdg-open`/`rundll32` is forbidden by convention — new call sites must use `pkg/browser.OpenURL`.

Callers that construct `mailto:` URLs from user-influenced data must additionally `url.QueryEscape` every parameter value to prevent header injection. See the `EmailDeletionRequestor` implementation in `pkg/telemetry/deletion.go` for the canonical pattern, and its test suite for the caller-contract assertion.
