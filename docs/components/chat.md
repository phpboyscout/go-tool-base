---
title: AI Chat
description: Unified interface for interacting with AI providers (OpenAI, Claude, Gemini, local claude binary, and OpenAI-compatible endpoints) including structured output and tool calling.
date: 2026-03-18
tags: [components, chat, ai, llm]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI Chat

The `chat` package provides a unified, high-level interface for interacting with various AI providers. It abstracts away the complexities of different APIs, allowing you to focus on building intelligent features for your CLI.

## Overview

Whether you're generating code, analyzing errors, or creating interactive assistants, the `chat` package serves as your gateway to Large Language Models (LLMs). It supports:

- **Multiple Providers:** OpenAI, Claude, Gemini, a locally installed `claude` binary, and any OpenAI-compatible endpoint.
- **Structured Output:** Easily unmarshal AI responses into Go structs.
- **Tool Calling:** Expose your own Go functions to the AI.
- **Extensible Registry:** Register custom providers from external packages without modifying the core.

## Getting Started

### Configuration

The `chat` package integrates with the application's configuration system, picking up authentication tokens from environment variables automatically.

The `Config` struct accepts the following fields:

| Field | Type | Description |
| :--- | :--- | :--- |
| `Provider` | `Provider` | The provider constant. Defaults to `ProviderOpenAI` if unset. |
| `Model` | `string` | Model name. Falls back to a sensible default per provider if empty. Required for `ProviderOpenAICompatible`. |
| `Token` | `string` | API key. Optional if set via environment variable. |
| `BaseURL` | `string` | API endpoint override. Required for `ProviderOpenAICompatible`. |
| `SystemPrompt` | `string` | Initial system prompt for the conversation. |
| `ResponseSchema` | `any` | JSON schema for enforcing structured output (used by `Ask`). |
| `SchemaName` | `string` | Name for the response schema tool. |
| `SchemaDescription` | `string` | Description for the response schema tool. |
| `MaxSteps` | `int` | Maximum ReAct loop iterations in `Chat()`. Zero uses the default (20). |
| `MaxTokens` | `int` | Maximum tokens per response. Zero uses the provider default (OpenAI: 4096, Claude: 8192, Gemini: 8192). |
| `ParallelTools` | `bool` | Enables concurrent execution of multiple tool calls within a single ReAct step. Disabled by default. |
| `MaxParallelTools` | `int` | Maximum number of tool calls executing concurrently. Zero uses the default (5). Only effective when `ParallelTools` is true. |

```go
import "github.com/phpboyscout/go-tool-base/pkg/chat"

cfg := chat.Config{
    Provider:     chat.ProviderOpenAI, // or ProviderClaude, ProviderGemini, ProviderClaudeLocal, ProviderOpenAICompatible
    Model:        "gpt-4o",
    // Token is optional if set via OPENAI_API_KEY environment variable
    SystemPrompt: "You are a helpful CLI assistant.",
}
```

### Initialization

```go
client, err := chat.New(ctx, props, cfg)
if err != nil {
    return errors.Newf("failed to initialize chat client: %w", err)
}
```

## Features

### Basic Chat

Send a natural language prompt and receive a text response.

```go
response, err := client.Chat(ctx, "Explain how to use the 'ls' command.")
if err != nil {
    // Handle error
}
fmt.Println(response)
```

### Structured Output (`Ask`)

The `Ask` method forces the AI to return data in a specific JSON structure, automatically unmarshaled into your Go struct.

```go
type AnalysisResult struct {
    Severity    string   `json:"severity"`
    Suggestions []string `json:"suggestions"`
}

var result AnalysisResult

err := client.Ask(ctx, "Analyze this error log and suggest fixes...", &result)
if err != nil {
    // Handle error
}

fmt.Printf("Severity: %s\n", result.Severity)
```

When `ResponseSchema` is set in the config at construction time, all subsequent `Ask` calls enforce that schema.

### Tool Calling

The `chat` package provides a robust mechanism for exposing Go functions as tools to the AI, implemented using JSON Schema for parameter definition and a handler-based execution loop.

#### Registration

```go
tools := []chat.Tool{
    {
        Name:        "read_file",
        Description: "Read the contents of a file",
        Parameters:  chat.GenerateSchema[struct { Path string `json:"path"` }]().(*jsonschema.Schema),
        Handler:     myHandler,
    },
}
client.SetTools(tools)
```

