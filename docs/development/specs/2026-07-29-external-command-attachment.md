---
title: "External Command Attachment Specification"
description: "A first-class, regeneration-safe mechanism for a gtb-generated project to attach whole Cobra command trees from an external module onto its root, via two channels — a manifest-declared vocabulary render and a user-owned adapter escape-hatch — replacing the cmd/<tool>/main.go + .gtb/ignore workaround."
date: 2026-07-29
status: IN PROGRESS
tags:
  - specification
  - generator
  - manifest
  - commands
  - root
  - external-module
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# External Command Attachment Specification

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   29 July 2026

Status
:   IN PROGRESS

Tracking
:   GitLab issue #8 — "a regeneration-safe mechanism to attach custom/external commands to a generated project's root"

---

## 1. Problem statement

A gtb-generated CLI often needs to attach commands the generator does **not**
itself produce — in particular, whole Cobra command trees provided by an
**external module** (a separate Go module), which are not created via
`gtb generate command` and whose implementation lives elsewhere.

Today the only wiring point is the generated root command, `pkg/cmd/root/cmd.go`,
which the generator **owns and re-renders**. `regenerateRootCommand`
(`internal/generator/regenerate.go:411`) unconditionally overwrites that file
from the manifest on every `gtb regenerate`, every `gtb enable/disable
<feature>`, and every `gtb enable signing` — and it does **not** consult
`.gtb/ignore` (verified: `IsIgnored` is called only for skeleton assets, docs,
overlays and the goreleaser splice, never for the generated Go command tree).
This forces the author into a lose/lose choice:

- **Leave the root generator-managed** → the customisation is re-rendered away.
- **Add `pkg/cmd/root/cmd.go` to `.gtb/ignore`** → `.gtb/ignore` has no effect on
  it anyway (the root render never checks ignore rules), *and* even if it did,
  the root would stop receiving generator improvements (feature-flag wiring, new
  middleware, structural changes).

So there is currently **no safe place** to attach external commands to a
generated project's root.

### 1.1 The concrete motivating example (the workaround this replaces)

`sigillum` (`gitlab.com/phpboyscout/sigillum`) is a gtb-generated standalone CLI
whose entire purpose is to expose the `sign` and `keys` command tree from the
external `gitlab.com/phpboyscout/go/signing-cli` module as top-level commands.
These are complete Cobra command builders — with their own subcommands and flags
— from another module, not `gtb generate command` output, so there is no
generator-native place to declare them.

The current workaround attaches them in `cmd/sigillum/main.go`, **after** the
generated root is constructed, and ignores *that* file instead of the root:

```go
// cmd/sigillum/main.go — hand-edited, listed in .gtb/ignore
func main() {
    rootCmd, p := root.NewCmdRoot(version.Get())

    rootCmd.Register(
        setup.Wrap("", signingcli.NewCmdSign(p.GetLogger())),
        setup.Wrap("", signingcli.NewCmdKeys(p.GetLogger())),
    )

    gtbRoot.Execute(rootCmd, p)
}
```

It works, but it is a hand-rolled convention resting on `.gtb/ignore`, not a
supported mechanism. It also required `gtb disable init` and a hardened
`.gtb/ignore` to keep `regenerate` conflict-free. This spec replaces that
workaround with a declared, generator-managed entity.

### 1.2 Why the external call shape is the crux

Local subcommands and external command trees do **not** share a constructor
signature, and this is the central design constraint:

| | Local (`gtb generate command`) | External (`go/signing-cli`) |
|---|---|---|
| Constructor | `NewCmdFoo(p *props.Props) *setup.Command` | `NewCmdSign(l Logger) *cobra.Command` |
| Return type | `*setup.Command` (attach directly) | `*cobra.Command` (needs `setup.Wrap`) |
| Dependency | the whole `*props.Props` container | a **narrow** structural seam (here `Logger`) |
| Rendered call | `foo.NewCmdFoo(p)` | `setup.Wrap("", signingcli.NewCmdSign(p.GetLogger()))` |

An external module is deliberately **props-decoupled** — it cannot import GTB
(`go/signing-cli` depends only on cobra + `go/signing`, which is exactly why
there is no module cycle). So the mechanism must be able to render a call that:

