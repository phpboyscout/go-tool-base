---
title: "`gtb ignore` command group and `.gtb/ignore` discoverability"
description: "The .gtb/ignore mechanism already works — it marks deliberately-diverged generated files hands-off so regenerate stops re-rendering them and stops raising conflicts — but it is undiscoverable: there is no command to manage it (unlike gtb enable/disable and gtb template add/list/remove), a fresh scaffold ships no commented .gtb/ignore, the conflict warning does not name it as the remedy, and neither the generated README nor the AI-agent guidance mentions it. Add a gtb ignore add/list/remove/check command group mirroring gtb template, plus five discoverability changes, so the feature is findable rather than rediscovered from scratch on each new project."
status: IMPLEMENTED
date: 2026-07-28
tags:
  - specification
  - generator
  - ignore
  - regeneration
  - cli
  - dx
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude (Fable 5)
    role: AI drafting assistant
---

# `gtb ignore` command group and `.gtb/ignore` discoverability

Authors
:   Matt Cockayne, Claude (Fable 5) *(AI drafting assistant)*

Date
:   2026-07-28

Status
:   IMPLEMENTED

Tracks
:   GitLab issue #3 — *feat(generator): add an 'ignore' command to manage .gtb/ignore rules*

---

## 0. Implementation record (2026-07-28)

Shipped in `feat(generator): add gtb ignore command and .gtb/ignore discoverability (#3)`.

**Command group** — new `internal/cmd/ignore/` mirroring `internal/cmd/template/`,
registered in `internal/cmd/root/root.go` and `cmd/e2e/main.go`:

- `gtb ignore add <pattern>...` — idempotent, comment/order-preserving, writes an
  explanatory header on create; reports `added:` / `already present (no-op):`.
- `gtb ignore remove <pattern>` — drops the literal rule line; errors when absent.
- `gtb ignore list` — resolves rules against the manifest's tracked files
  (`file → status (rule)`) and flags stale rules matching nothing.
- `gtb ignore check <path>...` — reports ignored/not and **names the winning
  rule** under last-match-wins + `!` negation.
- `--dry-run` on `add`/`remove` prints the resulting file without writing.

**Generator primitives** (`internal/generator/ignore.go`, `ignore_command.go`):

- `AppendIgnorePattern` / `RemoveIgnorePattern` — idempotent, comment/order-preserving
  writers; `PreviewAppendIgnorePatterns` / `PreviewRemoveIgnorePattern` back `--dry-run`.
- `ScaffoldIgnoreFile` — writes the inert commented header (no-op if present).
- `(*IgnoreRules).Explain(relPath) (rule, negated, matched)` — the winning-rule
  accessor; `(*IgnoreRules).Rules()` lists active rules.
- `(*Generator).ListIgnoreRules` / `CheckIgnorePaths` / `DivergedUnignoredFiles`.

**Discoverability changes:**

1. **[P0]** `generate project` scaffolds a commented, inert `.gtb/ignore`
   (wired in `skeleton.go`; `ScaffoldIgnoreFile`). Test flipped:
   `TestScaffold_ShipsCommentedGtbIgnore`.
2. **[P0]** Conflict warning at `hash.go` and `skeleton.go` now carries a `hint`
   naming `.gtb/ignore` / `gtb ignore add <path>` (`ignoreConflictHint`).
3. **[P1]** Generated `README.md` regeneration-model + Contributing sections
   document the opt-out.
4. **[P1]** AI-agent guidance rides in the generated `README.md` (per Q1 below,
   option (a) — no separate `AGENTS.md` scaffold).
5. **[P2]** `doctor` check `Generator ignore coverage` — a **gtb-only**
   registered check (`internal/cmd/ignore/doctor.go`, under the default-enabled
   doctor feature so scaffolded tools do not inherit it) that reports
   diverged-and-unignored tracked files, and skips cleanly outside a project.

**Tests/scenarios:** the three PENDING tests are un-skipped and green
(`TestIgnoreAdd_Idempotent_PreservesComments`, `TestIgnoreAdd_WritesHeaderOnCreate`,
`TestIgnoreCheck_NamesWinningRule`); new generator + command unit tests; six
Gherkin scenarios in `features/generator/ignore-lifecycle.feature`. Docs updated
in `docs/how-to/configure-generator-ignore.md`.

**Open questions resolved:** Q1 → (a) README-only for now. Q3 → verbs stay pure
file edits, no `--regenerate`. Q4 → `remove` matches the literal rule line.
Q5 → patterns stored verbatim (trimmed). Q6 → `check` requires ≥1 path; `list`
owns the whole-project view. Q2 → doctor check implemented as a gtb-only
registered check (conditional on a manifest).

---

## 1. Reported problem

