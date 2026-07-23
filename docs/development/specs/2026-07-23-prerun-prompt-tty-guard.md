---
title: "TTY-guard the interactive prompts in the root pre-run (telemetry consent, update prompt)"
description: "The two huh prompts that run inside the root pre-run — telemetry consent and the out-of-date update confirm — have no utils.IsInteractive() gate, unlike every other interactive flow in the codebase. Under MCP stdio or piped stdin they rely on huh erroring out rather than being skipped, risking hangs and protocol corruption; the consent skip also honours only the --ci config flag, not the CI environment variable. Gate both prompts on interactivity, honour CI=true, and exempt the mcp command from prompting."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - bootstrap
  - cli
  - tui
  - mcp
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# TTY-guard the interactive prompts in the root pre-run (telemetry consent, update prompt)

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (GTB-core §HIGH
    pre-run prompt TTY-guard finding). Context: the resolved MR !157 init-hang incident —
    huh forms hung the CLI e2e suite on non-terminal stdin, and the fix established the
    `utils.IsInteractive()` guard as the codebase convention for every interactive flow.

---

## 1. Problem

Two interactive `huh` prompts run inside the root `PersistentPreRunE` with **no TTY
guard**:

- `promptTelemetryConsent` (`pkg/cmd/root/root.go:1019-1076`) — a confirm form shown
  when `TelemetryCmd` is enabled but `telemetry.enabled` is unset;
- the update confirm in `handleOutdatedVersion` (`root.go:546-599`, `form.Run()` at
  `root.go:571-579`) — shown when the tool is behind and the update policy is
  prompt/enabled.

Both rely on the documented assumption that "without a usable TTY … form.Run returns an
error" (`root.go:565-568`). That is exactly the assumption the **MR !157 incident**
disproved: huh forms *hung* the e2e suite on non-terminal stdin, and the resolution
established `utils.IsInteractive()` as the convention — now used at
`pkg/setup/init.go:170`, `pkg/setup/update.go:314` and `update.go:354`,
`pkg/cmd/config/edit.go:62`, and `internal/cmd/feature_toggle.go:72`. The two pre-run
prompts are the remaining unguarded sites, and they run before *every* command.

Concrete failure scenarios:

- **MCP stdio.** `tool mcp start` is spawned by an MCP client with stdin as the open
  JSON-RPC pipe. On a machine where telemetry is feature-enabled but unconfigured (gtb
  itself ships in exactly this state), the consent form runs against the pipe: it can
  block indefinitely or consume protocol bytes from stdin, and its rendering writes to
  the stdout that carries the JSON-RPC responses. The same applies to the update prompt
  for tools with `UpdatePolicyPrompt` once they fall behind.
- **Piped/scripted stdin.** Any `tool <cmd> < file` or cron invocation on an
  unconfigured-telemetry machine attempts the prompt on every run.
- **CI is only half-detected.** The consent skip checks the `ci` config/flag value
  (`view.GetBool("ci")`, `root.go:1041`) but **not** the `CI` environment variable that
  the forge initialisers already honour (`pkg/setup/forge/profile.go:192`,
  `os.Getenv("CI") == "true"`). A real CI run that does not pass `--ci` re-attempts the
  consent prompt on every invocation (and, because non-interactive `form.Run` errors are
  swallowed at debug level, does so silently and never persists a choice).

## 2. Proposed change

1. **Gate both prompts on `utils.IsInteractive()`.**
   - `promptTelemetryConsent`: return early (debug log, no persistence) when
     `!utils.IsInteractive()` — the unset state remains unset and the prompt reappears
     on the next interactive run, which is the intended one-time-opt-in behaviour.
   - `handleOutdatedVersion`: skip `form.Run()` entirely when non-interactive, leaving
     `runUpdate = false`. Policy semantics are unchanged: `prompt` continues with the
     existing warning; `enabled` still yields the hard "update required" error
     (`root.go:590-596`) — but now deterministically, without touching stdin.
2. **Honour `CI=true` alongside `--ci`.** Add an `os.Getenv("CI") == "true"` check to
   the consent skip (matching `pkg/setup/forge/profile.go:192`) and to
   `shouldSkipUpdateCheck`'s CI gate (`root.go:506-513`) so config-flag and environment
   detection agree. Factor a single small helper (e.g. `isCIEnvironment(view)`) used by
   both, rather than a third ad-hoc check.
3. **Never prompt under the `mcp` command.** Add the mcp command to the pre-run's
   prompt exemptions (aligned with the bootstrap fast-path work in the companion
   auxiliary-command-exemptions spec, via `setup.FeatureOf(cmd) == p.McpCmd` — not a
   name match). Even with an interactive terminal, an MCP server process must not write
   prompt UI to its protocol stdout.
4. **Guard placement note.** `utils.IsInteractive` checks stdin; the MCP case shows
   stdout matters too. Keep `IsInteractive` as the shared gate but confirm (and test)
   that it covers the piped-stdout case, or gate mcp explicitly per (3) — which this
   spec requires regardless.

## 3. Scope & release plan

- GTB-repo only: `pkg/cmd/root` (both prompt sites, CI helper, mcp exemption). No
  public `pkg/` API changes expected; ships as `fix(root)` (patch).
- Unit tests: table tests driving `promptTelemetryConsent` and `handleOutdatedVersion`
  with an injected interactivity check (the existing `WithForm` seam at `root.go:540-544`
  covers form substitution; add an equivalent injectable gate rather than a
  package-level variable, per the no-package-level-mocking-hooks rule).
- **E2E/BDD implications: yes.** The MR !157 incident was caught by the e2e suite; add
  Gherkin coverage for the surviving prompts: "Given telemetry is enabled but
  unconfigured, When I run `<tool> <cmd>` with piped stdin, Then the command completes
  without prompting and no consent is persisted", and an `mcp start`-under-pipe scenario
  asserting clean JSON-RPC output. Gate under the existing CLI/smoke tags; run with
  `-count=1`.
- `/gtb-verify` before the MR; update the root pre-run docs in `docs/components/`.

## 4. Acceptance criteria

- With stdin a pipe (not a TTY): any command on an unconfigured-telemetry tool completes
  without blocking, writes no prompt bytes to stdout/stderr, reads no bytes from stdin,
  and persists no `telemetry.enabled` value.
- With `CI=true` in the environment and no `--ci` flag: the consent prompt and the
  pre-run update check are both skipped (parity with `view.GetBool("ci")`).
- `tool mcp start` never renders a prompt, interactive terminal or not; an e2e scenario
  exchanges at least one JSON-RPC message over stdio on a telemetry-unconfigured,
  update-behind configuration without corruption.
- With `UpdatePolicyEnabled` and non-interactive stdin, the pre-run returns the existing
  "update required" error immediately (no `form.Run` attempt); with `UpdatePolicyPrompt`
  it warns and continues.
- Interactive behaviour is unchanged: on a real TTY both prompts appear exactly as
  today (existing form tests still pass).

## 5. Open questions

1. Should `CI=true` also suppress the persistent `warnIfBehindCached` reminder
   (currently gated only on `view.GetBool("ci")`, `root.go:455-457`), for full parity
   between the flag and the environment variable?
2. Should the non-interactive consent skip write nothing forever (current proposal), or
   persist an explicit `telemetry.enabled: false` after N silent skips to stop
   re-evaluating? (Proposal: never auto-persist — absence of consent is not refusal.)
3. Does `utils.IsInteractive` need extending to consider stdout (for prompt rendering)
   as well as stdin, codebase-wide, or is the mcp-feature exemption sufficient for the
   only stdout-sensitive case?
