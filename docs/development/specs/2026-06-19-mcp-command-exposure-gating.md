---
title: "Per-command MCP exposure gating (build-time, default-on, explicitly excluded)"
description: "When the mcp feature is enabled, GTB exposes every runnable command as an MCP tool with no consumer seam to keep sensitive commands (post, approve, auth) off the tool surface while leaving them on the CLI. Add a per-command, build-time exposure control owned by the command's own manifest entry: commands are exposed by default and explicitly excluded via a manifest field, a setup-level marker stamped into the generated command, an ophis selector composed in the root, and gtb enable/disable mcp verbs. No runtime config lever — exposure is fixed in the binary and auditable."
status: IMPLEMENTED
date: 2026-06-19
tags:
  - specification
  - mcp
  - ophis
  - generator
  - manifest
  - security
  - setup
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Per-command MCP exposure gating (build-time, default-on, explicitly excluded)

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   2026-06-19

Status
:   IMPLEMENTED (open questions resolved in review 2026-06-19; shipped on branch feature/mcp-tool-gating)

Driver
:   Downstream feature request `GTB-FEATURE-REQUEST-mcp-tool-gating-2026-06-18.md`
    (keryx session); keryx interface-contract spec `0002 §5 R-MCP-2`.

## Summary

When the `mcp` feature is enabled, GTB registers the [ophis](https://github.com/njayp/ophis)
MCP command with `Config.Selectors` left `nil`. Per ophis' own contract
(`config.go:37`: *"If nil or empty, defaults to exposing all commands with all
flags."*), **every runnable command in the tree becomes an MCP tool**. A
consuming tool has no seam to keep publish/spend/secret commands (`post`,
`approve`, `auth`) off the MCP surface. The only existing lever — cobra
`Hidden` — also removes the command from the CLI, which is unacceptable.

This spec adds a **per-command MCP exposure control** that is:

- **Owned by the command itself.** The decision lives on the command's own
  manifest entry (alongside `hidden`, `protected`, `with_assets`), not in a
  separate `mcp:` tree and not in a central allow/deny list that drifts.
- **Default-on.** Commands are exposed as today; exclusion is opt-in per
  command. A project that declares nothing behaves identically to today.
- **Build-time only.** Exposure is decided at `generate`/`regenerate` time and
  baked into the binary as an annotation. There is **no runtime config key** to
  flip it — a runtime toggle is itself an attack surface (config injection could
  re-expose a gated tool), so the tool surface is fixed and auditable in the
  shipped binary. State changes only via the manifest, applied through
  `regenerate` or the new `gtb enable mcp` / `gtb disable mcp` verbs.
- **Subtree-aware.** Excluding a parent excludes its whole subtree; the marker
  is recorded once on the parent.

This deliberately **inverts the polarity** proposed in the originating feature
request (which asked for "gated, off by default, opt-in via runtime config").
After review, GTB adopts "exposed by default, explicitly excluded at build time"
because it keeps today's behaviour as the zero-config default, co-locates the
decision with the command, and removes the runtime attack surface.

## Problem Statement

`pkg/cmd/root/root.go` → `registerFeatureCommands` builds the ophis command
with no selectors:

```go
if props.Tool.IsEnabled(p.McpCmd) {
    mcpCmd := ophis.Command(&ophis.Config{
        SloggerOptions: &slog.HandlerOptions{Level: mcpLogLevel},
    })
    rootCmd.Register(setup.Wrap(p.McpCmd, mcpCmd))
}
```

ophis registers tools by walking the command tree (`config.go:163-193`),
applying basic safety filters (hidden/deprecated/non-runnable/built-ins —
`config.go:198-208`) and then the configured selectors. With no selectors it
exposes everything. There is no `props`, option, or annotation path for a
consumer to exclude a command, and `Hidden` is off the table because the
command must stay runnable on the CLI.

Concrete downstream impact: keryx exports 53 MCP tools including `keryx_post`,
`keryx_approve`, and `keryx_auth` with no way to drop them — an MCP client could
publish publicly or trigger a token refresh unprompted. This is not
keryx-specific: any GTB tool with destructive/credential commands has the same
hole.

## Goals & Non-Goals

### Goals

1. Let a tool author mark a command as **excluded from the MCP tool surface**
   without hiding it from the CLI.
2. Make exclusion a **property of the command's own manifest entry**, default
   exposed, explicitly set to exclude.
3. Compose the ophis selector in the root so excluded commands (and their
   subtrees) are omitted from `mcp tools` / `mcp start`.
4. Round-trip the decision through the generator: `gtb generate command`
   accepts the control, `.gtb/manifest.yaml` records it, and `regenerate
   project` reproduces it.
5. Provide `gtb disable mcp [command]` (gate) and `gtb enable mcp [command]`
   (un-gate) verbs that update the manifest and re-render the affected command.
6. Keep zero-config behaviour **byte-for-byte identical to today**.

### Non-Goals

- **No runtime config lever.** Explicitly out of scope by design (security).
- **No flag-level gating.** Only whole-command (and subtree) exclusion.
- **No native MCP hint automation** (`destructiveHint`/`openWorldHint`). These
  are client *hints*, not hard exclusions, and belong to the R-MCP-3 roadmap
  item (see [Roadmap](#related-roadmap-out-of-scope)).
- **No selector-passthrough escape hatch** (`WithMCPSelectors(...)`). The
  annotation path covers the need; a raw-selector option would leak ophis types
  across the GTB API boundary. Revisit only if a consumer needs arbitrary
  selectors.

## Design Overview

Three layers, mirroring the existing `Protected` precedent (a `*bool` carried
through manifest → `CommandContext` → `CommandData`, settable by a dedicated
verb):

| Layer | Today (`Protected`) | New (`MCPEnabled`) |
|-------|---------------------|--------------------|
| Marker in generated code | n/a (manifest-only) | `setup.ExcludeFromMCP(cmd)` / `setup.IncludeInMCP(cmd)` stamped when the field is explicit |
| Manifest field | `protected: *bool` | `mcp_enabled: *bool` (tri-state) |
| Runtime effect | overwrite policy | ophis selector omits the command |
| Mutating verb | `generate command protect/unprotect` | `gtb enable/disable mcp [command]` |

`MCPEnabled` is a **tri-state `*bool`** with **cascading inheritance** down the
command tree:

- `nil` (field absent) → **inherit** from the nearest ancestor that sets an
  explicit value; **exposed** if no ancestor does (default, backward
  compatible).
- `false` → **excluded** from MCP; `setup.ExcludeFromMCP` is stamped. Excludes
  this command and, by inheritance, its descendants…
- `true` → **explicitly exposed**; `setup.IncludeInMCP` is stamped. *…except* a
  descendant may set `true` to **override an excluded ancestor** and remain on
  the MCP surface (see [Resolution & subtree override](#resolution--subtree-override)).
  Also recorded for a deliberate, auditable re-enable.

### Resolution & subtree override

Exposure is resolved at runtime by walking the command and its ancestors and
taking the **nearest explicit value** — `true` exposes, `false` excludes — and
defaulting to **exposed** when no command in the chain sets one. This gives
subtree exclusion by default *and* a precise override: a parent `false`
withholds the whole subtree **except** a descendant that explicitly sets `true`
(and, transitively, that descendant's own subtree until some deeper command
sets `false` again).

```
post                 mcp_enabled: false   → EXCLUDED
post due             (nil → inherit)       → EXCLUDED   (nearest explicit: post=false)
post status          mcp_enabled: true     → EXPOSED    (override of post=false)
post status retry    (nil → inherit)       → EXPOSED    (nearest explicit: post status=true)
```

Because the explicit value is carried as an annotation **in generated code**
(both polarities), it is fully expressible at runtime and round-trips through
`regenerate manifest` — there is no "true collapses to nil" loss.

## Public API

### `pkg/setup` — markers + resolver (new)

The three states are modelled by an **enum-style const**, `MCPExposure`, which
is the canonical in-code representation (the manifest's `*bool` converts to it at
the generator boundary). The marker is a **tri-state annotation** carrying the
explicit value; **absence** means "inherit". A negative-only "excluded" marker
would be insufficient — a descendant must be able to carry an explicit *exposed*
that overrides an excluded ancestor.

```go
// MCPExposure is a command's explicit MCP-surface decision. The zero value is
// Inherit, so an unset field/annotation naturally means "inherit". It mirrors
// the manifest mcp_enabled *bool: nil↔Inherit, true↔Exposed, false↔Excluded.
type MCPExposure uint8

const (
    MCPExposureInherit  MCPExposure = iota // unset — inherit nearest ancestor; exposed if none
    MCPExposureExposed                     // explicit: on the MCP surface
    MCPExposureExcluded                    // explicit: withheld from the MCP surface
)

// MCPExposureFromBool maps the manifest/CLI tri-state *bool to the enum.
func MCPExposureFromBool(b *bool) MCPExposure

// MCPExposureAnnotation is the cobra.Command.Annotations key under which a
// command's explicit decision is recorded. Mirrors FeatureAnnotation. Value is
// "exposed" or "excluded"; the key is absent when the command inherits.
const MCPExposureAnnotation = "gtb.mcp.exposure"

// ExcludeFromMCP marks a command as excluded (stamps the annotation
// "excluded"): when the mcp feature is enabled, the command — and, by
// inheritance, descendants that do not themselves set IncludeInMCP — is omitted
// from `mcp tools` / `mcp start`. CLI behaviour is unaffected. Returns cmd.
func ExcludeFromMCP(cmd *Command) *Command

// IncludeInMCP marks a command as explicitly exposed (stamps "exposed"). Its
// primary use is to override an excluded ancestor so a specific subcommand
// stays on the MCP surface; it is also stamped for any command whose
// mcp_enabled is explicitly true. Returns cmd.
func IncludeInMCP(cmd *Command) *Command

// MCPExposureOf returns a command's own explicit decision (Inherit when it
// carries no annotation). Nil-safe.
func MCPExposureOf(cmd *cobra.Command) MCPExposure

// IsExposedToMCP reports whether cmd is exposed, walking cmd and its ancestors
// and returning the nearest explicit value (Exposed→true, Excluded→false),
// defaulting to true when no command in the chain sets one. Operates on the raw
// *cobra.Command so it is callable from the root selector closure. Nil-safe.
func IsExposedToMCP(cmd *cobra.Command) bool
```

`ExcludeFromMCP`/`IncludeInMCP` stamp `cmd.Annotations[MCPExposureAnnotation]`,
initialising the map if nil, exactly as `Wrap` stamps `FeatureAnnotation`
(`pkg/setup/command.go:44-54`). `IsExposedToMCP` is the nearest-explicit-ancestor
resolver described in [Resolution & subtree override](#resolution--subtree-override);
its walk is a `switch` over `MCPExposureOf` up the `cmd.Parent()` chain.

### `pkg/cmd/root/root.go` — selector composition (changed)

```go
if props.Tool.IsEnabled(p.McpCmd) {
    mcpCmd := ophis.Command(&ophis.Config{
        SloggerOptions: &slog.HandlerOptions{Level: mcpLogLevel},
        Selectors: []ophis.Selector{{
            // Expose a command when the nearest explicit mcp_enabled in its
            // ancestor chain is true (or none is set). With nothing marked,
            // every command resolves to exposed — equivalent to the nil-
            // selector default: all commands, all flags. The annotation walk
            // runs at tool-registration time (mcp start / mcp tools), by which
            // point the full command tree is built — build-time-safe, no config.
            CmdSelector: func(c *cobra.Command) bool {
                return setup.IsExposedToMCP(c)
            },
        }},
    })
    rootCmd.Register(setup.Wrap(p.McpCmd, mcpCmd))
}
```

Rationale for a single always-installed selector (vs. the feature request's
build-time branch between `[{notGated}]` and `nil`): selectors are evaluated by
ophis lazily, when it enumerates tools at `mcp start`/`mcp tools` runtime
(`config.go:129-159`), *after* the full command tree exists. Branching at
root-build time is unsafe because user commands self-register *after*
`registerFeatureCommands` runs. A single closure that walks annotations at
eval-time is robust and, with a `LocalFlagSelector`/`InheritedFlagSelector` of
`nil`, preserves the all-flags default for every exposed command.

### Generator surfaces

- **`internal/generator/manifest.go`** — add to `ManifestCommand`:
  ```go
  MCPEnabled *bool `yaml:"mcp_enabled,omitempty"`
  ```
- **`internal/generator/context.go` + `Config`** — carry `MCPEnabled *bool`
  through `CommandContext` and `generator.Config`, **mirroring `Protected`
  exactly** (`buildCommandContext` from `cmd.MCPEnabled`, `ToConfig` to
  `Config.MCPEnabled`). The `*bool` is the tri-state at every generator edge
  (manifest/Config/CLI) — the same `IsSet`+value shape — and is **not** juggled
  with ad-hoc `if p != nil && *p`: it is converted to the `setup.MCPExposure`
  enum exactly **once**, at the `CommandData` boundary below.
- **`internal/generator/templates/command.go`** — add
  `MCPExposure setup.MCPExposure` to `CommandData`; the generator sets it via
  `setup.MCPExposureFromBool(g.config.MCPEnabled)` when building `CommandData`
  (in `commands.go`). In `generateNewCmdFunction`, after `cmd := setup.Wrap(...)`,
  `switch data.MCPExposure`: `Excluded` → `setup.ExcludeFromMCP(cmd)`, `Exposed`
  → `setup.IncludeInMCP(cmd)`, `Inherit` → emit nothing. The enum lands exactly
  where the 3-way render decision is made; representing both explicit polarities
  is what lets a descendant `Exposed` override an excluded ancestor and lets
  `regenerate manifest` recover the explicit value.
- **`internal/generator/generator.go`** — add
  `func (g *Generator) SetMCPEnabled(ctx context.Context, commandPath string, enabled bool) error`,
  mirroring `SetProtection`: locate the command in the manifest, set
  `MCPEnabled = &enabled`, persist, then **re-render that single command** so
  the generated `setup.ExcludeFromMCP`/`setup.IncludeInMCP` marker is updated.
  (Unlike `SetProtection`, which is manifest-only, this changes generated code,
  so it must trigger the single-command regeneration path.)

### `gtb generate command` — flags + interactive UI

`generate command` is the primary author entry point, so both its
non-interactive flags **and** its interactive wizard must expose the control.
`CommandOptions` (`internal/cmd/generate/command.go:27-53`) gains
`MCPEnabled *bool`, threaded into the `generator.Config` built in `Run`
(`:563-591`) alongside `Protected`.

**Non-interactive flag (tri-state, mirrors `--protected`).** The `--protected`
flag uses a backing `bool` var read only when `cmd.Flags().Changed(...)`
(`:95-97`, `:133-140`, `:166`). Mirror it exactly:

```go
var mcpEnabledFlag bool
// ...
cmd.Flags().BoolVar(&mcpEnabledFlag, "mcp-enabled", true,
    "Expose this command as an MCP tool (tri-state: --mcp-enabled for true, "+
        "--mcp-enabled=false to exclude it from the MCP surface, omitted for default-exposed)")
// in RunE, beside the existing Changed("protected") block:
if cmd.Flags().Changed("mcp-enabled") {
    opts.MCPEnabled = &mcpEnabledFlag
}
```

So: omitted → `nil` → exposed (default); `--mcp-enabled=false` → excluded;
`--mcp-enabled` / `--mcp-enabled=true` → explicitly exposed. The CLI carries the
full tri-state. Add a worked example to the command's `Long` examples block
(`:102-127`), e.g. *"# Generate a command kept off the MCP tool surface — `gtb
generate command -n post --mcp-enabled=false`"*.

**Interactive wizard.** MCP exposure is a security control, so it gets a
**dedicated, prominent confirm** rather than being one checkbox among the
"Include …" items in the existing Options multiselect (`buildMainGroup`,
`:316-325`). Add a standalone `huh.NewConfirm` whose description spells out the
security implication, so the author makes a conscious decision:

```go
huh.NewConfirm().
    Title("Expose to MCP?").
    Description("If enabled, AI assistants can invoke this command as an MCP tool.\n"+
        "Disable for publish/spend/secret commands — it stays available on the CLI,\n"+
        "but is withheld from `mcp tools` / `mcp start`.").
    Affirmative("Expose").
    Negative("Exclude").
    Value(&o.ExposeToMCP),
```

`CommandOptions` gains `ExposeToMCP bool`, **initialised to `true`** before the
wizard runs (exposed is the default), so a user who tabs past the confirm keeps
today's behaviour. Placed as its own field in the main group, immediately after
the Options multiselect, it reads as a distinct decision rather than blending
into the feature toggles. (Whether it warrants a fully separate wizard *screen*
vs. a standalone field in the main group is left to implementation; a standalone
field with this description satisfies the "prominent call-out" intent without an
extra navigation step.)

**Confirm → field mapping.** After the wizard completes (in
`runAdditionalSteps`, alongside `syncOptionsToFlags`), translate the bool to the
tri-state pointer — only the *exclude* choice is recorded; *expose* leaves
`nil`, matching the default and the non-interactive flag's behaviour:

```go
if !o.ExposeToMCP {
    no := false
    o.MCPEnabled = &no
}
```

The interactive path only ever needs *nil vs. false* (an author never asserts
explicit `true` at creation — exposed is already the default); explicit `true`
is reachable via `--mcp-enabled=true` or `gtb enable mcp`. MCP is **not** routed
through the `o.Options` multiselect round-trip (a `[]string` of positives cannot
represent `false` distinctly), and `syncFlagsToOptions` needs no MCP case — the
non-interactive path sets `opts.MCPEnabled` directly from `--mcp-enabled`.

### Mutating verbs

- **`internal/cmd/disable/disable.go`** — add `gtb disable mcp [command-path]`
  (the gating action; default is on, so *disable* is what protects a sensitive
  command). Calls `gen.SetMCPEnabled(ctx, args[0], false)`.
- **`internal/cmd/enable/enable.go`** — add `gtb enable mcp [command-path]`
  (un-gate). Calls `gen.SetMCPEnabled(ctx, args[0], true)`.

Both verbs take the command path as `cobra.ExactArgs(1)` and a `-p/--path`
flag, exactly like `generate command protect/unprotect`
(`internal/cmd/generate/command.go:176-222`).

### Regeneration round-trip (`regenerate project` / `regenerate manifest`)

`regenerate` is a deliberate **all-or-nothing** operation in one of two
directions, where the developer declares which artefact is the source of truth —
there is intentionally **no per-command, partial, or merge ("dilution")
reconciliation**:

- **`regenerate project`** — manifest → code. `mcp_enabled` flows from the
  manifest, converts to `MCPExposure`, and the template re-stamps
  `setup.ExcludeFromMCP` / `setup.IncludeInMCP`. Round-trips with no extra work.
- **`regenerate manifest`** — code → manifest, via `manifest_scan.go` →
  `extractCommandMetadata` (`ast_extract.go`). Because the markers now live **in
  generated code** (unlike `Protected`, which is manifest-only), faithful
  extraction **must recover them**, exactly as the existing `detectAssets` /
  `detectInitializer` / `detectConfigValidation` helpers recover their features.
  Add a sibling `detectMCPExposure` that finds a `setup.ExcludeFromMCP(cmd)`
  call → `mcp_enabled: false`, or `setup.IncludeInMCP(cmd)` → `mcp_enabled:
  true`, on the extracted `ManifestCommand` (neither call → field absent).
  Without it, a code-as-source-of-truth regenerate would drop the gating and the
  next `regenerate project` would **silently re-expose** the command.

This is full-fidelity extraction, **not** a preserve/merge of the prior manifest
— consistent with the no-dilution design. Because both polarities are carried in
code (the `MCPExposure` enum, stamped as `exposed`/`excluded`), the explicit
`mcp_enabled: true` audit state round-trips exactly — there is no "true collapses
to nil" loss.

**Changing a single command** is an explicit re-run of `gtb generate command`
(with its change-sensitive overwrite), which both sets `--mcp-enabled` and
rewrites the code — not a `regenerate` variant.

## Data Models

`.gtb/manifest.yaml`, a gated subtree with one override:

```yaml
commands:
  - name: post
    description: Publish queued content to social platforms
    mcp_enabled: false          # excluded; whole subtree withheld by inheritance
    commands:
      - name: due
        description: Publish only content past its due time
        # no field — inherits Excluded from post
      - name: status
        description: Read the publish queue status (safe)
        mcp_enabled: true       # override: stays on the MCP surface despite post=false
        commands:
          - name: watch
            description: Stream status updates
            # no field — inherits Exposed from post status
```

Resolved surface: `post`, `post due` → **excluded**; `post status`,
`post status watch` → **exposed**.

Generated constructors (excerpts):

```go
// NewCmdPost
cmd := setup.Wrap("post", &cobra.Command{ /* ... */ })
setup.ExcludeFromMCP(cmd)   // MCPExposure: Excluded

// NewCmdStatus (under post)
cmd := setup.Wrap("status", &cobra.Command{ /* ... */ })
setup.IncludeInMCP(cmd)     // MCPExposure: Exposed — overrides post

// NewCmdDue / NewCmdWatch: no marker (Inherit)
```

## Error Cases

| Case | Behaviour |
|------|-----------|
| `gtb enable/disable mcp <path>` where command not found in manifest | Return a wrapped not-found error naming the path; no manifest write. |
| `gtb enable/disable mcp` outside a GTB project (no manifest) | Reuse existing `ErrNotGoToolBaseProject` path via project resolution. |
| Target command is `protected` | `SetMCPEnabled` regeneration honours the existing protection gate (`ErrCommandProtected`); document that the operator must `--force`/unprotect first, consistent with other regenerate flows. |
| `mcp_enabled: true` on a command with no MCP feature enabled | No effect (selector only consulted when `mcp` feature is on); field is inert. |
| Malformed `mcp_enabled` in a hand-edited manifest | YAML decode error surfaced by `DecodeManifestFile` as today. |

## Backward Compatibility

- A project that sets nothing → `MCPEnabled` is `nil` everywhere, no
  `ExcludeFromMCP` stamped, and the root installs a selector that exposes all
  commands with all flags — **behaviourally identical to the current nil
  selector**.
- `mcp_enabled` is `omitempty`, so existing manifests are unchanged on
  re-serialisation.
- Additive `pkg/setup` API (`MCPExposure` enum, `MCPExposureFromBool`,
  `ExcludeFromMCP`, `IncludeInMCP`, `MCPExposureOf`, `IsExposedToMCP`,
  `MCPExposureAnnotation`); additive generator/CLI surface. No breaking
  signatures. (Pre-1.0 anyway — `docs/about/api-stability` policy is
  aspirational; this change ships as a `feat` minor.)
- Run `just apidiff` for visibility; the change is purely additive.

## Security Considerations

- **Build-time fixing is the security property.** The MCP tool surface is
  determined entirely by annotations baked into the binary; no config file, env
  var, or flag can re-expose an excluded command at runtime. This is the
  rationale for rejecting the originating request's runtime opt-in.
- ophis basic safety filters (hidden/deprecated/non-runnable/built-ins) still
  apply *before* our selector; exclusion is strictly additional.
- `setup.ExcludeFromMCP`/`IncludeInMCP` take no user-influenced free-form
  string, so no `template_escape`/validation perimeter applies. The manifest
  field is a boolean; the annotation value is a fixed enum string; no new
  validation surface.
- Excluded commands remain on the CLI by design — the gate narrows the MCP
  surface only.

## Testing Strategy

### Unit (`pkg/setup`) — ≥90% coverage required (public pkg)

- `MCPExposureFromBool` maps `nil`/`true`/`false` → `Inherit`/`Exposed`/`Excluded`.
- `ExcludeFromMCP`/`IncludeInMCP` stamp the annotation `excluded`/`exposed`;
  idempotent; nil-map init; returns cmd. `MCPExposureOf` reads it back (and
  `Inherit` when absent); nil-safe.
- `IsExposedToMCP` resolution: self explicit wins; nearest-ancestor wins;
  **override case** — parent `Excluded`, child `Exposed` → child exposed, a
  nil grandchild under the child inherits `Exposed`; default-exposed when the
  whole chain is `Inherit`; nil-command/nil-annotations safety.

### Unit (`pkg/cmd/root`)

- Build a fake cobra tree with an excluded subtree containing one `Exposed`
  override; assert via the composed `CmdSelector` that the excluded command and
  its inheriting descendants are omitted, the override (and its inheriting
  descendants) remain, and sibling subtrees are untouched — all flags intact.
- Confirm the zero-marker case exposes the same set as a nil selector.

### Unit (`internal/generator`)

- `mcp_enabled` round-trips through encode/decode; `buildCommandContext`
  converts the `*bool` to the correct `MCPExposure` enum and carries it through
  `ToConfig`.
- Template emits `setup.ExcludeFromMCP(cmd)` for `Excluded`,
  `setup.IncludeInMCP(cmd)` for `Exposed`, and nothing for `Inherit`.
- `SetMCPEnabled` updates the manifest field and re-renders the command file
  (correct marker after disable/enable); honours protection.
- `detectMCPExposure` recovers `mcp_enabled: false` from `setup.ExcludeFromMCP`
  and `mcp_enabled: true` from `setup.IncludeInMCP` during `regenerate manifest`
  (code → manifest); neither call → field absent. A full round-trip
  (`regenerate manifest` then `regenerate project`) preserves gating **and** the
  explicit-true override.

### Unit (`internal/cmd/generate`)

- `--mcp-enabled=false` (and `=true`, and omitted) resolve to the correct
  `MCPEnabled` tri-state on `CommandOptions` via the `Changed("mcp-enabled")`
  gate, paralleling the existing `--protected` tests.
- The interactive `ExposeToMCP` confirm maps to `MCPEnabled == false` when the
  author chooses "Exclude", and leaves it `nil` (default `true`) otherwise.
- The resolved `MCPEnabled` reaches `generator.Config` in `Run`.

### E2E BDD (Godog) — **warranted**

Per `CLAUDE.md` ("New CLI commands … must include Gherkin scenarios") and the
suitability assessment in `2026-03-28-godog-bdd-strategy.md`, this feature adds
CLI verbs *and* observable MCP-surface behaviour, so it qualifies. Add
`features/mcp_exposure_gating.feature`:

- Given a generated tool with a `post` command, when I run `gtb disable mcp
  post` and then `<tool> mcp tools`, then the output omits `<tool>_post` (and
  `<tool>_post_due`) but still lists read commands.
- When I run `gtb enable mcp post`, then `<tool>_post` reappears.
- Given a command excluded from MCP, when I run `<tool> post …` on the CLI, then
  it still executes (CLI unaffected).

Reuse the `cmd/e2e/` test binary (all features enabled) and the existing
`mcp tools` invocation path; gate under `INT_TEST_E2E_CLI=1`.

## Implementation Phases

1. **`pkg/setup` enum + markers + resolver** (`MCPExposure`,
   `MCPExposureFromBool`, `MCPExposureAnnotation`, `ExcludeFromMCP`,
   `IncludeInMCP`, `MCPExposureOf`, `IsExposedToMCP`) + unit tests.
   *(Library-first.)*
2. **Root selector composition** in `registerFeatureCommands` + root unit tests.
3. **Manifest + context threading** (`MCPEnabled *bool` on `ManifestCommand`,
   `CommandContext`, and `Config`, mirroring `Protected`) + generator unit tests.
4. **Template emission** of `setup.ExcludeFromMCP` / `setup.IncludeInMCP` +
   template tests.
5. **`SetMCPEnabled` generator method** (manifest update + single-command
   re-render) + tests.
6. **`generate command` flags + interactive UI**: tri-state `--mcp-enabled`
   flag (mirroring `--protected`), the dedicated "Expose to MCP?" confirm in the
   wizard with its `ExposeToMCP`→`MCPEnabled` mapping, the `Long` examples
   update, and `MCPEnabled` threaded into `generator.Config`.
7. **Mutating verbs**: `gtb disable mcp` / `gtb enable mcp`.
8. **`detectMCPExposure`** in `extractCommandMetadata` (both markers) so
   `regenerate manifest` (code → manifest) preserves gating and overrides.
9. **Docs**: update the MCP component/concept docs with the gating model and the
   new verbs; note the build-time-only security rationale.
10. **E2E BDD** feature + steps.
11. `/gtb-verify`; regenerate a scratch project to confirm the marker renders
    and `mcp tools` omits the excluded command; `/simplify` changed files.

## Open Questions

1. **Field/helper naming + 3-state representation.** *(Resolved 2026-06-19,
   revised for the subtree-override edge case.)* Manifest field stays the
   positive `mcp_enabled *bool` (nil = inherit, true = exposed, false =
   excluded) for verb symmetry and readable YAML. In code, the three states are
   a `setup.MCPExposure` enum (`Inherit`/`Exposed`/`Excluded`) — the canonical
   type threaded through the command hierarchy and resolver — with the `*bool`
   converting at the generator boundary (`MCPExposureFromBool`). Both explicit
   polarities are stamped (`ExcludeFromMCP`→`excluded`, `IncludeInMCP`→
   `exposed`) under annotation `gtb.mcp.exposure`; absence = inherit. The earlier
   negative-only marker was dropped because the override requires an explicit
   "exposed" to be expressible in the compiled tree.
2. **Verb placement.** *(Resolved 2026-06-19.)* Top-level
   `gtb enable/disable mcp [command]`, in the existing `enable`/`disable`
   groups (alongside `enable/disable signing`) — a clearer contract for human
   and AI users than nesting under `generate`. The gating action is `disable`
   (exposure defaults on); `enable` un-gates.
3. **Explicit-true value.** *(Resolved 2026-06-19.)* `gtb enable mcp` writes an
   explicit `mcp_enabled: true` — a deliberate re-enablement of a security
   control must be captured in the manifest, not erased.
4. **AST-extraction scope.** *(Resolved 2026-06-19.)* In scope, as
   full-fidelity code → manifest extraction: a `detectMCPExposure` helper
   recovers `mcp_enabled: false` from `setup.ExcludeFromMCP` and
   `mcp_enabled: true` from `setup.IncludeInMCP` during `regenerate manifest`,
   mirroring the existing `detect*` helpers. No preserve/merge of the prior
   manifest (that would be the rejected "dilution"); per-command edits go
   through an explicit `gtb generate command` re-run. Because both polarities
   are carried in code, the explicit-true override round-trips exactly.
5. **Interactive wizard placement.** *(Resolved 2026-06-19.)* A dedicated,
   prominent `huh.NewConfirm` ("Expose to MCP?", Affirmative "Expose" / Negative
   "Exclude", defaulting to expose) with a security-framed description — *not* a
   checkbox buried in the Options multiselect. As a security control it warrants
   its own visible call-out so the author makes a conscious decision.
6. **Doctor check (optional).** *(Resolved 2026-06-19 — out of scope.)* A
   name-heuristic `doctor` advisory for "sensitive-looking but un-gated"
   commands is too noisy (false positives/negatives) and orthogonal to the core
   mechanism; authors can already audit the surface with `mcp tools`. Noted for
   the roadmap.

## Related (roadmap, out of scope)

- **R-MCP-3 (SHOULD):** exposed sensitive tools default to a dry-run/confirm
  posture. ophis `Selector.Middleware` is the natural hook. Pairs with auto-set
  `destructiveHint`/`openWorldHint` (ophis `annotations.go`).
- **R-MCP-4 (SHOULD):** MCP tool results as the structured (`--json`) output
  form.

## References

- Driver: `GTB-FEATURE-REQUEST-mcp-tool-gating-2026-06-18.md`; keryx
  `docs/development/specs/0002-interface-contracts.md` §5 (`R-MCP-2`).
- ophis `v1.1.4`: `config.go` (`Config.Selectors`, nil-default, registration
  walk), `selector.go` (`Selector`/`CmdSelector`), `selectors.go`
  (`ExcludeCmds`/`AllowCmds`), `annotations.go` (native MCP hints).
- GTB: `pkg/cmd/root/root.go:639-646` (mcp block); `pkg/setup/command.go`
  (`FeatureAnnotation`/`Wrap` precedent); `internal/generator/manifest.go`
  (`ManifestCommand`), `context.go` (threading), `templates/command.go`
  (`generateNewCmdFunction`), `generator.go:83` (`SetProtection` precedent);
  `internal/cmd/{enable,disable}` and `generate/command.go:176-222`
  (`protect`/`unprotect`).
</content>
</invoke>