`.gtb/ignore` already does the right thing. It marks deliberately-diverged files
as hands-off so `regenerate` stops re-rendering them and stops raising conflicts.
It is implemented in `internal/generator/ignore.go`, respected by both the
embedded skeleton walk and template overlays, and documented in
`docs/how-to/configure-generator-ignore.md`.

The problem is purely **discoverability**. Nothing in a generated project surfaces
the mechanism:

- A fresh scaffold contains `.gtb/manifest.yaml` and nothing else in `.gtb/`.
  There is no empty-but-commented `ignore` file hinting the mechanism exists.
- The generated `README.md` explains the hash-tracking and conflict model in
  detail and tells the reader that hand-edited files prompt on conflict — but
  never mentions that there is a supported way to opt a file out permanently.
- The conflict warning itself (`conflict detected: file has been manually
  modified path=…`) does not name `.gtb/ignore` as the remedy.
- Every other project-level generator concern already has a command:
  `gtb enable`/`disable` for features, `gtb template add/list/remove/update` for
  overlays. Ignore rules are the odd one out — editable only by hand.

The practical result is that this is rediscovered from scratch on each new
project. Divergence from the skeleton is normal and expected — a project acquires
container files, extra `just` recipes, a real README — and each of those turns
into a recurring conflict prompt until someone remembers the ignore file exists.
It is a particularly sharp edge for AI coding agents, which read the generated
`README.md`, find the conflict model described, and reasonably conclude that
living with the prompts is the intended workflow.

This was hit again bootstrapping a new GTB-based project where the scaffolded
`justfile`, `README.md`, and `.gitignore` were all deliberately extended; all
three then showed as conflicts until `.gtb/ignore` was written by hand. That is
at least the third project where the same thing played out. Once the ignore file
was in place the behaviour was exactly right. **The feature works; it just needs
to be findable.**

## 2. Current-state validation

Verified against `origin/main` (worktree at commit `e7578e20`). Evidence:

### What exists (the working mechanism)

- **`internal/generator/ignore.go`** — `LoadIgnoreRules(fs, projectPath)` reads
  `.gtb/ignore`, returning empty rules when the file is absent. `IsIgnored(relPath)`
  evaluates rules **top-to-bottom; later patterns override earlier ones**;
  `!` negation re-includes a file an earlier pattern excluded (`ignored = !rule.negate`
  on each match). Supports basename globs (`justfile`, `*.yml`), anchored paths
  (`.github/workflows/test.yml`), and recursive `**` (`.github/**`).
- **Respected on both write paths**: `skeleton.go` (`generateSkeletonTemplateFilesWithSources`,
  `walkSkeletonAssets`) and `templatesource_apply.go` (`applyOverlays`) both call
  `rules.IsIgnored(relPath)` and, when ignored, skip the write but still hash the
  on-disk content (`hashIgnoredFile`) so the manifest stays accurate. `--force`
  does not override ignore rules.
- **`docs/how-to/configure-generator-ignore.md`** — a complete how-to: syntax,
  pattern-type table, negation, hashing behaviour, common patterns.
- The behaviour has locked-in unit coverage in this branch's
  `internal/generator/ignore_command_test.go` (`TestIgnoreRules_EvaluationSemantics`,
  `TestIgnoreRules_EmptyAndMissing`) — both pass today.

### What is missing (the discoverability gap — all four confirmed)

| Gap | Evidence |
|-----|----------|
| **No `gtb ignore` command** | `internal/cmd/` has `enable/`, `disable/`, `template/`, `generate/`, `regenerate/`, `remove/`, `keys/`, `sign/` — no `ignore/`. Nothing registered in `internal/cmd/root/root.go` or `cmd/e2e/main.go`. |
| **No scaffolded `.gtb/ignore`** | `internal/generator/assets/skeleton/` contains no `.gtb/` directory and no `ignore` file. The manifest is written programmatically by `writeSkeletonManifest`; nothing writes an ignore file. `TestScaffold_HasNoGtbIgnore_CurrentGap` (this branch) confirms a fresh scaffold has `.gtb/manifest.yaml` but no `.gtb/ignore`. |
| **Conflict warning does not name the remedy** | `skeleton.go:771` and `hash.go:59` both emit `Logger.Warn("conflict detected: file has been manually modified", "path", …)` with no hint. The interactive prompt (`hash.go:112`) offers only Yes/No/View-diff — no "ignore it" affordance. |
| **Generated README does not mention it** | `assets/skeleton/README.md` "The manifest & regeneration model" section (lines 94–113) explains hash-tracking and the conflict prompt, then says *"Put custom logic in your own packages, or accept the conflict prompt on the next regenerate."* — no permanent opt-out. The Contributing note (line 185) repeats "do not hand-edit generated files" without the escape hatch. |
| **No AI-agent guidance** | The skeleton ships **no** `AGENTS.md`/`CLAUDE.md`; agent-facing guidance lives only in the generated `README.md`, which (per above) omits `.gtb/ignore`. |

