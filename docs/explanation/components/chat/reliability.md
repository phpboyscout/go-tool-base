---
title: Reliability
description: Error handling and thread-safety guarantees.
date: 2026-03-18
tags: [components, chat, ai, llm]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Reliability

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
