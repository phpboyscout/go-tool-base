---
title: "Update Channels (stable/beta/canary) + Rollback / Pin — opt-in release-channel selection with safe downgrades"
description: "Let a tool opt into release channels (stable/beta/canary, mapped to semver prerelease tags) and let users pin to an exact version or roll back to a prior release — all while preserving the existing checksum and signature verification guarantees of the self-updater. Builds on the injectable release source and the opt-in ForcedUpdate work without re-architecting either."
status: DRAFT
date: 2026-06-21
tags:
  - specification
  - update
  - self-update
  - release
  - channels
  - rollback
  - version-pinning
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Update Channels + Rollback / Pin Specification

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   21 June 2026

Status
:   DRAFT

---

## Summary

GTB's self-updater (`pkg/setup.SelfUpdater`) discovers the *single* latest
release via `release.Provider.GetLatestRelease` and, when a `--version` is
given, an exact tag via `GetReleaseByTag`. There is no notion of a **release
channel**: a tool author cannot publish a beta line alongside stable, and a user
cannot opt their installation into "give me betas" or "only stable". Equally,
while a user *can* target an exact tag with `update --version vX.Y.Z`, there is
no first-class, durable concept of a **pin** (stay on this version, do not drift)
or a **rollback** (deliberately move *backwards* to a known-good prior release) —
and the updater's `IsLatestVersion` logic actively treats a lower target as "your
tardis travelled too far into the future", i.e. it is built around the assumption
that updates only ever go forward.

This spec adds, item **A6** on the roadmap:

1. **Update channels** — `stable` | `beta` | `canary`, a tool-author opt-in,
   resolved from the **semver prerelease label** of each release tag (e.g.
   `v1.2.0` = stable, `v1.2.0-beta.1` = beta, `v1.2.0-canary.3` = canary). No new
   release infrastructure is required: channels are a *view* over the existing
   tag stream, computed locally.
2. **Pin** — durably fix the installation to an exact version so neither the
   background check nor an unqualified `update` moves it.
3. **Rollback** — deliberately and safely move *backwards* to a prior release,
   re-running the full checksum + signature verification pipeline against the
   target, so a downgrade is exactly as trustworthy as an upgrade.

It is **purely additive** and backwards compatible: a tool that sets no channel
behaves precisely as today (stable-only, forward-only).

## Goals & Non-Goals

### Goals

- A tool author can **opt into** publishing/consuming channels via a
  `Tool.UpdateChannel` baseline, overridable at runtime by an `update.channel`
  config key (mirroring the existing `update.policy` / `update.check_interval`
  two-tier resolution from the ForcedUpdate spec).
- Channel selection is computed **locally** from the semver prerelease label of
  release tags — no dependency on a provider "prerelease" boolean (the
  `release.Release` interface does not expose one; see Design Decision 1).
- A user can **pin** (`update --pin [version]`) and **unpin** (`update --unpin`),
  with the pin persisted next to the existing update markers in the tool config
  dir and honoured by the background check.
- A user can **roll back** (`update --rollback [version]`) to a prior release
  with the **same checksum + signature verification** the forward path enforces,
  and with a clear, explicit confirmation that this is a downgrade.
- The background update check (`pkg/cmd/root`) respects the channel and the pin:
  a pinned tool never nags about a newer release on its channel.
- **Backwards compatible**: no channel + no pin = today's stable, forward-only
  behaviour, byte-for-byte.

### Non-Goals

- **No new release artefacts or publishing pipeline.** Channels are derived from
  existing tags; GoReleaser/releaser-pleaser output is unchanged. (How a tool
  author *chooses* to tag betas is their concern; we document the convention.)
- **No staged/percentage rollout, A/B cohorts, or remote channel assignment.**
  A channel is a local selection, not a server-driven feature-flag system. (Noted
  under Future Considerations.)
- **No relaxation of verification for downgrades.** Rollback is not a bypass —
  it is a *direction*. `require_checksum` / `require_signature` apply identically.
- **No automatic rollback-on-failure** of a forward update (the in-place replace
  already restores on failure; an *automatic* "revert to previous binary" cache
  is a separate, larger feature — Future Considerations).
