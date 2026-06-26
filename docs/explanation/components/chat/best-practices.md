---
title: Best Practices
description: Recommended patterns for building on the chat client.
date: 2026-03-18
tags: [components, chat, ai, llm]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Best Practices

## Best Practices

- **Context Management**: All methods that perform I/O (`Add`, `Ask`, `Chat`) accept `context.Context` as the first parameter. Always pass an appropriate context to ensure operations can be cancelled or timed out.
- **One Client Per Goroutine**: Do not share a `ChatClient` instance across goroutines. Create a new client for each concurrent conversation.
- **System Prompts**: Use `SystemPrompt` in the config to define the AI's persona and constraints.
- **Validation**: Validate AI outputs before using them in critical code paths, even when using structured `Ask` responses.
- **Token Limits**: Be mindful of token limits when building conversation history; consider summarizing or truncating long sessions.
- **Rate Limiting**: Implement appropriate backoff when encountering rate limit errors.
- **Local vs. API**: Prefer `ProviderClaudeLocal` only when API access is restricted; API providers offer lower latency and full feature support.
