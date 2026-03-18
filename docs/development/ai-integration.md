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
    Add(prompt string) error
    Chat(ctx context.Context, prompt string) (string, error)
    Ask(question string, target any) error
    SetTools(tools []Tool) error
}
```

All five built-in providers implement this interface:

- **OpenAI** (and compatible APIs via `ProviderOpenAICompatible`)
- **Google Gemini**
- **Anthropic Claude** (API)
- **Claude Local** — via the locally installed `claude` CLI binary

### Structured Output

The `chat` package supports structured output via JSON schemas. When adding new providers or improving existing ones, ensure that schema validation remains consistent across all backends. The `GenerateSchema[T]()` helper generates a `*jsonschema.Schema` from any Go type.

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
    "github.com/phpboyscout/gtb/pkg/chat"
    "github.com/phpboyscout/gtb/pkg/props"
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
