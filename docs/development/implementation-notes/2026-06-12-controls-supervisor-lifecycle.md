---
title: "Implementation notes, controls supervisor & lifecycle hardening"
description: "What was implemented for the 2026-06-12 controls supervisor/lifecycle spec, deviations, the O4 signature enumeration, and open questions."
date: 2026-06-12
tags: [implementation-notes, controls, lifecycle, concurrency]
---

# Implementation notes: controls supervisor & lifecycle hardening

Spec: [`0075-controls-supervisor-lifecycle`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0075-controls-supervisor-lifecycle)
Branch: `fix/controls-supervisor-lifecycle`

## What was implemented

All seven findings (and the three folded-in low/info items) are addressed.

- **D1: Explicit run outcomes.** `Services.classifyRun(ctx, err)` returns one of
  `outcomeCleanStart`, `outcomeCancelled`, `outcomeError`. `runOnce` and the new
  `runOnceWithRestart` consult it. A `nil` Start return is a clean start, never an
  exit; a clean-start service with no health monitoring blocks in `monitorHealth`
  on `<-ctx.Done()` rather than falling through to a restart.
- **D2: No nil on errs; consecutive-failure counter.** `runWithRestartPolicy`
  only forwards non-nil Start errors; health failures store their error via
  `monitorHealth`/`updateInfo`. The counter resets after `RestartResetInterval`
  (default `DefaultRestartResetInterval` = 30 s). `errors.Wrap(nil, …)` is guarded
  in the max-restarts path.
- **D3: Idempotent Start.** `Start` does `compareAndSetState(Unknown, Running)`
  and returns early otherwise. The service count is snapshotted under
  `services.mu` before the matching `wg.Add`.
- **D4: No busy-spin; goroutines terminate.** The error/context handler nils its
  local `done` channel after first receipt and exits on `shutdownComplete` (closed
  by `handleStopMessage`), draining buffered errors first. The message processor
  and signal handler also exit on `shutdownComplete`; the signal handler adds a
  second-signal force-exit.
- **D5: Validated lifecycle funcs.** `Services.add` defaults missing
  `Start`/`Stop` to `noopStart`/`noopStop`. `callStop` recover-wraps the stop call
  (matching the existing `callProbe` pattern).
- **D6: Signal disposition + force-stop.** `signal.Notify` is deferred until after
  options in `NewController` and only fires when `c.signals != nil`.
  `SetSignalsChannel` and shutdown call `signal.Stop`. `Services.stop` iterates in
  reverse registration order, running each `StopFunc` in a goroutine awaited
  against `ctx.Done()` so a context-ignoring stop is abandoned at the deadline.
- **D7: Readiness fails closed; ValidErrorFunc wired.** `toServiceStatus` takes a
  `failClosed` flag (true only for `Readiness`); a nil cached async result is
  reported not-ready. `WithValidError` sets `Controller.validError`, propagated to
  `Services.validError` in `Start` and consulted by `classifyRun`.

## Deviations from the spec (and why)

1. **`Register` does NOT return an error.** The spec offered "return an error OR
   default to no-ops" (D5). I chose **default-to-no-ops**. `Register` is on the
   widely-implemented `Runner`/`Controllable` interface with a generated mock and
   many call sites (`pkg/grpc`, `pkg/http`, `pkg/gateway`, `pkg/telemetry`).
   Defaulting keeps the signature stable, avoids a mock regen + multi-package
   churn, and still satisfies "every call site nil-checks before invoking" because
   the funcs are never nil after registration. Net result: **no breaking
   exported-signature change** (see O4).
2. **Reverse-order stop is best-effort past the deadline.** When a `StopFunc`
   exceeds the shutdown deadline, the remaining (earlier-registered) services still
   get a stop attempt, but with the deadline already elapsed their own `ctx.Done()`
   fires immediately. The abandoned goroutine is left to finish on its own (a small,
   bounded leak that ends when the misbehaving stop eventually returns). This favours
   "always return from `Wait()`" over "guarantee every stop completes", which is the
   spec's intent.
3. **`monitorHealth` now returns `bool`** (true = ended by health-failure). It also
   blocks on `<-ctx.Done()` when there is nothing to monitor, so a clean start is
   never restarted. This is internal-only.

## O4: Enumerated exported-signature changes (for changelog/migration)

Migration note: `docs/migration/v0.16-controls-supervisor.md`.

| Symbol | Before | After | Kind |
|--------|--------|-------|------|
| `controls.WithValidError(ValidErrorFunc) ControllerOpt` | — | new | **Additive** |
| `controls.WithRestartResetInterval(time.Duration) ServiceOption` | — | new | **Additive** |
| `controls.RestartPolicy.RestartResetInterval time.Duration` | — | new field | **Additive** |
| `controls.DefaultRestartResetInterval` (= 30s) | — | new const | **Additive** |
| `controls.Register` | `Register(id string, opts ...ServiceOption)` | unchanged | **No change** |

There is **no breaking exported-signature change**. The breaking change is
**behavioural**: a clean `Start` return is no longer restarted, and `MaxRestarts`
now counts consecutive (not lifetime) failures. Documented with a `BREAKING
CHANGE:` footer and the migration note above. Pre-1.0, this is a MINOR bump.

## Verification

- `go test ./pkg/controls/`: green.
- `go test -race ./pkg/controls/`: green across repeated runs (the core goal).
- `golangci-lint run ./pkg/controls/...`: 0 issues.
- Coverage on `pkg/controls`: ~96% of statements (≥90% gate met).
- Dependent packages (`pkg/grpc`, `pkg/http`, `pkg/gateway`, `pkg/telemetry`) and
  the full `go test ./...` suite pass.
- `mockery` was **not run** locally (binary not installed in the worktree), but no
  mocked interface signature changed, so `mocks/pkg/controls/Controllable.go` is
  unaffected and still compiles (`go build ./mocks/...` passes).
- Controls E2E BDD (`INT_TEST_E2E_CONTROLS=1 just test-e2e`): **ran green** in this
  worktree (graceful shutdown with real HTTP+gRPC, restart-after-health-failure,
  idempotent stop, context-cancellation, readiness/liveness scenarios all pass).

## Open questions for review

1. **`Register` return value.** Confirmed choice: default-to-no-ops over returning
   an error (keeps the API non-breaking). If reviewers prefer the validating
   `Register(...) error` variant, it is a larger, mock-regenerating change, flag if
   wanted.
2. **Restart-reset default of 30 s.** Taken verbatim from the resolved O1. Confirm
   this is the right default for the common embedder.
3. **Best-effort reverse-stop after deadline.** Confirm the "abandon and continue,
   leaving the goroutine to finish" semantics (vs. abandoning all remaining stops at
   the first deadline breach).
4. **E2E ran locally; integration tests pending CI.** The controls BDD E2E suite
   (`INT_TEST_E2E_CONTROLS=1 just test-e2e`) ran green in this worktree. The
   `INT_TEST=1 just test-integration` group (which also stands up real servers) was
   not separately run here; the `*_integration_test.go` files compile against the new
   API. Please confirm the integration group in CI.
5. **Timing tolerances.** `TestForceStop_*` uses a 100 ms shutdown timeout and
   `TestReadiness_FailsClosed*` blocks the first async run via a channel. These are
   poll/`Eventually`-based, not hard sleeps, but confirm the tolerances hold on
   slower CI runners under `-race`.
