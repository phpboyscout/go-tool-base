---
title: "Delete pkg/forms and rewrite the generator wizards on native huh v2"
description: "pkg/forms is an internal-only wrapper (used solely by the scaffolding generator) that fakes multi-form back-navigation by chaining separate huh.Form.Run() calls and catching ErrUserAborted. huh v2 has since absorbed its reason for being: native shift+tab back-navigation across all groups in one form (skipping hidden groups), Group.WithHideFunc for conditional sections, and reactive field funcs (TitleFunc/DescriptionFunc/PlaceholderFunc/OptionsFunc) for content that depends on earlier answers. This spec deletes pkg/forms and rewrites internal/cmd/generate/{command.go,project.go} onto single native huh forms, keeping only the command flag-collection loop as an imperative multi-Run loop (huh has no repeat-group primitive). It also adds the headless interactive-path test coverage the wizards lack today."
date: 2026-07-21
status: IMPLEMENTED
tags:
  - specification
  - forms
  - huh
  - generator
  - refactor
  - tui
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Delete `pkg/forms` and rewrite the generator wizards on native huh v2

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-21

Status
:   IMPLEMENTED (2026-07-21). Open questions resolved with user: **Q1** native shift+tab
    for back (Esc unbound, ctrl+c aborts); **Q2** aborting a flag form ends flag entry;
    **Q3** reactive `*Func` for same-form defaults; **Q4** done standalone. See the
    implementation note at the end for what the rewrite settled and how it is tested.

Related
:   [leaf module extraction](2026-07-21-leaf-module-extraction-workspace-changelog.md) (the sibling spec — `forms` was pulled out of that batch by this decision),
    [forms component page](../../explanation/components/forms.md) (to be deleted),
    [huh form testing guide](../testing/huh-form-testing.md) (to be reconciled),
    [extraction report](../reports/2026-07-07-package-extraction-report.md)

---

## 1. Summary

`pkg/forms` (~126 LOC) provides two things: `NewNavigable(groups…)` — a `huh.Form`
with Esc bound to Quit and AltScreen on — and `Wizard`, which chains **separate**
`huh.Form.Run()` calls as steps and interprets `huh.ErrUserAborted` as "go back one
step" (`runSteps` decrements the index and re-runs the previous step, rebuilding its
form). It is imported by exactly two files, **both internal**:
`internal/cmd/generate/command.go` and `internal/cmd/generate/project.go`. Nothing in
GTB's reusable framework surface uses it, and no downstream tool built on GTB touches
it.

The gap `forms` was built to fill — its own doc states *"once a form's `Run()`
returns there is no built-in way to step back into the previous form"* — exists only
**because it uses one `Run()` per step**. huh v2 closes that gap natively:

- **Back-navigation across every group in one form** via `shift+tab`, skipping hidden
  groups (`form.go` `prevGroupMsg` walks to the previous non-hidden group).
- **Conditional groups** via `Group.WithHideFunc(func() bool)`, re-evaluated on every
  keypress.
- **Reactive field content** via `TitleFunc`/`DescriptionFunc`/`PlaceholderFunc`/
  `OptionsFunc(fn, bindings)` — recomputed when the bound state changes.

So `forms` is no longer bridging a huh gap; it is an imperative-style convenience for
capabilities huh v2 now offers declaratively. This spec **deletes `pkg/forms`** and
rewrites the two generator wizards onto native huh v2, and adds the headless test
coverage the interactive path currently lacks.

