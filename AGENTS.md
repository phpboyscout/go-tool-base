# AGENTS.md

This file provides guidance to AI coding agents (Claude Code, agy, codex, etc.) when working with code in this repository.

## Workflows

The skills and commands below ship from the [phpboyscout marketplace](https://gitlab.com/phpboyscout/claude-code-plugins) — the `phpboyscout-gtb-dev` segment and its siblings — installed as plugins rather than committed here. Always prefer them over ad-hoc steps.

| Task | Workflow / Skill |
|------|-----------------|
| Any development or maintenance work | Read the `gtb-dev` skill first |
| Drafting a new feature specification | `/gtb-spec` |
| Adding or modifying a reusable library component in `pkg/` | `/gtb-library-contribution` |
| Defining or generating a new CLI command | `/gtb-command-generation` |
| Verifying correctness before committing or raising a PR | `/gtb-verify` |
| Resolving golangci-lint issues | `/gtb-lint` |
| Updating documentation without touching code | `/gtb-docs` |
| Preparing or validating a release | `/gtb-release` |

## Development Lifecycle

### Step 0: Spec Check (Before Any Implementation)

**Do not write implementation code until this is complete.**

1. Check the [spec register](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/home) for an existing spec matching the feature.
2. Only proceed if the spec status is `APPROVED` or `IN PROGRESS`.
3. **Review open questions**: Before writing any code, review the spec for open questions, unresolved design decisions, gaps, or ambiguities. Present them to the user for resolution. Do not begin implementation until each open question is answered or explicitly deferred.
4. For **non-trivial features** (new packages, public API changes, generator modifications, architectural changes) with no existing spec: run `/gtb-spec` to draft one, claim the next number from the [register](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/home) and publish it to the wiki as `specs/NNNN-<feature-name>` with status `DRAFT`.
5. For **quick fixes and minor changes** (bug fixes, small refactors that don't alter the public API): proceed directly.
6. Update spec status to `IN PROGRESS` when starting, `IMPLEMENTED` when done.

### Implementation (TDD)

- Write failing tests first, derived from the spec's public API, error cases, and edge cases.
- For features with **CLI commands, multi-step user workflows, or service lifecycle coordination**, also write Gherkin feature files in `features/` as E2E BDD scenarios. These are not optional for user-facing behaviour — they complement unit tests by expressing workflows in Given/When/Then format.
- Implement the minimum code to pass. Refactor. Re-run tests.
- Use `gitlab.com/phpboyscout/go/errors` for all error creation and wrapping. It is the estate's own package — stdlib-only, so it adds nothing to the dependency graph — and every symbol GTB used from `cockroachdb/errors` has a same-named equivalent. Package-level sentinels use `NewSentinel(kind, msg)` rather than `New`: at package scope `New` captures its stack at initialisation, which points at `runtime.doInit` instead of anywhere the error was returned. Kinds are namespaced `gtb.<package>.<name>`.
- New `pkg/` features must have **≥90% test coverage**.
- Address the root cause first; do not silence a linter to avoid fixing real issues. When a suppression is **genuinely unavoidable** (e.g. gosec G204 for an intentional dev-tooling subprocess, G304 for an operator-named file path, gochecknoglobals for an ldflags injection target), use a **narrowly-scoped inline `//nolint:<linter> // <justification>`** on the exact line — never a file-scoped `.golangci.yaml` exclusion, which would also mask new, unintended violations of that linter in the same file.

### Library-First

New features must be implemented in `pkg/` as a reusable component before being exposed via the CLI. When modifying library APIs that affect scaffolded output, also update templates in `internal/generator/`.

### After Implementation

1. Run `/gtb-verify` (tests, race detector, lint, mocks).
2. If generator output was affected: `just build && go run ./cmd/gtb generate <command> -p tmp`, verify `tmp/`, delete it.
3. Update `docs/explanation/components/` and `docs/explanation/concepts/` — any functional change **must** include a doc update, cross-referenced with the code for accuracy.
4. Run `/simplify` on changed files before raising a PR.

## Commands

This project uses `just` as the task runner:

```bash
just              # Default: tidy, generate, build binary to bin/gtb
just test         # Unit tests with coverage
just test-race    # Race condition detection
just test-integration  # Integration tests (requires INT_TEST=1)
just test-e2e     # E2E BDD tests via Godog (requires INT_TEST_E2E=1)
just test-e2e-smoke   # E2E smoke tests only (fast, no external deps)
just lint         # Run golangci-lint
just lint-fix     # Auto-fix linting issues
just mocks        # Regenerate mocks via mockery
just ci           # Full local CI: tidy, generate, test, test-race, lint
just coverage     # HTML coverage report
just generate     # go generate ./...
just bench        # Run benchmarks with memory stats
just check        # Run pre-commit hooks on all files
just vuln         # govulncheck for dependency vulnerabilities
just deadcode     # Find unreachable exported symbols
just fix          # Apply go fix for deprecated API usage
just install      # Install gtb binary to $GOPATH/bin
just snapshot     # Local goreleaser snapshot build (output to dist/)
just docs-serve   # Serve documentation locally via mkdocs
just cleanup      # Remove build artifacts
```

Run a single test:
```bash
go test ./pkg/props/... -run TestSpecificName -v
```

## Commit Conventions

All commits must follow [Conventional Commits](https://www.conventionalcommits.org/). releaser-pleaser uses these to compute the next version and the changelog on the Release MR.

**Do not commit without explicit user approval.** Present a summary of changes and a proposed message, then wait for confirmation.

**Do not add AI attribution** — no `Co-Authored-By:` trailers naming an AI, no references to AI assistance in **commit messages, MR/PR descriptions, or MR/PR comments**. Responsibility for code lies with the human entity approving the commit or MR; the approving developer owns the change entirely.

**Never `@`-mention anyone in content pushed or created on GitLab (or any other forge)** — MR/PR descriptions, MR/PR comments, commit messages, issue text. Raw tokens like `@cli`, `@smoke`, `@release` resolve to real usernames and ping those people on every reference. When you need to name a tag/scope/handle literally (e.g. a BDD tag), write it without the `@` (`cli`, `smoke`), wrap it in backticks (`` `@cli` ``), or otherwise neutralise it so the forge does not turn it into a notification.

| Type | Release |
|------|---------|
| `feat(scope):` | Minor |
| `fix(scope):` | Patch |
| `BREAKING CHANGE:` footer / `feat!:` | Major |
| `perf:` / `refactor:` / `ci:` / `chore:` / `style:` / `docs:` / `test:` | None |

Only `feat`, `fix`, and breaking changes cut a release. A batch containing only non-releasing types will not produce a Release MR; force a release with an `rp-next-version::*` label on the Release MR if needed.

Always include a scope identifying the functional area (package name, subsystem, feature). Each commit represents one coherent change.

## Architecture

**go-tool-base (GTB)** is a framework for building Go CLI tools and services. It provides a reusable, opinionated base with AI integration, self-updating, service lifecycle management, and interactive TUI components.

### Dependency Injection: Props Container

The central pattern is the `Props` struct in `pkg/props/`. Every command receives a `Props` instance containing:
- `Logger` — logging backend
- `Config` — Viper-based configuration
- `Assets` — embedded assets (default configs, templates)
- `FS` — `afero.Fs` for testable filesystem access
- `ErrorHandler` — structured user-facing error reporting
- `Tool` — tool metadata (name, release source for updates)
- `Version` — runtime/ldflags version info

Narrow provider interfaces (`LoggerProvider`, `ConfigProvider`, etc.) allow packages to declare only the dependencies they need.

### Command Architecture (Cobra)

Commands are built on Cobra. The root command in `pkg/cmd/root/` wires Props, loads config, and registers global `PersistentPreRunE` middleware for: config loading, log level setup, feature flag resolution, and update checks.

**Feature flags** control which built-in commands are active:
```go
props.SetFeatures(
    props.Disable(props.InitCmd),
    props.Enable(props.AiCmd),
)
```
Default-enabled: `UpdateCmd`, `InitCmd`, `McpCmd`, `DocsCmd`, `DoctorCmd`, `ChangelogCmd`.

### API Stability (pre-1.0)

GTB is currently **pre-1.0** (`v0.x`). During this phase the public `pkg/` API is **not** frozen: breaking changes are permitted and ship as a **minor** bump (the project has made several deliberate ones). The stability tiers and "no breaking changes without a major bump" guarantees described in `docs/reference/api-stability.md` are **aspirational and take effect from v1.0** — do not treat them as binding while the module is `v0.x`.

What this means in practice today:

- A breaking change to a `pkg/` type, interface, signature, or exported constant is allowed; it ships as a `feat`/`fix` minor/patch (no `BREAKING CHANGE:` footer is used on the v0 line — a major bump is not desired yet).
- Still prefer backward-compatible changes where the cost is low, and reach for a clean break over a long-lived shim when a break is warranted.
- When you do break a public API, note it in the commit body and add a migration note in `docs/reference/migration/` so the v1.0 migration guide stays accurate.
- Deprecations should still be annotated with `// Deprecated:` so downstreams get a transition window.
- `internal/` packages are unstable and never subject to this policy.

For **visibility** (not enforcement) of API changes pre-1.0, run `just apidiff`, which compares the working tree against the latest release tag (`apidiff -m gitlab.com/phpboyscout/go-tool-base <latest-tag> .`). The CI `apidiff` job runs this on MRs as an **advisory, non-blocking** check (`allow_failure: true`) so reviewers can see and confirm an API change is intentional. **From v1.0 this gate becomes blocking** and the full stability policy in `docs/reference/api-stability.md` applies.

The binary entry point is `cmd/gtb/main.go`. The `internal/cmd/` packages add GTB-specific commands (`generate`, `regenerate`, `remove`) for scaffolding new CLI tools based on this framework.

### Configuration

Configuration is the extracted `gitlab.com/phpboyscout/go/config` Store: layered precedence (changed CLI flags > env vars > project-local `.<tool>.yaml` > config files > tool `ConfigPaths` assets > merged `assets/config.yaml` embedded defaults), snapshot-pinned reads via `store.View()`, transactional in-place writes via `Apply`, and explicit hot-reload via `Store.Watch` (wired by the root pre-run). Embedded defaults always apply and are segregated per owning package/feature bundle — see [`0138-segregated-default-config`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0138-segregated-default-config).

### AI Chat Client

The multi-provider chat client was **extracted** to the standalone module
`gitlab.com/phpboyscout/go/chat` (+ per-provider modules `chat-anthropic`,
`chat-openai`, `chat-gemini`). `pkg/chat/` is now a **thin adapter** that
re-exports the module's API (so GTB call sites are unchanged), owns the GTB
config-key schema + `SettingsFromProps`/`NewFromProps` adapters, and
blank-imports the three provider modules so every provider is registered.
- Providers: Anthropic Claude, Claude Local (CLI binary, ships in the core module), OpenAI, OpenAI-compatible, Google Gemini
- Core interface: `ChatClient` (Add, Chat, Ask, SetTools) — defined in the module
- ReAct loop orchestration with automatic tool calling and JSON Schema parameter definitions
- Module docs: [chat.go.phpboyscout.uk](https://chat.go.phpboyscout.uk); GTB-side details in `docs/explanation/components/chat/`. Keep the core↔provider modules at matching minor versions (see the module's compatibility matrix).

### Service Lifecycle (Controls)

Service lifecycle orchestration — startup ordering, health monitoring, and graceful shutdown — lives in the extracted `gitlab.com/phpboyscout/go/controls` module. The server-side transports were extracted to `gitlab.com/phpboyscout/go/transport` (health/readiness/management HTTP endpoints and the gRPC remote-management server), with `gitlab.com/phpboyscout/go/httpclient` and `gitlab.com/phpboyscout/go/grpcclient` as the client halves. GTB retains thin config adapters that wire these modules from `Props`: `pkg/http/` and `pkg/grpc/` now hold only the `*FromContainable`-style settings adapters, not the transports themselves.

### Error Handling

Error handling is the extracted `gitlab.com/phpboyscout/go/errorhandling` module, built over `gitlab.com/phpboyscout/go/errors`: user-facing hints (`WithHint`/`WithHintf`), help channel config (Slack/Teams), and stack traces in debug mode.

**Nothing in it exits the process.** `Fatal` reports and *returns* the exit code it believes the process should use; `pkg/cmd/root`'s `Execute` owns termination. A terminal-but-successful error carries an `Outcome` rather than earning a branch in `Execute` — see `ErrUpdateComplete`, which exits zero and reports at warn because its `Outcome` says so.

### Version Control (VCS)

The GitHub/GitLab (and Enterprise, Bitbucket, Gitea) abstraction for auth, PR management, and release asset operations was extracted to the `gitlab.com/phpboyscout/go/forge` module and its per-provider modules (`forge-github`, `forge-gitlab`, `forge-gitea`, `forge-bitbucket`), plus `gitlab.com/phpboyscout/go/repo` for local repository operations. GTB's `pkg/vcs/` is now a thin config adapter wiring `go/forge` from resolved config; it is used by the update and init subsystems.

### Setup & Bootstrap

`pkg/setup/` handles first-run bootstrap: auth configuration, SSH key management, and pluggable self-updating from GitHub/GitLab releases.

### TUI Components

`pkg/docs/` implements the built-in interactive markdown documentation browser. Structured output formatting was extracted to the `gitlab.com/phpboyscout/go/output` module. The former `pkg/forms/` interactive terminal UI components (prompts, selections, inputs) built on Bubble Tea have been **removed**, pending a rewrite on huh v2 (see the forms rewrite spec) — do not restore the deleted package.

### Code Generation

`internal/generator/` uses `dave/dst` and `dave/jennifer` for AST-level Go code generation. The `generate`/`regenerate`/`remove` commands scaffold new CLI tools that extend this framework.

Every user-influenced field flowing into a skeleton template is validated by `internal/generator/validate.go` (NFC-normalised, field-specific character-class rules) and piped through one of the helpers in `template_escape.go` at non-code render sites (`escapeYAML`, `escapeMarkdown`, `escapeTOML`, `escapeComment`, `escapeMarkdownCodeBlock`, `escapeShellArg`). See `docs/development/template-security.md` when adding a new user-facing field.

### Testing

- Mocks live in `mocks/` and are generated by mockery.
- Table-driven tests with `t.Parallel()` is the standard pattern.
- Use `logger.NewNoop()` for test loggers.
- **No package-level mocking hooks.** Do not create `var execFoo = exec.Foo` for test mocking — this pattern races under `t.Parallel()`. Inject dependencies through functional options, struct fields, or `Config` fields. Use `internal/exectest` for common `exec.LookPath` / `exec.CommandContext` fakes. See `docs/how-to/testing.md` for the full race-avoidance guide and the `internal/exectest` API.
- **Integration tests** use env-var-based gating (not build tags) for compile-time safety and IDE discoverability:
  - Gate with `testutil.SkipIfNotIntegration(t, "tag")` from `internal/testutil/integration.go`.
  - `INT_TEST=1` enables all; `INT_TEST_<TAG>=1` enables a specific group (e.g. `INT_TEST_VCS=1`).
  - Integration tests live in dedicated `*_integration_test.go` files.
  - See `docs/development/integration-testing.md` for the full test inventory and writing guidelines.
- **E2E BDD tests** use [Godog](https://github.com/cucumber/godog) (Cucumber for Go) for behaviour-driven scenarios:
  - Feature files in `features/`, step definitions in `test/e2e/steps/`.
  - CLI scenarios use a dedicated test binary (`cmd/e2e/`) with all feature flags enabled.
  - Gated by `INT_TEST_E2E=1`; subsystem filters: `INT_TEST_E2E_SMOKE=1`, `INT_TEST_E2E_CONTROLS=1`, `INT_TEST_E2E_CLI=1`.
  - Run via `just test-e2e` (all) or `just test-e2e-smoke` (fast).
  - **New CLI commands or service lifecycle changes must include Gherkin scenarios.** Evaluate BDD fit using the suitability assessment in the strategy spec.
  - See [`0044-godog-bdd-strategy`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0044-godog-bdd-strategy) for the full strategy.

### URL Opening

All URL-opening in GTB — and in tools built on GTB — must route through `browser.OpenURL` from the extracted `gitlab.com/phpboyscout/go/browser` module. Do not call `github.com/cli/browser.OpenURL` or `exec.Command("open"|"xdg-open"|"rundll32")` directly. `go/browser` enforces a scheme allowlist (`https`, `http`, `mailto`), a URL-length bound, and control-character rejection before invoking the OS handler. Callers constructing `mailto:` URLs from user-influenced data must additionally `url.QueryEscape` every parameter value — see `pkg/telemetry.EmailDeletionRequestor` for the canonical pattern and `docs/explanation/components/browser.md` for the threat model.

### Regex Compilation

Any `regexp.Compile` call whose pattern originates outside the binary (config file, CLI flag, TUI input, HTTP payload, message queue) must route through `regexutil.CompileBounded` or `CompileBoundedTimeout` from the extracted `gitlab.com/phpboyscout/go/regexutil` module. The helper enforces a 1 KiB length cap and a 100 ms compile timeout to mitigate ReDoS. Literal patterns known at build time may continue to use `regexp.MustCompile`. See `docs/explanation/components/regexutil.md` for the full threat model and call-site guidance.

### Chat Provider Endpoints

`chat.Config.BaseURL` values must pass `chat.ValidateBaseURL`. The validator rejects non-HTTPS schemes, URLs containing userinfo (`user:pass@host`), and placeholder hosts (`example.com` and subdomains). Tests targeting an `httptest.Server` set `Config.AllowInsecureBaseURL: true`; that field is `json:"-"` so config files cannot downgrade HTTPS enforcement. Every successful `chat.New` call logs the endpoint hostname at INFO — never the path or query. See `docs/explanation/components/chat/` § Provider endpoint security.

### Credential Redaction

Use the extracted `gitlab.com/phpboyscout/go/redact` module for any free-form string written to telemetry, distributed logs, or a third-party observability surface. `redact.String` strips URL userinfo, common credential query parameters, Authorization headers, well-known provider prefixes (`sk-`, `ghp_`, `AIza`, `AKIA`, Slack), and very long opaque tokens. `TrackCommandExtended` already applies it automatically to `args` and `errMsg`; HTTP middleware uses `redact.SensitiveHeaderKeys` to redact headers at DEBUG. See `docs/explanation/components/redact.md`.

### Credential Storage

Credential storage is the extracted `gitlab.com/phpboyscout/go/credentials` module (with the opt-in `go/credentials/keychain` backend). User-supplied secrets (AI API keys, VCS tokens, Bitbucket app passwords) are stored via one of three modes selected by the setup wizard: env-var reference (recommended default), OS keychain (opt-in blank import of `go/credentials/keychain`), or literal in config (legacy). Literal mode is refused under `CI=true`. Resolution precedence at runtime: `{provider}.api.env` or `auth.env` → env var → `{provider}.api.keychain` or `auth.keychain` → `{provider}.api.key` or `auth.value` literal → well-known fallback env var. The `doctor` command's `credentials.no-literal` check warns when any literal credential is present in config. Keychain mode is activated by a blank import of `gitlab.com/phpboyscout/go/credentials/keychain` in the tool's `main` (see `cmd/gtb/keychain.go`); regulated downstreams omit the import, and linker dead-code elimination keeps go-keyring and its transitive deps out of the linked binary. See the `go/credentials` and `go/credentials/keychain` modules, and [`0054-credential-storage-hardening`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0054-credential-storage-hardening).

**Forge credential precedence is GTB's, not the forge module's.** `go/forge` v0.8.0 moved ordering out of the module and into the consumer, so `pkg/vcs/credential.go` composes the chain — `ForgeCredential` — and it is the single place the order is stated. Rungs 1 and 2 are *pointers*: `auth.env` names an environment variable and `auth.keychain` names a `service/account` entry, and the source dereferences it. That indirection is why GTB keeps `auth.env` rather than letting a prefixed env layer supply the value directly — a layer can carry a credential, but it cannot express "read whichever variable this deployment names", which is what lets a CI job redirect a token without rewriting config.

Two constraints on that code:

- **Every rung is lazy.** Nothing resolves until the returned `CredentialSource` is called, so a repository authenticating over SSH never triggers a keychain unlock prompt for a token it does not need. The keychain rung is a caller-supplied source rather than something reached through the config stack precisely because a `config.Backend` contributes from `Load`, which runs at store construction — so a keychain-backed backend would put an OS unlock prompt on the startup path of every command, `--help` included.
- **`forge.ConfigCredential` must not be used.** Its stale-key report exempts only the key it was pointed at and probes the relative `auth.env`/`auth.keychain` — exactly the keys GTB ships defaults for — so pointed at `auth.value` it reports a working configuration as stale. `TestConfigCredentialIsNotUsed` guards this.

`doctor` reports resolution as well as storage: a per-forge `<Forge> credential` check names the winning rung (`auth.env`, `auth.keychain`, `auth.value`, or the fallback variable) and never the value, and diagnoses a configured-but-broken credential rather than reporting it as absent. Note that every forge bundle ships `<forge>.auth.env: <FORGE>_TOKEN` as a default, so rung 1 already reads the variable the fallback rung would — the fallback is effectively shadowed for shipped forges, and `GITHUB_TOKEN` reports as `resolves from auth.env`. Which config layers a tool wires is declarable via `props.Tool.ConfigLayers`; unstated means the framework default, and the keychain is not among them. See [`0183-forge-credentials-and-configurable-config-layers`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0183-forge-credentials-and-configurable-config-layers).

### Release Signing (sign / keys commands)

The signing *logic* is the extracted `gitlab.com/phpboyscout/go/signing` module (backend registry) plus `go/signing/openpgpkey` and backends like `go/signing-aws-kms`. The `sign` and `keys` (`mint`/`generate`/`wkd`) **Cobra command builders** are the extracted `gitlab.com/phpboyscout/go/signing-cli` module — props-decoupled behind a narrow `Logger` interface (a `*slog.Logger` and GTB's `logger.Logger` both satisfy it), with constructors returning plain `*cobra.Command`. GTB re-attaches them in `internal/cmd/root` via `setup.Wrap("", signingcli.NewCmdSign(p.GetLogger()))` (the `gtb sign`/`gtb keys` CLI is unchanged); the standalone `sigillum` CLI (`gitlab.com/phpboyscout/sigillum`) attaches the same builders as top-level commands. Because `go/signing-cli` depends only on `go/signing` + cobra — never on GTB — there is no module cycle. Signing backends are registered by the host binary via blank import (`cmd/gtb/signing.go`); a build that omits a backend drops that SDK through dead-code elimination. See [`0176-ed25519-kms-signing`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0176-ed25519-kms-signing) and [signing-cli.go.phpboyscout.uk](https://signing-cli.go.phpboyscout.uk).

## Linting

Config in `.golangci.yaml` (v2 format, 50+ linters). Local import prefix: `gitlab.com/phpboyscout/go-tool-base`. Disabled linters: `perfsprint`, `wrapcheck`, `wsl`.

**Lint resolution order** (simplest to most complex): `errcheck` → `gocritic` → `staticcheck` → `exhaustive` → `nestif` → `cyclop`. Run tests after every structural fix.

## Release

Releases use the **Release-MR** pattern via [releaser-pleaser](https://releaser-pleaser.dev/) (a GitLab CI/CD component) — **do not manually tag**. On merges to `main`, releaser-pleaser opens/updates a "Release" MR containing the pending version bump and `CHANGELOG.md` entries. Merging that Release MR (the human gate) creates the `vX.Y.Z` tag and the GitLab Release with notes. The tag triggers the `goreleaser` job, which builds for darwin/linux/windows × amd64/arm64 (CGO disabled, FIPS) and **attaches binaries** to the existing Release (`release.mode: keep-existing`) — releaser-pleaser owns the changelog/notes, GoReleaser owns the artefacts. macOS binaries are notarized; a Homebrew **cask** (`homebrew_casks:` in `.goreleaser.yaml`, not a formula) is auto-updated on the GitLab-hosted tap at `gitlab.com/phpboyscout/homebrew`, pushed over SSH with a deploy key because GoReleaser's `token:` field is GitHub-only. Users install it with `brew install --cask gtb` after tapping that URL.

To cut a release: merge the open Release MR. To force a release from non-releasing commits, or to set a pre-release, add an `rp-next-version::*` label to the Release MR.

Pre-release check: run `just ci`, then `goreleaser check`, then `just snapshot` to verify `dist/` output.
