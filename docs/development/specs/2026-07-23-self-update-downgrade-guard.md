---
title: "Self-update downgrade guard: refuse to install an older 'latest' release without --force"
description: "When the release source reports a 'latest' version older than the running binary, the self-updater proceeds to download and install it — signature and checksum verification authenticate the artefact, not its recency, so a stale or rolled-back release listing silently downgrades the tool. Add a guard: the implicit (no --version) update path refuses to move to a lower version unless --force is given."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - setup
  - self-update
  - security
  - rollback
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Self-update downgrade guard: refuse to install an older "latest" release without --force

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH finding),
    [forced update feature](2026-06-16-forced-update-feature.md) (the `--force` semantics this builds on),
    [update channels & rollback](2026-06-21-update-channels-rollback.md) (sanctioned-downgrade UX),
    [remote update checksum verification](2026-04-02-remote-update-checksum-verification.md) (what verification does and does not cover)

---

## 1. Problem

`pkg/setup/update.go` has no downgrade guard on the implicit update path:

- `IsLatestVersion` (update.go:606-613) switches on
  `ver.CompareVersions(s.CurrentVersion, latestVersion)`. When the current version is
  **newer** than the reported latest (`case 1`, update.go:609-610), it returns
  `false` with the message "…your tardis travelled too far into the future… please
  downgrade to %s".
- `shouldSkipUpdate` (update.go:939-952) skips only when `isLatestVersion && !s.force`
  (update.go:945). A `false` result — including the current-is-newer case — lets
  `Update` (update.go:617-629) continue: it downloads the "latest" release and
  **installs the older binary**, with no `--force` and no confirmation.

**Threat model: rollback attack.** Signature and checksum verification (the Phase 2
signing enforcement) authenticate that an artefact is a genuine release — they say
nothing about its **recency**. An attacker who can influence the release listing — a
compromised forge account, a deleted/yanked newest release, or a rogue re-tag (a class
of incident this project has already experienced with an out-of-band `go/forge`
release) — can serve an older, *validly signed* release as "latest". Every
verification step passes and the tool downgrades itself to a known-vulnerable
version. The same mechanism also fires benignly: a maintainer deleting the newest
release for operational reasons silently rolls back every user who runs `update`.

## 2. Proposed change

Make version regression an explicit, forced action on the implicit path:

1. **Guard in `shouldSkipUpdate`** (or a sibling check called from `Update`): when no
   explicit version was requested (`s.version == ""`) and
   `CompareVersions(CurrentVersion, latestVersion) == 1`, refuse the update with a
   clear error unless `s.force` is set:

   > refusing to downgrade from vX.Y.Z to vA.B.C: the release source reports an older
   > "latest" release. If this is intentional, re-run with `--force`, or pin the
   > target with `--version`.

   The error should carry an `errors.WithHint` so the error handler renders the
   remediation. Refusal must be a **non-zero exit**, not a warning-and-skip — a
   silently skipped update under attack conditions should be visible to the operator.
2. **Keep the sanctioned downgrade paths.** Explicit `--version` + `--force` remains
   the supported downgrade route (per the forced-update spec). Decide whether
   `--version` alone targeting a lower version should also require `--force`
   (see §5 Q1); the guard above concerns only the implicit "install latest" path.
3. **Leave the background check untouched.** The throttled pre-run check in
   `pkg/cmd/root` only *notifies*; its "tardis" message (update.go:610) can stay, but
   reword it to state that a downgrade will not be applied automatically.
4. **Development builds** (`v0.0.0`, update.go:1055-1057) already require `--force`
   for any update; the new guard must not change that behaviour.

**Alternative considered:** an interactive confirmation prompt instead of a hard
refusal. Rejected as the primary mechanism — `update` runs headlessly (CI, scripts,
the background updater), and a prompt default of "yes" would neuter the guard. A
prompt may be layered on for interactive terminals later.

## 3. Scope & release plan

- **go-tool-base only**: `pkg/setup/update.go` (+ tests). No `go/forge*` change — the
  providers correctly report what the forge says; recency policy belongs in the
  consumer.
- Ships as `fix(setup): …` (patch) via the releaser-pleaser Release MR. Downstream
  tools built on GTB pick it up via their next go.mod bump; from that point their own
  self-update inherits the guard.
- Behavioural change note: users who relied on implicit downgrades (if any) must now
  pass `--force`. Document in `docs/components/` (update subsystem page) and the
  changelog body.

## 4. Acceptance criteria

- Unit test: current `v1.2.0`, latest reported `v1.1.0`, no `--force`, no `--version`
  → `Update` returns an error (not a silent skip), no download is attempted (assert
  via mock release client), exit is non-zero at the command layer.
- Unit test: same scenario with `--force` → update proceeds and installs (existing
  install pipeline assertions).
- Unit test: explicit `--version v1.1.0` behaves per the §5 Q1 decision, with a test
  pinning whichever behaviour is chosen.
- Regression tests: upgrade path (`-1`) and already-latest path (`0`) behave exactly
  as before, including the `--force` reinstall of the current version.
- Development-version (`v0.0.0`) behaviour unchanged: test that it still requires
  `--force` regardless of comparison result.
- The refusal message names both versions and the `--force`/`--version` remediation;
  a test asserts the hint is attached.
- E2E: extend the update Gherkin scenarios in `features/` with a "release source
  reports an older latest" scenario (fake forge fixture), asserting refusal.
- `just ci` green; touched code keeps ≥ 90% coverage.

## 5. Open questions

1. Should an explicit `--version` that is lower than `CurrentVersion` also require
   `--force`, or is naming an exact version already sufficient intent? (The review's
   direction keeps `--version` + `--force` as the sanctioned pair; requiring both is
   the safer default and matches the channels/rollback spec's framing.)
2. Should the guard compare against the *running* binary version only, or also record
   a high-water mark (last successfully installed version) to catch downgrades across
   reinstalls? Proposed: running version only for now; a persisted high-water mark is
   a larger TUF-style feature and out of scope.
