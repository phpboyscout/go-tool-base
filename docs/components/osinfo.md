---
title: "osinfo — Shared OS-version string"
description: "pkg/osinfo reports a human-readable operating-system version string. It is the single shared implementation behind the telemetry OS field and the doctor report support bundle, so neither imports the other's internals."
date: 2026-06-22
tags: [component, diagnostics, telemetry]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# osinfo — Shared OS-version string

`pkg/osinfo` exposes one function:

```go
// Version returns a human-readable OS version string. On Linux it reports the
// kernel version from /proc/version; on every other platform — and whenever
// /proc/version cannot be read — it falls back to runtime.GOOS.
func osinfo.Version() string
```

Examples: `6.8.0-124-generic` (Linux), `darwin`, `windows`.

## Why a dedicated package

The implementation was promoted out of `pkg/telemetry` (an unexported helper) so it could be shared without forcing a consumer to import telemetry internals. Both the telemetry collector (the event `OSVersion` field) and the [`doctor report`](commands/doctor.md#doctor-report--support-bundle) support bundle depend on it, guaranteeing one consistent OS string across both surfaces.

## Implementation notes

The core is structured for testability: the OS name and the `/proc/version` reader are injected into a pure helper, so every branch (non-Linux fallback, read error, kernel-field extraction) is unit-tested without a platform dependency. See `pkg/osinfo/osinfo.go`.