**No recent generator work has closed any of these** — the command, scaffold,
warning hint, README mention, and agent guidance are all still absent on `main`.

## 3. Assessment of the proposed command surface

The reporter proposes mirroring `gtb template` with
`gtb ignore add/list/remove/check`. Assessment:

### Mirroring `gtb template` is the right structure

`internal/cmd/template` is a clean, small precedent: a `NewCmdTemplate(p)` group
(`template.go`) wrapping `cobra.Command` via `setup.Wrap`/`group.Register`, with
one file of thin subcommand constructors (`subcommands.go`) each resolving the
project path with `icmd.ResolveProjectPath(p, path)` and delegating to a generator
method. `ignore` should follow the identical shape and register in `root.go` /
`cmd/e2e/main.go` next to `NewCmdTemplate`. Consistency here is the whole point of
the issue, so the parallel is a feature, not incidental.

One structural difference worth calling out: `template` mutations **regenerate**
(the manifest edit is only meaningful once re-rendered). `ignore add/remove` need
**not** regenerate — the ignore file is read fresh on the next `regenerate`, and
editing it changes nothing already on disk. So `ignore` verbs are pure file edits
(plus an optional convenience regenerate), simpler than `template`. `--dry-run`
therefore means "show the resulting ignore file / decision without writing it",
not "simulate a regenerate".

### `add` / `remove` — sound, with one caveat

- **`add` idempotent + comment/ordering-preserving + header-on-create** is well
  specified and aligns with the file format (`ignore.go` skips blank and `#`
  lines, so a header is free). Needs a new writer helper (e.g.
  `generator.AppendIgnorePattern`); `LoadIgnoreRules` is read-only today.
- **`remove <pattern>`** should match on the *literal pattern line*, not on a path
  that the pattern happens to match — otherwise `remove justfile` is ambiguous
  against `*.yml`. Recommend: remove the exact rule line; error if absent.

### `list` and `check` — semantically the sharp part

Both features want to explain *why* a file is (not) ignored. This is only coherent
because of `ignore.go`'s **last-match-wins** evaluation with `!` negation. Two
alignment notes:

- **`list` "resolves rules against the manifest"**: viable and valuable — walk the
  manifest's tracked files, run each through `IsIgnored`, and show
  `justfile → ignored (rule: justfile)`. It can also flag a **stale rule** (a
  pattern that currently matches no tracked file). Caveat: `list` must attribute
  each file to the **winning** rule, not the first that matches — with negation the
  winner can be a later `!` line. `IsIgnored` returns only a bool today, so this
  needs a new accessor.
- **`check [path]` "names the winning rule"**: this is the genuinely new capability.
  Because rules are evaluated top-to-bottom, *"why is this file still being
  overwritten?"* is a real question the flat file cannot answer. Implement a
  last-match-wins lookup on `IgnoreRules` returning the deciding rule (pattern +
  whether it was a negation) and whether any rule matched at all. The pending test
  `TestIgnoreCheck_NamesWinningRule_PENDING` pins the three cases (matched-by-glob,
  re-included-by-negation, matched-by-nothing).

**Verdict:** the proposed surface is well-judged and faithful to the mechanism.
The only real design work is the two new generator primitives — an idempotent
writer and a winning-rule accessor — everything else is ergonomics over existing
code.

## 4. Proposed design

### 4.1 The `gtb ignore` command group

New `internal/cmd/ignore/` mirroring `internal/cmd/template/`:

```
gtb ignore add <pattern>...    # append pattern(s), creating .gtb/ignore (with header) if absent; idempotent
gtb ignore list                # active rules + which tracked files each matches; flag stale rules
gtb ignore remove <pattern>    # drop a literal rule line; error if absent
gtb ignore check [path]...     # report ignored/not for each path AND name the winning rule
```

Shared: `--path/-p` (project root, default `.`, via `icmd.ResolveProjectPath`);
`--dry-run` on the mutating verbs (`add`, `remove`) prints the resulting file
without writing. Registered in `internal/cmd/root/root.go` and `cmd/e2e/main.go`
alongside `NewCmdTemplate`.

New generator primitives in `internal/generator/ignore.go`:

- `AppendIgnorePattern(fs, projectPath, pattern) (changed bool, err error)` —
  idempotent, comment/ordering-preserving, writes an explanatory header when
  creating the file.
- `RemoveIgnorePattern(fs, projectPath, pattern) (changed bool, err error)`.
- A winning-rule accessor on `*IgnoreRules`, e.g.
  `Explain(relPath) (rule string, negated bool, matched bool)`, backing both
  `check` and `list`.

