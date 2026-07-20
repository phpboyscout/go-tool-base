---
title: "Segregated default configuration via props.Assets"
description: "Remove setup.DefaultConfig entirely and resolve the embedded-defaults layer through props.Assets, so each package ships its own defaults. Fixes the underlying availability bug: per-package assets are mounted from initialiser constructors that run inside the init command's RunE, long after the config store is built, which is why the framework needed a compiled-in god file in the first place."
date: 2026-07-20
status: DRAFT
tags:
  - specification
  - config
  - assets
  - defaults
  - foundational
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
    this replaces the layer its D13 calls "embedded defaults"

## Summary

`setup.DefaultConfig` — a single compiled-in `pkg/setup/assets/config.yaml` — stops existing.
The embedded-defaults layer resolves entirely through `props.Assets`, which already merges a
given path across every registered bundle.

The interesting part is not the deletion. It is **why the god file exists**, which turns out
to be a bug rather than a design choice: per-package assets are mounted too late to be seen by
the config store, so the only way to make a default reliably apply was to compile it into the
framework. Deleting `DefaultConfig` without fixing that would silently delete the defaults
along with it.

## The mechanism already exists

`props.Assets` does not pick one bundle for a structured file. It **forward-merges** the path
across every registered bundle in registration order, later overriding earlier:

```go
// pkg/props/assets.go — openMergedStructured
// Structured data uses Forward Merge (Root -> Sub1 -> Sub2)
for _, fsName := range a.order {
	current, err := a.processAssetFile(fsName, name, ext)
	if err != nil {
		continue
	}
	if merged == nil {
		merged = current
	} else {
		_ = mergo.Merge(&merged, current, mergo.WithOverride)
	}
}
```

`a.order` is registration order, not map order, so it is deterministic. `.yaml`, `.yml`,
`.json`, `.toml`, `.xml`, `.properties` and `.env` take this path; static assets use
reverse-search shadowing instead.

The config store already reads through it. `buildConfigStore` (Phase 2 of the migration) calls
`Props.Assets.ReadFile` per candidate path, so the embedded layer already benefits from
per-bundle merging. Only `setup.DefaultConfig` bypasses it.

So the remaining work is layering, not machinery — **provided the bundles are actually
mounted**, which is where this falls down.

## The root cause: assets mount after the config is built

This is the finding that reframes the whole change.

`docs/how-to/validate-component-config.md` instructs package authors to mount their assets
from an `InitialiserProvider`:

```go
func init() {
    setup.Register(props.FeatureCmd("myfeature"),
        []setup.InitialiserProvider{
            func(p *props.Props) setup.Initialiser {
                p.Assets.Mount(assets, "pkg/myfeature")
                return &Initialiser{}
            },
        },
    )
}
```

Both production `Mount` calls follow it — `pkg/setup/ai/ai.go:313` and
`pkg/setup/github/github.go:174`, each inside a `New*Initialiser` constructor.

Those constructors run only when `discoverInitialisers` invokes the providers, and that is
called from **inside `RunE` of the init command** (`pkg/cmd/initialise/init.go:46`).

The consequence, in two parts:

- During any normal command — `mytool serve`, `mytool doctor` — the providers never run, so
  `pkg/setup/ai` and `pkg/setup/github` assets are **never mounted at all**.
- During `mytool init` they are mounted, but `RunE` runs *after* `PersistentPreRunE`, which is
  where the config store is built. So even then the defaults arrive **after** the store that
  was supposed to read them.

**The documented per-package defaults mechanism has therefore never worked for defaults.** It
works for `setup.Initialise`, which reads assets during init, and that is the only reason the
pattern looks correct in practice.

### The evidence that this is the god file's cause

`pkg/setup/github/assets/init/config.yaml` exists and contains exactly:

```yaml
github:
  url:
    api: https://api.github.com
    upload: https://uploads.github.com
  auth:
    env: GITHUB_TOKEN
  ssh:
    key:
      env: GITHUB_KEY
```

The `github:` stanza in `pkg/setup/assets/config.yaml` is byte-identical. The values were
duplicated into the framework file because the package's own copy never mounts in time. The
god file is not misplacement — it is the workaround.

## Motivation

**The god file grows.** Every package wanting a reliable default must add a stanza to a file
owned by `pkg/setup`, because its own bundle does not work.

**Tools that never initialise get nothing.** A tool with `InitCmd` disabled never runs
`discoverInitialisers` at all, so no per-package asset is ever mounted, and
`assets/init/config.yaml` never applies. Its only defaults are whatever the framework compiled
in. This is the case the change is really for.

