---
title: "`bug-report` — redacted support-bundle diagnostics command"
description: "Add a built-in, feature-flagged `bug-report` command (alias `info`) that prints a single, paste-ready, secret-redacted support bundle: resolved config (secrets stripped), config/cache paths, tool + Go + OS versions, enabled/disabled feature flags, and the full doctor check report. Library-first: a new `pkg/cmd/bugreport` collector that reuses `pkg/cmd/doctor` checks and routes every free-form field through `pkg/redact`. Text and JSON output honour the global `--output` flag."
status: DRAFT
date: 2026-06-21
tags:
  - specification
  - diagnostics
  - support
  - redaction
  - cli
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# `bug-report` — redacted support-bundle diagnostics command

Authors
:   Matt Cockayne

Date
:   2026-06-21

Status
:   DRAFT (item B2 — awaiting human review)

## Summary

Add a new built-in, feature-flagged command — `bug-report` (with the
alias `info`) — that emits a **single, paste-ready, secret-redacted
support bundle** a user can drop straight into a GitLab/GitHub issue.
The bundle gathers, in one place, the diagnostic state a maintainer
needs to triage a misbehaving tool:

- **Tool identity & versions** — tool name/summary, the running
  binary's version/commit/build-date (`props.Version`), the Go runtime
  version (`runtime.Version()`), and a human-readable OS/arch string.
- **Resolved configuration** — the *effective* merged config
  (`config.Containable.GetViper().AllSettings()`), with every value
  passed through `redact.String` so credentials never leak.
- **Filesystem locations** — the resolved config directory
  (`setup.GetDefaultConfigDir`), the config file in use (if any), and
  the cache directory the tool would use, so a maintainer can ask the
  user to inspect the right paths.
- **Feature-flag state** — every built-in `props.FeatureCmd`, rendered
  as enabled/disabled via `props.Tool.IsEnabled`.
- **Doctor check results** — the full `doctor.DoctorReport` produced by
  reusing `doctor.RunChecks`, so the bundle subsumes a `doctor` run.

Output is **safe-by-default**: the entire bundle is redacted before it
is written, in both text and JSON form, honouring the global
`--output` flag (`text` / `json`).

This is **library-first**: the collection and redaction logic lives in a
new `pkg/cmd/bugreport` package as a reusable `Collect` /
`BugReport` type; the Cobra command is a thin renderer over it.

```sh
# Human-readable bundle to paste into an issue:
gtb bug-report

# Machine-readable for attaching as a file or piping to a gist:
gtb info --output json > bug-report.json
```

## Motivation

When a downstream tool misbehaves, the maintainer's first three asks are
always the same: *what version are you on, what does your config look
like, and what does `doctor` say?* Today the user must run `version`,
hand-redact their config, and run `doctor` separately, then stitch the
three together — error-prone, and the manual config step routinely
leaks API keys and tokens into public issues.

`doctor` already validates health (pass/warn/fail/skip) but
deliberately reports **key names only, never values** (see
`checkNoLiteralCredentials` in `pkg/cmd/doctor/checks.go`). It is a
*health verdict*, not a *state dump*. `bug-report` is the complementary
**state dump**: it shows the resolved config, the resolved paths, and
the flag matrix that `doctor` does not, and it folds in the `doctor`
verdict for completeness — all behind a single redaction perimeter so it
is safe to paste in public.

### Decision-log check: no conflict with the alias rejection

The feature-decisions log
(`docs/development/feature-decisions.md` → *Command Aliases*, 31 Mar
2026) **rejected** a framework-level *command-alias* system on the
grounds that "most shells already provide aliasing." That decision is
about user-defined *shortcuts to existing commands* and does not bear on
this feature: `bug-report` is a **distinct command with distinct
behaviour** (collection + redaction + rendering), not a renamed
shortcut. The `info` alias here is a single, framework-owned alternate
*spelling* of one command (Cobra `Aliases`), not a user-configurable
aliasing subsystem, and so does not reopen the rejected proposal.

### Security-decisions check (M-1 / L-1): safe-by-default

The security audit's M-1 / L-1 line accepts that *some* diagnostic
detail may be surfaced to an operator who can already read their own
config file and environment. `bug-report` stays inside that envelope by
being **redacted by default with no opt-out flag to disable
redaction**: the bundle is constructed from data the invoking user can
already see locally, and every free-form field is scrubbed through
`pkg/redact` before it crosses the terminal boundary. There is no
"raw"/"unredacted" mode — a maintainer who needs an unredacted value
asks the user to read a specific config key directly, never via this
command. This keeps the threat model identical to "operator reads their
own config," with the leak surface (pasting into a public issue) closed.

