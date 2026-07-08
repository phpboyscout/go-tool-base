---
title: Chat
description: Unified multi-provider AI client with an agentic ReAct loop and tool calling.
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

The `chat` package can be configured directly with `chat.Config` or through
GTB's framework config adapter. The reusable package owns typed config shapes
(`RuntimeConfig`, `FallbackConfig`, and `CredentialConfig`); GTB adapter code
loads those from `ai.*`, `ai.fallback.*`, and provider API sections. This keeps
the package usable outside GTB without importing the framework config container.

The `Config` struct accepts the following fields:

| Field | Type | Description |
| :--- | :--- | :--- |
| `Provider` | `Provider` | The provider constant. Defaults to `ProviderClaude` if unset and `AI_PROVIDER` is not set. |
| `Model` | `string` | Model name. Falls back to a sensible default per provider if empty. Required for `ProviderOpenAICompatible`. |
| `Token` | `string` | API key. Optional if set via environment variable. |
| `Credentials` | `CredentialConfig` | Provider credential references supplied by the host application. `Token` wins when both are set. |
| `BaseURL` | `string` | API endpoint override. Required for `ProviderOpenAICompatible`. |
| `SystemPrompt` | `string` | Initial system prompt for the conversation. For Claude it is sent in the API's dedicated `system` field (not as a user turn); OpenAI sends it as the first system message; Gemini restores it to both config and history. |
| `ResponseSchema` | `any` | JSON schema for enforcing structured output (used by `Ask`). |
| `SchemaName` | `string` | Name for the response schema tool. |
| `SchemaDescription` | `string` | Description for the response schema tool. |
| `MaxSteps` | `int` | Maximum ReAct loop iterations in `Chat()`. Zero uses the default (20). |
| `MaxTokens` | `int` | Maximum tokens per response. Zero uses the provider default (OpenAI: 4096, Claude: 8192, Gemini: 8192). |
| `RequestTimeout` | `time.Duration` | Per-request timeout. Zero falls back to `ai.request_timeout`, then `DefaultChatRequestTimeout`. |
| `ParallelTools` | `bool` | Enables concurrent execution of multiple tool calls within a single ReAct step. Disabled by default. |
| `MaxParallelTools` | `int` | Maximum number of tool calls executing concurrently. Zero uses the default (5). Only effective when `ParallelTools` is true. |
| `Seed` | `*int64` | Optional sampling seed for OpenAI / OpenAI-compatible providers. `nil` (the default) omits the seed entirely so the model samples normally; set a value only for reproducible-ish completions. (Earlier builds hardcoded `seed=0`.) |
| `UsageObserver` | `func(Usage)` | Optional opt-in hook fired once per provider round-trip with that round-trip's token usage. See [Token usage & cost observability](#token-usage--cost-observability). |

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/chat"

