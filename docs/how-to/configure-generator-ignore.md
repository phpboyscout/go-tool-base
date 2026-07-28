---
title: Configure Generator Ignore Rules
description: How to use .gtb/ignore to prevent the generator from overwriting customised files during regeneration.
date: 2026-03-31
tags: [how-to, generator, ignore, regeneration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure Generator Ignore Rules

When you run `regenerate`, the GTB generator walks all skeleton template files and either writes or prompts to overwrite each one. If you've heavily customised certain files (CI workflows, build configs, Dockerfiles), you'll be prompted to decline overwrites every time.

The `.gtb/ignore` file lets you permanently mark files as "hands off" — the generator will skip them without prompting.

A fresh `generate project` scaffold already ships a commented, inert `.gtb/ignore` (a header explaining the syntax and nothing else), so the mechanism is discoverable in every new project.

---

## Quickest path: the `gtb ignore` command

Rather than editing the file by hand, use the `gtb ignore` command group, which manages `.gtb/ignore` for you:

```bash
gtb ignore add justfile              # append a rule (idempotent; creates the file with a header)
gtb ignore add '.github/workflows/**'  # quote globs so your shell does not expand them
gtb ignore check justfile            # is it ignored? and which rule decides?
gtb ignore list                      # resolve rules against the manifest's tracked files
gtb ignore remove justfile           # drop the literal rule line
```

Notes:

- These verbs are **pure file edits** — unlike `gtb template`, they do **not** regenerate. The rule takes effect on the next `gtb regenerate`.
- `add` is idempotent: re-adding a present pattern is a reported no-op and never duplicates a line; existing comments and ordering are preserved.
- `remove` matches the **literal rule line**, not any path the glob happens to match — so `remove justfile` never touches an overlapping `*.yml`.
- `check` names the **winning** rule under last-match-wins evaluation, including a `!` negation that re-includes a file — answering "why is this file still being overwritten?".
- `--dry-run` on `add`/`remove` prints the resulting file without writing it.

The rest of this guide describes the file format the command manages, for when you prefer to edit it by hand.

---

## Step 1: Create the Ignore File

The scaffold ships one already. To create it manually elsewhere, add `.gtb/ignore` in your project's `.gtb/` directory (alongside `manifest.yaml`) — or just run `gtb ignore add <pattern>`, which creates it with a header:

```bash
touch .gtb/ignore
```

## Step 2: Add Patterns

The syntax is gitignore-like. Add one pattern per line:

```
# Ignore my custom CI workflows
.github/workflows/**

# But keep the release workflow managed by the generator
!.github/workflows/release.yml

# Ignore my custom build config
justfile

# Ignore Docker files
Dockerfile
docker-compose.yml
```

### Pattern Types

| Pattern | What it matches |
|---------|----------------|
| `justfile` | Exact filename in any directory |
| `*.yml` | All `.yml` files in any directory |
| `.github/**` | Everything under `.github/` |
| `.github/workflows/test.yml` | Exact path only |
| `!pattern` | Re-includes a file excluded by an earlier pattern |
| `# comment` | Ignored (comment line) |

Patterns are evaluated top-to-bottom. Later patterns override earlier ones — this is how negation works.

## Step 3: Regenerate

Run `regenerate` as normal:

```bash
gtb regenerate project
```

Ignored files will be skipped silently. You'll see debug output for each skipped file if you run with `--debug`:

```
DEBU Ignored by .gtb/ignore: justfile
DEBU Ignored by .gtb/ignore: .github/workflows/test.yml
```

## How Hashing Works

Ignored files are **still tracked** in the manifest. The generator reads the current on-disk content of each ignored file and records its hash. This means:

- The manifest stays accurate — it reflects what's actually on disk
- Future regenerations know the file exists and hasn't been touched by the generator
- If you remove a file from `.gtb/ignore` later, the generator can detect whether you've modified it since the last regeneration

If an ignored file doesn't exist on disk (e.g. you deleted it), no hash is recorded.

## Common Patterns

### Protect All CI, Keep Release Managed

```
.github/workflows/**
!.github/workflows/release.yml
```

### Protect Build Configuration

```
justfile
Makefile
Dockerfile
docker-compose.yml
```

### Protect Everything Except Go Code

```
*.yml
*.yaml
*.json
!go.mod
```

## Targeted `enable` / `disable` commands honour it too

`.gtb/ignore` is not just a `regenerate project` concern. The targeted commands
that write a framework-structural asset — currently `gtb enable signing` and
`gtb disable signing`, which touch `.goreleaser.yaml` — also load these rules
and will **never** write an ignored path, even though they run the generator
with an "allow" overwrite mode. When `.goreleaser.yaml` is ignored (or otherwise
unsafe to edit), enable signing leaves it byte-for-byte untouched and instead
prints the exact top-level `signs:` block for you to paste, while still
scaffolding `internal/trustkeys`, wiring the root command, and updating the
manifest. See [How-to: secure releases](secure-releases.md#customised-goreleaseryaml-and-gtbignore).

## The Generated Commands Index

The CLI commands index — `docs/reference/cli/index.md` (Diátaxis layout) or
`docs/commands/index.md` (flat layout) — is regenerated on every `generate
command` / `generate add-flag` run so the command table stays current. It is
handled specially so hand-added prose is never silently discarded:

- The generated table is delimited by marker comments:

  ```markdown
  <!-- gtb:commands:start -->
  | Command | Description |
  | :--- | :--- |
  | [deploy](deploy.md) | Deploy the app |
  <!-- gtb:commands:end -->
  ```

  Only the region **between** the markers is rewritten. Any prose you add before
  or after them is preserved. Scaffolded projects ship the index with an empty
  marker pair already in place.

- If the on-disk index has **diverged** from its generated form (the markers were
  removed and hand-written prose is present), the generator leaves it untouched
  and logs a warning rather than clobbering it. Re-add the markers to resume
  automatic table updates.

- Listing the index in `.gtb/ignore` also protects it — the generator then skips
  it entirely, exactly like any other ignored file:

  ```
  docs/reference/cli/index.md
  ```

## Notes

- The `--force` flag does **not** override ignore rules. Ignored files stay ignored regardless.
- The ignore check takes precedence over the overwrite mode — an ignored path is never written even under `enable signing`'s "allow" overwrite.
- Missing `.gtb/ignore` is valid — the generator behaves exactly as before (no files ignored).
- Blank lines and lines starting with `#` are ignored.
- Patterns without a `/` match by filename (basename) in any directory.
- Patterns with a `/` match against the full relative path.

---

## Related Documentation

- [Generator Package](../explanation/components/internal/generator.md) — full generator architecture and ignore file format
- [Generator Ignore File Spec](../development/specs/2026-03-31-generator-ignore-file.md) — design specification
