---
title: "chat family: provider-conformance suite and batched contract fixes"
description: "Batches the MEDIUM/LOW findings from the 2026-07-23 architectural review of the chat module family — Ask semantics, timeout honouring, stream lifecycle, fallback credential handling, config and registry hygiene — around the review's durable recommendation: a shared provider-conformance test suite in the go/chat core that every provider module runs in CI, with the individual fixes hanging off it."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - chat
  - conformance
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# chat family: provider-conformance suite and batched contract fixes

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (chat-family section — all MEDIUM/LOW findings),
    [chat-gemini error-chain preservation](2026-07-23-chat-gemini-error-chain-preservation.md) (sibling HIGH fix, excluded here),
    [chat-openai request isolation](2026-07-23-chat-openai-request-isolation.md) (sibling HIGH fix, excluded here)

---

## 1. Context

The chat family — `go/chat` core plus `chat-anthropic`, `chat-openai`,
`chat-gemini` — promises that providers are drop-in interchangeable behind
`ChatClient`. The architectural review confirms the security invariants hold,
but surfaces ten MEDIUM/LOW findings that share one shape: each provider
quietly interprets the contract its own way. `Ask` retains history on two
providers and forgets it on the third; `RequestTimeout` bounds every HTTP
provider but not the claude-local subprocess; SSE streams leak on the
documented cancellation path; the fallback composite strips the caller's own
credentials. None is individually severe, but together they mean code written
against one provider breaks on another — the exact failure the family exists
to prevent.

Fixing each finding in isolation leaves the class open: nothing stops the next
provider release from re-diverging. The durable fix, recommended twice in the
review, is a **provider-conformance test suite** owned by the core and run by
every provider module's CI. This spec makes that suite the centrepiece and
batches the individual fixes as the first behaviours it locks in. The two HIGH
findings (Gemini error-chain flattening, OpenAI shared-params bleed) are
covered by the sibling specs above and are out of scope here, though both feed
cases into the suite.

## 2. Provider-conformance suite

Add an exported test-helper package to the core, `chat/conformancetest`
(imported only from `_test.go` files, like `net/http/httptest`). Its entry
point takes a factory for the provider under test plus a capability
descriptor, and runs a table of contract cases against it:

```go
package conformancetest

// Run executes the ChatClient contract suite against clients produced by
// newClient. Capabilities gates cases a provider legitimately cannot satisfy
// (e.g. claude-local has no SetTools yet).
func Run(t *testing.T, newClient func(t *testing.T, cfg chat.Config) chat.ChatClient, caps Capabilities)
```

Contract cases (initial set, each traceable to a review finding):

1. **Ask semantics** — schema-less `Ask` into `*string` returns raw text;
   schema-bound `Ask` round-trips the structured target; the answer joins
   history (`Ask(); Chat("summarise your last answer")` works); tools set via
   `SetTools` do not leak into `Ask` (§3.4).
2. **History retention** — `Add` buffers survive a failed call; `Chat` appends
   both turns; history size is observable (§3.2, §3.4).
3. **Timeout honouring** — with `RequestTimeout` set and a hanging backend
   (stub server / stub subprocess), `Chat` and `Ask` return within the bound
   (§3.2).
4. **Stream lifecycle** — a streaming callback returning an error closes the
   underlying stream promptly; verified with an instrumented backend that
   observes body close (§3.1).
5. **Error-chain preservation** — the error from `Chat`/`StreamChat`/`Ask` for
   a synthetic provider HTTP error satisfies the module's registered status
   extractor (the case the sibling Gemini spec asks for; generalised here).

The suite drives each provider against local stubs (httptest servers, fake
subprocess via the existing `ExecCommand` seam) — no live API calls, so it
runs unconditionally in CI, not behind `INT_TEST_*` gates. Each provider
module adds one `conformance_test.go` invoking `conformancetest.Run`; the core
runs it against `claude-local` and the fallback composite.

## 3. Items

### 3.1 Concurrency & resource safety

**3.1.1 Fallback `toolInvoked` data race under ParallelTools — MEDIUM.**
`chat/fallback.go:497-515` (`instrumentTools`) wraps every tool handler with an
unsynchronised `f.toolInvoked = true`; with `Config.ParallelTools`, handlers
run in bare goroutines (`chat/tools.go:45-50`), so two tool calls in one ReAct
step race on the bool. Any consumer using the composite with parallel tools
fails `just test-race` intermittently. Direction: make `toolInvoked` an
`atomic.Bool`. *Acceptance: race detector clean with a two-parallel-tool
composite test.*