cfg := chat.Config{
    Provider:     chat.ProviderOpenAI, // or ProviderClaude, ProviderGemini, ProviderClaudeLocal, ProviderOpenAICompatible
    Model:        "gpt-5.4",
    // Token is optional if set via OPENAI_API_KEY environment variable
    SystemPrompt: "You are a helpful CLI assistant.",
}
```

#### Credential Resolution

Every provider resolves its API key through a shared four-step precedence so tool authors never need to re-implement the cascade:

1. **Direct token** — `Config.Token` supplied by the caller (tests, explicit overrides).
2. **Env-var reference in config** — `{provider}.api.env` names an env var (e.g. `ANTHROPIC_API_KEY`). The resolver reads the name from config and then `os.Getenv(name)` for the value. This keeps the literal secret out of the config file while letting the user control which env var holds it.
3. **Literal in config** — `{provider}.api.key`. Routed through Viper's `AutomaticEnv`, so a prefixed env var (e.g. `MYTOOL_ANTHROPIC_API_KEY`) is picked up here too.
4. **Unprefixed ecosystem env** — `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`. Final fallback for compatibility with provider SDKs and common CI conventions.

Three pairs of config-key constants describe the per-provider surface:

| Provider | Literal key | Env-var-reference key | Ecosystem fallback env var |
|----------|-------------|-----------------------|---------------------------|
| Claude   | `ConfigKeyClaudeKey` (`anthropic.api.key`) | `ConfigKeyClaudeEnv` (`anthropic.api.env`) | `EnvClaudeKey` (`ANTHROPIC_API_KEY`) |
| OpenAI   | `ConfigKeyOpenAIKey` (`openai.api.key`) | `ConfigKeyOpenAIEnv` (`openai.api.env`) | `EnvOpenAIKey` (`OPENAI_API_KEY`) |
| Gemini   | `ConfigKeyGeminiKey` (`gemini.api.key`) | `ConfigKeyGeminiEnv` (`gemini.api.env`) | `EnvGeminiKey` (`GEMINI_API_KEY`) |

The interactive `gtb init ai` wizard defaults to env-var mode — it prompts for an env var name (pre-populated with the provider standard) and writes only `{provider}.api.env`. The literal is never persisted to disk in the recommended path. See [`pkg/credentials`](../credentials.md) for the storage-mode taxonomy shared with the setup wizard, doctor, and config masker.

### Initialization

```go
client, err := chat.New(ctx, props, cfg)
if err != nil {
    return errors.Newf("failed to initialize chat client: %w", err)
}
```

### GTB Adapter Config

When constructed through GTB props, `chat.New` and `chat.NewWithFallback` adapt
the framework config into package-owned structs before provider construction:

| Config section | Typed struct | Purpose |
| :--- | :--- | :--- |
| `ai` | `RuntimeConfig` | Provider and request timeout defaults. |
| `ai.fallback` | `FallbackConfig` | Cross-provider failover toggle and provider order. |
| `openai.api`, `anthropic.api`, `gemini.api` | `CredentialConfig` | Env-var reference, keychain reference, and literal fallback for provider credentials. |

The config keys and precedence remain the GTB defaults: explicit `chat.Config`
fields win where set, GTB's resolved config fills unset fields, provider
ecosystem env vars are still fallback credential sources, and `AI_PROVIDER`
still overrides the provider when no provider is otherwise configured.

Existing chat clients do not live-reconfigure themselves when GTB config reloads.
To use changed provider, fallback, timeout, or credential settings, construct a
new client after reload. This keeps provider clients independent of GTB's config
observer system and avoids mutating conversation state mid-session.

## Agentic vs. Legacy Workflows

When building AI-powered features, it is helpful to distinguish between "Legacy" single-action patterns and the "Agentic" patterns enabled by GTB.

### Legacy: The One-Way Prompt

In a legacy workflow, the interaction is linear and deterministic:

1. **Request**: The user sends a prompt.
2. **Execution**: The model processes the input in a single pass.
3. **Response**: The model returns a static response.

This approach is brittle for complex tasks. If the AI needs a piece of information it doesn't have, it must either "hallucinate" a guess or fail. The developer is forced to front-load as much context as possible into the prompt (context-stuffing), which is expensive and often leads to lower-quality reasoning.

### Agentic: The Iterative Loop

GTB shifts the focus toward **Agentic Workflows**. Instead of trying to solve the entire problem in one shot, the AI is given a set of "senses"—your CLI commands and library functions.

1. **Reasoning**: The AI analyzes the request and decides on a *first step*.
2. **Action**: It calls a local tool (e.g., `ReadDir` or `GetConfig`).
3. **Observation**: It receives the *actual results* from your system.
4. **Correction**: Based on the observation, it updates its plan.
5. **Finality**: It repeats this until it has sufficient information to provide a verified answer.

!!! note "The Philosophy of Verification"
    In an agentic workflow, the AI doesn't just *say* it fixed a bug; it uses a `Test` tool to *verify* the fix before reporting success. This transforms the AI from a creative writer into a reliable collaborator.

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

`SetTools` **replaces** the client's tool set on every call — it does not merge.
Each call installs exactly the handlers in `tools` and discards any handlers
registered by a previous `SetTools` call, so a stale handler can never linger.
For Claude, the call also clears any `ResponseSchema` set at construction
(structured-output `Ask` and tool calling are mutually exclusive), keeping the
reset consistent with the other providers.

For a full, runnable tool-handler example — parsing arguments, returning a typed
result, and wiring it with `SetTools` — see the
[AI Tool Calling](../../../how-to/ai-tool-calling.md) how-to.

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

**Tool handler panics become tool-error content.** Tool handlers run model-generated, potentially adversarial input. A handler that panics is recovered and converted to a tool-error string (`Error: tool handler panicked: <value>`) that is fed back to the model as conversation content — exactly like a returned `error` — rather than crashing the process. This holds for **both** the sequential and the parallel dispatch paths (in the parallel path the handler runs in a bare goroutine, where an unrecovered panic would otherwise be fatal). The recovered value is also logged at `Error` level. Handlers should still return errors explicitly where possible; the recover is a safety net, not a substitute for error handling.

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

## Token usage & cost observability

Every provider surfaces token usage so a tool built on GTB can observe and cost its LLM calls. Usage is reported in a provider-neutral `Usage` struct — you never touch a provider SDK's usage type.


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/chat](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/chat) for the full API definition.


There are two complementary ways to read usage:

### `Usage()` accessor — cumulative total

`ChatClient.Usage()` returns the **cumulative** usage across every provider round-trip made by that client instance since construction:

```go
client, _ := chat.New(ctx, p, cfg)
_, _ = client.Chat(ctx, "Summarise this changelog…")

