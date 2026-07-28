---
title: "controls: medium/low follow-ups from the architectural review"
description: "Batches the three MEDIUM and three LOW controls findings from the 2026-07-23 architectural review into one hardening pass: enforcing the health-check Timeout with a goroutine+select and a staleness bound on cached results, guarding the Stop() channel send against a completed shutdown (the D9 pattern), releasing the services mutex during the stop sequence so health probes stay responsive, correcting documentation claims (startup ordering, phantom states), moving signal registration into Start, and clearing three supervisor/API blemishes. The HIGH Wait() finding is owned by the controls-bounded-wait spec."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - controls
  - concurrency
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# controls: medium/low follow-ups from the architectural review

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED — shipped in `gitlab.com/phpboyscout/go/controls` **v0.1.4**
    (F1–F6, MR !20). Q1 and Q2 resolved (see § 4). New concurrency decisions
    D11 (health-check timeout race + async cache staleness) and D12
    (services-mutex release during the stop sequence) recorded in the module's
    `docs/explanation/concurrency.md`; D9 extended to name `Stop()` as a covered
    sender. GTB picks the fix up on its next controls dep bump.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (§ controls),
    [controls: bound Wait()](2026-07-23-controls-bounded-wait.md) (the sibling HIGH finding, specced separately; claims D10)

---

## 1. Context

The 2026-07-23 architectural review found `gitlab.com/phpboyscout/go/controls`
concurrency-careful where its documented decisions (D3–D9 in
`docs/explanation/concurrency.md`) apply, but flagged three MEDIUM gaps where a
correct pattern was applied in one place and missed in its twin, plus three LOW
documentation/API blemishes. The HIGH finding — `Wait()` hanging on a
context-ignoring StartFunc — is owned by the
[bounded-wait spec](2026-07-23-controls-bounded-wait.md); this spec batches
everything else into one hardening pass so a single controls release wave
carries it. Every new non-obvious ordering choice made here must be recorded as
a fresh D-number in `docs/explanation/concurrency.md`, continuing the series
after the D10 the bounded-wait spec introduces.

## 2. Items

### 2.1 Health checking

**F1 — Enforce the health-check Timeout; bound cache staleness.** *(MEDIUM)*
`runCheck` (`healthcheck.go:19-31`) derives a timeout context but then calls
`e.check.Check(ctx)` synchronously — nothing enforces the deadline. A check
that ignores its context blocks both paths: the sync path
(`healthcheck.go:35-44`) runs it inline from `Status()`/`Readiness()`, hanging
every health HTTP request; the async path wedges the ticker goroutine
(`controller.go:461-478`), so `lastResult` is never refreshed and readiness
serves the last cached *healthy* result indefinitely — a dead dependency
reported healthy forever. There is also no staleness check on the cached
result's `Timestamp`.
Direction: run the check in a goroutine and select on `ctx.Done()`, recording
a timeout `CheckResult` on expiry (the abandoned goroutine is left to finish,
matching the StopFunc-abandonment policy); additionally fail readiness when an
async cached result is older than N×`Interval` (N to be fixed in Q1). Document
the abandonment choice as a new D-number.
*Acceptance:* a check blocking on `select {}` yields a timeout result within
`Timeout`+ε on both sync and async paths, and a stale async cache flips
readiness to not-ready, all under test with the existing goroutine-leak guard
updated for the deliberate abandonment.

### 2.2 Shutdown path

**F2 — Guard the Stop() send against a completed shutdown.** *(MEDIUM)*
`Stop()` does CAS `Running→Stopping` then a bare `c.messages <- Stop`
(`controller.go:272`) on an unbuffered channel whose only receiver,
`processControlMessages` (`controller.go:367-385`), exits once
`shutdownComplete` closes. Interleaving: caller A wins the CAS and is
descheduled before its send; a direct-channel `Stop` message drives the full
shutdown; the processor exits; A's send blocks forever. The supervisors'
`sendErr` (`services.go:136-141`) received exactly this guard as decision D9 —
`Stop()` did not.
Direction: apply the same
`select { case c.messages <- Stop: case <-c.shutdownComplete: }` guard and
extend the D9 write-up (or add a successor D-number) to name `Stop()` as a
covered sender.
*Acceptance:* a race test that closes `shutdownComplete` between the CAS and
the send observes `Stop()` returning promptly instead of hanging.

**F3 — Do not hold the services mutex for the whole stop sequence.**
*(MEDIUM)*
`Services.stop` (`services.go:325-353`) takes `q.mu` and holds it while
sequentially awaiting every StopFunc — up to the entire `shutdownTimeout`.
`status()`/`liveness()`/`readiness()` (`services.go:378, 407, 441`) take the
same mutex, so during shutdown every health/readiness probe blocks instead of
failing fast — exactly when a load balancer most needs a prompt not-ready
answer.
Direction: snapshot the service slice under the lock, release it, then run the
reverse-order stop sequence over the snapshot. Registration is already
impossible in `Stopping` state, so the snapshot cannot go stale; record that
reasoning as a D-number.
*Acceptance:* a test with a slow StopFunc observes `readiness()` returning
(not-ready) while `stop` is still in flight.

