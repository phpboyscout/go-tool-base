---
title: Generate Command
description: Internal command for scaffolding projects, commands, and flags.
date: 2026-02-16
tags: [components, internal, commands, generate]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generate Command

The `generate` command group handles the creation of new assets (projects, commands, flags).

## Help Output (`generate --help`)

```text
Scaffold new projects (skeletons) or add new commands to existing gtb projects.

Usage:
  gtb generate [flags]
  gtb generate [command]

Available Commands:
  add-flag    Add a new flag to an existing command
  command     Generate a new command or subcommand
  docs        Generate documentation for a command using AI
  skeleton    Generate a new cli project skeleton

Flags:
  -h, --help              help for generate
      --model string      AI model to use (per-provider defaults apply when omitted)
      --provider string   AI provider to use (openai/claude/gemini/claude-local/openai-compatible)

Global Flags:
      --ci                   flag to indicate the tools is running in a CI environment
      --config stringArray   config files to use (default [/home/mcockayne/.gtb/config.yaml,/etc/gtb/config.yaml])
      --debug                forces debug log output
```

## Subcommands

### Skeleton

Generates a new project structure from scratch.

**Help (`generate skeleton --help`):**

```text
Generate a new cli project skeleton

Aliases:
  skeleton, project, cli

Flags:
  -d, --description string   Project description (default "A tool built with gtb")
  -f, --features strings     Features to enable (init, update, mcp, docs, doctor, changelog, keychain, ai, config, telemetry) (default [init,update,mcp,docs,doctor,changelog,keychain])
      --git-backend string   Git backend (github or gitlab) (default "github")
      --go-version string    Go version for go.mod (defaults to the running toolchain version)
  -h, --help                 help for skeleton
      --host string          Git Host (e.g. github.com) (default "github.com")
  -n, --name string          Project name (e.g. mytool)
  -p, --path string          Destination path (default ".")
  -r, --repo string          Git repository (e.g. myorg/mytool)
```

#### Feature flags

The `--features` flag controls which subsystems and side-effect activations appear in the scaffolded project. Defaults are selected in the interactive wizard and reflected in the manifest so `gtb regenerate` preserves the choice.

| Feature | Default | What it scaffolds |
|---------|---------|-------------------|
| `init` | on | `init` subcommand |
| `update` | on | self-update subsystem |
| `mcp` | on | MCP server subsystem |
| `docs` | on | built-in docs browser |
| `doctor` | on | `doctor` subsystem |
| `changelog` | on | changelog subsystem |
| `keychain` | on | `cmd/<name>/keychain.go` blank-importing `pkg/credentials/keychain` — activates the OS keychain backend by default. Delete the scaffolded file for a regulated build (no go-keyring / godbus / wincred linked). See [How to configure credentials](../../../how-to/configure-credentials.md) and [How to implement a custom credential backend](../../../how-to/custom-credential-backend.md). |
| `ai` | off (opt-in) | AI chat subsystem |
| `config` | off (opt-in) | `config` subsystem with get/set/list |
| `telemetry` | off (opt-in) | telemetry subsystem |

Example — generate a regulated build with no keychain code linked:

```bash
gtb generate project -n mytool -r myorg/mytool \
  --features init,update,mcp,docs,doctor,changelog
```

#### Post-generation git step

After the skeleton is written and post-processing settles (`go mod tidy`,
`golangci-lint --fix`, manifest-hash refresh), `generate project` makes the
destination a real repository with a single initial commit. This is **opt-out**:
it runs by default, including under `--ci`/non-interactive.

| Flag | Default | Behaviour |
|------|---------|-----------|
| `--no-git` | off (git step on) | Skip the init + initial commit entirely; no `.git` is created. |
| `--push` | off (opt-in) | After the commit, add the derived remote as `origin` and push the default branch. |
| `--git-branch <name>` | `main` | Default branch the initial commit lands on (matches the rendered releaser-pleaser CI). |

Behaviour details:

- **Already a repo.** If the destination is already inside a git repository
  (discovery walks upward for an enclosing `.git`), the whole step is skipped
  with an info log — the scaffold is never committed into someone else's tree,
  and a subdirectory of an existing repo is treated as already-a-repo.
- **Staging.** The tree is staged honouring the generated `.gitignore`, so build
  artefacts are excluded while the `.gitignore` itself is committed.
- **Commit message.** `chore: scaffold <tool> with gtb` — the non-releasing
  `chore:` type means the empty scaffold does not make releaser-pleaser cut a
  release.
- **Author identity.** Resolved in order: host git config (`user.name` /
  `user.email`, repo-local → global → system) → the GTB config keys
  `generator.git.author.name` / `generator.git.author.email` → a safe framework
  fallback (`gtb <gtb@localhost>`), so the commit never fails for lack of an
  identity.
- **Push target.** Derived from the release source as
  `https://{host}/{owner}/{repo}.git` (GitLab nested group paths are preserved),
  using `pkg/vcs/repo`'s provider-aware auth. **Creating the remote on the forge
  is out of scope** — the push assumes the remote exists.
- **Best-effort.** Every git action is non-fatal: any failure (init, commit,
  missing remote, auth, network) logs a warning and generation still succeeds.
  A failed push prints the manual `git push -u origin <branch>` to run once the
  remote exists.
- **Conflicting flags.** `--no-git` with `--push` is rejected — push has no
  commit to publish without the git step.
- **Dry-run.** `--dry-run` performs no git actions; the would-be init/commit
  (and push under `--push`) is listed under "Post-generation actions".
- **`regenerate` / `remove`** never init, stage, commit, or push — the git step
  is exclusive to initial scaffolding via `generate project`.

