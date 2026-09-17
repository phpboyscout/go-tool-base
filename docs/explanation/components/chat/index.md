---
title: Chat
description: go-tool-base's adapter over the standalone go/chat multi-provider AI client.
date: 2026-07-13
tags: [components, chat, ai, llm]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI Chat

The multi-provider AI chat client has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/chat`](https://gitlab.com/phpboyscout/go/chat)
module** (plus per-provider modules: `chat-anthropic`, `chat-openai`,
`chat-gemini`, `chat-bedrock`, `chat-openai-azure`). Its full
documentation: the `ChatClient` API, the ReAct tool-calling loop, streaming,
cross-provider fallback, conversation persistence, multimodal input, token-usage
accounting, and the provider capability matrix. Now lives at:

> **[chat.go.phpboyscout.uk](https://chat.go.phpboyscout.uk)**

go-tool-base consumes the module through a thin adapter in
`pkg/chat`; this page documents only that **adapter**. See the
[migration note](../../../reference/migration/v0.x-chat-extracted.md) for the
module map and how to consume the light client directly.

## What the GTB adapter adds

The module is deliberately config-system-agnostic. `pkg/chat` layers GTB's
framework integration on top:

- **Props construction.** `chat.NewFromProps(ctx, p, cfg)` and
  `chat.NewWithFallbackFromProps(...)` map a `Props` instance and GTB's layered
  config (read through a pinned `props.Config.View()`) into the module's typed
  `chat.Settings`, then call the module constructor. `chat.SettingsFromProps`
  exposes just the mapping.
- **The GTB config-key schema.** The adapter owns the config keys and their
  precedence; the module knows nothing about them:

  | Provider | Literal key | Env-var-reference key | Keychain key | Ecosystem fallback env |
  |---|---|---|---|---|
  | Claude | `anthropic.api.key` | `anthropic.api.env` | `anthropic.api.keychain` | `ANTHROPIC_API_KEY` |
  | OpenAI | `openai.api.key` | `openai.api.env` | `openai.api.keychain` | `OPENAI_API_KEY` |
  | Gemini, Gemini Vertex | `gemini.api.key` | `gemini.api.env` | `gemini.api.keychain` | `GEMINI_API_KEY` |
  | Azure OpenAI | `azure.api.key` | `azure.api.env` | `azure.api.keychain` | `AZURE_OPENAI_API_KEY` |

  `openai-compatible` shares OpenAI's root. The local CLIs (`claude-local`,
  `codex-local`, `agy-local`) and `bedrock` carry no GTB credential:
  `chat.NeedsCredential(p)` says which. The provider is chosen by
  `ai.provider` (or `AI_PROVIDER` when that is unset); fallback is configured
  under `ai.fallback.*`. Resolution precedence: direct token → env-var
  reference → OS keychain → literal → ecosystem env var. The recommended path
  (env-var reference) keeps the literal secret out of the config file.
- **The whole `ai:` section reaches the client.** `ai.model`, `ai.base_url`,
  `ai.api_version`, `ai.project` and `ai.location` fill the matching
  `chat.Config` fields when the caller left them empty, so `openai-compatible`,
  `azure-openai`, `gemini-vertex` and `bedrock` are configurable from a file.
  They apply to `ai.provider` only. A fallback chain is built by the module's
  `NewWithFallbackSettings`: the primary keeps its model and addressing, every
  other member resolves its own, and GTB supplies each member's credential
  through `WithProviderCredentials`. GTB used to derive the chain itself with a
  denylist that cleared the primary's model and would have carried addressing
  to every member.
- **Three layers, three owners.** A generated tool's chat configuration is
  three things that used to blur together. The *author's defaults* (default
  provider, model, endpoint) live in the manifest's `chat.default` block and
  ship as an embedded defaults bundle beside `cmd/<name>/chat.go`, the lowest
  config layer. The *end user's overrides* live in their own config file,
  written by `init ai` or by hand, and win over the author's. *Credentials*
  are only ever the end user's, in their file or their environment; nothing
  the author records carries one. See spec 0196 and the
  [generate reference](../../../reference/cli/generate.md).
- **Hardened HTTP + keychain seams.** The adapter injects `pkg/http`'s hardened
  transport and wires `pkg/credentials.Retrieve` as the keychain lookup, so GTB
  tools get the framework's security posture; the module core carries neither.
- **No provider registered here.** A provider is a blank import in the binary
  that ships it (spec 0194): `cli/cmd/gtb/providers.go` links every module, and
  a generated tool links the ones its manifest selects. A hand-wired tool adds
  the imports itself; see the
  [migration note](../../../reference/migration/v0.x-adapters-registered-by-main.md).
- **Provider to module table.** `chat.ProviderModule(p)` and
  `chat.ProviderModules()` name the module whose blank import registers each
  `chat.Provider`. The generator, `doctor` and error hints read this one table.
  When `chat.New` fails because a provider is not registered
  (`gochat.ErrProviderNotRegistered`), the adapter adds a hint naming the
  import to add.
- **Linked providers are a feature-set question.** `chat.LinkedProviders(set)`
  is how `init ai` and `doctor` ask what this binary ships. A generated tool
  declares its manifest's providers as link-kind features
  (`props.DeclareLinks(props.ChatLinkPrefix, ...)` in its `chat.go`), so the
  answer is the author's choice rather than every provider a linked module
  registers; a declared provider the registry lacks is reported as unlinked.
  A tool declaring none is read from `gochat.RegisteredProviders` alone, the
  same way the forge side falls back to `forge.Registered`
  ([#81](https://gitlab.com/phpboyscout/go-tool-base/-/issues/81)).

## Related how-to guides

- [Add AI to your tool](../../../how-to/ai-integration.md) · [AI tool calling](../../../how-to/ai-tool-calling.md) · [Structured AI responses](../../../how-to/structured-ai-responses.md) · [Persist conversations](../../../how-to/persist-chat-conversations.md)