**3.1.2 SSE streams never closed on early exit — MEDIUM.**
Both SDK stream types expose `Close()`, and neither provider calls it: a
callback error exits the `stream.Next()` loop in
`chat-anthropic/claude.go:523-560` and `chat-openai/openai.go:443-481` with the
response body left open — one leaked connection per cancel via the documented
mechanism (`chat/streaming.go:48`). Direction: `defer stream.Close()`
immediately after creating each stream. *Acceptance: conformance case 4 passes
in both provider modules.*

### 3.2 claude-local contract

**3.2.1 Subprocess ignores `RequestTimeout` — MEDIUM.**
`chat/claude_local.go:220-234` (`runClaude`) invokes the subprocess bounded
only by the caller's context; `Config.RequestTimeout` and the family-wide
`DefaultChatRequestTimeout` are never applied, so a wedged `claude` binary
hangs `Chat`/`Ask` forever under `context.Background()` (the common CLI case).
Direction: derive `context.WithTimeout(ctx, resolveChatTimeout(c.cfg))` around
the invocation. *Acceptance: conformance case 3 passes for claude-local with a
stub subprocess that sleeps past the timeout.*

**3.2.2 Schema-less `Ask` violates the raw-text contract — MEDIUM.**
`chat/claude_local.go:110` unconditionally `json.Unmarshal`s the raw CLI text,
so plain prose into a `*string` target fails with `invalid character` — while
the API-based provider honours the contract via `assignText`
(`chat-anthropic/claude.go:271-290`). Code that works on `claude` breaks on
`claude-local`. Direction: port `assignText` into the core and share it from
both. *Acceptance: conformance case 1 (schema-less branch) passes for
claude-local.*

**3.2.3 Buffered `Add()` context dropped on failure — LOW.**
`buildPrompt` (`chat/claude_local.go:152-161`) clears `c.pending` before the
subprocess runs; on failure the buffered turns are gone and a retry sends only
the bare prompt, unlike the HTTP providers which keep history. Direction: clear
`pending` only after a successful run. *Acceptance: conformance case 2
(failed-call branch) passes for claude-local.*

### 3.3 Fallback composite

**3.3.1 Module-level fallback constructors strip the caller's credentials —
MEDIUM.** `chat/fallback.go:227-236` (`perProviderConfig`) clears `Token`,
`Credentials`, `Model`, `BaseURL` for every provider in the chain — including
the primary — so a standalone module user's explicitly supplied token is
discarded and only well-known env vars can satisfy construction. GTB escapes
only because its adapter re-resolves per provider
(`pkg/chat/config_adapter.go:97-105`); the module has no equivalent hook.
Direction: add a per-provider credential-resolution hook to `FallbackSettings`
(mirroring the GTB adapter), retain the caller's `Token`/`Credentials` for the
provider matching the base `Config.Provider`, and convert the field denylist
to an allowlist of what carries over. *Acceptance: `NewWithFallbackSettings`
with an explicit primary token constructs without env vars; an allowlist test
proves new `Config` fields do not leak across providers by default.*

### 3.4 Ask contract & history

**3.4.1 `Ask` semantics diverge across providers — MEDIUM.**
History: OpenAI appends the answer (`openai.go:255`), Gemini persists the
session (`gemini.go:247`), Claude forgets it (`claude.go:180-202`), leaving a
dangling user turn. Tools: Claude excludes them from `Ask`, OpenAI sends them
(`openai.go:244`) risking `tool_calls` with empty content, Gemini combines
carried tools with forced JSON output (`gemini.go:223-231`). Direction: adopt
one contract — **Ask is a tool-free structured call whose answer joins
history** — document it on `ChatClient`, and enforce it in all three modules
via conformance case 1. *Acceptance: conformance case 1 passes in all three
provider modules and claude-local.*

**3.4.2 No conversation-history bounding — MEDIUM.**
Every provider appends forever (`claude.go:174`, `openai.go:220`,
`gemini.go:207`) and re-sends full history each ReAct step; a long-lived agent
loop degrades monotonically until a context-overflow 400, which the failover
policy correctly-but-unhelpfully classifies as fatal. Direction: minimum
deliverable is observability — expose history length and a token estimate on
`ChatClient` (or a core helper) so callers can decide when to reset; a
pluggable truncation/summarisation policy is a design question (§5).
*Acceptance: history size/estimate is queryable on every provider and asserted
by conformance case 2.*

### 3.5 Config & registry hygiene

