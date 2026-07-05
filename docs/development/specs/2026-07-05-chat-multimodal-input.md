---
title: "Multimodal Input for pkg/chat Specification"
description: "Add media (image/document/audio/video) input to the core chat client across Gemini, Claude, and OpenAI, with MIME auto-detection and safety filtering."
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
`Add(ctx, prompt string)`, `Ask(ctx, question string, target)`, and
`Chat(ctx, prompt string)` accept no media.

Downstream tools need **multimodal input** — sending one or more images (and, where
a provider supports it, documents/audio/video) to a model alongside a text prompt.
The immediate driver is **krites' AI image review** (krites spec `0009`), which
critiques photographs against a rubric and must upload the image. Without this,
krites hand-rolls a provider HTTP client, duplicating the credential handling,
provider abstraction, usage accounting, and structured-output machinery `pkg/chat`
already provides.

Every cloud provider SDK `pkg/chat` already uses supports media input:

- **Gemini** — `google.golang.org/genai` `Part` carries inline blobs
  (`genai.NewPartFromBytes(data, mimeType)`); supports images, PDF, audio, video.
- **Claude** — `github.com/anthropics/anthropic-sdk-go` has base64 image and
  document (PDF) content blocks.
- **OpenAI** — `github.com/openai/openai-go/v3` supports image content parts
  (`image_url` with a `data:` URI), and file/audio parts on capable models.

So the capability exists at the SDK layer; the **interface is the bottleneck**.

## 2. Design principles (from review, 2026-07-05)

- **Upgrade the core client, don't fork it.** go-tool-base is **pre-1.0**, so we
  are free to change signatures. Rather than a parallel `MultimodalChatClient`, we
  **extend the existing `ChatClient` methods** with a variadic media parameter.
  This keeps one client, one code path, and — because the parameter is variadic —
  every existing text-only call compiles and behaves unchanged.
- **Open up to the providers' full media range.** Not an images-only intersection:
  accept whatever the *selected provider* supports (images, PDF documents, and
  audio/video on Gemini), enforced per provider.
- **Detect the type, don't trust the caller.** When the caller doesn't set a MIME
  type, **sniff it from the bytes** with the stdlib **`net/http.DetectContentType`**
  ("mime magic"), never from a filename. Its coverage sets the v1 accepted range
  (§5): a type the sniffer cannot positively identify is **rejected**, not sent —
  the safe default. A richer sniffer to widen the range is a later, isolated swap
  behind the same choke point.
- **Safety-filter every attachment.** The sniffed type must be on an **allowlist**
  of genuine media types the providers accept; anything else — executables,
  scripts, archives, HTML, unknown/`application/octet-stream` — is **rejected
  before any network call**, so a caller cannot accidentally (or maliciously)
  upload dangerous content.

## 3. The `Media` type

```go
// Media is one input attachment sent alongside a text prompt. Data is the raw
// bytes. MIMEType is optional: when empty it is detected from Data by content
// sniffing (§5). The detected/declared type is then validated against the media
// allowlist and the selected provider's support (§5, §6).
type Media struct {
    // MIMEType optionally declares the media type (e.g. "image/jpeg"). Empty means
    // "detect from Data". A declared type is still verified against the sniffed
    // type — a mismatch is rejected (a declared image/jpeg that sniffs as a ZIP is
    // not sent).
    MIMEType string
    // Data is the raw attachment bytes.
    Data []byte
}
```

Bytes-only (not URLs) in v1: the lowest common denominator across providers,
sniffable, and free of an SSRF surface. A `URL` field is a possible additive
extension later (OpenAI-native remote images).

## 4. Extending `ChatClient`

The three text methods gain a trailing variadic `media ...Media`. The interface is
otherwise unchanged; existing callers are source-compatible.

```go
type ChatClient interface {
    Add(ctx context.Context, prompt string, media ...Media) error
    Ask(ctx context.Context, question string, target any, media ...Media) error
    Chat(ctx context.Context, prompt string, media ...Media) (string, error)
    SetTools(tools []Tool) error
    Usage() Usage
}
```

`StreamChat` on `StreamingChatClient` gains the same variadic tail.

- **Ordering.** Media parts are appended **after** the prompt text in the same user
  turn, in caller order — the `[text, media, media, …]` shape each provider expects.
