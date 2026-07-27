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
| `CI` | `true` | Sets `--skip-telemetry` default to `true` during `init`, and skips the root pre-run consent prompt (parity with `--ci` / `ci: true`) |

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

### Root pre-run consent prompt

Independently of `init`, the root pre-run shows the same one-time opt-in prompt
the first time a `TelemetryCmd`-enabled tool runs with `telemetry.enabled` still
unset. That prompt is **TTY-gated**: it is skipped — without ever reading
stdin — when the run is in CI (`--ci` / `ci: true` / `CI=true`), when stdin is
not a terminal (cron, piped input), or under the `mcp` command (whose stdout
carries JSON-RPC frames). A skipped prompt **persists nothing**: absence of
consent is not refusal, so `telemetry.enabled` stays unset and the opt-in
reappears on the next interactive run.

### Tools Without Init

For tools that disable `InitCmd` (like the GTB binary itself), the `telemetry enable` command auto-creates the config file in the default config directory (`~/.toolname/config.yaml`) if one doesn't exist.
