---
title: Init Command
description: Initialize tool configuration, GitHub authentication, and SSH keys.
date: 2026-02-16
tags: [components, commands, init, setup]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Init Command

The `init` command initializes the tool's configuration and performs initial setup.

## Usage

```bash
mytool init [flags]
mytool init [subcommand]
```

## Description

Initializes the default configuration for the tool, sets up authentication with GitHub (if needed), and configures SSH keys. This command creates configuration files in the appropriate directories and prepares the tool for first use.

The credential wizards (GitHub, Bitbucket, AI) are interactive. When `init` runs **without an interactive terminal** (piped/redirected stdin, CI, or a test harness) those wizards are skipped automatically and only the base configuration is written, so `init` never blocks waiting on a prompt that can't be answered. Configure a provider later by running its subcommand (e.g. `mytool init bitbucket`) from a terminal, or by setting the relevant config keys directly.

## Flags

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-d, --dir` | Directory to initialize the config in | `~/.mytool/` |
| `-c, --clean` | Reset existing configuration and replace with defaults | `false` |
| `-l, --skip-login` | Skip the GitHub login process | `false` (or `true` in CI) |
| `-k, --skip-key` | Skip SSH key configuration, for every forge that offers it | `false` (or `true` in CI) |
| `--skip-gitlab` | Skip configuring GitLab credentials | `false` (or `true` in CI) |
| `--skip-gitea` | Skip configuring Gitea credentials | `false` (or `true` in CI) |
| `--skip-codeberg` | Skip configuring Codeberg credentials | `false` (or `true` in CI) |
| `--skip-bitbucket` | Skip configuring Bitbucket credentials | `false` (or `true` in CI) |

Only an enabled feature contributes its flags: a tool with no forge feature has no `--skip-login` or `--skip-key`, and a GitLab-only tool has `--skip-gitlab` but not `--skip-login`. The same gate decides which `init <provider>` subcommands exist.

!!! info "CI Mode Detection"
    When the `CI` environment variable is set to `true`, the `--skip-login`, `--skip-key`, `--skip-gitlab`, `--skip-gitea`, `--skip-codeberg` and `--skip-bitbucket` flags default to `true` to avoid interactive prompts in automated environments. Independently of those flags, the credential wizards are also skipped whenever stdin is not an interactive terminal, so `init` is safe to run in pipelines and scripts without hanging.

## Example

```bash
# Initialize with default settings
mytool init

# Initialize and reset existing config
mytool init --clean

# Initialize to a custom directory
mytool init --dir /etc/mytool/
```

## Subcommands

### Init GitHub

Force reconfiguration of GitHub authentication and SSH keys, regardless of current configuration state.

**Usage:**
```bash
mytool init github [--dir <path>]
```

**Description:**
Runs the full GitHub authentication flow (token generation and SSH key configuration) even if already configured. Useful when tokens expire or you need to switch accounts.

### Init GitLab

**Usage:**
```bash
mytool init gitlab [--dir <path>]
```

**Description:**
Configures the GitLab token via the three-mode selector (env-var reference, OS keychain, or literal), and generates or selects an SSH key for Git operations.

Login uses GitLab's OAuth device flow against an application the framework owns on `gitlab.com`, so no setup is required to authenticate there. A **self-hosted** instance needs its own OAuth application: register one on that instance and supply its client ID via the `gitlab.auth.client_id` config key or the `GITLAB_CLIENT_ID` environment variable. Without one, login degrades to manual token entry rather than failing.

!!! note "Scopes"
    The device flow requests `api` and `write_repository`. `api` is not a preference: SSH-key upload writes a user-level resource (`POST /user/keys`), and GitLab has `read_user` but no `write_user`, while `write_repository` covers Git-over-HTTP rather than API access. No narrower scope can register a key. Request less via the provider's `Scopes` setting and accept the manual SSH path if that trade is wrong for you.

### Init Gitea

**Usage:**
```bash
mytool init gitea [--dir <path>]
```

**Description:**
Configures the Gitea token via the three-mode selector, and generates or selects an SSH key.

Gitea has **no interactive browser login** (the upstream adapter is personal-access-token only by design) so this wizard goes straight to manual token entry. It also carries no default host, since every Gitea instance is self-hosted; the token-creation guidance names the forge rather than inventing a URL.

### Init Codeberg

**Usage:**
```bash
mytool init codeberg [--dir <path>]
```

**Description:**
Configures the Codeberg token via the three-mode selector, and generates or selects an SSH key.

Codeberg runs Forgejo and shares Gitea's adapter, so it behaves the same way: no interactive browser login, straight to manual token entry. It differs in having a default host (`codeberg.org`) and in writing its own `codeberg.*` config keys, so a token configured here is not shared with a self-hosted Gitea instance.

### Init Bitbucket

**Usage:**
```bash
mytool init bitbucket [--dir <path>]
```

**Description:**
Configures Bitbucket's dual credentials (`username` + `app_password`) via the three-mode selector. Env-var mode records two variable names, keychain mode stores a single JSON blob, and literal mode writes both fields to config.

An SSH key is then configured, as for the single-token forges: Bitbucket's adapter accepts key uploads, and offering one is a capability question rather than a consequence of the credential shape. The key stage runs **after** the credentials are captured, because the upload is authorised by them. Pass `--skip-key` to leave keys alone.

### Init AI

When the AI feature is enabled, the `init` command gains an `ai` subcommand for configuring AI provider integration.

**Usage:**
```bash
mytool init ai
```

**Description:**
Configures the AI provider and its credential. The provider select offers the providers this binary links (of the ten the framework knows: `claude`, `claude-local`, `openai`, `openai-compatible`, `codex-local`, `gemini`, `gemini-vertex`, `agy-local`, `bedrock`, `azure-openai`), labelled and glossed from one table; a provider the binary does not register is refused with the module to import. A binary that registers no provider at all is offered the whole table, and `doctor`'s Chat providers check is what reports that gap. A provider that carries no GTB credential (the local CLIs, `bedrock`) is written as `ai.provider` and the storage and key forms are skipped; an API provider goes on to the three-mode selector for its key (`anthropic.api.*`, `openai.api.*`, `gemini.api.*` or `azure.api.*`).

If `AI_PROVIDER` is exported, a note says it is read only when `ai.provider` is unset, so the provider chosen here takes effect once written. An end user's choice here overrides the author's default the tool ships with.

### Init config

```bash
mytool init config <name> [--dir <dir>]
```

Configures one of the config sources the tool declares: where it connects and
how it authenticates. The source's kind asks what it needs and writes it under
`config.sources.<name>` in your own config file, never a project file. There is
one subcommand per declared source, and a tool that declares none has no
`init config`. A source the tool's own code builds (etcd, sftp, billy, iofs,
afero) has nothing to configure here and says so.

A required source nobody has configured stops every command but `init`,
`doctor` and `config convert`, naming the `init config` command that fixes it.

| Flag | Default | Description |
|---|---|---|
| `--dir` | `~/.<tool>` | Directory containing the config file |

## Implementation

The init command is implemented in `cmd/initialise/init.go` and uses the `pkg/setup` package to perform the actual initialization work.