#### Complete Tool Handler Example

```go
package main

import (
    "context"
    "encoding/json"
    "os"

    "github.com/phpboyscout/go-tool-base/pkg/chat"
    "github.com/cockroachdb/errors"
)

type ReadFileParams struct {
    Path string `json:"path" jsonschema:"description=The file path to read"`
}

type FileContents struct {
    Content string `json:"content"`
    Size    int    `json:"size"`
}

func readFileHandler(ctx context.Context, args json.RawMessage) (any, error) {
    var params ReadFileParams
    if err := json.Unmarshal(args, &params); err != nil {
        return nil, errors.Newf("failed to parse arguments: %w", err)
    }

    content, err := os.ReadFile(params.Path)
    if err != nil {
        return nil, errors.Newf("failed to read file: %w", err)
    }

    return FileContents{
        Content: string(content),
        Size:    len(content),
    }, nil
}

func setupTools(client chat.ChatClient) error {
    tools := []chat.Tool{
        {
            Name:        "read_file",
            Description: "Read the contents of a file from the filesystem",
            Parameters:  chat.GenerateSchema[ReadFileParams]().(*jsonschema.Schema),
            Handler:     readFileHandler,
        },
    }
    return client.SetTools(tools)
}
```

#### Execution Loop

When a model issues a tool call, the `Chat` method:

1. **Intercepts** the response.
2. **Identifies** the requested tool by name.
3. **Unmarshals** arguments into the handler's expected format.
4. **Executes** the handler.
5. **Injects** the result back into the conversation history.
6. **Automatically** resumes the conversation to get the model's next response.

This loop continues for up to `Config.MaxSteps` iterations (default 20) before returning an error.

#### Parallel Tool Execution

When a provider returns multiple tool calls in a single response step, they can be executed concurrently rather than sequentially. This reduces latency for I/O-bound tools (HTTP requests, file reads, subprocess invocations).

Enable via `Config.ParallelTools`:

```go
cfg := chat.Config{
    Provider:         chat.ProviderClaude,
    Model:            "claude-sonnet-4-6",
    ParallelTools:    true,
    MaxParallelTools: 3, // optional; defaults to 5
}
```

**Behaviour:**

- Disabled by default — sequential execution is preserved unless opted in.
- Only activates when the provider returns more than one tool call in a single step. Single tool calls always use the sequential path regardless of this setting.
- Results are returned in the same order as the input tool calls, regardless of completion order.
- Context cancellation propagates to all in-flight tool goroutines.
- Tool errors (handler errors, tool not found) are returned as error strings in the conversation, consistent with the sequential path — they do not abort the ReAct loop.
- Bounded by `MaxParallelTools` (default 5) to prevent goroutine storms when the AI returns many calls at once.

**Thread safety:** tool handlers receive independent `json.RawMessage` inputs and return independent results. Parallel execution is safe as long as individual handlers do not share mutable state without synchronization.

### Multi-Turn Conversations

The chat client maintains conversation history. You can build multi-turn conversations:

```go
func interactiveSession(ctx context.Context, client chat.ChatClient) error {
    response1, err := client.Chat(ctx, "I have a Go project at /tmp/myproject")
    if err != nil {
        return err
    }
    fmt.Println("AI:", response1)

    // Second turn — client remembers the context
    response2, err := client.Chat(ctx, "What files are in the cmd directory?")
    if err != nil {
        return err
    }
    fmt.Println("AI:", response2)

    return nil
}
```

### Streaming Chat

Providers that support streaming implement the `StreamingChatClient` interface in addition to `ChatClient`. Streaming delivers partial response text as it is generated rather than waiting for the full response, which reduces perceived latency for long replies.

Discover streaming support via a type assertion:

