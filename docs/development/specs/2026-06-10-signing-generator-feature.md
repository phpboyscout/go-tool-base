---
title: "Generator signing support — `gtb enable signing`, scaffold `internal/trustkeys`, wire `props.Signing`"
description: "Teach the generator to set up consumer-side release-signing verification without hand-editing generated code. Signing is disabled by default (it needs real prep — a key, KMS, WKD — so a quick `generate project` shouldn't carry it); enabling is an active choice via a new `gtb enable signing` command (and a `--signing` flag plus an interactive prompt on `generate project`). When enabled it scaffolds an `internal/trustkeys` embed package, templates the `Signing: props.SigningConfig{...}` block into the generated root command, and emits a manifest-driven `signing.go` for the enforcement defaults. `gtb enable signing` updates `.gtb/manifest.yaml` and selectively regenerates only the affected files. Minting (`gtb keys mint`) and WKD publishing (`gtb keys wkd`) are out of scope — this is only the embed-and-wire half."
status: DRAFT
date: 2026-06-10
tags:
  - specification
  - generator
  - signing
  - self-update
  - openpgp
  - scaffolding
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Generator signing support — `gtb enable signing`, scaffold `internal/trustkeys`, wire `props.Signing`

Authors
:   Matt Cockayne

Date
:   10 June 2026

Status
:   DRAFT

---

## Summary

Phase 2 self-update signature verification reads its embedded trust anchor from
`props.Tool.Signing.EmbeddedKeys` (`pkg/props/tool.go`), populated by an
`internal/trustkeys` package that `//go:embed`s the release public key(s). `gtb`
itself wires this by hand in `internal/cmd/root/root.go` and keeps its enforcement
defaults in a hand-written `internal/cmd/root/signing.go`, deliberately separate from
`root.go` "so the scaffolding generator (which templates root.go) doesn't ship these
values into downstream tools that have their own signing posture."

A *generated* tool gets none of this, and the only way to add it today is to hand-edit
the generated root command and hand-create the `internal/trustkeys` package, which is
exactly the regen-owned code the generator exists to keep people out of.

This spec adds first-class generator support. Signing is **disabled by default** —
turning it on is a deliberate `gtb enable signing` (or a `--signing` flag / interactive
prompt on `generate project`). Once enabled, the package, the `Signing:` wiring, and a
manifest-driven `signing.go` are scaffolded and regenerated like any other generated
code, never hand-edited. This is the framework half of the "Sign your own binaries"
tutorial's embed-and-verify step; the tutorial is blocked on it.

## Motivation

The "Sign your own binaries with go-tool-base" tutorial reached the step where a
consumer embeds their minted public key and turns verification on, and the only way to
express it was "edit the `props.Props` your generator produced." That is wrong: the
root command is templated by `internal/generator/templates/skeleton_root.go` and is
subject to regeneration and hash-protection, so a hand-edit either gets clobbered on
the next `gtb regenerate` or trips the conflict guard. Telemetry, the closest existing
capability, is never hand-edited — the generator templates the `Telemetry:` block
conditionally from manifest values. Signing should work the same way.

Signing is **off by default** because it carries real prerequisites: a signing key
(ideally in a KMS), a published WKD endpoint, and a release pipeline wired to sign.
Someone scaffolding a quick CLI shouldn't be handed any of that machinery they didn't
ask for. Enabling it is therefore an explicit, active choice.

## Design overview

When signing is enabled, the generator does three things:

1. **Scaffolds `internal/trustkeys/`** — a `trustkeys.go` (`//go:embed all:keys` + a
   `Keys() [][]byte` accessor, mirroring the existing `internal/trustkeys/trustkeys.go`)
   and a `keys/.gitkeep` so the directory exists in git while empty. The author later
   drops their minted `*.asc` files into `keys/`. The package is generated and
   regen-protected; keys are added as files, never by editing the `.go`.
2. **Templates the `Signing:` block** into the generated root command's `props.Tool{}`,
   exactly the way Telemetry is templated:
   `Signing: props.SigningConfig{EmbeddedKeys: trustkeys.Keys()}`.
3. **Generates a `signing.go`** in the generated root-command package — an `init()` that
   sets the `pkg/setup` enforcement defaults (`DefaultExternalKeyEmail`,
   `DefaultRequireSignature`, `DefaultKeySource`, `DefaultRequireExternalCrosscheck`)
   from manifest values. This mirrors `gtb`'s own `internal/cmd/root/signing.go` split,
   but is generated from the manifest rather than hand-written, so the author tunes
   posture by changing the manifest (via `gtb enable signing` flags), not by editing
   code.

All three are driven by a `signing` block in `.gtb/manifest.yaml`, so regeneration
reproduces them deterministically. The whole thing is gated on `signing.enabled`, which
defaults to `false`.

## What gets scaffolded

