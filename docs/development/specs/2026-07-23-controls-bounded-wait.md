---
title: "controls: bound Wait() against context-ignoring StartFuncs"
description: "Wait() hangs forever when a service's StartFunc ignores cancellation and its StopFunc cannot unblock it: the supervisor goroutine never returns, so the WaitGroup never drains, even though the controller has completed its bounded shutdown and reports Stopped. Add a deadline-bounded WaitContext variant applying the same abandon-at-deadline policy Services.stop already uses for StopFuncs, documented as a new D-number decision."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - controls
  - concurrency
  - lifecycle
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# controls: bound Wait() against context-ignoring StartFuncs

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Shipped in go/controls v0.1.3 (controls MR !13), red-first TDD with the defect reproduced against the module's pre-fix main before the change; GTB picks it up via the config-family / module dependency bumps. Open questions were resolved by the maintainer during triage.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH controls finding),
    `go/controls` `docs/explanation/concurrency.md` (decisions D1–D9 this extends)

---

## 1. Problem

`go/controls` promises "bounded graceful shutdown": `Services.stop`
(`services.go:326-353`) runs each StopFunc in its own goroutine and awaits it
against `ctx.Done()`, explicitly abandoning a context-ignoring **StopFunc** at
the shutdown deadline "rather than hanging the caller (and Wait()) forever"
(its own doc comment). But the bound does not extend to **StartFuncs**:

- `services.go:152-162` — the supervisor decrements the controller WaitGroup
  only via the deferred `markStarted` when `supervise` returns; for the
  no-restart-policy path (`runOnce`, `services.go:184`) that requires
  `srv.Start(ctx)` itself to return.
- `controller.go:259-261` — `Wait()` is bare `c.wg.Wait()` with no escape.

If a StartFunc blocks while ignoring `ctx.Done()` — the canonical case being a
wrapper around a third-party blocking `Run()` with no cancellation support —
and its StopFunc does not unblock it, the supervisor goroutine never exits.
The shutdown sequence itself completes: services are stopped (or abandoned) on
deadline, the state machine reaches `Stopped`, `shutdownComplete` closes — and
then `Wait()` hangs forever. A `main` shaped as
`controller.Start(); controller.Wait()` (the documented usage) becomes an
unkillable-without-SIGKILL process whose controller reports itself stopped.

The module's `-race` stress suite and goroutine-leak guards are strong, but
there is no ctx-ignoring StartFunc test — which is exactly where this lives.

## 2. Proposed change

Add a deadline-bounded wait, applying to supervisor exits the same
abandon-at-deadline policy `stop` already applies to StopFuncs:

```go
// WaitContext blocks until all supervisor goroutines have exited, or until
// ctx is done. Returns nil on a clean drain, ctx.Err() when abandoned.
func (c *Controller) WaitContext(ctx context.Context) error
```

- Implementation: wait on `c.wg.Wait()` in a helper goroutine that closes a
  channel; select on that channel vs `ctx.Done()`. On the abandon path, the
  helper (and any stuck supervisors) are leaked deliberately — the identical,
  documented tradeoff already accepted for abandoned StopFuncs and for
  `regexutil`'s compile timeout.
- After shutdown completes internally, the controller should drive the same
  bound itself: `handleStopMessage` (or its caller) waits for supervisor exit
  with the remaining shutdown deadline and logs a WARN naming each service
  whose StartFunc failed to return — turning a silent hang into a diagnosable
  message.
- `Wait()` remains as-is for backward compatibility, with its doc comment
  rewritten to state loudly that it requires context-respecting StartFuncs and
  to point at `WaitContext`.
- Document the decision as the next D-number (D10) in
  `docs/explanation/concurrency.md`, covering: why the WaitGroup cannot be
  force-drained (calling `wg.Done()` on behalf of a stuck supervisor would
  race a late-returning `Start` into a double-decrement panic — hence
  select-around-`Wait`, not synthetic decrements), and the leaked-goroutine
  accounting exemption needed by the goroutine-leak guard tests.

Alternative considered: change `Wait()` itself to time-bound against the
shutdown timeout. Rejected — it changes observable behaviour of a published
API without a signature to signal it, and callers who *want* an unbounded wait
(context-respecting services, guaranteed cleanup) lose it. An explicit
`WaitContext` matches the module's context-first style.

## 3. Scope & release plan

- **Module:** `gitlab.com/phpboyscout/go/controls` (currently v0.1.x).
  Additive API (`WaitContext`) + internal shutdown-path wait + docs →
  `feat(controller):` minor release via releaser-pleaser.
- **Downstream pickup:** GTB consumes controls directly
  (`go.mod: go/controls v0.1.0`) for service lifecycle. Dep bump in GTB; audit
  GTB call sites of `Wait()` (and the scaffolded service template in
  `internal/generator/`, if it emits a `Wait()` call) and move them to
  `WaitContext` with the tool's shutdown deadline. Scaffold change means
  regenerate-and-verify per the generator workflow.
- **Docs:** module README/`docs/index.md` shutdown section updated so the
  "bounded graceful shutdown" claim states its coverage precisely; new D10
  entry in `concurrency.md`.

## 4. Acceptance criteria

- **ctx-ignoring StartFunc test:** a service whose `StartFunc` blocks on a
  bare `select {}` (ignores ctx; StopFunc is a no-op) — `Stop()` completes,
  state reaches `Stopped`, and `WaitContext` with a short deadline returns
  `context.DeadlineExceeded` promptly instead of hanging. Guarded by a test
  timeout well under the suite default.
- Clean-path test: with context-respecting services, `WaitContext` returns
  `nil` and (per the existing leak guards) no goroutines remain.
- Shutdown-path WARN test: the stuck service's name appears in a WARN log
  record after the internal post-stop wait expires.
- Race coverage: the new paths run under the existing `-race` stress patterns
  (concurrent `Stop`/`WaitContext`, pre-loaded signal), including
  `WaitContext` called concurrently from multiple goroutines.
- Goroutine-leak guard updated to tolerate (and count) the deliberately
  abandoned supervisor in the stuck-StartFunc test only.
- `Wait()` behaviour for existing conforming callers is byte-for-byte
  unchanged.

## 5. Open questions

1. Should the internal post-stop supervisor wait share the existing
   `shutdownTimeout` budget (stop consumed some of it) or get its own small
   grace period? Recommendation: share the deadline — one number for "bounded
   shutdown" is easier to reason about and matches the `stop` contract.
2. Is a stuck StartFunc worth surfacing beyond WARN — e.g. recording it in
   `ServiceInfo` so `Status()` can report "abandoned"? Nice-to-have; can be a
   follow-up.