- targets a symbol in an arbitrary module/import path;
- passes it **narrowed dependencies derived from `p`** (a `Logger`, and
  plausibly `Config`/`FS`/nothing), not `p` itself;
- wraps a `*cobra.Command` return in `setup.Wrap("", …)` (or attaches a
  `*setup.Command` return directly);
- and routes the result through the framework's middleware pipeline
  (`setup.Command.Register` → `Chain`, `pkg/setup/command.go:155`), identically
  to a local subcommand.

## 2. Goals and non-goals

### 2.1 Goals

1. A **first-class, generator-managed** external-command attachment entity — no
   hand-edited `main.go`, no `.gtb/ignore` hack.
2. Attachments **survive** `gtb regenerate project`, `gtb regenerate command`,
   and every targeted command that re-renders the root (`enable`/`disable`
   `<feature>`, `enable`/`disable signing`), without being clobbered.
3. The author **forfeits no generator-managed updates** to the root command
   itself — the root stays fully generator-owned; attachments are re-rendered
   into it, exactly like local subcommands and the `Signing:` block.
4. Works for **whole external Cobra command trees** (their own subcommands and
   flags), and for **multiple constructors** from one module (sign *and* keys).
5. Attached commands **pick up the framework's global middleware** (the
   `setup.Command` / feature pipeline), identically to local subcommands.
6. The generator **manages the `go.mod` require** for the declarative channel
   (add on attach at an explicit pinned version, prune on detach).
7. Two channels, so the common case needs zero user code and the exotic case is
   still supported (see §3):
   - **declarative** — manifest-declared, vocabulary-rendered (the 90% path);
   - **adapter** — a user-owned, generator-scaffolded escape-hatch for any shape
     the vocabulary cannot express.
8. A CLI surface (`gtb attach …` / `gtb detach …`) consistent with
   `gtb generate command` and `gtb enable`.

### 2.2 Non-goals

1. **Not** a plugin system: attachment is compile-time (a Go `require` + a
   rendered call), never a runtime shared-object load.
2. **No runtime feature-gating of declarative attachments in v1.** Declarative
   attachments are always-on (`setup.Wrap("", …)`). Gating is *deliberately*
   deferred (§8.4) rather than opening the curated feature catalogue to
   open-ended custom feature names with no current consumer. An author needing a
   gate uses the adapter channel and self-gates. See §8.4 for the rationale and
   the future-work path.
3. **Not** responsible for the external module's own quality, versioning, or API
   stability. GTB records a pin and renders a call.
4. **Does not** change how *local* `gtb generate command` output is wired.
5. **No source fetch / symbol-existence check at attach time.** `go mod tidy` +
   the compiler, run inside the atomic staged-FS regenerate, are the
   verification (§6). A `doctor` check is future work (§8.5).

## 3. Design overview

The manifest is the single source of truth. All attachment state lives under
`properties.external_commands`, modelled on the existing `TemplateSource`
provenance+pin pattern (`manifest.go:422`): a declarative record carrying no
behaviour, riding the existing `DecodeManifestFile` / `marshalManifestBytes`
plumbing for byte-stable serialisation. Two attachment channels share that
block.

### 3.1 Channel 1 — declarative (the 90% path)

Each declarative entry names an external module, a pinned version, an import
path, and one or more constructor calls to render. The generator:

1. **renders the attach calls into `pkg/cmd/root/cmd.go`** via the skeleton-root
   template — re-emitted on *every* root render, so `enable signing` /
   `regenerate` cannot drop them;
2. **adds the `require`** to the project `go.mod` at the explicit version;
3. needs **no** `.gtb/ignore` entry and leaves `cmd/<tool>/main.go` pristine.

The call shape is rendered from a **closed injection vocabulary** — a small,
fixed set of argument tokens that each map to a well-known expression derived
from `p`:

| Token | Renders to |
|-------|-----------|
| `logger` | `p.GetLogger()` |
| `props` | `p` |
| `config` | `p.Config` |
| `fs` | `p.FS` |
| `version` | `p.Version` |
| *(none)* | `()` — zero-arg constructor |

