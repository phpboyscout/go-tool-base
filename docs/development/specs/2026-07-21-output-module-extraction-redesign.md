---
title: "Extract and redesign pkg/output into the standalone go/output module"
description: "Extract go-tool-base's pkg/output (structured CLI output: JSON/text/table/spinner/progress/status renderers, ~1261 LOC) into gitlab.com/phpboyscout/go/output, folding in a broad redesign rather than a verbatim repoint. The core module is made cobra- AND pflag-free by having Emit/EmitError take (io.Writer, Format) instead of *cobra.Command; the tiny flag->writer/format binding moves to an opt-in go/output/cobra subpackage (cobra in go.mod, never linked unless imported, root guarded cobra-free). The redesign injects the output writer and interactivity via functional options (removing hardcoded os.Stderr / CI-env globals), unifies Status/Progress/Spinner styling, applies a Unicode display-width correctness pass, and extends the full Format matrix (YAML/CSV/TSV/Markdown) beyond TableWriter. pkg/output has zero go-tool-base imports and no GTB config/adapter, so the GTB side is a clean repoint of ~13 call sites across pkg/cmd/*."
date: 2026-07-21
status: IMPLEMENTED
tags:
  - specification
  - output
  - module-extraction
  - redesign
  - cli
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract and redesign `pkg/output` into the standalone `go/output` module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-21

Status
:   IMPLEMENTED (2026-07-21). `go/output` v0.1.0 published; GTB cut-over MR !278
    merged (deleted `pkg/output`, migrated 14 call sites to the façade + `ocobra`).
    §9 open questions resolved with the maintainer: **Q1 = single public `Renderer`
    façade** (§5); Q2 = full-matrix `Writer` in v0.1.0; Q3 = minimal public `Theme`;
    Q4 = keep the charm TUI stack in the core; Q5 = `output/cobra` offers
    `RegisterOutputFlag`. Module coverage 95.9%; docs live at
    `output.go.phpboyscout.uk`.

Related
:   [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5 procedure, §10/§11 findings),
    [workspace + changelog extraction](2026-07-21-leaf-module-extraction-workspace-changelog.md) (the most recent pure-repoint leaf batch),
    [extraction report](../reports/2026-07-07-package-extraction-report.md) (readiness checklist — `output` listed "Ready now, with an upgrade")

---

## 1. Summary

`pkg/output` is a **clean, framework-free leaf**: it has **zero** `go-tool-base`
imports, uses `cockroachdb/errors` for errors, owns no GTB config schema and no
`*FromProps` adapter. By the playbook's §11.1 test ("does GTB own an adapter or
config schema on top of the package?" — no) it is a **pure repoint**.

Unlike the previous leaf batch (workspace, changelog — moved verbatim), `output`
is extracted **with a deliberate redesign** ("broad redesign" scope, chosen
2026-07-21). The extraction is the moment to fix accumulated design debt:

1. **Decouple from cobra *and* pflag.** Today three functions (`IsJSONOutput`,
   `Emit`, `EmitError`) take a `*cobra.Command` purely to read the `--output`
   flag value and the output writer. The redesigned core takes plain
   `(io.Writer, Format)` — no flag-library dependency at all — and the tiny
   cobra binding moves to an **opt-in `go/output/cobra` subpackage**.
2. **Remove hardcoded global state.** `NewStatus`, `NewProgress`, `Spin`,
   `IsInteractive`, `RenderMarkdown` reach directly for `os.Stderr` / `os.Stdout` /
   `os.Stdin` and the `CI` env var. The redesign injects the writer and
   interactivity via functional options (defaults preserved), making every helper
   testable without global-state hacks.
3. **Unify the progress-family renderers.** `Status`, `Progress`, and the
   `Spin` spinner are three independently-styled implementations (raw ANSI escapes
   in `Status`, hand-built ASCII bars in `Progress`, a bubbles spinner in `Spin`).
   The redesign gives them a shared, themeable rendering seam and consistent
   Unicode display-width handling (`ansi.StringWidth`, as `TableWriter` already
   uses).
4. **Extend the `Format` matrix beyond tables.** `TableWriter` already honours
   `JSON/YAML/CSV/TSV/Markdown/Text`; the top-level `Writer.Write` only handles
   JSON-vs-text. The redesign makes `Writer` honour the full matrix.

The published module becomes a **proper reusable CLI-output package** usable by
any tool (cobra, urfave/cli, or a raw HTTP handler), with the framework coupling
isolated where it can be swapped or dropped.

## 2. Current state — inventory

`pkg/output`, 8 production files, **~1261 LOC** (3030 incl. tests):

