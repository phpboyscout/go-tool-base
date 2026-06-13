---
title: "Implementation notes — Flag-to-config binding"
description: "What shipped for the flag-to-config-binding spec, deviations, and open questions for review."
date: 2026-06-13
tags: [implementation-notes, config, cmd, flags]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Implementation notes — Flag-to-config binding

Spec: [2026-06-12-flag-to-config-binding](../specs/2026-06-12-flag-to-config-binding.md) (status: IMPLEMENTED).

## What was implemented

### D1 — `WithBoundFlags` + convention helper (`pkg/cmd/root/options.go`)

- New `RootOption` functional-option type and an extensible constructor
  `NewCmdRootWithOptions(props, opts ...RootOption)`. The existing
  `NewCmdRoot` and `NewCmdRootWithConfig` are now thin wrappers over it, so all
  existing call sites and the generator's AST injection into `NewCmdRoot` are
  unchanged.
- `WithBoundFlags(map[string]*pflag.Flag)` — explicit config-key → flag map.
- `WithConventionBoundFlags(*pflag.FlagSet)` — derives keys from flag names via
  the hyphen-to-dot convention.
- `ConventionKey(name)` — exported `strings.ReplaceAll(name, "-", ".")` so
  downstreams can compute the same keys.
- `WithSubcommands` and `WithConfigPaths` were added so the wrapper constructors
  compose cleanly through options.
- Both flag options register the supplied flags onto the root command's
  persistent flag set (so cobra parses them) in addition to recording them for
  binding. This is the key ergonomic decision — see deviations below.

### D2 — Bind during config load, changed flags only (`pkg/cmd/root/root.go`)

- Binding happens in `newRootPreRunE`, immediately after `props.Config = cfg`
  (i.e. after the file + env layers are established) via the new
  `bindCommandFlags` helper. Viper's `BindPFlag` sits above `AutomaticEnv`, so
  no custom precedence logic was needed.
- `bindChangedFlags` filters by `flag.Changed`, so a flag at its default never
  clobbers config. Bind errors are logged at debug and skipped (one bad flag
  never aborts startup).

### D3 — Built-ins folded through the same path

- `--ci` and `--debug` are bound to config keys `ci` / `debug` through the same
  `bindChangedFlags` path (`builtinBoundFlags`). `Config.GetBool("ci")` now
  reflects `--ci`.
- `--debug`'s **immediate** log-level effect is preserved: `configureLogging`
  still reads `flags.Debug` from `extractFlags` independently of binding. Only
  config-visibility changed, exactly as the spec required.

### D4 — Docs reconciled

- `docs/concepts/config.md` previously listed **env > .env > flags > files**.
  Corrected to the canonical **flags > env > file > embedded > defaults**, with
  a new "Binding Flags to Config" subsection.
- `docs/components/config.md` already documented flags-first; added a "Binding
  CLI flags to config" section, the `BindPFlag` method in both interface
  listings, and replaced the manual `if dbHost == ""` example narrative.
- New how-to: `docs/how-to/bind-flags-to-config.md` (linked from
  `docs/how-to/index.md`).

### D5 — Per-command binding

- `bindCommandFlags` binds the dispatched command's **local** flags
  (`cmd.LocalFlags()`) by the hyphen-to-dot convention, filtered by `Changed`.
  Inherited persistent flags are skipped (the root binds its own). The built-in
  keys are excluded from convention binding to avoid double-mapping.
- The root `PersistentPreRunE` runs for the command's own subtree, so per-command
  binding works on plain `main` without depending on the unmerged
  cobra-traverse-hooks change.

### Container API (`pkg/config/container.go`)

- Added `BindPFlag(key string, flag *pflag.Flag) error` to the `Containable`
  interface and `*Container`. It routes through `resolverViper()` and
  `qualifyKey()`, so binding works correctly on `Sub()` containers
  (env-aware delegation preserved). Mock regenerated
  (`mocks/pkg/config/Containable.go`).

## Deviations from the spec (and why)

1. **New constructor instead of changing `NewCmdRoot`'s signature.** The spec's
   illustrative shape implied options on `NewCmdRoot`. `NewCmdRoot` already takes
   variadic `*setup.Command`, and the generator injects subcommands into that
   call by AST. To avoid a breaking signature change and generator churn, options
   live on a new `NewCmdRootWithOptions`; the old constructors delegate to it.
2. **Bound-flag options also register the flags on the root command.** Binding
   needs the *same* `*pflag.Flag` object cobra parses. Requiring authors to both
   register a flag and pass its pointer separately is error-prone, so the options
   call `PersistentFlags().AddFlag` for any supplied flag not already present.
   This makes the one-call ergonomic correct by construction.
3. **Per-command binding uses `LocalFlags()` + convention only** (no per-command
   explicit map option in this pass). The spec's D5 calls for per-command binding
   filtered by `Changed`; convention binding of a command's own flags satisfies
   the stated example (`serve --server-port`) with zero boilerplate. An explicit
   per-command map can be layered on later if a downstream needs non-conventional
   keys (see open questions).

## Verification

- New tests: `pkg/config/bindflag_test.go` (container), `pkg/cmd/root/bindflag_test.go`
  (E2E through `PersistentPreRunE`: explicit, convention, precedence flag>env>file,
  built-ins, per-command), `pkg/cmd/root/options_internal_test.go` (option/helper
  edge cases). `options.go` helpers at 100% coverage.
- `just test` green; `just test-race` green; `golangci-lint run` on the changed
  packages (`pkg/cmd/root`, `pkg/config`, `mocks/pkg/config`) reports **0 issues**.
- NOTE: a full-module `just lint` fails on **pre-existing** wsl_v5 findings in
  committed mock files (and leakage from a sibling git worktree in this
  environment); this is unrelated to and not introduced by this change — the
  baseline (`git stash` of this branch) fails identically.

## Open questions for review

1. **Built-in key names.** `--ci`/`--debug` bind to bare keys `ci`/`debug`. If a
   downstream nests these (e.g. `runtime.ci`), the binding won't reach them.
   Acceptable? The previous behaviour read bare `ci` from config, so this is
   consistent.
2. **Convention mapping of dotted flag names.** A flag literally named with a dot
   (`--a.b`) maps verbatim to key `a.b`. We map by hyphen→dot only; dots are not
   themselves transformed. Documented as "avoid dots in flag names". Is an
   explicit reject/warn warranted instead of silent verbatim mapping?
3. **Per-command explicit binding.** Only convention-based per-command binding
   shipped (deviation 3). Do we want a `WithCommandBoundFlags(cmd, map)` style
   option for non-conventional per-command keys, or is convention sufficient?
4. **Root local flags via convention.** When the *root* command runs with no
   subcommand, its local flags (`--config`, `--output`) are convention-bound when
   changed (`--output` → `output`, `--config` → `config`). Binding `output` is
   useful; binding `config` (a `[]string`) is harmless but unused. Leave as-is, or
   exclude `--config`/`--output` from convention binding like the built-ins?
