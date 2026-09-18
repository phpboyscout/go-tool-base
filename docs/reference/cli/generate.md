---
title: generate Command
description: Framework-developer command to scaffold new projects, commands, flags, and docs with the gtb CLI.
date: 2026-06-26
tags: [reference, commands, generate, scaffolding, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `generate` Command

`gtb generate` scaffolds new projects and commands. It is part of the
**framework-developer** CLI (the `gtb` binary), used while building a tool, not a
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

Generate a new project skeleton. Run without `--name` (or without `--repo` for
a hosted project) in an interactive terminal to launch the wizard, whose pages
are described in the [scaffolding how-to](../../how-to/framework-cli/scaffold-project.md#interactive-wizard);
otherwise supply the flags directly. The wizard and the flags produce the same
manifest.

**Core:**

| Flag | Default | Description |
|------|---------|-------------|
| `--name, -n` | — | Project name (e.g. `als`). |
| `--repo, -r` | — | Repository in `org/repo` format. |
| `--forge-backend` | `github` | The forge the project is hosted on: `github`, `gitlab`, `gitea`, `codeberg` or `bitbucket`. Decides the release source, the host default and the CI skeleton (only GitHub and GitLab ship one; the others get no CI files and the run says so). Recorded as `release_source.backend` and enables the forge's feature. Replaces `--git-backend`, which is gone. |
| `--no-forge` | `false` | The project is not hosted on a forge: no backend, no repository; requires `--module`. |
| `--module` | *(`<host>/<org>/<repo>`)* | Go module path. Required with `--no-forge`; otherwise an override for a vanity import path. Recorded as `module_path`. |
| `--forge-credentials` | — | Further forges to enable for credential capture (their `init <forge>` wizard and adapter), never the release source. |
| `--release-channel` | *(`forge` when hosted)* | Where self-update releases from when `update` is enabled. `forge` is the only channel in this release, so a project generated with `--no-forge` cannot enable `update`. The direct channel (a static location, no forge) is withdrawn pending its design, [#90](https://gitlab.com/phpboyscout/go-tool-base/-/issues/90); `direct` is refused. |
| `--host` | *(backend's canonical host)* | Git host, for a self-managed instance. |
| `--private` | `false` | Mark the repository private (requires a token for updates). |
| `--description, -d` | `A tool built with gtb` | Project description. |
| `--features, -f` | `update,init,mcp,docs,doctor,changelog,keychain` | Features to enable: see [below](#features). The flag **replaces** the default set rather than adding to it. A forge name is refused here; the forge is chosen with `--forge-backend`. |
| `--chat-providers` | *(every known provider)* | Chat providers the tool links when `ai` is among its features: see [adapters](#adapters). Ignored without `ai`; empty with `ai` is refused. |
| `--chat-default-provider` | *(the only provider, when one is linked)* | The tool's default chat provider. Required when `--chat-providers` links more than one: the generator does not choose for you. Must be one of the linked providers. Recorded as `chat.default.provider` and shipped as the tool's embedded default: see [chat defaults](#chat-defaults). |
| `--chat-default-model` | *(the provider module's choice)* | Default model for the default provider. |
| `--chat-base-url` | — | API endpoint for the default provider. Required by `openai-compatible` and `azure-openai`; HTTPS, no userinfo, no placeholder host. |
| `--chat-api-version` | — | Dated API version. Required by `azure-openai`, which has no default. |
| `--chat-project` | — | Cloud project, for `gemini-vertex` (optional; falls back to `GOOGLE_CLOUD_PROJECT` at runtime). |
| `--chat-location` | — | Region, for `gemini-vertex` and `bedrock` (optional; falls back to the platform's environment at runtime). |
| `--go-version` | *(running toolchain)* | Go version for `go.mod`. Recorded as `version.go`; `regenerate` renders that, never the toolchain it happens to run on. |
| `--telemetry-endpoint` | — | Where the `telemetry` feature sends usage events, an `http` or `https` URL. Plain `http` is accepted for a collector on a private network and every generate and regenerate warns about it. Recorded as `telemetry.endpoint`. |
| `--telemetry-otel-endpoint` | — | OpenTelemetry collector endpoint. Recorded as `telemetry.otel_endpoint`. |
| `--auto-initialise` | `false` | Run the first-run bootstrap automatically when the config is missing. Recorded as `bootstrap.auto_initialise`. |
| `--skip-config-check` | — | Commands that run without a config file (repeatable). Recorded as `bootstrap.skip_config_check`. |
| `--auxiliary-commands` | — | Commands that take the root pre-run's auxiliary fast path (repeatable). Recorded as `bootstrap.auxiliary_commands`. |
| `--config-layers` | *(framework default)* | Config-stack layers the tool wires, in precedence order. Recorded as `config_layers`. |
| `--help-type` | `none` | Help channel type: `slack`, `teams`, or `none` (with `--slack-*`/`--teams-*`). |
| `--path, -p` | `.` | Destination path. |
| `--overwrite` | `ask` | File-conflict handling: `allow`, `deny`, or `ask`. |
| `--no-verify` | `false` | Skip `go mod tidy` and `golangci-lint` after generation; exit 0 unverified. Without it a failed or unavailable step exits 3 with its reason (see [regenerate's exit codes](regenerate.md#exit-codes-emitted-is-not-verified)). Either way `go.mod` carries the direct requirements the scaffold's imports imply. |
| `--env-prefix` | — | Env-var prefix for config overrides (e.g. `MY_APP`). |
| `--update-policy` | *(framework default: disabled)* | Self-update posture: `disabled`, `prompt`, or `enabled`. |
| `--mcp-mode` | *(compact)* | MCP publication mode: `compact` (three discovery tools) or `direct` (one native tool per command). Recorded as `mcp.mode`; `gtb set mcp.mode` changes it later. See [AI Agents & MCP](../../explanation/components/mcp-agents.md#compact-and-direct-publication). |
| `--update-check-interval` | *(framework default: 24h)* | Interval between self-update checks, as a Go duration (e.g. `24h`, `168h`). |
| `--ci-component-source` | *(gitlab.com/phpboyscout/cicd)* | Override the `phpboyscout/cicd` component include base in the scaffolded GitLab pipeline. |
| `--template` | — | Custom template overlay source `<src>@<ref>` (local path or forge repo); repeatable, layered in order. |

**Help channel** (used when `--help-type` is `slack`/`teams`): `--slack-channel`, `--slack-team`, `--teams-channel`, `--teams-team`. The channel is required for its type; a type outside `slack`, `teams`, `none` is refused.

<a id="features"></a>
**Features** accepted by `--features`:

| Group | Values | Notes |
|-------|--------|-------|
| Built-in commands (default on) | `update`, `init`, `docs`, `doctor`, `changelog` | Wired via `props.SetFeatures`. |
| Built-in commands (opt-in) | `ai`, `config`, `telemetry`, `man` | |
| Forges | *(not selectable here)* | A forge feature is implied by `--forge-backend` (one) and `--forge-credentials` (more). Each enabled forge feature adds that forge's `init <forge>` credential wizard, config section, embedded asset bundle and linked adapter. After generation `gtb enable <forge>` / `gtb disable <forge>` still toggle them. Constants live in `pkg/setup/forge`, not `props`. |
| Links (build-time) | `keychain`, `mcp` | Not `SetFeatures` toggles: each selects a `cmd/<name>/<id>.go` blank import, which the manifest's entry owns. `gtb enable <id>`/`gtb disable <id>` write or remove the file; a hand-deleted file comes back on the next regenerate. `mcp` defaults on (a manifest that says nothing links it; leaving it out of `--features` records `mcp: false`), the keychain off. A binary without `mcp.go` ships without `go/mcp` and the MCP SDK. |

`--features` replaces the default set rather than extending it, so a selection
must name every feature the tool should ship with: `--features gitlab` alone
yields a tool with the six default built-ins **off**. An unrecognised name is
rejected before anything is written.

Every feature can be toggled after generation with
[`gtb enable`/`gtb disable`](enable-disable.md), which leaves the tree in line
in one command: the root command, the adapter files and any derived field a
newly enabled feature needs (the `ai` feature's provider list) are written by
the same run, and no `regenerate` is needed afterwards.

<a id="adapters"></a>
**Adapters.** A chat provider or a forge is a module the tool blank-imports
from its own `main` package, and the generator writes those imports from the
manifest into two `DO NOT EDIT` files beside `keychain.go`:

| File | Derived from | Modules |
|------|--------------|---------|
| `cmd/<name>/chat.go` | `chat.providers` in the manifest, only when `ai` is enabled | `claude`, `claude-local` → `go/chat-anthropic`; `openai`, `openai-compatible`, `codex-local` → `go/chat-openai`; `gemini`, `gemini-vertex`, `agy-local` → `go/chat-gemini`; `bedrock` → `go/chat-bedrock`; `azure-openai` → `go/chat-openai-azure` |
| `cmd/<name>/forge.go` | the enabled forge features, implied by `--forge-backend` and `--forge-credentials` | `github` → `go/forge-github`; `gitlab` → `go/forge-gitlab`; `gitea`, `codeberg` → `go/forge-gitea`; `bitbucket` → `go/forge-bitbucket` |

Every known provider is pre-selected: a generated tool is configured by its
consumers the way `gtb` itself is, so it ships every provider and the operator
narrows with `--chat-providers` or the wizard. Generation only emits the
import; what the running tool's `init` wizard and `doctor` can set up for a
provider is a separate, narrower question (today: the three API-key providers
and the local CLIs, which need nothing; Vertex, Bedrock and Azure are
configured through `go/chat`'s own settings). A name no module registers is
refused, at generation and at regenerate.

**A durable override is a manifest field, never a deleted file.** Every `DO
NOT EDIT` file under `cmd/<name>/` (`chat.go`, `forge.go`, `keychain.go`, the
`chat/assets` bundle) and `pkg/cmd/root/signing.go` is re-emitted from the
manifest by every command that writes the manifest, so its presence is a fact
about the manifest and its absence is temporary. To ship no chat provider, set
`chat.providers: []`; to drop the keychain, `gtb disable keychain`; to turn
signing off, `gtb disable signing`. A project generated before the `chat:`
block existed has no block at all, and gets the full list written into its
manifest the first time it is regenerated (or `enable ai` is run) with `ai`
enabled.

**The generated `go.mod` names no `gtb` tool line.** The manifest's
`version.gtb` is the pin, `regenerate` refuses an older gtb, and the README
carries `go install gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb@<version>`.
Nor does it name golangci-lint or mockery: the justfile and CI run the installed
binaries, and the golangci-lint tool line pinned v1 and dragged an old viper
whose `cloud.google.com/go/compute` made `go mod tidy` ambiguous the moment
`chat-gemini` was linked. Only the framework's own `cmd/changelog` and
`cmd/docs` remain.

**Every author setting has one home.** Each flag above that says "recorded as" names the manifest field it writes, and `regenerate` reads that field back unchanged; `cli/pkg/generator/author_settings.go` is the table, and a test holds it and `SkeletonConfig` to each other (spec [0197](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0197-author-settings-as-one-surface) D1, D2). Nothing about a generated project depends on the machine `regenerate` runs on.

<a id="chat-defaults"></a>
**Chat defaults.** AI in a generated tool is one decision with several parts:
which providers to link, which is the default, which model, and the endpoint a
few providers need. The manifest records the answer under `chat:` and the
generator ships it as the tool's lowest config layer, beside the file that
links the modules:

```yaml
chat:
  providers: [claude, claude-local]
  default:
    provider: claude
    model: claude-opus-5     # optional
    base_url: ""             # openai-compatible, azure-openai
    api_version: ""          # azure-openai
    project: ""              # gemini-vertex
    location: ""             # gemini-vertex, bedrock
```

| File | Role |
|------|------|
| `cmd/<name>/chat/assets/config.yaml` | The author's defaults, registered by `chat.go` as an `ai` defaults bundle. The lowest layer of the running tool's config: an end user's own file overrides every key. |
| `cmd/<name>/chat/assets/init/config.yaml` | The same values as an `init` template, so a tool with the `init` feature seeds them into the end user's file as a visible, editable starting point. |

Both are `DO NOT EDIT` files rewritten from the manifest by `regenerate
project`, and both are absent when the manifest names no default. One linked
provider is its own default. With several, name one: an older manifest that
links several and names none regenerates without an author default and warns
naming `chat.default.provider`; the tool then falls back to its runtime
resolution. Credentials never appear in the manifest or these files; they are
the end user's, captured by `init ai` or supplied through the environment.

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
| `--signing-require-checksum` | `false` | Fail a self-update closed without a verified checksum. Safe from day one. Recorded as `signing.require_checksum`; renders `props.Tool.Signing.RequireChecksum`. |
| `--signing-require-signature` | `false` | Fail a self-update closed without a valid signature. Not before your first signed release has shipped: an unsigned release then fails every consumer's update. Recorded as `signing.require_signature`. |
| `--signing-email` | — | Release WKD email (`external_key_email`); enables the external trust-anchor leg. |
| `--signing-key-source` | `both` | Trust-anchor source: `embedded`, `external`, or `both`. |
| `--signing-require-external-crosscheck` | `false` | Fail signing closed when the external (WKD) resolver is unreachable. |
| `--signing-key-id` | — | Signing key id/ARN/alias (or PEM path for `local`) the release pipeline signs with; wires the GoReleaser signs block. Refused without `--signing` or `--signing-email`, since only the signing path renders it. |
| `--signing-backend` | *(aws-kms when `--signing-key-id` set)* | `gtb sign` backend for the release pipeline. |
| `--signing-kms-region` | *(eu-west-2)* | AWS region for the `aws-kms` backend. |
| `--signing-public-key` | *(internal/trustkeys/keys/signing-key-v1.asc)* | Path to the embedded public key the signature identifies. |

#### Environment

| Variable | Effect |
|---|---|
| `GTB_FRAMEWORK_REPLACE=<dir>` | **Development only.** Every `go.mod` the generator writes (on `generate project` and on `regenerate`) gains `replace gitlab.com/phpboyscout/go-tool-base => <dir>`, so a scaffold tidies and builds against that framework working tree rather than the latest release. The e2e suite sets it to the repo root; set it yourself to test a template change against a branch. It is read at render time and recorded nowhere: regenerate with it unset and the directive is gone. Never publish a project with it set. |

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

All `generate` subcommands also accept these persistent flags (for AI-assisted generation): `--provider` (any chat provider the `gtb` binary links; `gtb generate --help` lists them, and the list is `chat.ProviderModules()`), `--model` (AI model), and `--dry-run` (preview changes without writing files).

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
| `--no-ai-attribution` | `false` | Keep AI/model attribution out of the generated frontmatter `authors:`: human author(s) only. Default: the AI model is appended as an additive co-author. |
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
| `--date` | *(none, reproducible)* | Stamp this date (`YYYY-MM-DD` or RFC3339) into the `.TH` header. Omit for reproducible output with no date trailer. |

### `generate protect` / `generate unprotect`

`gtb generate protect <command-path>` marks a command so regeneration won't
overwrite it; `unprotect` reverses it. See
[Configure Generator Ignore](../../how-to/configure-generator-ignore.md).

> Run any subcommand with `--help` for the complete, authoritative flag set.
