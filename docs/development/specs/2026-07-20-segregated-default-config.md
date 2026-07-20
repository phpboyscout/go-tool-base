---
title: "Segregated default configuration via props.Assets"
description: "Replace the single compiled-in setup.DefaultConfig god file with defaults resolved through props.Assets, so each package or command contributes its own defaults and they merge deterministically. Makes framework defaults available to tools that do not use local config initialisation."
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
:   [config v0.3.x migration](2026-07-20-config-v0.3-migration.md) — this changes the layer
    that migration's D13 calls "embedded defaults", so the two land together or in order.

## Summary

`setup.DefaultConfig` is a single compiled-in `pkg/setup/assets/config.yaml`. It already
carries `log`, `update` and `github` settings in one file, so it is a god file in the making:
every package that wants a default has to add a stanza to a file owned by `pkg/setup`.

Defaults should instead resolve through `props.Assets`, so each package or command ships its
own defaults and the framework merges them.

## The mechanism already exists

This is the finding that sizes the work. `props.Assets` is not a lookup that picks one bundle
— for structured files it **merges the same path across every registered bundle**, in
registration order, later overriding earlier:

```go
// assets.go — openMergedStructured
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

`a.order` is registration order, not map order, so this is deterministic. `.yaml`, `.yml`,
`.json`, `.toml`, `.xml`, `.properties` and `.env` all take this path; static assets use
reverse-search shadowing instead.

So segregated per-package defaults are already supported by the asset layer. What bypasses it
is `setup.DefaultConfig`, a `//go:embed` byte slice read directly.

The embedded-asset config layer already goes through `Assets` and therefore already merges
per bundle. Only the *default* layer does not.

## Motivation

**The god file.** `pkg/setup/assets/config.yaml` holds `github.url.api`, `github.auth.env` and
`github.ssh.key.env` — settings owned by `pkg/setup/github`. Adding a default for a new
subsystem means editing a file in an unrelated package.

**Tools that do not initialise local config.** A tool with `InitCmd` disabled never writes a
local config file, so `assets/init/config.yaml` never applies to it. Its only defaults are
whatever the framework compiled in — it cannot ship its own without a config file on disk.
This is the case the change is really for.

**Consumers cannot extend framework defaults.** A tool built on GTB can register asset
bundles today, but nothing in the default path reads them.

## Decisions

### D1 — Defaults resolve through `props.Assets`, not a compiled-in byte slice

`setup.DefaultConfig` stops being the source of default configuration. The framework reads
its defaults path through `props.Assets`, which merges every registered bundle's copy.

The framework registers its own bundle so its defaults participate in the same merge as a
consumer's rather than being privileged. Registration order decides precedence, and the
framework registers first so a tool can override any framework default.

### D2 — Defaults come from `assets/config.yaml`, not `assets/init/config.yaml`

These are different things and conflating them would be a quiet bug.

`assets/init/config.yaml` is a **template for the file a user will own**. It is written to
disk by `setup.Initialise`, and it is written to be *read by a person*: comments explaining
each setting, and deliberately-empty placeholders. The framework's own copy contains

```yaml
update:
  # Leave empty so the tool author's baseline (props.Tool.UpdatePolicy) applies;
  # set it to override.
  policy: ""
```

An empty string there means "not set, defer to the tool baseline" **because it is sitting in a
file the user is expected to edit**. Promote that same file to a defaults layer and `policy:
""` becomes a value that is genuinely present — it would shadow `props.Tool.UpdatePolicy`
rather than defer to it, and the tool author's baseline would stop applying to every tool that
ships an init template.

`assets/config.yaml` is therefore a separate path meaning "defaults that apply whether or not
a user config exists". A package may ship either, both, or neither.

### D3 — Precedence: defaults sit below embedded asset configs

Lowest to highest:

| | Layer |
|---|---|
| 1 | `assets/config.yaml`, merged across bundles — **new** |
| 2 | embedded asset configs at the tool's `ConfigPaths` |
| 3 | config files (`--config` or the defaults) |
| 4 | environment variables |
| 5 | CLI flags |

This is additive to the migration's D13: it splits what that spec calls "embedded defaults"
into two layers and gives the lower one a merge rule.

### D4 — Defaults apply always, not only when no config file exists

This is the behaviour change, and it is the point of the exercise.

Today `setup.DefaultConfig` **substitutes** for a missing user config file — if any config
file exists, the compiled-in defaults are not consulted at all. That is why the config v0.3
migration was careful to reproduce it: layering it unconditionally would have changed
behaviour.

Under this spec that changes deliberately. A defaults layer that only applies when the user
has no config file is not a defaults layer; it is a fallback file. A user who creates a config
file setting one key should not thereby lose every framework default.

**This is user-visible and needs saying plainly in the release notes.** A key that is absent
from a user's config file but present in the defaults will now resolve to the default where
before it resolved to zero. That is the intended behaviour, and it is still a change in what
existing installations read.

### D5 — `setup.DefaultConfig` stays, narrowed

Three callers use it for something other than the default config layer:

| Site | Use |
|---|---|
| `pkg/setup/init.go:192` | seeds the config being written during init |
| `pkg/setup/github/github.go:682` | reads `github.*` defaults when configuring |
| `pkg/setup/bitbucket/bitbucket.go:458` | same, for Bitbucket |

These want "the framework's shipped defaults" as a document, which is a legitimate and
different need from "the defaults layer of a running store". The variable stays and keeps
serving them; it stops being what the store reads.

Whether the `github:` stanza moves out of the framework file and into `pkg/setup/github`'s own
bundle is **open question 2** — it is the god-file problem in miniature, and it changes what
those two callers see.

## Blast radius

Small, because the merge machinery exists.

- `pkg/cmd/root` — one layer added to `buildConfigStore`
- `pkg/setup` — register the framework asset bundle; possibly split `assets/config.yaml`
- the generator — scaffolded tools get an `assets/config.yaml` alongside the init template
- documentation — the precedence table, and a how-to on shipping package defaults

## Testing strategy

1. A default in a package's `assets/config.yaml` resolves when no user config file exists.
2. The same default still resolves when a user config file exists but omits the key — this is
   D4, and it fails today.
3. A user config file overrides a default.
4. Two bundles defining the same key resolve in registration order, later winning.
5. A tool bundle overrides a framework default.
6. `update.policy: ""` in an init template does **not** shadow `props.Tool.UpdatePolicy` —
   the D2 hazard, asserted directly.
7. A tool with `InitCmd` disabled still gets its package defaults.

## Open questions

1. **Does the framework register one bundle or one per package?** One bundle
   (`pkg/setup/assets/config.yaml`) is the smaller change and keeps the god file, merely
   demoting it. One per package (`pkg/setup/github/assets/config.yaml`, and so on) is the
   thing that actually solves the problem, at the cost of every framework package gaining an
   embed and a registration.
2. **Does the `github:` stanza move out of the framework defaults file?** It is the clearest
   instance of the god-file problem, and moving it changes what `github.go` and
   `bitbucket.go` read via `setup.DefaultConfig` (D5).
3. **Does D4's always-apply behaviour need a migration note for existing installs**, or is
   "defaults now actually apply" self-evidently correct? A key a user deliberately left out of
   their config to get the zero value would start resolving to the default.
4. **Should this land inside the config v0.3.x migration MR or follow it?** It changes a layer
   that migration introduces, so doing it after means writing `buildConfigStore` twice; doing
   it inside means one MR carrying two user-visible behaviour changes.

## Status

DRAFT — not to be implemented until the open questions are settled. Question 1 in particular
decides whether this is a small change or a broad one.