```go
client, err := chat.New(ctx, p, chat.Config{
    Provider: chat.ProviderClaude,
    Model:    "claude-sonnet-4-6",
})
if err != nil {
    return err
}

if streamer, ok := client.(chat.StreamingChatClient); ok {
    result, err := streamer.StreamChat(ctx, "Write a haiku about Go.", func(e chat.StreamEvent) error {
        switch e.Type {
        case chat.EventTextDelta:
            fmt.Print(e.Delta) // progressive output
        case chat.EventComplete:
            fmt.Println() // newline after stream ends
        case chat.EventToolCallStart:
            fmt.Printf("[calling tool: %s]\n", e.ToolCall.Name)
        case chat.EventToolCallEnd:
            fmt.Printf("[tool result: %s]\n", e.ToolCall.Result)
        case chat.EventError:
            return e.Error
        }
        return nil
    })
    if err != nil {
        return err
    }
    _ = result // full assembled text, equal to concatenation of all EventTextDelta fragments
}
```

**Callback contract:**

- The callback is invoked synchronously for each event; it blocks the stream while executing.
- Return a non-nil error from the callback to cancel the stream — that error is returned by `StreamChat`.
- `StreamChat` returns the complete assembled response (concatenation of all `EventTextDelta` fragments) regardless of whether it exited early due to a callback error.

**Tool calls during streaming:**

Tool calls are handled transparently inside the `StreamChat` ReAct loop. The callback receives `EventToolCallStart` when execution begins and `EventToolCallEnd` (with the result populated) when it completes. `Config.ParallelTools` and `Config.MaxParallelTools` are respected.

**`ProviderClaudeLocal` does not implement `StreamingChatClient`.** Use the type assertion pattern to handle this gracefully.

### Streaming as the preferred path

When building components that benefit from progressive output (TUI widgets, CLI answer commands, doc generators), prefer `StreamChat` over `Chat` and fall back gracefully:

```go
func queryAI(ctx context.Context, client chat.ChatClient, prompt string, deltaFn func(string)) (string, error) {
    if streamer, ok := client.(chat.StreamingChatClient); ok {
        return streamer.StreamChat(ctx, prompt, func(e chat.StreamEvent) error {
            if e.Type == chat.EventTextDelta && deltaFn != nil {
                deltaFn(e.Delta)
            }
            return nil
        })
    }

    return client.Chat(ctx, prompt)
}
```

This pattern is used by `pkg/docs` (`AskAI`) and `internal/generator` (`writeAIDocs`) so that all three streaming providers benefit automatically without callers needing to know which provider is active.

## Provider Reference

### Provider Constants

| Constant | String Value | API Key Required |
| :--- | :--- | :--- |
| `chat.ProviderOpenAI` | `"openai"` | Yes — `OPENAI_API_KEY` |
| `chat.ProviderClaude` | `"claude"` | Yes — `ANTHROPIC_API_KEY` |
| `chat.ProviderGemini` | `"gemini"` | Yes — `GEMINI_API_KEY` |
| `chat.ProviderClaudeLocal` | `"claude-local"` | No — uses local `claude` binary |
| `chat.ProviderOpenAICompatible` | `"openai-compatible"` | Backend-dependent (set via `Token`) |

The default provider when `Config.Provider` is empty (and `AI_PROVIDER` env var is not set) is `ProviderOpenAI`.

### Capability Comparison

| Provider | Tool Calling | Parallel Tools | Structured Output | Streaming | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **OpenAI** | ✓ | ✓ | ✓ JSON Schema | ✓ | |
| **Claude** | ✓ | ✓ | ✓ Tool-based | ✓ | |
| **Gemini** | ✓ | ✓ | ✓ JSON Schema | ✓ | |
| **Claude Local** | ✗ | ✗ | ✓ `--json-schema` | ✗ | MCP tool support planned |
| **OpenAI-Compatible** | ✓ | ✓ | ✓ JSON Schema | ✓ | Backend-dependent |

### ProviderClaudeLocal

`ProviderClaudeLocal` routes requests through the locally installed `claude` CLI binary instead of the API. This is valuable in environments where direct outbound HTTPS to `api.anthropic.com` is blocked but the pre-authenticated `claude` binary is permitted.

**Requirements:**
- `claude` binary installed and authenticated (`claude login`)
- Binary must be in `PATH`
- No `Token` or API key needed

```go
client, err := chat.New(ctx, p, chat.Config{
    Provider:     chat.ProviderClaudeLocal,
    Model:        "claude-sonnet-4-6", // optional; uses claude's default if empty
    SystemPrompt: "You are a helpful assistant.",
})
```

