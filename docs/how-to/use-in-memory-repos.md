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
    "github.com/phpboyscout/go-tool-base/pkg/props"
    "github.com/phpboyscout/go-tool-base/pkg/vcs/repo"
)

func analyzeRepo(p *props.Props, url string) error {
    r, err := repo.NewRepo(p)
    if err != nil {
        return err
    }

    // Clone into memory
    _, _, err = r.OpenInMemory(url, "main")
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
exists, err := r.FileExists("cmd/root.go")
if exists {
    file, _ := r.GetFile("cmd/root.go")
    // Use file.Reader() to read content
}
```

## 3. Accessing the Underlying Repository

Use `WithRepo` and `WithTree` to interact with the underlying `go-git` objects. These callback-style methods keep the pointer scoped to the closure:

```go
err := r.WithRepo(func(gr *git.Repository) error {
    head, err := gr.Head()
    if err != nil {
        return err
    }
    fmt.Println("HEAD:", head.Hash())
    return nil
})
```

!!! warning "Pointer lifetime"
    The `*git.Repository` and `*git.Worktree` pointers passed to the callback are only valid for the duration of the closure. Do not store them in variables outside the callback or pass them to goroutines that outlive the callback — doing so bypasses any thread-safety guarantees.

## 4. Hydrating the Application Filesystem

If you need to move files from the in-memory Git storage to your application's primary filesystem (e.g., for processing or output), use `AddToFS`.

```go
// r.AddToFS(target_fs, git_file, target_path)
err := r.AddToFS(p.FS, gitFile, "/tmp/analysis/root.go")
```

## 5. Concurrent Access

If multiple goroutines need to share a repository, use `ThreadSafeRepo` instead of `Repo`. It wraps every operation with a mutex:

```go
ts, err := repo.NewThreadSafeRepo(p)
if err != nil {
    return err
}

_, _, err = ts.OpenInMemory(url, "main")
if err != nil {
    return err
}

// Safe to call from multiple goroutines
var wg sync.WaitGroup
for range 5 {
    wg.Add(1)
    go func() {
        defer wg.Done()
        exists, _ := ts.FileExists("go.mod")
        fmt.Println("go.mod exists:", exists)
    }()
}
wg.Wait()
```

For single-goroutine workflows, `*Repo` is sufficient and has no locking overhead. See the [Repo component reference](../components/vcs/repo.md#thread-safety) for the full thread-safety guide.

## 6. Why use In-Memory?

- **Cleanup**: No need to manage temporary directories or track files for deletion.
- **Speed**: I/O is restricted to memory, making it significantly faster for small-to-medium repositories.
- **Security**: Reduces the risk of leaving sensitive source code on shared disk space or CI environments.

!!! warning "Memory Constraints"
    Large repositories (especially those with heavy binary history) can quickly consume all available RAM. For repositories over 500MB, consider using a local shallow clone (`WithShallowClone(1)`) instead.
