---
title: "Controls supervisor & lifecycle hardening — idempotent start, real restart semantics, no busy-spin"
description: "Fix the pkg/controls supervisor and goroutine-lifecycle cluster: an idempotent Start, a restart policy that distinguishes clean-exit/cancel/error and never sends nil errors, a busy-spin-free error/context handler, force-stop on shutdown timeout, correct signal disposition for WithoutSignals, validated lifecycle funcs, and readiness that fails closed before the first async check. Closes seven controls findings from the 2026-06-12 audit."
status: APPROVED
date: 2026-06-12
tags:
  - specification
  - controls
  - lifecycle
  - concurrency
  - service-orchestration
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-fable-5)
    role: AI drafting assistant
---

# Controls supervisor & lifecycle hardening

Authors
:   Matt Cockayne, Claude (claude-fable-5) *(AI drafting assistant)*

Date
:   2026-06-12

Status
:   APPROVED (open questions resolved in review 2026-06-12)

## Summary

`pkg/controls` orchestrates long-running services (startup ordering, health monitoring, graceful shutdown). The audit found a cluster of seven supervisor/lifecycle defects whose fixes interlock — they share the controller's state machine, the restart loop, and the message/signal goroutines — so they are specified together rather than patched piecemeal.

Findings addressed (from `docs/development/reports/codebase-audit-2026-06-12.md` §3.2):

- `error-handler-busy-spins-after-cancel` — **High** (the error/context handler never disables the `ctx.Done()` case after cancellation; spins a full core until process exit)
- `restart-policy-restarts-clean-exit-and-sends-nil-errors` — **High** (a clean `Start` return falls through into the restart path: `restarts++`, `errs <- nil`, restart after backoff; an http service whose `Start` returns immediately enters a restart loop at startup)
- `start-not-idempotent` — **Medium** (no CAS guard; a second `Start()` double-starts every service and adds a `wg` count `handleStopMessage` never decrements, hanging `Wait()`)
- `nil-start-stop-func-panics` — **Medium** (a service registered without `WithStart`/`WithStop` nil-panics at start/shutdown)
- `withoutsignals-leaves-notify-registered` — **Medium** (`NewController` calls `signal.Notify` before applying options; `WithoutSignals` orphans the registration, swallowing SIGINT/SIGTERM; `signal.Stop` is never called)
- `no-force-stop-despite-documented-timeout` — **Medium** (sequential, synchronous `StopFunc`s on the message goroutine; one context-ignoring stop hangs `Wait()` forever)
- `unrun-async-check-reports-healthy` — **Medium** (an async health check reports OK before its first run completes; `/readyz` fails open during the startup window)

Also folded in (low/info, same files, cheap alongside the above): `valid-error-func-never-used` (the documented `ValidErrorFunc` is wired to nothing), `controller-goroutines-never-terminate`, `unsynchronized-channel-logger-setters`.

## Motivation

These are correctness bugs in a component whose entire job is reliable lifecycle management. The two High items are the most acute: the busy-spin burns a full core on every shutdown (bites long-lived embedders and parallel test suites), and the restart policy actively misbehaves for the most common case — a `pkg/http` service whose `Start` returns immediately after spawning its serve goroutine is treated as "exited" and restarted in a loop. `errors.Wrap(nil, ...)` returning nil also clobbers the stored health-failure error.

## Design decisions

### D1 — Explicit run outcomes

Replace the "Start returned → restart" fall-through with an explicit classification of a service run: **clean exit**, **context cancelled**, or **error**. Only an *error* (and only when the restart policy permits, and `ctx.Err() == nil`) triggers a restart. A `Start` that returns nil because it serves in a background goroutine is a *clean start*, not an exit — these services are supervised via their health check, not by `Start`'s return.

### D2 — Never send nil on the error channel; reset the counter on health

The restart loop must never `errs <- nil`. `restarts` is reset after a service has been healthy for **30 s** (configurable via `WithRestartResetInterval`), so the count is "consecutive failures", not lifetime. Honour `WithValidError`/`ValidErrorFunc` (D7) to exempt expected terminal errors (e.g. `http.ErrServerClosed`, `context.Canceled`) from counting as failures.

