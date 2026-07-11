---
title: Providers
description: Provider reference, the registry, and cross-provider fallback & routing.
date: 2026-03-18
tags: [components, chat, ai, llm]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Providers

## Provider Reference

### Provider Constants

| Constant | String Value | API Key Required |
| :--- | :--- | :--- |
| `chat.ProviderOpenAI` | `"openai"` | Yes — `OPENAI_API_KEY` |
| `chat.ProviderClaude` | `"claude"` | Yes — `ANTHROPIC_API_KEY` |
| `chat.ProviderGemini` | `"gemini"` | Yes — `GEMINI_API_KEY` |
| `chat.ProviderClaudeLocal` | `"claude-local"` | No — uses local `claude` binary |
| `chat.ProviderOpenAICompatible` | `"openai-compatible"` | Backend-dependent (set via `Token`) |

The default provider when `Config.Provider` is empty and `AI_PROVIDER` is not
set is `ProviderClaude`.

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
client, err := chat.NewFromProps(ctx, p, chat.Config{
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
client, err := chat.NewFromProps(ctx, p, chat.Config{
    Provider: chat.ProviderOpenAICompatible,
    BaseURL:  "http://localhost:11434/v1",
    Model:    "llama3.2",
    Token:    "ollama", // Ollama ignores the token; any non-empty value works
})

// Groq (cloud)
client, err := chat.NewFromProps(ctx, p, chat.Config{
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

After importing your package, `chat.NewFromProps(ctx, p, chat.Config{Provider: "my-backend"})` routes to your factory.

## Cross-provider fallback & routing

A single provider can rate-limit (HTTP 429), suffer an outage (5xx), or be
unreachable. `chat.NewFallback` wraps an **ordered list** of clients and, on a
*retryable* failure from the active one, transparently advances to the next. The
composite **is** a `ChatClient` (and a `StreamingChatClient` iff every wrapped
client streams), so callers are unchanged.

```go
// Wrap already-constructed clients (first is primary, rest are fallbacks):
client, err := chat.NewFallback([]chat.ChatClient{primary, secondary})

// Or build from configs (each provider resolves its own credentials + model):
client, err := chat.NewFallbackFromConfigs(ctx, props, []chat.Config{
    {Provider: chat.ProviderClaude, Model: "claude-opus-4-8"},
    {Provider: chat.ProviderOpenAI},                          // uses OpenAI's default model
    {Provider: chat.ProviderGemini},
})
```

### Config surface (opt-in, default off)

```yaml
ai:
  provider: claude          # single-provider default (unchanged)
  fallback:
    enabled: false          # default — single-provider behaviour is byte-for-byte preserved
    providers: [claude, openai, gemini]   # ordered; index 0 is primary
```

When `ai.fallback.enabled` is true and `ai.fallback.providers` is non-empty,
`chat.NewWithFallback` (used by the AI call sites) builds a composite; otherwise
it is exactly `chat.New`. `providers[0]` is the primary and **overrides**
`ai.provider` (a WARN is logged on disagreement). A fallback provider whose
credentials are missing is dropped with a WARN (endpoint host only); the
**primary's** absence is fatal.

> **Known limitation — per-provider model.** On the config-driven path each
> provider in the chain uses **its own default model**; the global `ai.model`
> key is *not* applied per provider (a single model name rarely makes sense
> across Claude/OpenAI/Gemini). To pin a specific model per provider, construct
> the chain explicitly with `chat.NewFallbackFromConfigs` and set each
> `Config.Model`.

### Which errors trigger failover

The default policy (`chat.DefaultFailoverPolicy`, overridable via
`WithFailoverPolicy`) advances on transient/unavailable conditions and treats
operator-fixable faults as fatal:

| Advance (`FailoverNext`) | Fatal (`FailoverFatal`) |
|--------------------------|-------------------------|
| 408, 429, 500, 502, 503, 504 | 400, 401, 403, 404, 422 |
| network errors (DNS, connection refused/reset, TLS) | caller-cancelled context |
| per-request timeout | claude-local non-zero exit (operator-fixable) |

Status is extracted via `errors.As` against the SDK error types
(`*anthropic.Error`, `*openai.Error`, `*genai.APIError`), so the providers'
`errors.Wrap` layers are transparent. The policy never inspects error *messages*.

### Behaviour across a failover boundary — known limitations

- **Lossy transcript replay.** The composite keeps a provider-neutral transcript
  of user/assistant **text** turns and replays the *user* turns into a fallback
  provider on first use (assistant turns and tool-call/tool-result interleaving
  cannot be reconstructed). A conversation that did heavy tool use before
  failover resumes with reduced context. Pass `WithStrictToolContext` to fail
  fast instead, once a tool has executed.
- **Tools** re-apply cleanly (handlers are provider-agnostic).
- **Streaming** fails over **only before the first visible event**
  (`EventTextDelta`/`EventToolCallStart`) reaches your callback — once a delta is
  emitted it cannot be un-emitted, so a later error is terminal.
- **Usage** is the **sum** across every provider the composite drove (so a
  failover's combined spend is visible).

### Logging & redaction

Each transition logs exactly one WARN line — `chat provider failover` with
`from`/`to` (enum names) and a coarse `reason` (`status`/`network`). The
triggering error's message is **never** logged verbatim; any endpoint detail is
reduced to the host only, consistent with the chat client's existing rule (see
[`redact`](../redact.md)).