When `signing.enabled` is true, the generated file set gains:

| Path | Generated? | Owner | Notes |
|---|---|---|---|
| `internal/trustkeys/trustkeys.go` | yes (templated) | generator | `//go:embed all:keys`, `Keys() [][]byte`. Regen-protected. |
| `internal/trustkeys/keys/.gitkeep` | yes | generator | keeps the dir in git; `all:` embed needs a file to match. |
| `internal/trustkeys/keys/*.asc` | **no** | author | the author adds minted keys here (from `gtb keys mint`). |
| root command `props.Tool{}` `Signing:` field | yes (templated) | generator | `props.SigningConfig{EmbeddedKeys: trustkeys.Keys()}`. |
| root-command-package `signing.go` | yes (templated) | generator | `init()` setting the `setup.Default*` enforcement vars from the manifest. |

The `Keys()` accessor returns `nil` when no `*.asc` is present, and
`props.SigningConfig{}`'s zero value is safe, so a tool that has *enabled* signing but
not yet added a key still builds and runs with verification dormant. Enabling the
feature is harmless on its own; nothing breaks before there's a key.

## Manifest representation

Add a `Signing` block to `ManifestProperties` (`internal/generator/manifest.go`),
parallel to the existing `Telemetry` block:

```yaml
# .gtb/manifest.yaml
properties:
  # ...
  signing:
    enabled: true                             # default false; set by `gtb enable signing`
    external_key_email: "release@acme.dev"    # derives the WKD URL; enables the WKD leg
    require_signature: false                  # N+1 rollout: stays false until the key has shipped
    key_source: "both"                        # embedded | external | both (default both)
    require_external_crosscheck: false        # fail closed if WKD is unreachable
```

```go
type ManifestSigning struct {
    Enabled                   bool   `yaml:"enabled,omitempty"`
    ExternalKeyEmail          string `yaml:"external_key_email,omitempty"`
    RequireSignature          bool   `yaml:"require_signature,omitempty"`
    KeySource                 string `yaml:"key_source,omitempty"`
    RequireExternalCrosscheck bool   `yaml:"require_external_crosscheck,omitempty"`
}
```

`signing.enabled` defaults to `false`: a project with no `signing:` block (every project
today) scaffolds nothing signing-related. Once enabled, empty values for the rest map to
the framework defaults (`key_source` → `"both"`, `require_signature` → `false`).

## `gtb enable signing` (and `gtb disable signing`)

Enabling signing on a project is its own verb, not a `generate` subcommand: `generate`
creates new user-facing surface (commands, flags, config fields), whereas this toggles a
capability and rewrites existing wiring. A new `gtb enable` command group hosts it (and
leaves room for `gtb enable <other-capability>` later):

```sh
gtb enable signing \
    --email release@acme.dev            # external_key_email (recommended; enables the WKD leg)
    --key-source both                   # embedded | external | both (default both)
    # --require-signature               # default off; flip later, see rollout
    # --require-external-crosscheck     # default off
```

`gtb enable signing`:

1. Sets `properties.signing.enabled = true` and the supplied options in
   `.gtb/manifest.yaml`.
2. **Selectively regenerates only the signing-affected files** — it writes
   `internal/trustkeys/trustkeys.go`, `internal/trustkeys/keys/.gitkeep` and the
   generated `signing.go`, and re-renders the one generated root-command file to add the
   `Signing:` field. It does **not** run a full `regenerate project`; unrelated files are
   left untouched. The same hash-protection applies to the files it does touch.
3. Is idempotent: run again with new flags and it updates the manifest block and
   re-renders just those files.

`gtb disable signing` is the inverse: sets `signing.enabled = false` and selectively
regenerates to drop the `Signing:` field and the generated `signing.go`. It leaves the
`internal/trustkeys` directory and any author-added `*.asc` in place (the generator never
deletes author content) and says so in its output.

Selective regeneration is a small addition to the regenerate machinery: a way to
re-render a named subset of generated files rather than the whole tree. If that proves
awkward, the fallback is to run the existing full `regenerate project` after the manifest
edit, but the intent is to touch only what signing owns.

## Collecting signing configuration

Configuration (`external_key_email`, `key_source`, `require_signature`,
`require_external_crosscheck`) must be collectable both non-interactively (flags) and
interactively, from both entry points:

- **`gtb enable signing`** — flags as above. When a flag is omitted and the terminal is
  interactive, prompt for it (at minimum the WKD email); in `--ci` / non-interactive mode,
  omitted flags take their defaults and nothing prompts.
- **`gtb generate project`** — gains a `--signing` flag (off by default) plus
  `--signing-email`, `--signing-key-source`, etc., and the **interactive wizard** gains a
  signing step: an "Enable release signing?" prompt (default No), and when yes, follow-up
  prompts for the WKD email and key source. The wizard never prompts for
  `require_signature` (it must stay false until a key has shipped — see rollout); that is
  only ever flipped later via `gtb enable signing --require-signature`.

