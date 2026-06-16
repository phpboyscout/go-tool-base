---
title: "Opt-in ForcedUpdate — three-state self-update gating with a configurable interval"
description: "Replace the always-on self-update check (which hijacks unrelated commands and masks failures with exit 0) with an opt-in ForcedUpdate capability that has three states (enabled / prompt / disabled), a configurable check interval, a locally cached latest-version with a persistent out-of-date WARN, and a non-zero exit on a genuinely failed forced update. --ci continues to bypass the check entirely."
status: IMPLEMENTED
date: 2026-06-16
tags:
  - specification
  - update
  - self-update
  - feature-flags
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# Opt-in ForcedUpdate — three-state self-update gating with a configurable interval

Authors
:   Matt Cockayne

Date
:   16 June 2026

Status
:   IMPLEMENTED

---

## Summary

Today every non-`--ci` invocation of a GTB-built tool runs a self-update check
in the root command's `PersistentPreRunE`. When a newer release is found the
tool attempts to update and, on a successful update, **aborts the requested
command** so the user must re-run it. Worse, when the update *fails* (e.g. the
release has no asset for the platform — see the companion keryx report #3) the
requested command is still aborted **and the process exits 0**, masking the
failure: nothing was generated, but CI and scripts see success.

This spec replaces that always-on behaviour with an **opt-in `ForcedUpdate`
capability** with three explicit states:

- **`enabled`** — when an update is available (per the interval check), block
  every command until the tool is updated. This is today's behaviour, made
  explicit and opt-in.
- **`prompt`** — when an update is available, prompt the user; they may decline
  and continue with their command.
- **`disabled`** — never auto-update or prompt; just log that an update is
  available.

In every state the tool **caches the latest version discovered by a check** and
emits a `WARN` on **every** subsequent invocation while the running version is
behind, so a user who declined (or who disabled the feature) is continuously
reminded they are out of date. `--ci` (and `ci: true` in config) continues to
bypass the check entirely.

## Motivation

From the keryx downstream (the framework's first real consumer):

> Running `gtb generate command …` (no `--ci`) triggered a self-update check;
> it found v0.17.0, tried to update, failed (`unable to find asset for Linux
> x86_64`), and **aborted the generate without doing any work — while exiting
> 0.**

Three distinct problems:

1. **The update check hijacks unrelated commands.** A `generate` invocation
   should not be turned into an update-or-nothing operation as a side effect.
2. **A failed forced update exits 0.** The masked failure is the most dangerous
   part: scripts and CI cannot tell that the real work never ran.
3. **The behaviour is not configurable.** The forced-update behaviour was
   intentional for an earlier incarnation of the framework but is no longer
   universally desirable; consumers should choose their posture, and the
   24-hour check interval is hard-coded.

## Current behaviour (as of v0.17.x)

| Concern | Location | Today |
|---|---|---|
| Update check wired into every command | `pkg/cmd/root/root.go` `newRootPreRunE` → `checkForUpdates` | Runs at the end of bootstrap unless skipped |
| Skip conditions | `pkg/cmd/root/root.go` `shouldSkipUpdateCheck` | `UpdateCmd` disabled, dev build, `--ci`/`ci:true`, throttle |
| Outdated handling | `pkg/cmd/root/root.go` `handleOutdatedVersion` | Confirm-form → `update.Update()` → set `ShouldExit` |
| Masked exit 0 | `pkg/cmd/root/execute.go` (`ErrUpdateComplete`) | Returns without `ErrorHandler.Check` → exit 0 |
| Hard-coded interval | `pkg/setup/update.go` `defaultCheckInterval = 24h` | Not configurable |
| Throttle | `pkg/setup/update.go` `SkipUpdateCheck`, `GetTimeSinceLast` | "≥ 24h since last check" |
| Platform asset lookup | `pkg/setup/update.go` `findReleaseAsset` | Fatal `unable to find asset for %s %s` |
| Feature flags | `pkg/props/tool.go` (`FeatureCmd`, `SetFeatures`) | `UpdateCmd` is enabled by default |

## Design

### The ForcedUpdate state

A new tri-state type captures the posture:

```go
// in pkg/props (or pkg/setup) — exact home is an open question (OQ1)
type UpdatePolicy string

const (
    UpdatePolicyEnabled  UpdatePolicy = "enabled"  // block until updated
    UpdatePolicyPrompt   UpdatePolicy = "prompt"   // ask; may decline
    UpdatePolicyDisabled UpdatePolicy = "disabled" // log only
)
```

Resolution precedence (highest first):

1. `--ci` flag / `ci: true` config → **bypass** the whole check (unchanged).
2. `update.policy` config key (`enabled` | `prompt` | `disabled`).
3. The framework default baked into the tool via `props.Tool` / `SetFeatures`.

**Defaults (decided):**

- **Framework default: `disabled`.** A tool built on GTB does nothing
  surprising out of the box — it logs that an update is available and carries
  on. Forced/prompted behaviour is strictly opt-in.
- **The GTB CLI itself uses `prompt`** (wired in `internal/cmd/root`), so `gtb`
  nudges its own users to upgrade without blocking them.

`UpdateCmd` (the existing feature flag) continues to gate whether the `update`
subcommand and the check exist at all; `update.policy` governs *how* the check
behaves when it runs.

### Behaviour by state

For all states the check still respects the interval throttle and `--ci` bypass.
When a check runs and finds the running version is behind the latest release:

| State | Action |
|---|---|
| `enabled` | Warn, then (interactive) offer/perform the update; **block the requested command** until updated. If the update fails, **exit non-zero** (see below). Non-interactive without `--ci`: log and **exit non-zero** rather than silently proceeding (OQ3). |
| `prompt` | Prompt "update now? [y/N]". **Yes** → update, then exit asking the user to re-run (or continue — OQ4). **No / non-interactive** → cache the latest version, `WARN`, and **continue with the requested command**. |
| `disabled` | Do not update or prompt. Cache the latest version and `WARN`; **continue with the requested command**. |

### Fix the masked exit-0

`pkg/cmd/root/execute.go` currently swallows `ErrUpdateComplete` and returns
(exit 0). Two changes:

1. **A *successful* forced update that requires a restart** still exits 0 — that
   is correct (the tool updated; re-run). Keep the "please run the command
   again" message but make it unmistakable.
2. **A *failed* forced update** (the update was attempted and errored — e.g. no
   asset for the platform, download/verify failure) must propagate a non-zero
   exit via `ErrorHandler.Check`, never the silent `ErrUpdateComplete` path. The
   distinction is "did we successfully update?" not "did we decide to update?".

### Configurable check interval

Replace the hard-coded `defaultCheckInterval = 24 * time.Hour` as the *sole*
source of the throttle with a two-tier model that mirrors the policy: a tool
author baseline (`props.Tool.UpdateCheckInterval`, a `time.Duration`) plus a
runtime config override.

```yaml
update:
  policy: prompt          # enabled | prompt | disabled  (empty = tool baseline)
  check_interval: ""      # any Go duration; empty = tool baseline; 0 = every run
```

`setup.ResolveCheckInterval(toolDefault, configValue)` resolves the throttle: a
valid `update.check_interval` (where `0`/`0s` means "every run") wins; else the
positive `props.Tool.UpdateCheckInterval` baseline; else the framework default
`setup.DefaultCheckInterval` (24h). A zero-value baseline is treated as "unset"
and falls through to 24h — the every-run cadence is reachable only via config,
never as a compiled-in baseline. `SkipUpdateCheck` takes the resolved duration.

**Implemented:** the shipped `setup` default config leaves `check_interval`
empty so the baseline is honoured. `gtb generate project` exposes the baseline
via the `--update-check-interval` flag and the **Update Check Interval** wizard
step (validated by `generator.ValidateUpdateCheckInterval`), persists it to the
manifest (`update_check_interval`), and renders it into the generated
`props.Tool` literal as a `time.Duration` expression (e.g. `168 * time.Hour`).

### Cached latest version + persistent out-of-date WARN

Today the "you are behind" signal is only emitted in the same run that performs
the check, and is lost afterwards. Add a small persisted record next to the
existing last-checked timestamp (`~/.<tool>/`):

```
last_checked         # existing: timestamp of last check
latest_version       # new: the latest version string the last check discovered
```

On **every** invocation (subject to `--ci` bypass and `UpdateCmd` enabled),
*before* deciding whether to run a fresh check, the bootstrap reads
`latest_version`; if it is newer than the running version, it emits a single
`WARN` (e.g. `a newer <tool> is available: vX.Y.Z (run '<tool> update')`). This
makes "you're on an old version" sticky across runs for `prompt`/`disabled`
users without hitting the network every time.

### Graceful missing-asset handling

`findReleaseAsset` returning `unable to find asset for <os> <arch>` is currently
fatal inside an update attempt. Under the new model:

- In `prompt`/`disabled`, a check that *discovers* a newer version but cannot
  resolve an asset still caches the version and `WARN`s ("a newer version is
  available but no <os>/<arch> asset was published"), and **continues**.
- In `enabled`, a forced update that cannot resolve an asset is a **failed
  update → non-zero exit** with a clear message (not a masked exit 0).

This also mitigates the keryx #3 transient (a tag pipeline that didn't publish
the platform asset) from bricking every command.

## Implementation

- **`pkg/props/tool.go`** (or `pkg/setup`): add `UpdatePolicy` type + constants;
  add a `Tool`-level default (OQ1) so a tool can set its baseline
  (`gtb` → `prompt`, framework default → `disabled`).
- **`pkg/setup/update.go`**: read `update.check_interval` (default 24h) in
  `GetTimeSinceLast`/`SkipUpdateCheck`; persist/read `latest_version`; split the
  "discover latest" step from the "perform update" step so a discovery can cache
  + WARN without forcing an update; make asset-miss non-fatal for discovery.
- **`pkg/cmd/root/root.go`**: resolve `UpdatePolicy`; branch
  `checkForUpdates`/`handleOutdatedVersion` on it; emit the persistent
  out-of-date WARN early in bootstrap from the cached `latest_version`.
- **`pkg/cmd/root/execute.go`**: distinguish *update succeeded → restart* (exit
  0) from *update failed* (non-zero); never mask a failed update.
- **`internal/cmd/root/root.go`**: set the GTB CLI's own policy to `prompt`.
- **Config defaults / schema** (`pkg/config`, embedded defaults): document
  `update.policy` and `update.check_interval`.
- **Docs**: `docs/components/` update subsystem page + a migration note in
  `docs/migration/` (the default behaviour changes from "forced" to "disabled").

## Testing

- Unit: policy resolution precedence (`--ci` > config > tool default); interval
  parsing/validation; the cached-`latest_version` WARN fires when behind and not
  when current; asset-miss is non-fatal for discovery but fatal (non-zero) for an
  `enabled` forced update.
- Unit: `execute.go` exits **non-zero** on a failed update and **0** on a
  successful update-and-restart.
- E2E (Godog, gated): a non-`--ci` run under `disabled` runs the requested
  command and logs the WARN; under `prompt` a declined prompt continues; `--ci`
  bypasses entirely. (CLI behaviour change → Gherkin per the BDD strategy.)

## Migration

Default behaviour changes: a tool built on GTB no longer force-updates by
default (new default `disabled`). Downstreams that *want* the old behaviour set
`update.policy: enabled`. A `docs/migration/` note will spell this out. (Pre-1.0,
this ships as a `feat` minor.)

## Open questions

1. **Home for `UpdatePolicy`** — `pkg/props` (alongside feature flags, since the
   tool author sets the default) vs `pkg/setup` (alongside the updater). Leaning
   `pkg/props` for the default + `pkg/setup` consuming it. **Resolve before
   implementation.**
2. **Is `update.policy` per-tool config or also a `SetFeatures`-style option?**
   i.e. should the tool author set the baseline via a functional option
   (`props.WithUpdatePolicy(prompt)`) AND allow `update.policy` config override,
   or config-only? (Spec assumes: option for the baseline default + config
   override.)
3. **`enabled` + non-interactive (no `--ci`)** — block with a non-zero exit and a
   "run `<tool> update`" message (spec's assumption), or fall back to `prompt`'s
   continue? The masked-exit-0 fix argues for non-zero.
4. **`prompt` accept → restart vs continue** — after a user accepts and the
   update succeeds, exit-and-ask-to-re-run (current restart model, exit 0) vs
   attempt to continue the original command in-process (harder, the binary just
   changed). Spec assumes restart model.
5. **Interval `0` semantics** — does `update.check_interval: 0` mean "check every
   invocation" or "never check"? Proposal: `0` = check every invocation;
   `disabled` policy is how you turn checking off.
6. **WARN cadence** — every invocation while behind (spec's assumption) vs
   throttled (e.g. once per interval) to avoid noise in tight loops.

## Resolved decisions (as built)

The open questions above were resolved as follows during implementation:

1. **`UpdatePolicy` lives in `pkg/props`** (`update_policy.go`); `pkg/cmd/root`
   and `pkg/setup` consume it.
2. **Baseline via the `Tool.UpdatePolicy` struct field** (a single value needs
   no functional option), with `update.policy` config overriding at runtime via
   `props.ResolveUpdatePolicy`.
3. **`enabled` + non-interactive (or declined) → non-zero error** (blocked),
   never a masked continue.
4. **`prompt` accept → restart model** (update, exit 0, ask to re-run) — reuses
   the existing `ErrUpdateComplete` path.
5. **`update.check_interval: 0` = check every invocation**; `disabled` is how you
   turn checking off.
6. **Out-of-date WARN every invocation** while behind the cached version
   (suppressed under `--ci`).

Two scope refinements emerged:

- **The latest version is cached in the existing `last_checked` marker's body**
  (its modtime already drives the throttle — one file, two jobs) rather than a
  new file, per reviewer feedback.
- **Graceful missing-asset needs no `findReleaseAsset` change**: version
  *discovery* (`GetLatestVersionString`) is asset-independent, so `disabled` /
  `prompt`-decline never touch assets; an actual update that cannot resolve a
  platform asset correctly fails non-zero (the masked-exit-0 fix).

### Testing & BDD suitability

The three-state branching, interval resolution, version-cache round-trip and
policy resolution are covered by unit tests (`pkg/props`, `pkg/setup`,
`pkg/cmd/root`). **No Gherkin e2e scenario is added**: a meaningful end-to-end
test of the background check requires a *newer published release than the test
binary* and live network access, which a Godog scenario cannot trigger
deterministically without a mock release server — a poor BDD fit per the
strategy's suitability assessment. The behaviour is instead pinned by the
deterministic unit tests at the handler boundary.

---

*Remediation spec for keryx bug-report #2 (and mitigating #3). Implemented.*
