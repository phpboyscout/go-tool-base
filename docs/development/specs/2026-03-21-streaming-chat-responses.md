---
title: "Streaming Chat Responses Specification"
description: "Add streaming support to the ChatClient interface for all three providers, enabling real-time partial response delivery."
date: 2026-03-21
status: IMPLEMENTED
tags:
  - specification
  - chat
  - streaming
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Streaming Chat Responses Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   IMPLEMENTED

---

## Overview

The current `ChatClient` interface only supports synchronous request/response interactions. For long-running AI responses, users see nothing until the full response is generated — which can take 10-30 seconds for complex queries. Streaming delivers partial responses as they're generated, providing immediate feedback and enabling progressive UI rendering.

All three provider SDKs (Anthropic, OpenAI, Google Gemini) support server-sent events (SSE) streaming natively.

---

## Design Decisions

**Separate `StreamingChatClient` interface**: Rather than adding streaming methods to `ChatClient`, define a new interface. Not all consumers need streaming, and not all providers may support it equally. Providers that support streaming implement both interfaces.

**Callback-based delivery**: Use a callback function `func(StreamEvent)` rather than channels. Callbacks are simpler to use correctly — no need to manage channel lifecycle, and backpressure is handled naturally (the callback blocks the stream).

**Event types**: Stream events include text deltas, tool calls, and completion signals. This maps directly to all three providers' SSE event formats.

**No streaming for Ask()**: `Ask()` returns structured data that must be complete before unmarshalling. Streaming only applies to `Chat()` and a new `StreamChat()` method.

**Tool call events — `EventToolCallStart` and `EventToolCallEnd` only**: `EventToolCallDelta` (partial argument streaming) is omitted from this iteration. Callback consumers receive `EventToolCallStart` when a tool call begins execution and `EventToolCallEnd` when it completes, providing enough signal for TUI progress indicators without the complexity of streaming partial JSON arguments. These events are for real-time in-stream notification to callback consumers, not for the telemetry framework (which operates at coarser command-level granularity).

**Gemini streaming via `Chat.SendStream`**: The existing non-streaming `Chat()` implementation uses `g.client.Chats.Create` → `chat.Send`. The streaming path uses the same `Chat` object's `SendStream` method, which is the direct streaming parallel. `SendStream` automatically manages conversation history (appends input and assembled response to `curatedHistory`), so no manual history tracking is needed for Gemini in `StreamChat`.

**History management**: After a successful `StreamChat`, conversation history must be updated so that multi-turn conversations and `Save()`/`Restore()` (see chat-conversation-persistence spec) work correctly. For Gemini this is handled automatically by `Chat.SendStream`. For Claude and OpenAI the assembled response is appended to their respective message history the same way `Chat()` does.

**Parallel tools in `StreamChat`**: `Config.ParallelTools` and `Config.MaxParallelTools` are respected. When the stream ends with multiple tool calls, they are executed using the same `executeToolsParallel` branch logic as `Chat()` before the next stream iteration begins.

---

## Public API Changes

### New Interface: `StreamingChatClient`

```go
// StreamingChatClient extends ChatClient with streaming support.
// Implementations that support streaming implement this interface
// in addition to ChatClient.
type StreamingChatClient interface {
    ChatClient

    // StreamChat sends a message and streams the response via the callback.
    // The callback is invoked for each event in the stream. If the callback
    // returns an error, the stream is cancelled.
    // The final return value is the complete response text (concatenation of
    // all text deltas) or an error.
    StreamChat(ctx context.Context, prompt string, callback StreamCallback) (string, error)
}

// StreamCallback receives streaming events. Return a non-nil error to cancel the stream.
type StreamCallback func(event StreamEvent) error

// StreamEvent represents a single event in a streaming response.
type StreamEvent struct {
    // Type indicates the kind of event.
    Type StreamEventType

    // Delta contains the text fragment for TextDelta events.
    Delta string

    // ToolCall contains tool call information for ToolCallStart/ToolCallDelta events.
    ToolCall *StreamToolCall

    // Error contains error information for Error events.
    Error error
}

// StreamEventType identifies the kind of stream event.
type StreamEventType int

const (
    // EventTextDelta is a partial text response.
    EventTextDelta StreamEventType = iota
    // EventToolCallStart indicates a tool call is beginning.
    EventToolCallStart
    // EventToolCallDelta contains partial tool call arguments.
    EventToolCallDelta
    // EventToolCallEnd indicates a tool call is complete and will be executed.
    EventToolCallEnd
    // EventComplete indicates the stream has finished successfully.
    EventComplete
    // EventError indicates an error occurred during streaming.
    EventError
)

// StreamToolCall contains information about a streaming tool call.
type StreamToolCall struct {
    ID        string
    Name      string
    Arguments string // partial or complete JSON arguments
}
```

