---
title: "Segregated default configuration via props.Assets"
description: "Delete setup.DefaultConfig and read the embedded-defaults layer from assets/config.yaml through props.Assets, which already merges that path across every registered bundle. Each package ships its own defaults; registration is feature-gated — command constructors register their bundles when the enabled command is built, and non-command feature packages register through the setup registry. Init templates flow through the same bundles, replacing DefaultConfig, mergeExtraConfig and the initialiser Mount calls."
date: 2026-07-20
status: DRAFT
tags:
  - specification
  - config
  - assets
  - defaults
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Segregated default configuration via `props.Assets`

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   20 July 2026 — revised same day: open question 1 resolved by feature-gated
    registration (D8), and the init seeding path folded in (D9) after tracing
    `setup.Initialise` end to end. The first draft treated init as out of scope;
    it is not, because `setup.DefaultConfig` is also init's seed document.

Status
:   DRAFT

Related
:   [config v0.3.x migration](2026-07-20-config-v0.3-migration.md) — lands inside that MR;
    this supplies the layer its D13 calls "embedded defaults";
    [change scope](2026-07-20-config-chain-scope.md) — the delete/change/add inventory

## Summary

`setup.DefaultConfig` — a compiled-in `pkg/setup/assets/config.yaml` serving as both the
fallback configuration *and* the seed for `init`'s written config — is deleted. Its two roles
separate, and both become reads through `props.Assets`:

```go
fs.ReadFile(props.Assets, "assets/config.yaml")       // the defaults layer
fs.ReadFile(props.Assets, "assets/init/config.yaml")  // init's seed template
```

`Assets` merges each path across every registered bundle, so each package ships its own
defaults and its own init template, and the framework never needs a central file.
Registration is **feature-gated**: a bundle reaches `props.Assets` only when the feature
that owns it is enabled, so a disabled feature's defaults never leak into the resolved
configuration.

## This is the mechanism working as designed

`docs/explanation/components/assets.md` already describes exactly this, under Structured Data
Merging:

> This allows for a "patch-like" pattern where features can contribute additional settings to a
> global `config.yaml` … without needing to edit the original source files.

The pieces are all present:

- a package embeds `assets/*` and calls `p.Assets.Register("name", &assets)` in its command
  constructor (assets.md § Subcommand Contribution)
- constructors run while the command tree is built, before cobra invokes `PersistentPreRunE`,
  which is where `buildConfigStore` runs
- `Open` on a `.yaml` path forward-merges every registered bundle's copy in registration order
  (`pkg/props/assets.go:133`)

Verified rather than assumed — two bundles, one bare path:

```
Register("root"), Register("github")  →  fs.ReadFile(assets, "assets/config.yaml")
  → "github:\n  api: https://api.github.com\nlog:\n  level: info\n"
```

So there is no machinery to build beyond one registry slot (D8). `setup.DefaultConfig`
predates the pattern and bypasses it: a `[]byte` read directly, never consulting `Assets`.
Everything below is consequence.

## Decisions

### D1 — The defaults layer is `assets/config.yaml`, read through `Assets`

`buildConfigStore` reads that one path and adds it as the lowest layer. Absent is fine — a
tool need not ship defaults.

### D2 — Defaults are `assets/config.yaml`, not `assets/init/config.yaml`

The two shipped templates show why without needing an argument:

| File | Content |
|---|---|
| `pkg/setup/github/assets/init/config.yaml` | real values — `https://api.github.com` |
| `pkg/setup/ai/assets/init/config.yaml` | placeholders — `provider: ""`, `key: ""` |

`assets/init/config.yaml` is the template written to a user's config by `setup.Initialise`. It
is written to be read by a person: comments, and deliberately empty placeholders the wizard
fills in. `policy: ""` means "not set, defer to the baseline" **because it sits in a file the
user edits**. As a defaults layer it becomes a value that is genuinely present, and the ai
template would make `ai.provider` present-and-empty everywhere.