| File | LOC | Public surface | Notes |
|---|---|---|---|
| `output.go` | 169 | `Format`, `Response`, `Writer`, `NewWriter`, `Write`, `IsJSON`, `RenderMarkdown`, `Render`, **`IsJSONOutput`/`Emit`/`EmitError`** | glamour markdown render; **the only cobra users** |
| `table.go` | 635 | `Column`, `TableWriter`, `NewTableWriter`, `With*` options, `WriteRows` | full Format matrix; `ansi.StringWidth` already used |
| `spinner.go` | 170 | `Spin`, `SpinWithResult[T]` | bubbles/bubbletea spinner + plain fallback; Ctrl-C→`context.Canceled` |
| `status.go` | 93 | `Status`, `NewStatus`, `Update`/`Success`/`Warn`/`Fail`/`Done` | **raw `\r\033[K` escapes**; static `iconSpin` frame |
| `progress.go` | 121 | `Progress`, `NewProgress`, `Increment`/`IncrementBy`/`Done` | **hand-built ASCII `=`/`>` bar** |
| `interactive.go` | 18 | `IsInteractive` | `CI=true` OR non-TTY → non-interactive |
| `doc.go` | 55 | — | package doc |

External deps: `charm.land/{bubbles,bubbletea,glamour,lipgloss}/v2`,
`github.com/charmbracelet/x/{ansi,term}`, `github.com/cockroachdb/errors`,
`github.com/spf13/cobra`, `gopkg.in/yaml.v3`.

GTB consumers (13 files, all in `pkg/cmd/*` + `pkg/docs/`):

- **cobra-helper users** (`Emit`/`IsJSONOutput`): `cmd/changelog`, `cmd/config/{get,path,unset}`, `cmd/initialise`, `cmd/update`.
- **core users** (`NewWriter`/`RenderMarkdown`/`NewTableWriter`/`Spin`/`Progress`/`Status`): the above plus `cmd/config/list`, `cmd/docs/ask`, `cmd/doctor/{doctor,report}`, `cmd/version`, `pkg/docs/ask`.

The `--output` flag is a **root persistent flag**
(`pkg/cmd/root/root.go:886` — `PersistentFlags().String("output", "text", …)`),
so every command already resolves format the same way.

## 3. Module shape & naming

Per the playbook §2:

| Concern | Value |
|---|---|
| Core module | `gitlab.com/phpboyscout/go/output` |
| Opt-in cobra binding | `gitlab.com/phpboyscout/go/output/cobra` (subpackage, same module) |
| GitLab project | `phpboyscout/go/output` |
| Docs microsite | `output.go.phpboyscout.uk` |
| Package name | `package output` (core), `package cobra` (subpackage) |

**No branding in the fresh repo** (per the workspace lesson): scaffold from the
minimal shared toolkit header only — no logo/favicon/docs images.

### 3.1 Why a subpackage, not a companion module

`output/cobra` stays a **subpackage of the same module**, not a separate
`output-cobra` module, because it is a *thin binding over the core's own types*
(unlike the `chat-<provider>` / `transport-openapi` companions, which carry heavy
independent dependency graphs). cobra is lightweight and lands in the core
module's `go.mod` but is **never compiled/linked** into a consumer that does not
import the subpackage (Go package-level dead-code elimination). The
`depfootprint_test.go` guard asserts `go list -deps .` (the **root** package)
excludes cobra/pflag; the subpackage legitimately imports cobra and is excluded
from that assertion.

## 4. The `Renderer` façade (Q1 resolved — single public entry point)

The redesign's organising decision: replace the scattered constructors
(`NewWriter`, `NewStatus`, `NewProgress`, `Spin`, `NewTableWriter`) and the loose
cobra functions (`Emit`/`EmitError`/`IsJSONOutput`) with **one configured
façade**. The output *destination*, *format*, *theme*, and *interactivity* are
configured **once** on the `Renderer`; every method reads them, so no method
re-takes `(w, format)`.

```go
// Core (go/output) — no cobra, no pflag.
func New(opts ...Option) *Renderer

type Option func(*config)
func WithWriter(w io.Writer) Option      // default os.Stderr
func WithFormat(f Format) Option         // default FormatText
func WithInteractive(v bool) Option      // default: auto-detect (TTY && !CI)
func WithTheme(t Theme) Option           // default DefaultTheme (§5.3)

// Structured / envelope output — format-aware (full matrix, §5.4):
func (r *Renderer) Write(data any, textFunc func(io.Writer)) error
func (r *Renderer) Table(rows any, opts ...TableOption) error
func (r *Renderer) Render(markdown string) error           // glamour
func (r *Renderer) Emit(resp Response) error               // JSON envelope; no-op unless FormatJSON
func (r *Renderer) EmitError(command string, err error) error
func (r *Renderer) IsJSON() bool

// Progress family — share the Renderer's writer/theme/interactivity:
func (r *Renderer) Status() *Status
func (r *Renderer) Progress(total int, description string) *Progress
func (r *Renderer) Spin(ctx context.Context, msg string, fn func(context.Context) error) error
func SpinWithResult[T any](r *Renderer, ctx context.Context, msg string, fn func(context.Context) (T, error)) (T, error)
```

