---
title: VCS & Repository Abstraction
description: Polymorphic repository management and unified Git/GitHub automation.
date: 2026-02-17
tags: [concepts, vcs, git, github, abstraction, memfs]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# VCS & Repository Abstraction

Managing source code repositories is a core requirement for many developer tools. GTB provides a unified abstraction layer over `go-git` and the GitHub API, allowing your tool to interact with repositories consistently across different environments (CI, Local, or CLI).

## Polymorphic Repositories

The `RepoLike` interface (`pkg/vcs/repo.go`) defines the contract for repository operations. This allows the application to switch between different storage strategies without changing business logic.

### 1. Local Repositories (`SourceLocal`)
Standard Git repositories stored on the host's physical disk. Best for tools that require persistent local state or user interaction with the code.

### 2. In-Memory Repositories (`SourceMemory`)
Utilizing `memfs` and `memory.Storage`, these repositories exist entirely in RAM.

- **Use Case**: Pulling a repository for temporary analysis or code generation without leaving artifacts on the user's machine.
- **Performance**: Extremely fast for read-only operations or short-lived tasks.

## The Bridged Filesystem Pattern

One of the most powerful features of the VCS layer is its integration with the `afero` filesystem abstraction.

The `AddToFS` function bridges the `go-git` object model with the application's `Props.FS`:
```go
// Ensures a file from a Git commit is available in the application's Filesystem
err := repo.AddToFS(props.FS, gitFile, targetPath)
```
This allows you to "hydrate" your application's virtual filesystem with files from any point in a repository's history.

## GitHub Client Abstraction

The `GitHubClient` interface provides high-level orchestration for GitHub-specific workflows:

- **PR Management**: Creating and updating Pull Requests with labels and context.
- **Release Discovery**: Discovering tags and downloading binary assets for updates.
- **Repository Setup**: Initializing new repositories and managing SSH keys.

## Transparent Authentication

The VCS layer handles the complexity of Git authentication through a multi-stage fallback strategy:

| Method | Priority | Source |
| :--- | :--- | :--- |
| **SSH Agent** | 1 | Standard Unix SSH Agent |
| **Identity File** | 2 | Configured `github.ssh.key.path` |
| **PAT Token** | 3 | `GITHUB_TOKEN` or `auth.value` |

Developers only need to call `NewRepo(props)`, and the framework determines the best available authentication method based on current configuration and environment.

## Design Goals

- **Testability**: Every VCS operation can be mocked or run against memory-based repositories.
- **Consistency**: High-level operations (Clone, Commit, Push) behave identically regardless of the underlying storage.
- **Simplicity**: Complex operations like PR creation or release asset discovery are encapsulated behind a clean API.
