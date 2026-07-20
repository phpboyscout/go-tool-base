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
`fs.ReadFile(Props.Assets, path)` per candidate path, so the embedded layer already benefits
from per-bundle merging. Only `setup.DefaultConfig` bypasses it.

So the remaining work is layering, not machinery — **provided the bundles are actually
mounted**, which is where this falls down.

## The mechanism works; nothing uses it for defaults

An earlier draft of this spec claimed asset registration happened too late for the config
store to see it. That was wrong, and the correction matters because it changes the shape of
the work from "fix a bug" to "adopt the existing mechanism".

**`Register` runs at command construction, before any config is resolved.** Subcommands are
constructed as arguments to `NewCmdRootWithOptions(props, WithSubcommands(subcommands...))`,
so argument evaluation — including each constructor's `p.Assets.Register(...)` — completes
before the root command exists, let alone before cobra invokes `PersistentPreRunE`, which is
where `buildConfigStore` runs. This is what `docs/explanation/components/assets.md` documents
under "Subcommand Contribution", and the generator emits exactly that
(`internal/generator/templates/command.go:270`).

Verified rather than reasoned about:

```
Register("root"), Register("github")  →  fs.ReadFile(assets, "assets/config.yaml")
  → "github:\n  api: https://api.github.com\nlog:\n  level: info\n"
```

Two bundles, one bare path, deep-merged in registration order.

**So a package can already ship defaults that reach the config store.** Nothing reads them as
defaults, because the defaults layer is `setup.DefaultConfig` — a compiled-in `[]byte` that
never consults `Assets` at all. That is the whole gap.

### `Mount` is a different feature and is not the defaults path

`assets.md` lists mounting under "Advanced Extensibility": attaching an arbitrary `fs.FS` at a
virtual prefix, for plugins and external resources. It is not subcommand contribution.

The distinction is not cosmetic — mounted content is addressable **only** under its prefix:

```
Mount(bundle, "pkg/setup/github")
  fs.ReadFile(assets, "assets/init/config.yaml")                    → file does not exist
  fs.ReadFile(assets, "pkg/setup/github/assets/init/config.yaml")   → content
```

`pkg/setup/ai` and `pkg/setup/github` mount their bundles from `New*Initialiser`, which is
correct for what those bundles are: init templates, needed when `init` runs interactively, and
the initialiser is exactly when that happens. They are not defaults and were never wired as
defaults.

It does mean the prefixed path is read by nothing in the repo — `mergeExtraConfig`
(`pkg/setup/init.go:168`) opens the bare `assets/init/config.yaml`, which resolves against the
tool's own registered bundle rather than these. Whether the two mounts are load-bearing or
vestigial is **open question 5**; it is a loose end, not the foundation of this change.

### Why the god file holds `github:`

`pkg/setup/github/assets/init/config.yaml` and the `github:` stanza in
`pkg/setup/assets/config.yaml` are byte-identical.

The earlier draft read this as a workaround for broken timing. The simpler reading is right:
one is the template written into a user's config at init, the other is the runtime default,
and they agree because they describe the same settings. The duplication is real and worth
removing — that is D7 — but it is ordinary drift-prone duplication, not evidence of a bug.

What it does show is that `pkg/setup/github` has no way to contribute a *default* today. It is
not a command, so it has no construction hook to `Register` from; its only entry point is the
initialiser, which is the wrong tool. That is the gap D1 has to close, and it is narrower than
the earlier draft claimed.

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

### D1 — Non-command packages get a registration hook that runs at tree construction

Commands already have one: their constructor. `pkg/setup/github`, `pkg/setup/ai` and any
future non-command package do not — their only callback is the initialiser provider, which
runs inside the init command's `RunE` and is scoped to interactive setup.

So the gap is narrow: a way for a package with no command to contribute an asset bundle at the
same point a subcommand does. Registration is already in an `init()` (`setup.Register`), and
the registry is sealed once the tree is built, so the bundle belongs alongside it.

Proposed: `setup.Register` gains an asset bundle, or a sibling `setup.RegisterAssets(name,
fs.FS)` is called from the same `init()`; the root command drains the registry and calls
`Props.Assets.Register` for each, in registry order, during tree construction.

Bundles must register **unprefixed**, so every package's `assets/config.yaml` collides on one
path and merges. That is what makes segregated defaults aggregate into a single defaults
document, and it is the opposite of what `Mount` does.

The existing `Mount` calls in `New*Initialiser` are left alone by this decision — they serve
init templates, not defaults (see open question 5).

**Exact mechanism is open question 1** — it is the only real design choice here and it sets
the API every package author touches.

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

Two existing quirks of `Register` become live once order carries meaning
(`pkg/props/assets.go:465-475`): re-registering an existing name **replaces the filesystem but
keeps its original position**, so precedence is set by first registration and content by last;
and the two traversal directions are opposite — static files shadow in reverse
(last-registered wins whole-file), structured files merge forward (last-registered wins
per-key). The net precedence agrees, but the implementations do not look alike, and a reader
checking one will draw the wrong conclusion about the other.

**`Assets` has no seal and no mutex.** `embeddedAssets` is a bare struct with a map and a
slice, mutated in place by `Register` with no locking — unlike the sibling `setup` registry,
which has both a `sync.RWMutex` and a seal that panics on late registration
(`pkg/setup/registry.go:52,78`). Reads walk `a.order` live, so a late `Register` is visible to
code that already read, which is exactly how the current bug hides. Once defaults depend on
registration having finished, a late registration must be an error rather than a silent
reordering. `Names()` also returns the internal slice rather than a copy
(`pkg/props/assets.go:79`), so a caller can corrupt precedence.

**`Assets` has no `ReadFile`, and adding one naively would reintroduce this class of bug.**
The interface embeds `fs.FS`, `fs.ReadDirFS`, `fs.GlobFS` and `fs.StatFS` — not
`fs.ReadFileFS`. Reads must go through `fs.ReadFile`, which falls back to `Open`, and `Open`
is where the cross-bundle merge happens. A hand-rolled `ReadFile` indexing a single bundle
would compile, look right, and silently return one bundle's copy. The migration's
`embeddedSources` had exactly this defect and is fixed alongside this spec.

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
9. A registration after sealing is an error.
10. A file in a mounted bundle is retrievable by the path its consumer actually opens — the
    dead-payload case, which current tests miss by asserting the mount rather than the read.
11. `fs.ReadFile` through `Assets` returns the merged document, not one bundle's copy.

Tests 7, 8 and 10 are the ones that would have caught the current bugs, and each must be shown
to fail before its fix.

## Migration procedure

**Phase A.** Move mounting to registration (D1), adopting the pattern the generator already
uses successfully. No behaviour change intended beyond assets becoming available earlier; the
existing constructor `Mount` calls stay until Phase C so nothing breaks mid-sequence.

This phase also settles the prefix question: whether bundles register unprefixed (so
`assets/config.yaml` collides across packages and merges, which is what a defaults layer
wants) or prefixed (so they stay addressable separately, which is what `Mount` attempted and
got wrong). The merge behaviour this spec relies on requires the unprefixed form.

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
5. **Are the `pkg/setup/ai` and `pkg/setup/github` mounts load-bearing or vestigial?** Their
   content is addressable only under the mount prefix, and nothing in the repo opens that
   path — `mergeExtraConfig` opens the bare one, which resolves against the tool's own
   registered bundle. Either they are dead and should go, or something is meant to read them
   and does not. Independent of this spec, but adjacent enough to settle while here.

## Status

DRAFT — question 1 sets the API and should be settled first. The rest can be decided during
implementation without reopening the shape of the change.
