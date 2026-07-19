---
title: Repo
description: How GTB wires the standalone go/repo module — config-derived settings, forge auth resolution, and the props adapters.
date: 2026-07-19
tags: [components, vcs, git, repo, memfs, afero]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Repo

Git repository operations have been **extracted** to the standalone module
[`gitlab.com/phpboyscout/go/repo`](https://gitlab.com/phpboyscout/go/repo).

- **Guides:** [repo.go.phpboyscout.uk](https://repo.go.phpboyscout.uk)
- **API reference:** [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/repo)

Clone/commit/branch/checkout, tree inspection, the in-memory and filesystem
backends, the `RepoLike` role interfaces, `ThreadSafeRepo` and the live
`afero.Fs` worktree view are all documented there. This page covers only what
remains GTB's concern: **turning GTB configuration into the module's
`Settings`.**

The billy↔afero bridge behind the worktree view was extracted alongside it, to
[`aferobilly`](https://aferobilly.go.phpboyscout.uk) — see
[aferobilly](aferobilly.md).

---

## Package: `pkg/vcs/repo`

What remains in GTB is a **composition root**, not a wrapper. It resolves the
framework concerns the module deliberately refuses to know about — the release
source, the config subtree, the credential chain, the SSH key location — and
hands the module plain data.

Nothing is re-exported. Import the module directly for its own types, and this
package only for the adapters below:

```go
import (
    "gitlab.com/phpboyscout/go/repo"

    gtbrepo "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/repo"
)

r, err := gtbrepo.NewRepoFromProps(p)       // GTB-wired construction
found, err := repo.DiscoverRepository(path) // module API, used directly
```

| Adapter | Purpose |
|---------|---------|
| `SettingsFromProps(p)` | Maps `props.Tool`, `Config`, `Logger`, `FS` into `repo.Settings` |
| `SettingsFromContainable(source, cfg, log, fs)` | The same mapping from an explicit release source + config container |
| `NewRepoFromProps(p, ops...)` | `repo.NewRepo` with GTB-derived settings |
| `NewThreadSafeRepoFromProps(p, opts...)` | `repo.NewThreadSafeRepo` with GTB-derived settings |

---

## Forge resolution

The forge is derived from `Tool.ReleaseSource.Type` (`github`, `gitlab`,
`bitbucket`, `gitea`, `codeberg`), overridable with the `vcs.provider` config
key. An empty or `direct` type falls back to `github` — the `direct` release
source is a download URL with no git remote, so it has no forge of its own.

The adapter normalises the name **once** and stores the result in
`Settings.Forge`. The module applies the same normalisation internally to pick
the git-over-HTTPS username, so that pass is a no-op: the config subtree, the
fallback environment variable and the auth convention are guaranteed to agree
rather than agreeing by coincidence.

---

## Authentication

Credentials are resolved from the tool's **forge** config subtree:

| Priority | Condition | Auth method |
|----------|-----------|-------------|
| 1 | `<forge>.ssh.key.type = "agent"` | SSH agent |
| 2 | `<forge>.ssh.key.path`, or the env var named by `<forge>.ssh.key.env` | Identity file |
| 3 | No `<forge>.ssh` config at all | Token via `vcs.ResolveToken` on the `<forge>` subtree (`auth.env` → `auth.keychain` → `auth.value` → `<FORGE>_TOKEN`) |

Two properties of this wiring are deliberate, and worth keeping in mind before
changing it:

- **Token resolution is deferred.** The adapter binds the config subtree eagerly
  but wraps `vcs.ResolveToken` in the module's `TokenSource` closure. A
  repository authenticating over SSH therefore never walks the credential chain,
  so it never triggers a keychain unlock prompt it does not need.
- **Environment reading happens here, not in the module.** The module reads no
  environment of its own — every input arrives through `Settings`. The adapter
  calls the module's `repo.KeyPath` helper to apply the documented "explicit
  path, else named env var" precedence for `<forge>.ssh.key`.

**Missing credentials are non-fatal for public repositories.** When no token
resolves and `ReleaseSource.Private` is false, construction proceeds with
unauthenticated access. Only `Private: true` enforces a token, failing fast with
a hint naming the fallback env var.

Existing `github.*` configs keep working unchanged — `github` was always the
GitHub forge subtree; other forges' subtrees are simply read alongside. No
migration is required.

---

## Related Documentation

- **[repo.go.phpboyscout.uk](https://repo.go.phpboyscout.uk)** — module guides:
  clone and commit, authentication, in-memory repositories, the worktree
  filesystem, concurrency, and testing with the role mocks
- **[aferobilly](aferobilly.md)** — the extracted billy↔afero bridge
- **[VCS index](index.md)** — package overview and authentication helper
- **[GitHub](github.md)** — GitHub API client (separate from git operations)
- **[Generator](../internal/generator.md)** — GTB's main consumer, for scaffold
  git initialisation and template-source clones