- **Not a config-profile / environment-overlay mechanism.** See the explicit
  non-conflict note below.

## Relationship to the Rejected `--profile` Feature (non-conflict)

The decision log
([`docs/development/feature-decisions.md`](../feature-decisions.md) §
"Environment Profiles (dev/staging/prod)") **rejected** a `--profile` flag that
would select environment-specific *config overlays* (`config.dev.yaml`), on the
grounds that GTB should not opinionate on how users compose configuration — the
existing multi-`--config` mechanism already covers it.

**Update channels are categorically distinct and do not reopen that decision:**

| Rejected `--profile` | This spec's channels |
|---|---|
| Selects **which config file(s)** to merge | Selects **which release tag** to install |
| A *configuration-composition* concern | A *release-selection* concern |
| Overlaps existing `--config a --config b` | No existing mechanism (the updater only ever sees one "latest") |
| Imposes an opinion on deployment-config layout | Imposes nothing on config; reads one scalar key |

A channel never alters the merged configuration, never selects a config source,
and never changes runtime behaviour beyond *which binary version the self-updater
considers a candidate*. It is the release analogue of "track" in `apt`/`brew`
(`stable`/`edge`), not an environment overlay. The single scalar `update.channel`
key flows through the **same** existing config precedence chain as every other
key — it does not introduce a parallel overlay system. **Confirmed: no conflict
with the `--profile` rejection.** (This spec adds a corresponding entry to the
decision log's "Accepted, with scope" section on approval, to record the
distinction permanently.)

## Background — anchor code

| Concern | Location | Today |
|---|---|---|
| Updater + options | `pkg/setup/update.go` `SelfUpdater`, `UpdaterOption`, `WithReleaseProvider` | Resolves one provider; `version` field pins an exact tag for a single run |
| Latest/by-tag discovery | `pkg/setup/update.go` `GetLatestRelease`, `GetLatestVersionString` | `GetLatestRelease(owner,repo)` or `GetReleaseByTag(owner,repo,tag)` |
| Forward-only assumption | `pkg/setup/update.go` `IsLatestVersion` | A lower latest → "tardis travelled too far into the future" |
| Release abstraction | `pkg/vcs/release/provider.go` `Provider`, `Release`, `ReleaseAsset`, `ChecksumProvider`, `SignatureProvider` | `Release` exposes `GetTagName`/`GetDraft`; **no prerelease flag** |
| List discovery | `Provider.ListReleases(ctx,owner,repo,limit)` | Used today only for changelog notes (`filterReleaseNotes`) |
| Source config | `pkg/vcs/release/source_config.go` `ReleaseSourceConfig`, `props.ReleaseSource` | `Type/Host/Owner/Repo/Private/Params` |
| Update command | `pkg/cmd/update/update.go` `NewCmdUpdate` | Flags `--force`, `--version`, `--from-file` (`version`⊕`from-file`) |
| Semver | `pkg/version/version.go` `CompareVersions`, `FormatVersionString` (wraps `golang.org/x/mod/semver`) | Compare + prefix normalise |
| Policy/interval resolution | `pkg/props/update_policy.go` `ResolveUpdatePolicy`; `pkg/setup` `ResolveCheckInterval` | Two-tier: config key over `Tool.*` baseline over framework default |
| Background check | `pkg/cmd/root/root.go` `checkForUpdates`, `shouldSkipUpdateCheck`, `warnIfBehindCached` | Reads `update.policy`/`update.check_interval`; caches latest in `last_checked` marker body |

Two structural facts shape the design:

- **`release.Release` has no prerelease boolean.** It exposes `GetTagName()` and
  `GetDraft()` only. Channel discrimination therefore must come from the
  **semver prerelease label** of the tag, via `golang.org/x/mod/semver.Prerelease`
  — which is correct, provider-agnostic, and already the dependency `pkg/version`
  wraps. (GitHub *does* surface a prerelease flag on its API type, but GitLab /
  Gitea / Direct do not uniformly, and threading a new method through the
  `Provider` interface + every backend + the `releasetest` double is a far larger
  change for no extra signal — the tag label is authoritative.)
