---
title: Version Control
description: Integrated Git functionality and pluggable VCS API clients (GitHub/GitLab) for repository management.
date: 2026-03-18
tags: [components, vcs, git, github, gitlab, version-control]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

The Version Control component provides integrated Git functionality and repository management capabilities for GTB applications. It enables seamless integration with multiple VCS providers (GitHub and GitLab), automated version checking, and repository operations.

## Overview

The VCS component provides two distinct areas of functionality:

1. **Pluggable VCS API Integration** - Standardized integration with GitHub Enterprise and GitLab APIs for repository management, pull requests, releases, and asset downloads.

2. **Git Repository Operations** - The `Repo` struct enables direct Git operations on repositories, supporting both local filesystem and in-memory repository management.

### Abstraction Strategy

The VCS package implements custom abstractions over upstream Git and GitHub libraries to provide several key benefits:

**Enhanced Testing and Mocking Strategy**
: Many upstream packages (like `go-git` and `google/go-github`) don't provide comprehensive interfaces suitable for testing. Our abstractions (`GitHubClient`, `RepoLike`, and `release.Provider` interfaces) enable clean dependency injection and comprehensive mocking capabilities that wouldn't otherwise be available.

**Multi-Backend Support**
: The component is architected to support multiple version control backends. While GitHub remains the primary focus, the system now includes robust GitLab support, especially for self-updating and release management.

**Simplified Authentication Layer**
: Rather than dealing with the complex authentication patterns of multiple upstream libraries, our abstractions provide a unified authentication interface that handles SSH keys, basic authentication, and token-based authentication consistently across both GitHub API operations and Git repository operations.

**Filesystem Abstraction Integration**
: Our implementations integrate seamlessly with the `afero` filesystem abstraction used throughout the GTB framework, allowing for consistent file operations whether working with local filesystems, in-memory filesystems, or other storage backends.

**Unified Error Handling**
: The abstractions provide consistent error handling patterns using the `cockroachdb/errors` package, ensuring that all VCS operations follow the same error wrapping and context patterns used throughout the framework.

This separation allows for flexible integration patterns where you can use GitHub API operations independently of local Git operations, or combine them for comprehensive version control workflows, all while maintaining excellent testability and consistent interfaces.

## GitHub Integration

The VCS package provides comprehensive GitHub Enterprise API integration. This component is specifically designed to interact with both public GitHub and GitHub Enterprise instances.

### GHClient - GitHub Enterprise Client

The `GHClient` is the primary implementation for GitHub Enterprise API operations. Engineers should use this concrete type rather than the interface directly:

```go
type GHClient struct {
    Client *github.Client
    cfg    config.Containable
}

// Create a new GitHub Enterprise client
func NewGitHubClient(cfg config.Containable) (*GHClient, error)

// Core methods available on GHClient:
func (c *GHClient) GetClient() *github.Client
func (c *GHClient) CreatePullRequest(ctx context.Context, owner string, repo string, pull *github.NewPullRequest) (*github.PullRequest, error)
func (c *GHClient) GetPullRequestByBranch(ctx context.Context, owner, repo, branch, state string) (*github.PullRequest, error)
func (c *GHClient) AddLabelsToPullRequest(ctx context.Context, owner, repo string, number int, labels []string) error
func (c *GHClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, pull *github.PullRequest) (*github.PullRequest, *github.Response, error)
func (c *GHClient) CreateRepo(ctx context.Context, owner, slug string) (*github.Repository, error)
func (c *GHClient) UploadKey(ctx context.Context, name string, key []byte) error
func (c *GHClient) ListReleases(ctx context.Context, owner, repo string) ([]string, error)
func (c *GHClient) GetReleaseAssets(ctx context.Context, owner, repo, tag string) ([]*github.ReleaseAsset, error)
func (c *GHClient) GetReleaseAssetID(ctx context.Context, owner, repo, tag, assetName string) (int64, error)
func (c *GHClient) DownloadAsset(ctx context.Context, owner, repo string, assetID int64) (io.ReadCloser, error)
func (c *GHClient) DownloadAssetTo(ctx context.Context, fs afero.Fs, owner, repo string, assetID int64, filePath string) error
func (c *GHClient) GetFileContents(ctx context.Context, owner, repo, path, ref string) (string, error)
```

### Creating a GitHub Enterprise Client

