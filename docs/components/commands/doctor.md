---
title: Doctor Command
description: Diagnostic command that validates configuration, checks environment health, and reports runtime details.
date: 2026-03-26
tags: [components, commands, doctor, diagnostics]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Doctor Command

The `doctor` command runs diagnostic checks to validate configuration, connectivity, and the runtime environment. It is enabled by default via the `DoctorCmd` feature flag.

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

## Implementation

The doctor command is implemented in `pkg/cmd/doctor/doctor.go` with built-in checks in `pkg/cmd/doctor/checks.go`. The check registry lives in `pkg/setup/`.
