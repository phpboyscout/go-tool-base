// Package chat provides a unified multi-provider AI chat client supporting
// Claude, OpenAI, Gemini, Claude Local (via CLI binary), and OpenAI-compatible
// endpoints.
//
// The [ChatClient] interface exposes five methods: Add (append a message), Chat
// (multi-turn conversation with tool use via a ReAct loop), Ask (structured output
// with JSON schema validation), SetTools (register callable tools), and Usage
// (token accounting). Streaming-capable providers also implement
// [StreamingChatClient] (StreamChat).
//
// Tool calling follows a JSON Schema parameter definition, and the ReAct loop
// automatically dispatches tool calls and feeds results back until the model
// produces a final text response. Per-provider token limits and maximum agent
// steps are configurable via [Config].
//
// New providers can be registered at runtime via [RegisterProvider]. Structured
// output helpers such as GenerateSchema simplify schema generation for Ask calls.
//
// # Multimodal input
//
// Add, Ask, Chat and StreamChat accept a trailing variadic of [Media] — images
// (and, on Gemini, PDF and A/V) sent alongside the text prompt. A text-only call
// passes no media and is unchanged. Each attachment's type is sniffed from its
// bytes (never a caller-supplied filename), cross-checked against any declared
// MIMEType, allowlisted, and checked against the selected provider's support
// before any network call; disguised or unsupported content is rejected with
// [ErrMediaRejected] or [ErrMediaUnsupported]. Vision support is per provider:
// Gemini (images, PDF, A/V), Claude and OpenAI (images); ProviderClaudeLocal
// accepts no media.
package chat
