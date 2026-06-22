---
title: "Cross-Provider Fallback & Routing for the Chat Client"
description: "A composite ChatClient that automatically fails over (and optionally routes) across an ordered list of configured providers — Claude → OpenAI → Gemini — when one errors, rate-limits, or is unreachable, behind the existing ChatClient/StreamingChatClient/PersistentChatClient interfaces so callers are unchanged."
date: 2026-06-21
status: IMPLEMENTED
tags:
  - specification
  - chat
  - resilience
  - fallback
  - routing
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Cross-Provider Fallback & Routing for the Chat Client

Authors
:   Matt Cockayne

Date
:   21 June 2026

Status
:   IMPLEMENTED (item E1 — open questions resolved; see Resolutions)

Implementation note
:   Delivered in `pkg/chat/fallback.go` (composite + `NewFallback`/
    `NewFallbackFromConfigs`/`NewWithFallback`), `pkg/chat/fallback_policy.go`
    (`FailoverPolicy`/`DefaultFailoverPolicy`/`providerHTTPStatus`), and the
    `ai.fallback.*` keys in `client.go`; the AI call sites (`pkg/docs/ask.go`,
    the two generators) now go through `NewWithFallback`. End-to-end failover is
    proven by a gated httptest integration test
    (`fallback_integration_test.go`, real OpenAI-SDK 503 → advance). A Godog/
    Gherkin scenario was **not** added: cross-provider fallback is transparent
    library/config behaviour, not a CLI command or service-lifecycle change, so
    the "must ship Gherkin" rule does not apply; the integration test covers the
    e2e path.

Decision-log
:   Roadmap item **E1**. Open — chat enhancements are squarely in scope (`pkg/chat`
    is a flagship subsystem). No conflict with an existing or in-flight spec was
    found: the cited chat specs (deduplication, interface improvements, Gemini
    review, streaming, persistence, BaseURL validation) are all `IMPLEMENTED` and
    orthogonal to this work.

---

## Overview

`pkg/chat` exposes a single live provider per client. `chat.New` reads
`ai.provider` (or `AI_PROVIDER`, defaulting to `ProviderClaude`) and constructs
exactly one of Claude / OpenAI / OpenAI-compatible / Gemini / ClaudeLocal. If
that provider rate-limits (HTTP 429), suffers an outage (5xx), or is unreachable
(DNS/connect failure), every `Chat`/`Ask`/`StreamChat` call fails and the caller
sees an error — there is no automatic recourse to a second provider.

This spec adds a **composite `ChatClient`** — `chat.NewFallback` — that wraps an
ordered list of underlying clients and, on a *retryable* failure from the active
client, transparently advances to the next. It preserves the existing
`ChatClient`, `StreamingChatClient`, and `PersistentChatClient` semantics so that
**callers are unchanged**: a fallback client is a drop-in `ChatClient`.

The same composite is the natural home for *routing* (choosing the initial
provider by policy — cost, latency, model capability) but routing is scoped as a
**Phase 4 / future** extension behind the same type; Phases 1–3 deliver failover
only.

### Goals

- Automatic failover across an ordered provider list on retryable errors.
- Zero caller changes: the composite satisfies `ChatClient` and conditionally
  `StreamingChatClient` / `PersistentChatClient`.
- Preserve tool-calling (ReAct), streaming, and persistence semantics across a
  failover boundary, with explicit, documented behaviour for mid-stream and
  mid-conversation failover.
- A clear, auditable classification of which errors trigger failover, with all
  logging routed through `pkg/redact`.
- Config surface: an ordered provider list, opt-in, with sane defaults.

### Non-Goals

- **Mid-call** failover that resumes a partially-streamed or partially-tool-called
  turn on a new provider (see Design Decision "Failover granularity").
- Cross-provider *load balancing* / weighted distribution.
- Translating provider-specific message history from one provider's wire format
  into another's (see "Conversation state across providers").
- Automatic retry against the *same* provider with backoff — that is a separate,
  complementary concern (noted under Future Considerations); this spec is about
  crossing to a *different* provider.

