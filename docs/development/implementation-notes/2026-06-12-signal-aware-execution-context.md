---
title: "Implementation notes, signal-aware execution context"
description: "What was implemented for the 2026-06-12 signal-aware execution context spec, deviations, and open questions for review."
date: 2026-06-13
tags:
  - implementation-notes
  - cmd
  - lifecycle
  - signals
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# Implementation notes: signal-aware execution context

Spec: [`0079-signal-aware-execution-context`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0079-signal-aware-execution-context) (now `IMPLEMENTED`).

## What was implemented

### `pkg/cmd/root/execute.go`

- `Execute` now runs the command tree via `rootCmd.ExecuteContext(ctx)` under a cancellable context watching `os.Interrupt` and `syscall.SIGTERM` (D1, O1, no build tags; the SIGTERM registration is inert on Windows).
- **First signal** cancels `cmd.Context()` and logs a warning ("press again to force quit"). **Second signal** force-exits immediately with `128+signum` (D2/O4, kubectl/docker UX).
- A signal-terminated run exits `128+signum` (130 SIGINT, 143 SIGTERM) **through the ErrorHandler's fatal path**, not a parallel `os.Exit` call site (D2/O2).
- The signal watcher is fully injectable for tests (`executeOptions{signals, forceExit}`), so no test raises real OS signals or kills the test process.
- Telemetry flush (D4) now runs on **every** path via a `sync.Once`-guarded flush: deferred for the non-exiting paths and invoked explicitly *before* `ErrorHandler.Check(..., LevelFatal)` fires `os.Exit`. The flush keeps its bounded background context, so cancellation cannot abort the flush itself.

### `pkg/errorhandling`: exit-code threading

- New `WithExitCode(err, code)` / `ExitCode(err)` pair (`exitcode.go`). The attachment is transparent to `errors.Is`/`errors.As`, survives wrapping, and the fatal path in `logError` now calls `h.Exit(ExitCode(err))` (default remains `1`; `0` for nil). This is additive: no existing public API changed.

### Generator (D3)

- `internal/generator/templates/skeleton_main.go`: the scaffolded `main.go` now carries a doc comment describing the signal contract. **No structural change was needed**: the skeleton already delegates to `gtbRoot.Execute`, which is where the signal context lives, so every scaffolded tool (and every existing tool that rebuilds against this version) gets the behaviour automatically.
- Regeneration verified: `just build && go run ./cmd/gtb generate project -n tmptool -r acme/tmptool -p tmp --overwrite allow --ci` produced a `main.go` with the documented `gtbRoot.Execute` delegation; `tmp/` deleted afterwards.

### TUI coexistence (D5): analysis, not code

Bubble Tea v2 (`charm.land/bubbletea/v2@v2.0.6`, `tea.go` `handleSignals`) confirms:

- With a TTY, the terminal is in **raw mode** while a prompt is active, so **Ctrl-C is a keystroke**, not a signal: huh aborts the current prompt (`ErrUserAborted`) and *no* SIGINT reaches the outer watcher. A stray Ctrl-C in a prompt aborts the prompt, not the run. This is exactly the resolved O3 semantic and required no code.
- An **external** `kill -INT`/`kill -TERM` is delivered to *both* Bubble Tea's notify channel and the outer watcher (Go fan-outs signals to all `signal.Notify` subscribers): the prompt aborts *and* the run cancels gracefully with exit 130/143. We judged this the correct semantic for supervisors (systemd, CI runners): an external terminate should end the run, prompt or no prompt.

### Update-temp cleanup (D4)

`pkg/setup/update.go` `extractAndInstallBinary` previously left the partial `"<target>_"` temp file behind whenever the copy failed: exactly what happens when SIGINT cancels the download mid-install. A deferred cleanup now removes it on every failure path; the successful rename path is untouched. Covered by `pkg/setup/update_extract_test.go`.

### Docs

- `docs/components/commands/root.md`: new "Signal Handling" section (lifecycle, exit codes, TUI note, `cmd.Context()` usage example).
- `docs/concepts/architecture.md`: new "Signal-aware Execution Lifecycle" workflow.
- `docs/components/error-handling.md`: new "Custom Exit Codes" section for `WithExitCode`/`ExitCode`.

