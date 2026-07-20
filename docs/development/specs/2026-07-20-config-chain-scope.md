---
title: "Change scope: the configuration chain"
description: "Exact inventory of what is deleted, changed and added in the config chain — before any code moves. Covers the v0.3.x migration, segregated defaults, and the simplifications both make available: flags become a layer, DefaultConfig disappears, and post-hoc flag binding goes with it."
date: 2026-07-20
status: IMPLEMENTED
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
:   IMPLEMENTED — 20 July 2026

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

### C-4 — Init seeds from the merged `assets/init/config.yaml`; `mergeExtraConfig` is deleted

*(Revised 2026-07-20 — the first version had init reading `assets/config.yaml`, i.e. writing
defaults into the user's file. Wrong direction: a default written into a user's file is pinned
there, overriding any future change to the shipped default. The written file is a template.)*

`mergeExtraConfig` opens the bare `assets/init/config.yaml` (`setup/init.go:168`), but ai and
github **mount** at a prefix and `mountedFS` only serves the prefixed path — so their init
content lands in no generated config today. **A live bug, and `mergeExtraConfig` has no test
in either direction.**

With bundles *registered* per segregated-defaults D8, the bare path merges the framework
template with every enabled feature's template. `initializeConfig` seeds from one
`fs.ReadFile(props.Assets, "assets/init/config.yaml")`, replacing both the
`setup.DefaultConfig` read and `mergeExtraConfig` (~26 lines, deleted). The `Mount` calls in
`NewGitHubInitialiser` / `NewAIInitialiser` are deleted as superseded. See
segregated-defaults D9, including the regression test.

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
| Framework bundle: `assets/config.yaml` (`log.*`) + `assets/init/config.yaml` (template) | `pkg/cmd/root/assets/`, registered unconditionally at root construction as `framework` |
| `assets/config.yaml` for `github.*` | `pkg/setup/github/assets/` |
| `setup.RegisterAssets(feature, name, bundle)` + `GetAssets()` registry slot | `pkg/setup/registry.go` — segregated-defaults D8 |
| Feature-gated bundle application at root construction | `pkg/cmd/root`, beside `registerFeatureCommands` |
| Generator seed for `assets/config.yaml` | `internal/generator/files.go` |

`update.*` ships **no defaults at all**: `ResolveUpdatePolicy` and `ResolveCheckInterval`
treat `""` as "unset, defer to the tool baseline" (verified), so the god file's empty-string
stanza was a no-op as a default. Its commented placeholders are template commentary and move
to the framework bundle's `assets/init/config.yaml`. `log.*` has no owning package and lands
in the framework bundle.

## Net effect

| | Lines |
|---|---:|
| Deleted outright | ~180 (incl. `mergeExtraConfig` ~26 and the two `Mount` calls — C-4 revision) |
| Dead code removed | ~20 |
| Added | ~45 (incl. `RegisterAssets`/`GetAssets` and the gated application loop) |
| **Net** | **≈ −155** |

Plus: one god file gone, one duplicated YAML block gone, one duplicated minimal-config writer
gone, three duplicated `ci` conditionals gone, two Viper escape hatches gone, and one live bug
fixed (ai/github init content never reaching a written config).

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

1. ~~**Where do `pkg/setup/github` and `pkg/setup/ai` register their bundles?**~~ **Resolved
   2026-07-20: through the setup registry they already announce themselves in.**
   `setup.RegisterAssets(feature, name, bundle)` is called from the same package `init()`
   as `setup.Register`; root construction applies the bundles of enabled features to
   `props.Assets` before `PersistentPreRunE` runs. See segregated-defaults D8.
2. ~~**Do the dead `SkipLogin/SkipKey/SkipAI` fields carry intent that `Interactive: &false`
   does not?**~~ **Resolved 2026-07-20: no — delete them, and it is not a regression.** The
   user-facing skip behaviour never flowed through `InitOptions`: the `--skip-login` /
   `--skip-key` / `--skip-ai` flags bind the package-level vars in `pkg/setup/github` and
   `pkg/setup/ai` (via the registry `FeatureFlag` closures), which gate the
   `InitialiserProvider` (nil when skipped) and populate `GitHubInitialiser.SkipLogin/SkipKey`
   — a different struct, which is read (`github.go:197-217`). Nothing has ever read the
   `InitOptions` fields in this repo's history (`git log -S 'opts.SkipLogin'` is empty back to
   the initial commit). `autoInitialiseConfig` is doubly covered without them: it passes no
   `Initialisers` at all, so the wizard loop iterates an empty slice, and
   `Interactive: &false` guards the same path.
3. **Should `ConfigPaths` and `CfgPaths` be renamed?** They are disjoint namespaces against
   different filesystems in different precedence tiers, distinguished only by a missing three
   letters. Free to fix while the struct is being touched; out of scope if not.
4. ~~**Flags-as-a-layer surface (D-2)**~~ **Resolved 2026-07-20: accept — honest provenance,
   no new exposure.** `config.WithFlags(cmd.Flags())` contributes every changed flag by the
   dash-to-dot convention (net new: `--config` and `--output` become visible config keys, and
   inherited persistent flags participate). The exposure question was checked against both
   read surfaces: `config list`/`get` route every value through `Masker.MaskIfSensitive`
   (masked by default, `--unmask` is explicit opt-in), and the doctor report's
   `report_redact.go` is a single redaction choke point — credential-shaped keys are dropped
   to a sentinel and every other leaf is scrubbed through `redact.String`. A token supplied
   via a flag therefore sits behind exactly the masking the same token would get from a
   config file; the surface is unchanged.

## Status

IMPLEMENTED — 20 July 2026. Open questions 1, 2 and 4 were resolved before implementation;
question 3 (the `ConfigPaths`/`CfgPaths` rename) was left as-is — out of scope, still open
as a future cleanup.
