# Chat (go-tool-base adapter)

This package is go-tool-base's **thin adapter** over the standalone multi-provider
AI chat client, [`gitlab.com/phpboyscout/go/chat`](https://gitlab.com/phpboyscout/go/chat).

The client itself — the `ChatClient` API, ReAct tool-calling loop, streaming,
cross-provider fallback, persistence, and multimodal input — lives in the module
and its per-provider modules (`chat-anthropic`, `chat-openai`, `chat-gemini`).
Full docs: **[chat.go.phpboyscout.uk](https://chat.go.phpboyscout.uk)**.

This adapter adds go-tool-base's framework integration:

- `SettingsFromProps` / `NewFromProps` / `NewWithFallbackFromProps` — map a
  `Props` instance and its `go/config` store into the module's typed
  `Settings`, taking the caller's `gochat.Config` as the starting point.
- the GTB config-key schema (`ConfigKeyClaudeKey`, `ConfigKeyAIProvider`, …),
  `CredentialKeysFor` and the credential-resolution precedence, declared to
  `credentialposture` so `doctor` reports them.
- the hardened `go/httpclient` transport and the `go/credentials` keychain
  lookup, injected into the module via its seams.
- the provider-to-module table (`ProviderModule`, `ProviderModules`) and
  `LinkedProviders`, which reads what the binary declares it links.

**It registers no provider.** A provider is a blank import in the binary that
ships it (spec 0194): `cli/cmd/gtb/providers.go` links every module, and a
generated tool links the ones its manifest selects. Nothing is re-exported:
call sites import `gitlab.com/phpboyscout/go/chat` for the client types and
this package for the GTB glue. See `docs/explanation/components/chat/index.md`
and the migration note at `docs/reference/migration/v0.x-chat-extracted.md`.