Each constructor entry lists its argument tokens in order and a `wrap` flag:
`wrap: true` when the constructor returns `*cobra.Command` (render
`setup.Wrap("", …)`); `wrap: false` when it returns `*setup.Command` (attach
directly). This covers the sigillum case exactly
(`NewCmdSign`, `args: [logger]`, `wrap: true`) and the props-style case
(`args: [props]`, `wrap: false`) with **no user-authored glue**.

The vocabulary is deliberately closed rather than a free-form Go expression: it
keeps the generated root type-safe and reviewable and prevents the manifest from
becoming an arbitrary-code injection vector (consistent with the generator's
template-security posture, `validate.go` + `template_escape.go`). Anything the
vocabulary cannot express uses Channel 2.

### 3.2 Channel 2 — adapter (escape hatch)

For constructor shapes the vocabulary cannot express (extra dependencies, custom
wrapping, per-command runtime gating, conditional assembly), the generator
scaffolds — **once** — a user-owned adapter:

```go
// pkg/cmd/external/attach.go — scaffolded once, then author-owned.
package external

import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Commands returns external command trees to attach to the root. Author-owned:
// build/wrap arbitrary constructors here. Return already-wrapped *setup.Command
// values (use setup.Wrap for *cobra.Command builders). Self-gate here if needed.
func Commands(p *props.Props) []*setup.Command {
    return nil
}
```

The generated root renders **one fixed call** — `external.Commands(p)` spread
into the subcommand slice — recorded by a manifest boolean
(`external_commands_adapter: true`) so the root knows to emit it. The adapter
file is preserved across regeneration by the **same seed-file / preserve-if-
exists mechanism `main.go` already uses** (`files.go` `handleExecutionFile`):
scaffolded when absent, never overwritten when present, its on-disk hash tracked
for drift.

**Channel boundary.** The generator owns the *require* and the *call render* for
Channel 1; for Channel 2 the author owns the adapter body **and its imports**
(`go mod tidy` in post-processing resolves adapter-introduced requires). Adapter
commands are therefore **self-gated and self-versioned** — the escape hatch
trades generator management for unlimited flexibility. The two channels coexist
in one project: declarative for clean cases, adapter for the rest.

### 3.3 Where the calls are rendered

The generated root's `NewCmdRoot(v)` builds `p`, then calls
`gtbRoot.NewCmdRoot(p, <subcommands…>)` (`skeleton_root.go:96-103`;
`NewCmdRoot(props *props.Props, subcommands ...*setup.Command)`,
`pkg/cmd/root/root.go:756`). Both channels append `*setup.Command` values to that
same variadic call:

```go
// generated pkg/cmd/root/cmd.go (illustrative)
rootCmd := gtbRoot.NewCmdRoot(
    append([]*setup.Command{
        serve.NewCmdServe(p),                                  // local subcommand
        setup.Wrap("", signingcli.NewCmdSign(p.GetLogger())), // Channel 1 (declarative)
        setup.Wrap("", signingcli.NewCmdKeys(p.GetLogger())), // Channel 1 (declarative)
    }, external.Commands(p)...)...,                            // Channel 2 (adapter)
)
```

(When no adapter is declared, the `external.Commands(p)` spread and its import
are omitted; when there are no declarative attachments, only the local subs and
the optional adapter spread remain.) All args are `*setup.Command`, so the
framework applies middleware to every one uniformly via
`wrapped.Register(o.subcommands...)` (`root.go:857` → `setup.Command.Register`,
`command.go:155`) — behaviourally identical to sigillum's post-construction
`rootCmd.Register(...)`, but rendered into the DO-NOT-EDIT root instead of a
hand-edited `main.go`.

> **Design decision — render into the root, not `main.go`.** `main.go` is
> currently the only place the workaround can live *because* it is the one file
> the generator does not re-render. The fix makes attachments re-renderable
> (manifest-driven) so the root — the correct, middleware-applying home — is
> safe, and `main.go` returns to being generator-managed with zero
> customisation.

## 4. Public API and data model

### 4.1 Manifest schema (`internal/generator/manifest.go`)