Multi-turn continuity is maintained via session IDs captured from the CLI's JSON output and passed via `--resume` on subsequent calls.

### ProviderOpenAICompatible

Use `ProviderOpenAICompatible` to target any backend that exposes an OpenAI-compatible API, including Ollama, Groq, Fireworks AI, Together AI, LM Studio, and vLLM.

**Requirements:**
- `BaseURL` must be set in `Config`
- `Model` must be set (no default — model names are backend-specific)

```go
// Ollama (local)
client, err := chat.New(ctx, p, chat.Config{
    Provider: chat.ProviderOpenAICompatible,
    BaseURL:  "http://localhost:11434/v1",
    Model:    "llama3.2",
    Token:    "ollama", // Ollama ignores the token; any non-empty value works
})

// Groq (cloud)
client, err := chat.New(ctx, p, chat.Config{
    Provider: chat.ProviderOpenAICompatible,
    BaseURL:  "https://api.groq.com/openai/v1",
    Model:    "llama-3.3-70b-versatile",
    Token:    os.Getenv("GROQ_API_KEY"),
})
```

Token chunking falls back to `cl100k_base` encoding for model names not recognised by the tokenizer, so Ollama and other non-OpenAI model names are handled gracefully.

## Provider Registry

The provider registry is open for extension. Register a custom provider from any package:

```go
// mypackage/provider.go
func init() {
    chat.RegisterProvider("my-backend", newMyBackend)
}

func newMyBackend(ctx context.Context, p *props.Props, cfg chat.Config) (chat.ChatClient, error) {
    return &MyBackendClient{token: cfg.Token, baseURL: cfg.BaseURL}, nil
}
```

After importing your package, `chat.New(ctx, p, chat.Config{Provider: "my-backend"})` routes to your factory.

## Error Handling

The `chat` package normalizes errors from each provider:

- **Gemini**: `genai.APIError` is extracted and formatted as `Gemini API Error (<code>): <message>`.
- **OpenAI / Compatible**: `ResponseFormat` is cleared when `Chat` is called so JSON schema mode does not bleed into regular chat calls.
- **Claude Local**: subprocess `stderr` is captured and surfaced when the `claude` binary exits non-zero.

### Error Recovery Example

```go
func robustChat(ctx context.Context, p *props.Props, prompt string) (string, error) {
    client, err := chat.New(ctx, p, chat.Config{
        Provider: chat.ProviderClaude,
        Model:    "claude-sonnet-4-6",
    })
    if err != nil {
        return "", err
    }

    response, err := client.Chat(ctx, prompt)
    if err != nil {
        p.Logger.Warn("Primary provider failed, trying fallback", "error", err)

        fallback, fbErr := chat.New(ctx, p, chat.Config{
            Provider: chat.ProviderOpenAI,
            Model:    "gpt-4o",
        })
        if fbErr != nil {
            return "", errors.Newf("both providers failed: primary=%v, fallback=%w", err, fbErr)
        }

        return fallback.Chat(ctx, prompt)
    }

    return response, nil
}
```

## Thread Safety

`ChatClient` implementations are **not safe for concurrent use** by multiple goroutines. This is consistent with Go conventions (`http.Request`, `json.Decoder`, etc.). Each goroutine should create its own client instance via `chat.New()`.

Message history from `Add()` calls persists across `Chat()` and `Ask()` calls within the same client instance. To start a fresh conversation, create a new client.

## Best Practices

- **Context Management**: All methods that perform I/O (`Add`, `Ask`, `Chat`) accept `context.Context` as the first parameter. Always pass an appropriate context to ensure operations can be cancelled or timed out.
- **One Client Per Goroutine**: Do not share a `ChatClient` instance across goroutines. Create a new client for each concurrent conversation.
- **System Prompts**: Use `SystemPrompt` in the config to define the AI's persona and constraints.
- **Validation**: Validate AI outputs before using them in critical code paths, even when using structured `Ask` responses.
- **Token Limits**: Be mindful of token limits when building conversation history; consider summarizing or truncating long sessions.
- **Rate Limiting**: Implement appropriate backoff when encountering rate limit errors.
- **Local vs. API**: Prefer `ProviderClaudeLocal` only when API access is restricted; API providers offer lower latency and full feature support.
