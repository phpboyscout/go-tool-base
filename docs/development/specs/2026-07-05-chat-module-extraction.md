---
title: "Extract pkg/chat into a standalone module with per-provider SDK modules"
description: "Extract the multi-provider AI chat client out of go-tool-base into its own standalone module gitlab.com/phpboyscout/go/chat, dependency-inverted onto narrow Logger/Config/HTTP/credential seams. Split each vendor-SDK provider (Anthropic, OpenAI, Gemini) into its own per-provider module registering via init() blank-import, so consumers link only the SDK(s) they opt into. go-tool-base consumes the module(s) through a thin Props-mapping adapter. Mirrors the signing / signing-aws-kms extraction (work item #1)."
date: 2026-07-05
status: APPROVED
tags:
  - specification
  - chat
  - ai
  - module-extraction
  - dependency-inversion
  - provider-registry
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/chat` into a standalone module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   5 July 2026

Status
:   APPROVED

Builds on
:   [`2026-07-12-go-module-extraction-playbook.md`](2026-07-12-go-module-extraction-playbook.md) (naming, repo framework, migration procedure), and the `signing` / `signing-aws-kms` extraction (work item #1, relocated to `gitlab.com/phpboyscout/go/*` v0.2.0)

Feasibility
:   `chat-extraction-feasibility.md` (companion analysis)

---

## 1. Context & motivation

`pkg/chat` is a 4,362-line multi-provider AI client (Anthropic Claude, Claude-local CLI,
OpenAI, OpenAI-compatible, Google Gemini) with a ReAct tool-calling loop, streaming,
fallback policy, usage accounting, and encrypted persistence. Recent work made it more
multimodal. It is a genuinely reusable asset that non-GTB projects would want.

Two forces make extraction worthwhile — the same two that drove the signing extraction:

1. **Reuse.** A consumer should be able to `import gitlab.com/phpboyscout/go/chat` without
   dragging in go-tool-base (viper, otel, controls, vcs, version, …).
2. **Selective SDK weight.** Importing `pkg/chat` today unconditionally links **all three**
   vendor SDKs — `anthropics/anthropic-sdk-go`, `openai/openai-go/v3` (+ `tiktoken-go`),
   `google.golang.org/genai` — because all four providers live in one package. A tool that
   only talks to Claude still compiles the OpenAI and Gemini SDKs.

The perceived blocker — deep coupling to GTB — does not hold up. Non-test chat source uses
only a **narrow** slice of each GTB package (see §2). The provider registry needed for
selective inclusion **already exists** (`RegisterProvider`, `ProviderFactory`,
`providerRegistry` in `client.go`); it just doesn't pay off yet because there is no package
boundary per provider.

## 2. The actual coupling surface

Chat imports five GTB packages. What it *uses* is far narrower than what those packages
*weigh*:

| Import | Used surface | Transitive weight | Inversion |
|---|---|---|---|
| `pkg/props` | `p.Logger`, `p.Config`, `p.GetConfig` only | heavy (`vcs/release`, `version`, `errorhandling`) | narrow `Deps` interface |
| `pkg/logger` | logging interface (`Debugf`…), 12 sites | light | local `Logger` interface |
| `pkg/config` | `Containable.GetString` only | heavy (viper, afero, fsnotify) | local `ConfigLookup` interface |
| `pkg/http` | `NewTransport`/`NewClient`/`WithTransport`/`WithTimeout` | **very heavy** (otel SDK, controls, authn, tls, redact) | injected `*http.Client` |
| `pkg/credentials` | `Retrieve(ctx, service, account)` | light; keychain already inverted | `CredentialResolver` seam |

`chat.New(ctx, *props.Props, cfg)` is called 87× and `ProviderFactory` is typed on
`*props.Props`, which is what makes chat *look* welded to GTB. Underneath, every provider
body only reads `p.Logger` and `p.Config`. Replacing that one parameter with a two-method
interface removes the welding.

## 3. Goals / non-goals

### Goals

- A standalone `gitlab.com/phpboyscout/go/chat` core module with **zero** go-tool-base and **zero**
  vendor-SDK dependencies beyond the SDK-free `claude-local` provider.
- Per-provider modules — `chat-anthropic`, `chat-openai`, `chat-gemini` — each adding exactly
  one SDK and self-registering via `init()` (blank-import to activate).
- go-tool-base consumes the module(s) via a thin adapter; **no behaviour change** for existing
  GTB consumers, all 14 of them.
- Coverage parity (chat is currently well above the 90% `pkg/` bar; the module must keep it).

### Non-goals

- No change to provider behaviour, the ReAct loop, streaming semantics, or the wire protocol.
- No new provider in this spec.
- The GTB config-key schema (`claude.api.env`, …) and the 5-step credential cascade stay on
  the **GTB side** — the module is config-system-agnostic (see §4.4).
- Not touching `pkg/chat`'s consumers' *logic*, only the entry-point they call.

## 4. Design

### 4.1 The seams (what the module defines)

```go
// Logger — the minimal logging surface chat needs.
type Logger interface {
    Debugf(format string, args ...any)
    // …exact set derived from current call sites
}

// ConfigLookup — read-only key lookup; GTB's Containable satisfies it.
type ConfigLookup interface {
    GetString(key string) string
}

// CredentialResolver — resolves a provider's API key however the consumer wishes.
// GTB supplies the 5-step cascade; a bare consumer can return a constant.
type CredentialResolver interface {
    Resolve(ctx context.Context, provider Provider) (string, error)
}

// Deps — everything a factory needs, replacing *props.Props.
type Deps struct {
    Logger      Logger
    Config      ConfigLookup
    HTTPClient  *http.Client        // injected; module default is a plain bounded client
    Credentials CredentialResolver  // optional; nil ⇒ direct-token / env-fallback only
    FS          afero.Fs            // optional; nil ⇒ afero.NewOsFs() for filestore/persistence
}

type ProviderFactory func(ctx context.Context, d Deps, cfg Config) (ChatClient, error)
```

### 4.2 Module layout

**Core — `gitlab.com/phpboyscout/go/chat`:** `client.go` (registry, `New`, `Config`,
`ChatClient`), `schema`, `streaming`, `usage`, `fallback`, `fallback_policy`, `persistence`,
`filestore`, `baseurl`, `tools`, `constants`, the seams above, and `claude_local.go`
(SDK-free default provider).

**Per-provider modules**, each `depends on chat` + one SDK, `init()`-registers:

- `gitlab.com/phpboyscout/go/chat-anthropic` → `anthropics/anthropic-sdk-go`
- `gitlab.com/phpboyscout/go/chat-openai` → `openai/openai-go/v3` + `tiktoken-go/tokenizer`
- `gitlab.com/phpboyscout/go/chat-gemini` → `google.golang.org/genai`

Activation is a blank import, e.g. `import _ "gitlab.com/phpboyscout/go/chat-anthropic"`.

### 4.3 HTTP injection

The module default is a plain `*http.Client` with the existing `DefaultChatRequestTimeout`
(5m) and a response-header timeout — no otel, no circuit breaker. GTB's adapter injects
`pkg/http`'s hardened transport instead, preserving today's behaviour for GTB tools.

### 4.4 Credential / config-key division of labour

The module owns provider *mechanics*. The **GTB adapter** owns:

- the `ConfigKey*` / `Env*` constant naming (GTB's config schema),
- the 5-step resolution cascade (`credentials.go`, `resolveAPIKey`), wired as a
  `CredentialResolver`,
- hardened-HTTP injection.

This keeps the module free of GTB config conventions — a non-GTB consumer supplies a key via
`Config.Token`, an env fallback, or its own `CredentialResolver`.

### 4.5 go-tool-base cut-over

`pkg/chat` becomes a thin adapter (or is removed and callers repoint) that:

1. maps `*props.Props → Deps` (Logger, Config, hardened HTTP, 5-step resolver),
2. re-exports the config-key constants and `Provider`/`Config`/`Tool`/schema/streaming types
   the 14 consumers use, so their call sites change minimally,
3. blank-imports all three provider modules (GTB wants every provider).

Mirrors `refactor(signing): consume external signing + signing-aws-kms modules`.

### 4.6 Footprint

A Claude-only consumer links `chat` + `chat-anthropic` + `anthropic-sdk-go` — **not**
openai-go, tiktoken, or genai. GTB links all three (no change from today). The SDK-free
`claude-local` path links no vendor SDK at all.

## 5. Sequencing — invert in place, then move

To keep the extraction low-risk, split into two stages:

1. **In-place inversion (a `refactor` inside GTB, no new deps, identical behaviour):**
   introduce `Deps`/`Logger`/`ConfigLookup`/`CredentialResolver`, retype `ProviderFactory`
   and `New`, inject the HTTP client and resolver. Ships as one reviewable, fully-covered
   refactor with no functional change. Land on `main`.
2. **Extraction (mechanical):** move files to the new module(s), split `go.mod`, wire the GTB
   adapter, blank-import providers. Because stage 1 already removed the coupling, this is a
   file move + module plumbing, not a rewrite. Branch off stage 1 once it lands (stacked-MR
   ordering: dependency in `main` first).

## 6. Development model (TDD / BDD)

- The module carries its own unit tests (the existing `pkg/chat/*_test.go`, de-GTB'd) and must
  hold coverage ≥ the current level.
- GTB's existing chat E2E/integration scenarios (`INT_TEST_E2E_*`, fallback/parallel
  integration tests) run against the adapter unchanged — they are the cut-over's safety net.
- New seams get table-driven tests (nil-resolver fall-through, injected-client honoured,
  registry blank-import activation).

## 7. Documentation (Diátaxis)

- Module `README` + `doc.go` (reference) in each new repo.
- GTB `docs/components/chat.md` repointed to the module, retaining the GTB-side security notes
  (endpoint validation, redaction, credential storage) as adapter concerns.
- A `docs/migration/` note for the GTB version that cuts over (analogous to the
  v0.27→v0.28 signing-modules-extracted guide).

## 8. Repository & project framework

Each new module is its own GitLab repo in the **`phpboyscout/go` subgroup**
(`gitlab.com/phpboyscout/go/chat`, `…/go/chat-anthropic`, etc.), bootstrapped to
the standard framework defined in the
[extraction playbook](2026-07-12-go-module-extraction-playbook.md) §3: cicd
**v0.21.0** components (go-lint / go-test+Godog / go-security / releaser-pleaser /
zensical-pages / renovate-self), `.golangci.yaml` v2 with the module's local
prefix, `.mockery.yml`, `go 1.26.5`, `cockroachdb/errors`, `*slog.Logger` seams,
and a **`depfootprint_test.go`** guard. The core module's guard forbids
`go-tool-base`, viper, pflag, charmbracelet, **and every vendor AI SDK**
(`anthropic-sdk-go`, `openai-go`, `genai`, `tiktoken`) — proving the SDK-free
core. Each provider module's guard forbids `go-tool-base` and the *other*
providers' SDKs. Docs publish to `chat.go.phpboyscout.uk`; GTB's component page
becomes a stub linking there (playbook §4.1). Only the core carries a full docs
site; the provider modules ship README + `Example` + a page on the core site.

## 9. Resolved decisions

_Playbook-resolved and 2026-07-12 review._

1. **Core default provider — include `claude-local` in the core.** It is SDK-free,
   so it adds no vendor weight and gives a working default with zero provider
   modules imported.
2. **Tiktoken lives in `chat-openai`.** Token counting is OpenAI-specific; it must
   not sit in the core (the dep guard enforces this).
3. **Config-key constants — re-export from the GTB adapter.** The module stays
   config-schema-agnostic; GTB owns the `claude.api.env`-style keys and the 5-step
   cascade. A shared `chat-gtbschema` helper is deferred until a second
   GTB-convention consumer actually needs it (YAGNI).
4. **Module naming — `gitlab.com/phpboyscout/go/chat`** (playbook convention). The
   `go` subgroup disambiguates the generic `chat` token, so no `aichat`/`llmchat`
   rename is needed; per-provider backends follow `go/chat-<provider>`.
5. **N repos, not a monorepo** — matches the signing precedent and the playbook;
   keeps each provider's SDK weight quarantined by its own `go.mod`.
6. **Persistence/encryption seam — inject `afero.Fs`.** `filestore`/`persistence`
   take an injected `afero.Fs` (GTB passes its own; a bare consumer passes
   `afero.NewOsFs()`), so the module never assumes GTB's filesystem wiring. This
   is part of the Stage-1 `Deps` inversion.

## 10. Work items (once approved)

1. Stage-1 in-place inversion (`Deps` + seams + HTTP/credential injection) — GTB refactor.
2. Create `gitlab.com/phpboyscout/go/chat` core module; move de-GTB'd core + `claude-local`.
3. Create `chat-anthropic`, `chat-openai`, `chat-gemini` provider modules.
4. GTB adapter + blank-imports; repoint the 14 consumers.
5. Docs (module READMEs, `chat.md` repoint, migration note) + Renovate/CI wiring.
