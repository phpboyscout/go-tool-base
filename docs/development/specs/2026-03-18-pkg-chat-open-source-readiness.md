---
title: "pkg/chat Open-Source Readiness Specification"
description: "Migrate the Gemini SDK off the deprecated google/generative-ai-go, introduce a ProviderFactory registry for external extensibility, replace the ClaudeCodeLocal stub with a first-class ProviderClaudeLocal implementation, and add ProviderOpenAICompatible for OpenAI-compatible backends."
date: 2026-03-18
status: IMPLEMENTED
tags:
  - specification
  - chat
  - ai
  - gemini
  - open-source
  - provider
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-sonnet-4-6)
    role: AI drafting assistant
---

# pkg/chat Open-Source Readiness Specification

Authors
:   Matt Cockayne, Claude (claude-sonnet-4-6) *(AI drafting assistant)*

Date
:   18 March 2026

Status
:   IMPLEMENTED

---

## Overview

`pkg/chat` provides GTB's unified AI client interface over OpenAI, Claude, and Gemini. Before open-sourcing the GTB project, four issues need resolving:

1. **Gemini SDK deprecation** — `github.com/google/generative-ai-go` reached end-of-life November 30, 2025.
2. **No extension point** — adding a provider requires modifying `client.go` directly; external contributors cannot register providers from their own packages.
3. **Config pollution** — `ClaudeCodeLocal bool` is a provider-specific concern in the shared `Config` struct, and its implementation (`askLocal`) is an unimplemented stub.
4. **No OpenAI-compatible backends** — Ollama, Groq, Fireworks, LM Studio and most new entrants use an OpenAI-compatible API but cannot be targeted without forking the package.

Additionally, the `ClaudeCodeLocal` feature — routing through a locally installed `claude` CLI binary — is genuinely valuable for secure environments where direct API access to `api.anthropic.com` is blocked but the pre-authenticated `claude` binary is permitted. This spec implements it properly as a first-class provider.

---

## Design Decisions

**ProviderFactory registry over switch statement:** A thread-safe global registry with a `RegisterProvider()` function and `init()`-based self-registration is the idiomatic Go pattern for this type of extensibility. It allows external packages to register providers without importing or modifying `client.go`.

**ProviderClaudeLocal via subprocess:** The `claude` CLI supports `--output-format json` for structured single-shot responses, `--resume <session_id>` for multi-turn continuity, `--json-schema` for validated structured output, and `--system-prompt` / `--model` for configuration. This gives us everything needed to implement all `ChatClient` methods except `SetTools`, which requires an MCP server integration deferred to a future spec.

**BaseURL field on Config:** Rather than a completely separate implementation, OpenAI-compatible providers share `newOpenAI()` — the only difference is `option.WithBaseURL()` and the absence of a default model. A `ProviderOpenAICompatible` constant makes intent clear in user code.

**Remove ClaudeCodeLocal entirely:** The field exposed a leaking concern and the implementation was a stub. The feature it gestured at is now properly implemented as `ProviderClaudeLocal` via the registry.

---

## Public API Changes

### `client.go` — `Config` struct

```go
// Added:
BaseURL string  // overrides API endpoint; required for ProviderOpenAICompatible

// Removed:
ClaudeCodeLocal bool  // replaced by ProviderClaudeLocal
```

### `client.go` — New provider constants

```go
ProviderClaudeLocal      Provider = "claude-local"
ProviderOpenAICompatible Provider = "openai-compatible"
```

### `client.go` — New exported symbols

```go
// ProviderFactory creates a ChatClient for a named provider.
type ProviderFactory func(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error)

// RegisterProvider registers a factory for a provider name.
// Call from an init() function in provider files or external packages.
func RegisterProvider(name Provider, factory ProviderFactory)
```

---

## Internal Implementation

### Task 1: Migrate Gemini SDK

**Files:** `go.mod`, `pkg/chat/gemini.go`, `pkg/chat/gemini_schema.go`

Replace `github.com/google/generative-ai-go v0.20.1` with `google.golang.org/genai`.

Key API differences in the new SDK:

| Old (`google/generative-ai-go`) | New (`google.golang.org/genai`) |
|---|---|
| `genai.NewClient(ctx, option.WithAPIKey(k))` | `genai.NewClient(ctx, &genai.ClientConfig{APIKey: k})` |
| `client.GenerativeModel(name)` + `model.StartChat()` | History managed manually; call `client.Models.GenerateContent()` |
| `model.ResponseMIMEType`, `model.ResponseSchema` | Config passed per-call via `genai.GenerateContentConfig` |
| `genai.FunctionDeclaration`, `genai.Tool` | Same types, different package path |

All existing behaviour is preserved: agentic tool loop (max 20 steps), structured output, error parsing, history management.

### Task 2: ProviderFactory Registry

**Files:** `pkg/chat/client.go`, `pkg/chat/openai.go`, `pkg/chat/claude.go`, `pkg/chat/gemini.go`, `pkg/chat/client_test.go`

```go
// client.go
var (
    providerRegistry = map[Provider]ProviderFactory{}
    registryMu       sync.RWMutex
)

func RegisterProvider(name Provider, factory ProviderFactory) {
    registryMu.Lock()
    defer registryMu.Unlock()
    providerRegistry[name] = factory
}

func New(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
    if cfg.Provider == "" {
        if envProvider := os.Getenv(EnvAIProvider); envProvider != "" {
            cfg.Provider = Provider(envProvider)
        } else {
            cfg.Provider = ProviderOpenAI
        }
    }
    registryMu.RLock()
    factory, ok := providerRegistry[cfg.Provider]
    registryMu.RUnlock()
    if !ok {
        return nil, errors.Newf("unsupported provider: %s", cfg.Provider)
    }
    return factory(ctx, p, cfg)
}
```

Built-in providers self-register:
```go
func init() { chat.RegisterProvider(ProviderOpenAI, newOpenAI) }       // openai.go
func init() { chat.RegisterProvider(ProviderClaude, newClaude) }       // claude.go
func init() { chat.RegisterProvider(ProviderGemini, newGemini) }       // gemini.go
func init() { chat.RegisterProvider(ProviderClaudeLocal, newClaudeLocal) } // claude_local.go
```

**Test fix:** `client_test.go` `"default provider is Claude"` test name and assertion are wrong — the actual default is `ProviderOpenAI`. Fix to assert "OpenAI token is required".

### Task 3: ProviderClaudeLocal

**Files:** `pkg/chat/client.go` (removals + constant), `pkg/chat/claude.go` (removals), `pkg/chat/claude_local.go` (new), `pkg/chat/claude_local_test.go` (new)

**Removals from `claude.go`:**
- Guard `&& !cfg.ClaudeCodeLocal` in `newClaude()`
- `if c.cfg.ClaudeCodeLocal` branch in `Ask()`
- `askLocal()` stub method

**`claude_local.go` implementation:**

```go
type ClaudeLocal struct {
    ctx       context.Context
    props     *props.Props
    cfg       Config
    sessionID string   // captured from first response; used for --resume
    pending   []string // buffered Add() messages, prepended to next prompt
}
```

**`newClaudeLocal`:** No API key required. Calls `exec.LookPath("claude")` at construction — returns an error with install instructions if not found.

**`Add(prompt string) error`:** Appends to `pending`. Buffered messages are prepended (with `\n\n---\n\n` separator) to the combined prompt on the next `Chat()` or `Ask()` call.

**`Chat(ctx, prompt string) (string, error)`:** Invokes `claude -p "<combined>" --output-format json [--system-prompt ...] [--model ...] [--resume <id>]`. Parses the JSON result:
```json
{"type": "result", "result": "...", "session_id": "...", "is_error": false}
```
Captures `session_id` for multi-turn continuity. Returns `result` field.

**`Ask(question string, target any) error`:** Same as `Chat()`, plus `--json-schema '<schema>'` using the schema generated from `cfg.ResponseSchema`. Unmarshals `result` into `target`.

**`SetTools(tools []Tool) error`:** Phase 1 — returns an informative error. Phase 2 (future spec) will expose tools via a local MCP server and pass `--mcp-config` to the subprocess.

### Task 4: ProviderOpenAICompatible

**Files:** `pkg/chat/client.go`, `pkg/chat/openai.go`, `pkg/chat/client_test.go`, `pkg/chat/openai_test.go`