A package may ship either file, both, or neither.

### D3 — Precedence

Lowest to highest: `assets/config.yaml` (merged) → the tool's explicit `ConfigPaths` embedded
assets → config files → environment → flags.

This makes the user-facing precedence table's "embedded defaults shipped with the tool" row
true; today no single mechanism sits behind it.

### D4 — Defaults apply always, not only when no config file exists

The one real behaviour change.

`setup.DefaultConfig` today **substitutes** for a missing config file: if any config file
exists, the compiled-in defaults are never consulted. Phase 2 of the migration reproduced that
deliberately.

As a layer it always applies. A defaults layer that stops applying the moment a user creates a
config file is a fallback file, and it produces the surprising result that setting one key
costs you every default.

**User-visible, and belongs in the release notes.** A key absent from a user's config but
present in the defaults now resolves to the default where it previously resolved to zero.

### D5 — `setup.DefaultConfig` is deleted

Four read sites:

| Site | Replacement |
|---|---|
| `pkg/setup/init.go:192` (init's seed) | `fs.ReadFile(props.Assets, "assets/init/config.yaml")` — D9 |
| `pkg/setup/github/github.go:682` | the store's view — the values are in it |
| `pkg/setup/bitbucket/bitbucket.go:458` | as above |
| `pkg/cmd/root/root.go:222` (substitute branch) | the defaults layer, always present — D4 |

The github and bitbucket sites construct a throwaway Viper to read defaults the running store
already holds, so this removes two of the migration spec's D4 escape hatches as a side effect.

### D6 — The god file splits to its owning packages, and `update.*` ships no defaults

| Keys | Destination |
|---|---|
| `log.*` | `pkg/cmd/root/assets/config.yaml` — a framework bundle registered during root construction (D8) |
| `update.*` | **nowhere** — see below |
| `github.*` | `pkg/setup/github/assets/config.yaml`, deduplicating the copy already in its init template |

The god file's `update.policy: ""` and `update.check_interval: ""` are no-ops as defaults:
`props.ResolveUpdatePolicy` and `setup.ResolveCheckInterval` both treat an empty string as
"unset, defer to the tool baseline" (verified at `pkg/props/update_policy.go:58` and
`pkg/setup/update.go:150`). They exist as *commentary* for the user who opens their config
file — which is an init-template concern, so the commented placeholders move to the framework
bundle's `assets/init/config.yaml` (D9) and the defaults layer simply omits `update.*`.

### D7 — `docs/how-to/validate-component-config.md` is corrected

It names `assets/init/config.yaml` as the defaults location and shows mounting from an
initialiser provider. Both disagree with `assets.md`, which documents `Register` at
construction and reserves `Mount` for attaching an external `fs.FS` at a virtual prefix. The
how-to is the wrong one — it misled the first draft of this spec.

### D8 — Registration is feature-gated, through the two mechanisms that already exist

*(Resolves open question 1 of the first draft.)*

Two registration points, matching where features are already wired:

**Commands register their own bundles in their constructors** — the generator-emitted pattern
(`props.Assets.Register("<name>", &assets)` as the first statement). Built-in feature
commands are constructed inside `registerFeatureCommands` only when
`props.Tool.IsEnabled(feature)`, so command-owned defaults are inherently feature-gated with
no new machinery.

**Non-command feature packages register through the setup registry.** `pkg/setup/github`,
`pkg/setup/ai` and `pkg/setup/bitbucket` are not commands; they already announce themselves
via `setup.Register(feature, ...)` in package `init()`. The registry gains a sibling slot:

```go
// beside setup.Register / setup.RegisterChecks
func RegisterAssets(feature props.FeatureCmd, name string, bundle fs.FS)
```

called from the same `init()` functions. Root construction (`NewCmdRootWithOptions`, next to
`registerFeatureCommands`) walks the registered bundles and applies the gate:

```go
for feature, bundles := range setup.GetAssets() {
    if props.Tool.IsEnabled(feature) {
        for _, b := range bundles {
            props.Assets.Register(b.Name, b.FS)
        }
    }
}
```

This runs at construction time, before cobra invokes `PersistentPreRunE`, so every enabled
bundle is registered before `buildConfigStore` reads the merged paths. Timing verified — see
the change-scope spec.

**The framework's own bundle** (`log.*` defaults, framework init template) is registered
unconditionally during root construction from an embed in `pkg/cmd/root`, under the bundle
name `framework`. It is not feature-gated because logging is not a feature.

Alternatives rejected:

- *Widen `setup.Register` with an assets parameter* — churns three call sites and makes an
  already four-argument signature worse for a slot most features will not use.
- *Keep all defaults in one framework bundle* — recreates the god file under a new name,
  which is the thing being removed.

### D9 — Init seeds from the merged `assets/init/config.yaml`, and the Mount calls go

`setup.Initialise` today seeds the written config from `DefaultConfig`, then
`mergeExtraConfig` tries to fold in feature templates by opening the bare
`assets/init/config.yaml` — and silently gets nothing from ai and github, because their
initialisers `Mount` at a prefix and `mountedFS` serves only the prefixed path. That is the
live bug: `ai.provider`, `openai.api.key` etc. land in no generated config.

With D8, the enabled features' bundles are *registered* (not mounted) before any command runs,
so the bare `assets/init/config.yaml` path merges the framework template with every enabled
feature's template. Consequences:

- `initializeConfig` seeds from `fs.ReadFile(props.Assets, "assets/init/config.yaml")` —
  one read replaces the `DefaultConfig` read *and* `mergeExtraConfig`, which is deleted.
- The `p.Assets.Mount(assets, "pkg/setup/github")` / `"pkg/setup/ai"` calls in
  `NewGitHubInitialiser` / `NewAIInitialiser` are deleted — superseded, and they never
  served the purpose they were written for.
- **The written file remains a template, not a dump of defaults.** This matters: a default
  written into a user's file is pinned there — the file layer would override any future
  change to the shipped default. Templates with commented placeholders are what the wizard
  fills and the user edits; the defaults layer stays underneath, live.

**This is a live bug fix, not refactoring**, and it gets a regression test: the written
config from an init with ai and github enabled contains their template keys.

## Testing strategy

1. A package default in `assets/config.yaml` resolves when no user config file exists.
2. It still resolves when a user config exists but omits the key — D4, fails today.
3. A user config file overrides a default.
4. Two bundles defining the same key resolve in registration order, later winning.
5. `update.policy: ""` in an init template does **not** shadow `props.Tool.UpdatePolicy` — the
   D2 hazard, asserted directly.
6. A tool with `InitCmd` disabled gets its package defaults.
7. A **disabled** feature's bundle is not registered: its defaults are absent from the
   resolved configuration — D8's gate, asserted directly.
8. An init run with ai and github enabled writes a config containing their template keys —
   the D9 live-bug regression test. Must be shown to fail before the change, alongside test 2.

## Out of scope

Reading `assets.go` closely surfaced things worth knowing but not worth fixing here:
`Register` sets precedence on first registration and content on last; `Names()` returns the
internal slice rather than a copy; `embeddedAssets` has no mutex, unlike the sibling `setup`
registry; and `mountedFS` implements only `Open`, so mounted bundles drop out of `ReadDir` and
`Glob`. None are touched by this change. Recorded so they are not rediscovered as novel.

`Mount` itself also stays: attaching an external `fs.FS` at a virtual prefix is a real
feature (assets.md § Advanced Extensibility). Only its misuse as a defaults path goes.

## Open questions

None. Both questions from the first draft are resolved: registration is D8, and D4 is
release-note material recorded there.

## Status

DRAFT — awaiting review. No code moves until approved.