Create a `GHClient` instance to interact with your GitHub Enterprise instance:

```go
func setupGitHubClient(props *props.Props) (*vcs.GHClient, error) {
    // Create GitHub client using configuration
    client, err := vcs.NewGitHubClient(props.Config.Sub("github"))
    if err != nil {
        return nil, errors.WrapPrefix(err, "failed to create GitHub client", 0)
    }
    return client, nil
}
```

**Configuration for GitHub Enterprise:**

```yaml
github:
  url:
    api: "https://github.enterprise.com/api/v3"        # GitHub Enterprise API URL
    upload: "https://github.enterprise.com/api/v3"     # GitHub Enterprise upload URL
  auth:
    env: "GITHUB_TOKEN"                                 # Environment variable name for token
    value: "ghp_xxxxxxxxxxxx"                           # Direct token value (not recommended)
```

**Token Resolution Order:**

1. `auth.value` in configuration (if present)
2. Environment variable specified in `auth.env`
3. Fallback to `GITHUB_TOKEN` environment variable

### Repository Management Operations

Use the `GHClient` to interact with GitHub Enterprise repositories:

```go
func listReleases(ctx context.Context, props *props.Props) error {
    client, err := setupGitHubClient(props)
    if err != nil {
        return errors.WrapPrefix(err, "failed to setup GitHub client", 0)
    }

    owner := props.Config.GetString("github.owner")
    repo := props.Config.GetString("github.repo")

    releases, err := client.ListReleases(ctx, owner, repo)
    if err != nil {
        return errors.WrapPrefix(err, "failed to list releases", 0)
    }

    props.Logger.Info("Found releases", "count", len(releases))
    for _, release := range releases {
        props.Logger.Info("Release", "tag", release)
    }

    return nil
}
```

### Pull Request Workflows

Create and manage pull requests using the `GHClient`:

Create and manage pull requests in GitHub Enterprise:

```go
func createPullRequest(ctx context.Context, owner, repo, title, body, head, base string, props *props.Props) error {
    client, err := setupGitHubClient(props)
    if err != nil {
        return err
    }

    newPR := &github.NewPullRequest{
        Title: github.Ptr(title),
        Body:  github.Ptr(body),
        Head:  github.Ptr(head),
        Base:  github.Ptr(base),
    }

    pr, err := client.CreatePullRequest(ctx, owner, repo, newPR)
    if err != nil {
        return errors.WrapPrefix(err, "failed to create pull request", 0)
    }

    props.Logger.Info("Pull request created",
        "number", pr.GetNumber(),
        "url", pr.GetHTMLURL())

    // Add labels if needed
    labels := []string{"enhancement", "automated"}
    if err := client.AddLabelsToPullRequest(ctx, owner, repo, pr.GetNumber(), labels); err != nil {
        props.Logger.Warn("Failed to add labels", "error", err)
    }

    return nil
}
```

### Release Asset Management

Download assets from GitHub Enterprise releases using the `GHClient`:

```go
func downloadReleaseAsset(ctx context.Context, owner, repo, tag, assetName, outputPath string, props *props.Props) error {
    client, err := setupGitHubClient(props)
    if err != nil {
        return err
    }

    // Get asset ID
    assetID, err := client.GetReleaseAssetID(ctx, owner, repo, tag, assetName)
    if err != nil {
        return errors.WrapPrefix(err, "failed to get asset ID", 0)
    }

    props.Logger.Info("Downloading asset",
        "asset", assetName,
        "id", assetID,
        "output", outputPath)

    // Download asset to filesystem
    if err := client.DownloadAssetTo(ctx, props.FS, owner, repo, assetID, outputPath); err != nil {
        return errors.WrapPrefix(err, "failed to download asset", 0)
    }

    props.Logger.Info("Asset downloaded successfully")
    return nil
}
```

### File Content Retrieval

Retrieve file contents directly from GitHub Enterprise using the `GHClient`:

```go
func getFileFromGitHub(ctx context.Context, owner, repo, path, ref string, props *props.Props) (string, error) {
    client, err := setupGitHubClient(props)
    if err != nil {
        return "", err
    }

    content, err := client.GetFileContents(ctx, owner, repo, path, ref)
    if err != nil {
        return "", errors.WrapPrefix(err, "failed to get file contents", 0)
    }

    props.Logger.Info("Retrieved file from GitHub",
        "repo", fmt.Sprintf("%s/%s", owner, repo),
        "path", path,
        "ref", ref,
        "size", len(content))

    return content, nil
}
```