### 4.2 The five discoverability changes (prioritised)

1. **[P0] Scaffold a commented `.gtb/ignore`** in `generate project`. Empty apart
   from a header explaining the syntax and pointing at the how-to. Single
   highest-value change — the mechanism becomes visible by looking in `.gtb/`.
   Flips `TestScaffold_HasNoGtbIgnore_CurrentGap`. A comments-only file ignores
   nothing (`TestIgnoreRules_EmptyAndMissing`), so it is behaviourally inert.
2. **[P0] Name the remedy in the conflict warning.** At `skeleton.go:771` and
   `hash.go:59`, append a hint: *add `<path>` to `.gtb/ignore` (or run:
   `gtb ignore add <path>`) to keep your changes*. Puts the answer where the
   question arises. Consider an "ignore this file" option in the interactive
   prompt (`hash.go:112`) too.
3. **[P1] Mention it in the generated `README.md`** regeneration-model section
   (lines 105–113): two sentences that a file can be marked hands-off via
   `.gtb/ignore` / `gtb ignore add`.
4. **[P1] AI-agent guidance.** Add the escape hatch wherever agent-facing
   guidance ships. **Open point:** the skeleton ships no `AGENTS.md` today, so
   this rides in the generated `README.md` (change #3) unless we decide to
   scaffold an `AGENTS.md`/`CLAUDE.md` — see Open questions.
5. **[P2] `doctor` check** reporting files currently diverged from their manifest
   hash and *not* covered by an ignore rule — the set that will prompt on the next
   regenerate. **Design note:** `pkg/cmd/doctor` checks today are generic (Go
   version, config, API keys) and manifest-agnostic; a manifest-aware check must
   be conditional on a `.gtb/manifest.yaml` existing, or live as a
   generator-side/`gtb`-only check rather than in the framework doctor every
   scaffolded tool inherits. Lowest priority; largest surface.

## 5. Acceptance criteria

- `gtb ignore add/list/remove/check` exist, registered in the root tree and the
  e2e binary, structured like `internal/cmd/template`.
- `add` is idempotent (re-adding a present pattern is a reported no-op, no
  duplicate line), preserves existing comments/ordering, and writes an
  explanatory header when it creates the file.
- `remove` drops the exact literal rule line and errors when it is absent.
- `check <path>` reports ignored/not-ignored **and** names the winning rule,
  correct under `!` negation (the three cases in
  `TestIgnoreCheck_NamesWinningRule_PENDING`).
- `list` shows each tracked file's ignore status attributed to the *winning*
  rule and flags rules that match no tracked file.
- `--dry-run` on `add`/`remove` prints the resulting file without writing.
- A fresh `generate project` scaffold contains a commented `.gtb/ignore` with a
  header and a how-to pointer; it ignores nothing until edited
  (`TestScaffold_HasNoGtbIgnore_CurrentGap` flips to assert presence).
- The regenerate conflict warning names `.gtb/ignore` / `gtb ignore add` as the
  remedy.
- The generated `README.md` documents the opt-out in the regeneration-model
  section.
- New generator code carries ≥90% coverage; the pending tests in
  `internal/generator/ignore_command_test.go` are un-skipped and pass; docs in
  `docs/how-to/configure-generator-ignore.md` cross-reference the new command.

## 6. Open questions

1. **AI-agent guidance surface (change #4).** The skeleton ships no `AGENTS.md`.
   Do we (a) fold the guidance into the generated `README.md` only, or (b) begin
   scaffolding an `AGENTS.md`/`CLAUDE.md` in generated projects? (b) is a larger,
   separable decision.
2. **`doctor` check placement (change #5).** Framework `pkg/cmd/doctor` is
   inherited by every scaffolded tool and is manifest-agnostic. Should the
   diverged-and-unignored check be a conditional doctor check (only when a
   manifest exists), a generator/`gtb`-only check, or deferred to a follow-up?
3. **Should `add` optionally regenerate?** Unlike `template`, `ignore` edits need
   no regenerate. Offer an opt-in `--regenerate` convenience, or keep the verbs
   pure file edits and leave regenerate to the user?
4. **`remove` matching semantics.** Confirm remove matches the literal pattern
   line (recommended) rather than "any rule that would match this path", which is
   ambiguous under overlapping globs and negation.
5. **`add` normalisation.** Should `add cmd/tool/main.go` be stored verbatim, or
   normalised (e.g. trailing-slash handling, `./` stripping) to match how
   `parseIgnoreRule` interprets it? Verbatim is simplest but can surprise.
6. **`check` with no arguments.** Define `gtb ignore check` (no path): does it
   check every manifest-tracked file (making it a superset of `list`), or require
   at least one path? Recommend requiring a path and letting `list` own the
   whole-project view.
