---
title: Security & Secret Management
description: Security standards for the GTB library and generator.
tags: [security, secrets, library, standards]
---

# Security & Secret Management

As a framework that handles AI keys and VCS authentication, security is baked into the design of GTB.

## Secret Handling in Code

- **No Hardcoding**: Secrets must never be hardcoded into the library or generator templates.
- **Viper Integration**: Use the `pkg/config` container to handle secrets securely from environment variables or encrypted configuration backends.
- **Redaction**: Ensure that diagnostic logs redact sensitive keys by default.

## AI Provider Security

- **Credential Isolation**: Each AI provider implementation in `pkg/chat` must isolate credentials to prevent accidental cross-provider leakage.
- **Key Rotation**: Provide clear error messages that guide users to rotate their keys if an "Unauthorized" error is returned from a provider.

## Secure Templates

The generator templates in `internal/generator/templates` must generate code that follows secure defaults (e.g., standard permission masks for files, sanitized input handling).

## Reporting Vulnerabilities

If you discover a security vulnerability in GTB, please report it via the internal security channel as defined in the project's root README.