Both paths write the same `properties.signing` manifest block, so a project created with
`--signing` and one enabled later via `gtb enable signing` are identical.

## The N+1 rollout and `require_signature`

`require_signature` defaults to `false` and stays there until the embedded key has
shipped in a prior release (the N+1 / N+2 / N+3 discipline in
`docs/development/phase2-signing-prep.md`). The author flips it by re-running
`gtb enable signing --require-signature` once a signed release is out. Because the value
lives in the manifest and is templated into `signing.go`, the flip is a tracked,
regenerable change rather than a hand-edit of generated code, which is the point of this
spec.

## Why a config block, not a `FeatureCmd`

The `FeatureCmd` constants (`update`, `init`, `mcp`, `docs`, `doctor`, `config`,
`changelog`, `ai`, `telemetry`) are *command groups* a tool exposes to its end users.
Signing exposes no command to a tool's users — it is release-engineering wiring the
*author* sets up, the same kind of thing as Telemetry config, which is also not a
`FeatureCmd`. Modelling signing as a manifest config block keeps it off the
end-user-facing feature surface and avoids implying a `mytool signing` command that
should not exist. (Telemetry is also off by default, for consent reasons; signing is off
by default for a different reason — the prerequisite setup — but the config-block shape
is the same.)

## Regeneration, hashes and user-owned files

- `trustkeys.go`, the `Signing:` field and `signing.go` are generated and participate in
  the manifest hash set (`internal/cmd/regenerate/regenerate.go`), so a later
  `gtb regenerate project` keeps them in sync and the conflict guard catches manual
  edits, same as any scaffolded file.
- `internal/trustkeys/keys/*.asc` are author-owned content, never templated or hashed.
  The `.gitkeep` is generated once.
- `gtb disable signing` removes the generated `Signing:` field and `signing.go` but never
  deletes the `internal/trustkeys` directory or author-added `*.asc`.

## Testing

- **E2E (mirrors the scaffold-and-build tests):** `gtb generate project --signing
  --signing-email release@example.test`, then `go build ./...` succeeds, the
  `internal/trustkeys` package exists, and the generated root command contains the
  `Signing:` wiring and a `signing.go` setting the expected defaults.
- **Enable on existing:** scaffold a plain project (no signing), run `gtb enable signing
  --email release@example.test`, assert the manifest gains the signing block, only the
  signing-affected files change (diff is scoped), and `go build` passes.
- **Disable:** `gtb disable signing` drops the `Signing:` field and `signing.go`, leaves
  `internal/trustkeys` + any `*.asc`, and `go build` passes.
- **Dormant-safe:** signing enabled but no `*.asc` present → `trustkeys.Keys()` returns
  `nil`, binary builds and runs (verification dormant).
- **Interactive wizard:** the `generate project` wizard's "Enable release signing?" path
  collects the email and produces the same manifest block as the flag path.
- **Selective-regen scope:** `gtb enable signing` touches only the signing files; an
  unrelated generated file's hash is unchanged.
- **Regenerate idempotency / round-trip:** `gtb regenerate project` twice is a no-op; the
  manifest `Signing` block round-trips through read/write and `ast_extract`.

## Out of scope

- **Minting and key generation** — `gtb keys mint` / `gtb keys generate` (already shipped).
- **WKD publishing** — `gtb keys wkd` (already shipped).
- **KMS / OIDC infrastructure** — the public `terraform-aws-signing-kms` module.
- **Signing releases in CI** — the GoReleaser `signs:` block and the signing shim, owned
  by the tool author.

This feature is solely the generated tool's *embed-and-verify* wiring.

## References

- `pkg/props/tool.go` — `SigningConfig.EmbeddedKeys`, the `FeatureCmd` set.
- `internal/trustkeys/trustkeys.go` — the package shape to mirror.
- `internal/cmd/root/root.go`, `internal/cmd/root/signing.go` — gtb's own (hand-written)
  reference wiring, and the comment on why it is kept out of the templated root.
- `internal/generator/templates/skeleton_root.go:140-156` — the Telemetry conditional to
  mirror for `Signing:`.
- `internal/generator/skeleton.go:417` — the keychain conditional scaffold to mirror (the
  emission mechanism; signing's default is off rather than on).
- `internal/generator/manifest.go` — `ManifestProperties`/`ManifestTelemetry`, where
  `ManifestSigning` slots in.
- `internal/cmd/generate/` — the `generate project` wizard + flags to extend with signing.
- `internal/cmd/regenerate/` — the regenerate pipeline and hash protection; the basis for
  selective regeneration.
- `docs/development/phase2-signing-prep.md` — the N+1 rollout discipline.
- `docs/components/setup/signature-verification.md` — the resolver chain and log output.
