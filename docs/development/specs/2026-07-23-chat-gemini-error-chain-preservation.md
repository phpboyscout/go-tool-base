---
title: "chat-gemini: preserve the typed SDK error chain so cross-provider failover works"
description: "Gemini's handleGeminiError flattens every Chat/StreamChat error to a new string via errors.Newf without %w, destroying the typed *genai.APIError. The module's own registered geminiHTTPStatus extractor can then never match, so the fallback composite classifies retryable Gemini failures (429/503) as fatal and refuses to fail over. Fix: wrap instead of rebuild, matching the Anthropic and OpenAI providers."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - chat
  - gemini
  - failover
  - error-handling
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# chat-gemini: preserve the typed SDK error chain so cross-provider failover works

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (chat-family section, HIGH finding),
    [chat-openai request isolation](2026-07-23-chat-openai-request-isolation.md) (sibling provider-conformance fix from the same review),
    the chat extraction (`pkg/chat` → `go/chat` + provider modules) that introduced the SDK-free status-extractor registry this defect defeats

---

## 1. Problem

The `go/chat` core is deliberately SDK-free: cross-provider failover classifies
vendor errors through registered `HTTPStatusExtractor` functions
(`chat/fallback_policy.go:93-110`). Each provider module registers one in `init()`;
`chat-gemini` registers `geminiHTTPStatus` (`chat-gemini/gemini.go:18`, `29-36`),
which does `errors.As(err, &apiErr)` against `*genai.APIError`.

That extractor can never fire for `Chat` or `StreamChat`, because every error on
those paths routes through `handleGeminiError`
(`chat-gemini/gemini.go:338-345`, called at `gemini.go:313` and `gemini.go:493`):

```go
func (g *Gemini) handleGeminiError(err error, step int) error {
	var apiErr *genai.APIError
	if errors.As(err, &apiErr) {
		return errors.Newf("Gemini API Error (%d): %s", apiErr.Code, apiErr.Message)
	}

	return errors.Newf("gemini send message failed (step %d): %v", step, err)
}
```

Neither branch uses `%w` (the second flattens with `%v`), so the returned error is
a **new** error whose chain no longer contains the typed SDK error — or anything
else from the original. Two core mechanisms are defeated at once:

- `errors.As(err, &apiErr)` in `geminiHTTPStatus` finds no `*genai.APIError` →
  `providerHTTPStatus` reports no status.
- `errors.Is(err, context.DeadlineExceeded)` in `DefaultFailoverPolicy`
  (`chat/fallback_policy.go:73`) never matches a flattened per-request timeout.

**Concrete failure scenario.** A tool configured with the fallback chain
`[gemini, claude]` calls `Chat` and Gemini returns a 429 (rate limit) or 503.
`DefaultFailoverPolicy.Classify` (`chat/fallback_policy.go:45-91`) finds no HTTP
status, no deadline error, no net error → falls through to `FailoverFatal`
(`fallback_policy.go:90`) → the composite returns the error to the caller instead
of failing over to Claude. The entire point of configuring a fallback chain is
silently void for Gemini's primary call paths.

Only `Ask` classifies correctly, because it wraps with `%w` directly
(`gemini.go:231-234`). The other providers preserve the chain on all paths:
Anthropic wraps with `%w` / `errors.Wrap` (`chat-anthropic/claude.go:196`, `357`,
`550`), and OpenAI either returns the SDK error untouched
(`chat-openai/openai.go:613`) or wraps it (`openai.go:246`, `471`). The
provider-status extractors for both are exercised by tests against wrapped errors
(`chat-openai/openai_status_internal_test.go:31`); Gemini's defect is exactly the
case those tests guard against, but on the producing side.

## 2. Proposed change

Make `handleGeminiError` **wrap** rather than rebuild, preserving the chain on
both branches while keeping the current message text:

- API branch: `errors.Wrapf(err, "Gemini API Error (%d): %s", apiErr.Code, apiErr.Message)`
  (or `errors.Newf("...: %w", ...)` — match the module's prevailing style, which is
  `errors.Newf` with `%w`, e.g. `gemini.go:228`, `233`).
- Fallback branch: replace `%v` with `%w`:
  `errors.Newf("gemini send message failed (step %d): %w", step, err)`.

Add unit tests in `chat-gemini` mirroring `openai_status_internal_test.go`:
`geminiHTTPStatus` must extract the status from the exact error value
`handleGeminiError` returns (both branches), not just from a raw
`*genai.APIError`.

**Follow-on, not in scope here:** the review recommends a shared
provider-conformance test suite in the `go/chat` core that provider modules run in
CI. "The error returned by Chat/StreamChat/Ask must satisfy the module's
registered status extractor for a synthetic provider HTTP error" belongs in that
suite so this class of regression is caught for every provider, including future
ones. This spec adds the Gemini-local regression tests only; the conformance suite
is tracked as its own piece of work (see also the sibling
[chat-openai request isolation](2026-07-23-chat-openai-request-isolation.md) spec,
which feeds the same suite).

## 3. Scope & release plan

- **Module:** `gitlab.com/phpboyscout/go/chat-gemini` only. No API change — the
  fix is internal to `handleGeminiError` plus tests. No core (`go/chat`) change.
- **Release:** `fix(gemini):` patch via releaser-pleaser in the `chat-gemini`
  repo. The chat family's matching-minors compatibility contract is unaffected
  (patch release; core stays put).
- **GTB:** picks the fix up via a routine `go.mod` bump of `chat-gemini`
  (Renovate or manual); no GTB code change.

## 4. Acceptance criteria

1. `handleGeminiError` preserves the error chain on both branches: for any input
   `err`, `errors.Is(handleGeminiError(err, n), err)` holds, and a wrapped
   `*genai.APIError` is recoverable via `errors.As`.
2. `geminiHTTPStatus` unit test: given `handleGeminiError` applied to a
   `*genai.APIError{Code: 429}`, the extractor returns `(429, true)`.
3. Failover regression test (in `chat-gemini`, using the core's fallback
   composite or `DefaultFailoverPolicy` directly): a Gemini `Chat` error carrying
   a 429/503 `*genai.APIError` classifies as `FailoverNext`; a 400 classifies as
   `FailoverFatal`; a wrapped `context.DeadlineExceeded` classifies as
   `FailoverNext`.
4. User-facing error text is unchanged in substance (message prefix and
   code/step detail retained).
5. Module CI green (tests, race, lint); coverage on the touched file does not
   regress.

## 5. Open questions

None.