- **Capability guard.** A provider/model that cannot accept media returns
  `ErrMediaUnsupported` **when media is passed** (e.g. `ProviderClaudeLocal`, or a
  text-only model). A call with **no** media is unaffected — the guard never fires
  on the text path.

### 4.1 Media persistence — local cache + pointer (Q5b, decision 2026-07-05)

`PersistentChatClient` snapshots the conversation history. Media is persisted, but
**not inlined as base64** (which would bloat every snapshot and fight the
encryption/size design). Instead:

- On attach, the media bytes are written **once** to a local, content-addressed
  cache under the client's filestore dir — `media/<sha256>.<ext>` — deduplicated by
  hash, so the same image referenced across turns is stored once.
- The transcript/snapshot stores a **pointer**, not the bytes: `{ ref: <sha256>,
  mimeType, size }`. Snapshots stay small and diff-friendly.
- On restore, each pointer is resolved back to its cached file and re-loaded into
  history, so a persisted multimodal chat **retains its images** across restarts.
- Encryption (`WithEncryption`) applies to the cached blobs as it does to
  snapshots; a missing/mismatched cache entry surfaces a clear error rather than a
  silent drop.
- Cache lifecycle (pruning blobs no snapshot references) is a follow-up; v1 keeps
  blobs for the filestore's lifetime.

> **Implementation note (2026-07-05).** Implemented, but at the **store layer,
> generically**, rather than per-provider. Providers marshal their opaque native
> history into `Snapshot.Messages`, so instead of walking each provider's structs,
> the `FileStore` runs a **reversible transform** over the messages JSON on Save:
> any large base64 / data-URI string is written to a content-addressed cache
> (`<store>/media/<sha256>`, deduped, encrypted with the store key when set) and
> replaced with a `chatmedia+sha256:<hash>` reference; `Load` reverses it before
> the snapshot reaches a provider's `Restore`. This keeps the cache+pointer intent
> (small snapshots, media retained on restore) with **zero provider changes**, and
> is safe by construction — a heuristic miss only affects snapshot *size*, never
> correctness, because the transform is fully reversed on Load. A path-traversal
> guard validates every hash read from a snapshot.

## 5. MIME detection & safety filtering

A single choke point validates every attachment before it reaches a provider:

1. **Sniff.** If `MIMEType` is empty, detect it from the first 512 bytes with
   stdlib **`net/http.DetectContentType`** (decision, review 2026-07-05: no new
   dependency). It positively identifies the common images (jpeg, png, gif, webp),
   **PDF**, and the common A/V containers (mp4, webm, avi; mp3, wav, ogg, aiff).
   Detection never trusts a caller-supplied filename. **A type it returns as
   `application/octet-stream` (unidentifiable) is rejected** — so bytes the sniffer
   cannot vouch for never reach a provider.
2. **Reconcile.** If the caller declared a `MIMEType`, it must match the sniffed
   family; a mismatch (declared `image/png`, sniffs as `application/zip`) is
   rejected — this is the core anti-smuggling check.
3. **Allowlist.** The reconciled type must be on the **media allowlist** — the
   union of types any supported provider accepts (images: jpeg, png, webp, gif,
   heic; documents: pdf; audio/video: the Gemini-supported set). Anything else —
   `application/x-*executable*`, `text/html`, `application/zip`, scripts,
   `application/octet-stream`, unknown — is refused with `ErrMediaRejected`.
4. **Provider support.** The type must be supported by the **selected** provider
   (an audio clip to a provider that takes only images → `ErrMediaUnsupported`),
   per the §6 matrix.
5. **Limits.** Per-attachment size and per-request count are checked against
   conservative, documented per-provider caps; over-limit fails early with a clear
   error rather than an opaque API rejection.

All of this happens **before** the network call, so a bad attachment costs nothing
and leaks nothing.

## 6. Per-provider mapping & support matrix

| Provider | Images | PDF | Audio/Video | Wire construct |
| :--- | :---: | :---: | :---: | :--- |
| Gemini | ✅ | ✅ | ✅ | `genai.NewPartFromBytes(data, mime)` appended to the user `Content.Parts` |
| Claude | ✅ | ✅ | — | `anthropic.NewImageBlockBase64` / document block in the user message |
| OpenAI | ✅ | model-dependent | model-dependent | `data:<mime>;base64,<data>` `image_url`/file part on the user message |
| Claude Local | — | — | — | not supported → `ErrMediaUnsupported` when media is passed |