The step is built on the `pkg/vcs/repo` `Initializer` primitives
(`DiscoverRepository`, `InitLocal`, `AddAll`) plus `CreateRemote` / `Push` — see
[vcs/repo](../../vcs/repo.md).

```bash
# default: scaffold and make the initial commit on main
gtb generate project -n mytool -r myorg/mytool

# scaffold only, no commit
gtb generate project -n mytool -r myorg/mytool --no-git

# scaffold, commit, and push to the (assumed-existing) remote
gtb generate project -n mytool -r myorg/mytool --push
```

### Command

Generates a new Cobra command, optionally using AI or from a script.

**Help (`generate command --help`):**

```text
Generate a new command or subcommand with boilerplate code.

Examples:
  # Generate a command named 'login' in the current project
  gtb generate command --name login --short "Login to the system"

  # Generate a subcommand 'list' under 'login'
  gtb generate command --name list --parent login --short "List sessions"

  # Generate a command with flags and assets
  gtb generate command -n serve -f "port:int:Port to listen on" --assets

  # Generate a command from a script (e.g., bash)
  # The AI will attempt to convert the script to Go code
  # The autonomous agent is used by default for verification
  gtb generate command -n backup --script ./backup.sh

  # Use the original feedback loop instead of the autonomous agent
  gtb generate command -n backup --script ./backup.sh --agentless

  # Create a protected command (cannot be overwritten by generator)
  gtb generate command -n sensible --protected

  # Temporarily unprotect a command to allow overwrite
  gtb generate command -n sensible --protected=false --force

Usage:
  gtb generate command [flags]
  gtb generate command [command]

Available Commands:
  protect     Protect a command from being overwritten
  unprotect   Unprotect a command to allow overwriting

Flags:
      --agentless            Use original retry loop instead of autonomous agent
      --alias strings        Aliases for the command
      --assets               Include assets directory support
      --flag strings         Flags definition (name:type:description:persistent)
      --force                Overwrite existing files
  -h, --help                 help for command
      --long string          Long description
  -n, --name string          Command name (kebab-case)
      --parent string        Parent command name (default: root) (default "root")
  -p, --path string          Path to project root (default ".")
      --persistent-pre-run   Generate a PersistentPreRun hook
      --pre-run              Generate a PreRun hook
      --prompt string        Natural language description or path to a file containing the description
      --protected            Mark the command as protected (tri-state: --protected for true, --protected=false for false, omitted for nil)
      --script string        Path to a script to convert to Go (bash/python/js)
  -s, --short string         Short description
```

### Add Flag

Injects a new flag into an existing command file.

**Help (`generate add-flag --help`):**

```text
Add a new flag to an existing command

Usage:
  gtb generate add-flag [flags]

Flags:
  -c, --command string       Command name to add the flag to
  -d, --description string   Flag description
  -h, --help                 help for add-flag
  -n, --name string          Flag name
  -p, --path string          Path to project root (default ".")
      --persistent           Make the flag persistent
  -t, --type string          Flag type (string, bool, int, float64, stringSlice, intSlice) (default "string")
```

Both invocation paths validate their inputs. The non-interactive path
(both `--command` and `--name` supplied on the command line) applies the
same rules the interactive wizard enforces before touching the manifest:

- the command path is validated segment-by-segment via
  `generator.ValidateCommandName` (kebab-case + underscore, lowercase
  first, no path separators or dots),
- `--name` is validated by `generator.ValidateFlagName`
  (`^[a-z][a-z0-9-]{0,63}$`), and
- `--type` is validated by `generator.ValidateFlagType` (one of the
  renderable types; an unknown type is rejected rather than silently
  degraded to a string flag).

A bad value returns a `generator.ErrInvalidInput`-wrapped error instead
of corrupting `.gtb/manifest.yaml`.

### Docs

Generates documentation for a command. **AI doc-generation is opt-in:** it runs
only when an AI provider is explicitly configured (via `--provider` or the
`ai.provider` config key) and `--agentless` is not set. Otherwise — and this is
the default — GTB writes deterministic **boilerplate** documentation from the
manifest with **no API call**. A configured-but-failing AI call (e.g. no API
credit) degrades quietly to the same boilerplate rather than erroring loudly.
The same gate governs the docs step of `generate command`, so scaffolding a new
command never reaches out to a paid AI API by default.

**Help (`generate docs --help`):**

```text
Generate Markdown documentation for a Go command.

When an AI provider is configured (via --provider or the ai.provider config
key) and --agentless is not set, the command source is analysed and the AI
integration produces rich docs following the docs-site conventions. Otherwise
GTB writes deterministic boilerplate documentation with no API call — AI
doc-generation is opt-in.

Examples:
  # Boilerplate docs (no AI), the default
  gtb generate docs --command mycmd

  # AI-assisted docs (requires a configured provider)
  gtb generate docs --command mycmd --provider claude

  # Force boilerplate even with a provider configured
  gtb generate docs --command mycmd --agentless

Usage:
  gtb generate docs [flags]

Flags:
      --agentless        Skip AI doc-generation and write boilerplate docs only
      --command string   Name/Path of command to document
  -h, --help             help for docs
  -n, --name string      Command name (optional, inferred from path)
      --package string   Path to package to document (relative to project root)
      --parent string    Parent command name (optional, if not in manifest)
      --path string      Path to project root (default ".")
      --source string    Path to the command source code (deprecated, use --command)
```

`--source` is a **deprecated** alias for `--command` (Cobra prints a
deprecation warning steering you to `--command`). It still works:
`docs` falls back to `--source` when `--command` is empty, and it
participates in the one-required `{command|package|source}` group, so a
source-only invocation is accepted rather than wrongly rejected.
`--source` is mutually exclusive with `--package`.