---

## Anchor Files

| Path / Type | Relevance |
|-------------|-----------|
| `pkg/chat/client.go` — `ChatClient`, `Config`, `Provider`, `ProviderFactory`, `New`, `RegisterProvider` | The interface the composite must satisfy; the factory/registry it composes over. |
| `pkg/chat/streaming.go` — `StreamingChatClient`, `StreamEvent`, `StreamCallback` | Streaming semantics the composite must conditionally expose. |
| `pkg/chat/persistence.go` — `PersistentChatClient`, `Snapshot` | Persistence semantics; `Snapshot.Provider` pins a snapshot to one provider — central to the conversation-state analysis. |
| `pkg/chat/usage.go` — `Usage`, `usageTracker` | The composite must aggregate `Usage()` across the providers it has driven. |
| `pkg/chat/claude.go`, `openai.go`, `gemini.go`, `claude_local.go` | Concrete clients; each holds provider-specific SDK message state and surfaces SDK-typed errors carrying HTTP status. |
| `pkg/setup/ai/ai.go` — `RunAIInit`, credential/provider resolution | Where a single provider+credentials are resolved today; the fallback config surface plugs in here. |
| `pkg/redact` — `redact.String` | Required for any error/endpoint detail written to logs. |

---

## Design Decisions

### A composite that wraps `ChatClient`, not a new provider

The fallback lives **above** the provider registry, not inside it. `NewFallback`
accepts already-constructed `ChatClient` values (or a slice of `Config` it
constructs via `chat.New`). This keeps each concrete provider unaware of
fallback, preserves the registry's single-responsibility, and lets the composite
be wrapped around any future provider for free.

```go
// NewFallback builds a composite ChatClient that tries clients in order,
// advancing to the next on a retryable failure. The first client is the
// primary; the rest are fallbacks. At least one client is required.
func NewFallback(clients []ChatClient, opts ...FallbackOption) (ChatClient, error)

// NewFallbackFromConfigs constructs each provider via chat.New and wraps the
// result. The first Config is primary. A construction failure for a non-primary
// provider is downgraded to a logged warning (that provider is dropped) so a
// single missing fallback credential does not break the whole client; if the
// primary fails to construct, the error is returned.
func NewFallbackFromConfigs(ctx context.Context, p *props.Props, cfgs []Config, opts ...FallbackOption) (ChatClient, error)
```

### Failover granularity: per *call*, not mid-call

Failover triggers at the boundary of a **single public call** (`Chat`, `Ask`,
`StreamChat`, `Add`). When the active client returns a retryable error, the
composite re-issues *the same call* against the next client.

We deliberately do **not** attempt to resume a half-finished ReAct loop or a
partially-emitted stream on a new provider. The reasons are load-bearing:

- A ReAct turn may have already executed tools with side effects; replaying it on
  another provider could double-execute them.
- Provider message wire formats differ (Claude content blocks vs OpenAI message
  params vs Gemini `Content`), so there is no faithful mid-turn handoff.

Consequence: failover is clean only for a call that has **not yet emitted
externally-visible effects**. For `StreamChat`, this means failover is honoured
**only before the first `EventTextDelta`/`EventToolCallStart` is delivered to the
callback** (see "Streaming").

### Which errors trigger failover

A pluggable `FailoverPolicy` decides, per error, whether to advance:

```go
// FailoverDecision is the outcome of classifying a provider error.
type FailoverDecision int

const (
    // FailoverFatal — do not advance; return the error to the caller.
    FailoverFatal FailoverDecision = iota
    // FailoverNext — the active provider failed transiently or is
    // unavailable; advance to the next provider.
    FailoverNext
)

// FailoverPolicy classifies a provider error. It MUST NOT inspect or log
// the error's message directly; callers route any logged detail through redact.
type FailoverPolicy interface {
    Classify(err error) FailoverDecision
}
```

The **default policy** (`DefaultFailoverPolicy`) advances on:

