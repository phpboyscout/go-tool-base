---
title: "VCS"
description: "How GTB wires the extracted forge and repo modules: release-source config, provider registration, and the config adapters that stay."
date: 2026-07-19
tags: [components, vcs, forge, releases, git]
---

# VCS

The VCS layer has been **extracted** into standalone modules. What remains in
`pkg/vcs` is the glue that turns GTB configuration into what those modules need.

| Concern | Module | Docs |
|---|---|---|
| Release providers, registry, credential chain | `gitlab.com/phpboyscout/go/forge` | [forge.go.phpboyscout.uk](https://forge.go.phpboyscout.uk) |
| GitHub, GitLab, Gitea/Codeberg, Bitbucket | `go/forge-<name>` | [providers reference](https://forge.go.phpboyscout.uk/reference/providers/) |
| Git operations (clone, commit, worktrees) | `gitlab.com/phpboyscout/go/repo` | [repo.go.phpboyscout.uk](https://repo.go.phpboyscout.uk) |
| billy↔afero bridge | `gitlab.com/phpboyscout/go/aferobilly` | [aferobilly.go.phpboyscout.uk](https://aferobilly.go.phpboyscout.uk) |

---

## What stays in GTB

**`pkg/vcs`**, `ConfigFromReader`, adapting a GTB config view (`config.Reader`,
typically `props.Config.View()`) to the narrow `forge.Config` seam. The seam is
two methods wide precisely so a provider needs no config library; this bridge is
the one place that knows about both. It is also the forge-facing *view* of the
credential keys: GTB dereferences `auth.env` and `auth.keychain` itself (see
[credential precedence](../../../reference/migration/v0.x-forge-credential-precedence.md)),
so the view presents the resolved credential as `auth.value` and shows the two
pointer keys as unset. A provider factory composes `forge.ConfigCredential`
first, and that reports a configuration carrying either key beside an empty
`auth.value` as stale, failing construction; with GTB's shipped
`<forge>.auth.env` defaults that was every consumer's update check in a bare CI
image. This config glue is **all** that remains in
GTB: the forge clients themselves (GitHub, GitLab, Gitea/Codeberg, Bitbucket,
plus the built-in `direct` source) now live in the external `go/forge` and
`go/forge-<name>` modules. The interactive auth and SSH-key operations GTB used
to reach for on GitHub are now optional `forge.Authenticator` / `forge.KeyManager`
provider capabilities, driven from [setup](../setup/index.md).

**`pkg/vcs/repo`**: the props/config adapters for the `go/repo` module. See
[Repo](repo.md).

---

## Provider registration

Providers register themselves at `init()` when blank-imported. The framework
registers only `direct` (`pkg/setup/providers.go`); a forge adapter is a blank
import in the binary that ships it, so a tool links exactly the forges it
enables and no other forge's SDK. `gtb`'s own main links every one
(`cli/cmd/gtb/providers.go`):

```go
import (
    _ "gitlab.com/phpboyscout/go/forge-bitbucket"
    _ "gitlab.com/phpboyscout/go/forge-gitea" // gitea, codeberg
    _ "gitlab.com/phpboyscout/go/forge-github"
    _ "gitlab.com/phpboyscout/go/forge-gitlab"
)
```

`forge.ModuleFor(type)` names the module for each forge type, the root
command's pre-run fails early when an enabled forge feature has no registered
provider, and `doctor` reports the same as **Forge adapters**. See the
[migration note](../../../reference/migration/v0.x-adapters-registered-by-main.md).

A tool that supports one forge can import only that provider and shed the other
clients entirely, which is the point of the per-provider split. `direct` is not
a forge, so it ships inside the `forge` module.

Registering a source type twice **panics at init**, naming the module at fault.

---

## Configuration

The config-key layout is unchanged: `<forge>.auth.{env,keychain,value}`,
`<forge>.url.*`, and the well-known `<FORGE>_TOKEN` fallbacks. Per-provider keys
and capabilities are documented on the
[providers reference](https://forge.go.phpboyscout.uk/reference/providers/).

Self-update wiring, `props.Tool.ReleaseSource`, `setup.NewUpdater`, the
`update.require_checksum` / `require_signature` policy, is GTB's and is
documented under [setup](../setup/index.md).

## Related

- [Repo](repo.md): GTB's adapters for the git module
- [forge.go.phpboyscout.uk](https://forge.go.phpboyscout.uk): the release contract, credential chain, and every provider client
- [Version control](../version-control.md): the component family
