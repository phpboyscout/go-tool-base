---
title: "Telemetry Architecture & Concepts"
description: "Architectural concepts, privacy controls, data handling, and design limitations behind GTB's telemetry framework."
date: 2026-06-24
tags: [concepts, telemetry, privacy, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Telemetry Architecture & Concepts

This document explains the conceptual design, privacy guarantees, data lifecycle, and limitations of GTB's telemetry framework. For API references, configuration, and backend implementations, see the [Telemetry API](../components/telemetry.md).

## Two-Level Gating

Telemetry requires two conditions to be active:

| Level | Who controls it | How |
|-------|----------------|-----|
| Feature flag | Tool author | `props.Enable(props.TelemetryCmd)` in code |
| User consent | End user | `telemetry enable` command or `TELEMETRY_ENABLED=true` env var |

Both must be active for data to be collected. If either is missing, the collector is a silent noop.

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

Even with `ExtendedCollection` enabled, `command.args` and `command.error` values are never shipped verbatim. Every string is routed through [`pkg/redact`](../components/redact.md) before being attached to the outgoing event. The redactor strips URL userinfo, common credential query parameters (`apikey`, `token`, `access_token`, `password`, …), `Authorization` headers quoted in free text, well-known provider prefixes (`sk-`, `ghp_`, `AIza`, `AKIA`, Slack `xoxb-`, etc.), and very long opaque tokens.

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

## Buffer and Spill

Events are buffered in memory (capped at 1000) and flushed on process exit via Cobra's `OnFinalize` callback.

When the buffer is full, events are spilled to disk:

- **Location**: config directory (if available and writable), otherwise `/tmp`
- **File size cap**: 1 MB per spill file
- **File count cap**: 10 files — oldest deleted when exceeded
- **Recovery**: every `Flush` checks for spill files first, sends them before the current buffer

The shared `telemetry.ResolveDataDir(p)` helper determines the data directory for both spill files and local-only logs.

## Delivery Modes

| Mode | Behaviour | Trade-off |
|------|-----------|-----------|
| `DeliveryAtLeastOnce` (default) | Spill files deleted **after** successful send; a failed in-memory `Flush` batch is re-spilled for retry | Possible duplicates if ack is lost; no data loss |
| `DeliveryAtMostOnce` | Spill files deleted **before** send | Possible data loss; no duplicates |

The at-least-once guarantee relies on backends **surfacing** delivery failures from `Send`. When a backend returns an error, `flushSpillFiles` retains the spill file, and a failed in-memory `Flush` batch is re-spilled to disk so the next flush retries it — without this, a backend that swallowed transport errors would silently drop batches while reporting success, defeating the guarantee.

```go
Telemetry: props.TelemetryConfig{
    DeliveryMode: props.DeliveryAtMostOnce,
},
```

## GDPR Data Deletion

The `telemetry reset` command:

1. Drops all buffered events and deletes spill files
2. Sends a deletion request via the configured `DeletionRequestor`
3. Clears the local-only telemetry log (if present)
4. Disables telemetry

### Built-in Deletion Requestors

| Requestor | How it works |
|-----------|-------------|
| `NewHTTPDeletionRequestor(url, logger)` | POSTs `{"machine_id": "..."}` to the endpoint |
| `NewEmailDeletionRequestor(address, toolName)` | Opens a pre-filled `mailto:` link |
| `NewEventDeletionRequestor(backend)` | Sends a `data.deletion_request` event through the existing backend |

If no requestor is configured, the event-based requestor is used as the universal fallback.

## Consent Withdrawal

When the user runs `telemetry disable`:

1. Config is updated to `telemetry.enabled: false`
2. All buffered events are **immediately dropped**
3. All spill files are **deleted**
4. The `OnFinalize` flush re-checks the enabled state and no-ops

No events are sent after an explicit disable, even if they were collected while consent was active.

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

## Known Limitations

### Machine ID Variability

The machine ID is computed fresh on every invocation from four system signals: OS machine ID, MAC address, hostname, and username. If any signal changes (network adapter swap, hostname rename, container restart, user switch), the hash changes. This means:

- GDPR deletion requests sent via `telemetry reset` may not match all historical events if the machine ID has changed since those events were recorded.
- De-duplication on the backend side should use a time window in addition to machine ID.

Persisting the ID to config was considered but rejected — a stored identity that follows the user across machines is a greater privacy risk than occasional ID drift.

### Thread Safety of Spill Files

The spill file mechanism trades strict thread safety for simplicity. `flushSpillFiles()` reads spill files from disk without holding the buffer mutex, while concurrent `Track()` calls may write new spill files (with the mutex held). In practice:

- Filesystem operations are atomic at the OS level.
- The worst case is missing a freshly-written spill file (caught on the next flush) or attempting to read a file that was concurrently deleted (handled gracefully with a `continue`).
- `Drop()` deleting spill files during concurrent `Track()` is safe — `os.Remove` on a non-existent file succeeds silently, and `OnFinalize` re-checks the enabled state before flushing.

### Backend Error Semantics

`Backend.Send()` error behaviour varies by implementation:

| Backend | Network errors | Other errors |
|---------|---------------|--------------|
| Noop | N/A | Always returns `nil` |
| Stdout | N/A | Returns encoder errors |
| File | N/A | Returns file I/O errors |
| HTTP | Returned from `Send` (wrapped) | Non-2xx returned + logged at debug |
| OTLP | Surfaced via OTel error handler | Returns `nil` from `Send` |

The HTTP backend now **returns** transport and non-2xx failures from `Send` so the at-least-once spill/retry layer can honour the delivery guarantee. This does not block the CLI: `Flush()` and the spill replay log the error (at warn/debug) and continue — under `DeliveryAtLeastOnce` the failed batch is retained (spill file kept, in-memory batch re-spilled) for the next attempt. The OTLP backend still routes its transport failures through the OTel SDK error handler (its batch processor owns retry/queueing internally). Tool authors debugging delivery should enable debug logging.

### Backend Fallback on Misconfiguration

If a tool author misconfigures `OTelEndpoint` (e.g. missing scheme, unreachable host), the backend creation fails at startup. The collector falls back to a noop backend with a warning log. Events are silently discarded until the endpoint is corrected. Enable debug logging during development to surface these warnings.

### Buffer Size

The in-memory buffer is capped at 1000 events. This is not currently configurable. For most CLI tools this is more than sufficient (a typical invocation produces 1-3 events). Long-running services with high event rates may see frequent disk spills, which is handled gracefully but adds I/O overhead.

### Local-Only Mode

When `telemetry.local_only` is true in config (or `TELEMETRY_LOCAL=true`), the file backend is selected and no data is transmitted remotely. This is mutually exclusive with HTTP/OTLP backends — setting both does not produce dual-write. If you need both local logging and remote transmission, use a custom backend that tees to both.

### Metadata Merge Precedence

When both `TelemetryConfig.Metadata` (tool-level) and the `extra` parameter (per-event) contain the same key, the per-event value wins. This allows commands to override tool-level defaults for specific events.

### Insecure Transport

If `OTelEndpoint` uses the `http://` scheme (no TLS), event data is transmitted unencrypted. The code correctly enables insecure mode for this case but does not warn. Use `https://` for all production endpoints. The `WithOTelInsecure()` option is an explicit opt-in for local development collectors.