`SpinWithResult` stays a free generic function taking the `*Renderer` (Go methods
cannot add type parameters). `IsInteractive()` remains exported as the default
detector `WithInteractive` falls back to.

### 4.1 Cobra/pflag decoupling — the façade makes it trivial

The core façade takes no flag library. The opt-in `go/output/cobra` subpackage is
the **only** cobra user: it builds a core `*Renderer` from a command by reading
the two things the old helpers used — `cmd.OutOrStdout()` and the `--output`
flag.

```go
package cobra // import ".../go/output/cobra", aliased `ocobra` at call sites

// NewRenderer builds a core Renderer: writer = cmd.OutOrStdout(),
// format = the --output flag (default FormatText), plus any extra opts.
func NewRenderer(cmd *cobra.Command, opts ...output.Option) *output.Renderer

// RegisterOutputFlag adds a consistent persistent --output flag (Q5 resolved):
// default "text", value set text,json,yaml,csv,tsv,markdown.
func RegisterOutputFlag(cmd *cobra.Command)
```

A GTB call site collapses from `output.Emit(cmd, resp)` to:

```go
return ocobra.NewRenderer(cmd).Emit(output.Response{Status: output.StatusSuccess, Command: "update", Data: result})
```

Non-cobra consumers (HTTP handlers, urfave/cli, tests) call `output.New(...)`
directly. The `depfootprint_test.go` guard asserts the **root** package excludes
cobra/pflag; the subpackage is excluded from that assertion.

## 5. The redesign (broad scope)

### 5.1 Injectable writer + interactivity

Configured once on the `Renderer` (§4) via `WithWriter` / `WithInteractive`;
**defaults preserve today's behaviour** (`os.Stderr` + auto-detect). This kills
the `os.Getenv("CI")` / `term.IsTerminal(os.Stdout.Fd())` reach-through inside the
render calls and makes every path deterministically testable — no env fiddling,
no global stdout swap.

### 5.2 Unified progress-family rendering + Unicode correctness

`Status` (raw `\r\033[K`), `Progress` (ASCII `=`/`>`), and `Spin` (bubbles) share
no rendering seam today. Behind the façade they share one internal renderer that:

- Uses `ansi`-aware display width everywhere a message or bar cell is measured or
  truncated (matching `TableWriter`), so multi-byte / wide-rune / emoji messages
  don't corrupt the line.
- Routes line-clear/redraw through lipgloss rather than hand-written escapes.
- Gives `Status`'s "working" state a real animated spinner frame source (shared
  with `Spin`) instead of the static `iconSpin = "⠋"`.

### 5.3 Theming (Q3 resolved — minimal public `Theme`)

A public `Theme` value (icon set + lipgloss styles for success/warn/fail/spinner/
bar) with `DefaultTheme`, injectable via `WithTheme`. Replaces the package-level
`spinnerStyle` var and the hardcoded `iconSuccess/Warn/Fail` consts. Downstream
tools match their own palette from day one. Kept minimal (icons + a few styles) to
limit the pre-1.0 stability surface.

### 5.4 `Writer` honours the full Format matrix (Q2 resolved — in v0.1.0)

`Renderer.Write` honours `JSON/YAML/CSV/TSV/Markdown/Text` by delegating
structured formats to the same rendering core `TableWriter` already uses, so the
façade is format-complete and `Table` is a specialisation rather than the only
full-matrix path. This is the largest behavioural change; the cut-over verifies no
existing text output shifts unexpectedly.

## 6. Migration procedure (playbook §5 applied)

1. **Bootstrap** `phpboyscout/go/output` to §3 (branding-free; cicd components at
   GTB's current pin **v0.24.1**, Go **1.26.5**; `depfootprint_test.go` guarding
   the root cobra/pflag-free; **no** `goreleaser` — library).
2. **Move + redesign the code** into the module (core + `cobra/` subpackage).
   Carry all pure in-package tests; add tests for the new option seams and the
   Unicode-width paths **test-first**. Keep **≥90% coverage**.
3. **Docs (Diátaxis) + Pages:** minimal microsite (getting-started + a how-to per
   renderer family + an explanation of the format matrix); `zensical-pages`;
   custom domain + LE cert (**Pages visibility public**, §11.9).
