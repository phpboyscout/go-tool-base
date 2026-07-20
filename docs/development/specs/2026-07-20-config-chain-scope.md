---
title: "Change scope: the configuration chain"
description: "Exact inventory of what is deleted, changed and added in the config chain — before any code moves. Covers the v0.3.x migration, segregated defaults, and the simplifications both make available: flags become a layer, DefaultConfig disappears, and post-hoc flag binding goes with it."
date: 2026-07-20
status: DRAFT
tags:
  - specification
  - config
  - scope
  - assets
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Change scope: the configuration chain

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   20 July 2026

Status
:   DRAFT — scope for review. No code changes until agreed.

Related
:   [config v0.3.x migration](2026-07-20-config-v0.3-migration.md),
    [segregated default config](2026-07-20-segregated-default-config.md)

## Purpose

An exact inventory of what changes, established before anything moves. Two previous attempts
at this design failed by inferring the mechanism from one document instead of reading the
code; this exists so that cannot happen a third time.

Everything below was established by reading the source on this branch, not from the specs.

## Baseline

The config path today is **295 lines across 10 functions** in `pkg/cmd/root/root.go`, plus
`pkg/setup/init.go` at 274 lines.

| Function | Lines | Fate |
|---|---:|---|
| `newRootPreRunE` | 70 | change |
| `buildConfigStore` | 55 | change |
| `resolveBootstrapConfig` | 37 | change |
| `embeddedSources` | 28 | change |
| `autoInitialiseConfig` | 28 | fix |
| `discoverProjectConfig` | 21 | unchanged |
| `projectConfigPaths` | 19 | unchanged |
| `anyConfigFilePresent` | 17 | unchanged |
| `setupRootFlags` | 12 | unchanged |
| `configOpts` | 8 | **delete** |

118 non-test call sites read or write `props.Config`, across 19 packages. `pkg/cmd/config`
(26), `internal/generator` (19) and `pkg/setup` (14) are the concentrations.

## Defects to fix first

Three exist on this branch now, all introduced or exposed by Phase 2. They are listed
separately because they are corrections, not design.

| # | Defect | Location |
|---|---|---|
| 1 | `autoInitialiseConfig` calls `loadAndMergeConfig`, which was deleted and does not exist | `root.go:378` |
| 2 | `configOpts` names `config.ContainerOption`, `WithLogger`, `WithEnvPrefix` — all deleted in v0.3. Dead and non-compiling | `root.go:102-109` |
| 3 | Orphan doc comment for `mergeEmbeddedConfigs`, whose function was removed | `root.go:386-387` |

Defect 1 is the serious one: the auto-initialise reload path has no implementation, so a tool
with `AutoInitialise` set would not build.

## Deletions

### D-1 — `setup.DefaultConfig` and `pkg/setup/assets/config.yaml`

Four read sites, each with a replacement that already exists:

| Site | Today | After |
|---|---|---|
| `pkg/setup/init.go:192` | `cfg.ReadConfig(DefaultConfig)` | `fs.ReadFile(props.Assets, "assets/config.yaml")` |
| `pkg/setup/github/github.go:682` | `v.ReadConfig(DefaultConfig)` fallback | the store's view |
| `pkg/setup/bitbucket/bitbucket.go:458` | same fallback | the store's view |
| `pkg/cmd/root/root.go:222,227` | substitute layer when no file | the defaults layer, always present |

The github and bitbucket sites construct a throwaway Viper to read defaults the running store
already holds. Deleting them removes two of the migration spec's eleven Viper escape hatches
as a side effect. Note `pkg/setup/ai` does **not** have this fallback — an existing
inconsistency that resolves itself.

**~24 lines of YAML, one `//go:embed`, one exported var, four call sites.**

### D-2 — The post-hoc flag binding path

This is the largest single simplification and it is available only because of v0.4.0.

Today flags are registered on the flag set at build time (`root.go:719`), then bound onto
config **after** the store is constructed (`root.go:808`). That path is already broken:
`bindChangedFlags` requires `BindPFlag`, which `*config.Store` does not have.

v0.4.0 has `config.WithFlags(flags, opts...)` (`store.go:130`), which makes flags an ordinary
layer. The store is built inside `PersistentPreRunE`, where the dispatched command — and
therefore its full flag set — is already known, so a single construction-time `WithFlags`
covers all three groups the current code binds separately.