### GitHubClient Interface (For Testing and Mocking)

The `GitHubClient` interface is primarily used for testing and when working with provided mocks. In production code, use the concrete `GHClient` type:

```go
type GitHubClient interface {
    GetClient() *github.Client
    CreatePullRequest(ctx context.Context, owner string, repo string, pull *github.NewPullRequest) (*github.PullRequest, error)
    GetPullRequestByBranch(ctx context.Context, owner, repo, branch, state string) (*github.PullRequest, error)
    AddLabelsToPullRequest(ctx context.Context, owner, repo string, number int, labels []string) error
    UpdatePullRequest(ctx context.Context, owner, repo string, number int, pull *github.PullRequest) (*github.PullRequest, *github.Response, error)
    CreateRepo(ctx context.Context, owner, slug string) (*github.Repository, error)
    UploadKey(ctx context.Context, name string, key []byte) error
    ListReleases(ctx context.Context, owner, repo string) ([]string, error)
    GetReleaseAssets(ctx context.Context, owner, repo, tag string) ([]*github.ReleaseAsset, error)
    GetReleaseAssetID(ctx context.Context, owner, repo, tag, assetName string) (int64, error)
    DownloadAsset(ctx context.Context, owner, repo string, assetID int64) (io.ReadCloser, error)
    DownloadAssetTo(ctx context.Context, fs afero.Fs, owner, repo string, assetID int64, filePath string) error
    GetFileContents(ctx context.Context, owner, repo, path, ref string) (string, error)
}

## GitLab Integration

GitLab support is focused on release management and self-updating capabilities. It supports both GitLab.com and self-managed GitLab instances.

### GitLab Configuration

Configure GitLab in your `config.yaml`:

```yaml
gitlab:
  url:
    api: "https://gitlab.example.com/api/v4"
  auth:
    env: "GITLAB_TOKEN"
    value: ""
```

## Release Provider Abstraction

For features like self-updating, GTB uses a unified `release.Provider` interface that abstracts away the differences between GitHub and GitLab.

### Release Provider Interfaces

```go
type Provider interface {
    GetLatestRelease(ctx context.Context, owner, repo string) (Release, error)
    GetReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error)
    ListReleases(ctx context.Context, owner, repo string, limit int) ([]Release, error)
    DownloadReleaseAsset(ctx context.Context, owner, repo string, asset ReleaseAsset) (io.ReadCloser, string, error)
}

type Release interface {
    GetName() string
    GetTagName() string
    GetBody() string
    GetDraft() bool
    GetAssets() []ReleaseAsset
}

type ReleaseAsset interface {
    GetID() int64
    GetName() string
    GetBrowserDownloadURL() string
}
```

```

## Git Repository Operations

The VCS package provides comprehensive Git repository management through the `Repo` struct. This component handles direct Git operations on both local filesystem and in-memory repositories, making it ideal for different deployment scenarios including CI/CD pipelines and local development.

### Repo - Git Repository Manager

The `Repo` struct is the primary implementation for Git repository operations. Engineers should use this concrete type rather than the interface directly:

```go
type Repo struct {
    source int
    config *config.Config
    repo   *git.Repository
    auth   transport.AuthMethod
    tree   *git.Worktree
}

// Create a new repository manager
func NewRepo(props *props.Props, ops ...RepoOpt) (*Repo, error)

// Core methods available on Repo:
func (r *Repo) SourceIs(source int) bool
func (r *Repo) SetSource(source int)
func (r *Repo) SetRepo(repo *git.Repository)
func (r *Repo) GetRepo() *git.Repository
func (r *Repo) SetKey(key *ssh.PublicKeys)
func (r *Repo) SetBasicAuth(username, password string)
func (r *Repo) GetAuth() transport.AuthMethod
func (r *Repo) SetTree(tree *git.Worktree)
func (r *Repo) GetTree() *git.Worktree
func (r *Repo) Checkout(plumbing.ReferenceName) error
func (r *Repo) CheckoutCommit(plumbing.Hash) error
func (r *Repo) CreateBranch(string) error
func (r *Repo) Push(*git.PushOptions) error
func (r *Repo) Commit(string, *git.CommitOptions) (plumbing.Hash, error)

// Repository operations
func (r *Repo) OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
func (r *Repo) OpenLocal(string, string) (*git.Repository, *git.Worktree, error)
func (r *Repo) Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
func (r *Repo) Clone(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)

// File operations for in-memory repositories
func (r *Repo) WalkTree(func(*object.File) error) error
func (r *Repo) FileExists(string) (bool, error)
func (r *Repo) DirectoryExists(string) (bool, error)
func (r *Repo) GetFile(string) (*object.File, error)
func (r *Repo) AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error
```

