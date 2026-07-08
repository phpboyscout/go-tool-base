---
name: library-first-tdd
description: Build the reusable library component before the CLI wires it up, test-first from the spec's contracts, with dependencies injected through a container. Use when adding a feature to a Go CLI/library project that should stay reusable and testable.
---

# Library-first, test-first

Use this when adding a feature to a project that is *both* a library and a CLI
(the go-tool-base shape: a reusable `pkg/` consumed by downstream tools, with a
thin command layer on top). The discipline keeps logic reusable and testable
instead of trapped inside command handlers.

## Library before CLI

**Implement the feature in `pkg/` as a reusable component first; expose it via
the CLI second.** A command handler should be a thin adapter — parse flags, call
the library, render the result. If logic lives in the handler, no other code
(and no test) can reach it without going through Cobra.

- Define interfaces **at the point of use**, narrow to what the caller needs —
  not one giant "service" interface. A package that only logs takes a
  `LoggerProvider`, not the whole world.
- If the project scaffolds downstream tools from templates, update the templates
  in the same change so generated code reflects the new best practice. The
  generator is the source of truth for downstream consistency.

## Test-first, from the spec

Work the [spec](../../../phpboyscout-common/skills/spec-driven-development/SKILL.md)'s
contracts as a TDD loop:

1. **Write failing tests first**, derived from the spec's public API, its data
   model, its error cases, and its edge cases. Table-driven tests with
   `t.Parallel()` are the default shape.
2. **Implement the minimum** to make them pass — follow the spec's signatures and
   types exactly.
3. **Refactor** with the tests green.
4. **Verify**: `go test -race ./...` and the linter before moving on (see
   [verify-before-pr](../verify-before-pr/SKILL.md)).

New library code carries a real coverage bar — **≥90% on new `pkg/` code** is the
go-tool-base line. Coverage is a floor for *exercised behaviour*, not a target to
game.

## Dependency injection through a container

Pass dependencies in; don't reach for globals. go-tool-base threads a `Props`
container (logger, config, embedded assets, a filesystem abstraction, an error
handler, tool metadata) into every command, and packages declare only the narrow
provider interface they need. The payoff is testability: a test constructs the
component with fakes and never touches process-wide state. This is the same
property that [race-safe-test-injection](../race-safe-test-injection/SKILL.md)
depends on — injected seams instead of package-level mutable ones.

Two concrete habits that travel with this:

- **Use a filesystem abstraction** (e.g. `afero.Fs`) for anything that touches
  disk, so tests run against an in-memory FS.
- **One error library, used consistently** — go-tool-base standardises on
  `cockroachdb/errors` for creation and wrapping, with user-facing hints. Pick
  one and don't mix.
