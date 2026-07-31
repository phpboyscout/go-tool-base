---
title: "Implementation notes — controls pre-extraction lifecycle races (D8, D9)"
description: "Two concurrency defects found in a fresh-eyes review before extracting pkg/controls to a standalone module: a data race on the async health-check CancelFunc during startup, and an unguarded blocking error-channel send during shutdown."
date: 2026-07-13
tags: [implementation-notes, controls, lifecycle, concurrency, extraction]
---

# Implementation notes: controls pre-extraction lifecycle races

Branch: `fix/controls-startup-shutdown-races`

Before extracting `pkg/controls` into the standalone `gitlab.com/phpboyscout/go/controls`
module, a fresh adversarial review of the concurrency surface turned up two latent
defects beyond the seven closed by
[`0075-controls-supervisor-lifecycle`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0075-controls-supervisor-lifecycle)
(D1–D7). Both are narrow and timing-dependent — neither was caught by the existing
race tests — but both are cheaper to close in-tree than across a module boundary,
so they are fixed here. They continue that spec's `D<n>` marker convention.

## D8 — Data race on `healthCheckEntry.cancel` when shutdown lands mid-startup

`Start()` launched the control goroutines (`go c.controls()`, which starts the
signal handler and the message processor) **before** `startAsyncHealthChecks()`
recorded each async check's `context.CancelFunc` in `entry.cancel`. A shutdown
triggered in that window — an OS signal, a parent-context cancellation, both of
which the freshly-launched goroutines can act on — reaches `cancelHealthChecks()`,
which reads `entry.cancel` while `startAsyncCheck` is still writing it. The two
accesses have no happens-before edge: an unsynchronised read/write data race.

**Fix.** Reorder `Start()` so services and async health checks are wired up
*before* `go c.controls()`. All `entry.cancel` writes then happen-before the
goroutines that can read them start, so the accesses are ordered. This is also the
more natural order: set up the things a shutdown must clean up before starting the
loop that can trigger the shutdown.

**Test.** `TestStart_ShutdownDuringStartupNoCancelRace` pre-loads a buffered signal
before `Start()` so the signal handler fires at the earliest possible moment after
the control goroutines launch, maximally overlapping the two accesses; run under
`-race` it reliably flagged the race before the fix and is clean after.

## D9 — Unguarded blocking send on the error channel after the handler exits

The supervisor forwards genuine service errors on the unbuffered `errs` channel
(`runOnce`, `runWithRestartPolicy`). The error/context handler that drains `errs`
exits once `handleStopMessage` closes `shutdownComplete`. A supervisor goroutine
that reaches its `errs <- err` after the handler has exited would block forever —
a goroutine leak. In practice this is near-unreachable (the controller context is
cancelled at the very start of shutdown, so a run producing an error after that
classifies as `outcomeCancelled` and never forwards), but the send is not provably
non-blocking, which is an undesirable property to ship in a standalone module.

**Fix.** Thread `shutdownComplete` into the `Services` send sites and route every
forward through `sendErr(done, errs, err)`, which selects on `errs <- err` **or**
`<-done`. Once shutdown has completed the send falls through instead of blocking.
The forward is now provably non-blocking by construction, independent of the
classify/cancel timing.

**Test.** `TestSupervise_ErrorForwardDoesNotLeakOnShutdown` is a stress/leak guard
(the deterministic window is not reachable without a test hook into production
code): it runs many error-during-shutdown cycles and asserts the goroutine count
returns to baseline. A regression to an unguarded blocking send would accumulate
parked supervisor goroutines and fail the check.

## Verification

- `go test -race ./pkg/controls/...` (unit + `INT_TEST_CONTROLS=1`) green.
- Dependent transport packages (`pkg/http`, `pkg/grpc`, `pkg/gateway`) green under `-race`.
- `golangci-lint run ./pkg/controls/...` clean; coverage 96.6%.
