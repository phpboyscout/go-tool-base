---
title: "version command: degrade gracefully when the release source is unreachable"
description: "tool version fails hard — exit non-zero, nothing printed, after up to a 60-second wait — when the configured release source cannot be reached, even though the local version/commit/date are already in hand and the NewUpdater failure five lines earlier already degrades gracefully. Mirror that branch: print local info with a warning, shorten the passive check's timeout, and consider making the network check opt-in via --check."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - cli
  - version
  - update
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# version command: degrade gracefully when the release source is unreachable

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (GTB-core §HIGH
    version-offline finding; the §MEDIUM pre-run update-check 5-minute-budget finding is
    related but out of scope here — see §3)

---

## 1. Problem

`tool version` is the diagnostic command users run first on a broken machine — and it is
the command that fails hardest when the network is the thing that is broken.

Evidence (`pkg/cmd/version/version.go`):

- The command already holds everything it needs locally: `VersionInfo` is populated from
  ldflags-injected `props.Version` before any network activity (`version.go:46-51`).
- The `setup.NewUpdater` failure branch **does** degrade: it logs a warning and prints
  local info (`version.go:66-77`).
- But five lines later, a `GetLatestVersionString` failure is returned as a hard error
  (`version.go:79-82`): `return errors.Wrap(err, "unable to fetch latest version")` —
  exit non-zero, **nothing printed**.
- The surrounding timeout is `versionCheckTimeout = 60 * time.Second` (`version.go:21`),
  so the failure mode on a black-holing network is: wait up to 60 seconds, then exit
  non-zero without ever printing the version, commit, and date the process already has.

Failure scenarios: any offline or air-gapped machine, a firewalled/captive-portal
network, or a release-source outage (GitLab/GitHub down). In each, `tool version`
becomes useless precisely when it is most needed, and scripts that parse `tool version`
output break on network weather.

The inconsistency is the tell: the command treats "cannot construct an updater" as
best-effort but "updater cannot reach the release source" as fatal. Both are the same
condition from the user's point of view — the latest-version lookup is unavailable.

## 2. Proposed change

1. **Mirror the `NewUpdater`-failure branch** at `version.go:79-82`: on
   `GetLatestVersionString` error, log
   `props.Logger.Warn("failed to check latest version", "error", err)` and fall through
   to the same successful `out.Write` of local info. `Latest` stays empty and `Current`
   stays `true` (unknown ≠ behind); exit code 0. Structured output (`--output json`)
   carries the local fields exactly as the existing degraded branch does today.
2. **Shorten the passive check's budget.** Reduce `versionCheckTimeout` from 60s to a
   few seconds (proposed: 10s) — the check is a courtesy annotation on a local command,
   and with (1) the timeout now bounds a *wait*, not a failure. Keep the constant in one
   place so the flag in (3) can share it.
3. **Add an opt-in `--check` flag** for callers who *want* hard semantics: with
   `--check`, a lookup failure remains a non-zero error (current behaviour), and the
   command may use a longer timeout. This preserves a scriptable "is there an update and
   can you reach the source" probe while making the default path offline-safe.
4. **Surface degradation in text output**: when the check was attempted and failed,
   append a single stderr/log warning (not a change to the stdout format) so humans see
   why `Latest:` is absent; the stdout contract of `printVersionText`
   (`version.go:105-119`) is unchanged.

**Related but out of scope** (kept tight per the review's priority table): the root
pre-run's passive update check shares the download-sized `updateTimeout = 5 * time.Minute`
(`pkg/setup/update.go:54-58`) via `checkForUpdates` (`pkg/cmd/root/root.go:442-504`),
which can stall any command for minutes on a black-holing network. That deserves its own
small change (a dedicated short `context.WithTimeout` around the pre-run check, keeping
5 minutes for explicit `update` runs); it is noted here for cross-reference, and the
timeout constant introduced in (2) should be placed so that fix can reuse it.

## 3. Scope & release plan

- GTB-repo only: `pkg/cmd/version`. The new `--check` flag is additive; the degraded
  default is a behaviour change to a built-in command inherited by every scaffolded tool
  — ships as `fix(version)` (patch): the command now succeeds where it previously
  failed, no API change.
- No generator/template changes (the version command is a framework built-in, not
  scaffolded output), so the scaffold-verification step is not required; run it anyway
  if the release also carries generator work.
- **E2E/BDD implications: yes** — user-facing CLI behaviour changes. Add Gherkin
  scenarios (smoke-taggable, no external deps): "Given the release source is
  unreachable, When I run `<tool> version`, Then the exit code is 0 and the local
  version is printed"; "…When I run `<tool> version --check`, Then the exit code is
  non-zero". The e2e harness can point the tool at an unroutable/refused release host.
  Run with `-count=1`.
- Unit tests: table tests over reachable / unreachable / `--check` / update-disabled /
  development-build combinations, using the existing updater seams; text and JSON output
  assertions for the degraded path.
- `/gtb-verify` before the MR; update the version command page under `docs/` (behaviour
  and the new flag).

## 4. Acceptance criteria

- With the release source unreachable (connection refused or black-holed):
  `tool version` exits 0, prints `Version:`/`Build:`/`Date:` from local build info,
  omits `Latest:`, and emits exactly one warning about the failed check — completing
  within the new timeout bound.
- `tool version --output json` in the same condition exits 0 with a success-status
  response containing the local fields and no `latest` key.
- `tool version --check` in the same condition exits non-zero with the wrap
  "unable to fetch latest version"; with a reachable source it behaves exactly as the
  command does today (prints `Latest:` when behind).
- Reachable-source default behaviour is unchanged (latest annotation, "new version
  available" warning when behind).
- The disabled-update and development-build fast paths (`version.go:53-61`) are
  untouched and still skip the network entirely.
- Gherkin scenarios above pass under `INT_TEST_E2E=1`.

## 5. Open questions

1. Timeout value for the passive check: is 10s right, or should it match whatever
   short budget the related pre-run fix adopts (a shared constant in `pkg/setup`)?
2. Should the degraded JSON output carry an explicit marker (e.g. `"check_failed": true`)
   so scripts can distinguish "up to date" from "could not check", or is the absence of
   `latest` sufficient? (`Current: true` on the degraded path is arguably misleading.)
3. Should `--check` imply bypassing the development-build skip (`version.go:53`) so
   maintainers can probe the release source from a dev build, or keep the skip?