### Type Assertion Pattern

```go
// Consumer usage:
client, err := chat.New(ctx, props, cfg)
if streamer, ok := client.(chat.StreamingChatClient); ok {
    result, err := streamer.StreamChat(ctx, "prompt", func(e chat.StreamEvent) error {
        if e.Type == chat.EventTextDelta {
            fmt.Print(e.Delta) // progressive output
        }
        return nil
    })
}
```

---

## Internal Implementation

### Claude Streaming

```go
func (c *Claude) StreamChat(ctx context.Context, prompt string, callback StreamCallback) (string, error) {
    c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)))

    params := anthropic.MessageNewParams{
        Model:     c.model,
        MaxTokens: c.maxTokens,
        Messages:  c.messages,
        System:    []anthropic.TextBlockParam{{Text: c.system}},
    }

    stream := c.client.Messages.NewStreaming(ctx, params)
    var fullText strings.Builder

    for stream.Next() {
        event := stream.Current()
        switch e := event.(type) {
        case *anthropic.ContentBlockDeltaEvent:
            if e.Delta.Type == "text_delta" {
                fullText.WriteString(e.Delta.Text)
                if err := callback(StreamEvent{Type: EventTextDelta, Delta: e.Delta.Text}); err != nil {
                    return fullText.String(), err
                }
            }
        case *anthropic.MessageStopEvent:
            callback(StreamEvent{Type: EventComplete})
        }
    }

    if err := stream.Err(); err != nil {
        return fullText.String(), errors.Wrap(err, "claude stream error")
    }

    return fullText.String(), nil
}
```

### OpenAI Streaming

```go
func (a *OpenAI) StreamChat(ctx context.Context, prompt string, callback StreamCallback) (string, error) {
    a.params.Messages = append(a.params.Messages, openai.UserMessage(prompt))
    a.params.StreamOptions = &openai.ChatCompletionStreamOptionsParam{IncludeUsage: true}

    stream := a.oai.Chat.Completions.NewStreaming(ctx, a.params)
    var fullText strings.Builder

    for stream.Next() {
        chunk := stream.Current()
        for _, choice := range chunk.Choices {
            if choice.Delta.Content != "" {
                fullText.WriteString(choice.Delta.Content)
                if err := callback(StreamEvent{Type: EventTextDelta, Delta: choice.Delta.Content}); err != nil {
                    return fullText.String(), err
                }
            }
        }
    }

    if err := stream.Err(); err != nil {
        return fullText.String(), errors.Wrap(err, "openai stream error")
    }

    callback(StreamEvent{Type: EventComplete})
    return fullText.String(), nil
}
```

### Gemini Streaming

Uses `Chat.SendStream` (the streaming parallel to `Chat.Send` used by the non-streaming path). History is managed automatically by the SDK — `SendStream` appends input and the assembled response to `curatedHistory` internally, so no manual history tracking is needed.

```go
func (g *Gemini) StreamChat(ctx context.Context, prompt string, callback StreamCallback) (string, error) {
    chatCfg := g.cloneConfig()
    chatCfg.ResponseMIMEType = ""
    chatCfg.ResponseSchema = nil

    chat, err := g.client.Chats.Create(ctx, g.model, chatCfg, g.history)
    if err != nil {
        return "", errors.Newf("failed to create gemini chat session: %w", err)
    }

    var fullText strings.Builder
    for chunk, err := range chat.SendStream(ctx, genai.NewPartFromText(prompt)) {
        if err != nil {
            return fullText.String(), g.handleGeminiError(err, 0)
        }
        if text := chunk.Text(); text != "" {
            fullText.WriteString(text)
            if cbErr := callback(StreamEvent{Type: EventTextDelta, Delta: text}); cbErr != nil {
                return fullText.String(), cbErr
            }
        }
        // Tool calls are handled after the stream completes (see ReAct loop).
    }

    _ = callback(StreamEvent{Type: EventComplete})
    return fullText.String(), nil
}
```

### Tool Calls During Streaming

When a tool call appears in the stream, the streaming pauses, the tool is executed (non-streaming), and the result is fed back into a new streaming request. This mirrors the existing ReAct loop but with streaming output for the non-tool-call portions.

```go
// Simplified streaming ReAct loop
for step := range maxSteps {
    // Stream response
    // If tool calls detected → execute tools → continue loop
    // If no tool calls → stream is complete
}
```

### ClaudeLocal

