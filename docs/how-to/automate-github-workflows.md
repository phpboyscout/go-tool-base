---
title: Automate GitHub Workflows
description: How to use GHClient to create pull requests, manage labels, download release assets, and read file contents.
date: 2026-03-25
tags: [how-to, github, vcs, pull-requests, releases, automation]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Automate GitHub Workflows

GTB's `pkg/vcs/github` package wraps the GitHub API behind a testable interface. This guide covers the four most common automation patterns: creating pull requests, downloading release assets, reading file contents, and uploading SSH keys.

---

## Prerequisites

### Configuration

Add a `github` section to your embedded defaults (`assets/config/defaults.yaml`):

```yaml
github:
  url:
    api: ""      # empty = github.com; set for GitHub Enterprise
    upload: ""
  auth:
    env: GITHUB_TOKEN
    value: ""
```

Users set `GITHUB_TOKEN` in their environment:

```bash
export GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx
```

### Creating the Client

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/github"

client, err := github.NewGitHubClient(props.Config.Sub("github"))
if err != nil {
    return err
}
```

Pass the `github` config subtree — `NewGitHubClient` reads `url.api`, `url.upload`, and resolves the token automatically.

---

## Creating a Pull Request

```go
import gogithub "github.com/google/go-github/v80/github"

pr, err := client.CreatePullRequest(ctx, "my-org", "my-repo", &gogithub.NewPullRequest{
    Title: gogithub.Ptr("feat: add new command"),
    Head:  gogithub.Ptr("feature/new-command"),   // source branch
    Base:  gogithub.Ptr("main"),                   // target branch
    Body:  gogithub.Ptr("Automated PR from mytool.\n\n## Changes\n- ..."),
    Draft: gogithub.Ptr(false),
})
if err != nil {
    return errorhandling.WrapWithHint(err,
        "failed to create pull request",
        "Check that the branch exists and the token has 'repo' scope.")
}

props.Logger.Info("Pull request created",
    "number", pr.GetNumber(),
    "url", pr.GetHTMLURL(),
)
```

---

## Adding Labels to a Pull Request

```go
labels := []string{"automated", "feat"}
if err := client.AddLabelsToPullRequest(ctx, "my-org", "my-repo", pr.GetNumber(), labels); err != nil {
    // Non-fatal — log and continue
    props.Logger.Warn("Failed to add labels", "error", err)
}
```

---

## Finding an Existing Pull Request

Before creating a PR, check if one already exists for the branch:

```go
existing, err := client.GetPullRequestByBranch(ctx, "my-org", "my-repo", "feature/new-command", "open")
if err != nil {
    if !errors.Is(err, github.ErrNoPullRequestFound) {
        return err
    }
    // No existing PR — create one
    existing, err = client.CreatePullRequest(ctx, "my-org", "my-repo", newPR)
    if err != nil {
        return err
    }
}

props.Logger.Info("Working with PR", "number", existing.GetNumber())
```

---

## Updating an Existing Pull Request

```go
updated, _, err := client.UpdatePullRequest(ctx, "my-org", "my-repo", pr.GetNumber(), &gogithub.PullRequest{
    Title: gogithub.Ptr("feat: add new command (updated)"),
    Body:  gogithub.Ptr("Updated description."),
})
```

---

## Downloading a Release Asset

The lower-level `GHClient` methods give you direct access to asset IDs. Use this pattern for downloading binaries, archives, or data files bundled with a release:

```go
// Step 1: find the asset by name in a specific release tag
assetID, err := client.GetReleaseAssetID(ctx, "my-org", "my-repo", "v1.2.0", "mytool_linux_amd64.tar.gz")
if err != nil {
    return err
}

// Step 2: download directly into the afero filesystem
destPath := "/tmp/mytool_linux_amd64.tar.gz"
if err := client.DownloadAssetTo(ctx, props.FS, "my-org", "my-repo", assetID, destPath); err != nil {
    return err
}

props.Logger.Info("Asset downloaded", "path", destPath)
```

`DownloadAssetTo` writes into `props.FS` — use `afero.NewMemMapFs()` in tests to avoid touching disk.

If you need to stream the asset (e.g. to extract it on the fly):

```go
rc, err := client.DownloadAsset(ctx, "my-org", "my-repo", assetID)
if err != nil {
    return err
}
defer rc.Close()

// Stream directly to stdout or into an archive reader
io.Copy(os.Stdout, rc)
```

---

## Reading File Contents from a Repository

Retrieve a file at a specific branch or commit without cloning the repository:

```go
content, err := client.GetFileContents(ctx, "my-org", "my-repo", "config/schema.json", "main")
if err != nil {
    return err
}

// content is the decoded file content as a string
var schema map[string]any
json.Unmarshal([]byte(content), &schema)
```

---

## Creating a Repository

```go
repo, err := client.CreateRepo(ctx, "my-org", "new-repo-name")
if err != nil {
    if errors.Is(err, github.ErrRepoExists) {
        props.Logger.Info("Repository already exists", "repo", "new-repo-name")
    } else {
        return err
    }
}
```

---

## Uploading an SSH Key

Used by the GitHub initialiser to register a deploy key:

```go
pubKeyBytes, err := os.ReadFile("~/.ssh/id_ed25519.pub")
if err != nil {
    return err
}

if err := client.UploadKey(ctx, "mytool-deploy-key", pubKeyBytes); err != nil {
    return err
}
```

---

## Using the Release Provider Instead

For standard release-browsing workflows (auto-update, version checking), prefer `github.NewReleaseProvider` over the raw `GHClient` methods. It returns a backend-agnostic `release.Provider` that can be swapped for GitLab:

```go
provider := github.NewReleaseProvider(client)

latest, err := provider.GetLatestRelease(ctx, "my-org", "my-repo")
fmt.Println("Latest:", latest.GetTagName())
```

See **[Release Provider](../components/vcs/release.md)** for the full interface.

---

## Testing

Use the generated mock to test commands that depend on `GHClient`:

```go
import mock_github "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/vcs/github"

mockClient := mock_github.NewMockGitHubClient(t)
mockClient.EXPECT().
    GetPullRequestByBranch(mock.Anything, "my-org", "my-repo", "feature/x", "open").
    Return(nil, github.ErrNoPullRequestFound)

mockClient.EXPECT().
    CreatePullRequest(mock.Anything, "my-org", "my-repo", mock.Anything).
    Return(&gogithub.PullRequest{Number: gogithub.Ptr(42)}, nil)

mockClient.EXPECT().
    AddLabelsToPullRequest(mock.Anything, "my-org", "my-repo", 42, mock.Anything).
    Return(nil)
```

For HTTP-level tests, stand up a `net/http/httptest` server and pass its URL as `url.api` in the test config. See `pkg/vcs/github/client_coverage_test.go` for the established pattern.

---

## Related Documentation

- **[GitHub component](../components/vcs/github.md)** — full `GitHubClient` interface reference
- **[Release Provider](../components/vcs/release.md)** — backend-agnostic release interface
- **[Configure Self-Updating](configure-self-updating.md)** — wiring the release provider into the update command