### Repository Types and Clone Options

Git repository operations support different storage strategies and cloning configurations:

```go
type RepoType = string

var (
    LocalRepo    RepoType = "local"      // Filesystem-based repository
    InMemoryRepo RepoType = "inmemory"   // Memory-based repository
)

// CloneOption represents a function that configures clone options.
type CloneOption func(*git.CloneOptions)

// WithShallowClone configures a shallow clone with the specified depth.
func WithShallowClone(depth int) CloneOption

// WithSingleBranch configures the clone to only fetch a single branch.
func WithSingleBranch(branch string) CloneOption

// WithNoTags configures the clone to skip fetching tags.
func WithNoTags() CloneOption

// WithRecurseSubmodules configures recursive submodule cloning.
func WithRecurseSubmodules() CloneOption
```

### Repository Setup and Authentication

Create and configure a `Repo` instance with appropriate authentication:

```go
func setupRepository(props *props.Props) (*vcs.Repo, error) {
    repo, err := vcs.NewRepo(props)
    if err != nil {
        return nil, errors.WrapPrefix(err, "failed to create repo", 0)
    }

    // Configure authentication for Git operations
    if props.Config.Has("github.ssh_key") {
        keyPath := props.Config.GetString("github.ssh_key")

        publicKey, err := ssh.NewPublicKeysFromFile("git", keyPath, "")
        if err != nil {
            return nil, errors.WrapPrefix(err, "failed to parse SSH key", 0)
        }

        repo.SetKey(publicKey)
    } else if props.Config.Has("github.username") && props.Config.Has("github.password") {
        repo.SetBasicAuth(
            props.Config.GetString("github.username"),
            props.Config.GetString("github.password"),
        )
    }

    return repo, nil
}
```

### Repository Cloning Strategies

Clone repositories using different strategies based on your requirements with the `Repo`:

```go
func cloneRepository(ctx context.Context, repoURL, repoType string, props *props.Props) (*git.Repository, *git.Worktree, error) {
    repo, err := setupRepository(props)
    if err != nil {
        return nil, nil, err
    }

    props.Logger.Info("Cloning repository",
        "url", repoURL,
        "type", repoType)

    var gitRepo *git.Repository
    var worktree *git.Worktree

    switch repoType {
    case vcs.InMemoryRepo:
        // Clone into memory for temporary operations (CI/CD, analysis)
        gitRepo, worktree, err = repo.OpenInMemory(repoURL, "")
    case vcs.LocalRepo:
        // Clone to local filesystem for persistent operations
        gitRepo, worktree, err = repo.OpenLocal(repoURL, "./local-repo")
    default:
        return nil, nil, fmt.Errorf("unknown repository type: %s", repoType)
    }

    if err != nil {
        return nil, nil, errors.WrapPrefix(err, "failed to clone repository", 0)
    }

    props.Logger.Info("Repository cloned successfully")
    return gitRepo, worktree, nil
}
```

### Advanced Cloning Options

Configure specific cloning behavior for different scenarios using the `Repo`:

```go
func cloneWithOptions(ctx context.Context, repoURL string, props *props.Props) (*git.Repository, *git.Worktree, error) {
    repo, err := setupRepository(props)
    if err != nil {
        return nil, nil, err
    }

    // Clone with optimized options for CI/CD
    gitRepo, worktree, err := repo.Clone(repoURL, "./shallow-repo",
        vcs.WithShallowClone(1),           // Only latest commit
        vcs.WithSingleBranch("main"),      // Only main branch
        vcs.WithNoTags(),                  // Skip tags for faster clone
    )

    if err != nil {
        return nil, nil, errors.WrapPrefix(err, "failed to clone with options", 0)
    }

    return gitRepo, worktree, nil
}
```

