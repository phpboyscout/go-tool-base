---
title: "Config write batching and provenance follow-ups"
description: "The follow-ups the migration's simplify review recorded: an Editor that batches through one Apply, an atomic exclusive-credential write shared by the three wizards (fixing bitbucket's missing stale-key clearing), unset validating the layered result instead of the bare file, and a Plan-derived writable target. Also records the rejection of gating the config watcher."
date: 2026-07-20
status: IMPLEMENTED
tags:
  - specification
  - config
  - setup
  - credentials
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Config write batching and provenance follow-ups

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   20 July 2026

Status
:   IMPLEMENTED — 20 July 2026

Related
:   [config v0.3.x migration](2026-07-20-config-v0.3-migration.md),
    [segregated default config](2026-07-20-segregated-default-config.md)

## Summary

The migration's four-angle simplify review converged on one wrong-altitude
pattern: GTB re-deriving, per call site, capabilities go/config v0.4.0 already
owns — batched atomic writes, routed write targets, layered validation. These
follow-ups move each onto the module mechanism. Two carry deliberate
user-visible changes, called out below.

## Decisions

### D1 — `setup.Editor` gains `Apply`; `Set` stays as the single-key convenience

```go
type Editor interface {
	View() *config.View
	Set(key string, value any) error
	Apply(changes ...config.Change) error
}
```

`storeEditor.Apply` forwards to the store's variadic `Apply` with the editor's
contained context. This is the enabler for D2: a wizard's multi-key outcome
commits as **one** transactional file write instead of one write per key, and
an interrupt mid-switch can no longer leave a half-migrated credential block.

### D2 — One exclusive-credential writer, shared by the three wizards

```go
// WriteExclusive commits the winning keys and removes every other listed key
// in one transactional Apply — the single-credential-key invariant, enforced
// atomically.
func WriteExclusive(e Editor, winning map[string]any, all []string) error
```