```go
// ManifestProperties gains:
type ManifestProperties struct {
    // … existing fields …
    ExternalCommands        []ManifestExternalCommand `yaml:"external_commands,omitempty"`
    ExternalCommandsAdapter bool                      `yaml:"external_commands_adapter,omitempty"`
}

// ManifestExternalCommand declares one external module whose Cobra command
// builders are attached to the generated project's root (Channel 1). It carries
// a provenance pin (module + version) and the call descriptors the generator
// needs. It holds no behaviour, mirroring TemplateSource.
type ManifestExternalCommand struct {
    // Module is the Go module path providing the commands, e.g.
    // "gitlab.com/phpboyscout/go/signing-cli". Used for the go.mod require.
    Module string `yaml:"module"`
    // Version is the module version to require, e.g. "v0.1.0". Required
    // (explicit pin — no implicit latest resolution).
    Version string `yaml:"version"`
    // ImportPath is the package to import for the constructors. Defaults to
    // Module when empty (the signing-cli case: constructors in the module root).
    ImportPath string `yaml:"import_path,omitempty"`
    // Alias is the import alias for ImportPath in the generated root.
    Alias string `yaml:"alias,omitempty"`
    // Attach lists the constructor calls to render onto the root.
    Attach []ManifestExternalAttach `yaml:"attach"`
}

// ManifestExternalAttach describes a single constructor call to render.
type ManifestExternalAttach struct {
    // Constructor is the exported symbol to call, e.g. "NewCmdSign".
    Constructor string `yaml:"constructor"`
    // Args are injection tokens from the closed vocabulary (§3.1), rendered in
    // order. Empty means a zero-argument constructor.
    Args []string `yaml:"args,omitempty"`
    // Wrap is true when Constructor returns *cobra.Command (render
    // setup.Wrap("", …)); false when it returns *setup.Command (attach direct).
    Wrap bool `yaml:"wrap"`
    // Name, if set, is the expected top-level command name, used only for
    // best-effort collision detection (§4.5). It does not affect the render.
    Name string `yaml:"name,omitempty"`
}
```

> Note: there is **no** `feature` field — declarative attachments are un-gated
> in v1 (§2.2, §8.4). `wrap` is retained because it describes the constructor's
> *return type*, not gating.

Validation (extend `internal/generator/validate.go`):

- `Module` — a syntactically valid module path (non-empty, no whitespace/control
  chars; NFC-normalised, same discipline as other user fields).
- `Version` — required; must match a `vX.Y.Z[-…]` semver shape.
- `ImportPath` / `Alias` — valid Go import path / identifier.
- `Constructor` — a valid exported Go identifier.
- Every `Args` token must be a member of the closed vocabulary (§3.1); an
  unknown token is a hard validation error (fail loud).

### 4.2 Template layer (`internal/generator/templates/skeleton_root.go`)

```go
// SkeletonExternalCommand is the render-time view of one Channel-1 constructor.
type SkeletonExternalCommand struct {
    ImportPath  string   // package to Qual-import
    PkgAlias    string   // import alias
    Constructor string   // symbol to call
    Args        []string // pre-resolved call expressions, e.g. ["p.GetLogger()"]
    Wrap        bool     // wrap in setup.Wrap("", …)
}

// SkeletonRootData gains:
type SkeletonRootData struct {
    // … existing fields …
    ExternalCommands []SkeletonExternalCommand // Channel 1
    ExternalAdapter  bool                      // Channel 2 — emit external.Commands(p)
}
```

In `SkeletonRoot`, after the local-subcommand loop (`skeleton_root.go:96-99`),
append one arg per `ExternalCommands` entry (resolve `Args` tokens to jennifer
expressions, build `<alias>.<Constructor>(<args…>)`, wrap in `setup.Wrap("", …)`
when `Wrap`); and, when `ExternalAdapter`, spread `external.Commands(p)`. Add the
`setup`, module, and (if adapter) `pkg/cmd/external` imports via `f.ImportAlias`.

### 4.3 Generator API (`internal/generator/external.go`)

```go
// AttachExternalCommand records a Channel-1 attachment, adds the go.mod require,
// re-renders the root, and writes the manifest.
func (g *Generator) AttachExternalCommand(ctx context.Context, spec ExternalCommandSpec) error

// AttachExternalAdapter scaffolds pkg/cmd/external/attach.go (once), sets the
// adapter manifest flag, re-renders the root, and writes the manifest.
func (g *Generator) AttachExternalAdapter(ctx context.Context) error

// DetachExternalCommand removes a Channel-1 attachment by module path,
// re-renders the root, prunes the require if now unused, and writes the manifest.
func (g *Generator) DetachExternalCommand(ctx context.Context, module string) error

// ListExternalCommands returns the declared attachments (both channels).
func (g *Generator) ListExternalCommands() ([]ManifestExternalCommand, bool, error)
```

