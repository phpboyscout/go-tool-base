---
title: "config family: medium/low follow-ups from the architectural review"
description: "Batches the six MEDIUM and three LOW findings for the go/config module family from the 2026-07-23 architectural review into one workstream: exporting the sealed optional capability interfaces, defining the poll-interval contract once, fixing the consul watch/Verify interaction, backend ID uniqueness, synthesised source kinds, GTB's discarded watch stop handle, and a set of adapter documentation/escaping/error-taxonomy consistency fixes. The HIGH Apply finding is handled separately by its own spec."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - config
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# config family: medium/low follow-ups from the architectural review

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED — 2026-07-27 (core released as config `v0.9.3`; all adapter
    items merged; see § 5 for the resolved decisions and two flagged deviations)

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (§ config family),
    [config Apply fix](2026-07-23-config-apply-absent-source.md) (the sibling HIGH finding, specced separately),
    [config family version re-pin](2026-07-23-config-family-version-repin.md) (carries all of this into GTB)

---

## 1. Context

The 2026-07-23 architectural review of the `gitlab.com/phpboyscout/go/config`
family found the model sound — its verdict was "seam defects of a young
extraction, not cracks in the model". Beyond the single HIGH finding (Apply
failing on absent sibling sources, owned by the
[companion spec](2026-07-23-config-apply-absent-source.md)), it raised six
MEDIUM and three LOW findings spread across the core, several adapters, and
GTB's consuming glue. None is urgent alone; together they are one coherent
hardening pass over the family's seams, batched here as a single workstream so
the re-pin sweep carries them in one release wave rather than nine.

## 2. Items

### 2.1 Core capability seams

**F1 — Export the sealed optional capability interfaces.** *(MEDIUM)*
The comment-preservation probe (`config/codec.go:313-317`) asserts on an
anonymous `interface{ preservesComments() bool }`, and `watchErrorReporter` /
`watchPathReporter` (`config/codec.go:226-234`) carry unexported methods —
package-qualified, so no type outside package `config` can ever satisfy them
(verified at head). So `config-toml`/`config-hcl` genuinely preserve comments
yet report `PreservesComments == false`, external backends cannot route watch
errors to `OnReloadError` despite `store.go:1047-1052` claiming they can, and
an injected `Watcher` omits externally defined file-like backends.
Direction: export the three probes as public optional interfaces
(`CommentPreservingCodec`, `WatchErrorReporter`, `WatchPathReporter`) — the
pattern `FS`'s optional interfaces already get right — and adopt them in the
affected adapters.
*Acceptance:* a codec/backend defined outside package `config` satisfies each
interface and is observed by the store in a test.

**F2 — Validate backend ID uniqueness at construction.** *(MEDIUM)*
`adoptBackend` rejects duplicate IDs only on runtime `AddLayer`
(`config/store.go:265-278`); `NewStore` (`store.go:183-203`) appends unchecked
and `backendByID` returns the first match (`store.go:974-982`). With bare
prefix/path IDs (consul `app/`, azure `app/`), two backends can collide and a
routed write silently lands on the wrong system.
Direction: reject duplicate IDs in `NewStore`; decide whether adapter IDs get
kind-namespacing (Q2).
*Acceptance:* `NewStore` with two backends sharing an ID returns an error;
conformance suite asserts adapter IDs are collision-resistant per Q2's answer.

**F3 — Let backends declare their `SourceKind`.** *(MEDIUM)*
`config/store.go:932-940` synthesises the source entry for an empty writable
backend with a hard-coded `Kind: SourceFile` (verified at head), so `Plan`
output and `Operation.Target` present e.g. an empty Consul prefix with file
semantics; `sensitiveSources` (`store.go:949-953`) also skips synthesised
entries, so a future writable sensitive backend would lose the leak guard
while empty.
Direction: a small optional interface (or `Capabilities` field) through which
a backend declares its `SourceKind`; synthesised entries use it and inherit
sensitivity.
*Acceptance:* an empty writable non-file backend appears in `Plan`/`Sources`
with its own kind, and a synthesised sensitive source is guarded, under test.

### 2.2 Watch/poll contract