github, ai and bitbucket's per-mode writers stage their winning key(s) plus
the removal of every stale sibling through it. Per-feature variation (OAuth
capture, the key triple, the dual-credential JSON blob) stays per-feature —
only the invariant write converges. Change ordering inside the Apply is
interleaved per winning key: a stale key on the same dotted path as a winner
(`bitbucket.username` vs `bitbucket.username.env` — bitbucket's family nests)
is removed immediately before that winner is set, so the set never writes
through a scalar and no later subtree removal can take the winner with it;
every *independent* stale key is removed after all sets, so a shared parent
mapping never passes through an empty state mid-batch (the migrate lesson).
For flat sibling families like github's this degrades to exactly
sets-then-removes.

Two deliberate behaviour changes ride on this:

1. **Stale keys are REMOVED, not blanked to `""`.** Blanking was the
   `credentials.ClearKeysExcept` contract because "Viper has no unset
   primitive" — a rationale the store made obsolete (`config.Remove` exists).
   User files stop accumulating `value: ""` residue. Resolution and doctor
   already treat empty and absent identically, so nothing else moves.
2. **bitbucket now clears stale keys at all.** It never did — switching
   storage mode left the previous credential (or a literal secret) in the
   file. The invariant the other two wizards enforce now has a single home,
   which is exactly why the gap existed.

`setup.ClearCredentialKeysExcept` and its `editorKeyWriter` shim are deleted:
their only purpose was adapting the error-less viper-era `KeyWriter`, and no
caller remains. The module's `credentials.ClearKeysExcept` stays published for
viper-era downstreams.

### D3 — `unset` validates the layered candidate, not the bare file

The bare-file candidate check refused removing any key from a file that does
not itself set `log.level` — even though the defaults layer supplies it, so
the resolved configuration would remain valid. The candidate is now the
file-after-removal **layered over the merged embedded defaults**
(`setup.AssetSource`), matching what `config validate` checks.

**User-visible:** `config unset log.level` now succeeds — the resolved value
falls back to the shipped default. Refusal protected nothing once defaults
always apply; the e2e scenario flips to pin the fallback. An unset whose
result is invalid even over the defaults is still refused with the file
untouched.

Wiring the schema into the store itself (`config.WithSchema`, so every Apply
and reload validates) is the eventual deeper mechanism; it widens behaviour
across set/reload and is deferred until wanted on its own merits.

### D4 — The writable target comes from `Store.Plan`

`config path --writable` documents itself as "where set/unset/edit write",
but derived that answer from the last *loaded* file layer — which disagrees
with Apply's actual routing when the config file is declared but not yet
created. `resolveWritableConfigPath` now asks the store: `Plan` a probe `Set`
and read `Operations[0].Target.Name` — "exactly the routing the write will
use, rather than an approximation of it". The default-config-dir fallback
remains for a store with no writable layer.

### D5 — Watcher gating: **rejected**

The review suggested starting `Store.Watch` only for long-running commands.
Rejected: the cost on a short-lived process is one fd and a goroutine, while
an opt-in annotation reintroduces exactly the silent-no-reload failure D6 of
the migration spec exists to prevent — a downstream serve command that forgets
the annotation believes it hot-reloads and never does. Always-on stays.

### D6 — Upstream candidates, recorded not implemented

`Snapshot.FileUsed()`/`Files()` and a `View.DefinedBy(path, kinds...)`
provenance fold belong in go/config eventually; neither justifies a module
release alone. GTB's `props.ConfigFileSources` and `pkg/cmd/config`'s
predicates are the interim homes.

### D7 — Warn (and, interactively, confirm) a credential write to a project-local file

A later code review found that go/config routes a *new* key to the
highest-precedence writable file, which is the project-local `.<tool>.yaml`
when one exists in the working-directory tree. So `config set anthropic.api.key
sk-…` run inside a repo carrying a committed `.<tool>.yaml` writes the
plaintext secret into that committable file rather than the user's private
config.

The write is **not blocked** — a project-local secret can be deliberate, and
the project-local layer is an intended feature (a `.editorconfig`-style repo
convention). Instead `config set` warns when it recognises a credential landing
in a project-local file, and — when the session is interactive — asks the user
to confirm; a decline aborts with the file untouched. A non-interactive session
cannot be prompted, so it proceeds after the warning (CI and scripts are not
held hostage to a TTY).

Recognition uses two independent signals: the value matches a known credential
shape (`redact.String(value) != value` — the `sk-`/`ghp_`/`AIza`/`glpat-`
prefixes, JWTs, and long opaque tokens the redact package already catches), or
the key is one the migrate catalogue recognises as a literal-credential slot.
Reference-mode keys (`.env`, `.keychain`) hold an env-var name or keychain
locator, not a secret, so neither signal fires for them — the recommended
storage modes never trip the warning. "Project-local" is detected by the file's
base name (`.<tool>.yaml`), which the global `config.yaml` never shares.

The interactivity check reads the *command's* input stream (os.Stdin by
default), so a test can force the non-interactive branch with `cmd.SetIn`. The
interactive huh confirmation itself (reached only under a real TTY) is the
established `config migrate` confirm pattern and is not exercised by the
automated suite. `config edit` shares the routing root cause but opens the
whole file in an editor rather than taking a single key+value, so the
key-level warning does not apply there.

## Testing

- WriteExclusive: winning-key survival, sibling removal (absent, not blank),
  single-Apply atomicity, and the shared-parent case (`bitbucket.username` +
  `bitbucket.username.env` under one mapping) that broke migrate's editor.
- Each wizard's mode-switch test asserts stale keys are gone from the written
  file; bitbucket gains the clearing coverage it never had.
- sensitive-write guard: the recogniser (known keys, token-shaped values, and
  the `.env`/`.keychain` negatives), the project-local base-name detection, and
  the command flow — a credential to a project-local file warns and (non-
  interactively) proceeds; an ordinary key does not warn; a credential to the
  global config does not warn.
- unset: a defaulted key unsets successfully and resolves to the default; an
  unset invalid even over defaults is still refused; the env-only refusal is
  unchanged.
- `config path --writable` on a declared-but-missing file reports the declared
  path (where Apply will actually write), not the default fallback.
