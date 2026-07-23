---
title: "Transport stack follow-ups: bind addresses, option safety, retry semantics, and hardening batch"
description: "Batches the MEDIUM and LOW findings from the 2026-07-23 architectural review of the transport stack into one grouped remediation spec: server bind-address support, runtime rejection of mistyped ...any options, GTB port-error passthrough, retry idempotency and defaults, hostPinnedAuth explicitness, gRPC rate-limit peer keying, responseLogger Unwrap, breaker enum alignment, gateway connection lifecycle, and localca write/expiry hardening. The three HIGH findings are covered by dedicated sibling specs and are out of scope here."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - transport
  - transit
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Transport stack follow-ups: bind addresses, option safety, retry semantics, and hardening batch

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (transport section, MEDIUM/LOW findings),
    [gateway single middleware application](2026-07-23-transport-gateway-single-middleware-application.md) (HIGH),
    [transit breaker half-open recovery](2026-07-23-transit-breaker-halfopen-recovery.md) (HIGH),
    [transport server TLS/mTLS support](2026-07-23-transport-server-tls-mtls-support.md) (HIGH)

---

## 1. Context

The 2026-07-23 architectural review of the transport stack (`go/transit`,
`go/transport`, `go/transport-metrics`, `go/localca`, plus GTB glue) raised
three HIGH findings — each owned by a dedicated spec (see Related) — and
eleven MEDIUM/LOW findings that are individually small but collectively
meaningful: several weaken security posture by default (all-interfaces binds,
silently dropped auth options, non-idempotent retries), the rest are
correctness and lifecycle papercuts. This spec batches all eleven so they can
be sequenced, released, and verified together rather than trickling through
as unrelated one-line fixes.

## 2. Items

### 2.1 Server bind & option safety

**2.1.1 Bind-address support across the stack** *(MEDIUM)*.
Every managed server binds to all interfaces: `go/transport/http/server.go:198`
builds `Addr: fmt.Sprintf(":%d", port)` and `go/transport/grpc/server.go:230`
does the same for the gRPC listener; no settings type, option, or GTB config
key carries a host/bind field. `transport-metrics/server/server.go:30-32`
explicitly instructs operators to bind the metrics/pprof server to loopback —
which the API cannot express. A metrics server with `WithPprof()` on 0.0.0.0
is a real information-disclosure and DoS surface (heap dumps, 30-second CPU
profiles).
**Direction:** add `Host`/`BindAddress` to `ServerSettings` in both transports
(default `""` — all interfaces — for compatibility), thread it through
`resolvePort`/listen, default the standalone metrics server to `127.0.0.1`
(see §4 Q1), and expose the key in the GTB config adapters.
**Acceptance:** a server configured with `Host: "127.0.0.1"` is reachable on
loopback only; the GTB config key round-trips.