4. **Shared transitive alignment (§10.6/§11.5):** after `go mod tidy`, diff every
   shared transitive against GTB and bump to match (expect `x/sys`, `x/text`,
   `x/crypto`, `cockroachdb/errors`, the charm.land set). Fold bumps into the
   bootstrap MR; expect `osv-scanner` to flag a transitive the lean graph exposes.
5. **Cut `v0.1.0`** via the Release-MR flow (human-gated).
6. **GTB cut-over (single MR, `feat`):** add the dependency; **delete `pkg/output`**;
   repoint + migrate the 13 callers to the façade — core users construct
   `output.New(WithWriter(w), WithFormat(f), …)` and call methods; the 6
   cobra-helper sites use `ocobra.NewRenderer(cmd)` (aliased `ocobra` to avoid the
   package-name clash with `spf13/cobra`) and drop their hand-rolled
   `cmd.Flags().GetString("output")`; where GTB registers `--output` it may adopt
   `ocobra.RegisterOutputFlag`. Run `just ci`; add a
   `docs/reference/migration/v0.x-output-extracted.md` note (the API is
   redesigned, not just repointed — the migration note documents the old→new
   mapping for downstreams).
   **Commit type is `feat(output):`** — deleting a public `pkg/` is a breaking
   change and pre-1.0 ships as a minor; `refactor` would neither bump nor
   changelog it (see [[feedback_cutover_commit_type]]).
7. **GTB docs cross-reference** the microsite (§4.1): stub the component page,
   update the components index + README, sweep `docs/` for stale references.

## 7. Testing, guards, coverage

- Pure in-package tests move with the code (they import only the package +
  stdlib + testify). No cross-GTB-coupled tests exist in `pkg/output` (verified:
  zero go-tool-base imports), so **nothing stays behind** — this is a clean move.
- New seams (options, theme, Unicode width, full-matrix `Writer`, the `cobra`
  subpackage binding) are covered test-first to hold ≥90%.
- `depfootprint_test.go` (root pkg): forbids `go-tool-base`, `spf13/viper`,
  `spf13/pflag`, `spf13/cobra`, OTel, cloud SDKs. The `cobra` subpackage is
  excluded from the root assertion.
- **No mocks shipped** unless the module's own tests need them (§11.4).
- GTB side: deleting `pkg/output` removes it from `go list`; there is **no GTB
  adapter package to re-cover** (§11.7 — pure repoint), so no coverage backfill is
  needed GTB-side beyond the callers still compiling.

## 8. Downstream

afmpeg and any other GTB-based tools that import `pkg/output` repoint to
`go/output` (+ `go/output/cobra` where they emit JSON envelopes) as a normal
pre-1.0 import change.

## 9. Open questions — RESOLVED (2026-07-21)

All five resolved with the maintainer before implementation:

- **Q1 — Unified renderer depth. → Single public `Renderer` façade** (§4). Replaces
  the scattered constructors and loose cobra functions with one configured entry
  point. Larger blast radius on GTB's 13 call sites (accepted) in exchange for a
  coherent, format-complete API and a trivial cobra decoupling.
- **Q2 — Full-matrix `Writer`. → Include in v0.1.0** (§5.4). The redesign is the
  moment; the cut-over verifies no existing text output shifts.
- **Q3 — Theming public surface. → Ship a minimal public `Theme`** (§5.3): icons +
  a few lipgloss styles, `DefaultTheme`, `WithTheme`.
- **Q4 — charm TUI stack in core. → Keep in core for v0.1.0.** The unified façade
  renderer leans on it anyway; splitting fights the unification. Revisit only if
  the graph proves heavy.
- **Q5 — `--output` flag registration. → `go/output/cobra` offers
  `RegisterOutputFlag(cmd)`** (§4.1), default `text`, value set
  `text,json,yaml,csv,tsv,markdown`.

## 10. Acceptance criteria

1. `gitlab.com/phpboyscout/go/output` + `go/output/cobra` published at `v0.1.0`;
   resolve from the proxy; build in a throwaway consumer (`-buildvcs=false`).
2. `depfootprint_test.go` green: root package free of cobra/pflag/viper/OTel/
   go-tool-base; module ≥90% coverage.
3. Docs live at `output.go.phpboyscout.uk` (LE cert issued); landing-site row
   added; GTB component page stubbed and cross-referenced.
4. GTB cut-over MR (`feat(output):`) deletes `pkg/output`, repoints all 13
   callers, `just ci` green; migration note added.
5. The redesign items accepted in §9 are implemented and covered.