## Decisions to confirm in review

These are resolved as proposed below; flagged for explicit sign-off
because they touch the public API and the default command surface.

1. **Feature-flag constant & default state.** Add
   `BugReportCmd = FeatureCmd("bug-report")` in `pkg/props/tool.go`.
   **Proposed default: enabled** (added to `DefaultFeatures` and the
   `isDefaultEnabled` true-set), matching `DoctorCmd`/`ChangelogCmd`,
   because a support bundle is most valuable precisely when a user
   cannot get a tool working and a maintainer needs it on by default.
   *Alternative:* default-disabled (like `ConfigCmd`/`TelemetryCmd`) to
   keep the out-of-box surface minimal. **Recommendation: enabled.**
2. **Command name & alias.** Primary verb `bug-report`, Cobra
   `Aliases: []string{"info"}`. Confirm both spellings are wanted; if
   only one, drop the alias (see §"alias rejection" — a single alias is
   in scope, but it is optional).
3. **Cache-directory source.** GTB has no first-class cache abstraction
   (the audit rejected a `pkg/cache`). Proposed: report
   `os.UserCacheDir()` joined with `props.Tool.Name` as the
   *conventional* cache path, clearly labelled "conventional (not
   GTB-managed)". Confirm this is acceptable versus omitting the cache
   line entirely.
4. **Redaction of structured config keys.** We redact **values**
   (recursively, via `redact.String` on each leaf rendered as a string)
   but **preserve key names**, consistent with `doctor`'s key-name-only
   policy. Confirm this is the desired granularity (vs. also dropping
   keys whose name matches `redact.IsSensitiveHeaderKey`-style
   patterns). **Recommendation: redact values, keep keys; additionally
   hard-drop leaves whose key name is credential-shaped** (see R4).

## Goals

- A single command that produces a complete, paste-ready triage bundle.
- **Safe-by-default redaction** of the entire bundle (text *and* JSON),
  with no flag to disable it.
- **Library-first**: reusable collector in `pkg/cmd/bugreport`,
  reusing `doctor` checks rather than duplicating them.
- Honour the global `--output text|json` flag via `pkg/output`.
- Feature-flagged like the other built-ins, registered in
  `pkg/cmd/root`.
- ≥90% test coverage on the new `pkg/cmd/bugreport` package.

## Non-goals

- **No file/archive writing.** The bundle goes to stdout; the user
  redirects if they want a file. No tarball, no upload, no network call.
- **No new health checks.** Health verdicts are entirely delegated to
  `doctor.RunChecks`; this command does not add checks.
- **No environment-variable dump.** We do **not** enumerate the process
  environment (high leak risk, low triage value). Resolved config —
  which already reflects env overrides via Viper precedence — is
  sufficient.
- **No unredacted/raw mode.** There is no opt-out from redaction.
- **No new cache subsystem.** We only *report* a conventional path.

## Design

### Package layout

```
pkg/cmd/bugreport/
  bugreport.go        // BugReport type, Collect(), Render helpers, NewCmdBugReport
  redact.go           // recursive value-redaction over the resolved config map
  bugreport_test.go
```

### The `BugReport` type

```go
// BugReport is the fully-collected, already-redacted support bundle.
// Every string field and every value in Config has passed through
// pkg/redact before this struct is returned by Collect.
type BugReport struct {
    Tool     ToolSection            `json:"tool"`
    Runtime  RuntimeSection         `json:"runtime"`
    Paths    PathsSection           `json:"paths"`
    Features []FeatureFlag          `json:"features"`
    Config   map[string]any         `json:"config"`            // redacted, key-name-preserving
    Doctor   *doctor.DoctorReport   `json:"doctor"`            // reused verbatim
}

type ToolSection struct {
    Name    string `json:"name"`
    Summary string `json:"summary,omitempty"`
    Version string `json:"version"`
    Commit  string `json:"commit,omitempty"`
    Date    string `json:"date,omitempty"`
}

type RuntimeSection struct {
    Go   string `json:"go"`   // runtime.Version()
    OS   string `json:"os"`   // GOOS + human OS string
    Arch string `json:"arch"` // runtime.GOARCH
}

type PathsSection struct {
    ConfigDir  string `json:"config_dir,omitempty"`
    ConfigFile string `json:"config_file,omitempty"`
    CacheDir   string `json:"cache_dir,omitempty"` // conventional; labelled
}

type FeatureFlag struct {
    Cmd     props.FeatureCmd `json:"cmd"`
    Enabled bool             `json:"enabled"`
}
```

### Collection (`Collect`)

