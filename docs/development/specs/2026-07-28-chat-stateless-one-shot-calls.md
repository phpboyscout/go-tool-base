---
title: "chat: a stateless one-shot mode so batch work stops paying quadratic history costs"
description: "ChatClient accumulates conversation history across calls, so a caller doing N independent Ask calls on one client re-sends the whole prefix each time — quadratic input cost, rate-limit trips, and cross-call contamination of independent classifications. Adds Config.Stateless: history is neither read nor appended across calls, while the intra-call ReAct loop keeps the turns it needs. Includes a capability guard so setting the flag against a provider module that predates it fails loudly rather than silently billing."
date: 2026-07-28
status: DRAFT
tags:
  - specification
  - chat
  - cost
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
:   DRAFT — pending review

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
   a provider-conformance defect on its own terms, independent of cost, and it
   belongs in the conformance suite tracked by
   [2026-07-23-chat-provider-conformance](2026-07-23-chat-provider-conformance.md).

### 1.2 The composite compounds it

`fallbackClient` keeps its own `transcript []string` of user turns
(`chat/fallback.go:64-68`) and appends on every `Add`/`Ask`/`Chat`
(`:319`, `:331`, `:355`) so it can replay context into the next provider on
failover (`ensureReady`, `:468-486`). A batch caller behind a fallback composite
therefore accumulates twice: once in the active provider client, once in the
wrapper. Any fix that only touches the provider modules leaves the composite
growing unbounded.

### 1.3 What it cost, and what the numbers say

The report from `scoutdm`: one `Ask` per source document, eighteen independent
sources, one client held for the run. Source 18 re-sends the previous seventeen
documents *and* their extraction responses.

The reported arithmetic — `18×19/2 = 171` document-equivalents against 18, a
**9.5× multiplier** — is correct for the shape described.

The billed figures (Gemini 3.6 Flash, text):

| SKU | Tokens |
|---|---|
| input | 17,628,739 |
| **cached input** | **61,923,684** |
| output | 997,374 |

**Worth resolving before this spec is approved (OQ-1):** 9.5× applied to a
375k-token corpus predicts ~3.6M input tokens for one run, not 79.5M. The
billed total is ~22× that, which most plausibly means the pipeline was run
around twenty times during development rather than that the per-call growth is
worse than modelled. The *diagnosis* does not depend on reconciling this — the
3.5:1 ratio of cached to fresh input is exactly the signature of an ever-growing
re-cached prefix, and the code in §1.1 shows the mechanism directly — but the
spec should not carry an unexplained order of magnitude.

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

## 2. Proposed change

Add an opt-in stateless mode to `chat.Config`, implemented by every provider,
guarded so it cannot be silently ignored.

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

### D5 — `Save`/`Restore` under stateless

`PersistentChatClient` (`chat/persistence.go:25-35`) has no coherent meaning
without retained state.

- `Save` returns a snapshot with an empty `Messages` array — valid, honest, and
  round-trippable.
- `Restore` returns a sentinel error (`ErrStatelessRestore`) rather than
  silently succeeding and discarding the snapshot.

See OQ-2 — there is a reasonable argument for `Restore` instead seeding a
persistent per-call prefix, which would make "restore a primed context, then
run N independent calls against it" expressible. That is a genuinely useful
shape and is *not* served by `SystemPrompt` alone.

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
- New `docs/how-to/batch-processing.md` in the `chat` repo: the batch shape, the
  quadratic trap with the arithmetic, and the stateless client that fixes it.
- `docs/explanation/architecture.md:22` extended to describe both modes.
- The per-provider `Ask` divergence found in §1.1 is fixed or explicitly
  documented — see OQ-3.

## 3. Scope & release plan

Four repositories. The core lands first; providers follow; no provider is
required for the core to be releasable, because D6 makes a missing
implementation a construction-time error rather than a silent miss.

| Repo | Change | Release |
|---|---|---|
| `go/chat` | `Config.Stateless`, `StatelessCapable`, `ErrStatelessRestore`, `New` guard, `fallbackClient` (D4), `claude-local` (D2/D3), conformance cases, docs (D8) | `feat:` minor |
| `go/chat-anthropic` | call-local messages (D2), `Add` buffer (D3), marker | `feat:` minor |
| `go/chat-openai` | call-local `params.Messages` (D2), `Add` buffer (D3), marker; covers `openai-compatible` | `feat:` minor |
| `go/chat-gemini` | call-local history — do not call `persistSessionHistory` (D2), `Add` buffer (D3), marker | `feat:` minor |

All via releaser-pleaser. The chat family's matching-minors compatibility
contract means the four minors move together; provider modules bump their
`go/chat` requirement to the version carrying the marker interface.

GTB picks this up through a routine module bump. The GTB adapter maps its own
config schema into `chat.Config`, so exposing `Stateless` as an `ai.*` config
key (or leaving it programmatic-only) is a GTB-side decision — out of scope
here, noted as OQ-4.