`AttachExternalCommand` follows the exact shape of `applySigningPosture`
(`signing.go:200`): mutate the manifest block → `regenerateRootCommand(*m)` →
manage go.mod → `writeManifest(m)` → `runSkeletonPostProcessing` (`go mod tidy` +
lint-fix + hash refresh), all inside the atomic staged FS. Because it routes
through the same `regenerateRootCommand` that `enable signing` and `regenerate`
use, the attachments are rendered by precisely that code path — which is *why*
they survive.

### 4.4 CLI surface (`internal/cmd/attach/`, `internal/cmd/detach/`)

```text
gtb attach command <module>@<version> \
    --constructor NewCmdSign --arg logger --wrap \
    --constructor NewCmdKeys --arg logger --wrap \
    [--import-path <path>] [--alias <alias>] [--name <cmd-name>]

gtb attach adapter          # scaffold pkg/cmd/external/attach.go + wire the root
gtb attach list             # show declared attachments (both channels)
gtb detach command <module> # remove a Channel-1 attachment
```

`attach`/`detach` avoid overloading `generate` (scaffold new code here) and
`enable` (toggle a built-in feature). The distinct `attach adapter` verb keeps
the two channels visibly separate. Each verb is a thin cobra command that builds
the spec and calls the matching generator method.

### 4.5 Collision detection

The generator cannot statically see an external command's runtime `Use` name, so
collision detection is **best-effort**:

- Always reject a duplicate `(module, constructor)` declaration.
- When an attachment declares `--name`, check it against local top-level command
  names and other declared attachment names, reusing the approach from
  `2026-07-28-generate-subcommands-conflict-detection.md` where feasible.
- Without `--name`, the compiler (duplicate imports) and cobra runtime (a real
  duplicate command name panics/errors at registration) are the backstop.

## 5. Generator impact

| Area | Change |
|------|--------|
| `manifest.go` | `ManifestExternalCommand` / `ManifestExternalAttach` types; `ExternalCommands` + `ExternalCommandsAdapter` on `ManifestProperties`. |
| `validate.go` | Validate module path, required version, import path, alias, constructor identifiers, and the closed `Args` vocabulary. |
| `templates/skeleton_root.go` | `SkeletonExternalCommand`; render loop appends Channel-1 args and the optional `external.Commands(p)` spread to `gtbRoot.NewCmdRoot(...)`; add `setup` / module / external imports. |
| `regenerate.go` | `buildSkeletonRootData` (`:380`) populates `ExternalCommands` (resolving `Args` tokens) + `ExternalAdapter` from the manifest. No change to the "always re-render root" behaviour — the property we rely on. |
| new `external.go` (generator) | `AttachExternalCommand` / `AttachExternalAdapter` / `DetachExternalCommand` / `ListExternalCommands`; go.mod require add/prune; adapter scaffold-once via the seed-file path. |
| new `assets/skeleton/pkg/cmd/external/attach.go.tmpl` | The adapter seed file (scaffolded once, preserve-if-exists). |
| `internal/cmd/attach/`, `internal/cmd/detach/` | New CLI command packages, registered in the internal command wiring. |
| go.mod management | `require <module> <version>` on attach; prune-then-tidy on detach. Adapter-introduced requires are left to `go mod tidy`. |

**Interaction with `.gtb/ignore`:** none required — the attachment lives in the
manifest (Channel 1) or in a seed-file-preserved adapter (Channel 2), so no file
needs ignoring. sigillum can then remove `cmd/sigillum/main.go` from
`.gtb/ignore` and delete its hand-edit.

**Interaction with the `enable signing` clobber (#4 /
`2026-07-28-enable-signing-respects-ignore`):** this spec *dissolves* that class
of problem for command attachment — because attachments are re-rendered from the
manifest, `enable signing` re-rendering the root re-emits them instead of
dropping them. The two specs are complementary: #4 protects *ignored skeleton
assets*; this makes *external command wiring* generator-managed rather than
ignored.

