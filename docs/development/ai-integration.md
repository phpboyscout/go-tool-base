---
title: AI Integration Layer
description: Deep dive into the GTB AI provider abstraction and chat package.
date: 2026-03-18
tags: [ai, openai, gemini, claude, library, chat]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI Integration Layer

GTB provides the foundational `chat` package for interacting with LLMs.

## Design Philosophy

`pkg/chat` is a **thin, purpose-built abstraction** for CLI tooling — not a general-purpose AI framework. Before selecting this approach, a comprehensive evaluation of alternatives (LangChain Go, go-openai, vercel/ai-sdk, and ~10 others) was performed.

The conclusion was clear: no existing library matched GTB's specific requirements:

- **Minimal interface surface** — the `ChatClient` interface exposes exactly four methods: `Add`, `Chat`, `Ask`, and `SetTools`. Downstream code never needs to know which provider is active.
- **Tool calling + structured output** — both capabilities are required together across providers, a combination that most thin wrappers do not handle uniformly.
- **CLI-first design** — features like token chunking, history management, and subprocess providers (e.g., `ProviderClaudeLocal`) are CLI concerns that general AI frameworks don't address.
- **Extensible without forking** — the `RegisterProvider` registry allows third-party packages to add providers without modifying `pkg/chat`.
- **Testable by default** — generated mocks in `pkg/mocks/chat` allow downstream applications to stub the entire AI layer without network calls.

This positioning makes `pkg/chat` a "right-sized" component: large enough to solve real provider-abstraction complexity, small enough that its full interface fits on a single screen.

## Core Abstractions

### `chat.ChatClient`

The `ChatClient` interface is the primary entry point. It abstracts the differences between all supported providers:

```go
type ChatClient interface {
    Add(ctx context.Context, prompt string, media ...Media) error
    Ask(ctx context.Context, question string, target any, media ...Media) error
    Chat(ctx context.Context, prompt string, media ...Media) (string, error)
    SetTools(tools []Tool) error
    Usage() Usage
}
```

All five built-in providers implement this interface:

- **OpenAI** (and compatible APIs via `ProviderOpenAICompatible`)
- **Google Gemini**
- **Anthropic Claude** (API)
- **Claude Local** — via the locally installed `claude` CLI binary

### Structured Output

The `chat` package supports structured output via JSON schemas. When adding new providers or improving existing ones, ensure that schema validation remains consistent across all backends. The `GenerateSchema[T]()` helper generates a `*jsonschema.Schema` from any Go type.

### Multimodal Input

`Add`, `Ask`, `Chat` and `StreamChat` take a trailing variadic of `Media` — images (and, on Gemini, PDF and A/V) sent alongside the text prompt. The parameter is variadic, so **text-only calls are unchanged** and the media path never affects them.

```go
img, _ := os.ReadFile("frame.jpg")

// Structured output over an image.
var out struct{ Description string `json:"description"` }
err := client.Ask(ctx,
    "Describe this photo as JSON {\"description\": string}", &out,
    chat.Media{Data: img}) // MIMEType optional — sniffed from the bytes

// Or free-form, with more than one attachment.
answer, err := client.Chat(ctx, "Which of these is better lit?",
    chat.Media{Data: imgA}, chat.Media{Data: imgB})
```

Attachments are placed **after** the prompt text in the same user turn, in the order given.

#### The `Media` type

```go
type Media struct {
    MIMEType string // OPTIONAL — see below
    Data     []byte // the raw attachment bytes
}
```

`MIMEType` is an **optional cross-check**, not an override. The type is always sniffed from `Data`, and the sniffed type is what gets sent. If you set `MIMEType`, it must match the sniffed family (e.g. `image/*`) or the attachment is rejected — this catches disguised content (a ZIP labelled `image/png`) and your own bugs. Leave it empty to let detection do the work.

#### Safety filtering

Every attachment is validated **before any network call**:

1. **Sniff** the type from the bytes with `net/http.DetectContentType` (never from a filename).
2. **Reconcile** against a declared `MIMEType` (family mismatch → rejected).
3. **Allowlist** — the sniffed type must be a recognised media type; anything else (executables, scripts, archives, HTML, `application/octet-stream`) is refused.
4. **Provider support** — the type must be accepted by the selected provider (matrix below).
5. **Limits** — at most **16 attachments** per request, **20 MiB** each.

Nothing is uploaded on failure. Two error sentinels distinguish the causes (test with `errors.Is`):

| Error | Meaning |
| :--- | :--- |
| `ErrMediaRejected` | Failed the safety filter: empty, oversize, over-count, an unidentifiable/disallowed type, or a declared-vs-sniffed mismatch. |
| `ErrMediaUnsupported` | The type is valid but the selected provider (or model) doesn't accept it — including any media sent to `ProviderClaudeLocal`. |

#### Provider support matrix

| Provider | Images | PDF | Audio/Video |
| :--- | :---: | :---: | :---: |
| Gemini | ✅ | ✅ | ✅ |
| Claude | ✅ | — | — |
| OpenAI (+ compatible) | ✅ | — | — |
| Claude Local | — | — | — |

Images map to each provider's native shape (Gemini inline blob, Claude base64 image block, OpenAI `image_url` data URI). The v1 accepted range is what stdlib detection can positively identify — common images, PDF, and the common A/V containers (`mp4`/`webm`/`avi`; `mp3`/`wav`/`ogg`/`aiff`). Long-tail formats stdlib cannot name (`mov`, `flv`, `wmv`, `flac`, `m4a`) are rejected until a richer sniffer lands.

#### Known limitations (v1)

- **PDF/document input for Claude and OpenAI** is a fast-follow (Gemini has it now).
- **Media is not persisted in `PersistentChatClient` snapshots** — a restored conversation keeps its text history but not its attachments. See the multimodal spec §4.1 for the planned content-addressed-cache design.
- **Streaming** carries media (`StreamChat`), but the long-tail A/V formats and non-image types for Claude/OpenAI are out of v1 scope.

## Developing for the AI Layer

### Adding a New Provider

Providers self-register via `init()` using the `RegisterProvider` extension point. To add a new AI provider from any package:

1. Implement the `ChatClient` interface.
2. Register the implementation with a unique `Provider` constant name:

```go
// myprovider/provider.go
package myprovider

import (
    "context"
    "gitlab.com/phpboyscout/go-tool-base/pkg/chat"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func init() {
    chat.RegisterProvider("my-backend", newMyBackend)
}

func newMyBackend(ctx context.Context, p *props.Props, cfg chat.Config) (chat.ChatClient, error) {
    return &MyBackendClient{token: cfg.Token, baseURL: cfg.BaseURL}, nil
}
```

After importing your package (e.g., via a blank import in `main.go`), `chat.New(ctx, p, chat.Config{Provider: "my-backend"})` routes to your factory. No changes to `pkg/chat` are required.

Built-in providers follow the same pattern — each file registers itself in its own `init()`:

| File | Registers |
| :--- | :--- |
| `openai.go` | `ProviderOpenAI`, `ProviderOpenAICompatible` |
| `claude.go` | `ProviderClaude` |
| `gemini.go` | `ProviderGemini` |
| `claude_local.go` | `ProviderClaudeLocal` |

### Testing AI Logic

We use a "Golden File" approach for testing complex AI prompts and responses. This ensures that changes to our implementation don't unintentionally alter the prompt structure sent to the models.

### Mocking in Consumer Apps

We provide generated mocks in `pkg/mocks/chat` to allow downstream applications to test their AI-powered features without making real network calls. The mock satisfies `ChatClient` and can be configured to return canned responses or return errors.

```go
mockClient := mocks.NewChatClient(t)
mockClient.EXPECT().Chat(mock.Anything, "analyze this").Return("looks good", nil)
```
