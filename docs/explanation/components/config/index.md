---
title: Configuration
description: go-tool-base's integration of the standalone go/config store: embedded-asset defaults, the project-local config layer, env-prefix propagation, flag binding, initialisers, and sensitive-value masking.
date: 2026-07-18
tags: [components, config, configuration, store]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration

The configuration layer has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/config`](https://gitlab.com/phpboyscout/go/config)
module**. Its full documentation, the `Store`/`View` model, layered sources and
precedence, typed sections (`UnmarshalSection` / `ObserveSection`), schema
validation, explicit hot-reload (`Watch`), transactional writes (`Apply`), flag
binding, and testing with the published `mocks` package. Now lives at:

> **[config.go.phpboyscout.uk](https://config.go.phpboyscout.uk)**

API reference: **[pkg.go.dev/gitlab.com/phpboyscout/go/config](https://pkg.go.dev/gitlab.com/phpboyscout/go/config)**.
See the [migration note](../../../reference/migration/v0.x-config-extracted.md) for the
import-path change.

go-tool-base imports the module directly (no adapter package). This page documents only
what **GTB layers on top**. The conventions and wiring that are framework concerns and
deliberately not in the module.

## How GTB wires the container

The root command builds the store during its pre-run and publishes it as
`props.Config`, so every command and initialiser receives the same instance:

```go
p := &props.Props{
    Config: cfg,   // *config.Store
    Logger: l,
    FS:     fs,
}

// Reads pin an immutable snapshot:
timeout := p.Config.View().GetString("app.timeout")
```

The store is the live object that owns reloads and writes; **reads go through
`props.Config.View()`**, which pins a consistent snapshot (a `*config.View`,
satisfying the module's `config.Reader`). Hot reload is explicit, the root
pre-run calls `Store.Watch`, so file changes propagate without any per-package
wiring, and writes go through the store's transactional `Apply`, which edits
the target file in place, preserving comments and writing only the named keys.

The layer declaration, highest precedence first: changed CLI flags; environment
variables under the tool's `EnvPrefix`; the project-local `.<tool>.yaml`; the
config files (`--config` paths if given, otherwise `~/.<tool>/config.yaml` then
`/etc/<tool>/config.yaml`); the tool's `ConfigPaths` embedded assets; and the
merged `assets/config.yaml` embedded defaults, which always apply. The per-user
config outranks the system `/etc` file, the Unix convention.

Only config files that **exist** are declared as layers, a non-existent file
contributes nothing and must not become a phantom write target. The one
exception is the write destination: the highest-precedence path is always
available so `config set`/`unset`/`edit` have somewhere to land and can create
the file on first write. Writes therefore go to the per-user config (or the
project-local file when one is present), never to a system path the user cannot
write.

Two safeguards ride on every config-command write. The written file is
re-tightened to **`0600`**: the store deliberately *preserves* an existing
file's mode on write (it treats the mode as the owner's choice), but a GTB
config file routinely holds credentials, so the framework actively re-asserts
owner-only permissions. And when a write would place a **recognised credential
in a project-local `.<tool>.yaml`**. A file that may be committed to version
control: `config set` warns, and, in an interactive terminal, asks to confirm.
It never blocks the write (a project-local secret can be deliberate), but it
nudges toward env-var or keychain storage. See
[`config set`](../../../reference/cli/config.md) and
[Configure credentials](../../../how-to/configure-credentials.md).

`Props.Tool.EnvPrefix` is propagated into the store as the module's
`WithEnv` layer, so a tool's config keys resolve from `MYTOOL_*` rather than bare
names. See the module's
[env-prefix rationale](https://config.go.phpboyscout.uk/explanation/precedence-and-merge/#the-env-prefix-is-a-security-control).

### Project-local trust: security keys are ignored until you trust the file

The project-local `.<tool>.yaml` is convenient, but it arrives with a repository
you may not have written. A `git clone` can ship one. So its **security-sensitive
keys are ignored** unless you explicitly trust the file, while its ordinary
workflow keys (logging, output, feature toggles) always apply. The protected set
is the keys that govern self-update verification and posture
(`update.require_signature`, `update.require_checksum`,
`update.require_external_crosscheck`, `update.policy`, `update.key_source`,
`update.external_key_email`), telemetry consent (`telemetry.enabled`), and every
credential subtree (`*.auth.*`, `*.api.*`, `bitbucket.app_password`). When any of
these is stripped from an untrusted file, the framework logs a WARN naming the
file and the ignored keys. Never a silent drop.

Trust is **direnv-style**. `<tool> config trust` records the file's absolute path
and the SHA-256 of its exact current content in a per-user store
(`~/.<tool>/trusted-projects.yaml`, owner-only, never inside a repository).
Editing a trusted file (or a fresh clone swapping it out) changes the hash and
revokes trust until you run the command again. Use `config trust --list` to see
what is trusted and `config trust --forget` to revoke. Until a file is trusted it
is also **read-only**: `config set` in an untrusted repository routes the write to
your own config, not the repository file.

An explicit `--config` suppresses the project-local layer entirely (naming a file
means "use this one"), so this trust gate only applies to the implicitly
discovered `.<tool>.yaml`. CI runs untrusted by default, a pipeline that
legitimately depends on project-local security keys should trust the file in a
provisioning step or supply those values through the user config or environment.
See [`config trust`](../../../reference/cli/config.md) and the
[security-decisions record](../../../development/security-decisions.md#m-2-project-local-toolyaml-could-downgrade-security-posture).

## Embedded defaults: the `assets/config.yaml` convention

GTB discovers shipped assets at fixed paths inside each registered bundle's
`embed.FS` (directive: `//go:embed assets/*`):

