---
name: verify-before-pr
description: Run the full local quality gate before raising a PR — tests, race detector, lint, regenerated mocks, coverage, and a simplify pass — so CI confirms rather than discovers. Use before opening or updating a merge request on a Go project.
---

# Verify before the PR

Use this right before you open or update a merge request. The goal: CI should
*confirm* a green state you already established locally, not be where you discover
breakage. Run the gate in order — each step can invalidate a later one, so don't
skip ahead.

## The gate, in order

1. **Tests.** `just test` (or `go test ./...`) — the full suite, green.
2. **Race detector.** `go test -race ./...` — mandatory, not optional.
   Concurrency bugs that pass without `-race` are the ones that page you later.
3. **Lint, auto-fixed first.** `golangci-lint run --fix` clears the mechanical
   issues; resolve the rest deliberately (see
   [lint triage](#lint-triage-simplest-first)). **Re-run the tests after any
   structural lint fix** — extracting a nested block or splitting a function can
   silently change behaviour.
4. **Mocks.** If you added or changed an interface, regenerate mocks
   (`mockery` / `just mocks`) so they match the current contract — a stale mock
   compiles and lies.
5. **Coverage.** New library code clears the bar — **≥90%** on new `pkg/` code in
   go-tool-base. Coverage is a floor for *exercised behaviour*; don't pad it with
   assertions that never fail.
6. **`go vet ./...`** — and crucially, this also surfaces integration/E2E test
   files that don't compile (they're env-gated off at runtime but still built).
7. **Simplify pass.** Review the *changed* code for reuse, redundant state, and
   over-abstraction before a human does. Fixing it now is cheaper than a review
   round-trip.
8. **E2E**, when the change touches a CLI command, a user-facing workflow, or
   service lifecycle: `just test-e2e` (or the smoke subset for fast feedback).
   New CLI commands/flags need a Gherkin scenario (see
   [bdd-when-and-how](../bdd-when-and-how/SKILL.md)).

If the change affects generated/scaffolded output, also regenerate into a temp
dir, eyeball it, and delete the temp dir.

## Lint triage: simplest first

When the linter returns a pile, fix in increasing order of blast radius so the
small fixes don't get redone after a big one:

`errcheck` → `gocritic` → `staticcheck` → `exhaustive` → `nestif` → `cyclop`

- **errcheck**: handle or explicitly `_ =` the ignored error.
- **staticcheck** deprecations: migrate to the replacement (for a module rename,
  fix the import path then `go mod tidy`).
- **exhaustive**: add every missing enum case — don't paper over with a default
  unless a default is genuinely correct.
- **nestif / cyclop**: extract to **named** helper functions, not closures —
  closures still count toward the function's complexity. Re-run the tests after.

**Don't reach for `//nolint`.** Suppressing a linter hides a real issue; fix the
root cause. Reserve a narrowly-scoped, commented `//nolint:<linter>` for the rare
genuinely-unavoidable case (an intentional-by-design construct the linter can't
know about), never as a way to move on.

## Then publish

A clean gate means the PR is ready. Hand off to
[drive-ci-from-the-cli](../../../phpboyscout-common/skills/drive-ci-from-the-cli/SKILL.md)
to open it, watch the pipeline, and merge on green — and remember the commit and
publish discipline (Conventional Commits, no AI attribution, no `@`-mentions).
