---
title: Configuration
description: go-tool-base's integration of the standalone go/config container — embedded-asset defaults, the project-local config layer, env-prefix propagation, flag binding, initialisers, and sensitive-value masking.
date: 2026-07-18
tags: [components, config, configuration, viper]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration

The configuration container has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/config`](https://gitlab.com/phpboyscout/go/config)
module**. Its full documentation — the `Containable` interface, sources and
precedence, typed sections (`UnmarshalSection` / `ObserveSection`), schema
validation, hot-reload safety, flag binding, and testing with the published
`configmock` package — now lives at:

> **[config.go.phpboyscout.uk](https://config.go.phpboyscout.uk)**

API reference: **[pkg.go.dev/gitlab.com/phpboyscout/go/config](https://pkg.go.dev/gitlab.com/phpboyscout/go/config)**.
See the [migration note](../../../reference/migration/v0.x-config-extracted.md) for the
import-path change.

go-tool-base imports the module directly (no adapter package). This page documents only
what **GTB layers on top** — the conventions and wiring that are framework concerns and
deliberately not in the module.

## How GTB wires the container

The root command builds the container during its pre-run and publishes it as
`props.Config`, so every command and initialiser receives the same instance:

```go
p := &props.Props{
    Config: cfg,   // config.Containable
    Logger: l,
    FS:     fs,
}
```

`Props.Tool.EnvPrefix` is propagated into the container as the module's
`WithEnvPrefix`, so a tool's config keys resolve from `MYTOOL_*` rather than bare
names — see the module's
[env-prefix rationale](https://config.go.phpboyscout.uk/explanation/precedence-and-merge/#the-env-prefix-is-a-security-control).

## Embedded defaults: the `assets/init/config.yaml` convention

GTB discovers shipped defaults at a fixed path inside each package's `embed.FS`:

- **Path:** `assets/init/config.yaml`
- **Directive:** `//go:embed assets/*`

The root command aggregates assets from every subcommand and merges them, so each
feature package ships its own defaults without a central file:

```go
//go:embed assets/*
var assets embed.FS

allAssets := []embed.FS{assets}
// ... append each subcommand's assets ...
rootCmd := root.NewCmdRoot(props, allAssets)
```

Defaults live **only** here — never duplicated into `default:` struct tags, which the
module treats as hint text and never applies.

## The project-local config layer

At startup GTB also looks for a **project-local** file named `.<tool>.yaml` (e.g.
`.myapp.yaml`), discovered by walking up from the working directory to the filesystem
root — a repo-root convention like `.editorconfig`. When found it is merged **last among
the file sources**, so it deep-merges over and overrides the per-user
`~/.<tool>/config.yaml`:

```
~/.myapp/config.yaml          # global, per-user
/path/to/repo/.myapp.yaml     # project — overrides the global, committed with the repo
```

This keeps a project's non-secret settings in the repo that owns them. A tool opts out
simply by not having the file; it never errors when absent. Environment variables and
flags still override it — it sits in the *file* tier of the module's precedence chain.
The filename derives from `Props.Tool.Name`.

## Binding CLI flags

GTB registers and binds flags on your behalf, so you rarely call the module's
`BindPFlag` directly:

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
defaulted flag from masking file or env values — see the module's
[default-clobber warning](https://config.go.phpboyscout.uk/how-to/bind-cli-flags/).

The built-in `--debug` and `--ci` flags fold through the same path, so
`Config.GetBool("ci")` reflects `--ci`; `--debug` additionally retains its immediate
effect on the log level.

## Initialiser integration

`config.Containable` is the standard interface for
[tool initialisers](../setup/initialisers.md): an initialiser checks existing state with
`IsConfigured(cfg)` and writes new values with `cfg.Set(...)`, giving a consistent API
across the whole setup lifecycle.

## Sensitive-value masking

Masking lives in GTB's `config` **command** (`pkg/cmd/config/sensitive.go`), not in the
container — the module never inspects values for sensitivity. `config get` / `config
list` render secrets as `****<tail>` using two independent strategies:

1. **Key-name matching** — the leaf segment of the dotted key against `token`,
   `password`, `secret`, `key`, `apikey`, `auth`.
2. **Value-content matching** — the value against known token patterns (e.g. `ghp_…`,
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
| Find where config actually lives | `config path` (backed by the module's `ConfigFiles()`) |
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