| Condition | Detection |
|-----------|-----------|
| HTTP 429 (rate limit / quota) | SDK-typed status: `anthropic.Error`, `openai.Error`, Gemini `apierror.APIError` carry a status code. Unwrap via `errors.As`. |
| HTTP 500/502/503/504 (provider outage) | Same status extraction. |
| HTTP 408 / request timeout | Status, plus `errors.Is(err, context.DeadlineExceeded)` *only when the call context itself is not done* (a caller-cancelled context is fatal — see below). |
| Network unreachable: DNS failure, connection refused/reset, TLS handshake failure | `errors.As(err, *net.OpError)`, `*net.DNSError`, `os.ErrDeadlineExceeded`. |

The default policy treats as **fatal** (no failover):

| Condition | Rationale |
|-----------|-----------|
| HTTP 400 / 422 (bad request, schema violation) | A malformed request will fail identically on every provider. |
| HTTP 401 / 403 (auth) | A credential problem is operator-fixable, not transient; failing fast surfaces it. |
| HTTP 404 (unknown model) | Provider/model mismatch is a config error. |
| `context.Canceled` / caller-cancelled context | The caller asked to stop; respect it. |
| Tool-handler errors | These are application logic, already folded into conversation content by `toolResultOrError`; they never surface as a provider error. |

Status-code extraction lives in one helper (`providerHTTPStatus(err error) (int, bool)`)
that knows each SDK's error type, so the default policy is a simple status →
decision table. `errors.As` is used rather than message matching, so the
`errors.Wrap` layers added in the providers are transparent.

> **Open question OQ-1 (resolve before implementation):** ClaudeLocal wraps a
> CLI subprocess; its failures surface as `exec.ExitError` with stderr, not HTTP
> status. Should the default policy treat a ClaudeLocal non-zero exit as
> retryable (advance to a network provider) or fatal? Proposed default: **fatal**,
> because a local CLI failure (missing binary, bad auth) is operator-fixable and
> not a transient remote outage — but a "claude binary not found" arguably *should*
> fall through to a remote provider. Needs a decision.

### Conversation state across providers

This is the subtle core of the design. Each concrete client stores history in its
**own provider-specific format** (`a.params.Messages` of `openai.…MessageParamUnion`,
Claude content blocks, Gemini `[]*genai.Content`). `Snapshot.Provider` pins a
snapshot to exactly one provider, and `Restore` rejects a mismatched provider.
There is therefore **no faithful, lossless transfer** of an in-progress
provider-native history to a different provider.

The composite resolves this by keeping a **provider-neutral transcript** of the
*inputs it has been given* — the ordered list of `Add`/`Ask`/`Chat` prompts and
the assistant text replies it returned to the caller — and **replaying that
transcript into a fallback client** when failover first crosses to it:

```
composite.transcript = [
  {role: user,      text: "..."},   // from Add / first arg of Chat/Ask
  {role: assistant, text: "..."},   // the string the composite returned
  ...
]
```

When the composite advances to provider *N* for the first time, it replays the
transcript into provider *N* via that client's own `Add` (for user turns) so the
new provider starts from an equivalent plain-text context. This is **lossy by
design**: tool-call/tool-result interleaving and provider-native reasoning blocks
are *not* reconstructed — only the user/assistant text turns are. This is
documented as a known limitation; a conversation that has done heavy tool use
before failover will resume with reduced context on the new provider.

- **Tools** carry across cleanly: the composite owns the `[]Tool` set via
  `SetTools` and re-applies it to whichever client becomes active (tool handlers
  are live functions, provider-agnostic).
- **System prompt / model**: each underlying `Config` carries its own
  `SystemPrompt`/`Model`. The composite does not rewrite them; provider *N* uses
  provider *N*'s configured model.

> **Open question OQ-2 (resolve before implementation):** Is plain-text transcript
> replay acceptable as v1, or must failover be *disabled* once a tool call has
> executed in the current conversation (fail fast rather than silently lose tool
> context)? Proposed default: replay text-only **and** emit a single WARN-level
> log noting reduced context, with a `FallbackOption` (`WithStrictToolContext`)
> to switch to fail-fast.

