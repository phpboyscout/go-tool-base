---
title: "Transit resilience: half-open trial deadline, stream-trial semantics, and panic-safe done callbacks"
description: "The shared circuit-breaker core has no time-based escape from HalfOpen: the trial slot is released only by the done callback, so an abandoned stream wedges the breaker permanently and a long-lived healthy stream holds the only trial slot for its lifetime. This spec adds a half-open trial deadline to the resilience core, redefines what constitutes stream trial success, and makes the unary paths invoke done via defer so a panicking invoker cannot leak the slot."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - transport
  - transit
  - resilience
  - circuit-breaker
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Transit resilience: half-open trial deadline, stream-trial semantics, and panic-safe done callbacks

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Shipped in go/transit v0.1.2 (transit MR !8), red-first TDD with the defect reproduced against the module's pre-fix main before the change; GTB picks it up via the config-family / module dependency bumps. Open questions were resolved by the maintainer during triage.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH finding, transport section),
    [resilience middleware](2026-06-21-resilience-middleware.md) (original breaker design),
    [transit module extraction](2026-07-16-transit-module-extraction.md)

---

## 1. Problem

The shared breaker core (`go/transit/resilience/breaker.go`) has **no time-based
escape from HalfOpen**. `Allow` (`breaker.go:113-139`) advances Open→HalfOpen
when the cooldown has elapsed (the check at line 119 applies only to
`StateOpen`), then rejects whenever `halfOpenInFlight >= HalfOpenMaxRequests`
(lines 129-130). The in-flight slot is decremented **only** by the `done`
callback (`record`, lines 151-165). Nothing else can move the breaker out of
HalfOpen.

With the default `HalfOpenMaxRequests: 1`, two concrete failures follow on the
gRPC stream path (`go/transit/grpc/circuitbreaker.go`), where `done` fires only
from a terminal `RecvMsg`/`SendMsg` error or `io.EOF`
(`breakerClientStream`, lines 217-257):

- **(a) Healthy long-lived stream starves everything else.** A watch/subscribe
  RPC admitted as the half-open trial holds the only slot for its entire
  lifetime. Every other RPC through that breaker is rejected with
  `codes.Unavailable` even though the downstream has fully recovered — the
  breaker never observes an outcome, so it never closes.
- **(b) Abandoned stream wedges the breaker permanently.** A caller that
  cancels its context and drops the stream object without a final `RecvMsg`
  never triggers `done`; `halfOpenInFlight` never decrements, and the breaker
  rejects **forever** — a permanent, unrecoverable outage of that client path.

The unary paths have the same exposure to a leaked slot if the wrapped call
panics: `done` is invoked inline, not deferred —
`transit/grpc/circuitbreaker.go:164-173` (`invoker` then `done`) and
`transit/http/circuitbreaker.go:149-161` (`next.RoundTrip` then `done`). A
panic in the invoker/transport (recovered by an outer layer or the gRPC
machinery) skips `done` and leaks the slot the same way.

## 2. Proposed change

Three coordinated changes — one in the core, one in the stream wrapper's
semantics, one mechanical hardening of the unary paths.

### 2.1 Half-open trial deadline in the resilience core

Stamp the time on entry to HalfOpen (`halfOpenAt`). In `Allow`, when the state
is HalfOpen, the budget is exhausted, **and** `Now() - halfOpenAt >= Cooldown`,
treat the outstanding trial as expired: re-trip the breaker to Open (a trial
that produced no verdict within a full cooldown is not evidence of health), from
which the normal Open→HalfOpen cooldown cycle resumes. `record` ignores a
`done` arriving after its trial expired — the existing "completed after
state moved on" guard (the `StateOpen` arm of `record`, lines 176-180) already
gives the shape; it needs a trial-generation counter so a stale `done` from an
expired trial cannot decrement a newer trial's slot.

Re-open (rather than silently releasing the slot in place) is proposed because
it keeps the invariant "HalfOpen admits at most `HalfOpenMaxRequests` verdicts
per trial window" and reuses the existing, well-tested Open→HalfOpen path. The
alternative — release the slot and stay in HalfOpen — admits an unbounded
series of verdict-less trials.

### 2.2 Stream establishment as trial success (semantic decision)

The current stream semantics conflate two roles: *trial verdict* (should the
breaker close?) and *failure counting* (per-message errors count against the
breaker). Holding the verdict until stream termination is what makes (a)
possible. The proposal: on the stream path, report **successful establishment**
as the trial verdict —