```go
func Collect(ctx context.Context, props *p.Props) *BugReport
```

Reuses existing surfaces and applies the redaction perimeter:

| Section   | Source                                                                                  |
|-----------|------------------------------------------------------------------------------------------|
| Tool      | `props.Tool.Name` / `.Summary`; `props.Version.GetVersion/GetCommit/GetDate`             |
| Runtime   | `runtime.Version()`, `runtime.GOOS`, `runtime.GOARCH`, plus a human OS string (see R6)   |
| Paths     | `setup.GetDefaultConfigDir(props.FS, props.Tool.Name)`; config file in use; cache dir    |
| Features  | iterate the known `FeatureCmd` set; `props.Tool.IsEnabled(cmd)` per entry                |
| Config    | `props.Config.GetViper().AllSettings()` → recursive redaction (see R4)                   |
| Doctor    | `doctor.RunChecks(ctx, props)` — used verbatim, then its free-form fields re-redacted     |

`Collect` is total: a nil `props.Config` or nil `props.Version` yields
empty/omitted sections, never a panic (mirrors `doctor`'s nil-guards in
`checks.go`).

### Redaction perimeter (the load-bearing invariant)

Per the project's credential-redaction rule, **every free-form string
written to a paste-able / shareable surface MUST go through
`pkg/redact`.** `Collect` is the single choke point:

- **Config map** — `redactValues` walks the `AllSettings()` map
  depth-first. For each leaf it (a) **drops** the value when the *key
  name* is credential-shaped (R4), else (b) renders the leaf to a string
  and replaces it with `redact.String(...)`. Map keys are preserved.
- **Doctor report** — `RunChecks` already returns key-name-only details,
  but `Collect` defensively re-runs `redact.String` over each
  `CheckResult.Message` and `.Details` (cheap, idempotent — `redact`
  guarantees `String(String(s)) == String(s)`).
- **Paths** — config/cache paths are `redact.String`-passed too (a home
  directory is low-risk, but a path embedded with a token via env
  expansion would be caught).

Because `redact.String` is idempotent and length-bounded, double
application across nested calls is safe and adds no risk.

### Command (`NewCmdBugReport`)

A thin renderer, mirroring `doctor.NewCmdDoctor` and
`version.NewCmdVersion`:

```go
func NewCmdBugReport(props *p.Props) *setup.Command {
    cmd := &cobra.Command{
        Use:     "bug-report",
        Aliases: []string{"info"},
        Short:   "Print a redacted, paste-ready support bundle",
        Long:    `...`,
        RunE: func(cmd *cobra.Command, _ []string) error {
            format, _ := cmd.Flags().GetString("output")
            out := output.NewWriter(os.Stdout, output.Format(format))
            report := Collect(cmd.Context(), props)
            return out.Write(output.Response{
                Status:  output.StatusSuccess,
                Command: "bug-report",
                Data:    report,
            }, func(w io.Writer) { PrintReport(w, report) })
        },
    }
    return setup.Wrap(p.BugReportCmd, cmd)
}
```

`PrintReport` renders the human-readable form: a header line (tool +
version), then labelled sections (Runtime / Paths / Features / Config /
Checks), with the doctor section reusing `doctor.PrintReport` for visual
consistency.

### Registration

In `pkg/cmd/root/root.go` `registerFeatureCommands`, alongside the
existing gated registrations:

```go
if props.Tool.IsEnabled(p.BugReportCmd) {
    rootCmd.Register(bugreport.NewCmdBugReport(props))
}
```

### Feature flag

In `pkg/props/tool.go`:

- Add `BugReportCmd = FeatureCmd("bug-report")` to the const block.
- Add `Enable(BugReportCmd)` to `DefaultFeatures` (proposed default-on).
- Add `BugReportCmd` to the `true` arm of `isDefaultEnabled`.

## Requirements

- **R1 — Single redaction choke point.** All free-form strings in the
  bundle (config values, paths, doctor messages/details) pass through
  `redact.String` inside `Collect`. No renderer (text or JSON) emits a
  field that bypassed `Collect`.
- **R2 — No opt-out.** There is no flag, env var, or config key that
  disables redaction.
- **R3 — Output formats.** `--output text` (default) and `--output
  json` both produce a redacted bundle; JSON is the `BugReport` struct
  serialised by `pkg/output`.
- **R4 — Credential-shaped keys are dropped, not shown.** A config leaf
  whose key segment matches a credential-shaped pattern (reuse the
  spirit of `redact.IsSensitiveHeaderKey` and the
  `doctor.literalCredentialKeys` list — e.g. `*.api.key`, `auth.value`,
  `app_password`, `token`, `secret`, `password`) is replaced with the
  sentinel `"<redacted>"` regardless of value, so even a malformed
  value cannot leak.