This is a **refactor, not an extraction** — there is no module to publish. It is
specced separately because it modifies the generator (a non-trivial change per the
project's spec-first rule) and carries a small but real user-visible UX change.

## 2. Motivation

- **`forms` is not reusable surface.** Its only consumers are `internal/`. Publishing
  it as a module (the original plan) would ship a package no one outside GTB's own
  generator uses, whose justification huh has absorbed. Deleting it removes a
  package GTB maintains for no external benefit.
- **Idiomatic huh.** A single dynamic form with `WithHideFunc` + reactive funcs is the
  documented huh v2 pattern; it gives richer, continuous back-navigation than the
  faked single-level "back" the Wizard emulates.
- **Coverage.** The interactive `runWizard`/`runInteractivePrompt` paths are
  essentially untested today (`internal/cmd/generate/purelogic_test.go` covers only
  the non-interactive branches; the interactive path is gated off by
  `utils.IsInteractive()` and only runs on a real terminal). The rewrite is the
  moment to add headless tests.

## 3. Current behaviour (baseline)

`internal/cmd/generate/project.go` `runWizard` (project.go:359-520) — a seven-step
`forms.Wizard`:

1. **stage1** static group: name, description, path, features (multiselect),
   **git backend** (drives step 5), **help channel** (drives step 6).
2. `runEnvPrefixStep` — **defaults `o.EnvPrefix` from `o.Name`**, then prompts.
3. `runUpdatePolicyStep` — select, options pre-selected from current value.
4. `runUpdateCheckIntervalStep` — input with validation.
5. **git closure** — group whose heading, repo-field description and placeholder
   depend on `o.GitBackend`; **defaults `o.Host` from the backend**.
6. **help closure** — `switch o.HelpType`: slack group / teams group / nothing.
7. `runSigningStep` — confirm "enable signing?"; **only when enabled**, a second
   group collects WKD email, key source, key id; **defaults `SigningKeySource`**.

`internal/cmd/generate/command.go` `runInteractivePrompt` (command.go:289-301) — a
two-step `forms.Wizard`:

1. **main group**: name, short/long description, aliases, parent, args, options
   (multiselect), expose-to-MCP, **add-flags** (drives the flag loop), **set-prompt**
   (drives the prompt form).
2. `runAdditionalSteps`: split aliases CSV → slice; **if AddFlags**, run the flag
   loop; **if AddPrompt**, run the prompt form; sync options→flags; apply MCP
   defaulting.

The **flag loop** (`runFlagLoop`, command.go:527-553) repeats a flag-entry form while
the user keeps choosing "add another", accumulating into `o.Flags`, with a dynamic
description summarising flags entered so far.

## 4. Target design (native huh v2)

### 4.1 `NewNavigable` inlined

Replace `forms.NewNavigable(groups…)` with the native idiom at each call site (huh v2
has no `WithAltScreen`; the ViewHook is the supported path — unchanged from `forms`):

```go
huh.NewForm(groups...).
    WithViewHook(func(v tea.View) tea.View { v.AltScreen = true; return v })
```

Back-navigation is native `shift+tab`; **Esc is no longer rebound** (see §5).

### 4.2 `project.go runWizard` → one native form

A single `huh.NewForm(...)` composed of:

- **Core group**: name, description, path, features, git-backend select,
  help-channel select (as today).
- **Env-prefix field**: default reactively — the placeholder/suggested value derives
  from `o.Name` via `Input.PlaceholderFunc(fn, &o.Name)` (and/or seed `o.EnvPrefix`
  before `Run()` and keep the regex `Validate`). Replaces the imperative
  `o.EnvPrefix = upper(name)` step-boundary assignment.
- **Update-policy** select + **interval** input (validated), as today.
- **Git group**: one group with host / repository / private fields; the
  backend-dependent repo **description and placeholder** become
  `DescriptionFunc`/`PlaceholderFunc(fn, &o.GitBackend)`; the backend-dependent
  **heading** ("GitHub Repository" vs "GitLab Repository") moves to a leading
  `huh.NewNote().DescriptionFunc(fn, &o.GitBackend)` because group Title/Description
  are static-only. `o.Host` default becomes reactive/seeded rather than a step-side
  effect.
- **Help groups**: a slack group `WithHideFunc(func() bool { return o.HelpType != "slack" })`
  and a teams group `WithHideFunc(… != "teams")`. `none` hides both.
- **Signing groups**: the confirm in one group; the WKD/key detail fields in a second
  group `WithHideFunc(func() bool { return !o.Signing })`. Seed
  `SigningKeySource="both"` before `Run()`.

### 4.3 `command.go runInteractivePrompt` → native form + residual flag loop

- **Main group** → one native form; the AI-prompt fields move into a group
  `WithHideFunc(func() bool { return !o.AddPrompt })` so they appear inline when the
  user opts in.
- **Flag loop stays imperative.** huh has **no repeat-group primitive**, so
  `runFlagLoop` remains a `for` loop calling native `huh.NewForm(flagGroup).Run()` per
  flag, accumulating `o.Flags`, with the running-summary description as today. This is
  the one place a separate `Run()` genuinely cannot fold into the single form.
- **Side-effect code preserved** as pre/post-form logic (not form fields): alias-CSV
  seed/split, `ExposeToMCP` default, `syncOptionsToFlags`, `applyMCPExposureChoice`,
  per-flag `Type="string"` default.

### 4.4 Delete `pkg/forms`

Remove `pkg/forms/` entirely (`forms.go`, `doc.go`, `forms_test.go`) once both
consumers are rewritten and green.

## 5. User-visible change: back-navigation key

**Today:** `forms` binds **Esc** to go back one step (Ctrl+C hard-cancels).
**After:** back-navigation is native **shift+tab** (moving between fields/groups,
skipping hidden groups and preserving entered values); **Ctrl+C** remains the abort.
Esc is not a huh default binding and gains no "back" meaning.

This is a deliberate, documented UX change. shift+tab back-nav is continuous and
non-destructive (richer than the old one-shot per-step back), but it is a different
keystroke. Alternative (Q1): reintroduce Esc→previous-field via a custom key handler
if we want to preserve the exact keystroke — at the cost of bespoke message handling
huh doesn't natively support.

One consequence: across the residual command.go flag-loop boundary there is no
back-navigation into the main group (the Wizard used to provide it via abort). Aborting
a flag form ends flag entry; it does not return to the main group. Judged acceptable
(a rare path), but called out as Q2.

## 6. Testing strategy (headless)

No TTY and no `teatest` in the dependency tree, but huh v2 is testable headlessly two
ways, both used by huh's own suite:

1. **Direct `Update`/`View` driving** — construct the form, `f.Update(f.Init())`, feed
   `tea.KeyPressMsg`, assert `field.GetValue()`, `f.View()`, and `f.State`. Mirrors
   huh's `TestHideGroup` (drives group navigation and asserts which group renders) and
   `TestInput`. This is the primary coverage for: hide-func outcomes (slack/teams/none;
   signing phase-2; prompt group), reactive text bound to backend/name, and field
   validation.
2. **Accessible-mode scripted input** — `WithAccessible(true).WithInput(strings.NewReader("…"))
   .WithOutput(&buf).Run()` drives a full run line-by-line with no renderer (huh
   auto-enables this when `TERM=dumb`). Use for at least one end-to-end happy-path run
   of each wizard.

Add these as new tests alongside `internal/cmd/generate/purelogic_test.go`. Net effect:
the interactive path goes from ~no coverage to covered.

## 7. Non-goals

- No change to what the generator ultimately produces — same `SkeletonOptions`/command
  fields, same scaffold output. Only how they are collected changes.
- No new dependency (no teatest).
- No change to the non-interactive `ValidateOrPrompt`/flag-driven path.
- `pkg/forms` is not extracted or preserved anywhere.

## 8. Open questions

- **Q1 — Esc vs shift+tab for "back".** Accept native shift+tab (recommended — native,
  richer, zero custom code) or reimplement Esc→previous-field with a custom key
  handler to preserve the current keystroke? *Proposed: accept shift+tab; document the
  change.*
- **Q2 — flag-loop back-navigation.** Accept that aborting a flag form ends flag entry
  (no return to the main group), or invest in preserving that cross-boundary back-nav?
  *Proposed: accept; it is a rare path and the alternative reintroduces the Wizard we
  are deleting.*
- **Q3 — reactive defaulting vs seed-before-Run.** For `EnvPrefix`-from-`Name` and
  `Host`-from-backend, prefer reactive `PlaceholderFunc(…, &binding)` (updates live as
  the user changes the source field) or seed the value once before `Run()` (simpler,
  but stale if the user edits the source field afterward)? *Proposed: reactive for the
  ones whose source is in the same form (they can change mid-form); the map flags this
  as the key subtlety.*
- **Q4 — sequencing vs the extraction batch.** Do this refactor before, after, or
  independently of the workspace/changelog extractions? They touch disjoint code
  (generator vs leaf packages), so any order is safe. *Proposed: independent; can land
  in parallel.*

## 9. Acceptance criteria

1. `pkg/forms/` deleted; no remaining imports of it anywhere (`grep` clean).
2. Both generator wizards rewritten on native huh v2; `just build` and the full suite
   green; `gtb generate command`/`generate project` produce identical scaffolds to
   before (verified by generating into a temp dir and diffing the output, not the
   prompt flow).
3. New headless tests cover the hide-func branches, reactive text, and one
   end-to-end accessible-mode run per wizard.
4. `docs/explanation/components/forms.md` deleted; `docs/development/testing/huh-form-testing.md`
   reconciled (it should describe the native `Update`/`View` + accessible-mode
   approach, not the deleted `forms` helpers); component index and any `docs/`
   references swept.
5. Extraction report updated: `forms` marked **not extracted — deleted; internal
   generator wizards migrated to native huh v2** with a one-line rationale.

## 10. Implementation notes (2026-07-21)

What the rewrite settled, and where §6/§9 met reality:

- **Single form + residual flag loop.** `project.go`'s wizard is now one native form
  (`SkeletonOptions.wizardForm`) of nine groups: `basicsGroup` (name/desc/path/features
  + backend + help selects), env-prefix, update-policy, update-interval, git, slack,
  teams, signing-enable, signing-detail. `command.go`'s main form folds the AI-prompt
  group in via `WithHideFunc`; its **flag loop stays imperative** (a per-flag
  `newForm(...).Run()` loop) because huh has no repeat-group primitive.
- **Seeds via idempotent validators.** The name→env-prefix and backend→host defaults are
  seeded in the entry group's field validators (guarded by `if x == ""`), so the later
  groups pick them up pre-filled; `deriveEnvPrefix`/`hostForBackend` back them and the
  reactive field text.
- **Group title/description are static in huh** — the dynamic "GitHub/GitLab Repository"
  heading became a static group title plus reactive field descriptions/placeholder
  (`DescriptionFunc`/`PlaceholderFunc` bound to `&o.GitBackend`). No Note-field
  workaround was needed.
- **Shared `newForm` helper** (`internal/cmd/generate/form.go`) sets AltScreen via the
  view hook; back-nav is huh's native shift+tab, ctrl+c aborts. Esc is unbound (Q1).
- **Testing — corrected from §6/§9.** The interactive path is now tested three ways, but
  *not* via a full end-to-end accessible-mode drive as §6 imagined:
  - Pure unit tests for the seed/reactive-text logic (`deriveEnvPrefix`, `backendLabel`,
    `hostForBackend`, `repoDescription`, `repoPlaceholder`).
  - A **`tea.Model` drive** of the real `wizardForm()` (huh's own test style: synthetic
    key events, parallel-safe, no global stdin) proving the name→env-prefix seed fires.
  - The @generator BDD suite + `generator_build` for the flag path and generated output.
  A full accessible-mode (`TERM=dumb`) drive was prototyped and **dropped**: huh's
  `runAccessible` ignores `WithHideFunc` (it ranges every group) and its line-by-line
  input consumption (empty-line-keeps-value, placeholder interplay, select-with-preset)
  proved brittle and huh-version-coupled. Hiding is a TUI-event-loop behaviour that huh
  tests itself; the predicates here are trivial (`o.HelpType != "slack"`, `!o.Signing`).
  `internal/cmd/generate` remains coverage-excluded (Bucket B), as `pkg/forms` was.
- **Scaffold output unchanged.** The rewrite touches only interactive *collection*; the
  generator and its emitted scaffold are untouched (the flag path bypasses the wizard).
  A non-interactive `gtb generate project` smoke run succeeds end-to-end.