### Tests

- `pkg/cmd/root/execute_signal_test.go`: blocking-command cancellation (SIGINT→130, SIGTERM→143), second-signal force-exit, telemetry flush on the cancellation path (spy backend), flush-before-fatal-exit on the ordinary error path, success/`ErrUpdateComplete` no-exit paths, and the `signalExitCode` mapping table. All injectable (no real signals) so they are `t.Parallel()`-safe and race-clean.
- `pkg/errorhandling/exitcode_test.go`: `WithExitCode`/`ExitCode` semantics and the fatal path honouring attached codes.
- `internal/generator/templates/skeleton_main_test.go`: scaffolded `main.go` delegates to `gtbRoot.Execute` and documents the contract.
- Coverage on the changed surface: `execute` 95.2%, every other new/changed function 100%.

## Deviations from the spec

1. **`signal.NotifyContext` is not used literally.** D1 names `signal.NotifyContext`, but it cannot distinguish *which* signal fired (needed for `128+signum`) nor observe a *second* signal (needed for force-exit). The implementation uses the equivalent primitive composition (`signal.Notify` + `context.WithCancel`) with identical semantics plus the D2 requirements. `signal.Stop` restores default disposition on return, equivalent to `defer stop()`.
2. **Skeleton template changed only in comments.** D3 anticipated a `main.go` template change; because the context derivation lives inside `Execute` (the better place, every existing tool inherits it on rebuild), the template only gained documentation. Regeneration verification was still run.
3. **Bonus fix surfaced by D4:** previously, a *normal* fatal error skipped the deferred telemetry flush entirely (`ErrorHandler.Check` → `os.Exit` skips defers). The flush-once-before-exit structure fixes that path too, with a regression test.

## Open questions for review

1. **E2E Gherkin SIGINT smoke scenario** (spec verification item 3). **Resolved: added.** `features/cli/signal.feature` drives the dedicated e2e binary (`cmd/e2e`): a hidden, contrived `block` fixture command (`cmd/e2e/block.go`) parks on `cmd.Context().Done()` and, on cancellation, prints `graceful shutdown complete` to stdout. The scenario starts the binary, waits for its readiness marker, sends a real `os.Interrupt`, then asserts the process **exits with code 130** *and* the **`graceful shutdown complete`** marker is on stdout: proving real OS signal delivery cancelled the context and the command unwound gracefully rather than being hard-killed. Step definitions live in `test/e2e/steps/signal_steps_test.go` (spawns the binary, signals it, waits for exit, asserts code + output). Tagged `@cli @smoke @signal`, so it runs under `INT_TEST_E2E_CLI=1` and the smoke set; confirmed green.
2. **TUI coexistence integration test.** **Stays analysis-backed (not changed).** D5 remains satisfied by analysis of Bubble Tea v2's documented raw-mode behaviour (and its own `handleSignals` comment). The new SIGINT *process-level* E2E (above) covers the external-signal path at the OS level; the PTY/TUI raw-mode `^C`-as-keystroke case is still covered by analysis rather than a `creack/pty` harness, as agreed.
3. **External SIGINT during a prompt cancels the whole run** (both Bubble Tea and the outer watcher are notified). I judged this correct for supervisors, but if you'd rather the prompt absorb externally-delivered SIGINT too, the outer watcher would need a "TUI active" gate (e.g. a flag set around `form.Run()` call sites): more invasive, touches every prompt call site.
4. **Exit message wording.** **Resolved: interrupt notice demoted to debug.** An interrupt is a deliberate user choice, not a failure, so the `interrupted by signal: …` notice is no longer logged at error. It now routes through the new `errorhandling.LevelFatalQuiet` level, which exits identically to `LevelFatal` (the 128+signum `WithExitCode` code is still honoured, the telemetry flush still runs) but logs the notice at **debug**: invisible to end users on a normal run, still surfaced under `--debug`. The change is additive to the `errorhandling` public API (`LevelFatalQuiet` constant; new `logError` case). Covered by `pkg/errorhandling/exitcode_test.go` (`TestCheck_FatalQuietLogsDebugButStillExits`) and `pkg/cmd/root/execute_signal_test.go` (`TestExecute_InterruptNoticeIsDebugNotError`).