### Streaming

The composite implements `StreamingChatClient` **iff every** underlying client
does (checked once at construction via type assertion; if any client is not a
`StreamingChatClient`, the composite does not advertise streaming and a
`StreamChat` call on it is a compile-time non-method — exactly as today for a
non-streaming provider).

`StreamChat` buffers nothing, but tracks whether **any externally-visible event**
(`EventTextDelta`, `EventToolCallStart`) has been forwarded to the caller's
callback. Failover is permitted only **before** the first such event:

- Active client errors *before* first visible event → swallow, advance, restart
  `StreamChat` on the next client. The caller's callback has seen nothing yet.
- Active client errors *after* first visible event → the error is delivered as a
  terminal `EventError` and returned; **no** failover (we cannot un-emit deltas).

ClaudeLocal is not a `StreamingChatClient`, so a fallback list that includes it
cannot be a streaming composite — documented, and `NewFallback` logs at INFO
which capability tier the composite resolved to.

### Usage aggregation

The composite embeds a `usageTracker` and returns the **sum of `Usage()` across
every underlying client it has driven**, so a caller reading `Usage()` after a
failover sees the combined token spend (primary's failed-but-counted tokens, if
the SDK reported any, plus the fallback's). `Config.UsageObserver` on each
underlying `Config` continues to fire per round-trip as today; the composite does
not intercept it.

---

## Public API Changes

New, additive — no breaking change to existing types:

```go
// pkg/chat/fallback.go (new)

func NewFallback(clients []ChatClient, opts ...FallbackOption) (ChatClient, error)
func NewFallbackFromConfigs(ctx context.Context, p *props.Props, cfgs []Config, opts ...FallbackOption) (ChatClient, error)

type FallbackOption func(*fallbackConfig)
func WithFailoverPolicy(policy FailoverPolicy) FallbackOption
func WithStrictToolContext() FallbackOption          // see OQ-2
func WithOnFailover(func(from, to Provider))  FallbackOption // observability hook

type FailoverPolicy interface{ Classify(err error) FailoverDecision }
type FailoverDecision int
const ( FailoverFatal FailoverDecision = iota; FailoverNext )

var DefaultFailoverPolicy FailoverPolicy

// providerHTTPStatus(err error) (int, bool) — unexported helper.
```

Per the pre-1.0 stability stance in `CLAUDE.md`, even were a break required it
would ship as a minor; here nothing existing changes, so this is purely additive.

---

## Config Surface

Failover is **opt-in**. A new ordered list under `ai`:

```yaml
ai:
  provider: claude          # unchanged: the single-provider default
  fallback:
    enabled: false          # default off — single-provider behaviour preserved
    providers:              # ordered; index 0 is primary
      - claude
      - openai
      - gemini
```

- `ConfigKeyAIFallbackEnabled = "ai.fallback.enabled"`
- `ConfigKeyAIFallbackProviders = "ai.fallback.providers"` (a `[]string` of
  `Provider` names)

Resolution: when `ai.fallback.enabled` is true and `ai.fallback.providers` is
non-empty, the client factory used by `pkg/setup/ai` and the root command builds
a `Config` per listed provider (reusing the existing per-provider credential
resolution in `pkg/setup/ai/ai.go` and `getXCredentials` in each provider) and
calls `NewFallbackFromConfigs`. When disabled, behaviour is byte-for-byte
today's single-provider path.

Each listed provider still resolves its credentials through the existing
precedence (env-ref → env → keychain → literal → well-known fallback). A provider
whose credentials are absent is dropped from the fallback chain with a WARN (its
endpoint host only, via `redact`) — except the primary, whose absence is fatal.

> **Open question OQ-3 (resolve before implementation):** Should `ai.provider`
> and `ai.fallback.providers[0]` be required to agree, or does enabling fallback
> let `providers[0]` override `ai.provider`? Proposed: `providers[0]` wins when
> fallback is enabled; if `ai.provider` is set and disagrees, emit a WARN.

---

## Logging & Redaction

Every failover transition logs **one** structured line at WARN:

```
"chat provider failover"
  from=<provider>          // enum name, safe
  to=<provider>            // enum name, safe
  reason=<status|network>  // coarse class from the policy, never the raw message
```

The triggering error's message is **never** logged verbatim. Where any
provider-supplied detail (an error string, an endpoint) must appear, it is passed
through `redact.String` first (consistent with the chat client's existing
"endpoint host only at INFO" rule in `client.go`). The `FailoverPolicy.Classify`
contract explicitly forbids the policy from logging the error itself.

---

## Project Structure

```
pkg/chat/
├── fallback.go            ← NEW: composite client, NewFallback(FromConfigs), options
├── fallback_policy.go     ← NEW: FailoverPolicy, DefaultFailoverPolicy, providerHTTPStatus
├── fallback_test.go       ← NEW: composite behaviour (table-driven, t.Parallel)
├── fallback_policy_test.go← NEW: classification matrix per SDK error type
├── client.go              ← MODIFIED: add ConfigKeyAIFallback* constants
pkg/setup/ai/
├── ai.go                  ← MODIFIED: build a fallback chain when ai.fallback.enabled
docs/components/
├── chat.md                ← MODIFIED: new "Cross-provider fallback" section
features/
├── chat_fallback.feature  ← NEW (Gherkin): failover scenarios (see Testing)
```

---

## Testing Strategy

Unit (table-driven, `t.Parallel()`, fakes — no live providers):

| Test | Scenario |
|------|----------|
| `TestFallback_PrimarySucceeds` | Primary returns; fallbacks never called. |
| `TestFallback_AdvancesOn429` | Primary returns 429-typed error → second client used; result from second. |
| `TestFallback_AdvancesOnNetworkError` | `*net.OpError` from primary → advance. |
| `TestFallback_FatalAuthError` | 401 from primary → error returned, no advance. |
| `TestFallback_AllExhausted` | Every client retryable-fails → aggregate error (`errors.Join`) returned. |
| `TestFallback_TranscriptReplayedOnFailover` | `Add`+`Chat` then primary fails → second client receives replayed user turns. |
| `TestFallback_ToolsReapplied` | `SetTools` then failover → second client has the tools. |
| `TestFallback_UsageAggregated` | Usage summed across driven clients. |
| `TestFallback_StreamFailoverBeforeFirstDelta` | Stream errors pre-delta → restarted on next client. |
| `TestFallback_StreamNoFailoverAfterFirstDelta` | Stream errors post-delta → terminal `EventError`, no advance. |
| `TestFallback_NotStreamingWhenAnyClientIsnt` | Composite with ClaudeLocal does not assert as `StreamingChatClient`. |
| `TestDefaultPolicy_Classify_*` | One case per status/network/fatal row in the policy table, using real `anthropic.Error`/`openai.Error`/Gemini error values. |
| `TestNewFallbackFromConfigs_DropsMissingFallbackCred` | Missing non-primary credential → dropped + WARN; missing primary → error. |

E2E BDD (`features/chat_fallback.feature`, gated `INT_TEST_E2E`): a scenario where
the primary endpoint (an `httptest.Server`, `AllowInsecureBaseURL` via a test
OpenAI-compatible config) returns 503 and a second endpoint returns a completion —
asserting the caller receives the second's answer and exactly one failover WARN
was logged. Aligns with the CLAUDE.md requirement that user-facing workflow
changes carry Gherkin scenarios.

Coverage target: **≥90%** for `pkg/chat/fallback*.go` per the `pkg/` policy;
the policy classifier should reach ~100% (small, critical).

---

## Linting

- `golangci-lint run` clean; no new `nolint`.
- `providerHTTPStatus` `errors.As` chain may trip `exhaustive`/`cyclop`; resolve
  by structuring it as a small ordered list of `errors.As` attempts, not a switch.

---

## Documentation

- `docs/components/chat.md` — new "Cross-provider fallback & routing" section:
  the composite, the default policy table, the **lossy transcript-replay**
  limitation, the streaming-before-first-delta rule, and the config surface.
- Cross-reference `pkg/components/redact.md` for the logging contract.

---

## Backwards Compatibility

- Fully additive. With `ai.fallback.enabled` absent/false, `chat.New` and
  `pkg/setup/ai` behave exactly as today.
- The composite is a strict superset: it *is* a `ChatClient`, and conditionally a
  `StreamingChatClient`/`PersistentChatClient`, so existing call sites compile and
  run unchanged whether or not they are handed a fallback client.

---

## Implementation Phases

### Phase 1 — Policy & status extraction
1. `fallback_policy.go`: `FailoverPolicy`, `FailoverDecision`, `DefaultFailoverPolicy`, `providerHTTPStatus`.
2. Full classification matrix tests against real SDK error types.

### Phase 2 — Composite client (non-streaming)
1. `fallback.go`: `NewFallback`, the transcript, `Add`/`Ask`/`Chat`/`SetTools`/`Usage`, failover loop, transcript replay, tool re-application.
2. WARN-on-transition logging via `redact`. `WithFailoverPolicy`/`WithOnFailover`/`WithStrictToolContext`.

### Phase 3 — Streaming + config wiring
1. Conditional `StreamingChatClient` with the before/after-first-event rule.
2. `NewFallbackFromConfigs`; `ai.fallback.*` config keys; `pkg/setup/ai` wiring.
3. `docs/components/chat.md`; Gherkin feature.

### Phase 4 — Routing (future, separate spec)
Initial-provider selection by policy (cost/latency/capability) over the same
composite — explicitly out of scope here, recorded so the type is designed not to
preclude it.

---

## Verification

```bash
go build ./...
go test -race ./pkg/chat/... ./pkg/setup/ai/...
golangci-lint run

# Composite satisfies the interfaces (compile-time assertions in fallback.go):
grep -n "var _ ChatClient = " pkg/chat/fallback.go

# No raw error message reaches a log call in the failover path:
grep -n "err.Error()\|%v\|%w" pkg/chat/fallback.go   # expect none in log calls

just test-e2e-smoke   # if the fallback feature carries the smoke BDD tag
```

---

## Open Questions (resolve before implementation)

1. **OQ-1** — ClaudeLocal non-zero exit: retryable (advance) or fatal? (Proposed: fatal.)
2. **OQ-2** — Post-tool-call failover: lossy text replay + WARN, or fail-fast? (Proposed: lossy replay, with `WithStrictToolContext` opt-out.)
3. **OQ-3** — `ai.provider` vs `ai.fallback.providers[0]` precedence. (Proposed: list[0] wins, WARN on disagreement.)
4. **OQ-4** — Should `Usage()` include tokens from a *failed* primary round-trip if the SDK still reported them, or only successful calls? (Proposed: include whatever the SDK reported, since they were billed.)

## Resolutions (open questions confirmed with user 2026-06-21)

1. **OQ-1 ClaudeLocal exit** — RESOLVED: **fatal, do not advance.** A non-zero
   exit from the local Claude CLI signals misconfig/auth/missing-binary, not a
   transient remote failure; surface it rather than masking it with a failover.
2. **OQ-2 Post-tool-call failover** — RESOLVED: **lossy text replay + WARN**, with
   a `WithStrictToolContext` opt-out that forces fail-fast for callers who cannot
   tolerate a lossy tool-context handoff.
3. **OQ-3 Precedence** — RESOLVED: **`ai.fallback.providers[0]` wins** when fallback
   is enabled; WARN on disagreement with a stale `ai.provider`.
4. **OQ-4 Usage accounting** — RESOLVED: **include whatever the SDK reported**,
   including tokens from a failed primary round-trip — they were billed, so
   `Usage()` should reflect real spend across all attempts.