**Downstream:** `scoutdm` currently builds a fresh client per document, with a
test asserting distinct clients are constructed. That workaround is replaced by
a single stateless client once the Gemini module ships; the test that asserts
per-document construction is deleted with it.

## 4. Acceptance criteria

1. **No cross-call growth.** For each provider, against a recording fake
   backend: ten sequential `Ask` calls on one stateless client produce ten
   requests whose message arrays are all length 1 (or 1 + the system prompt as
   that provider carries it). The same ten calls on a default client produce
   monotonically growing arrays — the regression test that would have caught
   this.
2. **No cross-call contamination.** Request N contains no text from any prompt
   or response of calls 1..N-1.
3. **Tool calling still works stateless.** A multi-step ReAct conversation
   completes on a stateless client: the step-2 request contains the step-1
   assistant `tool_use` and its `tool_result`; the step-1 turns are absent from
   the *next* `Chat` call's request.
4. **`Add` is one-shot.** `Add(a); Add(b); Chat(c)` sends all three parts in one
   request; the immediately following `Chat(d)` sends only `d`.
5. **Capability guard.** `chat.New` with `Stateless: true` against a registered
   provider whose client does not implement `StatelessCapable` returns an error
   naming the provider. A test registers a deliberately non-capable provider to
   prove it.
6. **Fallback composite.** A stateless composite accumulates no `transcript`;
   after a forced failover the new provider's first request carries no replayed
   turns and the call still succeeds. Tools are still re-applied.
7. **Persistence.** `Save` on a stateless client returns a snapshot with empty
   `Messages`; `Restore` returns `ErrStatelessRestore` (subject to OQ-2).
8. **`claude-local`.** No `--resume` in the argv of any stateless invocation,
   including the second and subsequent calls on one client.
9. **Default unchanged.** The full existing test suite passes untouched across
   all four modules; `Stateless: false` behaviour is byte-identical.
10. **Usage accounting.** `Usage()` remains cumulative across calls on a
    stateless client — the point of holding one client for a batch is
    whole-batch accounting.
11. Docs from D8 land with the core release; CI green (tests, race, lint) and
    coverage does not regress in any of the four modules.

## 5. Open questions

**OQ-1 — Reconcile the billed figures (§1.3).** The 9.5× model predicts ~3.6M
input tokens for one 18-document run against 375k tokens of source; the bill
shows 79.5M. Roughly twenty runs during development is the likely explanation
and would close it. Worth confirming from `scoutdm`'s run history before
approval, so the spec's headline number is defensible. The diagnosis stands
either way.

**OQ-2 — Should `Restore` be an error, or seed a per-call prefix?** D5 proposes
an error as the honest reading. The alternative — `Restore` installs a fixed
prefix that every subsequent one-shot call is seeded with, and that no call ever
mutates — makes "prime a context once, then run N independent calls against it"
expressible, which few-shot batch classification genuinely wants and
`SystemPrompt` does not cover. It is a larger surface and could be deferred to
its own spec. Recommendation: ship D5 as written, and record the prefix idea
here so it is not re-derived from scratch.

**OQ-3 — Fix Anthropic's `Ask` asymmetry now, or hand it to the conformance
suite?** §1.1 found that `chat-anthropic`'s `Ask` appends the question and never
the answer, so a stateful `Ask` loop on Claude builds a run of unanswered user
turns. Stateless mode makes it moot for batch callers but not for conversational
ones. It is a genuine conformance defect and arguably belongs to
[2026-07-23-chat-provider-conformance](2026-07-23-chat-provider-conformance.md)
rather than here; folding it in would widen this spec's blast radius from
"additive flag" to "changes existing stateful behaviour". Recommendation: leave
it out of scope, and raise it against the conformance spec with the evidence
from §1.1.

**OQ-4 — Does GTB expose `Stateless` as an `ai.*` config key?** A per-tool
config key is plausible; so is programmatic-only, on the grounds that whether a
given call site is batch or conversational is a property of the code, not of the
deployment. GTB-side decision, listed so it is not forgotten at adapter-bump
time.

**OQ-5 — Are stateless clients safe for concurrent use?** `ChatClient` documents
that implementations are not (`client.go:46-53`). A stateless client mutates far
less per call, and `UsageTracker` is already mutex-guarded (`chat/usage.go:77-78`),
so "stateless clients are safe to share across goroutines" looks reachable — and
it is what a batch caller actually wants next, since the natural follow-on to
"one client for N documents" is "one client for N documents across W workers".
It needs a real audit of each provider's remaining per-client state (the pending
`Add` buffer under D3 is mutable and shared, and the vendor SDK clients need
checking) rather than an assertion. Recommendation: hold the current
not-concurrency-safe contract for this change, and spec it separately.

## 6. Resolved

*(Nothing yet — this spec is DRAFT. Resolutions from review land here, dated.)*