- **`assets/config.yaml`**: the embedded-defaults layer. Merged across every
  registered bundle and always applied as the lowest-precedence layer, so a
  user file that omits a key resolves to the shipped default.
- **`assets/init/config.yaml`**: the init template: the human-facing document
  (comments included) that `init` writes to the user's config file.

The framework's own baseline bundle registers first (inside `props.NewAssets`),
the tool's bundle next, and feature bundles (registered via
`setup.RegisterAssets`) are applied for enabled features at root construction:
later bundles override earlier ones in the merged structured reads:

```go
//go:embed assets/*
var assets embed.FS

p := &props.Props{
    Assets: props.NewAssets(props.AssetMap{"root": &assets}),
    // ...
}
```

Defaults live **only** here. Never duplicated into `default:` struct tags, which the
module treats as hint text and never applies.

## The project-local config layer

At startup GTB also looks for a **project-local** file named `.<tool>.yaml` (e.g.
`.myapp.yaml`), discovered by walking up from the working directory to the filesystem
root. A repo-root convention like `.editorconfig`. When found it is merged **last among
the file sources**, so it deep-merges over and overrides the per-user
`~/.<tool>/config.yaml`:

```
~/.myapp/config.yaml          # global, per-user
/path/to/repo/.myapp.yaml     # project — overrides the global, committed with the repo
```

This keeps a project's non-secret settings in the repo that owns them. A tool opts out
simply by not having the file; it never errors when absent. Environment variables and
flags still override it: it sits in the *file* tier of the module's precedence chain.
An explicit `--config` **suppresses the layer entirely**: naming a config file means
"use this one", and a project-local file the caller did not name must not override
files they did. The filename derives from `Props.Tool.Name`.

## Binding CLI flags

GTB registers and binds flags on your behalf, so you rarely touch the module's
flag layer (`config.WithFlags` / `config.BindFlag`) directly:

```go
portFlags := pflag.NewFlagSet("server", pflag.ContinueOnError)
portFlags.Int("server-port", 8080, "server port")

rootCmd := root.NewCmdRootWithOptions(props,
    root.WithBoundFlags(map[string]*pflag.Flag{"server.port": portFlags.Lookup("server-port")}),
    // or, by convention (--server-port -> server.port):
    root.WithConventionBoundFlags(portFlags),
)
```

A subcommand's own local flags are bound by the same hyphen-to-dot convention when that
command runs. **Only flags the user actually changed are bound**, which is what keeps a
defaulted flag from masking file or env values. See the module's
[default-clobber warning](https://config.go.phpboyscout.uk/how-to/bind-cli-flags/).

The built-in `--debug` and `--ci` flags fold through the same path, so
`Config.View().GetBool("ci")` reflects `--ci`; `--debug` additionally retains its
immediate effect on the log level.

## Initialiser integration

[Tool initialisers](../setup/initialisers.md) work against two narrow surfaces:
`IsConfigured(cfg config.Reader)` checks existing state against a pinned view, and
`Configure(p, cfg setup.Editor)` writes new values through `cfg.Set(...)`, the
`setup.Editor` routes writes through the store's transactional `Apply`, so
template comments in the user's file survive the wizards.

## Sensitive-value masking

Masking lives in GTB's `config` **command** (`pkg/cmd/config/sensitive.go`), not in the
container: the module never inspects values for sensitivity. `config get` / `config
list` render secrets as `****<tail>` using two independent strategies:

1. **Key-name matching**: the leaf segment of the dotted key against `token`,
   `password`, `secret`, `key`, `apikey`, `auth`.
2. **Value-content matching**: the value against known token patterns (e.g. `ghp_…`,
   `github_pat_…`), which catches keys like `github.auth.value` whose *name* is not
   sensitive.

Tool authors extend it via functional options:

```go
cmdconfig.NewCmdConfig(props,
    cmdconfig.WithKeyPattern("credential"),
    cmdconfig.WithValuePattern(regexp.MustCompile(`^sk-[A-Za-z0-9]{32}$`)),
)
```

## Relationship with `init` and `config`

| Workflow | Command |
| :--- | :--- |
| First-run bootstrap | `init` |
| Re-configure a subsystem interactively | `init <subsystem>` (e.g. `init ai`) |
| Read / write / remove a single value | `config get` / `config set` / `config unset` |
| Find where config actually lives | `config path` (backed by the store's declared file layers) |
| Hand-edit the file safely (re-validated) | `config edit` |
| Inspect all resolved config | `config list` |
| Validate config against schema | `config validate` |

Both `InitCmd` and `ConfigCmd` should be disabled in containerised services where local
YAML config is not applicable.

## Related

- **Module docs:** [config.go.phpboyscout.uk](https://config.go.phpboyscout.uk)
- **How-to:** [Test code that uses configuration](../../../how-to/test-configuration.md),
  [Bind flags to config](../../../how-to/bind-flags-to-config.md),
  [React to config changes](../../../how-to/config-hot-reload.md),
  [Validate component config](../../../how-to/validate-component-config.md)
- **Assets:** [Embed custom assets](../../../how-to/embed-custom-assets.md)
