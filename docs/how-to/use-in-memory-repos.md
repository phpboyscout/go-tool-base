---
title: How to Use In-Memory Repositories
description: Clone and inspect a repository in RAM from a GTB tool, using the props adapter and the extracted go/repo module.
date: 2026-07-19
tags: [how-to, vcs, git, memfs, memory, transient]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Use In-Memory Repositories

For transient analysis, code generation, or CI verification, you may want to
clone and interact with a repository without leaving files on the host disk.

Git operations live in the standalone
[`go/repo`](https://repo.go.phpboyscout.uk) module. This page covers the GTB
half — constructing a repository from your tool's props — and links to the
module docs for everything after that.

## 1. Construct from props

Use the GTB adapter so the repository picks up your tool's forge, credentials
and filesystem from configuration:

```go
import (
    "gitlab.com/phpboyscout/go/repo"

    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    gtbrepo "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/repo"
)

func analyseRepo(p *props.Props, url string) error {
    r, err := gtbrepo.NewRepoFromProps(p)
    if err != nil {
        return err
    }

    // Clone into memory — nothing touches disk
    if _, _, err = r.OpenInMemory(url, "main", repo.WithShallowClone(1)); err != nil {
        return err
    }

    return nil
}
```

`NewThreadSafeRepoFromProps(p)` is the equivalent when the repository will be
shared across goroutines.

Everything after construction is module API — `FileExists`, `GetFile`,
`WalkTree`, `WorkFS`, `AddAll`, `Commit` — called on `r` exactly as documented
on the module site.

## 2. Read the module guides

| Topic | Guide |
|---|---|
| Backends, clone options, the memory ceiling | [Work in memory](https://repo.go.phpboyscout.uk/how-to/in-memory/) |
| The live `afero.Fs` worktree view, and `AddToFS` vs `WorkFS` | [Read and write the worktree](https://repo.go.phpboyscout.uk/how-to/worktree-fs/) |
| `ThreadSafeRepo`, callback rules, the escaped-handle guarantee | [Share a repository across goroutines](https://repo.go.phpboyscout.uk/how-to/concurrency/) |
| Mocking a narrow role vs using a real in-memory repository | [Test with the role mocks](https://repo.go.phpboyscout.uk/how-to/testing/) |

## 3. Why in-memory?

- **Cleanup** — no temporary directories to create, track, or delete, and none
  left behind when a process dies mid-run.
- **Speed** — all I/O stays in memory, markedly faster for small and medium
  repositories.
- **Security** — nothing sensitive is written to shared disk, which matters on
  CI runners and multi-tenant hosts where a temp directory may outlive the job.

!!! warning "Memory constraints"
    Large repositories — especially those with heavy binary history — can consume
    all available RAM. Past a few hundred megabytes, prefer a local shallow clone
    (`WithShallowClone(1)`).

## Related

- **[Repo](../explanation/components/vcs/repo.md)** — how GTB wires the module:
  forge resolution, deferred token resolution, SSH key paths
- **[repo.go.phpboyscout.uk](https://repo.go.phpboyscout.uk)** — full module
  documentation