**3.5.1 Provider-specific knobs leak into core `Config` — LOW.**
`chat/client.go:132-142` carries `ExecLookPath`/`ExecCommand` (claude-local),
`Seed` (OpenAI-only), and `GenaiNewClient any` (Gemini, runtime-type-asserted
at `gemini.go:82-91`). Direction: move per-provider overrides into per-module
option types resolved at construction (typed context key or registration-time
option); keep `Config` to cross-provider fields. *Acceptance: core `Config`
contains no field usable by only one provider; misuse fails at compile time in
the owning module.*

**3.5.2 `AllowInsecureBaseURL` hardening is `encoding/json`-specific — LOW.**
`chat/client.go:157` tags the field `json:"-"`, but mapstructure and YAML
decoders ignore JSON tags, so any host decoding `chat.Config` wholesale from
config could enable it. GTB is safe by construction
(`pkg/chat/config_adapter.go:178-208`). Direction: add `mapstructure:"-"
yaml:"-"` tags and document that hosts must not decode `Config` directly from
untrusted config. *Acceptance: a decode test via mapstructure and yaml confirms
the field cannot be set from config input.*

**3.5.3 OpenAI module miscellany — LOW.** Three small divergences in
`chat-openai`: media validated as `chat.ProviderOpenAI` even for
`openai-compatible` backends (`openai.go:193,233`), bypassing the core's
capability map; an unconditional empty system message (`openai.go:108-110`)
that strict compatible backends may reject; and `Add`'s `chunkByTokens` using
an output-token constant to split a prompt that is sent whole anyway
(`openai.go:205`). Direction: validate media against the configured provider,
seed the system prompt only when non-empty (matching Claude/Gemini), and
delete the chunking (dropping the tokenizer dependency if now unused).
*Acceptance: openai-compatible media rejection test, no empty system turn in
captured requests, one user message per `Add`.*

**3.5.4 Registry overwrites duplicates silently; version coupling is
convention-only — LOW.** `RegisterProvider` (`chat/client.go:185-193`) lets the
last blank import win with no signal, and the matching-minors contract has no
runtime assertion — a behavioural mismatch surfaces only in production.
Direction: log (or reject) duplicate registrations; the conformance suite
itself becomes the behavioural-contract enforcement. A `chat.CoreAPIVersion`
handshake asserted at registration is worth considering, but version-coupling
mechanics across the toolkit belong to the ecosystem fleet spec — coordinate
there rather than inventing a chat-only scheme. *Acceptance: duplicate
registration produces a visible signal covered by a core unit test.*

## 4. Scope & release plan

1. **`go/chat` core first** — the `conformancetest` package plus the core-side
   fixes: 3.1.1 (atomic bool), 3.2.1/3.2.2/3.2.3 (claude-local), 3.3.1
   (fallback credential hook), 3.4.2 (history observability), 3.5.1/3.5.2
   (config hygiene), 3.5.4 (registry). One minor release (`feat(chat):`);
   3.5.1 and 3.3.1 are breaking-ish for a v0 module — clean break per the
   toolkit's pre-1.0 policy, with migration notes in the module docs.
2. **Provider modules adopt in lockstep minors** — `chat-anthropic`,
   `chat-openai`, `chat-gemini` each bump the core dependency, add
   `conformance_test.go`, and land their local fixes (3.1.2 stream close,
   3.4.1 Ask contract, 3.5.3 OpenAI misc). Per the family's matching-minors
   convention, all three release together against the new core minor.
3. **GTB pickup** — routine `go.mod` bumps of all four modules. Expected GTB
   code impact is nil-to-minimal (`pkg/chat/config_adapter.go` may simplify if
   the fallback credential hook subsumes its per-provider re-resolution).
   Verify with `just ci` and the chat-tagged integration tests.

## 5. Open questions

1. **Which Ask contract wins?** §3.4.1 proposes "tool-free, answer joins
   history" (matches OpenAI/Gemini on history, Claude on tools). Confirm this
   is the semantics GTB's callers want before the suite freezes it — changing
   the contract after three modules enforce it is a four-repo change.
2. **History-bounding policy shape.** Is observability (size + token estimate)
   enough for v1 of §3.4.2, with truncation left to callers? If a pluggable
   policy belongs in the core, does it truncate oldest-first, summarise via the
   provider, or both — and does the fallback composite's transcript share it?
3. **Where do provider-specific knobs live?** §3.5.1 needs a concrete
   mechanism: per-module `Options` structs passed via `Settings`, a typed
   context key, or registration-time functional options. Pick before the core
   release since it shapes the `Config` break and any GTB adapter wiring.
