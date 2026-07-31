---
title: "Implementation notes — bootstrap survives child PersistentPreRunE"
description: "Notes, judgment calls, and open questions for the EnableTraverseRunHooks fix."
date: 2026-06-13
tags: [implementation-notes, cmd, middleware, cobra]
---

# Implementation notes — bootstrap-prerun-traversal

Spec: [`0073-bootstrap-prerun-traversal`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0073-bootstrap-prerun-traversal)
Branch: `fix/bootstrap-prerun-traversal`

## What I implemented

Per the spec's resolved design (D1, D2, D3):

1. **D1 — `cobra.EnableTraverseRunHooks = true`** set at the top of
   `NewCmdRootWithConfig` in `pkg/cmd/root/root.go` (which `NewCmdRoot`
   delegates to, so both entry points are covered). With this, cobra runs
   **every** `PersistentPreRunE` from root to leaf instead of only the closest,
   so the framework bootstrap (root hook) always runs first and a downstream
   child hook runs after it. Confirmed the flag was not previously set anywhere.

2. **D2 — no opt-out.** Bootstrap always runs; nothing added to suppress it.

3. **D3 — one-time debug log + docs.** Added `logShadowingPreRunHooks` (with
   `commandTreeHasPersistentPreRun`), called once after the full tree is
   assembled in `NewCmdRootWithConfig`. If any non-root command in the tree
   defines a `PersistentPreRunE`/`PersistentPreRun`, it emits a single
   debug-level log noting the hook runs AFTER the framework bootstrap, not
   instead of it. Updated:
   - `docs/concepts/command-middleware.md` — new "Hooks vs. the framework
     bootstrap" section + amended the "Middleware vs. Hooks" tip.
   - `docs/how-to/nested-subcommands.md` — new subsection on subcommand
     `PersistentPreRunE` ordering, cross-linking the concept doc.

## Tests (TDD)

Added to `pkg/cmd/root/root_test.go`:

- `TestBootstrapRunsDespiteChildPersistentPreRunE` — the core regression: a
  child with its own `PersistentPreRunE` is executed via the full
  `rootCmd.ExecuteContext`, and we assert `props.Config` **and**
  `props.Collector` are populated (proving the root bootstrap ran). Verified it
  FAILS with the flag off and passes with it on.
- `TestBootstrapOrdering_RootBeforeChild` — captures `props.Config` from inside
  the child hook and asserts it is already non-nil, proving root→leaf ordering.

Both use a `bootstrapTestProps` helper (network/prompt features disabled,
`logger.NewNoop()`, a real in-memory config file pointed at via `--config`).
They follow the existing non-parallel + `setup.ResetRegistryForTesting()` +
`t.Cleanup` pattern because `NewCmdRoot` seals the global middleware registry.

## Judgment calls

- **Where to set the flag.** Set it inside `NewCmdRootWithConfig` rather than an
  `init()` so it is scoped to GTB root construction and easy to find. It is a
  cobra process-global; the spec explicitly accepts this (only the root defines
  a persistent pre-run hook in GTB's tree, so root→leaf traversal is otherwise a
  no-op). `gochecknoglobals` does not flag assigning a third-party package var.

- **D3 detection site.** Implemented as a post-registration tree walk in the
  root constructor rather than hooking into `setup.Command.Register`. This keeps
  `pkg/setup` untouched (no logger dependency added there) and gives a single,
  deterministic "once per tool" log. The walk also checks the deprecated
  `PersistentPreRun` (non-E) form for completeness.

- **Test config strategy.** I kept `InitCmd` enabled and wrote a real config
  file rather than disabling `InitCmd`. Disabling `InitCmd` flips `allowEmpty`
  true and makes bootstrap look up the embedded `assets/init/config.yaml`, which
  an empty test `Assets` doesn't have — an unrelated failure mode. A real
  `--config` file keeps the test focused on the traversal behaviour.

## Verification status

- `go build ./...` — clean.
- `go test ./pkg/cmd/root/...` — pass (also `-race` on root + setup).
- `golangci-lint run pkg/cmd/root/...` — 0 issues. No `//nolint` added.
- E2E (verification-plan item 4): added the `config-probe` fixture command in
  `cmd/e2e/config_probe.go` (defines its own `PersistentPreRunE` and reads
  `probe.value` from `props.Config` in `RunE`) and the
  `features/cli/bootstrap_traversal.feature` Gherkin scenario. Runs green under
  `INT_TEST_E2E=1 INT_TEST_E2E_CLI=1` and the `@smoke` set
  (`INT_TEST_E2E_SMOKE=1`); needs no external services. Asserts both the child
  hook marker and the config-loaded value print, proving root→leaf traversal.

## Deferred / out of scope

- **O3 (middleware unification)** — moving bootstrap to be the outermost
  registered middleware instead of a `PersistentPreRunE` remains deferred per
  the spec. Not touched here.

## Questions for Matt

1. ~~Do you want an E2E BDD scenario (verification-plan item 4: scaffolded tool
   with a custom subcommand hook resolving config correctly)?~~ **Resolved:**
   added. A contrived `config-probe` subcommand in `cmd/e2e/` (its own
   `PersistentPreRunE` + a `RunE` that reads `probe.value` from `props.Config`)
   plus `features/cli/bootstrap_traversal.feature`. The scenario runs the
   subcommand against a known `--config` file and asserts the config value
   prints (root bootstrap ran) and the child-hook marker prints (both hooks
   ran, in order). Tagged `@cli @smoke`; green under `just test-e2e-smoke` and
   the CLI gate. The `cmd/e2e` binary enables all feature flags, which is the
   closest standing analogue to a scaffolded tool with a custom hook.
2. The D3 shadowing notice is a **debug** log emitted once at construction time.
   Comfortable with debug level, or would you prefer it surfaced more loudly
   (e.g. info) for tool authors during development?
3. Generator templates: scaffolded tools call `root.NewCmdRoot`, so they inherit
   the fix automatically — no template change needed. Confirm you're happy there
   is nothing to change in `internal/generator/`.
