---
title: The Go Toolkit
description: What the phpboyscout Go toolkit is, its naming convention, and the modules GTB is assembled from.
date: 2026-09-18
tags: [concepts, toolkit, modules, extraction]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# The Go Toolkit

Between 2026-07 and 2026-09 most of what used to be GTB's own `pkg/` packages
moved out into standalone modules under `gitlab.com/phpboyscout/go/`. Each one
is small, framework-free and independently versioned: a project can take one
of them on its own, without taking any of GTB. Collectively they are the
**phpboyscout Go toolkit** ([go.phpboyscout.uk](https://go.phpboyscout.uk)).

GTB is one consumer of the toolkit, an opinionated assembly of it: the Props
container, the command tree, the config-key schemas, the doctor checks, and
the generator that scaffolds a new tool from it. The toolkit modules
themselves carry no dependency on GTB, and none of GTB's `pkg/` packages that
still exist assumes a reader has read this page first, this is the
orientation for someone new to the estate, not a prerequisite for the
component pages under [Components](../components/index.md).

## Naming convention

Every toolkit module follows the same three-part shape:

- **Module path**: `gitlab.com/phpboyscout/go/<name>`, a bare, lower-case
  name with no `pkg` or `lib` prefix.
- **Documentation microsite**: `https://<name>.go.phpboyscout.uk`, built with
  the same [zensical](https://gitlab.com/phpboyscout/cicd) tooling as this
  site and deployed via GitLab Pages. Not every module has one: a
  provider submodule (a forge, a chat backend, a signing backend) is
  documented on its **parent** module's site instead, under a providers or
  backends reference page, so a reader is not sent hunting across a dozen
  near-identical microsites for one paragraph.
- **Mocks**: a published `mocks` subpackage (`gitlab.com/phpboyscout/go/<name>/mocks`),
  not a `<name>mock` package alongside the code. A consumer imports it under
  an alias (`configmocks "gitlab.com/phpboyscout/go/config/mocks"` is the
  pattern GTB's own tests use) rather than generating its own.

## Relationship to GTB

The direction of dependency is one-way. A toolkit module never imports
`gitlab.com/phpboyscout/go-tool-base`, so none of them carry the framework,
Cobra, or any GTB-specific type into a project that uses them standalone.
Where GTB still has a `pkg/` package for something a module now owns, that
package is a **thin adapter**: it maps GTB's config store, `Props` and
logger onto the module's own constructors, and owns only what is GTB's to
own, a config-key schema, a doctor check, a link-kind blank import, a
generator template. The module is never the authority on its own API inside
a GTB doc page; every [component page](../components/index.md) that wires
one links out to the module's own docs for the API itself. See the
[migration guides](../../reference/migration/index.md) for the individual
cut-overs, each one a clean repoint with no compatibility shim.

## Modules GTB depends on

The tables below list every `gitlab.com/phpboyscout/go/*` module `go.mod` and
`cli/go.mod` require directly, at HEAD (colophon versions, not pinned to a
release date). A provider submodule (forge, chat, signing backend) is
documented on its parent's microsite rather than its own; where a module has
no published microsite yet, the link goes to its `pkg.go.dev` reference
instead and that gap is called out.

### Configuration and errors

| Module | What it does | What GTB uses it for | Docs |
|---|---|---|---|
| `config` | Layered configuration that can report where a value came from, and write one back without disturbing the rest of the file | The `props.Config` store: embedded defaults, project-local layer, env-prefix, flag binding, hot reload | [config.go.phpboyscout.uk](https://config.go.phpboyscout.uk) |
| `config-afero` | Adapts an `afero.Fs` to the module's `config.FS` seam | Lets the config store read/write through GTB's existing `afero.Fs` rather than the OS filesystem directly | [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/config-afero) (no microsite published yet) |
| `errors` | Stack traces, user-facing hints, structured attributes and a well-behaved aggregate, importing nothing outside the standard library | GTB's own error creation and wrapping estate-wide (see `AGENTS.md`); every symbol previously used from `cockroachdb/errors` has a same-named equivalent | [errors.go.phpboyscout.uk](https://errors.go.phpboyscout.uk) |
| `errorhandling` | Structured, user-facing error reporting: hints, exit codes carried on the error, debug-gated stack traces, a pluggable support-channel message | `pkg/cmd/root`'s `Execute` funnel: the one place every command's error is reported and the process exits | [errorhandling.go.phpboyscout.uk](https://errorhandling.go.phpboyscout.uk) |
| `features` | Feature gating as values rather than process state: a `Registry` declared at init, an immutable `Snapshot`, a resolved `Set`, and an `Evaluator` seam for dynamic flags | The core `props`/`setup` build on: which built-in commands, forges and link-kind features a binary carries | [features.go.phpboyscout.uk](https://features.go.phpboyscout.uk) DNS resolves but the TLS certificate does not yet cover this hostname; use [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/features) meanwhile |

### Credentials and signing

| Module | What it does | What GTB uses it for | Docs |
|---|---|---|---|
| `credentials` | Storage-mode abstraction for user-supplied secrets: env-var reference, OS keychain, or literal, with a pluggable backend and an auditable keychain opt-out | The setup wizard's storage-mode selector, `pkg/vcs`'s and `pkg/chat`'s credential resolution, and the `credentials.no-literal` doctor check | [credentials.go.phpboyscout.uk](https://credentials.go.phpboyscout.uk) |
| `signing` (+ `signing/openpgpkey`, `signing/verify`, `signing/local` subpackages) | OpenPGP/WKD release signing and verification: a backend registry, trust-set and key-resolver primitives, and OpenPGP packet assembly from any `crypto.Signer` | `gtb sign` / `gtb keys`, and Phase 2 self-update signature verification in `pkg/setup` | [signing.go.phpboyscout.uk](https://signing.go.phpboyscout.uk) |
| `signing-aws-kms` | The AWS KMS signing backend for `signing`, wrapping an asymmetric RSA-4096 `SIGN_VERIFY` key | One of the two backends the standard `gtb` binary blank-imports; kept in its own module so a build that omits the import drops the AWS SDK | no microsite published; see [signing.go.phpboyscout.uk's AWS KMS how-to](https://signing.go.phpboyscout.uk/how-to/sign-with-aws-kms/) |
| `signing-cli` | The shareable `sign`/`keys` Cobra command builders over `signing`, decoupled from any specific CLI framework behind a narrow `Logger` seam | The `gtb sign`/`gtb keys` command surface, re-attached unchanged; also used standalone by the `sigillum` CLI | [signing-cli.go.phpboyscout.uk](https://signing-cli.go.phpboyscout.uk) |

### Forge and repo

| Module | What it does | What GTB uses it for | Docs |
|---|---|---|---|
| `forge` | One Go interface over GitHub, GitLab, Gitea, Codeberg, Bitbucket and a plain-HTTP download source, each provider's SDK kept behind its own module boundary | Release discovery and the credential chain behind self-update and `pkg/setup`'s auth flows; `pkg/vcs` is the config-key adapter over it | [forge.go.phpboyscout.uk](https://forge.go.phpboyscout.uk) |
| `forge-github`, `forge-gitlab`, `forge-gitea`, `forge-bitbucket` | The per-provider release and auth clients (GitHub also does device-flow login and SSH-key upload) | Blank-imported by the binary that ships that forge; `gtb`'s own `main` links all four | documented on [forge.go.phpboyscout.uk's providers reference](https://forge.go.phpboyscout.uk/reference/providers/); no individual microsites |
| `repo` | Git repository operations over go-git: clone, commit, worktrees, tree inspection, behind role interfaces with in-memory and filesystem backends | `pkg/vcs/repo`'s adapters (`SettingsFromProps`, `NewRepoFromProps`) and the generator's template-source clones | [repo.go.phpboyscout.uk](https://repo.go.phpboyscout.uk) |

### Chat

| Module | What it does | What GTB uses it for | Docs |
|---|---|---|---|
| `chat` | Multi-provider AI chat client: the `ChatClient` interface, the ReAct tool-calling loop, streaming, cross-provider fallback, conversation persistence | `pkg/chat`'s config-key schema and `Props` adapters; no provider is registered by the core module itself | [chat.go.phpboyscout.uk](https://chat.go.phpboyscout.uk) |
| `chat-anthropic`, `chat-openai`, `chat-gemini`, `chat-bedrock`, `chat-openai-azure` | The per-provider SDKs (Claude; OpenAI and OpenAI-compatible; Gemini and Vertex; AWS Bedrock's Converse API; Azure OpenAI) | Blank-imported by the binary that ships each provider (`cli/cmd/gtb/providers.go` links every one); a generated tool links only what its manifest selects | documented on [chat.go.phpboyscout.uk's provider reference](https://chat.go.phpboyscout.uk/explanation/providers/); no individual microsites |

### Transport, controls and observability

| Module | What it does | What GTB uses it for | Docs |
|---|---|---|---|
| `controls` | Service-lifecycle supervisor: concurrent startup, health probes, reverse-registration shutdown, self-healing restarts | The `controls.Controller` GTB's HTTP/gRPC/gateway servers register against; consumed directly, no GTB adapter | [controls.go.phpboyscout.uk](https://controls.go.phpboyscout.uk) |
| `transport` (+ `transport/http`, `transport/grpc`, `transport/gateway` subpackages) | A framework-free HTTP + gRPC + gateway server stack: hardened constructors, health endpoints, `AuthMiddleware`/`AuthInterceptor`, security headers | The construction, start and stop that `pkg/http`, `pkg/grpc` and `pkg/gateway` no longer do themselves; those packages keep only the config-key adapters | [transport.go.phpboyscout.uk](https://transport.go.phpboyscout.uk) |
| `transit` (+ `transit/http`, `transit/grpc` subpackages) | Shared transport middleware and resilience: logging, OpenTelemetry, circuit breaking, rate limiting, retry, for both HTTP and gRPC, server and client | The middleware chain GTB's servers and clients compose from; `RateLimitConfigFromConfig`/`CircuitBreakerConfigFromConfig` are the two GTB config adapters left over it | [transit.go.phpboyscout.uk](https://transit.go.phpboyscout.uk) |
| `httpclient` | A hardened, framework-free `*http.Client` factory: secure TLS defaults, a downgrade-proof redirect policy, the `transit` retry/circuit-breaker/auth middleware | GTB's own outbound HTTP calls and any tool built on it; consumed directly, no GTB adapter | [httpclient.go.phpboyscout.uk](https://httpclient.go.phpboyscout.uk) |
| `observability` (+ `otelcore`, `tracing`, `metrics`, `logs` subpackages) | Hardened OpenTelemetry setup: OTLP logs, metrics and traces from one typed config, with graceful `OTEL_*` environment fallback | `pkg/telemetry`'s `Setup`/`SetupFromProps`: the config-key adapter that resolves `telemetry.*` into the module's typed settings and installs the OTel globals | [observability.go.phpboyscout.uk](https://observability.go.phpboyscout.uk) |
| `tls` | Hardened, opinionated TLS plumbing: a curated default config, typed cert pairs, server/client builders | `pkg/tls`'s `Resolve`, the one piece that stays in GTB: mapping the `server.tls` config cascade onto the module's typed `Pair` | [tls.go.phpboyscout.uk](https://tls.go.phpboyscout.uk) |

### MCP

| Module | What it does | What GTB uses it for | Docs |
|---|---|---|---|
| `mcp` | MCP (Model Context Protocol) discovery and execution for Go applications, with Cobra and service integrations | `pkg/mcp`'s `NewCmdMCP`: the `gtb mcp` command, exposure rules, and the publication mode read from a tool's manifest | [mcp.go.phpboyscout.uk](https://mcp.go.phpboyscout.uk) |

### Misc utilities

| Module | What it does | What GTB uses it for | Docs |
|---|---|---|---|
| `browser` | A safe entry point for opening URLs: scheme allowlist, length bound, control-character rejection before handing a URL to the OS | Every URL-opening call site in GTB (telemetry deletion mailto flow, device-login hand-off, docs-server launch); consumed directly | [browser.go.phpboyscout.uk](https://browser.go.phpboyscout.uk) |
| `changelog` | Turns a repository's Conventional-Commits history, or a release-notes archive, into a structured, categorised changelog | The `changelog` command, the `cmd/changelog` generator tool directive, and self-update's structured release notes | [changelog.go.phpboyscout.uk](https://changelog.go.phpboyscout.uk) |
| `output` | Structured, themeable CLI output: one `Renderer` façade for text/JSON/YAML/CSV/TSV/Markdown, tables, spinners, progress bars | GTB's own built-in commands (`version`, `doctor`, `config`, `changelog`, `update`, `init`, `docs`) via the opt-in `output/cobra` subpackage; consumed directly | [output.go.phpboyscout.uk](https://output.go.phpboyscout.uk) |
| `redact` | Strips credential-like content from free-form strings before they reach logs, telemetry, or any third-party surface | Telemetry's `TrackCommandExtended`, HTTP middleware's header-redaction catalogue, the `doctor report` support bundle | [redact.go.phpboyscout.uk](https://redact.go.phpboyscout.uk) |
| `regexutil` | Bounded, DoS-safe `regexp.Compile` for patterns from untrusted sources: a length cap and a compile timeout | Every `regexp.Compile` call whose pattern originates outside the binary (config, CLI flag, TUI input) | [regexutil.go.phpboyscout.uk](https://regexutil.go.phpboyscout.uk) |
| `workspace` | Finds a project's root by walking up from a starting directory to a marker file, over an injected `afero.Fs` | The generator commands (`regenerate`, `generate`, `remove`) resolving the project root when run from a subdirectory | [workspace.go.phpboyscout.uk](https://workspace.go.phpboyscout.uk) |

## The rest of the toolkit

Several toolkit modules exist that GTB does not currently depend on (a
YubiKey signing backend, a GCP KMS backend, standalone TUI components, and
others). For the full, current list of published modules, see
[phpboyscout.uk/projects](https://phpboyscout.uk/projects) rather than this
page: a project list maintained in one place is less likely to drift than a
second copy of it here.
