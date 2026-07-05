---
title: "Multimodal Input for pkg/chat Specification"
description: "Add image/media input to the chat client across Gemini, Claude, and OpenAI via an additive, backward-compatible MultimodalChatClient interface."
date: 2026-07-05
status: DRAFT
tags:
  - specification
  - chat
  - multimodal
  - providers
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Multimodal Input for pkg/chat Specification

Authors
:   Matt Cockayne

Date
:   5 July 2026

Status
:   DRAFT

---

## 1. Motivation

`pkg/chat` is a unified multi-provider chat client, but its surface is **text-only**:
`ChatClient` exposes `Add(ctx, prompt string)`, `Ask(ctx, question string, target)`,
and `Chat(ctx, prompt string)`. None accept images or other media.

Downstream tools increasingly need **multimodal input** — sending one or more
images to a vision-capable model alongside a text prompt. The immediate driver is
**krites' AI image review** (krites spec `0009`): it critiques wedding photographs
against a rubric, which requires uploading the image to a multimodal model. Today
krites cannot use `pkg/chat` for this and must hand-roll a provider HTTP client,
duplicating exactly the credential handling, provider abstraction, usage
accounting, and structured-output machinery `pkg/chat` already provides.

The good news: **every cloud provider SDK `pkg/chat` already uses supports image
input**:

- **Gemini** — `google.golang.org/genai` `Part` carries inline blobs
  (`genai.NewPartFromBytes(data, mimeType)`).
- **Claude** — `github.com/anthropics/anthropic-sdk-go` has base64 image content
  blocks (`anthropic.NewImageBlockBase64(mediaType, data)`).
- **OpenAI** — `github.com/openai/openai-go/v3` supports image content parts
  (`image_url` with a `data:` URI) on a user message.

So the capability exists at the SDK layer; only the **interface is the
bottleneck**. This spec adds multimodal input as an **additive, backward-compatible**
extension.

## 2. Non-goals

- **Image *generation* / output.** This is input only — sending media to the
  model. Models that return images are out of scope.
- **Audio / video / documents.** The design is media-typed and extensible, but
  this spec ships **images** only (the concrete need). PDFs/audio are a later,
  additive step behind the same `Media` type.
- **Changing the text-only path.** `Add`/`Ask`/`Chat` are unchanged; existing
  callers are unaffected.
- **Claude Local (CLI binary) multimodal.** `ProviderClaudeLocal` shells out to a
  CLI and does not support media here; it simply does not implement the new
  interface.

## 3. Design

### 3.1 The `Media` type

A provider-neutral attachment:

```go
// Media is a single input attachment (currently an image) sent alongside a text
// prompt to a multimodal model. Data holds the raw bytes; MIMEType names the
// format (e.g. "image/jpeg"). Providers transcode to their own wire format.
type Media struct {
    MIMEType string
    Data     []byte
}
```

Bytes-only (not URLs) in v1: it is the lowest common denominator (Gemini and Claude
take inline bytes; OpenAI takes a `data:` URI we build from the bytes), keeps the
caller from depending on provider-specific remote-fetch semantics, and avoids an
SSRF surface. A `URL` field is a possible additive extension (OpenAI-native) later.

Supported MIME types in v1: `image/jpeg`, `image/png`, `image/webp`, `image/gif`
(the intersection the three providers accept). Validation rejects others early with
a clear error.

### 3.2 The `MultimodalChatClient` interface (additive)

Mirroring the existing optional-extension pattern (`StreamingChatClient` embeds
`ChatClient` and adds `StreamChat`), multimodal is an **optional** interface a
vision-capable provider implements. Callers type-assert for it, exactly as they do
for streaming today.

```go
// MultimodalChatClient is a ChatClient that also accepts media (images) on the
// user turn. Providers implement it only when the configured model supports
// vision; callers type-assert to detect support.
type MultimodalChatClient interface {
    ChatClient

    // AddMedia appends a user message carrying the prompt text and the media,
    // without triggering a completion (the multimodal analogue of Add).
    AddMedia(ctx context.Context, prompt string, media []Media) error

    // AskWithMedia sends a question plus media and unmarshals the structured
    // response into target, honouring Config.ResponseSchema exactly as Ask does.
    AskWithMedia(ctx context.Context, question string, media []Media, target any) error

    // ChatWithMedia sends a message plus media and returns the response text,
    // running the tool ReAct loop as Chat does.
    ChatWithMedia(ctx context.Context, prompt string, media []Media) (string, error)
}
```

*(Open question §7-Q1 weighs this three-method shape against a smaller
`AddMedia` + reuse of the existing `Ask`/`Chat` for the follow-up call.)*

### 3.3 Capability discovery

