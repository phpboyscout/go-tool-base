---
name: bdd-when-and-how
description: Decide when a Gherkin/Godog BDD scenario earns its keep versus a unit test, then write it with the right tags and layout. Use when a change involves a CLI command, a multi-step user workflow, or service-lifecycle coordination.
---

# BDD: when, and how

Use this when you're deciding whether a change needs a Cucumber-style
(Gherkin/[Godog](https://github.com/cucumber/godog)) scenario, and how to write
one. BDD is not a replacement for unit tests — it's a different altitude. Get the
altitude wrong and you either under-test user-facing behaviour or drown internal
logic in slow, brittle scenarios.

## When a scenario earns its keep

Write a Gherkin scenario when the behaviour is **user-facing and has a
Given/When/Then shape**:

- A **CLI command** with visible output, flags, or error messages.
- A **multi-step workflow** — `init → configure → run`.
- **Service-lifecycle coordination** — `start → health check → shutdown`.
- Anything the spec already describes in Given/When/Then terms.

These are exactly the behaviours a unit test expresses awkwardly (lots of wiring)
but a scenario expresses naturally — and readably, for a reviewer who isn't deep
in the code.

## When NOT to

Keep it out of BDD when:

- It's **pure library logic** well covered by table-driven unit tests.
- It's an **internal implementation detail** (AST manipulation, config-merge
  internals) — testing those through a CLI scenario is indirection for its own
  sake.
- It's an **interactive TUI flow** needing simulated keystrokes — usually more
  cost than signal.

The division of labour: unit tests carry the *matrix* (every input, every error
path); BDD covers the *wired-together happy and sad path* end to end. Don't
duplicate the matrix in Gherkin.

## How to write it

1. **Feature files** live under `features/<area>/` (e.g. `features/cli/`,
   `features/controls/`), one `.feature` per behaviour.
2. **Step definitions** live in one place (e.g. `test/e2e/steps/`) — **reuse
   existing steps** before writing new ones; a sprawl of near-duplicate steps is
   the main way BDD suites rot.
3. **Tag every scenario** so suites are selectable. go-tool-base uses an area tag
   plus a speed/kind tag: `@cli @smoke`, `@controls @integration`,
   `@controls @integration @slow`. Tags drive both the env gates and which subset
   CI runs.
4. CLI scenarios drive a **dedicated test binary** with all feature flags
   enabled, so every command is reachable regardless of a tool's default
   feature set.
5. Gate the suite by env var, same mechanism as
   [env-gated-integration-tests](../env-gated-integration-tests/SKILL.md):
   `INT_TEST_E2E=1` for all, `INT_TEST_E2E_SMOKE=1` for the fast subset,
   `INT_TEST_E2E_CLI=1` for one area.

```gherkin
@controls @integration @smoke
Scenario: A 2 rps limiter rejects a five-request burst
  Given an HTTP server with a 2 rps rate limiter
  When 5 rapid GET requests are sent to "/"
  Then 2 of the requests succeed with status 200
  And 3 of the requests are rejected with status 429
```

## Make it a spec decision

When you draft a [spec](../../../phpboyscout-common/skills/spec-driven-development/SKILL.md),
include a short **BDD suitability** assessment in the testing strategy: does this
feature have user-facing workflow behaviour, yes or no, and which scenarios.
Deciding it up front stops BDD from being an afterthought (untested workflows) or
an over-reach (scenarios for pure library code).