- `streamer` returns an error → `done(isFailure(err))` (unchanged, terminal).
- `streamer` succeeds → call `done(false)` **immediately**, releasing the slot
  and closing the breaker. The wrapped stream keeps counting subsequent
  per-message failures against the (now Closed) breaker via a separate
  failure-report hook, preserving the "per-message errors trip the breaker"
  behaviour documented at `circuitbreaker.go:176-180`.

This is a real semantic choice, not a mechanical fix. Establishment-as-success
closes the breaker on evidence of a completed dial + stream open, which can be
weaker evidence than a served message (a server may accept streams and then
fail every `RecvMsg`). The alternative — **first successful `RecvMsg` as the
verdict** — is stronger evidence but never fires for send-only streams and
still holds the slot indefinitely for a healthy-but-quiet subscribe stream
awaiting its first event. Establishment-as-success is recommended because the
breaker's failure classifier (`Unavailable`/`DeadlineExceeded`) is
connectivity-shaped, matching what establishment proves; per-message failures
still re-trip via the counting hook, bounding the cost of a wrong verdict to
one failure-threshold cycle. §5 Q1 puts this to the maintainer.

The §2.1 deadline remains necessary regardless: it is the backstop for
abandoned trials on **both** transports (an HTTP `RoundTrip` that never
returns is the same wedge).

### 2.3 Defer `done` on the unary paths

Both unary wrappers invoke `done` via `defer` with a recover-and-rethrow so a
panicking `invoker`/`next.RoundTrip` records a failure verdict instead of
leaking the slot: classify a panic as `done(true)`, then re-panic. Mechanical;
no semantic choice involved.

## 3. Scope & release plan

1. **`go/transit`** — all three changes live here (`resilience` core +
   `grpc`/`http` wrappers). One `fix(resilience)` release (the deadline field
   is additive; `Config` gains no breaking change).
2. **`go/transport`** — sits above transit; re-release with the bumped transit
   dependency so server-side consumers pick it up.
3. **`go/httpclient` / `go/grpcclient`** — dependency bumps (they wire the
   transit client middleware).
4. **GTB** — routine dependency bumps in `pkg/http`/`pkg/grpc` glue and
   `internal/transportcfg`; no adapter API changes expected. New config knob (if
   Q2 adds one) threads through the existing resilience-section pattern.

## 4. Acceptance criteria

1. **Wedge-recovery test (core)**: breaker in HalfOpen with the slot held and
   `done` never called; after `Cooldown` elapses (injected `Now`), `Allow`
   admits a new trial. A stale `done` from the expired trial subsequently
   arriving does not corrupt `halfOpenInFlight` or the state (trial-generation
   guard).
2. **Abandoned-stream test (grpc)**: half-open trial stream is established, the
   caller cancels its context and drops the stream without a final `RecvMsg`;
   subsequent RPCs through the breaker succeed once the downstream is healthy —
   no permanent `Unavailable`.
3. **Long-lived-stream test (grpc)**: a healthy stream admitted as the
   half-open trial; concurrent unary RPCs through the same breaker are admitted
   while the stream is still open (establishment released the slot).
4. **Per-message failure still trips**: after establishment-as-success closes
   the breaker, `FailureThreshold` classified `RecvMsg` errors re-trip it Open.
5. **Panic-safety tests (both transports)**: a panicking invoker/RoundTripper
   in HalfOpen releases the slot with a failure verdict and re-panics; the
   breaker is Open (not wedged) afterwards.
6. Existing breaker suite green; race detector clean (`just test-race`
   equivalents in transit); coverage on `resilience` stays ≥90%.

## 5. Open questions

1. **Stream trial verdict** — establishment-as-success (recommended) vs first
   successful `RecvMsg` vs configurable? A config knob adds surface for a
   subtle distinction most users will never tune.
2. Should the half-open trial deadline be a **separate duration** (e.g.
   `HalfOpenTrialTimeout`, defaulting to `Cooldown`) rather than reusing
   `Cooldown`? Reuse keeps `Config` minimal; a separate field lets slow-call
   users trial longer than they cool down.
3. On trial expiry, is re-open the right transition (recommended, §2.1), or
   should expiry release the slot in place to give the next caller an immediate
   trial?
