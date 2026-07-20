---
title: "Segregated default configuration via props.Assets"
description: "Delete setup.DefaultConfig and read the embedded-defaults layer from assets/config.yaml through props.Assets, which already merges that path across every registered bundle. Each package ships its own defaults instead of adding a stanza to a framework-owned god file."
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
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Segregated default configuration via `props.Assets`

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   20 July 2026

Status
:   DRAFT

Related
:   [config v0.3.x migration](2026-07-20-config-v0.3-migration.md) — lands inside that MR;
    this supplies the layer its D13 calls "embedded defaults"

## Summary

`setup.DefaultConfig` — a compiled-in `pkg/setup/assets/config.yaml` — is deleted. The
embedded-defaults layer becomes one read through `props.Assets`:

```go
fs.ReadFile(props.Assets, "assets/config.yaml")
```

`Assets` merges that path across every registered bundle, so each package ships its own
defaults and the framework never needs a central file.

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

So there is no machinery to build. `setup.DefaultConfig` predates the pattern and bypasses it:
a `[]byte` read directly, never consulting `Assets`. Everything below is consequence.

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
fills in. The framework's own copy has

```yaml
update:
  # Leave empty so the tool author's baseline (props.Tool.UpdatePolicy) applies
  policy: ""
```

`policy: ""` means "not set, defer to the baseline" **because it sits in a file the user
edits**. As a defaults layer it becomes a value that is genuinely present, shadowing
`props.Tool.UpdatePolicy` for every tool that ships a template. The ai template would likewise
make `ai.provider` present-and-empty everywhere.

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

Three callers today:

| Site | Replacement |
|---|---|
| `pkg/setup/init.go:192` | `fs.ReadFile(props.Assets, "assets/config.yaml")` |
| `pkg/setup/github/github.go:682` | the store's view — the values are in it |
| `pkg/setup/bitbucket/bitbucket.go:458` | as above |

The latter two build a throwaway Viper to read defaults the running store already holds, so
this removes two of the migration spec's D4 escape hatches as a side effect.

### D6 — The god file splits to its owning packages

| Keys | Destination |
|---|---|
| `log.*`, `update.*` | a framework bundle registered during root construction |
| `github.*` | `pkg/setup/github/assets/config.yaml`, deduplicating the copy already there |

`pkg/setup/github` and `pkg/setup/ai` are not commands, so they have no constructor to
`Register` from. Where their registration happens is **open question 1** — the only part of
this that is not already solved.

### D7 — `docs/how-to/validate-component-config.md` is corrected

It names `assets/init/config.yaml` as the defaults location and shows mounting from an
initialiser provider. Both disagree with `assets.md`, which documents `Register` at
construction and reserves `Mount` for attaching an external `fs.FS` at a virtual prefix. The
how-to is the wrong one — it misled the first draft of this spec.

## Testing strategy

1. A package default in `assets/config.yaml` resolves when no user config file exists.
2. It still resolves when a user config exists but omits the key — D4, fails today.
3. A user config file overrides a default.
4. Two bundles defining the same key resolve in registration order, later winning.
5. `update.policy: ""` in an init template does **not** shadow `props.Tool.UpdatePolicy` — the
   D2 hazard, asserted directly.
6. A tool with `InitCmd` disabled gets its package defaults.

Test 2 must be shown to fail before the change.

## Out of scope

Reading `assets.go` closely surfaced things worth knowing but not worth fixing here:
`Register` sets precedence on first registration and content on last; `Names()` returns the
internal slice rather than a copy; `embeddedAssets` has no mutex, unlike the sibling `setup`
registry; and `mountedFS` implements only `Open`, so mounted bundles drop out of `ReadDir` and
`Glob`. None are touched by this change. Recorded so they are not rediscovered as novel.

## Open questions

1. **Where do `pkg/setup/github` and `pkg/setup/ai` register their bundles?** They have no
   command constructor. Options: the framework registers them during root construction, they
   gain a registration hook alongside `setup.Register`, or their defaults live in the
   framework bundle and only the duplication is removed.
2. **Is D4's always-apply change worth a migration note?** A user who omitted a key to get the
   zero value now gets the default.

## Status

DRAFT — open question 1 is the only blocker, and it is small.
