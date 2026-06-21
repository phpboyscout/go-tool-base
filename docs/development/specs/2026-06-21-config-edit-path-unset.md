---
title: "`config edit` / `config path` / `config unset` — rounding out the config command surface (B3)"
description: "Fill the obvious gaps in the existing `config` subcommand group with three new subcommands: `config edit` (open the active config file in $EDITOR/$VISUAL, re-validate on save, abort on invalid), `config path` (print the resolved config file path(s) across the merge precedence chain), and `config unset <key>` (remove a key from the user's config file). All three extend pkg/cmd/config/ and reuse the existing file-write helpers (writeConfigAtomic, setNestedKey/deleteNestedKey, resolveWritableConfigPath) and the schema validation pipeline (Container.Validate / ValidationResult) already established by config set/get/list/validate/migrate-credentials."
status: DRAFT
date: 2026-06-21
tags:
  - specification
  - config
  - cli
  - command-surface
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# `config edit` / `config path` / `config unset` — rounding out the config command surface (B3)

Authors
:   Matt Cockayne

Date
:   21 June 2026

Status
:   DRAFT

---

## Summary

The `config` command group (`pkg/cmd/config/`) today exposes `get`, `list`,
`set`, `validate`, and `migrate-credentials` — a read/write surface aimed at CI
automation and scripted setup rather than interactive editing
(`pkg/cmd/config/config.go`). Three obvious operations are still missing next to
those subcommands:

1. **`config edit`** — open the active config file in the user's editor
   (`$VISUAL` → `$EDITOR` → platform fallback), let them hand-edit it, then
   **re-validate the result and persist only if it is valid**, aborting and
   leaving the original file untouched on invalid input or a non-zero editor
   exit.
2. **`config path`** — print the resolved config file path(s). Because GTB
   merges config across a precedence chain (CLI flags > env > file > embedded
   assets > defaults), this prints the **ordered list of files** that
   contributed to the live config, plus the **writable target** that
   `set`/`unset`/`edit` mutate.
3. **`config unset <key>`** — remove a single dot-notation key from the user's
   config **file** (the writable layer only), the inverse of `config set`.

