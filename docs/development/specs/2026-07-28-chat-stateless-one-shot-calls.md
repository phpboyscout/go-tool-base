---
title: "chat: a stateless one-shot mode so batch work stops paying quadratic history costs"
description: "ChatClient accumulates conversation history across calls, so a caller doing N independent Ask calls on one client re-sends the whole prefix each time — quadratic input cost and silent cross-call contamination of independent classifications. Adds Config.Stateless, under which history is neither read nor appended across calls while the intra-call ReAct loop keeps the turns it needs; guarantees stateless provider clients are goroutine-safe so a batch caller can use a worker pool; guards the flag with a capability marker so a provider module that predates it fails loudly rather than silently billing; and fixes the Anthropic Ask history asymmetry found while verifying the report."
date: 2026-07-28
status: IMPLEMENTED
tags:
  - specification
  - chat
  - cost
  - concurrency
  - conformance
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 5
    role: AI drafting assistant
---

# chat: a stateless one-shot mode so batch work stops paying quadratic history costs

Authors
:   Matt Cockayne, Claude Opus 5 *(AI drafting assistant)*

Date
:   2026-07-28

Status
:   IMPLEMENTED (2026-07-28). Shipped in go/chat v0.2.0, chat-anthropic v0.1.3 (D10) and v0.2.0, chat-openai v0.2.0, chat-gemini v0.2.0. All seven open questions were resolved by the maintainer on 2026-07-28; see §6.