**F4 — Define the poll-interval contract once; stop billed 2s polling.**
*(MEDIUM)*
`Store.Watch` seeds `interval: DefaultPollInterval` (2s — `store.go:1000`,
verified; `watch.go:24`) and hands it to every backend. `config-azure-appconfig/watch.go:29-31`
honours it — so a default `Store.Watch` issues a billed List every 2 seconds
against the adapter's documented 30s intent — while `config-aws-ssm/watch.go:33`
and `config-gcp-parameter/watch.go:28-31` ignore it entirely, and
`config-consul` reuses it as blocking-query `WaitTime`. Three meanings, one
option.
Direction: define the contract once in the core — a "caller didn't choose"
sentinel or a backend-side `PollIntervalHinter` analogue (Q1) — and fix
azure-appconfig to use its own cadence when untouched.
*Acceptance:* default `Store.Watch` over an azure-appconfig backend polls at
the adapter's 30s cadence; an explicit `WithPollInterval` is honoured
uniformly, both under test.

**F5 — Consul watch loop must not advance the Load-time index.** *(MEDIUM)*
`config-consul/configconsul.go:118-123` documents `b.index` as captured at
Load so `Verify` compares against what routing saw — but
`config-consul/watch.go:52-58` advances it on every foreign change before the
store's settled reload runs, so `pending.Verify` (`write.go:150-160`) passes
for foreign changes the store has not yet reloaded; keys that informed
routing/shadowing decisions are unguarded (per-key CAS still covers written
keys). Direction: keep a separate watch cursor, or snapshot the index into
`pending` at Load and have Verify compare against that.
*Acceptance:* an interleaving test — foreign KV change seen by the watcher but
not yet reloaded → Apply — fails Verify.

### 2.3 Write-path integrity

**F6 — sftp: apply permissions before writing content.** *(LOW)*
`config-sftp`'s `WriteFile` creates the file at the server umask and `Chmod`s
to `perm` only after the payload is written — a staged file holding
credentials is briefly readable at the umask default on the remote host.
Direction: chmod (or open with mode) before writing bytes.
*Acceptance:* a fake-client test asserts the chmod/open-mode precedes the
first content write.

**F7 — Shared unwritable-value sentinel and wider scalar encoding.** *(LOW)*
Unwritable-value errors are `config.ErrBackendUnsafe` in toml but bare
module-local sentinels in consul (`configconsul.go:33-35`) and
azure-appconfig, so callers cannot `errors.Is` uniformly; the consul/azure
`encode` sets accept only `string/[]byte/bool/int/int64/float64`, refusing
`int32`, `uint`, `float32`.
Direction: wrap adapter unwritable-value errors in the shared core sentinel;
widen `encode` via `cast`.
*Acceptance:* `errors.Is(err, config.ErrBackendUnsafe)` holds across toml,
consul, and azure-appconfig; `int32`/`uint`/`float32` round-trip in both KV
adapters.

### 2.4 Adapter consistency

