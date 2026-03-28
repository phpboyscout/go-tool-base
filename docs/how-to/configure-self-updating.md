---
title: Configure Self-Updating
description: How to wire up the auto-update command with GitHub or GitLab as the release source.
date: 2026-03-25
tags: [how-to, update, release, github, gitlab, self-update]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure Self-Updating

GTB's `UpdateCmd` feature lets your tool check for newer releases and replace its own binary. This guide covers how to wire it up with either GitHub or GitLab as the release backend.

The update system has two parts:
1. **`props.Tool.ReleaseSource`** — tells the framework *where* to find releases at compile time
2. **Config (`github` or `gitlab` subtree)** — provides the API token and endpoint at runtime

---

## Step 1: Populate `props.Tool` in `main.go`

The `Tool` struct is constructed once at startup and injected into `Props`. Fill in the `ReleaseSource` field:

```go
import (
    "github.com/phpboyscout/go-tool-base/pkg/props"
)

tool := props.Tool{
    Name:    "mytool",
    Summary: "My developer tool",
    ReleaseSource: props.ReleaseSource{
        Type:  "github",       // or "gitlab"
        Owner: "my-org",       // GitHub org / GitLab group
        Repo:  "mytool",       // repository name
    },
}
```

For private repositories, set `Private: true` — the framework will require a token and error early if none is found:

```go
ReleaseSource: props.ReleaseSource{
    Type:    "github",
    Owner:   "my-org",
    Repo:    "mytool",
    Private: true,
},
```

For a self-managed GitLab instance, also set `Host`:

```go
ReleaseSource: props.ReleaseSource{
    Type:  "gitlab",
    Host:  "gitlab.example.com",
    Owner: "my-group",
    Repo:  "mytool",
},
```

---

## Step 2: Ensure `UpdateCmd` is Enabled

`UpdateCmd` is enabled by default. If you previously disabled it, re-enable it via `SetFeatures`:

```go
tool.Features = props.SetFeatures(
    props.Enable(props.UpdateCmd),
)
```

To disable it (e.g. for internal tools distributed another way):

```go
tool.Features = props.SetFeatures(
    props.Disable(props.UpdateCmd),
)
```

---

## Step 3: Configure the Token

The framework reads token configuration from the relevant subtree of your config file. Add defaults to your embedded config asset (e.g. `assets/config/defaults.yaml`):

**For GitHub:**

```yaml
github:
  url:
    api: ""          # leave empty for github.com; set for GitHub Enterprise
    upload: ""
  auth:
    env: GITHUB_TOKEN   # environment variable to read
    value: ""           # or set a literal token here (not recommended for public repos)
```

**For GitLab:**

```yaml
gitlab:
  url:
    api: ""          # leave empty for gitlab.com; set for self-managed
  auth:
    env: GITLAB_TOKEN
    value: ""
```

Users then set the environment variable before running the update command:

```bash
export GITHUB_TOKEN=ghp_xxxxxxxxxxxx
mytool update
```

---

## Step 4: Build with `ldflags` Version Info

The update command compares the running version against the latest release tag. It needs `Version` to be set at build time via ldflags. In your `goreleaser.yaml` or `Makefile`:

```bash
go build -ldflags "-X main.version={{.Version}} -X main.commit={{.Commit}}" ./cmd/mytool
```

In `main.go`, wire the version into `Props`:

```go
var (
    version = "dev"
    commit  = "none"
)

func main() {
    p := &props.Props{
        Tool: tool,
        Version: props.Version{
            Version: version,
            Commit:  commit,
        },
    }
    // ...
}
```

When `version == "dev"`, the update check is automatically skipped — no API calls are made during local development.

---

## Step 5: Verify

Build a release binary and run:

```bash
mytool update --check   # check without applying
mytool update           # check and apply if newer version found
```

Expected output when up to date:

```
INFO  You are running the latest version  version=v1.2.3
```

Expected output when an update is available:

```
INFO  Update available  current=v1.2.3 latest=v1.3.0
INFO  Downloading update...
INFO  Update applied successfully. Restart to use the new version.
```

---

## Throttling

By default, the update check runs at most once per 24 hours (stored in the tool's config directory). This prevents hammering the API on every command invocation. The throttle interval is configurable:

```yaml
update:
  check_interval: 24h   # default; set to "0" to always check
```

---

## Air-Gapped / Offline Environments

For environments without network access, the update command supports installing from a local release archive. This workflow has three steps:

### 1. Download on a connected machine

Download the release archive and checksum from the GitHub/GitLab releases page:

```bash
# On a machine with internet access
curl -LO https://github.com/my-org/mytool/releases/download/v1.3.0/mytool_Linux_x86_64.tar.gz
curl -LO https://github.com/my-org/mytool/releases/download/v1.3.0/mytool_Linux_x86_64.tar.gz.sha256
```

### 2. Transfer to the target machine

Copy both files to the air-gapped machine via USB, SCP, or your internal artifact pipeline.

### 3. Install from file

```bash
mytool update --from-file /path/to/mytool_Linux_x86_64.tar.gz
```

The checksum sidecar is automatically detected and verified. No API token or network access is required.

The `--from-file` flag is mutually exclusive with `--version`. The `--force` flag has no effect on offline updates since no version comparison is performed.

---

## Related Documentation

- **[Auto-Update Lifecycle](../concepts/auto-update.md)** — how the update loop works
- **[GitHub component](../components/vcs/github.md)** — `NewGitHubClient` and token resolution
- **[GitLab component](../components/vcs/gitlab.md)** — `NewReleaseProvider` for GitLab
- **[Configuring Built-in Features](builtin-features.md)** — enabling and disabling UpdateCmd
