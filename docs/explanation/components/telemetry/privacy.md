---
title: Privacy & Consent
description: Two-level gating, GDPR deletion, consent withdrawal, and known limitations.
date: 2026-03-31
tags: [components, telemetry, analytics, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Privacy & Consent

## Two-Level Gating

Telemetry requires two conditions to be active:

| Level | Who controls it | How |
|-------|----------------|-----|
| Feature flag | Tool author | `props.Enable(props.TelemetryCmd)` in code |
| User consent | End user | `telemetry enable` command or `TELEMETRY_ENABLED=true` env var |

Both must be active for data to be collected. If either is missing, the collector is a silent noop.

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
