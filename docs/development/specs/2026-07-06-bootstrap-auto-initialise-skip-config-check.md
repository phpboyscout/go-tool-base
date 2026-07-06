---
title: "Bootstrap auto-initialise and per-command config-check relaxation"
description: "Let a tool opt into auto-initialising a missing config (non-interactive) and name commands whose missing-config hard-fail is relaxed to a tolerant load, so downstream commands can own their own bootstrap without the root pre-run erroring first. Surfaced as a namespaced props.Tool.Bootstrap policy that the generator emits."
date: 2026-07-06
status: IMPLEMENTED
tags:
  - specification
  - cmd
  - props
  - bootstrap
  - config
  - generator
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Bootstrap auto-initialise and per-command config-check relaxation

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   6 July 2026

Status
:   IMPLEMENTED (open questions resolved in review 2026-07-06)

## Summary

The root command's `PersistentPreRunE` loads configuration and, when the `init`
feature is enabled, treats a **missing config file as a hard error**
(`config.ErrNoFilesFound`, "please run init …"). Because
`cobra.EnableTraverseRunHooks` is on (see
[2026-06-12 bootstrap-prerun-traversal](2026-06-12-bootstrap-prerun-traversal.md)),
the root bootstrap always runs **first**; when it returns that error, cobra
aborts the whole invocation and **no downstream `PreRunE`/`PersistentPreRunE`
ever runs**. A tool cannot currently arrange for a subcommand to bootstrap its
own config on first run.

This spec adds two opt-in behaviours, grouped under a single new
`props.Tool.Bootstrap` policy field:

1. **`AutoInitialise`** — when the config file is missing, the framework runs a
   **non-interactive** `init` inline (writing the default localised config),
   then re-loads config and continues normally, instead of erroring.
2. **`SkipConfigCheck`** — a list of command names (plus an equivalent
   `setup.SkipConfigCheck` command annotation) whose missing-config **hard-fail
   is relaxed to a tolerant load** (fall back to the embedded default config,
   exactly like the `init`-disabled path today), so the command's own
   `PreRunE`/`PersistentPreRunE` runs and can manage bootstrap itself.

Both are emitted by the generator into the scaffolded `props.Tool{…}` literal
alongside `Features:`.

## Motivation

The request comes from the **krites** downstream. krites enables `init`
(its config is localised and required), and added a `PreRunE` on its `studio`
command to auto-initialise config when `studio` runs. That hook never fires:
the root bootstrap loads config first, hits `ErrNoFilesFound`, and aborts before
`studio`'s hook is reached. There is no supported way today to either (a) have
the framework heal a missing config, or (b) let a specific command bypass the
missing-config gate so it can heal it itself.

This is a general first-run ergonomics gap, not krites-specific: any tool whose
primary command should "just work" on first invocation by materialising a
default config wants one of these two knobs.

## Relationship to the bootstrap-traversal decision (important)

[2026-06-12 bootstrap-prerun-traversal](2026-06-12-bootstrap-prerun-traversal.md)
(IMPLEMENTED) established **D2 — bootstrap always runs; no opt-out**: there is
deliberately no annotation that skips the framework bootstrap, because silently
skipping bootstrap (config load, telemetry, update check) is a footgun.

This spec does **not** reintroduce that footgun. Neither knob skips bootstrap:

- `SkipConfigCheck` keeps the full bootstrap pipeline running (logging,
  telemetry, update check, config load). It only changes the **missing-config
  outcome** from *fatal error* to *tolerant fallback to embedded defaults* — the
  identical treatment a tool with `init` disabled already gets today
  (`root.go:611`, `allowEmpty`). `props.Config` is always populated.
- `AutoInitialise` also keeps bootstrap running; it makes the missing-config
  case **self-healing** (init then load) rather than fatal.

The distinction the spec must preserve: **we relax the config-existence *error*,
we never skip *bootstrap*.** This keeps D2's invariant (`props.Config` is never
silently nil) intact.

## Design decisions

### D1 — A single namespaced `Tool.Bootstrap` policy struct

Rather than adding two ad-hoc booleans/slices directly to `Tool` (which already
carries many fields), group all config-bootstrap lifecycle behaviour under one
new field, `Bootstrap props.BootstrapPolicy`. This:

- keeps `Tool` uncluttered — one cohesive field, not N scattered knobs;
- is future-proof — later bootstrap-lifecycle permutations (e.g. a
  `RequireConfig` allowlist, a custom bootstrap hook) land inside
  `BootstrapPolicy` without touching `Tool`;
- keeps the `Feature{Cmd, Enabled}` type honest — we do **not** overload the
  enable/disable feature toggle to carry unrelated data;
- is zero-value safe — an omitted `Bootstrap` reproduces today's behaviour
  exactly, so there is no migration for existing tools;
- still lands "in the generated `Tool` definition" — the generator emits it as a
  nested literal right next to `Features:`.

### D2 — `SkipConfigCheck` identification: name **or full path**, list **and** annotation

*(O1 and O2 resolved in review: ship both mechanisms; match on both name and path.)*

`SkipConfigCheck []string` matches an entry against **either** `cmd.Name()`
(cobra's first word of `Use`, so it is not confused by an arg suffix) **or**
`cmd.CommandPath()` (the space-joined path from root, e.g. `"krites studio"`).
Supporting the full path resolves the collision risk raised in review: two
commands named `studio` at different levels of the tree would both match a bare
`"studio"` entry, which is undesirable — an author disambiguates by listing the
full path instead. A bare name remains accepted for the common unambiguous case.

The codebase already flags raw `cmd.Use` string matching as fragile (see
`setup/update.go:515`), so we **also** provide a rename-safe **annotation**,
`setup.SkipConfigCheck(cmd)`, mirroring the existing `setup.Wrap`/`setup.FeatureOf`
pattern the root pre-run already prefers for identifying the `init` command
(`root.go:599`). The pre-run relaxes the check when the command is **either**
annotated **or** matched (by name or path) in the policy list.

- **Declarative** (`[]string` in the `Tool` block) — visible in scaffolded
  output, good for generator wiring; accepts a bare name or a full command path.
- **Robust** (annotation on the command) — survives renames, no string coupling,
  set where the command is defined; the recommended form when collisions are a
  concern.

Custom commands are author-owned, so a string identifier is unavoidable for the
declarative path; the annotation is the escape hatch for authors who want the
fully robust form.

### D3 — `AutoInitialise` runs a non-interactive bootstrap

When `AutoInitialise` fires, it runs `setup.Initialise` with credential wizards
suppressed — equivalent to `init --skip-login --skip-key` with
`Interactive: false` — writing the default localised config **silently** so a
first-run `studio` launch is seamless. The tool author can still run the full
interactive `init` later to configure credentials. Full-interactive auto-init
was rejected: prompting mid-launch on first run is a worse first-run experience,
and credentials are not required to materialise a usable default config.

### D4 — Precedence: `SkipConfigCheck` over `AutoInitialise`

If a command is in `SkipConfigCheck` (or annotated), the config load tolerates a
missing file (no error), so `AutoInitialise` never triggers for that command —
the command has declared it owns bootstrap. Documented precedence:
`init` early-return  >  `SkipConfigCheck`  >  `AutoInitialise`  >  default
(hard-fail when `init` enabled).

### D5 — `AutoInitialise` is gated on the `init` feature

`AutoInitialise` is only honoured when `Tool.IsEnabled(InitCmd)` is true — it
reuses the `init` flow, so a tool that has disabled `init` (and therefore
already tolerates empty config via `allowEmpty`) gains nothing from it and it is
a no-op. This keeps the two mechanisms (init-disabled tolerant fallback vs
init-enabled auto-heal) from overlapping confusingly.

## Public API

### `pkg/props` — new policy type and `Tool` field

```go
// BootstrapPolicy governs how the framework treats configuration bootstrap and
// the missing-config gate in the root pre-run. The zero value reproduces the
// historical behaviour: a missing config file is a hard error when the init
// feature is enabled.
type BootstrapPolicy struct {
    // AutoInitialise, when true and the init feature is enabled, runs a
    // non-interactive init (writing the default config) instead of failing when
    // no config file is found, then continues with the freshly written config.
    AutoInitialise bool `json:"auto_initialise,omitempty" yaml:"auto_initialise,omitempty"`

    // SkipConfigCheck lists commands whose missing-config hard-fail is relaxed
    // to a tolerant load: the pre-run falls back to the embedded default config
    // rather than erroring, so the command's own PreRunE can manage bootstrap.
    // An entry matches a command when it equals either the command's Name() or
    // its full CommandPath() (e.g. "studio" or "krites studio") — the latter
    // disambiguates same-named commands at different tree levels. A command may
    // also be marked via the setup.SkipConfigCheck annotation; either mechanism
    // relaxes the check.
    SkipConfigCheck []string `json:"skip_config_check,omitempty" yaml:"skip_config_check,omitempty"`
}

// matchesSkipList reports whether a command identified by name and path is
// listed in SkipConfigCheck (an entry may be a bare Name() or a full
// CommandPath()). The annotation half is evaluated separately by the caller
// (setup.SkipsConfigCheck) to avoid a props->setup import cycle; see D2 note.
func (b BootstrapPolicy) matchesSkipList(name, path string) bool
```

```go
type Tool struct {
    // …existing fields…
    Features  []Feature       `json:"features" yaml:"features"`
    Bootstrap BootstrapPolicy `json:"bootstrap,omitempty" yaml:"bootstrap,omitempty"`
    // …
}
```

> Import-cycle note: `props` must not import `setup` (setup already imports
> props). The annotation getter therefore lives in `setup`, and the root
> pre-run combines
> `setup.SkipsConfigCheck(cmd) || tool.Bootstrap.matchesSkipList(cmd.Name(), cmd.CommandPath())`.
> The exported surface on `BootstrapPolicy` is kept minimal accordingly.

### `pkg/setup` — command annotation (mirrors `Wrap`/`FeatureOf`)

```go
// skipConfigCheckAnnotation marks a command whose missing-config gate the root
// pre-run should relax to a tolerant load.
const skipConfigCheckAnnotation = "gtb.skip_config_check"

// SkipConfigCheck stamps cmd so the root pre-run relaxes its missing-config
// gate. Returns cmd for chaining, mirroring setup.Wrap.
func SkipConfigCheck(cmd *cobra.Command) *cobra.Command

// SkipsConfigCheck reports whether cmd carries the skip-config-check annotation.
func SkipsConfigCheck(cmd *cobra.Command) bool
```

Usage in a downstream tool:

```go
// Declarative (emitted by the generator):
props.Tool{
    Name: "krites",
    Features: props.SetFeatures(props.Enable(props.InitCmd)),
    Bootstrap: props.BootstrapPolicy{
        AutoInitialise:  true,             // heal missing config on first run
        // or, instead of AutoInitialise, hand bootstrap to the command:
        SkipConfigCheck: []string{"studio"},
    },
}

// Robust (set where the command is defined):
setup.SkipConfigCheck(studioCmd)
```

## Internal implementation

All changes are in the root pre-run (`pkg/cmd/root/root.go`, `newRootPreRunE`,
around lines 607–626) plus the two new symbols above.

Revised gate (sketch):

```go
tool := props.Tool
initEnabled := tool.IsEnabled(p.InitCmd)

skipCheck := setup.SkipsConfigCheck(cmd) ||
    tool.Bootstrap.matchesSkipList(cmd.Name(), cmd.CommandPath())
autoInit  := tool.Bootstrap.AutoInitialise && initEnabled && !skipCheck

// allowEmpty already tolerates a missing file for init-disabled tools.
// SkipConfigCheck extends that tolerance per-command; AutoInitialise does NOT
// (it wants to detect the missing file and heal it, then load for real).
allowEmpty := tool.IsDisabled(p.InitCmd) || skipCheck

cfg, err := loadAndMergeConfig(ConfigLoadOptions{ /* … */, AllowEmpty: allowEmpty })
if err != nil {
    if autoInit && errors.Is(errors.Cause(err), config.ErrNoFilesFound) {
        dir := setup.GetDefaultConfigDir(props.FS, tool.Name)
        if dir == "" {
            return errors.Wrap(err, "auto-initialise: cannot resolve config dir")
        }
        no := false
        if _, ierr := setup.Initialise(props, setup.InitOptions{
            Dir: dir, SkipLogin: true, SkipKey: true, SkipAI: true, Interactive: &no,
        }); ierr != nil {
            return errors.Wrap(ierr, "auto-initialise failed")
        }
        cfg, err = loadAndMergeConfig(ConfigLoadOptions{ /* … */, AllowEmpty: false })
    }
    if err != nil {
        return errors.Wrap(err, "failed to load configuration")
    }
}
props.Config = cfg
// …unchanged: bindCommandFlags, validateConfig, configureLogging, telemetry, update check…
```

Notes:

- `loadAndMergeConfig` already falls back to `setup.DefaultConfig` when
  `AllowEmpty` is true (`root.go:163-178`), so the `SkipConfigCheck` path needs
  no new load logic — only the `allowEmpty` term changes.
- The auto-init retry uses `AllowEmpty: false` so a genuinely broken init
  (wrote nothing) still surfaces an error rather than silently masking it.
- Bootstrap continues (telemetry/update check) after either path — D2 preserved.

## Generator impact

`internal/generator/templates/skeleton_root.go`:

- Extend `SkeletonRootData` with `AutoInitialise bool` and `SkipConfigCheck []string`.
- In `buildToolDict` (lines 126-170), emit a `Bootstrap: props.BootstrapPolicy{…}`
  nested literal when either is set (mirroring the existing conditional emission
  of `Features:` at lines 163-165). Omit the field entirely when both are zero,
  so default scaffolds are unchanged.

`internal/generator/features.go` / manifest schema:

- Add manifest fields for `auto_initialise` and `skip_config_check` under the
  tool/bootstrap section so `generate`/`regenerate` round-trip them and
  `regenerateRootCommand` re-emits them. (Feature *toggles* remain the existing
  enable/disable set; these are bootstrap-policy values, not feature toggles.)

Regenerate verification: after changes, `go run ./cmd/gtb generate <command> -p tmp`
with a manifest setting both, confirm the emitted `props.Tool{…}` compiles and
contains the nested `Bootstrap` literal, then delete `tmp/`.

## Error cases

| Condition | Behaviour |
|-----------|-----------|
| Config missing, `init` enabled, no policy set | Unchanged: `ErrNoFilesFound` hard error ("please run init"). |
| Config missing, command in `SkipConfigCheck`/annotated | Tolerant load: embedded default config; no error; command's own hook runs. |
| Config missing, `AutoInitialise` on, `init` enabled | Runs non-interactive `Initialise`, re-loads; error only if init or reload fails. |
| `AutoInitialise` on but `init` disabled | No-op (D5); tool already tolerates empty config via `allowEmpty`. |
| `AutoInitialise` on but `GetDefaultConfigDir` returns "" (no HOME) | Wrapped error: cannot resolve config dir for auto-init. |
| `Initialise` fails (e.g. FS write error) | Wrapped `auto-initialise failed` error; bootstrap aborts (correct — cannot proceed without config). |
| Both `SkipConfigCheck` (for this cmd) and `AutoInitialise` set | `SkipConfigCheck` wins (D4): tolerant load, no auto-init. |

All errors created/wrapped with `cockroachdb/errors` per project standard.

## Testing strategy

### Unit (`pkg/props`)

- `BootstrapPolicy` zero-value = historical behaviour; `matchesSkipList` matches
  an entry against both `Name()` and `CommandPath()`, exact and case-sensitive;
  a bare-name entry does not match a differently-pathed same-named command when
  the entry is written as a full path.
- JSON/YAML round-trip of `Tool.Bootstrap` (omitempty when zero).

### Unit (`pkg/setup`)

- `SkipConfigCheck`/`SkipsConfigCheck` annotation set/read; unrelated commands
  report false. Table-driven, `t.Parallel()`.

### Unit (`pkg/cmd/root`)

- Missing config + `SkipConfigCheck` name → `props.Config` populated from
  embedded default, no error, downstream hook reached.
- Missing config + annotation → same.
- Missing config + `AutoInitialise` → `Initialise` invoked once, config file
  written to the resolved dir (afero in-memory FS), `props.Config` reflects the
  written file, downstream runs.
- `AutoInitialise` + `init` disabled → no `Initialise` call (D5).
- Precedence: command both in skip list and `AutoInitialise` on → no
  `Initialise` call (D4).
- Auto-init dir unresolved / `Initialise` error → wrapped error surfaces.
- Regression: default (no policy) still hard-fails with `ErrNoFilesFound`.

Coverage target ≥90% for new `pkg/` code.

### Generator

- `buildToolDict` emits the nested `Bootstrap` literal only when set; default
  manifest omits it. Golden-file / rendered-output assertion.

### E2E BDD (Godog) — evaluated, covered by cobra-level tests

*Decision (implementation):* a dedicated Godog scenario was **not** added. This
is transparent framework bootstrap, not a new CLI command or service-lifecycle
change (the categories CLAUDE.md makes Gherkin mandatory for). The behaviour is
already exercised **end-to-end through the real cobra command tree** in
`pkg/cmd/root/bootstrap_prerun_test.go`: each test builds a tool via `NewCmdRoot`
with a registered child command and calls `ExecuteContext`, asserting the
observable outcome (command runs / hard-fails, `props.Config` populated, config
file written or not). That gives the same "drive the CLI, observe behaviour"
coverage a smoke scenario would, without the Godog harness overhead. The
reference scenario below is retained for illustration should a downstream want it:

```gherkin
Scenario: auto-initialise materialises config on first run
  Given a tool with init enabled and bootstrap auto-initialise on
  And no configuration file exists
  When I run the "studio" command
  Then a default configuration file is created
  And the command runs without a "please run init" error
```

## Migration & compatibility

- **Backwards compatible.** New field is additive and zero-value safe; existing
  tools and scaffolds are byte-for-byte unchanged when `Bootstrap` is omitted.
- Pre-1.0, so even were a break needed it would be a minor bump — but none is.
- Add a short note under `docs/components/` (props/Tool + command-middleware) and
  a how-to snippet showing the krites first-run pattern. No `docs/migration/`
  entry needed (non-breaking).

## Future considerations

- A `RequireConfig []string` inverse (commands that must have real config even
  when the tool otherwise tolerates empty) — natural future member of
  `BootstrapPolicy`.
- A pluggable bootstrap hook (`func(*props.Props) error`) for tools whose
  first-run needs more than `init` — deferred until a concrete need appears.
- Unifying the fragile `update/auth/init/version` update-check skip list
  (`setup/update.go:515`) onto the same annotation mechanism — related cleanup,
  out of scope here.

## Open questions

*All resolved in review (2026-07-06).*

1. **O1 — Annotation vs list, ship both?** *Resolved: **ship both** — declarative
   `[]string` for the generator, `setup.SkipConfigCheck` annotation for robustness.* (D2)
2. **O2 — Match on `cmd.Name()` or full `cmd.CommandPath()`?** *Resolved: **both** —
   an entry matches either `Name()` or `CommandPath()`, so authors disambiguate
   same-named commands at different levels with the full path.* (D2)
3. **O3 — Should `AutoInitialise` also `SkipAI`?** *Resolved: **yes** — auto-init is
   credential/provider-free (`SkipLogin`, `SkipKey`, `SkipAI`); explicit `init`
   configures those later.* (D3)
4. **O4 — Manifest surface.** *Resolved: **yes, expose in the manifest** and honour
   it in the generator, for round-trip parity with `Features`.* (Generator impact)

## Out of scope

- Skipping the framework **bootstrap** itself (config load / telemetry / update
  check) — explicitly forbidden by 2026-06-12 D2 and not proposed here.
- Reworking the update-check skip list (noted as future cleanup).
- Interactive auto-init (rejected per D3).

## Related

- [2026-06-12 bootstrap-prerun-traversal](2026-06-12-bootstrap-prerun-traversal.md)
  — establishes `EnableTraverseRunHooks` and the "bootstrap always runs" invariant.
- [2026-03-28 godog-bdd-strategy](2026-03-28-godog-bdd-strategy.md) — BDD suitability.
- `pkg/cmd/root/root.go` (`newRootPreRunE`), `pkg/setup/init.go` (`Initialise`,
  `InitOptions`, `GetDefaultConfigDir`), `pkg/props/tool.go` (`Tool`, features),
  `internal/generator/templates/skeleton_root.go` (`buildToolDict`).