`ClaudeLocal` uses a CLI subprocess and does not support streaming. It implements `ChatClient` only, not `StreamingChatClient`. The type assertion pattern handles this gracefully.

---

## Project Structure

```
pkg/chat/
├── client.go          ← MODIFIED: StreamingChatClient interface, event types
├── streaming.go       ← NEW: StreamEvent, StreamCallback, StreamEventType
├── claude.go          ← MODIFIED: StreamChat implementation
├── openai.go          ← MODIFIED: StreamChat implementation
├── gemini.go          ← MODIFIED: StreamChat implementation
├── claude_local.go    ← UNCHANGED: does not implement StreamingChatClient
├── streaming_test.go  ← NEW: streaming tests
```

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestClaude_StreamChat_Success` | Mock SSE stream → callback receives text deltas and complete event |
| `TestOpenAI_StreamChat_Success` | Mock SSE stream → same |
| `TestGemini_StreamChat_Success` | Mock stream → same |
| `TestStreamChat_CallbackError` | Callback returns error → stream cancelled, partial text returned |
| `TestStreamChat_ContextCancelled` | Context cancelled mid-stream → appropriate error |
| `TestStreamChat_EmptyResponse` | Provider returns empty stream → empty string, no error |
| `TestStreamChat_WithToolCalls` | Stream contains tool call → tool executed, stream continues |
| `TestClaudeLocal_NotStreaming` | Type assertion → `StreamingChatClient` not satisfied |
| `TestStreamEvent_Types` | All event types have correct values |

### Mock SSE Server

```go
func newMockSSEServer(t *testing.T, events []string) *httptest.Server {
    t.Helper()
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        flusher := w.(http.Flusher)
        for _, event := range events {
            fmt.Fprintf(w, "data: %s\n\n", event)
            flusher.Flush()
        }
    }))
}
```

### Integration Tests

- **Live SSE round-trip**: Start an `httptest.Server` that emits a multi-event SSE stream, configure a provider client with its URL, call `StreamChat`, and assert the callback receives all deltas in order and the final assembled text is correct.
- **Stream cancellation under load**: Start a long-running mock SSE stream, cancel the context mid-stream, and verify the client returns promptly with the partial result and a context error.
- **Tool call during stream**: Mock SSE server emits a tool-call event mid-stream, verify the tool is executed and the stream resumes with the tool result.
- Gate with `testutil.SkipIfNotIntegration(t, "chat")` in a dedicated `streaming_integration_test.go` file.

### E2E BDD Tests (Godog) — **Not applicable**

Streaming is a pure library feature with no CLI surface. SSE protocol handling and callback orchestration are best verified through the unit tests and integration tests above. See the [suitability assessment](2026-03-28-godog-bdd-strategy.md#suitability-assessment) for guidance on when Godog adds value.

### Coverage
- Target: 90%+ for `pkg/chat/` including streaming paths.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for `StreamingChatClient` interface and all event types.
- Godoc explaining the callback contract (blocking, error cancellation).
- Update `docs/components/chat.md` with streaming usage examples and the type assertion pattern.

---

## Backwards Compatibility

- **No breaking changes**. `ChatClient` interface is unchanged. Streaming is opt-in via type assertion.
- Providers that implement `StreamingChatClient` still satisfy `ChatClient`.
- `ClaudeLocal` is explicitly excluded — this is documented, not a limitation.

---

## Future Considerations

- **Streaming Ask()**: If a structured response schema can be progressively validated, streaming `Ask()` could deliver partial results. Complex and likely not worth the effort.
- **WebSocket transport**: For long-lived connections, WebSocket may be more efficient than SSE. Provider SDKs would need to support this.
- **Stream recording**: For debugging, record stream events to a file for replay.

---

## Implementation Phases

### Phase 1 — Interface and Types
1. Define `StreamingChatClient` interface
2. Define `StreamEvent`, `StreamCallback`, `StreamEventType`
3. Add compile-time checks

### Phase 2 — Provider Implementations
1. Implement Claude `StreamChat`
2. Implement OpenAI `StreamChat`
3. Implement Gemini `StreamChat`
4. Handle tool calls within streaming

### Phase 3 — Tests
1. Create mock SSE servers
2. Add success, error, and cancellation tests for each provider
3. Add tool call streaming tests
4. Run with race detector

---

## Verification

```bash
go build ./...
go test -race ./pkg/chat/...
go test ./...
golangci-lint run --fix

# Verify streaming interface exists
grep -n 'StreamingChatClient' pkg/chat/client.go

# Verify implementations
grep -n 'func.*StreamChat' pkg/chat/claude.go pkg/chat/openai.go pkg/chat/gemini.go
```