Not every model behind a provider is vision-capable. Two layers:

1. **Interface presence** — a provider that *can* do vision returns a client that
   implements `MultimodalChatClient`; one that structurally cannot (Claude Local)
   does not. Callers `if mm, ok := client.(MultimodalChatClient); ok { … }`.
2. **Model capability** — within a vision-capable provider, a non-vision model
   (e.g. a text-only OpenAI model) will reject media. The provider guards this and
   returns a typed `ErrMediaUnsupported` **before** the network call where the
   model is known not to support vision, or surfaces the provider's own error
   otherwise. A conservative static allowlist per provider is the pragmatic v1
   (documented, easily updated), revisited in §7-Q2.

### 3.4 Per-provider mapping

| Provider | Media as | SDK construct |
| :--- | :--- | :--- |
| Gemini | inline blob part | `genai.NewPartFromBytes(m.Data, m.MIMEType)` appended to the user `Content.Parts` |
| Claude | base64 image block | `anthropic.NewImageBlockBase64(m.MIMEType, base64(m.Data))` in the user message's content blocks |
| OpenAI | image content part | a `data:<mime>;base64,<data>` URI as an `image_url` part on the user message |
| Claude Local | — | not supported (does not implement the interface) |

Media parts are appended **after** the prompt text in the same user turn, in caller
order, matching how each provider expects `[text, image, image, …]`.

### 3.5 Provider limits

Providers cap image count and size (e.g. Claude ~100 images/request and a per-image
size ceiling; OpenAI and Gemini similar). v1 validates against a conservative,
documented per-provider limit and returns a clear error rather than letting a large
request fail opaquely at the API.

## 4. Backward compatibility

Fully additive. `ChatClient` is unchanged, so every existing caller compiles and
behaves identically. `New(...)` returns the same concrete clients; they simply now
*also* satisfy `MultimodalChatClient` when vision-capable. No config migration.

## 5. Testing

- **Unit (per provider)** — with the SDK pointed at a mock/`httptest` transport,
  assert the outbound request carries the image in the provider's correct shape
  (Gemini inline blob, Claude base64 block, OpenAI `image_url` data URI) and that
  text-only calls are byte-for-byte unchanged (no regression).
- **Validation** — unsupported MIME type, empty data, over-limit count → typed
  errors, no network call.
- **Capability** — a non-vision model rejects media with `ErrMediaUnsupported`;
  Claude Local does not implement the interface.
- **Integration (env-gated, `INT_TEST=1`)** — one real round-trip per provider
  with a small synthetic image and a structured-output schema, asserting a parseable
  response and non-zero usage. Keyed by the provider env vars already used by the
  chat integration tests.

## 6. Documentation

- `pkg/chat` package doc + the living docs: a "Multimodal input" section with the
  `Media` type, the `MultimodalChatClient` interface, the per-provider support
  matrix, and a worked example (attach an image, `AskWithMedia` into a struct).
- A note in the provider table marking vision support.

## 7. Open questions

- **Q1 — interface shape.** The three-method `MultimodalChatClient`
  (`AddMedia`/`AskWithMedia`/`ChatWithMedia`) is explicit but wide. Alternative: a
  single `AddMedia` that *stages* media consumed by the next existing `Ask`/`Chat`
  call — smaller surface, but stateful and less obvious. **Lean: the explicit
  three-method interface** (stateless, symmetric with `Ask`/`Chat`), but confirm.
- **Q2 — capability discovery.** Static per-provider vision allowlist (simple,
  needs upkeep) vs. trusting the provider to error (zero upkeep, worse UX) vs. a
  dynamic capability probe (a `models.get`-style call; more work). **Lean: static
  allowlist in v1**, documented, with the provider error as the backstop.
- **Q3 — media source.** Bytes-only in v1 (chosen), or also accept a `URL` for
  OpenAI-native remote images? A URL is additive later; bytes cover every provider
  now.
- **Q4 — streaming multimodal.** Should `StreamingChatClient` gain a
  `StreamChatWithMedia`? Additive and later; not required by the driving use case.
- **Q5 — persistence.** The `PersistentChatClient` snapshot format would need to
  serialize media in history (or deliberately drop it). v1 can exclude media from
  snapshots (documented) and revisit.

## 8. Implementation plan (layered)

1. **The `Media` type + `MultimodalChatClient` interface + validation** (pure, no
   provider) — plus the typed errors and the capability allowlist scaffold.
2. **Gemini** — the first backend (krites' default), with unit + env-gated
   integration tests.
3. **Claude** — same seam.
4. **OpenAI** — same seam.
5. **Docs + the provider support matrix.**

Each backend is additive and independently testable, the same discipline the
existing provider adapters follow.