## 6. Error handling

`github.com/cockroachdb/errors` throughout, with `WithHint`/`WithHintf` (per
`docs/development/error-handling.md`).

- **Unknown injection token** → validation error naming the token and listing
  the valid vocabulary; fail before any write.
- **Missing `@version`** → error with a hint to pass an explicit pinned version
  (no implicit latest resolution in v1).
- **go.mod require conflict** (module already required at a different version) →
  surface both versions and require an explicit decision; never silently
  up/downgrade.
- **Detach of an unknown module** → clear "no such attachment" error listing the
  declared modules.
- **Post-render `go mod tidy` / build failure** (module or version does not
  exist, constructor symbol missing) → the atomic staged FS (`newStagedFS` →
  `materialise`) must **not** commit a broken tree; report the error and leave
  the project untouched. This is the primary correctness backstop given symbol
  existence is not statically checked (§2.2).

## 7. Testing strategy

TDD per phase (§11).

### 7.1 Unit tests (generator)

- **Manifest round-trip:** encode→decode of `ExternalCommands` +
  `ExternalCommandsAdapter` is byte-stable and deterministically ordered.
- **Validation:** each field valid/invalid; unknown `Args` token rejected;
  required-version enforced; module-path/identifier rules; duplicate
  `(module, constructor)` rejected.
- **Channel-1 render (golden files):** the sigillum manifest renders exactly the
  two `setup.Wrap("", signingcli.NewCmd*(p.GetLogger()))` lines with correct
  imports; props-style (`args: [props], wrap: false`) → `foo.NewCmdFoo(p)`;
  zero-arg → `x.NewCmdX()`.
- **Channel-2 render:** with the adapter flag set, the root spreads
  `external.Commands(p)` and imports `pkg/cmd/external`; the adapter seed file is
  scaffolded when absent and left byte-for-byte untouched when present.
- **`Attach*/Detach*`:** on a scaffolded project (real `afero.OsFs`), attach →
  root wiring + go.mod require present; detach → wiring gone + require pruned;
  adapter attach → seed file + root spread + manifest flag.
- **Regeneration safety (the headline guarantee):** attach (both channels), then
  run `RegenerateProject`, `ApplyFeatures` (toggle an unrelated feature), and
  `EnableSigning` — assert the wiring and the adapter spread survive every one
  byte-for-byte. This is the test that would have caught the original sigillum
  clobber.
- **≥90% coverage** for the new generator code (held to the `pkg/` bar per the
  coverage broken-window lessons).

### 7.2 E2E BDD (Godog) — required

Per the repo rule (new CLI commands / workflows **must** include Gherkin) and the
suitability assessment in `2026-03-28-godog-bdd-strategy.md`, this is a CLI +
multi-step generator workflow — in scope. Add
`features/external-command-attachment.feature` (CLI subsystem,
`INT_TEST_E2E_CLI=1`), driven by `cmd/e2e/`:

```gherkin
Feature: Attaching an external command tree to a generated project
  Scenario: A declarative attach survives a feature re-render
    Given a freshly generated project "widget"
    When I attach the external command module "example.com/ext-cli@v0.1.0" \
      with constructor "NewCmdThing" taking "logger" wrapped
    Then the generated root wires "NewCmdThing"
    And the project builds
    When I enable the "docs" feature
    Then the generated root still wires "NewCmdThing"
    When I regenerate the project
    Then the generated root still wires "NewCmdThing"

  Scenario: An adapter attach survives regeneration
    Given a freshly generated project "widget"
    When I scaffold the external command adapter
    Then the root calls "external.Commands(p)"
    And the adapter file "pkg/cmd/external/attach.go" exists
    When I regenerate the project
    Then the root still calls "external.Commands(p)"
    And the adapter file is unchanged

  Scenario: Detach removes wiring and prunes the require
    Given a generated project with a declarative external command
    When I detach the external command module
    Then the generated root no longer wires it
    And the go.mod no longer requires the module
```

