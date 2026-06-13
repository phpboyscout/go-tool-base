---
title: "Config hot-reload rework — a container-owned watcher that actually reloads merged config"
description: "Replace the viper-driven single-file WatchConfig with a container-owned fsnotify watcher that re-reads and re-merges all configured files on change, validates a candidate before swapping, notifies observers without deadlocking, and is race-safe. Closes five compounding hot-reload findings from the 2026-06-12 audit."
status: IMPLEMENTED
date: 2026-06-12
tags:
  - specification
  - config
  - hot-reload
  - concurrency
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-fable-5)
    role: AI drafting assistant
---

# Config hot-reload rework

Authors
:   Matt Cockayne, Claude (claude-fable-5) *(AI drafting assistant)*

Date
:   2026-06-12

Status
:   IMPLEMENTED (open questions resolved in review 2026-06-12; see `docs/development/implementation-notes/2026-06-12-config-hot-reload-rework.md`)

## Summary

`pkg/config`'s hot-reload feature is documented but broken in five compounding ways. The root cause is that it leans on viper's single-file `WatchConfig`, which cannot model GTB's *merged multi-file* configuration. This spec replaces it with a **container-owned fsnotify watcher** that owns the full reload lifecycle: re-read file[0], re-merge files[1:], validate a candidate, swap atomically, then notify observers — all race-safe and without the deadlock the current observer-error channel causes.

Findings addressed (from `docs/development/reports/codebase-audit-2026-06-12.md` §3.2):

- `observer-errs-channel-deadlock` — **High** (unbuffered `errs` channel nobody reads; first send hangs the watcher and all future reloads)
- `multi-file-hot-reload-destroys-merge` — **High** (viper watches only the last file and `ReadInConfig` replaces the whole map, discarding earlier merged layers)
- `loadfilescontainer-nil-nil-contract` — **High** (already fixed in MR !63 via `ErrConfigFileNotFound`; called out here because the watcher must respect the same contract)
- `single-file-watch-never-enabled` — **Medium** (`watchConfig()` sits inside the `len(files) > 1` branch, so single-file containers never watch despite the docs)
- `reload-validation-no-rollback` — **Medium** (viper replaces values *before* `OnConfigChange`, so "reject invalid reload" cannot hold)
- `observers-slice-data-race` — **Medium** (`observers`/`schema` mutated without a lock while the watcher goroutine ranges over them)

These compound: a single container-owned-watcher rewrite addresses all of them, which is why they are one spec rather than separate quick fixes. The audit names this the highest-leverage work in the report.

## Motivation

The documented promise is: *"automatically enables file watching; later files override earlier ones; invalid reloads are rejected; observers are notified of changes."* None of that currently holds for a merged container:

- The merge is built by repeated `SetConfigFile`+`MergeInConfig`, leaving viper's `configFile` pointing at the **last** file. `WatchConfig` therefore watches only that file, and on change calls `ReadInConfig()` which **replaces** the entire map with that one file's contents — silently dropping every earlier layer.
- Validation runs in `OnConfigChange`, but viper has *already* swapped the map by then, so a "rejected" reload still serves invalid values via `Get*`.
- The observer error channel is unbuffered and unread; the documented `AddObserverFunc` pattern sends on it, so the first observer error blocks `wg.Done`, hangs `wg.Wait` inside viper's synchronous callback, and **permanently stalls all subsequent reloads**.
- Single-file containers never watch at all (the call is in the multi-file branch); the dedicated test passes vacuously.
- The observer slice is mutated without synchronisation against the watcher goroutine.

## Design decisions

### D1 — Container owns the watcher, not viper

The `Container` (for file-backed containers) runs its own `fsnotify.Watcher` over **all** configured files. On any change event (debounced), it performs the full reload itself rather than delegating to viper's single-file path. This is the only way to preserve the merge.

### D2 — Reload = build a candidate, validate, then swap

On a change:

1. Build a **candidate** viper: read file[0] (honouring the `ErrConfigFileNotFound` contract), then `MergeInConfig` files[1:] in order.
2. If a schema is set, validate the candidate. On failure: keep the last-known-good config, notify observers of the *error* (see D3), and do **not** swap.
3. On success: swap the live viper for the candidate under the container lock, then notify observers of the change.

