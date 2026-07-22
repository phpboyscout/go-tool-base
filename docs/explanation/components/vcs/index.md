---
title: VCS
description: How GTB wires the extracted forge and repo modules — release-source config, provider registration, and the config adapters that stay.
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

**`pkg/vcs`** — `ConfigFromReader`, adapting a GTB config view (`config.Reader`,
typically `props.Config.View()`) to the narrow `forge.Config` seam. The seam is
two methods wide precisely so a provider needs no config library; this bridge is
the one place that knows about both. This config glue is **all** that remains in
GTB — the forge clients themselves (GitHub, GitLab, Gitea/Codeberg, Bitbucket,
plus the built-in `direct` source) now live in the external `go/forge` and
`go/forge-<name>` modules. The interactive auth and SSH-key operations GTB used
to reach for on GitHub are now optional `forge.Authenticator` / `forge.KeyManager`
provider capabilities, driven from [setup](../setup/index.md).

**`pkg/vcs/repo`** — the props/config adapters for the `go/repo` module. See
[Repo](repo.md).

---

## Provider registration

Providers register themselves at `init()` when blank-imported.
`pkg/setup/providers.go` imports the full first-party set, because the framework
cannot know which forge a downstream tool targets:

```go
import (
    _ "gitlab.com/phpboyscout/go/forge-bitbucket"
    _ "gitlab.com/phpboyscout/go/forge-github"
    _ "gitlab.com/phpboyscout/go/forge-gitea"
    _ "gitlab.com/phpboyscout/go/forge-gitlab"
    _ "gitlab.com/phpboyscout/go/forge/direct"
)
```

A tool that supports one forge can import only that provider and shed the other
clients entirely — which is the point of the per-provider split. `direct` is not
a forge, so it ships inside the `forge` module.

Registering a source type twice **panics at init**, naming the module at fault.

---

## Configuration

The config-key layout is unchanged: `<forge>.auth.{env,keychain,value}`,
`<forge>.url.*`, and the well-known `<FORGE>_TOKEN` fallbacks. Per-provider keys
and capabilities are documented on the
[providers reference](https://forge.go.phpboyscout.uk/reference/providers/).

Self-update wiring — `props.Tool.ReleaseSource`, `setup.NewUpdater`, the
`update.require_checksum` / `require_signature` policy — is GTB's and is
documented under [setup](../setup/index.md).

## Related

- [Repo](repo.md) — GTB's adapters for the git module
- [forge.go.phpboyscout.uk](https://forge.go.phpboyscout.uk) — the release contract, credential chain, and every provider client
- [Version control](../version-control.md) — the component family