- **The updater is built forward-only.** `GetLatestRelease` returns the single
  newest release; `IsLatestVersion` treats "latest < current" as an anomaly.
  Channels require enumerating candidates (`ListReleases`) and selecting the
  newest *on the chosen channel*; rollback requires explicitly permitting
  "target < current".

## Design

### Channel model and semver mapping

A channel is one of three values, defined in `pkg/props` alongside
`UpdatePolicy` (it is a tool-author baseline, set the same way):

```go
// pkg/props/update_channel.go
type UpdateChannel string

const (
    ChannelStable UpdateChannel = "stable" // releases with NO prerelease label
    ChannelBeta   UpdateChannel = "beta"   // -beta.* (and, by inclusion, stable)
    ChannelCanary UpdateChannel = "canary" // -canary.* (and beta, and stable)
)
```

Mapping a tag → channel uses `semver.Prerelease(tag)` (returns e.g. `-beta.1`,
or `""` for a final release):

| Tag | `semver.Prerelease` | Native channel |
|---|---|---|
| `v1.2.0` | `""` | stable |
| `v1.2.0-beta.1` | `-beta.1` | beta |
| `v1.2.0-canary.3` | `-canary.3` | canary |
| `v1.2.0-rc.1` | `-rc.1` | *(see OQ2 — treated as beta by default)* |