**`newOpenAI()` changes:**
- When `cfg.BaseURL != ""`: pass `option.WithBaseURL(cfg.BaseURL)` to `openai.NewClient()`
- When `cfg.Provider == ProviderOpenAICompatible && cfg.Model == ""`: return an error (no sensible default for non-OpenAI model names)

**Registration:**
```go
func init() {
    RegisterProvider(ProviderOpenAI, newOpenAI)
    RegisterProvider(ProviderOpenAICompatible, newOpenAI)
}
```

**Tokenizer fallback in `chunkByTokens()`:** Unknown model names (e.g. `llama3.2`, `mixtral`) currently cause a hard error. Fall back to `cl100k_base` with a debug log:
```go
enc, err := tokenizer.ForModel(tokenizer.Model(model))
if err != nil {
    enc, err = tokenizer.Get(tokenizer.Cl100kBase)
    if err != nil {
        return nil, errors.Newf("failed to get fallback tokenizer: %w", err)
    }
}
```

---

## Project Structure

```
pkg/chat/
├── client.go           MODIFIED: ProviderFactory, RegisterProvider, registry in New(),
│                                 add ProviderClaudeLocal + ProviderOpenAICompatible constants,
│                                 add BaseURL to Config, remove ClaudeCodeLocal from Config
├── openai.go           MODIFIED: init() registration, BaseURL support, tokenizer fallback,
│                                 ProviderOpenAICompatible model guard
├── claude.go           MODIFIED: init() registration, remove ClaudeCodeLocal guards + askLocal()
├── gemini.go           MODIFIED: init() registration, migrate to google.golang.org/genai
├── gemini_schema.go    MODIFIED: update genai.Schema type references for new SDK
├── claude_local.go     NEW: ProviderClaudeLocal implementation
├── client_test.go      MODIFIED: fix default provider test, add registry + new provider tests
├── openai_test.go      MODIFIED: add tokenizer fallback test
└── claude_local_test.go NEW: constructor path-check, Add() buffering, SetTools() error
go.mod                  MODIFIED: remove google/generative-ai-go, add google.golang.org/genai
```

---

## Testing Strategy

| File | Tests |
|---|---|
| `client_test.go` | Registry dispatch via mock factory; fixed default provider (OpenAI); `ProviderClaudeLocal` binary-not-found error; `ProviderOpenAICompatible` missing-model error |
| `openai_test.go` | `chunkByTokens` with unknown model name falls back to `cl100k_base` |
| `claude_local_test.go` | Constructor returns error when `claude` not in PATH; `Add()` buffers and clears; `SetTools()` returns informative error |

---

## Backwards Compatibility

- The `ChatClient` interface is **unchanged** — no consumer code is broken.
- `Config.ClaudeCodeLocal` is **removed** — any callers setting this field will get a compile error (intentional: the field was non-functional).
- `Config.BaseURL` is **additive** — existing callers are unaffected.
- `RegisterProvider` is **additive** — existing `New()` callers are unaffected.

---

## Verification

```bash
# All unit tests must pass
go test ./pkg/chat/...

# Deprecated import gone (Task 1)
grep "generative-ai-go" go.mod go.sum pkg/chat/

# Registry replaces switch (Task 2)
grep "switch cfg.Provider" pkg/chat/client.go

# ClaudeCodeLocal fully removed (Task 3)
grep -r "ClaudeCodeLocal" pkg/

# New symbols present (Tasks 3 & 4)
grep "ProviderClaudeLocal\|ProviderOpenAICompatible\|BaseURL" pkg/chat/client.go
```

---

## Future Considerations

- **ProviderClaudeLocal SetTools via MCP**: When tool calling is needed for local mode, expose user-defined `Tool` handlers through a short-lived in-process MCP server and pass `--mcp-config` to the `claude` subprocess.
- **Streaming support**: The `ChatClient` interface currently returns a complete response string. Streaming could be added as an optional interface (`StreamingChatClient`) without breaking existing consumers.
- **Provider version negotiation**: The `ProviderFactory` registry could be extended to include capability metadata (supported features) so `New()` can warn consumers when a requested feature is unavailable for the chosen provider.