**Consumers cannot extend framework defaults.** A tool can register bundles, but nothing in
the default path reads them at the time it matters.

## Decisions

### D1 — Mounting moves to command-tree construction, before any config is built

The fix has to come first; everything else depends on it.

Asset mounting must happen while the command tree is built, which is before cobra runs any
`PersistentPreRunE`. Registration is already the right shape for this — `setup.Register` is
called from `init()` functions and the registry is sealed once the tree is built — so the
bundle belongs in the registration, not in a constructor invoked much later.

Proposed: `setup.Register` gains an asset argument, or a sibling `setup.RegisterAssets(name,
fs.FS)` is called from the same `init()`. Bundles are mounted onto `Props.Assets` during root
command construction, in registry order.

The existing `Mount` calls inside `New*Initialiser` become redundant and are removed. They are
currently the only thing that makes ai and github assets available during init, so removing
them without D1 in place would break `setup.Initialise`.

**Exact mechanism is open question 1** — it is the one part of this spec that is genuinely a
design choice rather than a correction.

### D2 — Defaults come from `assets/config.yaml`, not `assets/init/config.yaml`

These are different documents and the codebase already treats them differently. Compare the
two shipped init templates:

| File | Content | Present in the god file |
|---|---|---|
| `pkg/setup/github/assets/init/config.yaml` | real values — `https://api.github.com` | **yes**, duplicated |
| `pkg/setup/ai/assets/init/config.yaml` | placeholders — `provider: ""`, `key: ""` | **no** |

`assets/init/config.yaml` is a **template for a file the user will own**. It is written to
disk by `setup.Initialise` and written to be read by a person: comments, and deliberately
empty placeholders that the wizard fills in. The framework's own copy contains

```yaml
update:
  # Leave empty so the tool author's baseline (props.Tool.UpdatePolicy) applies;
  # set it to override.
  policy: ""
```

An empty string means "not set, defer to the tool baseline" **because it sits in a file the
user edits**. Promote that same file to a defaults layer and `policy: ""` becomes a value that
is genuinely present — it would shadow `props.Tool.UpdatePolicy` rather than defer to it, and
the tool author's baseline would stop applying for every tool shipping an init template. The
ai template would likewise make `ai.provider` present-and-empty everywhere.

`assets/config.yaml` is therefore a distinct path meaning "defaults that apply whether or not
a user config exists". A package may ship either, both, or neither.

This contradicts `docs/how-to/validate-component-config.md`, which currently names
`assets/init/config.yaml` as the defaults location. That guide is wrong on two counts — the
path, and the claim that mounting from an initialiser makes defaults apply — and D6 covers it.

### D3 — Precedence

Lowest to highest:

| | Layer |
|---|---|
| 1 | `assets/config.yaml`, merged across bundles in registration order |
| 2 | the tool's explicit `ConfigPaths` embedded assets |
| 3 | config files — `--config` if given, else the defaults, then project-local |
| 4 | environment variables |
| 5 | CLI flags |

Layer 1 is what the user-facing precedence table calls "embedded defaults shipped with the
tool". There is currently no single mechanism behind that row; this makes the row true.

### D4 — Defaults apply always, not only when no config file exists

This is the behaviour change, and it is the point.

Today `setup.DefaultConfig` **substitutes** for a missing config file: if any config file
exists, the compiled-in defaults are not consulted at all. Phase 2 of the migration reproduced
that deliberately, because layering it unconditionally would have changed behaviour.

Under this spec it changes. A defaults layer that stops applying the moment a user creates a
config file is a fallback file, not defaults — and it produces the surprising outcome that
setting one key costs you every framework default.

**This is user-visible and belongs in the release notes.** A key absent from a user's config
file but present in the defaults now resolves to the default where before it resolved to zero.

### D5 — `setup.DefaultConfig` is removed entirely

Three callers use it today:

| Site | Use | Replacement |
|---|---|---|
| `pkg/setup/init.go:192` | seeds the config being written during init | read `assets/config.yaml` through `Props.Assets` |
| `pkg/setup/github/github.go:682` | reads `github.*` defaults while configuring | the store's own view — the values are in it by then |
| `pkg/setup/bitbucket/bitbucket.go:458` | same, for Bitbucket | as above |

The latter two construct a throwaway Viper to read defaults the running store already holds.
Once D1 makes the bundles available at build time, they can read from the store instead, which
also removes two of D4's Viper escape hatches in the migration spec.

Once `pkg/setup/assets/config.yaml` has been split per package (D7), the file and its
`//go:embed` go with it.