**F8 — Editing-codec escape rules.** *(LOW)*
The core YAML path escapes invisible/bidi characters as a load-bearing
guarantee (`config/yaml_codec.go:206-217`); `config-toml/write.go`'s `quote()`
escapes only `" \ \n \t \r`, so other control bytes render invalid TOML
(fail-closed, but surfacing as a confusing `ErrBackendParse` on the user's own
write); `config-json` feeds `edit.Path` verbatim into sjson/gjson path syntax
(`config-json/configjson.go:90-106`), where `*`, `?`, `#`, `\`, and numeric
segments carry meta-meaning — a key literally named `0` can address an array
index.
Direction: escape control characters in the TOML quoter, escape gjson path
metacharacters, and add a shared "renderable scalar / hostile key" case to the
`backendconformance` suite.
*Acceptance:* the new conformance case (control bytes, bidi, a key literally
named `0`, gjson metacharacters) passes on yaml, toml, and json codecs.

**F9 — config-vault: docs and dead state.** *(LOW)*
`config-vault/configvault.go:8` and `:76` link a `[NewPrefix]` constructor
that does not exist (broken godoc), `KV.List` is reachable by nothing, and
`backend.version` is recorded "for the watch" though no `Watch` is
implemented — `Store.Watch` over a vault-only store fails loudly.
Direction: decide Q3 — either ship `NewPrefix` plus a polling `Watch`, or cut
the doc claims, the `List` method, and the dead field.
*Acceptance:* godoc renders with no broken links and no exported symbol is
dead; if Watch ships, it runs the conformance watch cases.

### 2.5 GTB glue

**F10 — Retain the watcher stop handle.** *(MEDIUM)*
`pkg/cmd/root/root.go:830` (verified): `if _, err := cfg.Watch(cmd.Context());`
discards the stop function, so teardown is purely contextual — it depends on
the cancel-on-signal context wired in `pkg/cmd/root/execute.go:111-168`. An
embedder calling `ExecuteContext(context.Background())` on the reusable
command tree leaks the fsnotify/poll goroutines with no way to stop them.
Direction: retain the stop func on `rootState` and invoke it in the shutdown
path, with context cancellation as the backstop.
*Acceptance:* a test executing the root command with a background context
observes the watcher goroutines stopped after shutdown.

## 3. Scope & release plan

Repos that change:

1. **`gitlab.com/phpboyscout/go/config` (core)** — F1 interface exports, F2
   ID-uniqueness check, F3 source-kind declaration, F4 sentinel/contract, F7
   shared sentinel, F8 conformance case. Additive exports plus one
   constructor-time validation; ships first as a single minor.
2. **Adapters** — `config-toml`/`config-hcl` (F1); `config-azure-appconfig`,
   `config-aws-ssm`, `config-gcp-parameter`, `config-consul` (F4, F5, F7);
   `config-json`, `config-sftp`, `config-vault` (F8, F6, F9). Each re-pins the
   new core and adopts its items; independent of one another.
3. **GTB** — F10 only, plus the dependency bumps.

Sequencing: core changes land and release first → adapters adopt against the
released core → GTB last. Version propagation across the family and into GTB
is owned by the [re-pin spec](2026-07-23-config-family-version-repin.md);
these releases slot into that sweep (ideally the same wave as the Apply fix).
Items are independently mergeable — a stalled open question on one must not
hold the others.

## 4. Open questions

- **Q1 — poll-interval sentinel design.** Zero value as "unset" with
  per-backend defaults, a negative sentinel constant, or a backend-side
  `PollIntervalHinter` analogue? The hinter keeps `WithPollInterval`'s meaning
  single ("override everything") but adds an interface; the sentinel is
  smaller but makes the option's semantics conditional. Decide before F4.
- **Q2 — backend ID namespacing.** Is constructor-time duplicate rejection
  enough, or should adapter IDs be kind-namespaced (`consul:app/`)?
  Namespacing changes `Source.Name`/provenance output, so if wanted it should
  ride this pre-1.0 wave.
- **Q3 — config-vault direction.** Implement `NewPrefix` + a polling `Watch`,
  or trim the docs and dead state to match reality? Trimming is an hour; a
  watch is a real feature with billed-read cadence questions of its own (F4).
- **Q4 — encode widening scope.** Should the `cast`-based widening (F7) accept
  every fixed-width numeric, or stop at the lossless set and keep refusing
  e.g. `float32`?

## 5. Resolution (2026-07-27)

Landed as one wave: **config core** shipped first as `v0.9.3` (F1 interface
exports, F2 duplicate-ID rejection, F3 `SourceKindDeclarer`, F4 backend-side
`PollIntervalHinter` consumption, F8 conformance case), then the adapters
re-pinned it and adopted their items. Every behavioural item was captured
red-first (a regression test that failed on unmodified `main` before the fix);
for the F1 export, an external codec/backend that could not satisfy the sealed
interface was the compile-red evidence.

**Resolved open questions**

- **Q1 — poll interval: backend-side `PollIntervalHinter` analogue** (not a
  sentinel). A `WatchableBackend` may implement the existing
  `PollIntervalHinter`; `Store.Watch` adopts the hint only where the caller left
  the default, so `WithPollInterval` keeps its single meaning ("override
  everything"). `config-azure-appconfig` advertises its 30s cadence,
  `config-aws-ssm`/`config-gcp-parameter` now honour the interval they are handed
  **and** advertise their 60s cadence.
- **Q2 — backend ID: duplicate-ID rejection now; kind-namespacing deferred.**
  Constructor-time duplicate-ID rejection shipped in core (`NewStore` →
  `ensureUniqueIDs`, `ErrInvalidTarget`) with a `backendconformance` ID-stability
  assertion — this is the substantive safety mechanism. The kind-namespacing half
  (`consul:app/`) is **deferred** (see § 6) — see the deviation note below.
- **Q3 — config-vault: already resolved upstream, no change.** The trim premise
  was stale: config-vault `v0.2.0` (2026-07-25) shipped the *build* direction —
  a real `NewPrefix`, prefix mode, collision refusal and a polling `Watch`. All
  three "defects" (phantom `NewPrefix` doc, dead `KV.List`, dead `version` field)
  are gone; a `TestDocLinksResolve` guard exists. No config-vault MR was raised.
- **Q4 — encode widening: lossless set only.** `config-consul` and
  `config-azure-appconfig` widen `encode` via `cast` to the lossless numeric set
  (all fixed-width signed/unsigned ints + `float64`) and keep **refusing**
  `float32` (and maps/slices), with a test pinning the float32 refusal.

**Per-item outcomes**

- **F1** exported `CommentPreservingCodec` / `WatchErrorReporter` /
  `WatchPathReporter`; `config-toml` and `config-hcl` now report comment
  preservation through the public interface (previously always `false`).
- **F2** duplicate-ID rejection in `NewStore` (see Q2).
- **F3** `SourceKindDeclarer` — synthesised sources use the declared kind and a
  synthesised sensitive source is leak-guarded.
- **F4** poll-interval contract defined once in core; adapters as under Q1.
- **F5** `config-consul` keeps a separate `watchIndex` cursor so the watch loop
  no longer advances the Load-time `index` that `Verify` compares against; an
  interleaving change now correctly fails `Verify`.
- **F6** `config-sftp` chmods before writing content.
- **F7** shared `ErrBackendUnsafe` wrapping + `cast`-widened encode in consul and
  azure-appconfig (see Q4).
- **F8** editing-codec escape rules: TOML quoter escapes control bytes; config-json
  escapes gjson/sjson path metacharacters per segment; the new
  `renderable_scalars_and_hostile_keys` conformance case runs on yaml, toml and
  json.
- **F9** no change needed (see Q3).
- **F10** GTB retains and invokes the config watcher stop handle on shutdown
  (`pkg/cmd/root`), with context cancellation as the backstop.

**Merged MRs:** config core !69 (+ conformance follow-up !73); config-toml !18,
config-hcl !17, config-json !18, config-sftp !13, config-azure-appconfig !14,
config-consul !17, config-aws-ssm !14, config-gcp-parameter !14; go-tool-base
!309 (F10).

**Flagged deviations**

1. **Q2 kind-namespacing deferred to an atomic sweep** (ratified). Namespacing
   adapter IDs (`consul:app/`) changes provenance/`Source.Name` output and would
   need to land across *all* ~10 remote config adapters at once to stay
   consistent — most are outside this batch's F-items and several were already
   merged, so partial namespacing would be worse than none. The core duplicate-ID
   rejection (the safety fix) shipped; the namespacing is tracked in § 6.
2. **`config-azure-appconfig` carries a test-only wrapper.** Because azure honours
   the interval it is handed and hints a 30s cadence, the shared
   `backendconformance` watch case (`foreign_change_reaches_observers`) — which
   opened its watch at the default interval with a 3s window — timed out. Azure's
   MR resolved it locally with a test-only `hintlessBackend` wrapper (no public
   API change). The **proper** fix landed in core follow-up MR !73
   (`fix(config)`, releases as the next config patch): the conformance case now
   drives its watch with an explicit fast `WithPollInterval`, robust to any
   slow-hint polled backend. Azure's local wrapper can be dropped when it re-pins
   to that core version in the re-pin sweep.

## 6. Deferred follow-up

- **Atomic backend-ID kind-namespacing across config remote adapters.** Give each
  remote/KV adapter a kind-namespaced `ID()` (e.g. `consul:app/`,
  `azure-appconfig:app/`, `aws-ssm:/app`) so two backends of different kinds
  scoped to the same prefix/path no longer collide on a bare ID, and provenance
  names the kind. Must land atomically across the whole remote-adapter set
  (consul, azure-appconfig, azure-blob, azure-keyvault, aws-ssm, aws-s3,
  aws-secrets, gcp-parameter, gcp-gcs, gcp-secret, vault) so `Source.Name` output
  stays consistent, and updates each adapter's provenance tests. Rides the
  config-family re-pin sweep (see the
  [ecosystem-fleet-maintenance spec](2026-07-23-ecosystem-fleet-maintenance.md)).
  The core duplicate-ID rejection already shipped (F2/Q2), so this is a
  provenance/UX refinement, not a safety gap.