The exact accepted-type set per provider is a documented, easily-updated table in
code (the allowlist), not scattered magic strings. In v1 the effective set is the
**intersection of the allowlist and what stdlib detection can identify** (§5, Q2):
a provider may accept `.mov`, but v1 rejects it because the sniffer cannot yet name
it — widened when a richer sniffer lands, with no call-site change.

## 7. Backward compatibility

Source-compatible: the variadic parameter means every existing `Add`/`Ask`/`Chat`
call compiles and behaves identically, and the text path never triggers the media
guard. No config migration. (Being pre-1.0, we accept the interface *signature*
change; the compile-time impact on callers is nil.)

## 8. Testing

- **Detection & safety (pure)** — table tests: real image/PDF bytes sniff
  correctly; a declared/sniffed mismatch, a disguised executable, an archive, HTML,
  empty data, and over-limit all reject with the right typed error and **no**
  network call.
- **Unit (per provider)** — SDK pointed at a mock/`httptest` transport: the
  outbound request carries the media in the provider's correct shape, and text-only
  requests are byte-for-byte unchanged (no regression).
- **Capability** — a non-vision model / Claude Local rejects media with the typed
  error.
- **Integration (env-gated, `INT_TEST=1`)** — one real round-trip per provider
  with a small synthetic image + a structured schema, asserting a parseable
  response and non-zero usage.

## 9. Documentation

`pkg/chat` package doc + living docs: a "Multimodal input" section with the `Media`
type, the extended methods, the support matrix, the safety-filtering behaviour, and
a worked example (attach an image, `Ask` into a struct). The provider table marks
media support.

## 10. Open questions

- **Q1 — interface shape.** ✅ **Resolved (review 2026-07-05): extend the core
  `ChatClient` with variadic `media ...Media`** (pre-1.0, no parallel client).
- **Q2 — media breadth in v1.** ✅ **Resolved (review 2026-07-05): the v1 range is
  what stdlib detection can positively identify** — images (jpeg/png/gif/webp), PDF,
  and the common A/V containers (mp4/webm/avi; mp3/wav/ogg/aiff). The long-tail
  formats stdlib cannot name (mov/quicktime, flv, wmv, 3gpp, flac, m4a/aac) are a
  **fast-follow gated on a richer sniffer**; until then they are rejected rather
  than trusted, keeping the anti-smuggling guarantee intact.
- **Q3 — sniffer dependency.** ✅ **Resolved: stdlib `net/http.DetectContentType`,
  no new dependency.** It covers the v1 range above. Swapping in a fuller sniffer
  (e.g. `gabriel-vasile/mimetype`) later to widen coverage is an isolated change
  behind the single detection choke point.
- **Q4 — declared-type reconciliation.** ✅ **Resolved (review 2026-07-05):
  `MIMEType` stays as an *optional cross-check*.** The sniffed type is authoritative
  and is what is sent; if the caller declares a type it must match the sniffed
  family or the attachment is rejected (anti-smuggling + catches caller bugs).
  Declaring a type never overrides the sniff or bypasses the allowlist.
- **Q5a — streaming.** ✅ **Resolved: `StreamChat` gains the variadic media tail**
  (uniformly multimodal; shares provider request assembly).
- **Q5b — persistence.** ✅ **Resolved: persist media via a local content-addressed
  cache + a pointer in the transcript** (not inline base64) — see §4.1. Restored
  chats retain their images; snapshots stay small.

## 11. Implementation plan (layered)

1. **`Media` + the detection/safety choke point + typed errors + the allowlist**
   (pure, no provider) — the most valuable, most testable core.
2. **Extend the `ChatClient` + `StreamingChatClient` signatures + wire the guard**;
   update all providers' signatures (text path unchanged).
3. **Gemini** media mapping (krites' default) — unit + env-gated integration.
4. **Claude** media mapping.
5. **OpenAI** media mapping.
6. **Media persistence** — the content-addressed cache + transcript pointer in
   `PersistentChatClient` (§4.1), with round-trip tests (attach → snapshot →
   restore retains the image).
7. **Docs + support matrix.**

Each step is additive and independently testable, matching the existing provider
adapters' discipline.
