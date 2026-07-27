---
title: "config: Apply must tolerate absent sibling sources in rebuild"
description: "Store.Apply fails outright whenever any other declared source is absent, because rebuild re-loads every non-written backend and treats fs.ErrNotExist as fatal — unlike loadAll, which carries an absent optional source forward with no layers. GTB is directly exposed: the root command deliberately declares the highest-precedence write target even when the file does not yet exist, so config set on a key defined in a lower file fails until the top file has been created."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - config
  - bugfix
  - toolkit-module
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# config: Apply must tolerate absent sibling sources in rebuild

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Shipped in go/config v0.9.2 (config MR !49), red-first TDD with the defect reproduced against the module's pre-fix main before the change; GTB picks it up via the config-family / module dependency bumps. Open questions were resolved by the maintainer during triage.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (§ config family, HIGH finding),
    [config v0.3 migration](2026-07-20-config-v0.3-migration.md) (the release line that introduced `rebuild`),
    [config write-batching follow-ups](2026-07-20-config-write-batching-followups.md),
    [config family version re-pin](2026-07-23-config-family-version-repin.md) (the release that will carry this fix into GTB)

---

## 1. Problem

`Store.Apply` in `gitlab.com/phpboyscout/go/config` fails outright whenever any
*other* declared source is absent — even though declaring an optional,
not-yet-created source is an explicitly supported pattern at load time.

The asymmetry is between two functions in `config/store.go`:

- **`loadAll` (store.go:506–539)** tolerates a missing source: on
  `errors.Is(err, fs.ErrNotExist)` it carries the backend forward with no
  layers (fatal only for the *first* backend, and only under `requireFirst`).
  The code comments this as deliberate — "a source that is not there
  contributes nothing but is still a candidate for a write: creating it is how
  a new overlay comes into being."
- **`rebuild` (store.go:851–879)** — introduced by `f6eba9e` ("re-derive
  key-aware backends when building a candidate", shipped in v0.3.0) — re-loads
  every backend that was *not* written to (so key-aware backends such as env
  see the keys the write produced) and treats **any** load error, including
  `fs.ErrNotExist`, as fatal. There is no absent-source branch.

**Verified failure scenario** (reproduced with a test against the module): a
store declaring `base.yaml` (exists) and `overlay.yaml` (declared, absent — the
ordinary optional-overlay setup). `Apply(Set("foo", "baz"))` for a key defined
in `base.yaml` routes to `base.yaml`; `rebuild` then re-loads the absent
`overlay.yaml` and the whole Apply fails with `overlay.yaml: file does not
exist`. The write is refused even though the routed target is perfectly
writable.

**GTB is directly exposed.** `declaredConfigPaths`
(`pkg/cmd/root/root.go:292–304`) deliberately declares the highest-precedence
write target even when the file does not exist yet — that is *how* a write
creates it — and `config set` (`pkg/cmd/config/set.go`, `cfg.Set` at :47,
`props.Config.Apply` at :101 via `applyAndHarden`) uses unpinned routing. So on
any tool where the user config file has not been created yet, `config set` of a
key that already exists in a lower layer (embedded defaults, a system file)
fails with a confusing "file does not exist" error about a file the user never
mentioned.

The conformance/regression suites never caught this because no test applies a
write while a *different* declared source is absent.

## 2. Proposed change

In `rebuild`, mirror `loadAll`'s absent-source branch: when a non-written
backend's `Load` returns an error satisfying `errors.Is(err, fs.ErrNotExist)`,
carry the backend forward with no layers instead of failing:

```go
layers, err := bl.backend.Load(ctx, flatten(next))
if err != nil {
    if errors.Is(err, fs.ErrNotExist) {
        next = append(next, backendLayers{backend: bl.backend})
        continue
    }
    return nil, err
}
```

Notes on scope of the tolerance:

- Only `fs.ErrNotExist` is tolerated — parse failures, unsafe documents, and
  permission errors remain fatal in `rebuild` exactly as they are in
  `loadAll`, for the same reason: silently dropping a layer changes the
  effective configuration without telling anyone.
- `requireFirst` does not need re-checking here: a required-first source that
  existed at load and vanished mid-Apply is a genuine environment change; see
  open question Q1 for whether that edge deserves distinct handling.
- The written backend's staged layers are untouched; only the carried-over /
  re-read path changes.

**Regression coverage:** add a test to the shared `backendconformance` suite —
"Apply routed at one source succeeds while a sibling declared source is
absent" — so every adapter that runs the suite locks the behaviour, plus a
core `store` unit test mirroring the verified failure scenario above
(two-file store, absent overlay, Set on a key in the base file).

## 3. Scope & release plan

Repos that change:

1. **`gitlab.com/phpboyscout/go/config`** (the fix + tests). Ships as
   `fix(store): tolerate absent declared sources during Apply rebuild` — a
   patch/minor via the human-gated releaser-pleaser Release MR. This is the
   only *required* code change.
2. **GTB** picks the fix up via its config dependency bump. That bump is owned
   by the companion re-pin spec
   ([2026-07-23-config-family-version-repin.md](2026-07-23-config-family-version-repin.md));
   sequencing there is deliberately "config core fix first, then the family
   re-pin", so the sweep carries this fix rather than requiring a second GTB
   bump. No GTB code change is expected — `declaredConfigPaths` and
   `config set` are behaving as designed; the store was not.
3. **Adapters** need no code change; they inherit the conformance-suite test
   when they next re-pin (again, the companion spec).

Ordering: config core Release MR merged and tagged → re-pin sweep (companion
spec) → GTB Release MR. No other module blocks on this.

## 4. Acceptance criteria

- [ ] New core unit test: a store with an existing base source and a declared
      absent overlay accepts `Apply(Set(...))` routed at the base source; the
      resulting snapshot reflects the write and the absent source contributes
      no layers. Fails on the current head, passes with the fix.
- [ ] New `backendconformance` case exercising Apply with an absent sibling
      declared source, run green by at least the fs-backed adapters.
- [ ] A parse error (not `fs.ErrNotExist`) in a sibling source during
      `rebuild` still fails the Apply — covered by a test.
- [ ] `requireFirst` load-time behaviour is unchanged (existing tests pass).
- [ ] In GTB (once re-pinned): `config set <key> <value>` succeeds on a tool
      whose user config file does not yet exist while the key is defined in an
      embedded default — verified by an e2e or integration scenario.
- [ ] config CHANGELOG entry describes the user-visible symptom, not just the
      internals.

## 5. Open questions

- **Q1 — vanished-required-source edge.** If the *first* source (with
  `requireFirst`) exists at load but is deleted before an Apply, should
  `rebuild` fail (treating disappearance of a required source as fatal) or
  tolerate it like any other absent source? Tolerating is simpler and matches
  the "last-known-good plus the write" spirit; failing is arguably more
  honest. Maintainer call.
- **Q2 — backfill Reload.** `Reload` goes through `loadAll` and is therefore
  already tolerant; is there any other re-load path (e.g. watch-triggered
  settle) that shares `rebuild`'s strictness and should be audited in the same
  MR?
