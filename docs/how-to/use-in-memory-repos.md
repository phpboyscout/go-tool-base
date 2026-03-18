---
title: How to Use In-Memory Repositories
description: Guide to using the RepoLike interface and SourceMemory for transient analysis.
date: 2026-02-17
tags: [how-to, vcs, git, memfs, memory, transient]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Use In-Memory Repositories

For tasks like transient analysis, code generation, or CI verification, you may want to clone and interact with a repository without leaving files on the host disk. GTB supports this via the `SourceMemory` strategy.

## 1. Initialize a Memory Repository

Use `NewRepo` and `OpenInMemory` to clone a repository into RAM using `memfs`.

```go
import (
    "github.com/phpboyscout/gtb/pkg/props"
    "github.com/phpboyscout/gtb/pkg/vcs"
)

func analyzeRepo(p *props.Props, url string) error {
    repo, err := vcs.NewRepo(p)
    if err != nil {
        return err
    }

    // Clone into memory
    _, _, err = repo.OpenInMemory(url, "main")
    if err != nil {
        return err
    }
    
    // The repository is now resident in memory
    return nil
}
```

## 2. Inspect Files In-Memory

You can walk the tree or check for specific files without touching the disk.

```go
exists, err := repo.FileExists("cmd/root.go")
if exists {
    file, _ := repo.GetFile("cmd/root.go")
    // Use file.Reader() to read content
}
```

## 3. Hydrating the Application Filesystem

If you need to move files from the in-memory Git storage to your application's primary filesystem (e.g., for processing or output), use `AddToFS`.

```go
// repo.AddToFS(target_fs, git_file, target_path)
err := repo.AddToFS(p.FS, gitFile, "/tmp/analysis/root.go")
```

## 4. Why use In-Memory?

- **Cleanup**: No need to manage temporary directories or track files for deletion.
- **Speed**: I/O is restricted to memory, making it significantly faster for small-to-medium repositories.
- **Security**: Reduces the risk of leaving sensitive source code on shared disk space or CI environments.

!!! warning "Memory Constraints"
    Large repositories (especially those with heavy binary history) can quickly consume all available RAM. For repositories over 500MB, consider using a local shallow clone (`WithShallowClone(1)`) instead.