### D3 — Idempotent Start

Guard `Start` with `compareAndSetState(Unknown, Running)`; a second call returns early without double-starting or double-counting the wait group. Snapshot the service count under `services.mu`.

### D4 — No busy-spin; goroutines terminate

In the error/context handler, set the `ctx.Done()` channel to nil after first receipt (so the `select` case is disabled) and define a real exit condition (return once Stopped and `errs` drained). The message-processor and signal goroutines must exit on Stop; add a second-signal force-exit.

### D5 — Validate lifecycle funcs

`Register` validates that a service has at least the funcs the supervisor will call, or defaults missing `Start`/`Stop` to no-ops. Every call site nil-checks before invoking. (The status/liveness/readiness probes are already nil-checked + recover-wrapped — match that.)

### D6 — Correct signal disposition + force-stop

Defer `signal.Notify` until *after* options are applied and only when `c.signals != nil`; call `signal.Stop` in `WithoutSignals`/`SetSignalsChannel` and at shutdown. On shutdown, stop services in **reverse registration order**, one at a time, each `StopFunc` run under a `select` against `ctx.Done()` so a context-ignoring stop is abandoned at its deadline rather than hanging `Wait()` forever. Reverse order means the last-started service stops first, respecting startup dependencies.

### D7 — Readiness fails closed; ValidErrorFunc wired

A nil cached async-check result is treated as **not ready** for readiness gating (or the first async check runs synchronously before the controller marks Running). Add the `WithValidError` option and consult `ValidErrorFunc` in the restart supervisor (or `// Deprecated:` it if we decide it is not needed — see O3).

## Open questions

*O1–O3 resolved during review (2026-06-12); O4 is an implementation enumeration task.*

1. **O1 — Restart-counter reset interval.** *Resolved:* **30 s healthy window**, configurable via `WithRestartResetInterval`. (D2)
2. **O2 — Stop order.** *Resolved:* **reverse registration order**, one service at a time, each under a per-stop deadline vs `ctx.Done()`. Respects startup dependencies. (D6)
3. **O3 — `ValidErrorFunc`.** *Resolved:* **wire it** via a new `WithValidError` option; the supervisor consults it to exempt expected terminal errors (`http.ErrServerClosed`, `context.Canceled`) from the restart count. (D2, D7)
4. **O4 — Exported-signature changes (implementation task).** Enumerate the signature changes (e.g. `Register` returning an error, the new `WithValidError`/`WithRestartResetInterval` options) for the changelog/migration note. Pre-1.0, a breaking change is a **minor** bump — document, don't avoid.

## Verification plan

1. **Busy-spin regression** — assert the error/context goroutine exits after Stop (goroutine count before/after; CPU/iteration counter stays bounded for an idle second post-Stop).
2. **Restart table test** — clean-exit, cancel, and error paths each produce the correct supervisor behaviour; an http-style service whose `Start` returns nil immediately is *not* restarted; no nil ever reaches `errs`.
3. **Idempotent Start** — a double `Start()` does not double-start services and `Wait()` still returns.
4. **Nil funcs** — a service with no `Start`/`Stop` does not panic at start or shutdown.
5. **Signals** — `WithoutSignals` leaves SIGINT/SIGTERM with default disposition; a custom channel via `SetSignalsChannel` actually receives.
6. **Force-stop** — a `StopFunc` that ignores its context is abandoned at the shutdown deadline; `Wait()` returns.
7. **Readiness** — `/readyz` reports not-ready until the first async check completes.
8. **E2E** — re-run the controls BDD scenarios (`INT_TEST_E2E_CONTROLS=1`).
9. **Docs** — update `docs/components/controls.md` and `docs/concepts/service-orchestration.md`.

## Out of scope

- Transport-specific serve-error reporting (covered by the transport-bugs work already merged in MR !69).
- A new health-check engine (this consumes the existing checks).

## Related

- [2026-06-12 codebase audit](../reports/codebase-audit-2026-06-12.md) §3.2, §3.5
- [Plan 1 — security & bugs](../plans/2026-06-12-audit-plan-1-security-and-bugs.md) Phase 10
- `docs/components/controls.md`, `docs/concepts/service-orchestration.md`
