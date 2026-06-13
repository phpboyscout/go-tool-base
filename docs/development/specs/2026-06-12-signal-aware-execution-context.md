---
title: "Signal-aware execution context — flow Ctrl-C through cmd.Context() and the cleanup path"
description: "Give the root command a signal.NotifyContext so SIGINT/SIGTERM cancel cmd.Context(), run deferred telemetry flush and update-temp cleanup, and let long-running commands observe cancellation — instead of Go's default handler killing the process abruptly."
status: APPROVED
date: 2026-06-12
tags:
  - specification
  - cmd
  - lifecycle
  - signals
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-fable-5)
    role: AI drafting assistant
---

# Signal-aware execution context

Authors
:   Matt Cockayne, Claude (claude-fable-5) *(AI drafting assistant)*

Date
:   2026-06-12

Status
:   APPROVED (open questions resolved in review 2026-06-12)

## Summary

`rootCmd.Execute()` runs without a `signal.NotifyContext`. A Ctrl-C (SIGINT) or SIGTERM is handled by Go's default disposition, which terminates the process abruptly — `cmd.Context()` is never cancelled, deferred telemetry flush never runs, and an in-flight self-update can leave its temp file behind. This spec wires a signal-aware context into the root command's execution so interruption is graceful and observable by commands.

Finding addressed (from `docs/development/reports/codebase-audit-2026-06-12.md` §3.6):

- `no-signal-aware-execution-context` — **Medium, missing-feature**

## Motivation

GTB markets itself as a full-lifecycle CLI framework with telemetry, self-update, and long-running service commands. All three want a clean interrupt:

- **Telemetry** — the OTLP batch is flushed in a deferred `flushTelemetry`; an abrupt kill drops it.
- **Self-update** — the install writes a temp file then renames; an interrupt mid-install should clean it up.
- **Service / chat commands** — long-running work should observe `ctx.Done()` and unwind, not be `SIGKILL`-style terminated.

This is also a correctness multiplier for the controls work: a service controller that honours context cancellation is only useful if the *root command's* context is actually cancelled on signal.

## Design decisions

### D1 — `signal.NotifyContext` + `ExecuteContext`

In the `Execute` wrapper, derive `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` and call `rootCmd.ExecuteContext(ctx)` so every command's `cmd.Context()` is the cancellable one. `defer stop()` restores default disposition on return.

### D2 — Second signal forces exit; signal exit code

The first signal cancels the context (graceful). A *second* signal **force-exits immediately** (mirroring `kubectl`, `docker`), so a hung cleanup cannot trap the user. This pairs with the controls force-stop work. A signal-terminated run returns exit code **`128 + signum`** (130 for SIGINT, 143 for SIGTERM) — threaded through the `ErrorHandler`'s exit path so it doesn't conflict with normal error exits.

### D3 — Skeleton template update

The generator's `main` template (`cmd/<tool>/main.go`) and the `pkg/cmd/root` `Execute` helper both need updating so scaffolded tools get the behaviour by default. Generator output change ⇒ regeneration verification.

### D4 — Deferred cleanup ordering

`Execute` already defers the telemetry flush; ensure the flush and any update-temp cleanup run on the cancellation path (i.e. they are deferred *outside* the cancellable work, and the flush uses a bounded background context — as the existing `flushTelemetry` already does — so cancellation doesn't abort the flush itself).

### D5 — TUI owns Ctrl-C while active

While an interactive TUI prompt (`huh`/Bubble Tea) is running, the **TUI owns Ctrl-C** — it cancels the current prompt and returns control to the command. The outer `NotifyContext` only acts on Ctrl-C when no TUI is active. Bubble Tea already installs its own signal handling for the duration of a program; the requirement is to confirm the outer context and an active TUI coexist (the TUI's `tea.Quit`/`tea.Interrupt` on Ctrl-C must not also trip the outer context into cancelling the whole command). A stray Ctrl-C in a prompt should abort the prompt, not the run.

## Open questions

*All resolved during review (2026-06-12).*

1. **O1 — Windows signals.** *Resolved:* pass both — `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`. On Windows only `os.Interrupt` (Ctrl-C) fires; `syscall.SIGTERM` never delivers but is harmless. No build tags. (D1)
2. **O2 — Exit code on signal.** *Resolved:* `128 + signum` (130 for SIGINT, 143 for SIGTERM) — the Unix convention shells/CI/supervisors understand. Confirm it threads through the `ErrorHandler`'s exit path. (D2)
3. **O3 — TUI Ctrl-C.** *Resolved:* the **TUI owns Ctrl-C while active** (cancels the current prompt); the outer signal context owns Ctrl-C only when no TUI is running. (D5)
4. **O4 — Second-signal behaviour.** *Resolved:* **force-exit immediately** on the second signal (kubectl/docker UX) — a hung cleanup can never trap the user. (D2)

## Verification plan

1. **Unit** — `Execute` with a stub command that blocks on `<-cmd.Context().Done()` returns promptly when the derived context is cancelled.
2. **Cleanup** — a simulated SIGINT during a command runs the deferred telemetry flush (observable via a spy collector) and any temp cleanup.
3. **E2E** — a smoke scenario that sends SIGINT to a running command and asserts a graceful exit code and no orphaned temp files.
4. **Scaffold** — regenerate a tool and confirm its `main.go` uses `ExecuteContext` with `NotifyContext`.
5. **Docs** — update `docs/concepts/architecture.md` / `framework-cli.md`.

## Out of scope

- Per-command custom signal handling (commands read `cmd.Context()`; bespoke handlers are theirs to add).
- Service-level shutdown ordering (covered by the controls spec).

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.6
- [Plan 3 — improvements](../plans/2026-06-12-audit-plan-3-improvements.md) Phase 16
- [controls supervisor & lifecycle spec](2026-06-12-controls-supervisor-lifecycle.md)
