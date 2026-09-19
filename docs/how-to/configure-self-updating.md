---
title: Configure Self-Updating
description: How to wire up the auto-update command with GitHub, GitLab, Bitbucket, Gitea, Codeberg, or a direct HTTP server as the release source.
date: 2026-03-29
tags: [how-to, update, release, github, gitlab, bitbucket, gitea, codeberg, direct, self-update]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure Self-Updating

GTB's `UpdateCmd` feature lets your tool check for newer releases and replace its own binary. This guide covers how to wire it up with a release backend. Six built-in source types are available out of the box: `github`, `gitlab`, `bitbucket`, `gitea`, `codeberg`, and `direct`. For anything else, see [Add a Custom Release Source](custom-release-source.md).

The update system has two parts:
1. **`props.Tool.ReleaseSource`**: tells the framework *where* to find releases at compile time
2. **Config (provider-specific subtree)**: provides the API token and endpoint at runtime

---

## Step 1: Populate `props.Tool` in `main.go`

The `Tool` struct is constructed once at startup and injected into `Props`. Fill in the `ReleaseSource` field:

```go
import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

tool := props.Tool{
    Name:    "mytool",
    Summary: "My developer tool",
    ReleaseSource: props.ReleaseSource{
        Type:  "github",   // see supported types below
        Owner: "my-org",
        Repo:  "mytool",
    },
}
```

For private repositories, set `Private: true`. The framework will require a token and error early if none is found:

```go
ReleaseSource: props.ReleaseSource{
    Type:    "github",
    Owner:   "my-org",
    Repo:    "mytool",
    Private: true,
},
```

### Supported source types

| `Type` | Platform | Notes |
|---|---|---|
| `"github"` | GitHub / GitHub Enterprise | Set `Host` for GHE instances |
| `"gitlab"` | GitLab / self-managed | Set `Host` for self-managed GitLab |
| `"bitbucket"` | Bitbucket Cloud Downloads | Version inferred from asset filenames |
| `"gitea"` | Gitea / Forgejo | `Host` is required |
| `"codeberg"` | Codeberg (Forgejo) | `Host` defaults to `https://codeberg.org` |
| `"static"` | The static release channel: any https location the release publishes a pointer and per-tag manifests under, no forge involved | `BaseURL` is the one setting; see [below](#the-static-release-channel) |
| `"direct"` | Arbitrary HTTP / S3 / CDN through go/forge's direct provider | URL template required in configuration |

For a self-managed GitLab instance, also set `Host`:

```go
ReleaseSource: props.ReleaseSource{
    Type:  "gitlab",
    Host:  "gitlab.example.com",
    Owner: "my-group",
    Repo:  "mytool",
},
```

For a Gitea instance:

```go
ReleaseSource: props.ReleaseSource{
    Type:  "gitea",
    Host:  "https://git.example.com",
    Owner: "my-org",
    Repo:  "mytool",
},
```

For a direct download server, the release source names only the connection:

```go
ReleaseSource: props.ReleaseSource{
    Type: "direct",
    Repo: "mytool",
},
```

Its URL templates are configuration, under the `direct` subtree:

```yaml
direct:
  url_template: https://dl.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}
  version_url: https://dl.example.com/latest.json
```

They live in configuration rather than in a struct field so that two sources of
one type can coexist, each reading its own subtree. These were previously a
`ReleaseSource.Params` map; see the
[migration note](../reference/migration/v0.x-release-source-params-removed.md).

See the [Release Provider component](https://forge.go.phpboyscout.uk/reference/providers/) for the configuration keys each provider reads.

### The static release channel

A tool need not read a forge at all. On the static channel ([spec 0203](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0203-the-static-release-channel)) the release publishes two small JSON documents beside the binaries: `<base>/latest.json`, a pointer naming the current tag and its manifest, and `<base>/<tag>/release.json`, a manifest listing that release's archives with their platform, size and SHA-256, its `checksums.txt` and signature, and the tag before it. The tool reads the pointer, follows the manifest, picks its platform and verifies as it does on a forge. Nothing else is configured, and no credential is read: a public location is the design.

```go
ReleaseSource: props.ReleaseSource{
    Type:    props.ReleaseSourceStatic,
    BaseURL: "https://pkg.example.org/acme/mytool",
},
```

The base URL must be `https`, carry no userinfo and no query, and not be a placeholder host such as `example.com`. gtb itself updates this way from `https://pkg.phpboyscout.uk/go-tool-base`.

A generated project chooses the channel on the wizard's self-update page ("A static location", then the location on the page after) or with `--release-channel static --release-base-url <url>`; a project that is not hosted on a forge (`--no-forge`) can self-update this way and no other. The release configuration then publishes the two documents for you; [Secure releases](secure-releases.md#publishing-on-the-static-channel) says what that needs in CI, and the [layout reference](../reference/static-release-channel.md) is the contract any other publisher can meet.

On this channel `update` is also exempt from two gates that would stop a broken install helping itself: the unlinked-forge check and the missing-config gate. A tool whose forge adapters or configuration are broken can still pull its own fix.

To check a location by hand:

```bash
curl -fsS https://pkg.example.org/acme/mytool/latest.json          # the pointer: tag and manifest URL
curl -fsS https://pkg.example.org/acme/mytool/v1.2.0/release.json  # the manifest it names
curl -fsS https://pkg.example.org/acme/mytool/v1.2.0/checksums.txt # the digests the manifest repeats
mytool doctor                                                     # the Release source check reads the pointer and names the current tag
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
        // Props.Version is a version.Info; NewInfo normalises the v prefix.
        Version: version.NewInfo(version, commit, ""),
    }
    // ...
}
```

For a development build (a non-semver version, or one containing `-dev`/`-dirty`, `version.IsDevelopment()`), the update check is automatically skipped, so no API calls are made during local development.

---

## Step 5: Verify

Build a release binary and run:

```bash
mytool update            # check and apply if a newer version is found
mytool update --version 1.2.3   # update to a specific version
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

!!! warning "Interaction with `require_*` flags"
    The offline path enforces the same verification contract as the online path:

    - **`update.require_checksum: true`**: the install **fails** if the `.sha256` sidecar is missing. Always transfer both files.
    - **`update.require_signature: true`**: the offline path **cannot** verify an OpenPGP signature (there is no manifest+signature flow for a local archive), so it **refuses** the update. Use the online path for signature-verified updates, or disable `require_signature` for air-gapped installs.

---

## Related Documentation

- **[Auto-Update Lifecycle](../explanation/components/update.md)**: how the update loop works
- **[Release Provider component](https://forge.go.phpboyscout.uk/reference/providers/)**: all built-in providers, registry API, and per-provider configuration keys
- **[Add a Custom Release Source](custom-release-source.md)**: register your own provider for any backend
- **[GitHub provider](https://gitlab.com/phpboyscout/go/forge-github)**: GitHub release provider and token resolution (external `go/forge-github` module)
- **[GitLab component](https://forge.go.phpboyscout.uk/reference/providers/#gitlab)**: `NewReleaseProvider` for GitLab
- **[Configuring Built-in Features](builtin-features.md)**: enabling and disabling UpdateCmd