This makes "invalid reloads are rejected" actually true — values never change unless the candidate validates.

### D3 — Observer notification that cannot deadlock

Replace the unbuffered, unread `errs` channel by changing the `Observable` contract so `AddObserverFunc` (and the `Observable` interface) **returns an error** instead of sending on a channel. A return-error contract is simpler, removes the channel and the deadlock class entirely, and is the idiomatic Go shape.

This is a breaking change to the `Observable` / `AddObserverFunc` signature. The project is pre-1.0 (currently 0.16.x), so a breaking change is a **minor** version bump, not a major — there is no need for a compatibility shim. We take the clean break. (Resolved; see O1.)

### D4 — Race-safety

Guard `observers` and `schema` with the container mutex; copy the observer slice under lock at the top of the reload callback and iterate the copy. Start the watcher only after construction completes.

### D5 — Single-file containers watch too

Move `watchConfig()` out of the `len(files) > 1` branch into `loadFilesContainer`/`NewFilesContainer` so single-file containers watch as documented. Fix the vacuous test (`observed > 0` required).

### D6 — Debounce, configurable; re-watch after atomic renames

Coalesce the burst of fsnotify events a single save produces (write + rename + chmod) behind a **configurable debounce window, defaulting to 250 ms** (resolved O2 — the higher default tolerates slow/networked filesystems; expose `WithReloadDebounce(d)` for tuning). After each event, **re-establish the watch on the path** (resolved O3): editors that save via atomic rename replace the inode, which invalidates an inode-based watch, so the watcher must re-`Add` the path each cycle or the second save is missed.

### D7 — Partial-merge failure is fail-closed

If any file in the merge fails to parse during a reload (e.g. file[0] is valid but file[2] is malformed), the **entire reload is rejected** (resolved O4): keep the last-known-good config, serve the previous values from `Get*`, and notify observers of the error. The watcher never serves a half-merged state. This composes with D2 (validate the candidate before swapping).

## Migration and compatibility

D3 changes the `Observable` interface / `AddObserverFunc` signature (channel → returned error). The project is pre-1.0 (0.16.x), so per the current versioning policy a breaking change is a **minor** bump — no compatibility shim is carried. Downstream observers update their signature from `func(Containable, chan error)` to `func(Containable) error`; a migration note records the one-line change.

## Open questions

*All resolved during review (2026-06-12).*

1. **O1 — Observer error contract.** *Resolved:* clean return-error contract (D3). Pre-1.0, the breaking change is a minor bump, so no shim is needed.
2. **O2 — Debounce window.** *Resolved:* configurable via `WithReloadDebounce`, **default 250 ms** to tolerate slow filesystems. (D6)
3. **O3 — Atomic-replace editors.** *Resolved:* re-establish the watch on the path after every event, so atomic-rename saves are not missed. (D6)
4. **O4 — Partial-merge failure.** *Resolved:* **fail-closed** — reject the whole reload and keep last-known-good. (D7)

## Verification plan

1. **Race** — `go test -race` over a multi-file hot-reload test (2+ merged files), with concurrent `AddObserver` registration during reloads.
2. **Merge preservation** — change file[1] and assert file[0]'s values *survive* the reload (the current bug discards them).
3. **Validation rollback** — write an invalid reload and assert `Get*` still serves the last-known-good values and observers receive the error, not the bad config.
4. **No deadlock** — an observer that returns/sends an error must not stall subsequent reloads (the current hang).
5. **Single-file watch** — assert a single-file container observes a change (`observed > 0`).
6. **Docs** — update `docs/components/config.md` and the hot-reload how-to.

## Out of scope

- Hot-reload for reader/embedded containers (no backing file to watch).
- Schema/validation engine changes (this consumes the existing `Validate`).

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.2
- [Plan 1 — security & bugs](../plans/2026-06-12-audit-plan-1-security-and-bugs.md) Phase 9
- `docs/components/config.md`, `docs/how-to/config-hot-reload.md`
- [config schema validation spec](2026-05-29-config-generic-validation.md)
