---
title: "Conflict detection for the generate subcommands and non-interactive conflict resolution"
description: "Independent diagnosis of issue #6. On current main the reported silent revert of .pre-commit-config.yaml, .gitlab-ci.yml and the how-to/concepts index pages by `generate command`/`add-flag` no longer reproduces — those files are skeleton-owned and the generate subcommands never re-render the skeleton. Two real defects remain: the manifest-derived CLI index (docs/reference/cli/index.md) is rewritten unconditionally with no hash-conflict check and no .gtb/ignore consultation, and the generator conflict prompt has no utils.IsInteractive() guard, so a conflict in a headless/CI run emits per-file huh 'open /dev/tty' noise (or would block on a machine with a TTY)."
date: 2026-07-28
tags:
  - specification
  - generator
  - cli
  - docs
  - tui
status: IMPLEMENTED
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Conflict detection for the generate subcommands and non-interactive conflict resolution

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-28

Status
:   IMPLEMENTED

Related
:   [issue #6](https://gitlab.com/phpboyscout/go-tool-base/-/issues/6) — the original report;
    [TTY-guard the root pre-run prompts](2026-07-23-prerun-prompt-tty-guard.md) — established
    `utils.IsInteractive()` as the codebase convention for gating every interactive `huh`
    flow; the generator conflict prompt was not in that work's scope.

---

## Summary

Issue #6 reports that `gtb generate command` followed by `gtb generate add-flag`
silently reverted four hand-maintained files (`.pre-commit-config.yaml`,
`.gitlab-ci.yml`, `docs/how-to/index.md`, `docs/explanation/concepts/index.md`),
while `gtb regenerate project` correctly detects and skips them. It was observed
on `v0.32.0` and theorises the generate subcommands run project post-processing
without the hash-conflict detection `regenerate` has. A second defect is that the
conflict prompt requires a TTY and fails under non-interactive runners.

Diagnosed independently against **current `main`** (post the *generator-followups*
work — shared `CommandPipeline`, skeleton/command separation), the picture has
moved since `v0.32.0`:

- **The primary report no longer reproduces.** The four cited files are all
  skeleton-owned. Skeleton rendering — the path that also carries the conflict
  check — is unreachable from the generate subcommands. `generate command` +
  `add-flag` leave all four **byte-identical**. The reporter's root-cause theory
  is therefore **refuted as applied to current main**; the regression it describes
  was already closed.

- **A narrower instance of the same class is still live.** The one docs file the
  generate subcommands *do* rewrite — the manifest-derived CLI index
  (`docs/reference/cli/index.md`) — is written **unconditionally**, with no
  hash-conflict check and no `.gtb/ignore` consultation. Hand-added prose there is
  discarded silently.

- **The second defect (TTY) is confirmed present.** The generator's conflict
  prompt has no `utils.IsInteractive()` guard.

## 1. Reported problem

From issue #6:

| File | Reported change |
|------|-----------------|
| `.pre-commit-config.yaml` | golangci-lint / pre-commit-hooks versions downgraded to the template's |
| `.gitlab-ci.yml` | merge-method comment rewritten (reversing a documented project policy) |
| `docs/how-to/index.md` | added guide entries deleted, template placeholder restored |
| `docs/explanation/concepts/index.md` | added concept entries deleted |

The reporter notes `regenerate project --dry-run` handles the same files
correctly (`WARN conflict detected … WARN skipping overwrite`) and theorises the
fix is to route the generate subcommands' project writes through the same
hash-conflict check.

Second defect: when a conflict *is* detected, the prompt fails outright without a
controlling terminal —

```
WARN Prompt failed (non-interactive?): huh: bubbletea: error opening TTY:
     bubbletea: could not open TTY: open /dev/tty: no such device or address.
     Skipping overwrite.
```

— defaulting to skip (safe) but emitting a stack-flavoured warning per file and
making an overwrite impossible in CI. The report suggests a
`--on-conflict=skip|overwrite|fail` flag, and for index pages offers three
options: (1) generate entries by scanning the directory, (2) merge unknown
entries into the existing file, (3) treat index pages as write-once.

## 2. Independent root-cause analysis

### 2.1 Primary revert — DISAGREE with the reporter (fixed on current main)

The four cited files are all **skeleton assets**:

- `internal/generator/assets/skeleton/.pre-commit-config.yaml`
- `internal/generator/assets/skeleton-gitlab/.gitlab-ci.yml`
- `internal/generator/assets/skeleton/docs/how-to/index.md`
- `internal/generator/assets/skeleton/docs/explanation/concepts/index.md`

They are materialised only by `generateSkeletonTemplateFilesWithSources`
(`internal/generator/skeleton.go:517`), whose per-file write goes through
`renderAndHashSkeletonTemplate` → `writeRenderedSkeletonFile` →
`checkSkeletonConflict` (`skeleton.go:656`, `:691`, `:760`) — i.e. the conflict
check **is** on the skeleton write path. That function has exactly two callers:

- `generateSkeletonFiles` — `GenerateSkeleton`, i.e. `generate project` (new project);
- `regenerateSkeletonFiles` (`regenerate.go:565`) — `RegenerateProject`, i.e. `regenerate project`.

Neither `generate command` nor `generate add-flag` reaches it:

- `generate command` → `CommandOptions.Run` → `generator.Generate` → `generate`
  (`commands.go:97`) → `finalizeProject` → `postGenerate` → `CommandPipeline.Run`
  (`pipeline.go:57`). The pipeline's four steps are **asset files, parent/child
  registration, manifest persist, documentation** — none re-render the skeleton.
- `generate add-flag` → `AddFlagOptions.Run` → `regenerateCommand` →
  `Generator.RegenerateCommand` (`regenerate.go:278`), which drives the **same**
  `CommandPipeline` for a single command — again no skeleton render.

So on current `main` the generate subcommands never touch the four files. The
reporter's theory ("the generate subcommands run project post-processing without
the conflict detection") describes the `v0.32.0` code; the *generator-followups*
refactor that introduced the shared `CommandPipeline` and separated skeleton
concerns closed it. **Verified empirically** (§3): after `generate command` +
`add-flag`, all four files are byte-identical (SHA-256 unchanged).

### 2.2 Residual — the CLI index is rewritten unconditionally (AGREE, narrowed)

The one docs file the generate subcommands *do* write is the manifest-derived CLI
command index, via `generateCommandsIndex` (`docs.go:1144`), reached from the
pipeline's documentation step (`handleDocumentationGeneration` → `GenerateDocs` →
`writeBasicCommandDocs`/`writeAIDocs` → `generateCommandsIndex`, `docs.go:229`/`:276`).
It ends in an **unconditional** write:

```go
// internal/generator/docs.go:1165
if err := afero.WriteFile(g.props.FS, indexPath,
    []byte(g.buildCommandsIndexContent(m.Commands, diataxis)), DefaultFileMode); err != nil {
```

`indexPath` is `docs/reference/cli/index.md` (Diátaxis) or `docs/commands/index.md`
(flat). There is **no** `checkSkeletonConflict`, no stored-hash comparison, and no
`.gtb/ignore` consultation — unlike every skeleton write. A downstream that adds
prose to that index loses it on the next `generate command`, silently. This is a
genuine, if narrower, instance of the reporter's index-page complaint. It differs
from the how-to/concepts case because the CLI index is *generated content* (a table
of commands that must stay current), which shapes the fix options in §4.

### 2.3 Second defect — TTY conflict prompt (AGREE, confirmed present)

`promptOverwrite` (`hash.go:77`) resolves a conflict as follows:

```go
switch g.config.Overwrite {
case "allow": return true
case "deny":  return false
}
if os.Getenv("GTB_NON_INTERACTIVE") == "true" { return false }
return g.askOverwriteAction(path, existing, newContent) // -> huh.NewSelect().Run()
```

The only non-interactive escape is the opt-in `GTB_NON_INTERACTIVE=true` env var.
There is **no `utils.IsInteractive()` guard** and no honouring of `CI=true`. When
that env var is unset — the default in most CI/container runners — a detected
conflict calls `huh.NewSelect().Run()` (`hash.go:111`) unconditionally, which in a
headless environment fails with `open /dev/tty: no such device or address` and
logs the per-file `Prompt failed (non-interactive?)` warning (`hash.go:118`)
before skipping. On a machine that *does* expose `/dev/tty` it would instead block
on the prompt.

This is precisely the class fixed for the root pre-run prompts in the
[2026-07-23 TTY-guard spec](2026-07-23-prerun-prompt-tty-guard.md), which
established `utils.IsInteractive()` (plus a `CI=true` check) as the convention for
every interactive `huh` flow. The generator conflict prompt was outside that
work's scope and still lacks the guard.

## 3. Validation on current main + red evidence

**End-to-end (primary report — now fixed).** A GitLab-backend project scaffolded
with `generate project`, then the four files hand-edited, then:

```
gtb generate command --name widget --short "a widget"
gtb generate add-flag -c widget -n thing -t string -d "a thing"
```

SHA-256 before/after (`GTB_NON_INTERACTIVE=true`, first 16 hex):

| File | before | after |
|------|--------|-------|
| `.pre-commit-config.yaml` | `b24a87a37c525442` | `b24a87a37c525442` (unchanged) |
| `.gitlab-ci.yml` | `ad5500f2b329280a` | `ad5500f2b329280a` (unchanged) |
| `docs/how-to/index.md` | `661b4a331830cfed` | `661b4a331830cfed` (unchanged) |
| `docs/explanation/concepts/index.md` | `9088fd9307ef06b4` | `9088fd9307ef06b4` (unchanged) |
| `docs/reference/cli/index.md` | `16d42032d8cf9ab1` | `4a29f1f2e2dd21bb` (**rewritten** — expected: now lists `widget`) |

**Unit tests** — `internal/generator/issue6_conflict_detection_test.go` (red, on a
non-merging branch):

- `TestIssue6_DocPostProcessing_PreservesHandEditedIndexPages` — **GREEN guard**:
  the doc post-processing (`generateCommandsIndex`) leaves hand-edited
  `docs/how-to/index.md` and `docs/explanation/concepts/index.md` untouched. Locks
  in the §2.1 fix.
- `TestIssue6_CommandsIndex_ClobbersHandEditedIndex` — **RED** (§2.2): a
  hand-added intro paragraph in `docs/reference/cli/index.md` does not survive
  `generateCommandsIndex`:

  ```
  "…| Command | Description |…| [deploy](deploy.md) | Deploy it |…"
      does not contain "Hand-maintained intro paragraph"
  ```
- `TestIssue6_ConflictPrompt_NonInteractiveEmitsTTYNoise` — **RED** (§2.3):

  ```
  bug #6.2: conflict resolver attempted a TTY prompt in a non-interactive
  context and emitted noise:
  [warn] Prompt failed (non-interactive?): huh: bubbletea: error opening TTY:
         bubbletea: could not open TTY: open /dev/tty: no such device or address.
         Skipping overwrite.
  ```

Also confirmed live via `regenerate project` on the scaffolded repo (no
`GTB_NON_INTERACTIVE`, no TTY): every conflicting file emitted the same
`Prompt failed … open /dev/tty` warning before skipping.

## 4. Proposed solution direction

### 4.1 Non-interactive conflict resolution (§2.3)

Mirror the established `pkg/cmd/root` convention:

1. In `promptOverwrite`, detect non-interactive up front — before any `huh` call —
   using `utils.IsInteractive()` **and** a `CI=true` check (reuse/extract the
   `isCIEnvironment` helper the pre-run guard already owns rather than re-reading
   env inline). When non-interactive, resolve by policy with **no** terminal
   attempt and **no** per-file `Prompt failed` noise (a single summary line at
   most).
2. Add an explicit conflict policy the reporter asked for. Preferred shape: extend
   the existing `--overwrite` vocabulary rather than add a parallel flag —
   `--overwrite=allow|deny|ask|fail` — where `fail` aborts the run on the first
   conflict (useful for CI gating) and `ask` degrades to `deny` (skip, the current
   safe default) when non-interactive. If a distinct `--on-conflict` name reads
   better to users, alias it; avoid two independent knobs with overlapping
   semantics. Wire the flag on both `generate` and `regenerate` (regenerate
   already exposes `--overwrite`).
3. Keep `GTB_NON_INTERACTIVE=true` working as a compatibility shim, folded into
   the same non-interactive predicate.

### 4.2 CLI index rewriting (§2.2) — assessing the reporter's three options

The reporter's options were framed for template-driven index pages; the surviving
case is the *generated* CLI table, so each is re-assessed for that file:

1. **Scan the directory instead of a fixed template.** `generateCommandsIndex`
   already derives entries from the manifest (structurally equivalent to a scan),
   so this does not, by itself, preserve hand-added prose — the table is
   regenerated either way. Helps only if prose lives *outside* a regenerated
   region. Partial fit.
2. **Merge — regenerate the table, preserve the rest.** Delimit the generated
   command table with stable fenced markers (e.g. `<!-- gtb:commands:start -->` /
   `:end`) and only rewrite between them, leaving any surrounding prose intact.
   Robust and keeps the table current; moderate complexity. **Recommended for the
   content.**
