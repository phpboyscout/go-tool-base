---
title: Configuration Keys
description: Reference for the configuration keys the GTB framework reads, with types, defaults, env-var mapping, and what happens when a value is wrong.
date: 2026-08-02
tags: [reference, config, configuration, keys, environment, precedence]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configuration Keys

This page lists the configuration keys the **framework itself** reads, what each
one defaults to, and what happens when it is set to something the framework
cannot use. Tools built on GTB add their own keys on top.

For the internals of the store behind all of this — snapshots, transactional
writes, typed sections — see
[config.go.phpboyscout.uk](https://config.go.phpboyscout.uk).

## Which layer wins: the precedence order

GTB declares six layers. Highest precedence first — each one overrides
everything below it:

| | Layer | Notes |
|---|---|---|
| 1 | **Changed CLI flags** | Only flags the user actually typed. A flag sitting at its default contributes nothing. |
| 2 | **Environment variables** under the tool's `EnvPrefix` | Unprefixed variables cannot reach configuration at all. |
| 3 | **Project-local `.<tool>.yaml`** | Found by walking up from the working directory. Security-sensitive keys are ignored until the file is trusted. |
| 4 | **Config files** | `--config` paths if given, otherwise `/etc/<tool>/config.yaml` then `~/.<tool>/config.yaml`. |
| 5 | **The tool's `ConfigPaths` embedded assets** | Optional extra embedded documents a tool author registers. |
| 6 | **Embedded defaults** — `assets/config.yaml`, merged across every registered bundle | Always applies, so a key omitted everywhere else still resolves. |

Within the file layer, later files override earlier ones. The default order puts
the system-wide `/etc` file **below** the per-user file, so a user's own config
beats the machine's — the Unix convention. It also makes the per-user file the
highest-precedence writable layer, which is why `config set`, `config unset` and
`config edit` land there rather than in a root-owned `/etc` path.

The declaration order lives in `buildConfigStore` in
[`pkg/cmd/root/root.go`](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/pkg/cmd/root/root.go).

### Where the default config files live

`~/.<tool>/config.yaml` and `/etc/<tool>/config.yaml` — note the leading dot on
the per-user directory. It is **not** `~/.config/<tool>/`. The per-user directory
is derived from the tool's name (`props.Tool.Name`, lower-cased) and is created
with owner-only permissions because it routinely holds credentials.

Confirm it for any given tool by running `<tool> --help` and reading the
`--config` default, or `<tool> config path`.

### Only files that exist become layers

A config file that is not on disk contributes nothing and is not declared. That
matters because a declared layer is a candidate write target, and a missing
`/etc` path capturing a write is how a `config set` ends up failing with a
permissions error.

The one exception is the write target itself — the highest-precedence declared
path — which is always available so a first write has somewhere to land and can
create the file.

If **no** config file exists at all, most commands stop with `no config file
found` and the hint `Run '<tool> init' to create a configuration.` A tool author
can relax that per-command (`setup.SkipConfigCheck`) or heal it automatically
(`Tool.Bootstrap.AutoInitialise`).

### What `--config` does to the other layers

`--config` **replaces** the default file list rather than adding to it. Naming a
file means "use this one":

```bash
mytool run                                           # /etc/… then ~/.mytool/config.yaml
mytool run --config ./ci.yaml                        # only ci.yaml
mytool run --config ./base.yaml --config ./ci.yaml   # both, ci.yaml wins
```

Repeating the flag builds an ordered list in which the last occurrence wins.

`--config` **also suppresses the project-local layer entirely**. A `.<tool>.yaml`
in the working directory is a convention like `.editorconfig` and applies
whenever the tool runs without an explicit config; once you name a file, nothing
you did not name should override it. So in a repository containing
`.mytool.yaml`, `mytool run --config ./ci.yaml` takes both layers from `ci.yaml`
alone.

## How a config key maps to an environment variable

Every key maps to `<PREFIX>_<KEY>`, upper-cased with `.` replaced by `_`. With
prefix `MYTOOL`, `log.level` reads `MYTOOL_LOG_LEVEL`.

The prefix is set by the tool author (`props.Tool.EnvPrefix`) and is a security
control, not tidiness: without it, an unrelated process on a shared runner
setting `LOG_LEVEL` would reconfigure every tool on the box. **A tool with no
`EnvPrefix` has no environment layer at all** — the whole layer is empty rather
than swallowing the entire environment.

### Why `MYTOOL_UPDATE_CHECK_INTERVAL` may do nothing

The mapping back from an environment variable to a dotted key is genuinely
ambiguous: `MYTOOL_UPDATE_CHECK_INTERVAL` could mean `update.check_interval` or
`update.check.interval`, and the variable name does not say which. The store
resolves it against the keys the **lower layers already define**. A key defined
nowhere else falls back to treating every underscore as a separator.

So a key whose name contains an underscore is only reachable from the
environment once something below has defined it:

```bash
# ~/.mytool/config.yaml contains only  log.level
MYTOOL_UPDATE_CHECK_INTERVAL=1h mytool config list
# KEY                     VALUE
# log.level               info
# update.check.interval   1h        <- not update.check_interval; ignored

# ~/.mytool/config.yaml also declares  update.check_interval: ""
MYTOOL_UPDATE_CHECK_INTERVAL=1h mytool config get update.check_interval
# 1h
```

The fix is to declare the key — with an empty or default value — in the tool's
embedded `assets/config.yaml` or its `assets/init/config.yaml` template. Projects
scaffolded by `gtb generate project` already do this for `update.policy` and
`update.check_interval`.

If two keys a tool defines would both spell the same variable name, the store
refuses to guess: config loading fails with `environment variable is ambiguous:
it could mean <a> or <b>`. Rename one of the keys.

### Keys ending in `.env` hold a variable *name*, not a value

`github.auth.env: GITHUB_TOKEN` means "read the token from `$GITHUB_TOKEN`". It
does not set the token. See [Credentials](#credentials).

## Why security keys from a project `.<tool>.yaml` are ignored

The project-local layer is convenient, but the file arrives with a repository you
may not have written — `git clone` can ship one. So its **security-sensitive keys
are stripped** unless you explicitly trust the file, while ordinary workflow keys
(logging, output, feature toggles) always apply. Without this, cloning a
repository would be enough to turn off signature verification on your
self-updates or flip your telemetry consent.

The protected set is the exact list in
[`pkg/cmd/root/project_trust.go`](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/pkg/cmd/root/project_trust.go):

- `update.require_signature`, `update.require_checksum`,
  `update.require_external_crosscheck`, `update.policy`, `update.key_source`,
  `update.external_key_email`
- `telemetry.enabled`, `telemetry.consent`
- every credential subtree: any path with an `auth` segment (`github.auth.env`,
  `gitlab.auth.value`, …), plus `anthropic.api`, `openai.api`, `gemini.api` and
  `bitbucket.app_password`

Stripping is never silent. Each decode logs one WARN naming the file and the keys:

```
WARN ignoring security-sensitive keys from an untrusted project-local config file
  file=/repo/.mytool.yaml keys="telemetry.enabled, update.policy"
  hint="run 'mytool config trust' to trust this file if you authored it"
```

Trust is direnv-style. `<tool> config trust` records the file's absolute path and
the SHA-256 of its exact content in `~/.<tool>/trusted-projects.yaml` (owner-only,
never inside a repository). Editing a trusted file — or a fresh clone swapping it
out — changes the hash and revokes trust until you run the command again.

An untrusted project file is also **read-only**: `config set` in an untrusted
repository routes the write to your own config rather than the repository file.

CI runs untrusted by default, which is the safe direction. A pipeline that
legitimately depends on project-local security keys should either trust the file
in a provisioning step or supply those values through the user config or the
environment.

See [`config trust`](../cli/config.md#config-trust) and the
[Configuration component](../../explanation/components/config/index.md#project-local-trust-security-keys-are-ignored-until-you-trust-the-file).

## Core keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `log.level` | string | `info` | Log verbosity. |
| `log.format` | string | `text` | Log rendering. |
| `ci` | bool | `false` | Treat the invocation as a CI run. Also settable with `--ci`. |

### `output` and `debug` are flags, not usable config keys

`--output` and `--debug` are bound into the store like every other flag, so they
appear in `config list` when you pass them — but nothing reads them back out of
configuration. Commands read `--output` from the flag set directly, and the log
level is applied from `flags.Debug`.

So `output: json` in a config file, or `MYTOOL_OUTPUT=json` in the environment,
has **no effect**; only `--output json` on the command line does. The same is
true of `debug`. Use `log.level: debug` to raise the log level from
configuration.

**`log.level`** accepts `debug`, `info`, `warn`, `error` and `fatal` at runtime
(`logger.ParseLevel`). An unrecognised value is **silently ignored** — the logger
keeps the level it already had — so a typo produces no warning and no visible
change. Run `<tool> config validate` to catch it.

Note the mismatch: `config validate`'s base schema allows only `debug`, `info`,
`warn`, `error`, so a working `log.level: fatal` is reported as invalid.

**`log.format`** is applied by the root pre-run, which honours `json` and
`logfmt`. Any other value — including the literal `text` — leaves the formatter
alone, which produces text output because that is the constructed default.

**`--debug` outranks `log.level`.** It is applied first and a config hot-reload
cannot downgrade it.

## Self-update keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `update.policy` | string | *(tool baseline, then `disabled`)* | `disabled` (log only), `prompt` (ask; declining continues), `enabled` (block until updated). |
| `update.check_interval` | duration | *(tool baseline, then `24h`)* | How often to check. `0` or `0s` checks every run. |
| `update.require_checksum` | bool | `false` | Require a verified checksum before applying an update. |
| `update.require_signature` | bool | `false` | Require a valid release signature. |
| `update.require_external_crosscheck` | bool | `false` | Abort if the external (WKD) key resolver cannot be reached. |
| `update.key_source` | string | `both` | Where the release public key comes from: `embedded`, `external`, or `both`. |
| `update.external_key_email` | string | `""` | Email used to derive the WKD URL for the release key. |
| `update.checksum_asset_name` | string | `""` | Override the checksum asset filename in the release. |
| `update.signature_asset_name` | string | `""` | Override the detached-signature asset filename. |

`update.policy` resolution is case-insensitive and forgiving: an empty **or
unrecognised** value falls back to the tool author's baseline
(`props.Tool.UpdatePolicy`), and an empty or unrecognised baseline falls back to
`disabled`. Nothing errors — `update.policy: enbaled` silently means `disabled`.

`update.check_interval` takes any Go duration. An unparseable value, or a negative
one, falls back to the tool baseline and then to `24h`. A tool baseline of zero
means "use the framework default", **not** "check every run"; only the config key
set to `0` means every run.

`update.key_source` is the one update key that fails loudly, because a
misconfigured trust anchor must not silently downgrade to no verification:

- `embedded` with no embedded keys → `key_source=embedded requires embedded keys (WithEmbeddedKeys)`
- `external` with no `update.external_key_email` → `key_source=external requires update.external_key_email`
- `both` with neither → `key_source=both requires embedded keys, an external key email, or both`
- anything else → `unknown key_source "x" (want embedded, external, or both)`

Update checks are skipped entirely when the update feature is disabled, on a
development build, or in CI (`--ci`, the `ci` key, or `CI=true`).

See [Configure self-updating](../../how-to/configure-self-updating.md) and
[Secure releases](../../how-to/secure-releases.md).

## AI and chat keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `ai.provider` | string | `claude` | Active provider. |
| `ai.request_timeout` | duration | `5m` | Bound on a single AI request. Deliberately generous — a large single-shot generation runs well past the shared 30s HTTP default — but bounded, so a model stuck in a loop fails rather than hanging. |
| `ai.fallback.enabled` | bool | `false` | Try other providers when the primary fails. |
| `ai.fallback.providers` | list of string | `[]` | Ordered provider list for failover. |

**`ai.provider` accepted values are `claude`, `openai` and `gemini`.**
`anthropic` is *not* one of them — that is the name of the *credential section*
(`anthropic.api.*`), not the provider identifier. Setting `ai.provider:
anthropic` leaves `init`'s "is AI configured?" check reporting unconfigured and
fails at client construction with an unregistered-provider error.

The chat module also defines `claude-local` (drives a locally installed `claude`
CLI) and `openai-compatible` (any OpenAI-shaped endpoint; requires an explicit
base URL). Neither is offered by the `init ai` wizard, and `openai-compatible`
cannot be configured from these keys alone — it needs a `BaseURL` supplied in Go.

When `ai.provider` is unset, the framework uses the `AI_PROVIDER` environment
variable if present, and otherwise defaults to `claude`. `AI_PROVIDER` is read
directly, without the tool's config prefix.

When `ai.fallback.enabled` is true and `ai.fallback.providers` is non-empty, the
first entry becomes the primary. If that disagrees with an explicitly configured
`ai.provider`, the framework logs `ai.fallback.providers[0] overrides
ai.provider` and the fallback list wins.

Provider credentials use the `<provider>.api.*` blocks below.

### Keys the framework does not read

`ai.model` and `ai.claude.local` are read by the **`gtb` generator only**, when
choosing a model for AI-assisted code and doc generation. They are not part of
the runtime chat client, so setting them in a tool built on GTB has no effect on
that tool's own AI calls.

There is no `ai.max_tokens` key. Token limits are a per-call option in Go.

## Version-control and forge keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `vcs.provider` | string | *(the tool's release-source type)* | Overrides which forge's credential subtree and git conventions apply. |
| `github.url.api` | string | `https://api.github.com` | GitHub API base URL. Set for GitHub Enterprise. |
| `github.url.upload` | string | `https://uploads.github.com` | GitHub upload URL. |
| `github.auth.env` | string | `GITHUB_TOKEN` | Name of the env var holding the token. |
| `github.auth.keychain` | string | — | `<service>/<account>` keychain reference. |
| `github.auth.value` | string | — | Literal token (legacy). |
| `github.ssh.key.env` | string | `GITHUB_KEY` | Name of the env var holding the SSH key. |
| `github.ssh.key.path` | string | — | Path to the SSH private key. |
| `github.ssh.key.type` | string | — | SSH key type recorded by the `init github` wizard. |

`gitlab`, `gitea`, `codeberg` and `bitbucket` expose the analogous
`<provider>.url.*` and `<provider>.auth.*` keys. Bitbucket is the exception: it
uses a username/app-password pair rather than a single token, so its keys are
`bitbucket.username`, `bitbucket.username.env`, `bitbucket.app_password`,
`bitbucket.app_password.env` and `bitbucket.keychain`.

Only the values the framework's own defaults ship are guaranteed present; the
rest appear once the matching `init <provider>` wizard has run. `vcs.provider` is
only consulted when it is explicitly set — a value of `direct` (a plain download
source with no git remote) resolves to GitHub's conventions.

See the [VCS component](../../explanation/components/vcs/index.md).

## Telemetry keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `telemetry.enabled` | bool | `false` | Master opt-in. |
| `telemetry.local_only` | bool | `false` | Collect locally but never transmit. |

Telemetry is off until `telemetry.enabled` is explicitly set. Whether the key is
*set at all* is what the consent prompt keys off, so `telemetry.enabled: false`
and an absent key behave the same at runtime but differ in whether you are asked.
Both keys are stripped from an untrusted project-local file.

Remember the underscore rule above: `MYTOOL_TELEMETRY_LOCAL_ONLY` maps to
`telemetry.local.only` and is ignored unless `telemetry.local_only` is already
declared in a lower layer.

See the [Telemetry reference](../cli/telemetry.md) and the
[component docs](../../explanation/components/telemetry/index.md).

## Server keys

Read by tools that use `pkg/http`, `pkg/grpc` or `go/controls`. A plain CLI has
none of them.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server.port` | int | — | Shared port fallback, used when the transport's own `port` is unset. |
| `server.http.host` | string | `""` (all interfaces) | Bind address for the HTTP listener. |
| `server.http.port` | int | — | HTTP listener port. |
| `server.http.max_header_bytes` | int | — | Max request-header size; `0` uses the Go default. |
| `server.grpc.host` | string | `""` (all interfaces) | Bind address for the gRPC listener. |
| `server.grpc.port` | int | — | gRPC listener port. |
| `server.grpc.reflection` | bool | `false` | Enable gRPC server reflection. |
| `server.admin.port` | int | — | Admin/management listener port. |
| `server.admin.reflection` | bool | `false` | Enable reflection on the admin listener. |
| `server.tls.enabled` | bool | `false` | Shared TLS default for every transport. |
| `server.tls.cert` / `server.tls.key` | string | — | Shared certificate and key paths. |
| `server.tls.client_cas` | list of string | `[]` | PEM CA files client certificates must chain to (mTLS). |
| `server.tls.client_auth` | string | `""` | Client-certificate mode: `request`, `verify-if-given`, `require-verify`. |
| `server.<transport>.tls.enabled` | bool | *(inherits `server.tls.enabled`)* | Per-transport override. |
| `server.<transport>.tls.cert` / `.key` | string | *(inherits the shared pair)* | Per-transport override. |

TLS resolves from `server.tls.*` and is then overridden field by field from the
transport's own block (`server.http.tls`, `server.grpc.tls`,
`server.gateway.tls`) — but only for keys that block actually sets, so enabling
TLS globally and overriding just the certificate for one transport works.

**The mTLS keys are shared-only.** `server.tls.client_cas` and
`server.tls.client_auth` apply to every transport; a `client_cas` or
`client_auth` under a transport block (`server.http.tls.client_cas`) is not
carried through and has no effect. An unrecognised `client_auth` value is a hard
error — that one fails closed rather than falling back.

A malformed section — a non-numeric `port`, say — does not fail the load. The
typed decode fails and the adapter falls back to per-key reads, which yield zero
values for the bad keys. A port of `0` means "let the OS choose", so a typo can
produce a server listening on a random port rather than an error.

!!! warning "Bind address defaults to all interfaces"
    `server.http.host` and `server.grpc.host` default to `""`, which binds **all
    interfaces** (`0.0.0.0` / `[::]`). Set them to `127.0.0.1` for any listener
    that should not be reachable off-host — management, metrics or pprof
    endpoints in particular. The standalone metrics/pprof server shipped by
    `go/transport-metrics` defaults to loopback; see the
    [bind-address migration note](../migration/v0.x-server-bind-address.md).

## Credentials

Secrets — AI API keys, forge tokens — resolve through a fixed chain so the
literal value need never be written to disk. For a provider section, GTB checks
in order:

1. `<section>.api.env` (or `<section>.auth.env`) → read the **named** environment variable
2. `<section>.api.keychain` (or `<section>.auth.keychain`) → read the OS keychain via a `<service>/<account>` reference
3. `<section>.api.key` (or `<section>.auth.value`) → the literal value in config (legacy)
4. a well-known unprefixed fallback env var — `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `<FORGE>_TOKEN`

Every step trims whitespace and an empty result falls through, so a
half-configured entry cannot mask a fully-configured one lower down.

A keychain reference that does not parse, or a keychain that is unavailable,
returns nothing and falls through to the next step — it is not an error. That is
deliberate (a laptop without a keyring should still work from an env var) but it
does mean a mistyped keychain reference degrades quietly.

**Literal storage and CI.** The `init` wizards do not *offer* literal storage
when `CI=true`; they offer env-var and, where a keychain is usable, keychain
mode. A literal already written to a config file still resolves under CI — the
restriction is on what the wizard will write, not on what the resolver will read.
`doctor` warns about every literal it finds, and the root pre-run warns
separately when one of these keys is set but empty:

`anthropic.api.key`, `openai.api.key`, `gemini.api.key`, `github.auth.value`,
`gitlab.auth.value`, `gitea.auth.value`, `bitbucket.app_password`.

See the [Credentials component](../../explanation/components/credentials.md) and
[Configure credentials](../../how-to/configure-credentials.md).

## Environment variables read directly

These are read with a bare lookup, independent of the tool's config prefix, so
they apply to every GTB tool on the machine.

| Variable | Effect |
|----------|--------|
| `CI` | When exactly `true`, treats the run as CI: skips update checks, suppresses the telemetry consent prompt, and removes literal storage from the credential wizards. |
| `AI_PROVIDER` | Provider used when `ai.provider` is unset. |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GEMINI_API_KEY` | Last-resort AI credential fallback. |
| `<FORGE>_TOKEN` (e.g. `GITHUB_TOKEN`) | Last-resort forge credential fallback. |
| `EDITOR` / `VISUAL` | Editor launched by `config edit`. |

`CI` is compared against the literal string `true`. `CI=1`, which some runners
set, does **not** trigger CI behaviour — pass `--ci` instead.

## What configuration does not do

- **There is no per-command configuration namespace.** Keys are global to the
  tool. Two commands wanting different values for the same key need two keys.
- **A wrong value rarely stops the tool.** Almost every resolver falls back to a
  default rather than erroring, so a typo usually presents as "the setting had no
  effect". `config validate` is the way to find them, and it only checks
  `log.level` plus the keys a tool declares.
- **`config validate` cannot detect a typo'd key in a framework section.** An
  unknown key under `log`, `update`, `server`, `telemetry`, `ai`, a provider
  section, or any section the tool declares in its own embedded assets is
  accepted without a warning, because the base schema cannot enumerate every
  valid key.
- **Snake-case keys are unreachable from the environment** unless a lower layer
  already defines them. See [the mapping rule above](#why-mytool_update_check_interval-may-do-nothing).
- **`--config` is not additive.** It replaces the default file list *and*
  suppresses the project-local layer.
- **Defaults belong in `assets/config.yaml` only.** `default:` struct tags are
  treated as hint text and never applied.
- **The environment layer is never writable.** `config set` will not persist to
  it, and a value supplied by an env var will appear in `config list` but cannot
  be unset from the CLI.

## Related

- [Root command reference](../cli/root.md#global-flags) — the persistent flags every GTB tool carries
- [`config` command reference](../cli/config.md)
- [Configuration component](../../explanation/components/config/index.md) — what GTB layers on the standalone store
- [Bind flags to config](../../how-to/bind-flags-to-config.md)
- [React to config changes](../../how-to/config-hot-reload.md)