Manual verification for the real case: attach `go/signing-cli` to a scratch
project (or re-derive sigillum's wiring), `just build`, confirm `<tool> sign` /
`<tool> keys` work, `gtb regenerate project`, confirm they still work — the
`gtb-generator-manual-test` skill covers this, with `-count=1` to defeat the E2E
go-test cache.

## 8. Alternatives considered / decisions

### 8.1 Keep the `main.go` + `.gtb/ignore` convention (status quo) — rejected

It is the very problem #8 raises: forfeits generator management of `main.go`,
rests on a bare convention, and breaks under `enable signing` / `regenerate`.

### 8.2 Free-form Go expression in the manifest — rejected

A raw call-expression string spliced into the root turns the manifest into an
arbitrary-code injection surface, defeats validation/review, and conflicts with
the generator's template-security discipline (`template_escape.go`). The closed
vocabulary plus the adapter give the same coverage with none of that risk.

### 8.3 AST-inject the attachment (like `gtb generate command`) — rejected

`gtb generate command` injects via `dave/dst` (`ast.go:37`); an injected
attachment would be *removed* by the next full `regenerateRootCommand` re-render.
Rendering from the manifest on every root render is the only approach stable
across both the incremental and full-regenerate paths.

### 8.4 Runtime feature-gating of declarative attachments — deferred (decision)

`Tool.IsEnabled` already accepts any string, but built-in features are a
**closed, curated catalogue** (`FeatureCatalogue`, guarded by a test against
`props.AllFeatures` so features can't drift). Making `gtb enable/disable sign`
toggle a *declarative* attachment would require teaching that system about
open-ended, downstream-sourced **custom feature names** (manifest features +
`SetFeatures` render + `enable/disable` acceptance + conditional root
registration). That is a real expansion of a deliberately-curated system, with
**no current consumer** — sigillum uses the un-gated `setup.Wrap("", …)` form.
**Decision:** declarative attachments are un-gated in v1; anyone needing a gate
uses the adapter channel and self-gates (on a config key, env var, or their own
`IsEnabled` check). If genuine demand for `enable/disable` integration appears,
add the custom-feature model then, as its own spec with a consumer to validate
against.

### 8.5 Symbol/version verification at attach time — deferred (decision)

v1 relies on `go mod tidy` + build inside the atomic staged FS to reject a
missing symbol or bad version (the broken tree is never committed). A `gtb
doctor` attachment check (module resolvable, symbol present), mirroring the
`credentials.no-literal` pattern, is a later follow-up.

## 9. Migration and compatibility

- **Backwards-compatible manifest:** `external_commands` /
  `external_commands_adapter` are `omitempty`; existing manifests decode
  unchanged and render nothing new.
- **Pre-1.0 latitude:** GTB is `v0.x`; a breaking `SkeletonRootData` / generator
  API shape ships as a minor bump with a `docs/migration/` note.
- **sigillum migration (the acceptance demonstration):** once shipped, sigillum
  runs
  `gtb attach command gitlab.com/phpboyscout/go/signing-cli@<v> --constructor
  NewCmdSign --arg logger --wrap --constructor NewCmdKeys --arg logger --wrap`,
  deletes the `cmd/sigillum/main.go` hand-edit, and removes it from
  `.gtb/ignore`. Sequence this *after* the Friday rekey work so it doesn't
  collide with the signing release train.

## 10. Resolved decisions

All open questions from the initial draft were resolved with the maintainer
(2026-07-29):

| # | Decision |
|---|----------|
| CLI verb | `gtb attach command` / `gtb attach adapter` / `gtb attach list` / `gtb detach command`. |
| Escape hatch | **Shipped in v1** as Channel 2 (the adapter), alongside the declarative channel. |
| Vocabulary scope | Minimal — `logger, props, config, fs, version`. Add tokens only when a real constructor demands one. |
| Version pin | **Explicit `@version` required**; no implicit latest resolution in v1. |
| Feature-gating | **Un-gated in v1** (§8.4); no `feature` field; the feature catalogue is left untouched. |
| Collision detection | Best-effort `(module, constructor)` always; optional `--name` for a real name check; compiler/runtime backstop. |
| Symbol verification | Deferred to `go mod tidy` + build (§8.5); `doctor` check is future work. |
| Detach go.mod | Prune-then-tidy for a clean diff. |
| Adapter ergonomics | Distinct `gtb attach adapter` verb; adapter preserved via the seed-file / preserve-if-exists mechanism, recorded by a manifest boolean. |
| Gate granularity | N/A (no gating in v1). |

## 11. Implementation phases

**Phase 1 — Manifest + validation.** `ManifestExternalCommand` /
`ManifestExternalAttach` types, `ExternalCommands` + `ExternalCommandsAdapter` on
`ManifestProperties`, byte-stable round-trip, full `validate.go` coverage
(closed `Args` vocabulary, required version). TDD: schema + validation first.

**Phase 2 — Root rendering (both channels).** `SkeletonExternalCommand`, the
`skeleton_root.go` render loop (Channel-1 args + the optional
`external.Commands(p)` spread), the adapter seed file, and `buildSkeletonRootData`
population in `regenerate.go`. TDD: golden-file render tests (sigillum-shape,
props-shape, zero-arg, adapter-present) first. At the end of this phase a
hand-authored manifest renders correct wiring through `regenerate` — the core
guarantee is testable here.

**Phase 3 — Generator API + go.mod + adapter scaffold.**
`AttachExternalCommand` / `AttachExternalAdapter` / `DetachExternalCommand` /
`ListExternalCommands`, require add/prune, adapter scaffold-once, all atomic via
the staged FS. TDD: attach/detach/adapter + regeneration-safety tests (including
the `enable signing` survival test) first.

**Phase 4 — CLI + docs + BDD.** `gtb attach command|adapter|list` / `gtb detach
command`; the Godog feature file; documentation (§12). TDD: CLI contract tests +
Gherkin scenarios.

**Phase 5 — Prove on sigillum.** Migrate sigillum to the declarative mechanism,
delete the `main.go` hand-edit and its `.gtb/ignore` entry, verify `sign`/`keys`
survive `enable signing` + `regenerate`. The real-world acceptance gate.

## 12. Documentation impact

Documentation is a hard requirement, following Diátaxis (with the project's
established concessions):

- **How-to (new):** `docs/how-to/attach-external-commands.md` — both channels,
  the injection vocabulary, when to reach for the adapter, and the sigillum
  worked example.
- **Reference:** the `gtb attach`/`detach` commands via the generated CLI
  reference; the `external_commands` / `external_commands_adapter` manifest
  blocks alongside the existing manifest reference.
- **Explanation:** extend the generator/manifest explanation with *why*
  attachments are manifest-declared and rendered (regeneration safety), and the
  two-channel split, cross-referencing the root-render and middleware-pipeline
  concepts.
- **Tutorial:** per the repo's Diátaxis deviation, any tutorial is a blog post on
  phpboyscout.uk (linked from the docs tutorials section), **not** in-repo — flag
  "attach an external CLI to your gtb tool" as a blog candidate.

## 13. Implementation status

**Phases 1–4 are implemented, tested, and verified end-to-end** on branch
`feat/external-command-attachment` (all §10 decisions applied):

- **Phase 1–2** — manifest schema + validation, both render channels, and
  provenance recording. Unit + golden-file + regenerate + provenance-round-trip
  tests pass; `golangci-lint` clean.
- **Phase 3** — `AttachExternalCommand` / `AttachExternalAdapter` /
  `DetachExternalCommand` / `ListExternalCommands`, `go get` version pinning,
  adapter scaffold (preserve-if-exists). The headline survival test
  (`enable signing` does not drop an attachment) passes.
- **Phase 4** — `gtb attach command|adapter|list` / `gtb detach command`
  (registered in the root), this how-to, and 7 Godog scenarios (44 steps) under
  `@external-commands`, all passing. **Verified on the real binary:** attaching
  `go/signing-cli` into a scaffolded project produces a tree that builds and runs
  `sign`/`keys` as top-level commands, and the attachment survives both
  `regenerate project` and `enable signing`.
- **Phase 5** — migrating `sigillum` itself remains, **gated on the Friday
  rekey** (so it does not collide with the signing release train). Promote this
  spec to `IMPLEMENTED` once sigillum is migrated and its `main.go` hand-edit +
  `.gtb/ignore` entry are removed.

---

All §10 decisions are resolved. Phases 1–4 are implemented and verified; Phase 5
(the sigillum acceptance demonstration) is pending the rekey.
