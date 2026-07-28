---
title: generate Command
description: Framework-developer command to scaffold new projects, commands, flags, and docs with the gtb CLI.
date: 2026-06-26
tags: [reference, commands, generate, scaffolding, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `generate` Command

`gtb generate` scaffolds new projects and commands. It is part of the
**framework-developer** CLI (the `gtb` binary), used while building a tool — not a
runtime command shipped in your tool. See the
[Scaffolding](../../how-to/framework-cli/scaffold-project.md) and
[Generating Commands](../../how-to/framework-cli/generate-commands.md) how-tos for
task walkthroughs.

## Usage

```bash
gtb generate <subcommand> [flags]
```

## Subcommands

| Subcommand | Purpose |
|---|---|
| `project` | Generate a new project skeleton. |
| `command` | Generate a new command or subcommand. |
| `add-flag` | Add a flag to an existing command. |
| `protect <command-path>` | Mark a command as protected from regeneration overwrites. |
| `unprotect <command-path>` | Allow a command to be overwritten again. |
| `docs` | Generate Markdown documentation for a command or package. |
| `man` | Generate roff man pages for the command tree. |

### `generate project`

Generate a new project skeleton. Run without `--name`/`--repo` in an interactive
terminal to launch the guided wizard; otherwise supply the flags directly.

**Core:**

| Flag | Default | Description |
|------|---------|-------------|
| `--name, -n` | — | Project name (e.g. `als`). |
| `--repo, -r` | — | Repository in `org/repo` format. |
| `--git-backend` | `github` | Git backend: `github` or `gitlab`. |
| `--host` | *(backend's canonical host)* | Git host (for self-managed instances). |
| `--private` | `false` | Mark the repository private (requires a token for updates). |
| `--description, -d` | `A tool built with gtb` | Project description. |
| `--features, -f` | `init,update,mcp,docs,doctor,changelog,keychain` | Features to enable (also `ai`, `config`, `telemetry`). |
| `--go-version` | *(running toolchain)* | Go version for `go.mod`. |
| `--help-type` | `none` | Help channel type: `slack`, `teams`, or `none` (with `--slack-*`/`--teams-*`). |
| `--path, -p` | `.` | Destination path. |
| `--overwrite` | `ask` | File-conflict handling: `allow`, `deny`, or `ask`. |
| `--env-prefix` | — | Env-var prefix for config overrides (e.g. `MY_APP`). |
| `--update-policy` | *(framework default: disabled)* | Self-update posture: `disabled`, `prompt`, or `enabled`. |
| `--update-check-interval` | *(framework default: 24h)* | Interval between self-update checks, as a Go duration (e.g. `24h`, `168h`). |
| `--ci-component-source` | *(gitlab.com/phpboyscout/cicd)* | Override the `phpboyscout/cicd` component include base in the scaffolded GitLab pipeline. |
| `--template` | — | Custom template overlay source `<src>@<ref>` (local path or forge repo); repeatable, layered in order. |

**Help channel** (used when `--help-type` is `slack`/`teams`): `--slack-channel`, `--slack-team`, `--teams-channel`, `--teams-team`.

**Git lifecycle** (the new project is git-initialised with an initial commit by default):

| Flag | Default | Description |
|------|---------|-------------|
| `--no-git` | `false` | Skip the post-generation git init and initial commit. |
| `--push` | `false` | After the initial commit, add the derived remote as `origin` and push (push failures are non-fatal). Conflicts with `--no-git`. |
| `--git-branch` | `main` | Default branch the initial commit lands on. |

**Release signing** (off by default; supplying `--signing-email` implies `--signing`):

| Flag | Default | Description |
|------|---------|-------------|
| `--signing` | `false` | Enable consumer-side release-signature verification (scaffolds `internal/trustkeys`, wires `props.Signing`). |
| `--signing-email` | — | Release WKD email (`external_key_email`); enables the external trust-anchor leg. |
| `--signing-key-source` | `both` | Trust-anchor source: `embedded`, `external`, or `both`. |
| `--signing-require-external-crosscheck` | `false` | Fail signing closed when the external (WKD) resolver is unreachable. |
| `--signing-key-id` | — | Signing key id/ARN/alias (or PEM path for `local`) the release pipeline signs with; wires the GoReleaser signs block. |
| `--signing-backend` | *(aws-kms when `--signing-key-id` set)* | `gtb sign` backend for the release pipeline. |
| `--signing-kms-region` | *(eu-west-2)* | AWS region for the `aws-kms` backend. |
| `--signing-public-key` | *(internal/trustkeys/keys/signing-key-v1.asc)* | Path to the embedded public key the signature identifies. |

### `generate command`

Generate a new command or subcommand (optionally AI-converted from a script).

| Flag | Default | Description |
|------|---------|-------------|
| `--name, -n` | — | Command name (kebab-case). |
| `--short, -s` / `--long, -l` | — | Short / long help text. |
| `--parent` | `root` | Parent command to nest under; use `parent/child` for deep nesting. |
| `--args` | — | Positional-arg validator (e.g. `ExactArgs(1)`, `ArbitraryArgs`). |
| `--alias, -a` | — | Command alias(es) (repeatable). |
| `--flag, -f` | — | Flag spec(s) to add (repeatable): `name:type:description:persistent:shorthand:required:default:defaultIsCode`. |
| `--assets` | `false` | Include assets-directory support. |
| `--script` | — | Path to a script (bash/python/js) to convert to Go. Mutually exclusive with `--prompt`. |
| `--prompt` | — | Natural-language description (or a file path) to generate from. Mutually exclusive with `--script`. |
| `--agentless` | `false` | Use the original retry loop instead of the autonomous repair agent. |
| `--max-steps` | `0` (→20) | Max repair-agent reasoning steps. |
| `--non-interactive` | *(true when `CI` is set)* | Never pause for input: disables the repair agent's `query_user` tool. |
| `--persistent-pre-run` / `--pre-run` | `false` | Generate the corresponding hook. |
| `--with-initializer` | `false` | Generate an Initializer for this command. |
| `--with-config-validation` | `false` | Generate a config-validation stub for this command. |
| `--force` | `false` | Overwrite existing files. |
| `--protected` | `false` | Mark the command as protected from regeneration (tri-state: `--protected`, `--protected=false`, or omitted for nil). |
| `--mcp-enabled` | `true` | Expose this command as an MCP tool (tri-state: `--mcp-enabled=false` withholds it from the MCP surface; it stays runnable on the CLI). |
| `--path, -p` | `.` | Filesystem project root (not a command path). |

All `generate` subcommands also accept these persistent flags (for AI-assisted generation): `--provider` (AI provider: `openai`/`gemini`/`claude`), `--model` (AI model), and `--dry-run` (preview changes without writing files).

### `generate add-flag`

Add a new flag to an existing command (see [Add Flags](../../how-to/framework-cli/add-flags.md)).

### `generate docs`

Generate Markdown docs for a command or package.

| Flag | Default | Description |
|------|---------|-------------|
| `--command` | — | Name/path of the command to document. |
| `--package` | — | Package to document (relative to project root). |
| `--parent` | — | Parent command name (if not in the manifest). |
| `--agentless` | `false` | Skip AI generation; write boilerplate only. |
| `--public-api` | `false` | Module is publicly published: defer package API reference to pkg.go.dev (otherwise a local `go doc` hint). Equivalent to `module_published: true` in the manifest. |
| `--no-ai-attribution` | `false` | Keep AI/model attribution out of the generated frontmatter `authors:` — human author(s) only. Default: the AI model is appended as an additive co-author. |
| `--path` | `.` | Project root. |

One of `--command`/`--package`/`--source` is required. (`--source` is deprecated; use `--command`.)

Docs are emitted in the project's layout (`docs_layout` in `.gtb/manifest.yaml`): the Diátaxis quadrant tree (`docs/reference/cli/`, `docs/explanation/components/`) for new projects, or the legacy flat tree (`docs/commands/`, `docs/packages/`). See [Generating Documentation](../../how-to/framework-cli/generate-docs.md) → "Documentation layout".

### `generate man`

Generate roff man pages for the command tree.

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | `./man` | Output directory (pages under `<dir>/man<section>`). |
| `--section` | `1` | Man section number. |
| `--source` | *(`<tool> <version>`)* | `TH` source footer. |
| `--manual` | *(`<Tool> Manual`)* | `TH` manual title. |
| `--date` | *(none — reproducible)* | Stamp this date (`YYYY-MM-DD` or RFC3339) into the `.TH` header. Omit for reproducible output with no date trailer. |

### `generate protect` / `generate unprotect`

`gtb generate protect <command-path>` marks a command so regeneration won't
overwrite it; `unprotect` reverses it. See
[Configure Generator Ignore](../../how-to/configure-generator-ignore.md).

> Run any subcommand with `--help` for the complete, authoritative flag set.
