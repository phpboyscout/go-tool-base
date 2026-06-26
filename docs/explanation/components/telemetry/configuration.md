---
title: Configuration
description: TelemetryConfig, environment variables, and initialiser integration.
date: 2026-03-31
tags: [components, telemetry, analytics, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration

## TelemetryConfig


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/telemetry](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/telemetry) for the full API definition.


Endpoints are set by the tool author at build time and are **not user-configurable**. The user config file only stores consent (`telemetry.enabled`) and mode (`telemetry.local_only`).

---

## Environment Variables

| Variable | Values | Effect |
|----------|--------|--------|
| `TELEMETRY_ENABLED` | `true` / `false` | Bypasses interactive consent; overrides config at runtime |
| `TELEMETRY_LOCAL` | `true` / `false` | Forces local-only mode (file backend) |
| `CI` | `true` | Sets `--skip-telemetry` default to `true` during `init` |

These names are deliberately un-prefixed so tools building on GTB can use them without GTB-specific naming conventions.

---

## Init Integration

When `TelemetryCmd` is enabled and the tool has `InitCmd` enabled, the `TelemetryInitialiser` registers with the setup system. During `init`, the user is prompted to opt in:

```
? Anonymous usage telemetry
  Help improve mytool by sending anonymous usage statistics.
  No personally identifiable information is collected.
  You can change this at any time with `mytool telemetry enable/disable`.
  > Yes / No
```

The `--skip-telemetry` flag (default `true` when `CI=true`) suppresses the prompt in non-interactive environments. The `TELEMETRY_ENABLED` env var pre-answers the consent question.

### Tools Without Init

For tools that disable `InitCmd` (like the GTB binary itself), the `telemetry enable` command auto-creates the config file in the default config directory (`~/.toolname/config.yaml`) if one doesn't exist.