This is purely gap-filling alongside existing subcommands; see
[Decision Log](#decision-log). Nothing is rejected and there is no conflict with
the current command surface.

## Motivation

`config set <key> <value>` exists, but there is no `config unset` to undo it —
users must hand-edit YAML or delete the whole file. There is no way to ask the
tool *where* its config actually lives (a frequent support question, made
non-trivial by the multi-file merge precedence and the difference between
"files that were read" and "the file a write lands in"). And there is no
ergonomic "just let me edit the file safely" path: today a user editing the file
by hand gets no validation until the next command run, and a typo silently
breaks the tool or — under hot-reload (`pkg/config` `Observable`/watcher) — is
rejected fail-closed with only a log line.

All three are small, self-contained, and reuse machinery that already exists in
`pkg/cmd/config/` and `pkg/config/`. They round out the command to parity with
the config CLIs of comparable tools (`gh config`, `git config`).

## Scope

`ConfigCmd` is **default-disabled** (`pkg/props/tool.go`:
`isDefaultEnabled` lists `ConfigCmd` under the `false` branch; it is absent from
`DefaultFeatures`). These subcommands inherit that gating automatically — a tool
author opts in with `props.Enable(props.ConfigCmd)`. No feature-flag change is
required and none is made.

### In scope

- Three new subcommands registered in `NewCmdConfig`
  (`pkg/cmd/config/config.go`).
- `$VISUAL`/`$EDITOR` resolution with a platform fallback, surfaced as a small
  reusable helper.
- A read-modify-write round-trip for `edit` over the `afero.Fs` in
  `props.FS`, re-using `writeConfigAtomic` and the existing
  `resolveWritableConfigPath`.
- Re-validation of the edited / post-unset config against the base schema
  (`buildBaseSchema` in `validate.go`) before persisting.
- A new public accessor on `config.Container` to expose the ordered list of
  contributing config files for `config path` (see
  [API Surface](#public-api-surface)).
- Unit tests (≥90% for any new `pkg/` code) and Gherkin E2E scenarios
  (`config edit` and `config unset` are user-facing CLI workflows).

### Out of scope

- Encrypting or migrating config (covered by `migrate-credentials`).
- Editing env-var or flag-sourced values (those layers are not file-backed;
  `unset`/`edit` operate on the file layer only — documented explicitly).
- A TUI form editor (`pkg/forms`) — `edit` shells out to the user's editor by
  design.
- Changing the merge-precedence model or the writable-path resolution rules.

## Design

### Subcommand: `config path`

```
config path [--writable] [--output json|yaml|table]
```

- **Default**: prints every file that contributed to the live config, in merge
  order, one per line, followed by the writable target. Mirrors the
  `output.IsJSONOutput`/`output.Emit` pattern used by `config get`/`list` so the
  structured forms are scriptable.
- **`--writable`**: prints only the single path that `set`/`unset`/`edit` write
  to (the output of `resolveWritableConfigPath`), for use in scripts.
- Resolution:
  - The ordered contributing files come from a new
    `Container.ConfigFiles() []string` accessor (the private `configFiles`
    slice is otherwise unreachable; `GetViper().ConfigFileUsed()` returns only
    the *last* file, which is insufficient for a multi-file merge). See
    [API Surface](#public-api-surface).
  - The writable target is `resolveWritableConfigPath(props, fs)` (already in
    `set.go`), which returns `viper.ConfigFileUsed()` when bound, else
    `filepath.Join(setup.GetDefaultConfigDir(...), setup.DefaultConfigFilename)`.
- When no file is loaded (embedded-only config), `path` prints just the
  writable target (the file a future `set` would create) and notes that no file
  is currently loaded — it does **not** error.

### Subcommand: `config unset <key>`

```
config unset <key>
```

- `Args: cobra.ExactArgs(1)`; key is dot-notation, matching `set`/`get`.
- Errors with `config key %q not found` (matching `get`'s message) if the key
  is **not present in the file layer** — checked via `Container.Has(key)`
  (Viper `InConfig`, which inspects only file-sourced keys, exactly the layer
  `unset` can affect). This avoids the surprise of "unset succeeded" for a key
  that only ever came from an env var or flag and is therefore untouched.
- Mechanics mirror `persistConfigValue` in `set.go`, substituting the delete:
  1. `fs := writableFS(props)`; `path := resolveWritableConfigPath(props, fs)`.
  2. Read + `yaml.Unmarshal` the existing file into `map[string]any`.
  3. `deleteNestedKey(settings, key)` (already in `migrate.go`).
  4. **Re-validate** the resulting settings against `buildBaseSchema()` before
     writing — removing a required key (e.g. `log.level`) must be refused, not
     silently persisted. On invalid result, abort with the
     `printValidationResult` output and a non-zero exit; the file is untouched.
  5. `yaml.Marshal` + `writeConfigAtomic(fs, path, data)` (0600, temp-file +
     rename).
  6. Reload so subsequent `props.Config` reads reflect the write (same
     `ReadInConfig` step `migrate.go` performs after its rewrite).
- Prints `unset %s\n` on success; honours `output.Emit` for `--output json`.

### Subcommand: `config edit`

```
config edit [--editor <cmd>]
```

The round-trip, all over `props.FS` (`afero.Fs`) so it is testable without a
real terminal or editor:

1. **Resolve the writable path** via `resolveWritableConfigPath`. If the file
   does not yet exist, seed the temp file with the current effective config
   (so the user edits a populated file, not a blank one) — or, if nothing is
   loaded, an empty document with a header comment.
2. **Resolve the editor**: `--editor` flag → `$VISUAL` → `$EDITOR` → platform
   fallback (`vi` on unix, `notepad` on windows). Surfaced as
   `resolveEditor(flag string) (string, error)` (see [API Surface]).
3. **Require an interactive terminal**: if `utils.IsInteractive()` is false
   (piped/CI), refuse with a clear error pointing at `config set`/`unset` for
   non-interactive use. Literal-editor invocation in CI is a footgun.
4. **Temp-file round-trip**:
   - Write the current file contents (or seed) to a temp path on `props.FS`
     (sibling of the target, `*.edit.tmp`, mode 0600).
   - Launch the editor against the temp path and wait. The subprocess
     invocation is intentional dev-tooling exec and must carry a narrowly
     scoped `//nolint:gosec // G204: operator-selected editor on an
     operator-named temp file` per the project's suppression policy. Editor
     argv is split on whitespace to allow `$EDITOR="code --wait"`.
   - On non-zero editor exit, **abort**: discard the temp file, leave the
     original untouched.
5. **Re-validate before persist**:
   - Read the edited temp file; `yaml.Unmarshal` to detect syntax errors
     (abort with a parse error, keep the temp file path in the message so the
     user can recover their edit).
   - Build a throwaway validation view and run it against `buildBaseSchema()`.
     On invalid, print `printValidationResult`, **abort**, and tell the user
     their unsaved edit is preserved at the temp path.
6. **Persist atomically**: `writeConfigAtomic(fs, path, editedData)`, then
   reload (`ReadInConfig`) so the live `props.Config` reflects the edit. Print
   a confirmation; no-op message if the content was unchanged.

> Note on `props.FS` vs. the real editor: the editor subprocess edits a real OS
> path. When `props.FS` is the default `afero.NewOsFs()` this is the same file.
> Tests inject a memmap FS and a **fake editor** (a deterministic mutator
> function) through the same seam used elsewhere — the editor is invoked via an
> injectable `editorRunner func(ctx, editorCmd, path) error` field on the
> command constructor, **not** a package-level `var` (the project bans
> package-level exec hooks for `t.Parallel()` race-safety; see CLAUDE.md
> Testing). `internal/exectest` provides the real-exec wiring.

### Validation reuse

All three persist paths route through the **same** schema check as
`config validate`: `buildBaseSchema()` → `props.Config.Validate(schema)` →
`ValidationResult.Valid()` / `printValidationResult`. This keeps a single
source of truth for "what a valid config is" and means `edit`/`unset` can never
write a config that `validate` would reject.

## Public API surface

New exported symbol in `pkg/config` (the only library change; the rest lives in
the command package `pkg/cmd/config`, which is not part of the stable surface):

```go
// ConfigFiles returns the ordered list of config files that contributed to
// this container's live configuration, in merge order (lowest to highest
// precedence among file sources). Returns an empty slice for reader/embedded
// containers that were not loaded from any file. The slice is a copy; callers
// may not mutate the container's internal state through it.
func (c *Container) ConfigFiles() []string
```

- Added to `pkg/config/container.go` next to the existing accessors; backed by
  the existing private `configFiles []string` field (copied under `c.mu`, same
  discipline as `reload`'s `append([]string(nil), c.configFiles...)`).
- Added to the `Containable` interface in `container.go` so the command can
  depend on the interface, not the concrete type. **This is a breaking change
  to `Containable`** (a new method) — permitted pre-1.0 as a minor bump per the
  API-stability policy in `CLAUDE.md`; note it in the commit body and add a
  `docs/migration/` note. Any downstream implementing `Containable` directly
  (rare; most embed `*Container`) must add the method. The project's `mocks/`
  for `Containable` are regenerated via `just mocks`.

Command constructors (package-private, mirroring existing ones):

```go
func NewCmdPath(props *p.Props) *cobra.Command
func NewCmdUnset(props *p.Props) *cobra.Command
func NewCmdEdit(props *p.Props, opts ...EditOption) *cobra.Command  // EditOption injects the editorRunner for tests
```

All three are registered in `NewCmdConfig` via `setup.Wrap(p.ConfigCmd, …)`,
matching the existing registration block.

Editor helper (package-private to `pkg/cmd/config`, or `internal/` if reused):

```go
func resolveEditor(flagVal string) (cmd string, err error)
```

## Error handling

- All errors use `github.com/cockroachdb/errors` (project standard), with
  `WithHint` where a user action is implied (e.g. "set $EDITOR or pass
  --editor", "run `config validate` to see what's wrong").
- `edit`/`unset` are **fail-closed on validation**: an invalid result aborts
  and the original file is never overwritten. The atomic temp-file+rename in
  `writeConfigAtomic` guarantees no torn write even if persistence itself is
  interrupted.
- `edit` preserves the user's work: on parse/validation failure the temp file
  is **retained** and its path reported, so edits aren't lost.

## Testing strategy

Unit (table-driven, `t.Parallel()`, `logger.NewNoop()`, memmap `afero.Fs`):

- `config path`: single file, multi-file merge, no-file (embedded-only),
  `--writable`, and all three `--output` formats.
- `config unset`: present file key (removed + file rewritten), key absent from
  file but present via env (refused with not-found), required-key removal
  refused by validation, JSON output, nested key.
- `config edit`: happy path (fake editor mutates temp → validates → persisted +
  reloaded), editor non-zero exit (abort, original intact), invalid YAML after
  edit (abort, temp retained), schema-invalid after edit (abort, temp
  retained), non-interactive refusal, `--editor` override, `$VISUAL` over
  `$EDITOR` precedence, unchanged-content no-op.
- `ConfigFiles()`: copy semantics (mutating the returned slice doesn't affect
  the container), empty for embedded containers, order preserved.

E2E (Godog, `features/`, gated `INT_TEST_E2E_CLI=1`): a `config-surface.feature`
with scenarios for `config path` output and `config unset` round-trip using the
`cmd/e2e/` test binary (all features enabled). `config edit` uses a scripted
fake editor via an env-var-named command so the scenario is deterministic.

## Documentation

- Update `docs/components/` config command reference with the three new
  subcommands and the file-layer-only caveat for `unset`/`edit`.
- Add a `docs/migration/` note for the `Containable.ConfigFiles()` addition.
- Cross-reference the validation reuse and the writable-path resolution rules.

## Implementation plan

1. Add `Container.ConfigFiles()` + `Containable` method; regenerate mocks;
   migration note. (`pkg/config`)
2. `config path` subcommand + tests.
3. `config unset` subcommand + tests (reuse `deleteNestedKey`,
   `resolveWritableConfigPath`, `writeConfigAtomic`, `buildBaseSchema`).
4. `resolveEditor` helper + `config edit` subcommand with injectable
   `editorRunner` + tests.
5. Register all three in `NewCmdConfig`.
6. Gherkin scenarios + step defs.
7. `/gtb-verify`, docs, `/simplify`.

## Open questions

1. **`config path` default output** — list *all* contributing files plus the
   writable target, or only the writable target by default (with `--all` to
   expand)? This spec proposes "all files + writable target" as the default
   because the multi-file merge is exactly what makes the path
   non-obvious. Confirm.
2. **`config edit` seeding** — when the target file does not yet exist, seed
   the temp file with the **full effective config** (all merged values,
   including embedded defaults) or just an **empty/commented skeleton**? Seeding
   with the full effective config risks materialising embedded defaults into the
   user file (the very thing `persistConfigValue` is careful to avoid). Proposed:
   seed with an empty document + a header comment, so `edit` never silently
   bakes defaults into the file. Confirm.
3. **Editor argv splitting** — naive whitespace split handles
   `EDITOR="code --wait"` but not quoted paths with spaces. Acceptable for v1,
   or use `shellwords`-style parsing? Proposed: whitespace split for v1,
   documented.
4. **`ConfigFiles()` on `Containable`** — add to the interface (breaking, but
   clean) or expose only on the concrete `*Container` and have the command
   type-assert? Proposed: add to the interface (pre-1.0 minor break is
   acceptable and keeps the command interface-clean). Confirm.

## Resolutions (open questions confirmed with user 2026-06-21)

1. **`config path` default output** — RESOLVED: print **all contributing files
   (in merge order) plus the writable target** by default. The multi-file merge
   is what makes the resolved path non-obvious, so the full picture is the
   useful default.
2. **`config edit` seeding** — RESOLVED: seed a not-yet-existing target with an
   **empty document + header comment**, never the full effective config. `edit`
   must not silently materialise embedded defaults into the user file.
3. **Editor argv splitting** — RESOLVED: **handle both** — use shellwords-style
   (shell-quoting-aware) parsing so it covers both `EDITOR="code --wait"` and
   quoted paths containing spaces. This supersedes the draft's "whitespace split
   for v1" proposal; the design/testing sections should specify shell-quoting-
   aware parsing (table-driven cases: unquoted flags, quoted paths with spaces,
   mixed).
4. **`ConfigFiles()` on `Containable`** — RESOLVED: **add it to the interface.**
   The pre-1.0 minor break is acceptable; regenerate mocks and add a migration
   note. Keeps the command interface-typed.

## Decision Log

| Decision | Status | Rationale |
|----------|--------|-----------|
| Add `config edit`, `config path`, `config unset` | Accepted | Obvious gaps next to existing `get`/`set`/`list`/`validate`/`migrate-credentials`; no conflict with current surface. |
| Reuse `writeConfigAtomic` / `setNestedKey` / `deleteNestedKey` / `resolveWritableConfigPath` / `buildBaseSchema` | Accepted | Single source of truth for file writes + validation; no new write path. |
| `unset`/`edit` operate on the **file layer only** | Accepted | Env/flag layers are not file-backed; mutating them is out of scope and would be surprising. |
| `config edit` requires an interactive TTY (`utils.IsInteractive`) | Accepted | Shelling to `$EDITOR` in CI is a hang/footgun; `set`/`unset` cover scripted use. |
| Editor invocation via **injectable runner**, not a package-level `var` | Accepted | Project bans package-level exec hooks for `t.Parallel()` race-safety (CLAUDE.md). |
| Add `Container.ConfigFiles()` to `Containable` (breaking) | Accepted (pre-1.0 minor) | Needed for multi-file `config path`; `ConfigFileUsed()` only returns the last file. |
| _Nothing rejected_ | — | No conflicting or superseded design considered; this is gap-filling. |