### Branch Management

Perform Git branch operations using the `Repo`:

```go
func manageBranches(ctx context.Context, repo *vcs.Repo, props *props.Props) error {
    // Create a new branch
    if err := repo.CreateBranch("feature/new-feature"); err != nil {
        return errors.WrapPrefix(err, "failed to create branch", 0)
    }

    // Checkout the branch
    branchRef := plumbing.NewBranchReferenceName("feature/new-feature")
    if err := repo.Checkout(branchRef); err != nil {
        return errors.WrapPrefix(err, "failed to checkout branch", 0)
    }

    props.Logger.Info("Switched to branch", "branch", "feature/new-feature")
    return nil
}
```

### Commit Operations

Create commits with proper metadata using the `Repo`:

```go
func commitChanges(ctx context.Context, repo *vcs.Repo, message string, props *props.Props) error {
    // Create commit with author information
    commitOpts := &git.CommitOptions{
        Author: &object.Signature{
            Name:  props.Config.GetString("git.author.name"),
            Email: props.Config.GetString("git.author.email"),
            When:  time.Now(),
        },
    }

    commitHash, err := repo.Commit(message, commitOpts)
    if err != nil {
        return errors.WrapPrefix(err, "failed to create commit", 0)
    }

    props.Logger.Info("Changes committed",
        "commit", commitHash.String(),
        "message", message)

    return nil
}
```

### Repository File Operations

Access and manipulate files within Git repositories using the `Repo`:

```go
func processRepositoryFiles(ctx context.Context, repo *vcs.Repo, props *props.Props) error {
    // Walk through all files in the repository
    err := repo.WalkTree(func(file *object.File) error {
        props.Logger.Debug("Processing file", "path", file.Name)

        // Check if it's a specific file type
        if strings.HasSuffix(file.Name, ".go") {
            // Process Go files
            content, err := file.Contents()
            if err != nil {
                return errors.WrapPrefix(err, "failed to read file contents", 0)
            }

            props.Logger.Info("Found Go file",
                "path", file.Name,
                "size", len(content))
        }

        return nil
    })

    if err != nil {
        return errors.WrapPrefix(err, "failed to walk repository tree", 0)
    }

    return nil
}
```

### File Existence Checks

Verify repository structure and required files using the `Repo`:

```go
func checkRepositoryStructure(ctx context.Context, repo *vcs.Repo, props *props.Props) error {
    // Check for important files
    requiredFiles := []string{"go.mod", "README.md", "LICENSE"}

    for _, filename := range requiredFiles {
        exists, err := repo.FileExists(filename)
        if err != nil {
            return errors.WrapPrefix(err, fmt.Sprintf("failed to check file %s", filename), 0)
        }

        if exists {
            props.Logger.Info("Required file found", "file", filename)
        } else {
            props.Logger.Warn("Required file missing", "file", filename)
        }
    }

    // Check for directories
    directories := []string{"cmd", "pkg", "internal"}

    for _, dirname := range directories {
        exists, err := repo.DirectoryExists(dirname)
        if err != nil {
            return errors.WrapPrefix(err, fmt.Sprintf("failed to check directory %s", dirname), 0)
        }

        if exists {
            props.Logger.Info("Directory found", "directory", dirname)
        }
    }

    return nil
}
```

### Extract Files to Filesystem

Extract repository files to the local filesystem using the `Repo`:

```go
func extractRepositoryFiles(ctx context.Context, repo *vcs.Repo, targetDir string, props *props.Props) error {
    return repo.WalkTree(func(file *object.File) error {
        // Determine target path
        targetPath := filepath.Join(targetDir, file.Name)

        // Extract file to filesystem
        if err := repo.AddToFS(props.FS, file, targetPath); err != nil {
            return errors.WrapPrefix(err, fmt.Sprintf("failed to extract file %s", file.Name), 0)
        }

        props.Logger.Debug("Extracted file", "source", file.Name, "target", targetPath)
        return nil
    })
}
```

## Testing VCS Operations

### 1. Mock GitHub Client

Test GitHub operations with mocks:

```go
func TestGitHubOperations(t *testing.T) {
    mockClient := vcs.NewMockGitHubClient(t)

    expectedReleases := []string{"v1.0.0", "v2.0.0", "v2.1.0"}

    mockClient.EXPECT().ListReleases(mock.Anything, "owner", "repo").Return(expectedReleases, nil)

    // Test the operation
    props := &props.Props{
        Config: createTestConfig(),
        Logger: log.New(io.Discard),
    }

    releases, err := mockClient.ListReleases(context.Background(), "owner", "repo")

    assert.NoError(t, err)
    assert.Equal(t, 3, len(releases))
    assert.Equal(t, "v2.1.0", releases[2])
}
```

### 2. Mock Repository Testing

Test repository operations with mocks:

```go
func TestRepoOperations(t *testing.T) {
    mockRepo := vcs.NewMockRepoLike(t)

    // Set up mock expectations
    mockRepo.EXPECT().SourceIs(vcs.SourceMemory).Return(true)
    mockRepo.EXPECT().CreateBranch("feature/test").Return(nil)
    mockRepo.EXPECT().Checkout(mock.AnythingOfType("plumbing.ReferenceName")).Return(nil)

    // Test branch operations
    assert.True(t, mockRepo.SourceIs(vcs.SourceMemory))

    err := mockRepo.CreateBranch("feature/test")
    assert.NoError(t, err)

    branchRef := plumbing.NewBranchReferenceName("feature/test")
    err = mockRepo.Checkout(branchRef)
    assert.NoError(t, err)
}
```

### 3. Integration Testing

Test with real repositories in controlled environments:

```go
func TestRealGitOperations(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    // Create temporary directory for test repository
    tempDir := t.TempDir()

    // Set up props with temporary filesystem
    logger := log.New(io.Discard)
    props := &props.Props{
        Logger: logger,
        FS:     afero.NewOsFs(),
        Config: createTestConfig(),
    }

    // Create and configure repository
    repo, err := vcs.NewRepo(props)
    require.NoError(t, err)

    // Test cloning into memory
    gitRepo, worktree, err := repo.OpenInMemory("https://github.com/go-git/go-git.git", "")
    require.NoError(t, err)
    require.NotNil(t, gitRepo)
    require.NotNil(t, worktree)

    // Test file operations
    exists, err := repo.FileExists("README.md")
    assert.NoError(t, err)
    assert.True(t, exists)

    // Test walking tree
    fileCount := 0
    err = repo.WalkTree(func(file *object.File) error {
        fileCount++
        return nil
    })
    assert.NoError(t, err)
    assert.Greater(t, fileCount, 0)
}
```

### 4. Testing Asset Downloads

Test GitHub asset download functionality:

```go
func TestAssetDownload(t *testing.T) {
    mockClient := vcs.NewMockGitHubClient(t)
    mockFS := afero.NewMemMapFs()

    // Mock asset download
    mockClient.EXPECT().GetReleaseAssetID(mock.Anything, "owner", "repo", "v1.0.0", "asset.tar.gz").Return(int64(12345), nil)
    mockClient.EXPECT().DownloadAssetTo(mock.Anything, mockFS, "owner", "repo", int64(12345), "/tmp/asset.tar.gz").Return(nil)

    // Test download
    assetID, err := mockClient.GetReleaseAssetID(context.Background(), "owner", "repo", "v1.0.0", "asset.tar.gz")
    assert.NoError(t, err)
    assert.Equal(t, int64(12345), assetID)

    err = mockClient.DownloadAssetTo(context.Background(), mockFS, "owner", "repo", assetID, "/tmp/asset.tar.gz")
    assert.NoError(t, err)
}
```

## Configuration

### VCS Configuration Options

```yaml
# config.yaml

# Provider selection
vcs:
  provider: github # Options: github, gitlab

github:
  url:
    api: "https://api.github.com"          # GitHub API base URL
    upload: "https://uploads.github.com"   # GitHub upload API URL
  auth:
    env: "GITHUB_TOKEN"                    # Environment variable for token
  owner: "your-org"                        # Default repository owner
  repo: "your-repo"                        # Default repository name

gitlab:
  url:
    api: "https://gitlab.com/api/v4"       # GitLab API base URL
  auth:
    env: "GITLAB_TOKEN"                    # Environment variable for token
  owner: "your-org"                        # Default repository owner
  repo: "your-repo"                        # Default repository name

git:
  author:
    name: "Your Name"                      # Git commit author name
    email: "your.email@example.com"       # Git commit author email

# Repository cloning preferences
repository:
  default_type: "inmemory"                 # "inmemory" or "local"
  clone_depth: 1                          # Shallow clone depth
  single_branch: true                     # Clone single branch only
  include_tags: false                     # Include tags in clone
  recurse_submodules: false               # Recurse into submodules
```