- **R5 — Reuse, don't duplicate, doctor.** Health results come from
  `doctor.RunChecks`; the command must not re-implement checks.
- **R6 — OS string.** Report a human-readable OS string. The existing
  `pkg/telemetry/osversion.go` `osVersion()` helper is unexported;
  **export an equivalent** (e.g. promote to `pkg/osinfo.Version()` or
  export `telemetry.OSVersion()`) so both telemetry and bug-report share
  one implementation. Confirm placement in review.
- **R7 — Total / panic-free collection.** Nil `props.Config`, nil
  `props.Version`, or an unresolvable config dir yield omitted/empty
  sections, never a panic.
- **R8 — Feature-flag parity.** Gated by `BugReportCmd`; disabling it
  removes the command (and its `info` alias) from the surface.
- **R9 — Coverage.** `pkg/cmd/bugreport` reaches ≥90% line coverage.

## Testing

Table-driven, `t.Parallel()`, `logger.NewNoop()`, per house style.

- **Redaction (the critical path):** seed a `config.Container` with
  literal secrets across the `literalCredentialKeys` shapes plus
  free-form values containing `sk-...`, `ghp_...`, `glpat-...`,
  bearer tokens, and URL userinfo; assert the rendered bundle (both text
  and JSON) contains **none** of the raw secrets and that
  credential-shaped keys show `<redacted>`. A focused test asserts the
  whole JSON output, scanned as a string, matches no known token prefix.
- **Reuse:** assert the bundle's `Doctor` section equals
  `doctor.RunChecks` output (modulo idempotent re-redaction).
- **Nil-safety:** `Collect` with nil Config / nil Version produces a
  valid, secret-free bundle.
- **Feature flag:** with `Disable(BugReportCmd)` the command (and the
  `info` alias) is absent from the root command tree; enabled by
  default otherwise.
- **Format parity:** `--output json` round-trips into `BugReport`;
  `--output text` renders all sections.
- **E2E (Godog):** a smoke scenario in `features/` — *Given a config
  with a literal API key, When I run `bug-report`, Then the output
  contains no raw key and exits 0* — per the project rule that new CLI
  commands ship Gherkin scenarios. A second scenario asserts the `info`
  alias resolves to the same command.

## Documentation

- New `docs/components/bugreport.md`: purpose, the redaction threat
  model (cross-reference `docs/components/redact.md`), the
  default-enabled feature flag, and how to disable it
  (`props.Disable(props.BugReportCmd)`).
- Update `docs/components/doctor.md` to point at `bug-report` as the
  "paste-into-an-issue" companion (doctor = verdict, bug-report = state
  dump + verdict).
- Add a feature-decisions note confirming this is distinct from the
  rejected command-alias proposal.
- If a `pkg/osinfo` package is introduced for R6, add its component doc.

## Open questions

1. **Default-enabled vs. default-disabled** for `BugReportCmd`
   (recommendation: enabled). — *needs sign-off*
2. **Cache-directory line**: report conventional `os.UserCacheDir()`
   path, or omit entirely given GTB has no cache subsystem?
   — *needs sign-off*
3. **Home of the shared OS-version helper** (R6): new `pkg/osinfo`, or
   export `telemetry.OSVersion`? — *needs sign-off*
4. **Alias `info`**: keep both spellings, or ship `bug-report` only?
   — *needs sign-off*

## Resolutions (open questions confirmed with user 2026-06-21)

1. **Default-enabled vs disabled** — RESOLVED: **default-DISABLED.** `BugReportCmd`
   is opt-in per tool (like `ConfigCmd`/`TelemetryCmd`): not in `DefaultFeatures`,
   in the `false` branch of `isDefaultEnabled`. Downstream tools enable it via
   `SetFeatures(Enable(BugReportCmd))`. (Departs from the draft's recommendation.)
2. **Cache-directory line** — RESOLVED: **omit it.** GTB has no cache subsystem,
   so an `os.UserCacheDir()` line would point at nothing and imply a feature that
   doesn't exist. Revisit if a cache lands.
3. **Shared OS-version helper (R6)** — RESOLVED: **new `pkg/osinfo`.** Promote the
   unexported `telemetry.osVersion()` into a neutral shared package; both
   telemetry and bug-report depend on it (avoids bug-report importing telemetry
   internals).
4. **`info` alias** — RESOLVED: **`bug-report` only**, no `info` alias. Single
   canonical name. The title/description references to an `info` alias should be
   updated to drop it. (Departs from the draft's recommendation.)