3. **Write-once (create if absent, never rewrite).** Simplest, but the command
   table would then go stale as commands are added/removed — unacceptable for a
   generated index. Reject for this file.

Complementary and lower-risk: route `generateCommandsIndex` (and
`generatePackagesIndex`) through the **same conflict-checked write path** the
skeleton uses — record the last-generated index hash (in the manifest, or a small
side record) and, when the on-disk file diverges, apply the §4.1 policy
(detect/skip in CI, prompt interactively) exactly like a skeleton file. Also make
the index writer honour `.gtb/ignore`, so the documented workaround actually
protects the file. This unifies behaviour with `regenerate` without needing
marker parsing; markers (option 2) can layer on later if in-file prose must
coexist with a live table.

### 4.3 Non-goals

- Re-introducing skeleton rendering into the generate subcommands. §2.1 shows the
  separation is correct; the fix must not reconnect them.
- Changing the safe default direction (skip on unresolved conflict).

## 5. Acceptance criteria

- `generate command` and `generate add-flag` continue to leave every skeleton file
  (`.pre-commit-config.yaml`, `.gitlab-ci.yml`, `docs/how-to/index.md`,
  `docs/explanation/concepts/index.md`, …) byte-identical — the §3 GREEN guard and
  e2e stay green.
- A hand-modified `docs/reference/cli/index.md` (or `docs/commands/index.md`) is no
  longer silently overwritten: it is either conflict-detected and skipped under the
  active policy, preserved via marker-merge, or protected by `.gtb/ignore`.
  `TestIssue6_CommandsIndex_ClobbersHandEditedIndex` flips to asserting survival.
- A detected conflict in a non-interactive environment (no TTY, or `CI=true`, or
  `--overwrite` set to a non-`ask` policy) resolves **without** invoking `huh` and
  **without** the per-file `Prompt failed … /dev/tty` warning.
  `TestIssue6_ConflictPrompt_NonInteractiveEmitsTTYNoise` goes green.
- `--overwrite=fail` (and/or `--on-conflict=fail`) aborts non-zero on the first
  conflict; `allow`/`deny` behave as today; `ask` degrades to skip when
  non-interactive.
- Interactive behaviour (real TTY) is unchanged: the select prompt, including
  `View diff`, still appears.
- `GTB_NON_INTERACTIVE=true` remains honoured.
- Docs updated (`docs/` generator/regenerate pages) to describe the conflict policy
  and the CLI-index behaviour; ≥90% coverage on new/changed `pkg/`-visible logic
  (generator is `internal/`, but keep parity).

## 6. Open questions

1. **Q1 — policy surface.** Extend `--overwrite` with a `fail` value, or add a
   distinct `--on-conflict` flag (aliasing `--overwrite`)? Recommendation: extend
   `--overwrite`; confirm no downstream depends on the current three-value set.
2. **Q2 — CLI-index strategy.** Conflict-checked write (skip/prompt like a skeleton
   file) vs marker-merge (regenerate only the table region) vs both. Recommendation
   starts with conflict-checked write + `.gtb/ignore`; is marker-merge wanted now
   or deferred?
3. **Q3 — where does the CLI-index hash live?** The manifest already stores skeleton
   and command hashes; adding generated-doc hashes there is natural but grows the
   manifest. Acceptable, or use a separate record?