### Environment Variables

The VCS component recognizes these environment variables:

```bash
# GitHub authentication
GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx     # GitHub personal access token
GITHUB_CLIENT_ID=Iv1.xxxxxxxxxxxxxxxx     # OAuth client ID for GitHub login

# SSH key configuration
GITHUB_KEY=~/.ssh/id_rsa                  # Path to SSH private key

# Git configuration
GIT_AUTHOR_NAME="Your Name"               # Git commit author name
GIT_AUTHOR_EMAIL="your.email@example.com" # Git commit author email

# Debug options
GTB_GIT_ENABLE_PROGRESS=1                 # Enable Git operation progress output
```

### Authentication Priority

The authentication mechanism follows this priority order:

1. **SSH Key Authentication**
 (if `github.ssh_key` is configured)
2. **Basic Authentication** (if `github.username` and `github.password` are configured)
3. **Token Authentication** (from `github.auth.value` or environment variable)
4. **OAuth Flow** (if `GITHUB_CLIENT_ID` is set and other methods fail)

### Repository Types

Choose the appropriate repository type based on your use case:

| Type | Use Case | Pros | Cons |
|------|----------|------|------|
| `inmemory` | Temporary operations, CI/CD | Fast, no disk I/O | Limited by memory |
| `local` | Persistent work, development | Full Git features | Disk space required |

### Clone Options

Configure cloning behavior based on requirements:

```go
// Minimal clone for CI/CD
repo.Clone(repoURL, destination,
    vcs.WithShallowClone(1),        // Only latest commit
    vcs.WithSingleBranch("main"),   // Only main branch
    vcs.WithNoTags(),               // Skip tags
)

// Full clone for development
repo.Clone(repoURL, destination,
    vcs.WithRecurseSubmodules(),    // Include submodules
)
```

### RepoLike Interface (For Testing and Mocking)

The `RepoLike` interface is primarily used for testing and when working with provided mocks. In production code, use the concrete `Repo` type:

```go
type RepoLike interface {
    SourceIs(int) bool
    SetSource(int)
    SetRepo(*git.Repository)
    GetRepo() *git.Repository
    SetKey(*ssh.PublicKeys)
    SetBasicAuth(string, string)
    GetAuth() transport.AuthMethod
    SetTree(*git.Worktree)
    GetTree() *git.Worktree
    Checkout(plumbing.ReferenceName) error
    CheckoutCommit(plumbing.Hash) error
    CreateBranch(string) error
    Push(*git.PushOptions) error
    Commit(string, *git.CommitOptions) (plumbing.Hash, error)

    OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    OpenLocal(string, string) (*git.Repository, *git.Worktree, error)
    Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
    Clone(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)

    // Git tree operations for in-memory repositories
    WalkTree(func(*object.File) error) error
    FileExists(string) (bool, error)
    DirectoryExists(string) (bool, error)
    GetFile(string) (*object.File, error)
    AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error
}
```

## Best Practices

### 1. Use Concrete Types in Production

- Use `*vcs.GHClient` for GitHub Enterprise API operations
- Use `*vcs.Repo` for Git repository management
- Reserve interfaces (`GitHubClient`, `RepoLike`) for testing and mocking

### 2. Authentication

- Use environment variables for sensitive tokens
- Support multiple authentication methods
- Provide clear error messages for authentication failures

### 3. Error Handling

- Wrap errors with appropriate context
- Handle network timeouts gracefully
- Provide retry logic for transient failures

### 4. Repository Strategy Selection

- Use `InMemoryRepo` for CI/CD pipelines and temporary operations
- Use `LocalRepo` for development and persistent operations
- Consider shallow clones for large repositories when appropriate

### 5. Performance Optimization

- Use clone options to reduce bandwidth and storage
- Implement proper cleanup for temporary repositories
- Cache authentication credentials appropriately

### 6. Security

- Validate repository URLs and paths
- Sanitize user input for Git operations
- Use secure defaults for repository creation

The Version Control component provides robust Git and GitHub integration capabilities that enable powerful repository management and automated update functionality in GTB applications.
