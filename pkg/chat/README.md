# Chat (go-tool-base adapter)

This package is go-tool-base's **thin adapter** over the standalone multi-provider
AI chat client, [`gitlab.com/phpboyscout/go/chat`](https://gitlab.com/phpboyscout/go/chat).

The client itself — the `ChatClient` API, ReAct tool-calling loop, streaming,
cross-provider fallback, persistence, and multimodal input — lives in the module
and its per-provider modules (`chat-anthropic`, `chat-openai`, `chat-gemini`).
Full docs: **[chat.go.phpboyscout.uk](https://chat.go.phpboyscout.uk)**.

This adapter adds go-tool-base's framework integration:

- `SettingsFromProps` / `NewFromProps` / `NewWithFallbackFromProps` — map a
  `Props` instance and GTB's Viper config into the module's typed `Settings`.
- the GTB config-key schema (`ConfigKeyClaudeKey`, `ConfigKeyAIProvider`, …) and
  the credential-resolution precedence.
- the hardened `pkg/http` transport and `pkg/credentials` keychain lookup,
  injected into the module via its seams.
- blank-imports of all three provider modules, so every provider is registered.

It also re-exports the module's public types/constructors (`chat.Config`,
`chat.New`, `chat.Tool`, …) so existing GTB call sites reference them from this
package unchanged. See `docs/explanation/components/chat/index.md` and the
migration note at `docs/reference/migration/v0.x-chat-extracted.md`.