### D6 — `docs/how-to/validate-component-config.md` is corrected, not merely updated

The guide currently documents a mechanism that does not work. It must change on three points:

1. defaults live in `assets/config.yaml`, not `assets/init/config.yaml` (D2)
2. bundles are registered, not mounted from an initialiser (D1)
3. the diagram showing `Props.Assets.Open()` feeding a "Viper merge hierarchy" is replaced by
   the layer model

Anyone who followed this guide has defaults that silently never applied. That is worth an
explicit note rather than a quiet correction.

### D7 — The god file is split per owning package

`pkg/setup/assets/config.yaml` is distributed to the packages that own its keys:

| Keys | Destination |
|---|---|
| `log.*` | framework core |
| `update.*` | framework core |
| `github.*` | `pkg/setup/github/assets/config.yaml` — deduplicating the existing copy |

Whether the framework core keeps one shared bundle or splits further is **open question 2**.

## Ramifications

**Ordering becomes semantic.** Registration order decides which default wins when two bundles
define the same key. Today nothing depends on it; afterwards a reordering is a behaviour
change. The order needs to be deterministic and documented, and the framework must register
first so a consumer can override it.

**Sealing.** The registry is sealed after the tree is built. Mounting must happen before the
seal, and a late `Mount` should be an error rather than a silent no-op that produces exactly
the class of bug this spec exists to fix.

**Tools that disable features.** `discoverInitialisers` filters by `props.Tool.IsEnabled`.
Whether a disabled feature's defaults should still apply is a real question: its config keys
are inert, but its defaults appearing in `config list` may confuse. Proposed: mount
regardless, because a default for a disabled feature is harmless and filtering reintroduces
conditional availability.

**Test fixtures.** `props/test.New()` builds `NewAssets()` with nothing registered. Tests
asserting framework defaults will need the framework bundle, or will start seeing zero values.

**Generated tools.** The generator scaffolds `pkg/cmd/root/assets/init/config.yaml`. It gains
an `assets/config.yaml` alongside it, and the generated registration wires the bundle.

**Startup cost.** Merging every bundle's `assets/config.yaml` happens once at construction and
is bounded by the number of registered bundles. Not a concern, but it moves work into startup
that previously did not exist.

## Testing strategy

1. A package default in `assets/config.yaml` resolves when no user config file exists.
2. It still resolves when a user config file exists but omits the key — this is D4, and it
   fails today.
3. A user config file overrides a package default.
4. Two bundles defining the same key resolve in registration order, later winning.
5. A consumer bundle overrides a framework default.
6. `update.policy: ""` in an init template does **not** shadow `props.Tool.UpdatePolicy` — the
   D2 hazard, asserted directly.
7. A tool with `InitCmd` disabled still gets its package defaults — the case that is broken
   today, asserted to be fixed.
8. Defaults are present during `PersistentPreRunE`, not merely by `RunE` — the availability
   bug, pinned so it cannot regress.
9. A `Mount` after sealing is an error.

Tests 7 and 8 are the ones that would have caught the current bug, and both must be shown to
fail before the fix.

## Migration procedure

**Phase A.** Move mounting to registration (D1). No behaviour change intended beyond assets
becoming available earlier; the existing constructor `Mount` calls stay until Phase C so
nothing breaks mid-sequence.

**Phase B.** Add the `assets/config.yaml` defaults layer to `buildConfigStore` (D2, D3, D4).

**Phase C.** Split the god file (D7), remove `setup.DefaultConfig` and the redundant
constructor mounts (D5).

**Phase D.** Documentation (D6), including the note that the previous guidance did not work.

## Acceptance criteria

1. `setup.DefaultConfig` does not exist.
2. A package can ship a default that applies to a tool which never runs `init`.
3. The `github:` stanza exists in exactly one place.
4. The precedence table's "embedded defaults" row is backed by one mechanism.
5. Tests 7 and 8 above are shown to fail before the fix and pass after.

## Open questions

1. **How are bundles registered?** A new argument to `setup.Register`, a sibling
   `setup.RegisterAssets`, or something else. This is the only genuine design choice in the
   spec, and it sets the API every package author touches.
2. **Does the framework core keep one bundle or split further?** `log.*` and `update.*` have
   no obvious owning package short of creating one.
3. **Do disabled features' defaults still apply?** Proposed yes; it is a judgement call.
4. **Is D4's always-apply change worth a migration note for existing installs?** A user who
   deliberately omitted a key to get the zero value will now get the default.

## Status

DRAFT — question 1 sets the API and should be settled first. The rest can be decided during
implementation without reopening the shape of the change.
