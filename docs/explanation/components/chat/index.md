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
module** (plus per-provider modules for Anthropic, OpenAI, and Gemini). Its full
documentation — the `ChatClient` API, the ReAct tool-calling loop, streaming,
cross-provider fallback, conversation persistence, multimodal input, token-usage
accounting, and the provider capability matrix — now lives at:

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
  | Gemini | `gemini.api.key` | `gemini.api.env` | `gemini.api.keychain` | `GEMINI_API_KEY` |

  The provider is chosen by `ai.provider` (or `AI_PROVIDER`); fallback is
  configured under `ai.fallback.*`. Resolution precedence: direct token → env-var
  reference → OS keychain → literal → ecosystem env var. The recommended path
  (env-var reference) keeps the literal secret out of the config file.
- **Hardened HTTP + keychain seams.** The adapter injects `pkg/http`'s hardened
  transport and wires `pkg/credentials.Retrieve` as the keychain lookup, so GTB
  tools get the framework's security posture; the module core carries neither.
- **Every provider registered.** The adapter blank-imports all three provider
  modules, so GTB tools can use any provider with no import changes. A regulated
  downstream that wants a lighter binary can build its own adapter importing only
  the providers it needs.

## Related how-to guides

- [Add AI to your tool](../../../how-to/ai-integration.md) · [AI tool calling](../../../how-to/ai-tool-calling.md) · [Structured AI responses](../../../how-to/structured-ai-responses.md) · [Persist conversations](../../../how-to/persist-chat-conversations.md)