### 2.3 Documentation and API surface

**F4 — Correct documentation claims.** *(LOW)*
`README.md:5` and `docs/index.md:4` headline "startup ordering", but
`Services.start` (`services.go:143-150`) launches all supervisors concurrently
and `docs/how-to/register-services.md:56` admits it; `controls.go:77`
documents `State` as "(Created, Starting, Running, Stopped)" when the actual
states are `Unknown`/`Running`/`Stopping`/`Stopped` (verified at head).
Direction: rewrite the headline to "ordered (reverse-registration) shutdown"
and fix the `State` doc comment — do not implement startup ordering to match
the prose (that would be its own spec).
*Acceptance:* no doc or comment claims a state or ordering mechanism the code
lacks.

**F5 — Stop swallowing SIGINT/SIGTERM before Start.** *(LOW)*
`signal.Notify` is registered at construction (`controller.go:647-649`) but
the reader goroutine only starts inside `Start()` (`controller.go:282-309`);
a controller constructed but never started leaves the process permanently
ignoring Ctrl-C/SIGTERM.
Direction: move `signal.Notify` into `Start`, or call `signal.Stop` on every
pre-Start teardown path.
*Acceptance:* a process that constructs a controller without starting it still
terminates on SIGINT, under test.

**F6 — Supervisor/API blemishes.** *(LOW)*
Three small defects: (a) after a healthy run resets the `restarts` counter
(`services.go:266-318`), `timings.backoff` is never reset to
`InitialBackoff` — a service healthy for hours still waits the accumulated
`MaxBackoff` before its next restart; (b) the `Status` control message
computes `c.services.status()` and discards it (`controller.go:379`, verified:
`_ = c.services.status()`); (c) the `HealthMessage` channel
(`controller.go:59-65`) is dead plumbing — created, exposed via
`Health()`/`SetHealthChannel`, never read or written anywhere in the package.
Direction: reset backoff alongside the restart counter; make `Status` deliver
its result (or delete the message, per Q2); remove the `HealthMessage`
plumbing (pre-1.0 clean break) unless Q2 resolves to wiring it up.
*Acceptance:* backoff after a healthy run equals `InitialBackoff` under test;
no exported symbol is dead per `deadcode`.

## 3. Scope & release plan

- **Module:** `gitlab.com/phpboyscout/go/controls` (v0.1.x). F1–F3 are
  behavioural fixes (`fix(...)` commits), F4 is docs-only, F5–F6 are small
  fixes with one pre-1.0 API removal (`HealthMessage`, noted in the commit
  body) — one release wave via releaser-pleaser, ideally the same wave as the
  bounded-wait spec so GTB re-pins once. Each new concurrency decision (F1
  abandonment, F2 guard extension, F3 snapshot) gets a D-number entry in
  `docs/explanation/concurrency.md`.
- **Downstream pickup:** GTB consumes controls directly — a single dep bump in
  `go.mod` after release. Audit GTB and the `internal/generator/` service
  skeleton for uses of `Health()`/`SetHealthChannel` before removing them
  (none are expected); scaffold changes require the regenerate-and-verify
  step.
- Items are independently mergeable; a stalled open question on one must not
  hold the others.

## 4. Open questions (resolved)

- **Q1 — staleness bound. RESOLVED: 3× `Interval`; fail readiness *and* surface
  in `Status()`.** The looser 3× multiple was chosen over 2× to avoid flapping
  on a slow check. A cached async result older than `stalenessIntervalMultiple`
  (3) × `Interval` is treated as stale: it fails readiness closed and is
  reported as an `"ERROR"` entry in every aggregation, so it is surfaced in
  `Status()` output as well as gating readiness. Implemented in the module's
  `healthcheck.go` (`healthCheckEntry.stale`) and `controller.go`
  (`healthCheckStatuses`); documented as D11.
- **Q2 — Status message and HealthMessage fate. RESOLVED: DELETE both.** No
  production consumer exists, so both were removed as dead pre-1.0 surface (the
  `Status` control message computed and discarded `services.status()`; the
  `HealthMessage` channel was never read or written). The `refactor(controls)`
  commit drops `Status`, `HealthMessage`, `Health()` and `SetHealthChannel()`
  from the public API.

    **Deviation (surfaced for ratification):** the review assumed "no named
    consumer", but GTB's E2E step definitions
    (`test/e2e/steps/controls_steps_test.go`) *do* send `controls.Status` on the
    message channel (a smoke poke of the now-removed no-op) and the associated
    Gherkin scenarios exercise it. GTB itself uses none of `Health()`/
    `SetHealthChannel()`/`HealthMessage`. So the controls-side deletion stands
    per Q2, but the GTB dep bump that picks up controls v0.1.4 **must** also
    remove those `controls.Status` E2E steps and their feature scenarios, or the
    GTB E2E build will break. Tracked as required downstream follow-up.
