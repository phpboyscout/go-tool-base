---
title: Configure a Tool's Config Stack
description: How to choose the config formats a generated tool reads, declare config sources such as Vault, Consul or a cloud store, set where each one connects, order them, and check the result with doctor.
date: 2026-10-03
tags: [how-to, config, sources, formats, doctor]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure a Tool's Config Stack

A GTB tool reads its configuration from a stack of layers: embedded defaults,
the user's config files, a project file, environment variables and flags. This
guide adds to that stack. You choose which file formats the tool understands,
declare **config sources** (a team's Consul, a Vault secret, a file in a
bucket) as extra layers, and decide where each sits.

There are two people involved, and the split matters:

- **You, the author**, decide what the tool *can* read: which formats and which
  kinds of source it links, and in what order they apply. That is recorded in
  `.gtb/manifest.yaml` and compiled into the tool.
- **Each user** decides where a source *connects*: which Consul, which Vault
  path, which bucket. That lives in their own config file, never in the
  manifest, so one build serves every deployment.

## 1. Choose the formats

YAML is always available. Link another format when your users already keep
their config in it, or a source serves it; each one adds its parser to the
binary.

```bash
gtb generate project --config-formats toml,dotenv --config-format toml ...
```

`--config-formats` is what the tool can read. `--config-format` is the tool's
own file, the one `init` creates and `config set` edits, and it must be a
format that can be written back: `yaml`, `toml`, `json` or `hcl`. On an
existing project, `gtb set config.formats toml` and `gtb set config.format toml`
do the same.

## 2. Declare the sources

Each source is a **slot**: a name and a kind.

```bash
gtb generate project --config-source team=consul --config-source secrets=vault \
  --config-source-optional team ...
```

Or use the wizard's Configuration page, which explains each kind. The kinds:

| Group | Kinds | Where it connects comes from |
|---|---|---|
| On the user's machine | `file`, `keychain` | each user's settings |
| A service | `vault`, `consul`, `aws-s3`, `aws-ssm`, `aws-secrets`, `azure-blob`, `azure-keyvault`, `azure-appconfig`, `gcp-gcs`, `gcp-secret`, `gcp-parameter` | each user's settings, with their usual login |
| Your own code | `etcd`, `sftp`, `billy`, `iofs`, `afero` | your code: see [Build a Config Source in Your Own Code](override-a-config-source.md) |

A slot is **required** unless you mark it optional: a required slot that is not
configured or cannot be reached stops the tool, naming it. A slot is
**read-only** unless you mark it writable with `--config-source-writable`,
except a `keychain` slot, which is writable by default and needs the OS
Keychain feature. Declaring any slot gives the tool an environment prefix if it
has none, so its settings can come from the environment.

## 3. Say where each source connects

A slot's settings live under `config.sources.<name>`. Each kind's keys, and
what they mean, are in the [config sources table](../explanation/components/config/index.md#config-sources).

**With the Initialization feature**, each user runs:

```bash
mytool init config secrets
```

which asks for the kind's settings and saves them to their config file
(typically `~/.mytool/config.yaml`):

```yaml
config:
  sources:
    secrets:
      address: "https://vault.example.internal"
      mount: "secret"
      path: "mytool/config"
```

**To give every user a default**, put the same block in
`pkg/cmd/root/assets/config.yaml`. It applies until a user's own file or
`init config` says otherwise. Without the Initialization feature there is no
`init config`, so this file, the user's config file and the environment are
the only places a slot's settings come from.

A repository's project file can never set `config.sources`: choosing where
configuration comes from is the user's decision, not a cloned repository's.

## 4. Credentials

The cloud kinds use the provider's own credential chain: an AWS profile or the
SDK's default chain, Azure's environment, workload and managed identity or CLI
login, or GCP Application Default Credentials. Vault and Consul start from
their own variables (`VAULT_TOKEN`, `CONSUL_HTTP_TOKEN`), and a slot may
instead name its token, the way forges do:

| Setting | Holds |
|---|---|
| `config.sources.<name>.auth.env` | the **name** of a variable holding the token |
| `config.sources.<name>.auth.keychain` | a `service/account` keychain entry |
| `config.sources.<name>.auth.value` | the token itself; refused under CI |

See [Configure credentials](configure-credentials.md#how-a-config-sources-token-resolves).

## 5. Order the stack

The layer list is the precedence, lowest first. With sources declared and no
list stated, the generator places them above the embedded defaults and below
the user's config files, so a team source supplies values each user can
override:

```yaml
# .gtb/manifest.yaml
properties:
  config:
    layers: [defaults, team, secrets, files, project, env, flags]
```

To make a source enforce its values over the user's file, place it higher:

```bash
gtb generate project --config-layers defaults,files,team,project,env,flags ...
```

`defaults` must be lowest, `flags` highest, and `project` below `env`; a list
that breaks one of those is refused, saying which.

## 6. In CI

Set a slot's settings from the environment under the tool's prefix, for
example `MYTOOL_CONFIG_SOURCES_SECRETS_ADDRESS`, and point its token at the
job's own variable with `auth.env` in a committed default. Each underscore
becomes a dot unless a lower layer already defines the key, so a key whose
own name holds an underscore (`service_url`, `vault_url`) needs a default in
`pkg/cmd/root/assets/config.yaml` before a variable can set it. A literal
`auth.value` is refused under CI. Mark a source optional if a job should run
without it.

## 7. Check it with doctor

```
$ mytool doctor
  [!!] Config stack: 7 layers, 2 config sources, 1 left out
       1. defaults: embedded defaults
       2. team (consul): not configured, optional; run `mytool init config team`
       3. secrets (vault): built, required, read-only, sensitive; credential from auth.env
       4. files: /home/me/.mytool/config.yaml
       5. project: a discovered .mytool.* in the working tree, trust-filtered
       6. env: variables under MYTOOL_
       7. flags: changed flags
```

Each source says whether it was built, whether it is required and writable,
and which link of its credential chain answered. An optional source left out
is a warning naming what to run. A required source that fails never reaches
`doctor`: the tool refuses to start and names it, which is the diagnosis.