u := client.Usage()
if u.Known {
    fmt.Printf("tokens: in=%d out=%d total=%d\n", u.InputTokens, u.OutputTokens, u.TotalTokens)
}
```

### `UsageObserver` — opt-in per-round-trip hook

The chat client never depends on a telemetry collector. To emit a telemetry event, metric, or log line, wire the opt-in `Config.UsageObserver` callback. It fires **once per provider round-trip** (synchronously, on the calling goroutine — keep it fast):

```go
cfg := chat.Config{
    Provider: chat.ProviderClaude,
    UsageObserver: func(u chat.Usage) {
        // e.g. emit a telemetry event; the chat client itself has no telemetry dependency
        collector.Track(ctx, "llm.usage", map[string]any{
            "input_tokens":  u.InputTokens,
            "output_tokens": u.OutputTokens,
            "total_tokens":  u.TotalTokens,
        })
    },
}
```

### Per-loop summing semantics

A single `Chat`, `Ask`, or `StreamChat` call may make **multiple** provider round-trips: a ReAct tool-calling loop makes one round-trip per step. Usage is **summed across the whole loop** — this is the figure you want for cost accounting.

- The `Usage()` accessor returns the running total across every round-trip (and every call) on that client.
- The `UsageObserver` hook fires once per round-trip, so you see each step individually and can aggregate however you like.

### Per-provider mapping

| Provider | Source | Mapping |
| :--- | :--- | :--- |
| Claude (Anthropic) | `Message.Usage` (and the streaming `message_delta` event) | `input_tokens` → `InputTokens`, `output_tokens` → `OutputTokens`, `cache_read_input_tokens` → `CachedTokens`; `TotalTokens` computed. |
| OpenAI / OpenAI-compatible | `ChatCompletion.Usage` | `prompt_tokens` → `InputTokens`, `completion_tokens` → `OutputTokens`, `total_tokens` → `TotalTokens`, plus cached/reasoning detail tokens. Streaming opts in to the final usage chunk automatically. |
| Gemini | `GenerateContentResponse.UsageMetadata` | `promptTokenCount` → `InputTokens`, `candidatesTokenCount` → `OutputTokens`, `totalTokenCount` → `TotalTokens`, cached/thoughts tokens mapped. |
| **ProviderClaudeLocal** | optional `usage` block of the `claude` CLI JSON output | Surfaced when the binary reports it; **otherwise `Usage{Known: false}`**. The local CLI does not guarantee per-call token counts, so do **not** rely on `ProviderClaudeLocal` for cost accounting — always check `Usage.Known`. |

A freshly-constructed client, and any provider that reports nothing for a call, returns a zero-valued `Usage` with `Known == false`. Always check `Known` before treating the counts as authoritative.

## In this section

- **[Providers](providers.md)** — Provider reference, the registry, and cross-provider fallback & routing.
- **[Conversation Persistence](persistence.md)** — Save and restore chat conversations across sessions.
- **[Reliability](reliability.md)** — Error handling and thread-safety guarantees.
- **[Best Practices](best-practices.md)** — Recommended patterns for building on the chat client.

### Related how-to guides

- [Add AI to your tool](../../../how-to/ai-integration.md) · [AI tool calling](../../../how-to/ai-tool-calling.md) · [Structured AI responses](../../../how-to/structured-ai-responses.md) · [Persist conversations](../../../how-to/persist-chat-conversations.md)