Deletes:

| Symbol | Location | Lines |
|---|---|---:|
| `bindCommandFlags` | `root.go:853-888` | 36 |
| `bindChangedFlags` | `options.go:120-138` | 19 |
| `builtinBoundFlags` | `root.go:892-895` | 4 |
| `bindable` interface | `options.go:142-144` | 3 |

Also removes the `flags.CI \|\| props.Config.GetBool("ci")` idiom duplicated at `root.go:444`,
`:501` and `:1012` — it exists precisely because the flag was bound into config yet callers
did not trust it. With flags as a layer, `GetBool("ci")` is authoritative.

`WithBoundFlags`, `WithConventionBoundFlags` and `ConventionKey` stay: they are the public API
tool authors use to declare the flag-to-key mapping, and that mapping is what `WithFlags`
needs.

**~62 lines, plus three duplicated conditionals.**

### D-3 — The viper-bypass writers

| Symbol | Location | Lines | Why |
|---|---|---:|---|
| `ensureMinimalConfig` | `root.go:1049-1073` | 25 | builds a fresh `viper.New()` and writes behind the store |
| the same, duplicated | `pkg/cmd/telemetry/telemetry.go:245` | ~20 | near-identical minimal-config writer |

Both exist because the store had no way to persist a single key. `Store.Apply(ctx,
config.Set(...))` does, and preserves comments while doing it. `promptTelemetryConsent`
(`root.go:1035-1038`) loses its `Set` + `GetViper().WriteConfig()` pair for the same reason.

**~45 lines and one duplicated concept.**

### D-4 — Dead code found while mapping

| Symbol | Location | Why dead |
|---|---|---|
| `configOpts` | `root.go:102-109` | defect 2 above |
| `InitOptions.SkipLogin/SkipKey/SkipAI` | `setup/init.go:50-64` | superseded by the registry's `FeatureFlag` closures; nothing reads them |
| `dirPermUserOnly` | `setup/init.go:24` | unreferenced |
| orphan comment | `setup/init.go:29` | duplicate of `:38` |
| orphan comment | `setup/init.go:274` | describes `configureSSHKeyConfig`, which lives in `github/ssh.go` |
| orphan comment | `root.go:386-387` | defect 3 above |

Note `autoInitialiseConfig:369-372` passes `SkipLogin/SkipKey/SkipAI: true` — dead fields, so
that call is relying on flags that do nothing. Whether the intent (a non-interactive init with
no credential wizards) is achieved by `Interactive: &false` alone needs confirming before the
fields go.

## Changes

### C-1 — `buildConfigStore` gains a defaults layer and loses a branch

The four-way switch collapses. `setup.DefaultConfig` was a *substitute* for a missing file;
`assets/config.yaml` is a *layer*, so the "no file" case stops being special:

```
1. assets/config.yaml            merged across bundles    (new, always)
2. ConfigPaths embedded assets   existing
3. CfgPaths files                existing
4. env                           existing
5. flags                         (new — was post-hoc binding)
```

The `!AllowEmpty → ErrNoConfigFile` arm stays: auto-initialise depends on it.

One inconsistency to resolve while here — the current `DefaultConfig` branch declares **no
file layer**, so that store has nothing writable, while the `default:` branch declares the
files specifically so `Apply` and `config path` work. The collapsed form should always declare
the files.

### C-2 — `embeddedSources` loses its error return

It never returns a non-nil error, so the check at `buildConfigStore:200-202` is unreachable.

### C-3 — `autoInitialiseConfig` calls `buildConfigStore`

Fixes defect 1.

### C-4 — `mergeExtraConfig` reads `assets/config.yaml`

Currently opens `assets/init/config.yaml` (`setup/init.go:168`). Two consequences:

- it picks up init *templates*, including their deliberately-empty placeholders
- mounted bundles are unreachable through it, because `mountedFS` only serves the prefixed
  path — which is why `pkg/setup/ai`'s defaults land in no generated config today

Reading `assets/config.yaml` through `Assets` fixes both. **This is a live bug fix, not
refactoring.**

### C-5 — The 118 read sites

