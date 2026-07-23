---
title: "Bootstrap exemptions for auxiliary commands: help, completion, __complete, and the init subtree"
description: "The root pre-run runs the full framework bootstrap for cobra's auto-generated help, completion and hidden __complete commands, so on a fresh install (no config file) 'tool help' and shell completion exit non-zero, and every tab-completion keystroke pays the bootstrap — including the update check. The fix is annotation-based exemption using the existing setup.FeatureOf/SkipConfigCheck mechanism, replacing the fragile Use-string skip list in SkipUpdateCheck, extending the InitCmd exemption to the whole init subtree, and attaching a run-init hint to the missing-config error."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - bootstrap
  - cli
  - root-command
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Bootstrap exemptions for auxiliary commands: help, completion, __complete, and the init subtree

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED — 2026-07-23

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (GTB-core §HIGH
    auto-generated-command bootstrap + §LOW SkipUpdateCheck Use-strings + §LOW
    `init <provider>` config-gating findings)

---

## 1. Problem

The root `PersistentPreRunE` (`pkg/cmd/root/root.go:784-867`) performs the full framework
bootstrap — flag extraction, config-store construction with the missing-config gate,
telemetry consent, collector wiring, update check — for **every** runnable command.
With `cobra.EnableTraverseRunHooks`, that includes cobra's auto-generated `help`,
`completion <shell>` and hidden `__complete` commands, none of which are exempted: the
only fast-path is the `InitCmd` feature match at `root.go:796`.

Concrete failures on the current code path:

- **Fresh install breaks help and completion.** On a scaffolded tool with no config file
  yet (InitCmd enabled by default, `AutoInitialise` opt-in), `resolveBootstrapConfig`
  returns `ErrNoConfigFile` (`root.go:214`), so `tool help` and `tool completion bash`
  exit non-zero with `failed to load configuration: no config file found`
  (`root.go:810-813`) — with **no hint** telling the user to run `<tool> init`. Shell
  tab-completion (`__complete`) errors on every keystroke.
- **Completion pays the bootstrap even when config exists** — including the
  throttled-but-sometimes-real network update check and, for telemetry-enabled tools,
  the consent-prompt attempt.
- **`SkipUpdateCheck` matches fragile `Use` strings** (`pkg/setup/update.go:572-577`):
  the skip list `{"update","auth","init","version"}` is compared against the leaf
  `cmd.Use`, which may carry an args suffix, collides with any downstream command
  coincidentally named `auth`, and omits `mcp`, `doctor`, `help` and `completion`. The
  codebase already owns the right mechanism — the typed feature annotation stamped by
  `setup.Wrap` and read via `setup.FeatureOf`, used for exactly this reason at
  `root.go:792-796` (the comment there names the Use-string footgun).
- **`init <provider>` is config-gated on a fresh install** — the same mechanism gap.
  The pre-run's exemption matches only `FeatureOf(cmd) == p.InitCmd`, i.e. the init
  *leaf*; provider subcommands (`init github`, `init bitbucket`) are registered via
  `registerSubcommands` (`pkg/cmd/initialise/init.go:104-114`) and wrapped with their
  *provider* feature, so on a machine with no config file they fail the missing-config
  gate instead of running — the one command tree that exists to fix that state.

## 2. Proposed change

All changes use the existing typed-annotation machinery (`setup.Wrap`/`FeatureOf`,
`setup.SkipConfigCheck`/`SkipsConfigCheck` — `pkg/setup/command.go`) rather than adding
string matching.

1. **Pre-run fast path for cobra's auxiliary commands.** In `newRootPreRunE`, before
   `resolveBootstrapConfig`, return early (after the same debug-logging setup the
   `InitCmd` branch performs) when `cmd.Name()` is `help`,
   `cobra.ShellCompRequestCmd`, `cobra.ShellCompNoDescRequestCmd`, or the command is the
   root-registered `completion` group (match the built-in by identity/parentage, not by a
   downstream-colliding name where feasible; cobra's generated commands carry no
   annotations, so `Name()` against cobra's own constants is the correct discriminator
   here — these names are cobra-reserved, unlike `auth`).
2. **Exempt the whole init subtree.** Replace the leaf-only check at `root.go:796` with
   a walk up `cmd.Parent()` until a command with `FeatureOf == p.InitCmd` is found (or
   annotate provider subcommands with `setup.SkipConfigCheck` at registration in
   `registerSubcommands`). Either way, `init github` on a configless machine must run.
3. **Annotation-based `SkipUpdateCheck`.** Replace the `Use`-string list at
   `pkg/setup/update.go:572-577` with annotation matching: skip when
   `FeatureOf(cmd)` ∈ {`UpdateCmd`, `InitCmd`} and introduce a
   `setup.SkipUpdateCheckAnnotation` (mirroring `SkipConfigCheckAnnotation`) stamped on
   `version` (which is wrapped with the empty feature, `pkg/cmd/version/version.go:102`)
   and on the forge `auth` command — so the exemption is rename-safe and cannot collide
   with downstream commands named `auth`. Add `mcp`, `doctor`, `help` and `completion`
   to the skip set via the same annotation at their construction sites.