Related
:   [chat/#2](https://gitlab.com/phpboyscout/go/chat/-/work_items/2) (the originating report),
    [chat family provider-conformance suite](2026-07-23-chat-provider-conformance.md) (the suite this spec's conformance cases belong in),
    [chat-openai request isolation](2026-07-23-chat-openai-request-isolation.md) (prior per-call state-isolation fix; establishes the per-call-copy pattern reused here),
    [chat module extraction](2026-07-05-chat-module-extraction.md) (the core + provider-module split this change has to cross)

---

## 1. Problem

`ChatClient` is a *conversation*. Every provider implementation keeps a message
list on the client and seeds each request with everything that came before it.
That is correct for a chat session and wrong for batch work, where each call is
independent — and there is currently no way to ask for the second behaviour
short of constructing a new client per call.

The cost grows quadratically in the number of calls, and nothing surfaces it
until a bill arrives.

### 1.1 The behaviour, per provider

Confirmed against `origin/main` of each module on 2026-07-28:

| Provider | State | `Ask` appends | `Chat` appends |
|---|---|---|---|
| `claude` (`chat-anthropic/claude.go`) | `c.messages []anthropic.MessageParam` (`:50`) | the **question only** (`:190`) — never the answer | question + assistant turn + every tool round (`:350-380`) |
| `openai` (`chat-openai/openai.go`) | `a.params.Messages` (`:166-168`) | question **and** answer (`:250-271`) | both, plus tool turns |
| `gemini` (`chat-gemini/gemini.go`) | `g.history []*genai.Content` (`:46`) | whole session via `persistSessionHistory` (`:247`) | same (`:325`) |
| `claude-local` (`chat/claude_local.go`) | `c.sessionID` (`:25`), replayed with `--resume` (`:175-177`) | history is server-side, unbounded | same |

Two things fall out of that table:

1. **All four accumulate.** Provider choice changes the constant, not the
   asymptote. `chat.New` gives no way to opt out.
2. **They do not agree on what they accumulate.** Anthropic's `Ask` appends the
   user turn and never the assistant turn (`claude.go:190`; `parseAskResponse`
   at `:234-252` does no append). So an N-call `Ask` loop on Claude builds
   `[user, user, user, …]` — a growing prefix of *unanswered questions*. That is
   a conformance defect on its own terms, independent of cost, and it is fixed
   here as D10.

### 1.2 The composite compounds it

`fallbackClient` keeps its own `transcript []string` of user turns
(`chat/fallback.go:64-68`) and appends on every `Add`/`Ask`/`Chat`
(`:319`, `:331`, `:355`) so it can replay context into the next provider on
failover (`ensureReady`, `:468-486`). A batch caller behind a fallback composite
therefore accumulates twice: once in the active provider client, once in the
wrapper. Any fix that only touches the provider modules leaves the composite
growing unbounded.

### 1.3 What it cost

The downstream caller is `scoutdm`'s campaign-canon importer. The shape is
confirmed in `pkg/importer/model_extractor.go`: `Extract` builds one question
per document and issues exactly one `Ask` (`:147-176`), over a corpus of
eighteen independent session transcripts of roughly a hundred kilobytes each
(`:141-146`) — around 375k tokens in total. Held on one client, source 18
re-sends the previous seventeen documents *and* their extraction responses.

The reported arithmetic — `18×19/2 = 171` document-equivalents against 18, a
**9.5× multiplier** — is correct for that shape.

The billing evidence (Gemini 3.6 Flash text SKUs, Google Cloud billing report
for the charge period 1–28 July 2026):

| SKU | Tokens |
|---|---|
| input | 17,628,739 |
| **cached input** | **61,923,684** |
| output | 997,374 |

Read these for what they are: an **account-wide month-to-date total across both
projects and all 23 SKUs**, not the trace of a single run. The same report
carries Imagen 4 and Gemini 2.5 Flash native-image SKUs that have nothing to do
with extraction, and the pipeline was run repeatedly during development. So the
figures do not isolate one run and are not expected to match the ~3.6M tokens
that 9.5× on a 375k corpus predicts for one.

What they do establish, and it is enough:

- **Cached input is 3.5× fresh input.** A workload of genuinely independent
  one-shot calls has almost nothing to cache — there is no repeated prefix. A
  cached-input volume several times the fresh-input volume is only produced by
  an ever-growing prefix being re-sent and re-cached, which is exactly the
  mechanism §1.1 shows in the code.
- **The month-on-month step matches.** Gemini API usage cost runs £8.48 in
  March and £9.70 in April, collapses to £0.02 in May, then jumps to £24.05 in
  June and £46.06 across 1–28 July — the extraction pipeline coming online.

The mechanism is proven by the code in §1.1; the bill corroborates the
signature and the timing.

Two symptoms were misdiagnosed before the cause was found, both explained by
the above:

- It tripped `GenerateContentPaidTierInputTokensPerModelPerMinute` (2M/minute).
  That reads as a pacing problem and led to a between-run cooldown that could
  not have helped, because the growth is *within* a run.
- Run-to-run output varied substantially.

### 1.4 The correctness half

This is not only a cost issue, and that is the stronger argument for fixing it
in the library rather than documenting around it. A caller doing N independent
classifications gets call N conditioned on calls 1..N-1 without having asked
for it. An extractor that can see the previous seventeen sources may re-report
their entities; a judge that can see its own earlier verdicts anchors on them.
That is a measurement hazard, and it is silent.

### 1.5 Discoverability

The godoc does document the behaviour — `ChatClient`'s doc comment
(`chat/client.go:51-53`) says history from `Add()` persists across `Chat()` and
`Ask()`, and `docs/explanation/architecture.md:22` repeats it. But it is framed
as *`Add` history*, not as *every call appends to a growing conversation*, and
`Ask`'s own doc comment (`client.go:60-64`) says nothing about it. Build a
client, call `Ask` in a loop: the natural shape for batch work silently does the
expensive thing.

### 1.6 Why the tests did not catch it

No unit test could have. Every provider test runs against a fake or an
`httptest` backend, and a fake does not bill. The defect is invisible unless a
test asserts on *request size across calls* — which nothing did. This shapes the
acceptance criteria in §4: the primary regression test is a request-growth
assertion, not an output assertion.

## 2. Proposed change

Add an opt-in stateless mode to `chat.Config`, implemented by every provider,
guaranteed goroutine-safe, guarded so it cannot be silently ignored.

### D1 — `Config.Stateless bool`

```go
// Stateless makes every Chat, Ask and StreamChat call a one-shot: the client
// neither seeds the request with prior turns nor retains the turns from this
// call. One client can serve many independent calls with no per-call
// construction cost and no cross-call contamination.
//
// The zero value (false) keeps the conversational behaviour: history
// accumulates across calls on a single client instance.
//
// Within a single call, tool-calling still works normally — the ReAct loop
// keeps the assistant/tool turns it needs to make progress and discards them
// when the call returns.
//
// Stateless provider clients are safe for concurrent use by multiple
// goroutines; see the ChatClient documentation for the exceptions.
Stateless bool
```

Chosen over `Config.History bool` (the report's alternative) because a `bool`
field should default to its zero value meaning "no change", and over
`ChatClient.Reset()` because forgetting `Reset()` is silent and the failure mode
this spec exists to remove is precisely a silent one. The intent is declared at
construction, where the caller already decides what kind of client they want.

### D2 — Stateless means *across* calls, not *within* one

The ReAct loop needs intra-call turns: the assistant `tool_use` block and the
`tool_result` that answers it must both be present in the next request or the
loop cannot proceed. A naive implementation that simply skips the appends breaks
tool calling outright.

Each provider therefore builds a **call-local** message slice in stateless mode:
seeded from the system prompt (and any pending `Add` turns, per D3), appended to
freely for the duration of the call, and discarded on return. This is the same
per-call-copy shape established for OpenAI request params in
[2026-07-23-chat-openai-request-isolation](2026-07-23-chat-openai-request-isolation.md);
here it extends to `Messages` rather than only call-shape fields.

Gemini is nearly free: `Chats.Create` already returns a per-call `*genai.Chat`
session that owns the mutable history (`genai@v1.65.0/chats.go`), so stateless
mode is "create the session from an empty history and do not call
`persistSessionHistory`".

For `claude-local` the equivalent is: do not pass `--resume`, and do not retain
the returned `session_id` (`chat/claude_local.go:106-108`, `:144-146`,
`:175-177`). Each invocation starts a fresh CLI session.

### D3 — `Add` buffers a one-shot preamble; it does not error

In stateless mode `Add` appends to a pending buffer that is consumed by — and
cleared at the end of — the next `Chat`/`Ask`/`StreamChat` call.

- It keeps `ChatClient` **total**: no method becomes conditionally invalid, so a
  stateless client remains substitutable everywhere a `ChatClient` is expected.
  This matters concretely — `fallbackClient.ensureReady` calls `Add` to replay
  context (`fallback.go:484-486`), and an erroring `Add` would break the
  composite.
- It preserves the legitimate use of `Add`: assembling one multi-part request
  (a preamble plus the document) before sending it.
- It is already the semantics `claude-local` implements for `Add` today
  (`buildPrompt`, `claude_local.go:152-161`), so one provider needs no change
  here at all.

The buffer is shared mutable state and is therefore mutex-guarded under D9.

Rejected alternative: `Add` returns an error in stateless mode. Honest, but it
breaks substitutability for no gain over "buffer and consume".

### D4 — The fallback composite honours the flag

`fallbackClient` skips `transcript` accumulation entirely when the config is
stateless (`fallback.go:319`, `:331`, `:355`), and `ensureReady` (`:468-486`)
replays only tools, never turns. Failover in stateless mode is trivially lossless
— there is no cross-call context to lose — so `WithStrictToolContext`
(`fallback.go:35-40`) has nothing to guard and its fail-fast path is inert.

`fallbackProviderConfigs` (`fallback.go:208`) already clones the base config per
provider; `Stateless` must be among the fields it carries through rather than
clears, since it is a caller-intent field, not a provider-specific one.

### D5 — `Save` works, `Restore` errors

`PersistentChatClient` (`chat/persistence.go:25-35`) has no coherent meaning
without retained state.

- `Save` returns a snapshot with an empty `Messages` array — valid, honest, and
  round-trippable.
- `Restore` returns a sentinel error (`ErrStatelessRestore`) rather than
  silently succeeding and discarding the snapshot. Silently throwing away caller
  state is the same class of failure this spec exists to remove.

**Recorded for a future spec, deliberately not built here:** `Restore` could
instead install an *immutable prefix* that seeds every one-shot call and that no
call ever mutates, making "prime a context once, then run N independent calls
against it" expressible. Few-shot batch classification genuinely wants that and
`SystemPrompt` does not cover it. D2 already builds the call-local slice from a
seed, so adding a prefix later is cheap. It is left out now because overloading
"snapshot of a conversation" to mean "reusable prefix" is a muddier API than it
looks, and it would widen this change. Written down so it is not re-derived from
scratch.

### D6 — A capability guard, so the flag cannot be silently ignored

The core and the provider modules version independently. A caller who sets
`Stateless: true` while pinned to a provider module built before this change
gets a field that compiles, is ignored, and bills them exactly as before. That
is the same silent failure this spec exists to remove, so the core must catch it.

Provider clients that implement stateless mode assert a marker interface:

```go
// StatelessCapable is implemented by provider clients that honour
// Config.Stateless. chat.New rejects a stateless Config whose provider does
// not implement it, so the flag can never be silently ignored.
type StatelessCapable interface {
    ChatClient
    // SupportsStateless is a compile-time marker; it is never called.
    SupportsStateless()
}
```

`chat.New` (`client.go:196-224`) type-asserts the constructed client after the
factory returns and, when `cfg.Stateless` is set and the assertion fails,
returns an error naming the provider and the minimum module version. Same
discover-by-assertion pattern as `StreamingChatClient` and
`PersistentChatClient`, so it adds no new idiom.

This is the decision that makes the feature safe to ship across four repos with
independent release cadences, and it is the one most likely to be dropped as
ceremony. It should not be.

### D7 — The default does not change

The report asks whether the default is the right way round. It stays as it is:

- Flipping it is a silent behaviour change for every existing consumer, and the
  failure mode is *lost conversational context* — which surfaces as degraded
  model output, not an error. That is worse than the cost bug it would fix.
- `ChatClient` is named for a conversation. Conversational is the honest default.

The discoverability problem from §1.5 is addressed directly instead (D8), which
is the report's option 3 — worth doing regardless of options 1 and 2.

### D8 — Documentation, as its own deliverable

- `ChatClient.Ask` and `ChatClient.Chat` godoc state that the call appends to
  the conversation and that a client held across independent calls re-sends the
  accumulated prefix, pointing at `Config.Stateless`.
- `ChatClient`'s type-level doc gains the concurrency contract from D9.
- New `docs/how-to/batch-processing.md` in the `chat` repo: the batch shape, the
  quadratic trap with the arithmetic from §1.3, the stateless client that fixes
  it, and the worker-pool shape D9 unlocks.
- `docs/explanation/architecture.md:22` extended to describe both modes.

### D9 — Stateless provider clients are safe for concurrent use

The natural next step after "one client for N documents" is "one client for N
documents across W workers", and today `ChatClient` documents that
implementations are not safe for concurrent use (`client.go:46-53`). Under
stateless mode that restriction is nearly gone already, so the guarantee is made
explicit rather than left for callers to guess at.

**What the audit found.** All three vendor SDK clients hold immutable
configuration after construction and mutate no client state on the request path:
`anthropic.Client` and `openai.Client` are `Options []option.RequestOption` plus
value-type services (`anthropic-sdk-go@v1.56.0/client.go`,
`openai-go/v3@v3.46.0/client.go`); `genai.Client` holds an unexported
`clientConfig` and service pointers, with the mutable history living in the
per-call `Chat` session (`genai@v1.65.0/client.go`, `chats.go`).
`chat.UsageTracker` is already mutex-guarded (`chat/usage.go:77-78`). Under D2
the message state is not touched at all, and `claude-local`'s `sessionID` is
never written. That leaves exactly two pieces of shared mutable state: the D3
`pending` buffer, and the tools registry (`c.tools`/`c.toolParams`,
`a.tools`, `g.tools`/`g.config.Tools`).

**The design.** Each provider client gains a `sync.Mutex` used for short
critical sections only:

- `Add`: lock, append to `pending`, unlock.
- `SetTools`: lock, replace the registry, unlock.
- Call entry: lock, swap `pending` out to a local (leaving it nil) and take a
  reference to the current tools registry, unlock. The request then runs
  entirely on locals.

**The lock is never held across a provider round-trip.** That is the whole
point: a mutex held for the duration of the HTTP call would serialise the batch
and give back nothing. It also means `SetTools` racing a call is safe — the call
either sees the old registry or the new one, never a torn one.

**Excluded: the fallback composite.** `fallbackClient` mutates `f.active` and
`f.readyUpTo` mid-call when it fails over (`fallback.go:407-486`). Guarding
those with a plain mutex would serialise every call through the composite, and
an `RWMutex` fast path buys full parallelism only until the first transition,
at the cost of real complexity in the replay path. So the composite is
documented as **not** safe for concurrent use, stateless or not. A caller who
wants both fallback and a worker pool builds the pool over one composite per
worker. This boundary is stated in `NewFallback`'s godoc, not left implicit.

### D10 — Fix the Anthropic `Ask` history asymmetry

Independent of stateless mode, `chat-anthropic`'s `Ask` appends the question and
never the answer (§1.1), so a conversational `Ask` loop on Claude builds a run of
unanswered user turns — divergent from OpenAI and Gemini, and malformed for a
provider that expects alternating roles.

`Ask` will append the full assistant response blocks and, on the schema path,
the `tool_result` that the forced `submit_response` tool call requires:

```go
c.messages = append(c.messages,
    anthropic.NewAssistantMessage(resContentToBlocks(resp.Content)...))

// Schema path: the response is a tool_use block, and the API requires a
// matching tool_result before the next turn.
if toolUseID != "" {
    c.messages = append(c.messages, anthropic.NewUserMessage(ack(toolUseID)))
}
```

This is the shape the existing ReAct loop already uses (`claude.go:362`, `:380`),
so the file ends up with one history-append idiom rather than two. The no-schema
path appends the text blocks and needs no acknowledgement turn.

**This changes default-path behaviour** for existing Claude consumers: a
conversational `Ask`-then-`Chat` sequence will now carry the answers as well as
the questions, so subsequent calls send more tokens than before. That is the
correct conversation and matches every other provider, but it is a real change
and is called out in the changelog rather than buried in the stateless feature.
See OQ-6 on whether it ships ahead of the stateless work.

## 3. Scope & release plan

Four repositories. The core lands first; providers follow; no provider is
required for the core to be releasable, because D6 makes a missing
implementation a construction-time error rather than a silent miss.

| Repo | Change | Release |
|---|---|---|
| `go/chat` | `Config.Stateless`, `StatelessCapable`, `ErrStatelessRestore`, `New` guard (D6), `fallbackClient` (D4) + concurrency boundary (D9), `claude-local` (D2/D3/D9), conformance cases, docs (D8) | `feat:` minor |
| `go/chat-anthropic` | call-local messages (D2), `Add` buffer (D3), mutex (D9), marker (D6), **`Ask` history symmetry (D10)** | `feat:` minor + `fix:` |
| `go/chat-openai` | call-local `params.Messages` (D2), `Add` buffer (D3), mutex (D9), marker (D6); covers `openai-compatible` | `feat:` minor |
| `go/chat-gemini` | empty-seeded session, no `persistSessionHistory` (D2), `Add` buffer (D3), mutex (D9), marker (D6) | `feat:` minor |

All via releaser-pleaser. The chat family's matching-minors compatibility
contract means the four minors move together; provider modules bump their
`go/chat` requirement to the version carrying the marker interface.

**GTB:** picks this up through a routine module bump. Per the resolved OQ-4,
`Stateless` is **not** exposed as an `ai.*` config key — whether a call site is
batch or conversational is a property of the code, not of the deployment, and a
deployment-level toggle that silently changes whether calls share context would
be a footgun of the same family as the original bug. The GTB adapter sets
`chat.Config.Stateless` at the construction sites that know.

**Downstream:** `scoutdm`'s `NewModelExtractor` builds a throwaway client to
fail fast and then a fresh client per document via a `newClient` closure
(`pkg/importer/model_extractor.go:101-139`), with the accumulation hazard
documented in the comment at `:131-133`. That workaround collapses to a single
stateless client once the Gemini module ships, `newClient` and the test
asserting distinct clients are constructed both go, and the per-document
connection setup goes with them.

## 4. Acceptance criteria

**Cost and contamination**

1. **No cross-call growth.** For each provider, against a recording fake
   backend: ten sequential `Ask` calls on one stateless client produce ten
   requests whose message arrays are all length 1 (or 1 + the system prompt as
   that provider carries it). The same ten calls on a default client produce
   monotonically growing arrays — the regression test that would have caught
   this, and per §1.6 the one assertion nothing was making.
2. **No cross-call contamination.** Request N contains no text from any prompt
   or response of calls 1..N-1.
3. **Tool calling still works stateless.** A multi-step ReAct conversation
   completes on a stateless client: the step-2 request contains the step-1
   assistant `tool_use` and its `tool_result`; the step-1 turns are absent from
   the *next* `Chat` call's request.
4. **`Add` is one-shot.** `Add(a); Add(b); Chat(c)` sends all three parts in one
   request; the immediately following `Chat(d)` sends only `d`.

**Guards and boundaries**

5. **Capability guard.** `chat.New` with `Stateless: true` against a registered
   provider whose client does not implement `StatelessCapable` returns an error
   naming the provider. A test registers a deliberately non-capable provider to
   prove it.
6. **Fallback composite.** A stateless composite accumulates no `transcript`;
   after a forced failover the new provider's first request carries no replayed
   turns and the call still succeeds. Tools are still re-applied.
7. **Persistence.** `Save` on a stateless client returns a snapshot with empty
   `Messages`; `Restore` returns `ErrStatelessRestore`.
8. **`claude-local`.** No `--resume` in the argv of any stateless invocation,
   including the second and subsequent calls on one client.

**Concurrency (D9)**

9. **Race-clean under load.** For each provider: W goroutines issuing `Ask`
   concurrently on one stateless client, under `-race`, with every request
   recorded — all W requests are well-formed, each carries exactly its own
   prompt, and the detector is clean.
10. **`SetTools` and `Add` race the call path.** A goroutine calling `SetTools`
    (and one calling `Add`) concurrently with in-flight `Ask` calls is
    race-clean, and every request carries either the old or the new tool set,
    never a partial one.
11. **The lock does not serialise round-trips.** A test with a deliberately slow
    fake backend shows W concurrent calls completing in roughly the time of one,
    not W — the criterion that catches a mutex accidentally held across the
    request.
12. **The composite is documented, not guaranteed.** `NewFallback`'s godoc
    states the exclusion; no concurrency test asserts composite safety.

**Anthropic conformance (D10)**

13. **`Ask` history symmetry.** After a schema `Ask` on a default (non-stateless)
    Claude client, the message list ends `[…, user question, assistant tool_use,
    user tool_result]`; a second `Ask` on the same client succeeds against a fake
    asserting well-formed role alternation. Today's build produces a run of
    consecutive user turns — the red test.
14. **No-schema `Ask`** appends the assistant text blocks and no acknowledgement
    turn.

**Everything else**

15. **Default unchanged.** The existing test suite passes across all four
    modules; `Stateless: false` behaviour is byte-identical except for the D10
    change, which is covered by criteria 13–14 and called out in the changelog.
16. **Usage accounting.** `Usage()` remains cumulative across calls on a
    stateless client — the point of holding one client for a batch is
    whole-batch accounting.
17. Docs from D8 land with the core release; CI green (tests, race, lint) and
    coverage does not regress in any of the four modules.

## 5. Open questions

None. All seven were resolved during review on 2026-07-28; see §6.

## 6. Resolved

**2026-07-28 — OQ-1: the billing figures are genuine; there was no discrepancy
to reconcile.** The first draft flagged that 9.5× on a 375k corpus predicts
~3.6M input tokens against 79.5M billed. The figures are an account-wide
month-to-date total across both projects and all 23 SKUs for 1–28 July 2026 —
many runs plus unrelated Gemini usage — not the trace of one run. §1.3 was
rewritten to use the bill for what it does establish (the 3.5:1 cached-to-fresh
ratio, which independent one-shot calls cannot produce, and the June/July step
change) and to rest the mechanism claim on the code instead. The `scoutdm`
extractor's shape was confirmed directly in
`pkg/importer/model_extractor.go:101-176`.

**2026-07-28 — OQ-2: `Restore` errors under stateless; the primed-prefix idea is
recorded, not built.** See D5. Keeps the change additive; the prefix API is cheap
to add later because D2 already seeds the call-local slice.

**2026-07-28 — OQ-3: the Anthropic `Ask` asymmetry is fixed here, not deferred to
the conformance spec.** See D10. It appends the full response blocks plus a
`tool_result` acknowledgement on the schema path, matching the existing ReAct
loop's idiom rather than introducing a second one. Accepted consequence: this
spec now changes default-path behaviour on one provider, which the stateless work
alone does not — hence OQ-6 on release sequencing.

**2026-07-28 — OQ-4: `Stateless` stays programmatic; no `ai.*` config key.** See
§3. Batch-versus-conversational is a property of the call site, not the
deployment.

**2026-07-28 — OQ-5: stateless provider clients are guaranteed goroutine-safe;
the fallback composite is explicitly excluded.** See D9. The vendor SDK audit
(pinned versions) found no request-path mutation of client state, leaving only
the `pending` buffer and the tools registry to guard — with the lock deliberately
released before the round-trip. The composite is excluded because guarding
`active`/`readyUpTo` would serialise every call through it; an `RWMutex` fast
path was considered and rejected as complexity in the replay path for a
narrow gain.

**2026-07-28 — OQ-6: D10 ships first, as its own patch.** The Anthropic `Ask`
asymmetry is an independent defect and the one change here that alters
default-path behaviour, so it lands as a standalone `fix:` on `chat-anthropic`
ahead of the stateless minor — separately bisectable, with its own changelog
entry and its own blast radius.

**2026-07-28 — OQ-7: the gated integration test is worth the spend.** Added
under the module's existing `skipIfNotIntegration` convention as
`TestStatelessBilling_InputTokensGrowLinearly` in `chat-gemini`, run with
`INT_TEST_STATELESS=1` and a real key, never in CI. The unit tests assert the
mechanism (one turn per request); this asserts the consequence (the provider
bills one turn per request), which is the claim the spec rests on and the one
thing §1.6 says no fake can establish. The conversational control is gated again
behind `INT_TEST_STATELESS_CONTROL`, because it spends money reproducing the
defect rather than verifying the fix.

---

## 7. Implementation record

Shipped 2026-07-28, in this order:

| Release | Carries |
|---|---|
| `chat-anthropic` **v0.1.3** | D10 alone, as its own patch (OQ-6) |
| `go/chat` **v0.2.0** | D1-D9, the docs, and the `v0.2.x` compatibility row |
| `chat-anthropic` **v0.2.0** | D1-D3, D5, D6, D9 |
| `chat-openai` **v0.2.0** | D1-D3, D5, D6, D9 (covers `openai-compatible`) |
| `chat-gemini` **v0.2.0** | D1-D3, D5, D6, D9, and the OQ-7 gated test |

The family is back to a matching minor. Tests, race detector and lint are green
in all four modules, verified against the published `go/chat v0.2.0` rather than
a local workspace — which is the first check that `StatelessCapable` actually
resolves from the module proxy.

## 8. The measured result

OQ-7's gated test, run against the real Gemini API before the `v0.2.0` tag, over
six independent documents of equal size:

```
stateless input tokens per call: [2011 2011 2011 2011 2011 2011]
```

Flat, with no drift at all. Under the previous behaviour call six would have
carried the five documents before it. This is the claim §1 rests on, confirmed
by billing rather than by a fake — the gap §1.6 identifies, closed.
