---
title: Doctor Command
description: Diagnostic command that validates configuration, checks environment health, and reports runtime details.
date: 2026-03-26
tags: [components, commands, doctor, diagnostics]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Doctor Command

The `doctor` command runs diagnostic checks to validate configuration, connectivity, and the runtime environment. It is enabled by default via the `DoctorCmd` feature flag.

`doctor` is the *health verdict* (pass/warn/fail/skip). Its [`report` subcommand](#doctor-report--support-bundle) adds the *state dump* — resolved config (redacted), paths, versions, and feature flags — around that verdict.

## Usage

```bash
mytool doctor
mytool doctor --output json
```

## Description

Runs a series of built-in and feature-registered health checks, then reports the results. Each check returns one of four statuses:

| Status | Meaning |
|--------|---------|
| `pass` | Check passed successfully |
| `warn` | Non-critical issue detected |
| `fail` | Critical issue that needs attention |
| `skip` | Check could not run (e.g., missing config) |

## Built-in Checks

| Check | What it validates |
|-------|-------------------|
| **Go version** | Runtime Go version is 1.22+ |
| **Configuration** | Config is loaded and accessible |
| **Git** | `git` binary is available and the current directory is a repository |
| **API keys** | At least one AI provider API key is configured |
| **Permissions** | Config directory exists with correct owner permissions (rwx) |

## Output Example

```
mytool v1.2.3

  [OK] Go version: go1.26.0
  [OK] Configuration: loaded successfully
  [OK] Git: repository accessible
  [!!] API keys: no AI provider API keys configured
  [OK] Permissions: config dir: /home/user/.config/mytool (drwxr-xr-x)
```

JSON output (`--output json`) returns a `DoctorReport` struct with the tool name, version, and an array of check results.

## Extensibility

Features can register additional checks via the middleware system. When a feature is enabled, its registered check providers are automatically discovered and included in the report:

```go
func init() {
    setup.RegisterChecks(props.MyFeature, func(p *props.Props) []doctor.CheckFunc {
        return []doctor.CheckFunc{myCustomCheck}
    })
}
```

## `doctor report` — support bundle

`doctor report` emits a single, **secret-redacted, paste-ready support bundle** a user can drop straight into a GitLab/GitHub issue. It wraps the health verdict above with a state dump, so a maintainer gets *what version*, *what config*, *what paths*, *what flags*, and *what doctor says* in one block.

```bash
mytool doctor report                       # human-readable
mytool doctor report --output json > bug.json
```

It is available wherever `doctor` is (gated by `DoctorCmd`, default-on) — there is no separate feature flag.

### What it collects

| Section | Contents |
|---------|----------|
| **Tool** | Name, summary, version, commit, build date |
| **Runtime** | Go version, host OS string (`pkg/osinfo`), arch |
| **Paths** | Resolved config directory and config file in use (no cache dir — GTB has none) |
| **Features** | Every built-in feature flag, enabled/disabled |
| **Config** | The *effective* merged configuration, **redacted** |
| **Doctor** | The full report above, reused verbatim |

### Safe by default — redaction

The entire bundle passes through [`pkg/redact`](../../explanation/components/redact.md) before it is written, in **both** text and JSON. There is **no flag to disable redaction** — for a raw value, read the specific config key directly. Two layers protect the config:

1. **Credential-shaped keys are dropped to `<redacted>`** regardless of value — keys ending in `.api.key`, `.auth.value`, `.app_password`, `.password`, `.secret`, `.token`, or whose final segment is a known credential word. Even a malformed value cannot leak.
2. **Every other value is scrubbed through `redact.String`** (best-effort): URL userinfo, well-known token prefixes (`sk-`, `ghp_`, `glpat-`, `AKIA…`), and long opaque tokens. Map **keys are preserved** so the structure stays legible.

The process environment is **never** enumerated (high leak surface, low triage value); env-derived values still appear via the resolved config under Viper precedence. See [`pkg/redact`](../../explanation/components/redact.md) for the full pattern set.

## Implementation

The doctor command is implemented in `pkg/cmd/doctor/doctor.go` with built-in checks in `pkg/cmd/doctor/checks.go`; the `report` subcommand (collector + redaction) lives in `pkg/cmd/doctor/report.go` and `report_redact.go`. The check registry lives in `pkg/setup/`; the shared OS-version string comes from `pkg/osinfo`.

See the spec: `docs/development/specs/2026-06-21-bug-report-diagnostics-command.md`.
