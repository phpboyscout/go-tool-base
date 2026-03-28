# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Workflows

The `.agent/` directory contains the primary execution mechanisms for this project. Always prefer these over ad-hoc steps.

| Task | Workflow / Skill |
|------|-----------------|
| Any development or maintenance work | Read `.agent/skills/gtb-dev/SKILL.md` first |
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

1. Check `docs/development/specs/` for an existing spec matching the feature.
2. Only proceed if the spec status is `APPROVED` or `IN PROGRESS`.
3. **Review open questions**: Before writing any code, review the spec for open questions, unresolved design decisions, gaps, or ambiguities. Present them to the user for resolution. Do not begin implementation until each open question is answered or explicitly deferred.
4. For **non-trivial features** (new packages, public API changes, generator modifications, architectural changes) with no existing spec: run `/gtb-spec` to draft one, save it to `docs/development/specs/YYYY-MM-DD-<feature-name>.md` with status `DRAFT`, then pause for human review.
5. For **quick fixes and minor changes** (bug fixes, small refactors that don't alter the public API): proceed directly.
6. Update spec status to `IN PROGRESS` when starting, `IMPLEMENTED` when done.

### Implementation (TDD)

- Write failing tests first, derived from the spec's public API, error cases, and edge cases.
- Implement the minimum code to pass. Refactor. Re-run tests.
- Use `github.com/cockroachdb/errors` for all error creation and wrapping — `go-errors/errors` has been removed.
- New `pkg/` features must have **≥90% test coverage**.
- Never add `//nolint` decorators — always address the root cause.

### Library-First

New features must be implemented in `pkg/` as a reusable component before being exposed via the CLI. When modifying library APIs that affect scaffolded output, also update templates in `internal/generator/`.

### After Implementation

1. Run `/gtb-verify` (tests, race detector, lint, mocks).
2. If generator output was affected: `just build && go run ./cmd/gtb generate <command> -p tmp`, verify `tmp/`, delete it.
3. Update `docs/components/` and `docs/concepts/` — any functional change **must** include a doc update, cross-referenced with the code for accuracy.
4. Run `/simplify` on changed files before raising a PR.

## Commands

This project uses `just` as the task runner:

```bash
just              # Default: tidy, generate, build binary to bin/gtb
just test         # Unit tests with coverage
just test-race    # Race condition detection
just test-integration  # Integration tests (build tag: integration)
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

All commits must follow [Conventional Commits](https://www.conventionalcommits.org/). Semantic-release uses these to determine version bumps.

**Do not commit without explicit user approval.** Present a summary of changes and a proposed message, then wait for confirmation.

**Do not add AI attribution** — no `Co-Authored-By:` trailers naming an AI, no references to AI assistance in commit messages. The committing developer owns the change entirely.

| Type | Release |
|------|---------|
| `feat(scope):` | Minor |
| `fix(scope):` / `perf(scope):` / `refactor(scope):` | Patch |
| `ci:` / `chore:` / `style:` / `docs:` / `test:` | None |
| `BREAKING CHANGE:` footer | Major |

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
Default-enabled: `UpdateCmd`, `InitCmd`, `McpCmd`, `DocsCmd`, `DoctorCmd`.

The binary entry point is `cmd/gtb/main.go`. The `internal/cmd/` packages add GTB-specific commands (`generate`, `regenerate`, `remove`) for scaffolding new CLI tools based on this framework.

### Configuration

`pkg/config/` wraps Viper with hierarchical merging (precedence: CLI flags > env vars > file config > embedded assets > defaults). Hot-reload supported via the `Observable` interface.

### AI Chat Client

`pkg/chat/` provides a unified multi-provider client:
- Providers: Anthropic Claude, Claude Local (CLI binary), OpenAI, OpenAI-compatible, Google Gemini
- Core interface: `ChatClient` (Add, Chat, Ask, SetTools)
- ReAct loop orchestration with automatic tool calling and JSON Schema parameter definitions

### Service Lifecycle (Controls)

`pkg/controls/` orchestrates long-running services with startup ordering, health monitoring, and graceful shutdown. Two transports:
- `pkg/grpc/` — gRPC for remote management
- `pkg/http/` — health/readiness/management HTTP endpoints

### Error Handling

`pkg/errorhandling/` wraps `cockroachdb/errors` with user-facing hints (`WithHint`/`WithHintf`), help channel config (Slack/Teams), and stack traces in debug mode.

### Version Control (VCS)

`pkg/vcs/` abstracts GitHub and GitLab APIs (including Enterprise and nested group paths) for auth, PR management, and release asset operations. Used by the update and init subsystems.

### Setup & Bootstrap

`pkg/setup/` handles first-run bootstrap: auth configuration, SSH key management, and pluggable self-updating from GitHub/GitLab releases.

### TUI Components

`pkg/forms/` provides interactive terminal UI components (prompts, selections, inputs) built on Bubble Tea. `pkg/docs/` implements the built-in interactive markdown documentation browser. `pkg/output/` provides structured output formatting.

### Code Generation

`internal/generator/` uses `dave/dst` and `dave/jennifer` for AST-level Go code generation. The `generate`/`regenerate`/`remove` commands scaffold new CLI tools that extend this framework.

### Testing

- Mocks live in `mocks/` and are generated by mockery.
- Integration tests use the `//go:build integration` tag in dedicated `*_integration_test.go` files.
- Table-driven tests with `t.Parallel()` is the standard pattern.
- Use `logger.NewNoop()` for test loggers.

## Linting

Config in `.golangci.yaml` (v2 format, 50+ linters). Local import prefix: `github.com/phpboyscout/go-tool-base`. Disabled linters: `perfsprint`, `wrapcheck`, `wsl`.

**Lint resolution order** (simplest to most complex): `errcheck` → `gocritic` → `staticcheck` → `exhaustive` → `nestif` → `cyclop`. Run tests after every structural fix.

## Release

Releases are automated via semantic-release on merge to `main` — do not manually tag. GoReleaser (`.goreleaser.yaml`) builds for darwin/linux/windows × amd64/arm64 with CGO disabled and FIPS mode. macOS binaries are notarized; a Homebrew formula is auto-updated.

Pre-release: run `just ci`, then `goreleaser check`, then `just snapshot` to verify `dist/` output.