**Channel inclusion is a superset ladder** (Decision below): selecting `canary`
considers canary **and** beta **and** stable candidates; `beta` considers beta +
stable; `stable` considers only stable. This matches user intuition ("I want the
bleeding edge" implies "…but a newer stable is still an upgrade") and means a
channel never strands a user on an *older* prerelease when a newer stable exists.
Selection is: of all releases whose channel ≤ the selected channel in the ladder,
pick the highest by `semver.Compare` (which already orders `1.2.0-beta.1 <
1.2.0`).

A new helper centralises this, mirroring `ResolveUpdatePolicy`:

```go
// pkg/props/update_channel.go
func (c UpdateChannel) Normalize() UpdateChannel // "" → ChannelStable, lower/trim
func (c UpdateChannel) Valid() bool
// Includes reports whether a release of channel `relCh` is a candidate for a
// user on channel c (the superset ladder).
func (c UpdateChannel) Includes(relCh UpdateChannel) bool
// ChannelOf maps a (normalised) version tag to its native channel.
func ChannelOf(tag string) UpdateChannel
// ResolveUpdateChannel: config value over Tool baseline over ChannelStable.
func ResolveUpdateChannel(toolDefault UpdateChannel, configValue string) UpdateChannel
```

### Selecting the latest release *on a channel*

The updater gains a channel-aware discovery path. Rather than add a method to the
`Provider` interface, channel filtering is done **client-side** in `pkg/setup`
over `Provider.ListReleases` results — keeping every existing backend and the
`releasetest` double untouched:

```go
// pkg/setup/update.go (new, on *SelfUpdater)
// GetLatestReleaseForChannel returns the highest-precedence release whose
// native channel is included by ch (per UpdateChannel.Includes), skipping
// drafts. Falls back to GetLatestRelease when ch == ChannelStable AND no
// non-stable tags exist, so the common path is unchanged.
func (s *SelfUpdater) GetLatestReleaseForChannel(ctx, ch props.UpdateChannel) (release.Release, error)
```

Implementation: `ListReleases(ctx, owner, repo, releasesPerPage)` (the constant
already exists, =100), drop `GetDraft()==true`, map each `GetTagName()` via
`props.ChannelOf`, keep those `ch.Includes(...)`, and return the
`semver.Compare`-max. `releasesPerPage` is the existing single-page bound; paging
beyond 100 releases to find an older channel candidate is a documented limitation
(OQ4).

`SelfUpdater` gets a `channel props.UpdateChannel` field, resolved in
`NewUpdater` from `props.Config.GetString("update.channel")` over
`props.Tool.UpdateChannel` (via `ResolveUpdateChannel`), exactly as `policy` is
resolved elsewhere. `GetLatestRelease` / `GetLatestVersionString` /
`IsLatestVersion` consult the channel-aware path when `s.channel != ChannelStable
|| anyPrereleaseTagsExist`; otherwise they keep calling `GetLatestRelease`
verbatim (zero behaviour change for the default tool). A new
`UpdaterOption` `WithChannel(props.UpdateChannel)` lets direct/test callers set it
parallel-safely (mirroring `WithReleaseProvider`).

### Pinning

A **pin** durably fixes the installation to an exact version. It is persisted in
the tool config dir next to the existing `last_checked` / `last_updated` markers
(`pkg/setup`'s `GetDefaultConfigDir`), in a new `pinned_version` marker file
(0600, owner-only — same `markerFilePerm` as the others):

```go
// pkg/setup (new, alongside SetCheckedVersion / GetCheckedVersion)
func SetPinnedVersion(fs afero.Fs, name, version string) error // ""/"" clears
func GetPinnedVersion(fs afero.Fs, name string) string         // "" when unpinned
```

Semantics:

- `update --pin` (no value) → pin to the **current running version**.
- `update --pin vX.Y.Z` → pin to that version (validated against `semVerPattern`;
  the update proceeds to install it if not already current — pin-and-move).
- `update --unpin` → clear the pin.
- While pinned, the **background check** (`checkForUpdates`) emits at most a
  single INFO ("pinned to vX.Y.Z; run `<tool> update --unpin` to resume
  updates") and **does not** nag about newer releases or auto-update under any
  policy. An explicit `update` (no `--version`) while pinned is a no-op with a
  clear message unless `--unpin`/`--version`/`--rollback`/`--force` overrides it
  (OQ3 covers `--force` precedence).
- A pin to a version **on the user's channel ladder** is the norm; pinning to a
  tag outside the channel is allowed (an explicit user act overrides the channel)
  and warns once.

This makes the "stay put" intent durable and machine-checkable, which the current
single-run `--version vX.Y.Z` does not.

### Rollback

A **rollback** deliberately installs a release **older** than the running binary.
It reuses the entire existing download → checksum-verify → signature-verify →
extract → replace pipeline (`SelfUpdater.Update` via `GetReleaseByTag`); the only
differences are *direction permission* and *user intent confirmation*:

- `update --rollback` (no value) → roll back to the **immediately preceding
  release on the current channel** (the second-highest channel candidate, or the
  preceding stable). Useful for "the release I just took is bad, get me off it".
- `update --rollback vX.Y.Z` → roll back to an explicit prior tag.
- The target **must** resolve via `GetReleaseByTag` and pass the **same**
  `verifyAssetChecksum` + signature verification as a forward update — a
  downgrade is not a trust bypass. `require_checksum` / `require_signature` are
  honoured identically.
- Because `IsLatestVersion` / `shouldSkipUpdate` treat "target < current" as an
  anomaly, rollback sets an explicit `allowDowngrade` flag on the `SelfUpdater`
  (set by a `WithAllowDowngrade()` option, only ever set on the rollback path) so
  `shouldSkipUpdate` permits the backward move instead of reporting "already on a
  newer version".
- **Interactive confirmation**: a rollback prompts ("Roll back from vA to vB?
  This installs an older release.") unless `--ci` or `--force` is set
  (non-interactive rollback requires `--force` to proceed, so a scripted
  downgrade is always explicit).
- A successful rollback **implicitly pins** to the target (a rollback that the
  next background check immediately "fixes" back to latest would be useless). The
  pin is written as part of the rollback; `--unpin` later resumes normal updates.
  *(This coupling is OQ1 — auto-pin-on-rollback vs. leave-unpinned-and-warn.)*

### Command surface

`pkg/cmd/update.NewCmdUpdate` gains flags, preserving the existing
`--version`⊕`--from-file` exclusivity and adding new exclusivity groups:

```text
--channel string     one-shot channel override for this invocation (stable|beta|canary)
--pin [version]       pin to version (or current if omitted); persists
--unpin               clear an existing pin
--rollback [version]  roll back to a prior release (or previous on channel)
```

Exclusivity (`MarkFlagsMutuallyExclusive`): `--from-file` remains exclusive with
`--version`/`--rollback`/`--channel`; `--pin`⊕`--unpin`; `--rollback`⊕`--unpin`.
`--version` and `--rollback` are exclusive (both name a target; rollback adds the
downgrade-confirm + auto-pin semantics). `--channel` composes with `--version`?
No — an explicit `--version` already names the target, so `--channel` is ignored
with a warning when both are given (OQ3).

Validation reuses `semVerPattern` for every version-bearing flag. Channel values
are validated by `UpdateChannel.Valid()` after `Normalize()`.

### Config surface

```yaml
update:
  policy: prompt          # existing
  check_interval: ""      # existing
  channel: stable         # NEW: stable | beta | canary (empty = tool baseline)
```

`update.channel` flows through the **same** Viper precedence chain as every other
key (CLI flag > env > file > embedded > default); no new config mechanism. The
pin is **not** a config key — it is operational state (a marker file), like
`last_checked`, so it survives `init` re-runs and is per-installation rather than
per-config-file. (Putting the pin in config would make it spuriously
diff/commit-able and would fight the multi-`--config` merge.)

### Background-check integration (`pkg/cmd/root`)

`checkForUpdates` / `shouldSkipUpdateCheck` gain two reads, both cheap and local:

1. If `GetPinnedVersion` is non-empty → skip the network check entirely and emit
   the single pinned-INFO (still subject to `--ci` suppression). The persistent
   "you're behind" WARN (`warnIfBehindCached`) is **suppressed** while pinned —
   being behind is intentional.
2. Otherwise resolve `update.channel` and pass it to the updater so the cached
   "latest" reflects the user's channel, not always-stable. The cached value
   continues to live in the `last_checked` marker body (no new file for the
   common path).

The `UpdatePolicy` branching (enabled/prompt/disabled) is unchanged — channel and
pin sit *upstream* of it (they decide the candidate; policy decides what to do
about it).

## Public API

### `pkg/props`

```go
type UpdateChannel string
const ( ChannelStable; ChannelBeta; ChannelCanary )
func (UpdateChannel) Normalize() UpdateChannel
func (UpdateChannel) Valid() bool
func (UpdateChannel) Includes(UpdateChannel) bool
func ChannelOf(tag string) UpdateChannel
func ResolveUpdateChannel(toolDefault UpdateChannel, configValue string) UpdateChannel

// Tool gains:
//   UpdateChannel UpdateChannel `json:"update_channel,omitempty" yaml:"update_channel,omitempty"`
// Zero value normalises to ChannelStable.
```

### `pkg/setup`

```go
func WithChannel(ch props.UpdateChannel) UpdaterOption
func WithAllowDowngrade() UpdaterOption // rollback path only

func (s *SelfUpdater) GetLatestReleaseForChannel(ctx context.Context, ch props.UpdateChannel) (release.Release, error)

func SetPinnedVersion(fs afero.Fs, name, version string) error
func GetPinnedVersion(fs afero.Fs, name string) string
```

`SelfUpdater` gains unexported `channel props.UpdateChannel` and `allowDowngrade
bool` fields, resolved/wired in `NewUpdater`.

### `pkg/cmd/update`

New flags `--channel`, `--pin`, `--unpin`, `--rollback` on `NewCmdUpdate`; new
exported `UpdateResult` fields:

```go
type UpdateResult struct {
    PreviousVersion string `json:"previous_version"`
    NewVersion      string `json:"new_version"`
    Updated         bool   `json:"updated"`
    Channel         string `json:"channel,omitempty"`     // NEW
    Pinned          bool   `json:"pinned,omitempty"`      // NEW
    RolledBack      bool   `json:"rolled_back,omitempty"` // NEW
}
```

No change to the `release.Provider`, `Release`, `ReleaseAsset`,
`ChecksumProvider`, or `SignatureProvider` interfaces, nor to
`ReleaseSourceConfig` / `props.ReleaseSource`. **This is deliberate** — channels
are a client-side view over the unchanged release stream.

## Error Handling

- Unknown `--channel` / `update.channel` → reject with a clear message listing
  the three valid values (validated via `UpdateChannel.Valid()`).
- `--rollback`/`--pin`/`--version` to a tag that does not exist → the existing
  `release.ErrReleaseNotFound`-wrapped surface from `GetReleaseByTag` (no new
  error type; the injectable-release-source spec already introduced the
  sentinel).
- Rollback where the target's checksum/signature fails verification → the
  **existing** verification errors abort the install, binary unchanged — a
  downgrade gets no weaker treatment. No new error types.
- `--rollback` non-interactive without `--force`/`--ci` → refuse with a non-zero
  exit and a message explaining a downgrade needs explicit confirmation (mirrors
  the ForcedUpdate "never mask a destructive action" principle).
- A channel selection that finds **no** candidate on the ladder (e.g. `beta`
  selected but only stable exists) falls back to the newest stable with an INFO
  ("no beta release found; latest stable is vX.Y.Z"), never an error.
- All new errors use `cockroachdb/errors` with hints per
  `docs/development/error-handling.md`.

## Testing Strategy (TDD)

Per the project TDD workflow, failing tests first, derived from the contracts.

### Unit (`pkg/props`)

- `ChannelOf` mapping for stable / `-beta.N` / `-canary.N` / `-rc.N` / malformed
  tags; `Includes` ladder truth table; `Normalize`/`Valid` edge cases;
  `ResolveUpdateChannel` precedence (config > baseline > stable), case-insensitive,
  empty/invalid fallbacks — mirroring the existing `ResolveUpdatePolicy` tests.
- `Tool` with `UpdateChannel` round-trips through JSON/YAML (and omitempty when
  unset). ≥90% coverage on the new file.

### Unit (`pkg/setup`)

- `GetLatestReleaseForChannel` over a `releasetest.Source` seeded with a mix of
  stable/beta/canary/draft tags: picks the correct max per channel; ladder
  inclusion; drafts skipped; empty-channel fallback to stable.
- `SetPinnedVersion`/`GetPinnedVersion` round-trip, clear, 0600 perms, empty-dir
  no-op (same invariants as `SetCheckedVersion`).
- Rollback: `allowDowngrade` lets `shouldSkipUpdate` permit "target < current";
  **checksum/signature verification still runs** on the downgrade target (assert
  a corrupt-checksum rollback aborts, binary unchanged — reuse the
  `releasetest` corrupt/bad-sig constructors); auto-pin written on success.
- `WithChannel`/`WithAllowDowngrade` are parallel-safe (two concurrent updaters,
  different channels, no interference) — the project's package-mock ban.

### Unit (`pkg/cmd/update`, `pkg/cmd/root`)

- Flag parsing + exclusivity matrix (`--pin`⊕`--unpin`, `--version`⊕`--rollback`,
  `--from-file` exclusivity preserved); invalid version/channel rejected.
- `checkForUpdates`: pinned → network check skipped, pinned-INFO emitted,
  `warnIfBehindCached` suppressed; unpinned → channel threaded into the updater.

### E2E BDD (Godog, gated)

Per the Godog suitability assessment
([`2026-03-28-godog-bdd-strategy.md`](2026-03-28-godog-bdd-strategy.md)),
channels/pin/rollback are multi-step, user-visible CLI workflows → BDD adds
value, and the **injectable release source**
([`2026-06-19-injectable-release-source.md`](2026-06-19-injectable-release-source.md))
makes them hermetic. Extend `cmd/e2e`'s `GTB_E2E_RELEASE_SCENARIO` selector with
channel-bearing fixtures (a `releasetest.Source` seeded with stable+beta+canary
tags) and add `features/cli/update.feature` scenarios that assert **outcomes
that abort or no-op before any binary swap** (safe against the shared E2E
binary):

- `--channel beta` selects the newest beta over an older stable (assert the
  *targeted* version in output; happy apply stays Go-side per the injectable
  spec's self-replacement caveat);
- pinned tool: background check reports "pinned", does not nag;
- `--rollback` to a tag with a **corrupt checksum** → non-zero exit, checksum
  message, binary unchanged;
- `--rollback` non-interactive without `--force` → refused, non-zero exit.

The happy forward/rollback *apply* path stays Go-side (`update_e2e_test.go` on
`releasetest`) — binary self-replacement is unsafe against the shared E2E binary
(same reasoning as the injectable-release-source spec).

### Verification gates

`just ci` (tidy, generate, test, test-race, lint), `just mocks` clean, ≥90%
coverage on new `pkg/` code, `@cli`/`@generator` Godog suites green.

## Generator Impact

- `props.Tool` is scaffolded by `internal/generator/templates/skeleton_root.go`.
  The new `UpdateChannel` field is additive + `omitempty`; existing scaffolded
  `props.Tool{…}` literals compile unchanged (nil/zero → stable). As with the
  `UpdatePolicy` baseline, the generator **may** expose an `--update-channel`
  flag + wizard step (validated by a new `generator.ValidateUpdateChannel`)
  rendering `UpdateChannel: props.ChannelStable` into the literal — *or* leave it
  unset and rely on the zero-value default. **OQ5** decides flag-vs-default; the
  low-friction default-only path is preferred unless review wants the wizard
  affordance.
- Re-run the generator golden/AST tests and a `generate project -p tmp && go
  build ./...` smoke to confirm the scaffolded tree builds against the updated
  `pkg/props`.

## Migration & Compatibility

- **Additive, backwards compatible.** No channel + no pin = today's stable,
  forward-only behaviour exactly. No interface, `ReleaseSource`, or wire-format
  change.
- Pre-1.0 (per CLAUDE.md): adding an exported `props` type, a `Tool` field, two
  `UpdaterOption`s, and `pkg/setup` helpers is non-breaking; `just apidiff` shows
  them as advisory additions. A one-line `docs/migration/` entry records the new
  channel/pin/rollback surface for the v1.0 guide.
- **Docs:** `docs/components/` update/release pages gain channel/pin/rollback
  sections, including the **tagging convention** a tool author must follow to
  publish a channel (`vX.Y.Z-beta.N` / `-canary.N`), and the
  `docs/development/feature-decisions.md` "channels ≠ profiles" note.

## Future Considerations

- **Server-driven / staged rollout** (percentage cohorts, remote channel
  assignment) — explicitly out of scope; would need release-side infrastructure
  GTB does not own.
- **Automatic rollback-on-failure** (keep the previous binary and auto-revert if
  the new one crashes on first run) — a larger feature (previous-binary cache,
  health gate) tracked separately.
- **`rc` as a first-class fourth channel** between beta and stable, rather than
  folding `-rc.*` into beta (OQ2).
- **Paging `ListReleases` beyond one page** to find deep historical rollback
  targets (OQ4).
- Aligning a provider-native prerelease signal *if* a future need outweighs the
  cross-backend interface churn (today the semver label is authoritative).

## Open Questions

1. **Auto-pin on rollback** — does a successful `--rollback` implicitly pin
   (spec's assumption, so the next check doesn't immediately re-upgrade), or stay
   unpinned and rely on a loud WARN each run? Auto-pin is safer and matches user
   intent ("get me off the bad release and *keep* me off it"); the cost is a user
   must `--unpin` to resume. **Lean: auto-pin.**
2. **`-rc.*` mapping** — fold release candidates into `beta` (spec's default), or
   add a distinct `rc` channel between beta and stable? Folding keeps three
   channels (simpler); a fourth is more precise for tools that publish RCs.
3. **Flag precedence/composition** — `--channel` + `--version` (spec: `--version`
   wins, `--channel` ignored with a warn); `--force` + `--pin` (does `--force`
   override a pin for a one-shot update without clearing it?); `--rollback` +
   `--force` (force = skip the interactive downgrade confirm). Confirm the matrix.
4. **`ListReleases` single-page bound** — rollback/channel selection scans the
   newest `releasesPerPage` (100) releases. Acceptable for the foreseeable range;
   a tool with >100 releases since the rollback target can't reach it without
   `--version`. Document as a known limit, or page now? **Lean: document.**
5. **Generator surface** — expose `--update-channel` + wizard step (consistent
   with `--update-check-interval`), or rely on the zero-value stable default only?
   **Lean: default-only**, add the flag only if review wants the affordance.
6. **Channel of the *running* binary** — for the "are you behind?" comparison on
   a non-stable channel, do we compare against the channel-max (so a beta user is
   "behind" only when a newer beta/stable exists)? Spec assumes **yes** (the
   channel-aware candidate is the comparison basis). Confirm this is the intended
   UX and doesn't surprise a user who hand-installed a beta.

---

*Roadmap item A6. DRAFT — pending human review of the open questions above before
implementation, per the project's spec-driven workflow.*