Mechanical: `props.Config.GetX(...)` → `props.Config.View().GetX(...)`. Already scoped as
Phase 3 of the migration spec, split per package.

Worth recording: **zero call sites use `AddObserver`, `Watch`, `With` or `View` today.** The
reactive surface is entirely unadopted. Adopting it is explicitly *not* in this scope — it
would be a second behaviour change riding on a migration that is meant to be
behaviour-preserving.

### C-6 — The generator seeds `assets/config.yaml`

`generateAssetFiles` (`internal/generator/files.go:30-65`) writes
`<cmdDir>/assets/init/config.yaml` containing `"<name>:\n"`. It gains a sibling
`assets/config.yaml`.

The registration is already correct — `templates/command.go:269-273` emits
`props.Assets.Register("<name>", &assets)` as the constructor's first statement. **No template
change needed for wiring**, only for the seeded file.

## Additions

| Item | Where |
|---|---|
| `assets/config.yaml` per owning package | `pkg/setup/github/`, framework core for `log.*` / `update.*` |
| A registration point for non-command packages | open question 1 below |
| Generator seed for `assets/config.yaml` | `internal/generator/files.go` |

`update.*` keys are owned by `pkg/setup` (`update.go`, `update_signature.go`), **not**
`pkg/cmd/update`, which has no config keys at all. `log.*` has no owning package. Both
therefore land in a framework bundle rather than a command's.

## Net effect

| | Lines |
|---|---:|
| Deleted outright | ~155 |
| Dead code removed | ~20 |
| Added | ~30 |
| **Net** | **−145** |

Plus: one god file gone, one duplicated YAML block gone, one duplicated minimal-config writer
gone, three duplicated `ci` conditionals gone, two Viper escape hatches gone, and one live bug
fixed (ai defaults never reaching a written config).

## Order of work

Each step leaves the tree in a defined state.

1. **Defects** — fix 1–3. Restores intent to Phase 2; no behaviour change.
2. **Phase 3** — the 118 read sites, split per package. Restores the build. Largest diff,
   lowest risk.
3. **Phase 4/5** — writes move to `Apply`; the remaining escape hatches; D-3 deletions.
4. **Flags as a layer** — D-2. Behaviour-preserving in intent but touches precedence, so it
   wants its own tests and its own commit.
5. **Defaults layer** — C-1, C-4, D-1, the per-package `assets/config.yaml` files. The one
   deliberate user-visible behaviour change.
6. **Generator** — C-6, then the emission tests.
7. **Docs** — the precedence table, `validate-component-config.md`, `assets.md`.

Steps 4 and 5 are the two that change behaviour. Neither should share a commit with anything
else.

## Risks

**Test couplings to literal paths.** `generator_test.go:80-83` hardcodes
`assets/init/config.yaml` and the `"new-cmd:"` content; `skeleton_test.go:50,247` hardcode
`pkg/cmd/root/assets/init/config.yaml`; `detectAssets` matches the literal string
`//go:embed assets/*` and would not match `all:assets`.

**`props/test.New()` builds empty Assets.** Tests asserting framework defaults will start
seeing zero values unless the fixture registers the framework bundle.

**`SealRegistry` is never called in production** — only from two tests. If registration order
starts carrying precedence, nothing currently prevents a late registration.

**Defaults now always apply** (segregated-defaults spec D4). A key a user deliberately omitted
to get the zero value will resolve to the default. Release-note material.

## Open questions

1. **Where do `pkg/setup/github` and `pkg/setup/ai` register their bundles?** They are not
   commands. Their `NewCmdInit*` constructors exist but mount nothing — the mount happens in
   `New*Initialiser`, which runs during `init`'s `RunE`. Moving registration into
   `NewCmdInitGitHub` / `NewCmdInitAI` would work for tools that enable those commands, and
   not otherwise.
2. **Do the dead `SkipLogin/SkipKey/SkipAI` fields carry intent that `Interactive: &false`
   does not?** `autoInitialiseConfig` passes all three.
3. **Should `ConfigPaths` and `CfgPaths` be renamed?** They are disjoint namespaces against
   different filesystems in different precedence tiers, distinguished only by a missing three
   letters. Free to fix while the struct is being touched; out of scope if not.

## Status

DRAFT — scope for review. Open question 1 blocks step 5; everything before it can proceed.