4. **Q4 — `CI=true` parity.** Should the generator honour `CI=true` as a
   non-interactive signal (matching the pre-run guard's `isCIEnvironment`), or only
   `utils.IsInteractive()` + `GTB_NON_INTERACTIVE`? Recommendation: honour `CI=true`
   for parity.
5. **Q5 — should `regenerate project` also drop the per-file `Prompt failed` noise
   retroactively**, i.e. is §4.1 applied to the shared `promptOverwrite` (used by
   both `verifyHash` and `checkSkeletonConflict`) so `regenerate` benefits too?
   Recommendation: yes — fix at the shared choke point.

## 7. Implementation (IMPLEMENTED — 2026-07-28)

The primary report stays **refuted on current main** (§2.1) — the §3 GREEN guard
`TestIssue6_DocPostProcessing_PreservesHandEditedIndexPages` is kept as a
regression lock and no skeleton-render path was reconnected to the generate
subcommands. The two residuals were fixed.

### 7.1 Residual — CLI index clobber (§2.2)

`generateCommandsIndex` (`internal/generator/docs.go`) no longer ends in an
unconditional `afero.WriteFile`. It now:

- consults `.gtb/ignore` via `LoadIgnoreRules(...).IsIgnored(relPath)` and returns
  without touching the file when the index is ignored — the documented workaround
  now actually protects it (Q4-adjacent);
- delimits the generated command **table** with stable marker comments
  `<!-- gtb:commands:start -->` / `<!-- gtb:commands:end -->` and, via the new
  `mergeCommandsIndex`, rewrites **only** the region between them, so any prose
  before or after the markers survives (Q2 → marker-merge chosen, not a stored
  hash; no manifest growth, so **Q3 is moot**);
- migrates a purely-generated legacy index (no markers, no prose —
  `isGeneratedCommandsIndex`) to the marker form so it keeps updating, and
  **preserves** a diverged index (markers removed, hand-written prose present)
  untouched with a single WARN — never a silent clobber;
- the scaffolded skeleton index
  (`internal/generator/assets/skeleton/docs/reference/cli/index.md`) ships with an
  empty marker pair so a fresh project's first `generate command` splices the
  table in cleanly rather than triggering the divergence branch.

`buildCommandsIndexContent` was split into `buildCommandsIndexTable` (the table
only) + `freshCommandsIndex` (heading + wrapped table) so the same table renders
into both the fresh-file and splice paths.

`generatePackagesIndex` was **left unchanged** — it was not part of the report and
has a distinct (frontmatter + `# Package Reference`) shape; applying the same
marker-merge to it is recorded as a follow-up, not done here, to keep the change
focused.

Red test now GREEN: **`TestIssue6_CommandsIndex_ClobbersHandEditedIndex`**.

### 7.2 Residual — TTY conflict prompt (§2.3)

`promptOverwrite` (`internal/generator/hash.go`) now detects non-interactive up
front, **before** any `huh` call, via a new `isNonInteractive()` that treats as
non-interactive: `GTB_NON_INTERACTIVE=true` (compatibility shim retained),
`CI=true` (Q4 → honoured, matching `pkg/cmd/root.isCIEnvironment`), a non-terminal
stdin (`utils.IsInteractive`), **or** an unopenable controlling terminal. The last
signal is the decisive one for headless containers where stdin is a char device
(e.g. `/dev/null`) yet no controlling terminal is attachable: `controllingTerminalAvailable`
probes `/dev/tty` (unix, `tty_unix.go`) / `CONIN$` (windows, `tty_windows.go`) —
exactly the device `huh`/bubbletea drives — and, when it cannot be opened,
resolves by the safe default (**skip**) with no terminal attempt and no per-file
`Prompt failed … open /dev/tty` warning. Because `promptOverwrite` is the shared
choke point for both `verifyHash` and `checkSkeletonConflict`, `regenerate` gets
the same clean behaviour (**Q5 → yes**). Interactive runs (real TTY) are
unchanged: the select prompt, including `View diff`, still appears.

Red test now GREEN: **`TestIssue6_ConflictPrompt_NonInteractiveEmitsTTYNoise`**.

### 7.3 Deviation — the `--overwrite=fail` / `--on-conflict` flag (Q1)

**Deferred, not implemented.** The mandated fix (non-interactive-safe default) and
the two red tests are satisfied without a new policy value. Adding a `fail` value
is *not* a clean drop-in: a skipped conflict is currently **non-fatal** — the
skeleton walk (`walkSkeletonAssets`) deliberately logs and continues on the
`overwrite skipped` error — so a `fail` policy would need new fatal-vs-skip error
propagation threaded through the walk and both pipelines. That is a larger,
independently-testable change with its own blast radius, and neither red test nor
issue #6's core complaint requires it. It is recorded here as a follow-up; the
existing `--overwrite=allow|deny` plus the safe non-interactive default cover the
reported need today.

### 7.4 Verification

- `just build` — clean.
- `go test ./internal/... -race -count=1` — all packages pass.
- Generator e2e (`INT_TEST_E2E=1 INT_TEST_E2E_GENERATOR=1 go test ./test/e2e/... -count=1`) — pass.
- `golangci-lint run` — 0 issues.
- Manual end-to-end (built `bin/gtb`): a scaffolded project's CLI index ships the
  empty marker pair; hand-added prose both **before and after** the markers
  survives a `generate command` while the table updates; a marker-stripped index
  is preserved with a WARN; a `.gtb/ignore` entry for the index skips it entirely.
- Docs updated: `docs/how-to/configure-generator-ignore.md` (commands-index
  section) and `docs/development/code-generation-flows.md` (conflict-prompt +
  conflict-aware index notes).