4. **Hint on the missing-config error.** Where the pre-run wraps the load failure
   (`root.go:811-813`), attach
   `errors.WithHintf(err, "Run '%s init' to create a configuration.", props.Tool.Name)`
   when `errors.Is(err, ErrNoConfigFile)` and the init feature is enabled (fall back to
   a plain "no config file found at <paths>" hint when it is not).

## 3. Scope & release plan

- GTB-repo only: `pkg/cmd/root`, `pkg/setup` (annotation + `SkipUpdateCheck`),
  `pkg/cmd/initialise` (subtree exemption), `pkg/cmd/version` (annotation stamp).
  `SkipUpdateCheck`'s signature/semantics change is a public-API break —
  permitted pre-1.0, ships as `feat(root)` (minor) with a note in the commit body and a
  migration note in `docs/migration/`.
- **E2E/BDD implications: yes.** Fresh-install `help`/`completion` behaviour is exactly
  the user-facing workflow the project's BDD strategy targets. Add Gherkin scenarios in
  `features/` (steps in `test/e2e/steps/`, `cmd/e2e` binary): "Given no config file
  exists, When I run `<tool> help`, Then the exit code is 0 and usage is printed";
  likewise for `completion bash` and an `__complete` invocation; plus "init github with
  no config file runs the initialiser". Run with `-count=1` (Godog results are
  go-test-cached).
- Unit tests: pre-run fast-path table tests; `SkipUpdateCheck` annotation tests
  extending `pkg/setup/command_test.go`.
- `/gtb-verify` before the MR; update `docs/components/` for the root pre-run and setup
  command annotations (functional change requires a doc update).

## 4. Acceptance criteria

- On a filesystem with no config file: `tool help`, `tool --help`, `tool completion
  bash|zsh|fish`, and a `__complete` request all exit 0 with correct output and no
  config/update/consent side effects; covered by Gherkin scenarios.
- `tool init github` (and `init bitbucket`) run on a configless machine instead of
  failing the missing-config gate.
- With config present, `__complete` and `completion` never trigger the network update
  check or the telemetry consent prompt (assert via the fake release source / recorded
  prompts in the existing test harness).
- A downstream tool defining an unrelated command named `auth` or `init` gets the normal
  bootstrap (no accidental exemption) — annotation matching, not name matching, decides.
- Running any non-exempt command with no config file produces the existing error **plus**
  a hint naming `<tool> init`.
- `SkipUpdateCheck` has no remaining `cmd.Use` string comparisons.

## 5. Open questions — resolved

1. **Only cobra's own generated command instances are exempted**, not any
   downstream command that happens to share the name. The discriminator
   (`isCobraGeneratedCommand`, `pkg/cmd/root/root.go`) is a direct child of the
   root carrying no annotations — every GTB-wrapped command is stamped by
   `setup.Wrap`, so a downstream feature command named `help` or `completion`
   still gets the normal bootstrap (covered by
   `TestPreRun_DownstreamCompletionCommand_GetsNormalBootstrap`).
2. **The fast path still configures debug logging** (`applyDebugFlag`, shared
   semantics with the former `InitCmd` branch), so `--debug tool completion
   bash` stays debuggable. It is tolerant of `__complete`, whose flags cobra
   never parses.
3. **Both.** Cobra's auxiliary commands are exempted intrinsically, and a
   `Tool.Bootstrap.AuxiliaryCommands` name/path list (matching
   `SkipConfigCheck`'s semantics via `MatchesAuxiliaryList`) lets downstream
   tools extend the fast-path set without a GTB release. Auxiliary commands run
   with `props.Config` nil — stronger than `SkipConfigCheck`'s tolerant load.

### Implementation notes

- `SkipUpdateCheck` keeps its signature; only the exemption semantics changed
  (annotation/feature parent-chain walk, no `Use` strings). `help`/`completion`
  need no update-check annotation because the pre-run fast path returns before
  the check is ever reached; there is no construction site at which to stamp
  cobra's generated instances. The forge `auth` command named by §2.3 no longer
  exists post-extraction, so no stamp was needed there.
- Migration note: `docs/reference/migration/v0.x-update-check-annotations.md`.
- The `init github` fresh-install path is covered by unit test
  (`TestPreRun_FreshInstall_InitSubtreeRuns`) rather than a Gherkin scenario:
  the provider wizard reaches an unguarded `form.Run()` and huh hangs on
  non-terminal stdin (the MR !157 failure mode), which would risk hanging the
  e2e suite. Gherkin covers `help`, `completion bash`, `__complete` and the
  run-init hint (`features/cli/fresh_install.feature`).
