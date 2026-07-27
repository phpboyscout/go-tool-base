---
title: "chat-openai: isolate per-call request params so call kinds stop bleeding state"
description: "The OpenAI client keeps one mutable a.params for every call kind. StreamChat sets StreamOptions and never clears it, poisoning later non-streaming Chat/Ask requests; Chat and StreamChat permanently clear ResponseFormat, so a Chat(); Ask() sequence silently loses JSON-schema enforcement. Fix: build per-call params from a base, mirroring Gemini's cloneConfig pattern."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - chat
  - openai
  - state-isolation
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# chat-openai: isolate per-call request params so call kinds stop bleeding state

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Shipped in go/chat-openai v0.1.4 (chat-openai MR !12), red-first TDD with the defect reproduced against the module's pre-fix main before the change; GTB picks it up via the config-family / module dependency bumps. Open questions were resolved by the maintainer during triage.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (chat-family section, HIGH finding),
    [chat-gemini error chain preservation](2026-07-23-chat-gemini-error-chain-preservation.md) (sibling provider-conformance fix from the same review),
    the chat extraction (`pkg/chat` → `go/chat` + provider modules) whose drop-in-provider promise this defect undermines

---

## 1. Problem

`chat-openai/openai.go` holds a single mutable `a.params`
(`openai.ChatCompletionNewParams`) shared by every call kind. Conversation
history legitimately lives there (`params.Messages`), but **call-shape** settings
are mutated in place and leak between calls:

- **`StreamChat` poisons subsequent non-streaming calls.** `StreamChat` sets
  `a.params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}`
  (`openai.go:394-396`) and never clears it. A later `Chat` or `Ask` on the same
  client sends `stream_options` on a non-streaming request — which the OpenAI API
  rejects with a 400. Sequence `StreamChat(); Chat()` on one client: the second
  call fails outright.

- **`Chat`/`StreamChat` permanently disable `Ask`'s schema enforcement.**
  `ResponseFormat` (the JSON-schema constraint) is set once at construction when
  `cfg.ResponseSchema != nil` (`openai.go:135-146`). Both `Chat` and `StreamChat`
  clear it in place (`openai.go:597`, `openai.go:389` — "Clear structured output
  mode if it was set") and `Ask` (`openai.go:228-263`) never re-applies it — it
  sends `a.params` as-is (`openai.go:244`). Sequence `Chat(); Ask()`: the `Ask`
  request goes out with no `response_format`, the model may answer free-text, and
  the `json.Unmarshal` at `openai.go:257` fails — or worse, mis-parses
  JSON-looking prose into the target. The constructor-configured schema is
  silently a one-shot.

Both failures are invisible at the call site: each call is individually correct,
and only the *sequence* breaks. That defeats the `ChatClient` drop-in-provider
contract — the same sequences work on Gemini, which clones its generation config
per call (`chat-gemini/gemini.go:578-586` `cloneConfig`; `Ask` applies
`ResponseMIMEType` to the clone only, `gemini.go:223-224`) and therefore has no
cross-call bleed.

**Concrete failure scenario.** A GTB tool wires one OpenAI client with a response
schema for structured extraction, streams a chat turn to the terminal, then calls
`Ask` for the structured result. The `Ask` request carries a stale
`stream_options` (400 from the API) — and even with that fixed, no
`response_format`, so schema enforcement is gone exactly when it is needed.

## 2. Proposed change

Stop mutating call-shape fields on the shared struct. Keep `a.params` as the
**base** (model, max tokens, seed, tools, and the append-only `Messages`
history), and have each call kind build its request from a shallow per-call copy
— the OpenAI analogue of Gemini's `cloneConfig`:

```go
// requestParams returns a per-call copy of the base params. Callers set
// call-shape fields (ResponseFormat, StreamOptions) on the copy only.
func (a *OpenAI) requestParams() openai.ChatCompletionNewParams {
	p := a.params // shallow copy; Messages slice shared intentionally
	return p
}
```

- `Chat`: use the copy with `ResponseFormat` zeroed (its current intent) — the
  in-place clears at `openai.go:597` and `openai.go:389` are deleted.
- `StreamChat`: copy with `ResponseFormat` zeroed and `StreamOptions` set.
- `Ask`: copy with the constructor's `ResponseFormat` applied (restoring schema
  enforcement on every `Ask`, regardless of call order) and no `StreamOptions`.
- History appends (`a.params.Messages = append(...)`) continue to target the
  shared base so all call kinds see the same conversation, matching today's
  behaviour and `Save`/`Restore` (`openai.go:643-664`).

Note the ReAct loops re-send params across steps (`openai.go:611`, `448`); the
per-call copy must be taken (or refreshed) so appended tool/assistant turns are
included each step — the simplest correct shape is to rebuild the copy from the
base at each step, or to append to the base and re-copy.

**Follow-on, not in scope here:** the review recommends a shared
provider-conformance test suite in the `go/chat` core. A call-sequence state test
("for every ordered pair of call kinds, the second call's request shape is
unaffected by the first") belongs there so all providers — including Gemini and
Anthropic — are held to the same isolation contract. This spec adds the
OpenAI-local regression tests only; the suite is tracked as its own piece of work
(see also the sibling
[chat-gemini error chain preservation](2026-07-23-chat-gemini-error-chain-preservation.md)
spec, which feeds the same suite).

## 3. Scope & release plan

- **Module:** `gitlab.com/phpboyscout/go/chat-openai` only. No exported API
  change — the fix is internal request construction plus tests. No core
  (`go/chat`) change. The `openai-compatible` provider shares this client, so it
  is fixed by the same change.
- **Release:** `fix(openai):` patch via releaser-pleaser in the `chat-openai`
  repo. The chat family's matching-minors compatibility contract is unaffected.
- **GTB:** picks the fix up via a routine `go.mod` bump of `chat-openai`; no GTB
  code change.

## 4. Acceptance criteria

1. Call-sequence state test (table-driven over ordered pairs of
   `Chat`/`StreamChat`/`Ask` against an `httptest` fake backend): the request
   body of the second call never contains `stream_options` unless the second
   call is `StreamChat`, and contains the constructor's `response_format`
   whenever the second call is `Ask` on a schema-configured client.
2. Specifically: `StreamChat(); Chat()` succeeds (no stale `stream_options`
   400), and `Chat(); Ask()` sends `response_format` with the configured JSON
   schema.
3. A schema-less client's `Ask` still sends no `response_format` (current
   behaviour preserved).
4. Multi-step ReAct requests include the turns appended by earlier steps
   (history sharing across the per-call copy verified).
5. `Save`/`Restore` round-trip behaviour unchanged.
6. Module CI green (tests, race, lint); coverage on `openai.go` does not
   regress.

## 5. Open questions

1. Should `Chat` continue to force-clear a constructor-configured
   `ResponseFormat`? Today's in-place clear reads as a workaround, but "Chat is
   free-form, Ask is structured" may be the intended contract — the review's
   Ask-semantics finding (MEDIUM) suggests the core should define this. Proposed
   default: keep `Chat`/`StreamChat` schema-free (preserves behaviour), and let
   the conformance-suite work settle the cross-provider contract.
