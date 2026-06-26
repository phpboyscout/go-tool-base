---
title: What's Collected
description: Event types, the data collected, and machine identification.
date: 2026-03-31
tags: [components, telemetry, analytics, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# What's Collected

## Event Types

```go
props.EventCommandInvocation  // "command.invocation" — a command was run
props.EventCommandError       // "command.error" — a command failed
props.EventFeatureUsed        // "feature.used" — a feature was exercised
props.EventUpdateCheck        // "update.check" — update check performed
props.EventUpdateApplied      // "update.applied" — update was applied
props.EventDeletionRequest    // "data.deletion_request" — GDPR deletion request
```

These constants are defined in both `pkg/props` and `pkg/telemetry`. Since they resolve to plain strings, values from either package are interchangeable.

---

## What Is Collected

Every telemetry event contains:

| Field | Example | Description |
|-------|---------|-------------|
| `event.type` | `command.invocation` | Event category |
| `event.name` | `generate` | Specific command or feature |
| `tool.name` | `mytool` | Tool identity |
| `tool.version` | `1.2.3` | Tool version |
| `os.type` | `linux` | Operating system |
| `os.version` | `6.8.0-106-generic` | OS/kernel version |
| `host.arch` | `amd64` | CPU architecture |
| `go.version` | `go1.26.1` | Go runtime version |
| `machine.id` | `4a3f8c1d9e2b6f70` | Pseudonymous machine identifier (16 hex chars); stable per machine and correlatable across tools |
| `command.duration_ms` | `142` | Execution time (command events only) |
| `command.exit_code` | `0` | Exit status (command events only) |

Tool authors can add custom metadata via `TelemetryConfig.Metadata` (included in every event) or the `extra` parameter on `Track`/`TrackCommand` (per-event).

### Extended Collection (Enterprise)

For closed enterprise environments where users are contractually bound by security policies, tool authors can enable **extended collection** to include additional diagnostic data:

| Field | Example | When |
|-------|---------|------|
| `command.args` | `--name myapp --verbose` | `ExtendedCollection: true` |
| `command.error` | `missing template file` | `ExtendedCollection: true` |

Extended collection is **disabled by default** and must be explicitly opted into by the tool author:

```go
Telemetry: props.TelemetryConfig{
    ExtendedCollection: true, // enterprise only
    Endpoint: "https://internal-analytics.corp.example.com/events",
},
```

When disabled, `TrackCommandExtended` silently drops args and error messages — callers do not need to check the flag. Duration and exit code are always recorded regardless of this setting.

!!! warning "Privacy consideration"
    Only enable `ExtendedCollection` in tools deployed within controlled enterprise environments where data handling is governed by employment contracts and security policies. Never enable it for public-facing or open-source tools.

### Credential Redaction

Even with `ExtendedCollection` enabled, `command.args` and `command.error` values are never shipped verbatim. Every string is routed through [`pkg/redact`](../redact.md) before being attached to the outgoing event. The redactor strips URL userinfo, common credential query parameters (`apikey`, `token`, `access_token`, `password`, …), `Authorization` headers quoted in free text, well-known provider prefixes (`sk-`, `ghp_`, `AIza`, `AKIA`, Slack `xoxb-`, etc.), and very long opaque tokens.

```go
// A command invoked as:
//   tool deploy --api-token=sk-proj-abc123def456...

// Ships as:
event.Args  = []string{"--api-token=sk-proj-***", "deploy"}
event.Error = `failed POST https://<redacted>@api.example.co/v1?apikey=***: 401`
```

The redactor is idempotent and never retains the original string. It catches common shapes — not every possible credential format. Tool authors accepting unusual credential formats in their own commands should either match the common shape conventions (prefix + opaque hex/base64) or contribute a pattern upstream via a PR to `pkg/redact`.

When a custom telemetry backend is used, events arrive pre-redacted — the backend does not need to repeat the work.

### OTel Exporter Header Advisories

If `WithOTelHeaders` is called with a header name that matches the sensitive-header pattern (`Authorization`, `X-API-Key`, custom names containing `auth`/`token`/`secret`/`bearer`/`password`/`credential`), the OTel backend emits a WARN at initialisation time:

```
WARN  OTel header Authorization appears to carry credentials; ensure the
      exporter uses TLS and that any HTTP middleware logging headers
      redacts this name. See docs/components/telemetry.md.
```

The warning is advisory — the header is still honoured. It exists so operators can audit which headers carry credentials and confirm their exporter uses TLS. Header **values** never appear in the warning text.

### What Is NOT Collected

By default, the following are never collected:

- Command arguments or flags (unless `ExtendedCollection` is enabled)
- Error messages (unless `ExtendedCollection` is enabled)
- File paths or file contents
- Environment variables
- IP addresses
- Usernames, hostnames, or any raw PII
- Authentication tokens or credentials

## Machine ID

The machine ID is a privacy-preserving identifier derived from four system signals:

1. **OS machine ID** — `/etc/machine-id` (Linux), `IOPlatformUUID` (macOS), `MachineGuid` (Windows)
2. **MAC address** — first non-loopback network interface
3. **Hostname**
4. **Username**

All four are concatenated and hashed with SHA-256. The first 8 bytes (16 hex chars) are used. Each signal degrades gracefully if unavailable.

The machine ID is **pseudonymous, not anonymous**. The SHA-256 hash is one-way (you cannot directly read the source signals back out of it), but because the inputs are stable per machine, the resulting identifier is itself stable and therefore *correlatable*: the same machine produces the same ID, and — because no per-tool salt is applied — every GTB-based tool on that machine produces the *same* ID. An observer holding event streams from multiple tools can link them to a single machine, and anyone who can enumerate the (small) input space for a known machine could confirm a match by recomputing the hash. Treat it as a stable per-machine pseudonym, not as anonymised data.

The machine ID is computed fresh on every invocation — it is not persisted to config.

```bash
$ mytool telemetry status
Telemetry: enabled
Machine ID: 4a3f8c1d9e2b6f70
```
