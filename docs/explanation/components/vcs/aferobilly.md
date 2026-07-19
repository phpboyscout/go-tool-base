---
title: aferobilly — billy→afero adapter
description: The billy→afero adapter behind the worktree filesystem view has been extracted to the standalone gitlab.com/phpboyscout/go/aferobilly module.
date: 2026-07-19
tags: [components, vcs, repo, afero, billy, filesystem]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# aferobilly — billy→afero adapter

This adapter has been **extracted** to the standalone module
[`gitlab.com/phpboyscout/go/aferobilly`](https://gitlab.com/phpboyscout/go/aferobilly).

- **Guides:** [aferobilly.go.phpboyscout.uk](https://aferobilly.go.phpboyscout.uk)
- **API reference:** [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/aferobilly)

It presents a [`go-billy/v5`](https://github.com/go-git/go-billy) `Filesystem`
as an [`afero.Fs`](https://github.com/spf13/afero). go-git worktrees expose their
filesystem as **billy**; most Go code is written against **afero**. The adapter
bridges the two so afero-based code can read and write a git worktree directly —
one source of truth, no materialise-and-sync step.

It was extracted as its own module rather than folded into
[`repo`](https://repo.go.phpboyscout.uk) because it is **not VCS-specific**: it
works with any billy filesystem (`memfs`, `osfs`, `chroot`), and burying it
inside a git module would have hidden it behind a dependency its users don't
want.

## What moved, and what to read there

The module's docs carry the full detail, including the two things most likely to
bite a caller:

- **The [billy↔afero semantics table](https://aferobilly.go.phpboyscout.uk/explanation/billy-afero-semantics/)**
  — every place the two interfaces disagree and why each mismatch is resolved the
  way it is (`Mkdir` creates parents; `Chmod`/`Chown`/`Chtimes` are no-ops;
  directories get a synthesised read-only handle; the symlink argument-order
  trap).
- **The [concurrency guide](https://aferobilly.go.phpboyscout.uk/how-to/concurrency/)**
  — per-operation locking, why the wrapped billy object is deliberately
  unreachable, and both footguns: operations are atomic but **sequences are
  not**, and the locker is **non-reentrant**.

## GTB usage

GTB no longer imports this module directly. It arrives transitively via
[`repo`](https://repo.go.phpboyscout.uk), which uses it to implement the
`WorkFS()` / `WithWorkFS()` worktree accessors described in
[Repo](repo.md).

## Related Documentation

- **[Repo](repo.md)** — how GTB wires the extracted `go/repo` module
- **[VCS index](index.md)** — package overview
