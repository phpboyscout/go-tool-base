---
title: Configure Generator Ignore Rules
description: How to use .gtb/ignore to prevent the generator from overwriting customised files during regeneration.
date: 2026-03-31
tags: [how-to, generator, ignore, regeneration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure Generator Ignore Rules

When you run `regenerate`, the GTB generator walks every file it owns: the skeleton assets (CI workflows, build configs, `.goreleaser.yaml`, docs) *and* each command's generated `cmd.go`, `init.go` and `main_test.go`, and either writes or prompts to overwrite each one. If you've heavily customised certain files, you'll be prompted to decline overwrites every time.

The `.gtb/ignore` file lets you permanently mark files as "hands off". The generator will skip them without prompting.

**A rule stops the generator *regenerating* a file, not touching it.** Those are
different things, and the difference matters. See
[Two kinds of write](#two-kinds-of-write-regenerate-versus-wiring) below.

A fresh `generate project` scaffold already ships a commented, inert `.gtb/ignore` (a header explaining the syntax and nothing else), so the mechanism is discoverable in every new project.

---

## Quickest path: the `gtb ignore` command

Rather than editing the file by hand, use the `gtb ignore` command group, which manages `.gtb/ignore` for you:

```bash
gtb ignore add justfile              # append a rule (idempotent; creates the file with a header)
gtb ignore add '.github/workflows/**'  # quote globs so your shell does not expand them
gtb ignore seal pkg/cmd/x/cmd.go     # stronger: forbid every write, wiring included
gtb ignore unseal pkg/cmd/x/cmd.go   # back to a plain ignore
gtb ignore check justfile            # managed, ignored or sealed? and which rule decides?
gtb ignore list                      # resolve rules against the manifest's tracked files
gtb ignore remove justfile           # drop the literal rule line
```

Notes:

- These verbs are **pure file edits**: unlike `gtb template`, they do **not** regenerate. The rule takes effect on the next `gtb regenerate`.
- `add` is idempotent: re-adding a present pattern is a reported no-op and never duplicates a line; existing comments and ordering are preserved.
- `remove` matches the **literal rule line**, not any path the glob happens to match, so `remove justfile` never touches an overlapping `*.yml`.
- `check` names the **winning** rule under last-match-wins evaluation, including a `!` negation that re-includes a file: answering "why is this file still being overwritten?".
- `--dry-run` on `add`/`remove` prints the resulting file without writing it.

The rest of this guide describes the file format the command manages, for when you prefer to edit it by hand.

---

## Two kinds of write: regenerate versus wiring

The generator does two quite different things to a generated file.

**Regenerating** rewrites it from source: the generator authors the whole
content and whatever was there is gone. That is what an ignore rule refuses, and
refusing costs nothing. The file simply stays as it is.

**Wiring** is a small, surgical edit that leaves everything around it intact:
registering a subcommand in its parent's `cmd.go`, or injecting a hook stub into
`main.go`. A plain rule does **not** refuse these, because the cost of refusing
lands on your program rather than on the file:

- an unregistered subcommand still **compiles**: it is simply **absent from
  your CLI**, with nothing to tell you why;
- a missing hook stub leaves `cmd.go` calling a function that does not exist,
  which is a compile error.

So a hand-tuned `cmd.go` with an ignore rule keeps your edits *and* keeps
picking up new subcommands. That is almost always what you want.

The same carve-out covers **creating** a command's `main.go`. If you delete one
that a plain rule covers, the generator writes it back, the rendered `cmd.go`
references the `Run…` function that file defines, so refusing would leave a
package that does not compile. Use `sealed` when you mean it should stay gone.

### When you really do mean "never touch this"

Add the `sealed` attribute:

```bash
gtb ignore seal pkg/cmd/deploy/cmd.go
```

which writes:

```
pkg/cmd/deploy/cmd.go sealed
```

A sealed path is never rendered, never wired, never created and never deleted.
Sealing implies ignoring, so one rule is enough.

When a sealed file would have been wired, the run says so and names what it
could not register, then exits 0, you asked for this:

```
WARN sealed, not wired path=pkg/cmd/deploy/cmd.go skipped="registering subcommand push"
WARN sealed file is out of step with the manifest path=pkg/cmd/deploy/cmd.go
     detail="subcommand 'push' in the manifest but cannot be registered in a sealed file — it will be missing from the CLI"
```

Expect to wire it yourself. `gtb ignore unseal <path>` drops back to a plain
ignore (the path stays ignored, use `gtb ignore remove` to hand it back to the
generator entirely).

`-sealed` carves an exception out of a broader rule:

```
docs/**            sealed
docs/index.md      -sealed   # ignored, but the generator may still wire it
```

!!! warning "Sealed rules need gtb v0.37.0 or newer"

    An older gtb does not understand the attribute. It reads
    `pkg/cmd/deploy/cmd.go sealed` as a single pattern containing a space,
    matches nothing, and therefore **regenerates a path you sealed**, silently.
    If your project pins gtb, check the pin before relying on a seal.

---

## Step 1: Create the Ignore File

The scaffold ships one already. To create it manually elsewhere, add `.gtb/ignore` in your project's `.gtb/` directory (alongside `manifest.yaml`), or just run `gtb ignore add <pattern>`, which creates it with a header:

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
| `pattern sealed` | Forbids **every** generator write, wiring included |
| `pattern -sealed` | Ignored, but wiring is allowed again (undoes a broader `sealed`) |
| `# comment` | Ignored (comment line) |

Patterns are evaluated top-to-bottom. Later patterns override earlier ones. This is how negation works.

## Step 3: Regenerate

Run `regenerate` as normal:

```bash
gtb regenerate project
```

Ignored files are skipped without a conflict warning. The run reports a count
at the end, and names each one at debug level:

```
INFO 2 files ignored
DEBU ignored (covered by .gtb/ignore) path=justfile
DEBU ignored (covered by .gtb/ignore) path=.github/workflows/test.yml
```

## What happens to a diverged file you have *not* ignored

Declining an overwrite keeps that one file and the run carries on. It does not
abort, and later commands still regenerate. Whatever is kept is named in a
summary at the end, with the rule that would make the decision permanent:

```
INFO 1 file kept
WARN kept your version path=pkg/cmd/deploy/cmd.go reason="declined at the prompt" remedy="gtb ignore add pkg/cmd/deploy/cmd.go"
```

A kept file keeps the hash the manifest already had, so it conflicts again next
time. That is the prompt still doing its job. Adding the rule is what stops the
question being asked.

If keeping a parent command's `cmd.go` would leave one of its subcommands
unregistered, the summary names the subcommand rather than leaving you to find
out from a build failure.

To see which files will conflict before you regenerate, run `gtb doctor`, its
generator-coverage check reports every tracked file that has diverged and is not
covered by a rule, command files included.

## How Hashing Works

Ignored files stay **tracked** in the manifest, and their recorded hash is
**frozen** at whatever it was when the rule was added. The generator does not
refresh it from disk while the rule is in place.

That is deliberate, and it changed in gtb v0.37.0. Previously the hash tracked
the file on disk, which made this sequence destroy work silently:

```bash
# edit a generated file, then:
gtb ignore add pkg/cmd/deploy/cmd.go
gtb regenerate project          # hash refreshed to match your edit
gtb ignore remove pkg/cmd/deploy/cmd.go
gtb regenerate project          # looks unmodified -> overwritten, no prompt
```

Freezing the hash means un-ignoring raises a conflict and asks you, which is the
safe outcome. The trade is that while a rule is in place the manifest's hash no
longer reflects what is on disk, it reflects the last content the generator
itself wrote.

One consequence worth knowing: a file ignored before it was ever tracked has no
recorded hash at all. There is no baseline to compare against, so the first
regenerate after you un-ignore it will write it. Copy anything you want to keep
before removing such a rule.

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
that write a framework-structural asset, currently `gtb enable signing` and
`gtb disable signing`, which touch `.goreleaser.yaml`, also load these rules
and will **never** write an ignored path, even though they run the generator
with an "allow" overwrite mode. When `.goreleaser.yaml` is ignored (or otherwise
unsafe to edit), enable signing leaves it byte-for-byte untouched and instead
prints the exact top-level `signs:` block for you to paste, while still
scaffolding `internal/trustkeys`, wiring the root command, and updating the
manifest. See [How-to: secure releases](secure-releases.md#customised-goreleaseryaml-and-gtbignore).

## The Generated Commands Index

The CLI commands index, `docs/reference/cli/index.md` (Diátaxis layout) or
`docs/commands/index.md` (flat layout), is regenerated on every `generate
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

- Listing the index in `.gtb/ignore` also protects it: the generator then skips
  it entirely, exactly like any other ignored file:

  ```
  docs/reference/cli/index.md
  ```

## Notes

- A pattern may contain spaces. Trailing words are only read as attributes when
  **every** one of them is a known attribute (`sealed`, `-sealed`), so a rule
  like `my file.yaml` still matches that path. An unrecognised trailing word
  makes the whole line the pattern, `gtb ignore check` will show it matching
  nothing.
- The `--force` flag does **not** override ignore rules. Ignored files stay ignored regardless.
- The ignore check takes precedence over the overwrite mode: an ignored path is never written, even under `--overwrite allow` or `enable signing`'s "allow" overwrite.
- Rules cover command files as well as skeleton files: `pkg/cmd/deploy/cmd.go` is
  a valid pattern, and `gtb ignore check`, `gtb ignore list`, `gtb doctor` and
  `regenerate` all resolve it the same way.
- Missing `.gtb/ignore` is valid: the generator behaves exactly as before (no files ignored).
- Blank lines and lines starting with `#` are ignored.
- Patterns without a `/` match by filename (basename) in any directory.
- Patterns with a `/` match against the full relative path.

---

## Related Documentation

- [Generator Package](../explanation/components/internal/generator.md): full generator architecture and ignore file format
- [Generator Ignore File Spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0048-generator-ignore-file): design specification