**2.1.2 Reject mistyped `...any` options** *(MEDIUM)*.
The `...any` option variadics document that "other types are ignored"
(`transport/http/server.go:457-464`, `transport/grpc/server.go:96-110,
311-326, 442-461`; same pattern in GTB's `pkg/http/config_adapter.go:144-159,
209-227` and `pkg/grpc/config_adapter.go`). A caller passing
`transportgrpc.WithInterceptors(authChain)` to `NewServer` compiles and runs —
with the entire auth/logging/rate-limit chain silently dropped, leaving the
server unauthenticated.
**Direction:** add a `default:` arm to each type switch that returns an error
(preferred; the constructors already return errors) or, where the signature
cannot, logs at WARN naming the offending type. Apply to both modules and the
GTB adapters.
**Acceptance:** an option value outside the accepted families produces a
constructor error (or WARN) naming the rejected type; nothing is silently
discarded.

**2.1.3 GTB adapters must not convert an invalid port into an ephemeral bind** *(MEDIUM)*.
`pkg/http/config_adapter.go:184-188, 225-227` short-circuits an out-of-range
`WithPort` to `ServerSettings{}` — port 0, an OS-assigned random port — and
the adapter tests assert `":0"` deliberately. The transport module treats the
same input as a hard error (`transport/http/server.go:216-226`); the GTB
layer suppresses that contract, so a typo'd port env var yields a "healthy"
server nobody can find.
**Direction:** forward the raw value as `transporthttp.WithPort` so the
transport's validation fires, or return the error directly from the adapter.
**Acceptance:** an out-of-range configured port fails server construction with
a descriptive error; no test asserts an ephemeral-port bind for invalid input.

### 2.2 Client resilience semantics

**2.2.1 Do not retry non-idempotent requests by default** *(MEDIUM)*.
`go/transit/http/retry.go:133-151` — `shouldRetry` has no method/idempotency
check; the only guard is `GetBody == nil` (line 142), and `net/http`
auto-populates `GetBody` for common buffer bodies. With 502/504 in the default
retryable codes, a POST processed at the origin before a proxy 502 is silently
resent — a duplicated non-idempotent operation. Additionally `RoundTrip`
mutates the caller's request (`req.Body, err = req.GetBody()`, line 110),
violating the `http.RoundTripper` contract.
**Direction:** default to retrying only idempotent methods (GET/HEAD/OPTIONS/
PUT/DELETE per RFC 9110), plus any method when the failure is provably
before-send (connection refused); offer `WithRetryAllMethods` or a predicate
for opt-in (see §4 Q2). Clone the request per attempt, fixing the mutation in
the same change.
**Acceptance:** a POST receiving a retryable status is not retried under the
default config; the caller's original `*http.Request` is unmutated after
`RoundTrip`.

**2.2.2 Apply the documented `RetryableStatusCodes` default** *(MEDIUM)*.
`go/transit/http/retry.go:32-34` documents "Default: []int{429, 502, 503,
504}", but `normalized()` (lines 64-82) only clamps `MaxRetries` and the
backoff durations — a nil slice is passed straight to `defaultShouldRetry`,
where `slices.Contains(nil, code)` retries nothing. A caller writing
`RetryConfig{MaxRetries: 5}` gets network-error retries only; the 429/503 case
they configured it for silently does nothing.
**Direction:** in `normalized()`, default a nil `RetryableStatusCodes` to the
documented set; preserve an explicit empty non-nil slice as "network errors
only".
**Acceptance:** `RetryConfig{MaxRetries: 5}` retries a 503; `RetryableStatusCodes: []int{}` does not.

**2.2.3 Make `hostPinnedAuth` explicit about its pinned host** *(LOW)*.
`go/transit/http/client_middleware.go:90-106` pins the credential's allowed
host via `sync.Once` from whichever request wins the race on first use: two
goroutines issuing a client's first requests to different hosts means one
nondeterministically sends no credential and gets an unexplained 401. The
single-host assumption is documented, but the failure is silent.
**Direction:** accept the intended host explicitly in
`WithBearerToken`/`WithBasicAuth` (first-request pin kept as fallback), and
log at WARN whenever a request host mismatches the pin.
**Acceptance:** a mismatched-host request produces a WARN naming both hosts;
an explicitly supplied host is honoured regardless of request order.

### 2.3 Rate limiting & logging

**2.3.1 Key the gRPC rate limiter on client IP, not `ip:port`** *(MEDIUM)*.
`go/transit/grpc/ratelimit.go:153-159` — `PeerKey` returns `p.Addr.String()`,
which includes the ephemeral source port, unlike the HTTP counterpart
`ClientIPKey` which strips it. Every new TCP connection from the same client
gets a fresh token bucket, so a client evades per-client limiting by
re-dialing, and connection churn evicts legitimate entries from the bounded
LRU store. The two transports' "per-client" semantics silently differ.
**Direction:** split host from `p.Addr` via `net.SplitHostPort` in `PeerKey`,
mirroring `ClientIPKey`.
**Acceptance:** two connections from the same source IP share one bucket; the
HTTP and gRPC key functions produce equivalent keys for the same client.

**2.3.2 Add `Unwrap` to `responseLogger`** *(LOW)*.
`go/transit/http/logging.go:380-422` implements `Flush`/`Hijack` explicitly
but not `Unwrap() http.ResponseWriter`; the sibling `statusRecorder` in
transport-metrics does (`transport-metrics/http.go:183-185`). A handler behind
`LoggingMiddleware` calling `http.NewResponseController(w).SetWriteDeadline`
gets `ErrNotSupported`, and `io.Copy` loses the `ReaderFrom` sendfile path.
**Direction:** add `Unwrap` returning the wrapped writer.
**Acceptance:** `ResponseController.SetWriteDeadline` succeeds through
`LoggingMiddleware`.

### 2.4 Metrics & lifecycle glue

**2.4.1 Align breaker state enums or provide a mapper** *(LOW)*.
`resilience/breaker.go:19-27` orders `Closed=0, Open=1, HalfOpen=2`;
`transport-metrics/breaker.go:12-19` orders `Closed=0, HalfOpen=1, Open=2`.
No current GTB code bridges them, but the obvious wiring —
`cb.SetState(metrics.BreakerState(to))` in an `OnStateChange` hook — silently
swaps Open and HalfOpen on the dashboard.
**Direction:** align the numeric order in transport-metrics (pre-1.0 clean
break per house policy) or add a `FromResilienceState` mapper; document the
choice at both definition sites.
**Acceptance:** a naive state-change hook between the two modules reports the
correct state for all three values (cross-module mapping test).

**2.4.2 Close the gateway gRPC client connection** *(LOW)*.
`transport/gateway`'s `Register` receives the `*grpc.ClientConn` but registers
only the HTTP server's start/stop with the controller. GTB's
`pkg/gateway/config_adapter.go:163-183` dials the conn and — unlike
`NewFromConfig`, which closes on error — leaks it when `Register` fails;
neither path closes it on controller shutdown.
**Direction:** close the conn on the `Register` error path, and register a
controller stop hook closing it on shutdown (or explicitly document the conn
as process-lifetime and close only on error).
**Acceptance:** a failed `Register` leaves no open connection; controller
shutdown closes the dialed conn.

### 2.5 localca hardening

**2.5.1 Atomic key writes** *(LOW)*.
`go/localca/store.go:139-145` writes root key material in place via
`afero.WriteFile`; a crash mid-write leaves a truncated key that
`loadRoot`→`decodeRootPEM` then reports as a hard error rather than
re-minting, bricking the local CA until manual intervention.
**Direction:** write-to-temp-then-rename for all key/cert material (same
directory, 0600 before rename).
**Acceptance:** a simulated interrupted write leaves the previous valid key
intact and loadable.

**2.5.2 Clamp leaf expiry to root expiry** *(LOW)*.
`go/localca/authority.go:252-265` + `ca.go:80-125` — `ensureLeaf` checks only
the leaf's own expiry/coverage, so near the root's 10-year `NotAfter` it mints
a 90-day leaf outliving its root, which clients reject with a confusing chain
error rather than a clear "root expiring" signal.
**Direction:** clamp leaf `NotAfter` to the root's `NotAfter`, and surface a
"root expiring soon — re-run Install" warning when the clamp engages.
**Acceptance:** no minted leaf's `NotAfter` exceeds the root's; minting within
the warning window emits the re-install signal.

## 3. Scope & release plan

Repos touched: **go/transit** (2.2.1, 2.2.2, 2.2.3, 2.3.1, 2.3.2),
**go/transport** (2.1.1, 2.1.2, 2.4.2), **go/transport-metrics** (2.1.1
metrics-server loopback default, 2.4.1), **go/localca** (2.5.x), and **GTB
glue** (2.1.2 adapters, 2.1.3, 2.4.2 adapter path, config keys for 2.1.1).

Sequencing: **transit first** (client-side semantics land as one
`fix(http)`/`fix(grpc)` release), then **transport / transport-metrics /
localca** (transport picks up the transit bump alongside its bind-address and
option-safety changes), then **GTB** (dependency bumps + adapter fixes + new
`host` config keys in one MR). The bind-address addition is **additive API**
— `ServerSettings` gains a field defaulting to current behaviour — shipped as
a minor `feat`; the metrics-server loopback default and the retry idempotency
flip are the two deliberate behaviour changes and get changelog + migration
notes.

## 4. Open questions

1. **Metrics server loopback default is behaviour-breaking.** Anyone scraping
   a standalone metrics server over the network today relies on the implicit
   0.0.0.0 bind; defaulting to `127.0.0.1` breaks them on upgrade. Ship the
   safe default with a loud changelog/migration note (recommended pre-1.0), or
   keep `""` and only WARN when pprof is enabled on an all-interfaces bind?
2. **Retry idempotency opt-out shape.** Is a boolean `RetryAllMethods` enough,
   or should the escape hatch be a `RetryableMethods []string` /
   method-predicate for callers with idempotency-keyed POSTs? A predicate is
   more expressive but grows `RetryConfig`; the review suggested both.
3. **Option-safety failure mode per surface.** Constructors that return errors
   should error on unknown option types; surfaces without an error return may
   only be able to WARN. Is a mixed error/WARN policy acceptable, or should
   adapter signatures change so errors are possible everywhere?
4. **hostPinnedAuth compatibility.** Keep the zero-argument first-request-pin
   behaviour as a deprecated fallback, or require the explicit host (breaking,
   pre-1.0-acceptable) so the silent misdirection cannot recur?
