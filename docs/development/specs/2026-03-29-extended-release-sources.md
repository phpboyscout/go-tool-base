---
title: "Extended Release Sources Specification"
description: "Expand pkg/setup and pkg/vcs to support additional release source providers — Bitbucket, Gitea/Forgejo, Codeberg, and direct HTTP download — plus a provider registry that allows downstream consumers to register custom sources."
date: 2026-03-29
status: IMPLEMENTED
tags:
  - specification
  - setup
  - vcs
  - release
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Sonnet 4.6
    role: AI drafting assistant
---

# Extended Release Sources Specification

Authors
:   Matt Cockayne, Claude Sonnet 4.6 *(AI drafting assistant)*

Date
:   29 March 2026

Status
:   IMPLEMENTED

---

## Overview

GTB tools currently resolve self-update releases from two sources: GitHub (including GitHub Enterprise) and GitLab (including self-hosted instances). The `ReleaseSource.Type` string is a hard-coded switch inside `pkg/setup/update.go`, which means adding a new source requires modifying core library code.

This specification:

1. **Introduces a provider registry** — a compile-time extensibility mechanism in `pkg/vcs/release` that lets downstream consumers register custom `release.Provider` implementations without forking the library.
2. **Adds Bitbucket Cloud** as a built-in provider, using the Bitbucket Downloads API with filename-pattern-based version detection.
3. **Adds Gitea/Forgejo** as a built-in provider, leveraging Gitea's GitHub-compatible releases API.
4. **Adds Codeberg** as a first-class provider — a distinct `type: codeberg` backed by the Gitea provider with `codeberg.org` pre-configured.
5. **Adds a direct HTTP download** provider for tools hosted on arbitrary HTTP servers (S3 buckets, internal mirrors, CDNs) where no VCS platform manages releases.
6. **Extends `ReleaseSource`** with a `Params` field for provider-specific configuration without breaking the existing struct.

---

## Motivation and Context

### Current Limitations

- The `NewUpdater` function in `pkg/setup/update.go` contains a hard `if vcsProvider == "gitlab"` branch. Any new provider requires modifying this function.
- The `ReleaseSource` struct has no extensibility point — provider-specific fields (URL templates, API versions) cannot be expressed.
- Tools distributed via Bitbucket, Gitea, Forgejo, Codeberg, internal S3 buckets, or other HTTP hosts cannot use GTB's self-update machinery at all.

### Target Audience for New Sources

| Provider | Use Case |
|----------|----------|
| Bitbucket Cloud | Teams using Bitbucket for source control, distributing binaries via Bitbucket Downloads |
| Gitea / Forgejo | Self-hosted Git (popular in corporate environments and the open-source community) |
| Codeberg | Public Forgejo instance at codeberg.org; growing traction in the open-source community |
| Direct HTTP | Teams using S3, GCS, Azure Blob, Artifactory, Nexus, or a static web server as a release host |

---

## Design Decisions

**Provider registry over interface injection**: A global registry (`vcs/release`) is the simplest extensibility mechanism that does not require changes to `Props`, constructors, or command wiring. Downstream consumers call `release.Register(...)` once at startup (in `main.go`) to add custom providers. The registry uses a `sync.RWMutex` — written once at startup (during `init()` calls and any custom registration in `main()`), then read-only thereafter. This is idiomatic and avoids the complexity of a sealed/panic-on-late-register approach.

**Built-in providers remain zero-configuration**: All built-in providers register themselves via `init()` in their respective packages and are pulled in by blank imports in `pkg/setup/providers.go`. `NewUpdater` becomes a uniform registry lookup.

**`ReleaseSource.Params` for provider-specific configuration**: A `map[string]string` field is the lowest-friction extension point. Keys use `snake_case` throughout, consistent with the existing Viper-based config system. Providers document their recognised param keys. The field is `omitempty` in JSON and YAML so tools that don't use it produce identical serialised output to today.

**`vcs.provider` config override continues to work**: The runtime config key `vcs.provider` can override `ReleaseSource.Type` for all providers, including new ones. This is useful for operators who want to redirect a binary to a different host at runtime (e.g. a private mirror) without recompilation.

**Bitbucket uses Downloads, not Releases**: Bitbucket Cloud has no native "Releases" concept — only a flat Downloads list. Version is inferred from filename using a configurable regex pattern. The default pattern matches GoReleaser's naming convention (`{tool}_{OS}_{Arch}.tar.gz`) and extracts a version segment when present (e.g. `tool_v1.2.3_Linux_x86_64.tar.gz`). Engineers can override the pattern via `Params["filename_pattern"]`. Assets are sorted by upload date (`created_on`) descending; the most recent matching set is treated as the latest release. `GetReleaseByTag` and `ListReleases` return `ErrNotSupported`.

**Codeberg is a first-class provider**: Codeberg runs Forgejo and is at `codeberg.org`. Rather than expecting users to set `type: gitea` + `host: codeberg.org`, a dedicated `type: codeberg` is registered that pre-configures the Gitea/Forgejo provider with the correct host. This is a distinct registry entry backed by the same `GiteaReleaseProvider` implementation.

**Direct download version endpoint formats**: The `version_url` may serve any of four formats — plain text, JSON, YAML, or XML. The provider auto-detects based on `Content-Type` response header; the `version_format` param can override detection. For JSON, YAML, and XML, a configurable `version_key` param specifies the field to extract (default: tries `tag_name` then `version`). This gives consumers the broadest compatibility with existing version endpoints.

**No Bitbucket Server / Bitbucket Data Center in Phase 1**: Bitbucket Server has a different API and is largely superseded. Noted as a future consideration.

---

## Public API Changes

### New Constants in `pkg/vcs/release`

```go
const (
    SourceTypeGitHub    = "github"
    SourceTypeGitLab    = "gitlab"
    SourceTypeBitbucket = "bitbucket"
    SourceTypeGitea     = "gitea"
    SourceTypeCodeberg  = "codeberg"
    SourceTypeDirect    = "direct"
)
```

These are informational constants; any string is accepted by the registry. Downstream consumers can define their own.

### Provider Registry — `pkg/vcs/release/registry.go`

```go
// ProviderFactory is a function that constructs a release.Provider from a
// ReleaseSourceConfig and a Viper configuration subtree.
type ProviderFactory func(source ReleaseSourceConfig, cfg config.Containable) (Provider, error)

// Register associates a source type string with a factory function.
// Safe to call concurrently; uses a sync.RWMutex internally.
// Intended to be called from init() or early in main() before any Lookup call.
func Register(sourceType string, factory ProviderFactory)

// Lookup returns the ProviderFactory for the given source type.
// Returns ErrProviderNotFound if no factory is registered for that type.
func Lookup(sourceType string) (ProviderFactory, error)

// RegisteredTypes returns a sorted slice of all registered source type strings.
// Used for generating user-facing error messages.
func RegisteredTypes() []string
```

### `ReleaseSourceConfig` — `pkg/vcs/release/source_config.go`

Rather than passing `props.ReleaseSource` directly (to avoid a circular import between `pkg/vcs/release` and `pkg/props`), a lightweight config struct is defined in `pkg/vcs/release`:

```go
// ReleaseSourceConfig carries the information a ProviderFactory needs to
// construct its client. It is populated from props.ReleaseSource.
type ReleaseSourceConfig struct {
    Type    string
    Host    string
    Owner   string
    Repo    string
    Private bool
    Params  map[string]string
}
```

### `props.ReleaseSource` Extension

```go
type ReleaseSource struct {
    Type    string            `json:"type"    yaml:"type"`
    Host    string            `json:"host"    yaml:"host"`
    Owner   string            `json:"owner"   yaml:"owner"`
    Repo    string            `json:"repo"    yaml:"repo"`
    Private bool              `json:"private" yaml:"private"`
    // Params holds provider-specific configuration key/value pairs.
    // Keys use snake_case. Valid keys are documented per provider.
    Params  map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
}
```

No existing fields are changed; `Params` is additive and omitted when empty.

### `pkg/setup/update.go` — `NewUpdater` Refactor

The hard `if/else` switch is replaced with a registry lookup:

```go
func NewUpdater(props *props.Props, version string, force bool) (*SelfUpdater, error) {
    if props.Config == nil {
        return nil, errors.New("configuration is not loaded")
    }

    vcsProvider, _, _ := props.Tool.GetReleaseSource()
    if props.Config.IsSet("vcs.provider") {
        vcsProvider = strings.ToLower(props.Config.GetString("vcs.provider"))
    }

    if props.Tool.ReleaseSource.Private {
        if err := requireReleaseToken(vcsProvider, props); err != nil {
            return nil, err
        }
    }

    factory, err := release.Lookup(vcsProvider)
    if err != nil {
        return nil, errors.WithHintf(err,
            "Supported release source types: %s. Register a custom provider with release.Register().",
            strings.Join(release.RegisteredTypes(), ", "),
        )
    }

    sourceCfg := release.ReleaseSourceConfig{
        Type:    props.Tool.ReleaseSource.Type,
        Host:    props.Tool.ReleaseSource.Host,
        Owner:   props.Tool.ReleaseSource.Owner,
        Repo:    props.Tool.ReleaseSource.Repo,
        Private: props.Tool.ReleaseSource.Private,
        Params:  props.Tool.ReleaseSource.Params,
    }

    releaseClient, err := factory(sourceCfg, props.Config)
    if err != nil {
        return nil, errors.WithStack(err)
    }

    return &SelfUpdater{
        force:          force,
        version:        version,
        logger:         props.Logger,
        Tool:           props.Tool,
        releaseClient:  releaseClient,
        CurrentVersion: ver.FormatVersionString(props.Version.GetVersion(), true),
        Fs:             props.FS,
    }, nil
}
```

---

## New Providers

### Bitbucket Cloud — `pkg/vcs/bitbucket/`

**API**: Bitbucket Downloads API v2 (`https://api.bitbucket.org/2.0/repositories/{workspace}/{slug}/downloads`)

Bitbucket Cloud has no "Releases" concept. Binary artefacts are uploaded as flat Downloads associated with a repository. Version information is not stored by the platform and must be inferred from filenames.

**Authentication**: HTTP Basic auth with an App Password (`username:app_password`). The `Private` flag in `ReleaseSource` governs whether credentials are required.

**Token resolution** (in order of precedence):
1. `cfg.bitbucket.username` + `cfg.bitbucket.app_password` (config file)
2. `BITBUCKET_USERNAME` + `BITBUCKET_APP_PASSWORD` environment variables

**Version detection via filename pattern**:

The provider applies a regex to each Download's filename to identify matching artefacts and extract an optional version segment. The default pattern matches GoReleaser's output:

```
^{tool}(?:_v?(\d+\.\d+\.\d+[^_]*))?_{OS}_{Arch}\.tar\.gz$
```

- The version capture group is optional — artefacts without a version segment (e.g. `tool_Linux_x86_64.tar.gz`) are matched but reported with an empty version.
- Engineers can override the pattern via `Params["filename_pattern"]` (a Go `regexp` string). The first capture group, if present, is treated as the version.
- Matching artefacts are sorted by `created_on` descending. The most recent matching set constitutes the "latest release".
- `GetLatestRelease` returns a synthetic `Release` with `TagName` set to the extracted version (or the `created_on` timestamp in ISO 8601 if no version was captured).
- `GetReleaseByTag` and `ListReleases` return `ErrNotSupported`.

**`Params` keys**:

| Key | Description | Default |
|-----|-------------|---------|
| `workspace` | Bitbucket workspace slug (if different from `Owner`) | same as `Owner` |
| `filename_pattern` | Go regex for asset matching. First capture group = version. | default GoReleaser pattern |

**Asset matching**: The provider returns all Downloads that match the filename pattern for the current platform (`{OS}_{Arch}`), regardless of version segment.

### Gitea / Forgejo — `pkg/vcs/gitea/`

**API**: Gitea/Forgejo REST API v1 (`{host}/api/v1/repos/{owner}/{repo}/releases`)

The Gitea API mirrors GitHub's releases endpoint in structure, but uses different field names and does not issue CDN redirects on asset downloads. A dedicated implementation avoids coupling to the GitHub client.

**Authentication**: Bearer token via `Authorization: token <value>` header.

**Token resolution** (in order of precedence):
1. `cfg.gitea.token` (config file)
2. `GITEA_TOKEN` environment variable

**`Host` field**: Required — Gitea/Forgejo instances have no shared public host. The `Host` value is the full base URL (e.g. `https://git.example.com`).

**`Params` keys**:

| Key | Description | Default |
|-----|-------------|---------|
| `api_version` | API path version segment | `v1` |

### Codeberg — first-class type backed by `pkg/vcs/gitea/`

Codeberg (`https://codeberg.org`) is a public Forgejo instance with growing adoption in the open-source community. GTB registers `SourceTypeCodeberg = "codeberg"` as a distinct provider type. The factory pre-configures `GiteaReleaseProvider` with `Host: "https://codeberg.org"` — no `Host` field is required in `ReleaseSource` for Codeberg repositories.

**Token resolution**:
1. `cfg.codeberg.token` (config file)
2. `CODEBERG_TOKEN` environment variable

**`Params` keys**: Same as Gitea.

**Example configuration**:
```go
props.Tool{
    ReleaseSource: props.ReleaseSource{
        Type:  "codeberg",
        Owner: "myorg",
        Repo:  "mytool",
    },
}
```

### Direct HTTP Download — `pkg/vcs/direct/`

For tools distributed via arbitrary HTTP servers. The provider constructs asset download URLs from a configurable template and optionally fetches the latest version from a version endpoint.

**`Params` keys**:

| Key | Description | Required |
|-----|-------------|----------|
| `url_template` | Download URL template. Supported placeholders listed below. | Yes |
| `version_url` | URL that returns the latest version string. | No |
| `version_format` | Override format detection: `text`, `json`, `yaml`, or `xml`. | No (auto-detected) |
| `version_key` | Field name to extract from structured responses. Tried as-is, then dot-separated path for nested fields. Default: tries `tag_name` then `version`. | No |
| `pinned_version` | Static version string. Disables update checking (no network call). | No |
| `checksum_url_template` | Template for the checksum sidecar URL. Same placeholders as `url_template`. | No |

**URL template placeholders**:

| Placeholder | Value | Example |
|------------|-------|---------|
| `{version}` | Full version string | `v1.2.3` |
| `{version_bare}` | Version without leading `v` | `1.2.3` |
| `{os}` | Title-cased OS (GoReleaser convention) | `Linux`, `Darwin`, `Windows` |
| `{arch}` | Architecture (GoReleaser convention) | `x86_64`, `arm64` |
| `{tool}` | Tool name from `props.Tool.Name` | `mytool` |
| `{ext}` | Archive extension | `tar.gz` |

**Example configuration**:
```yaml
release_source:
  type: direct
  params:
    url_template: "https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}"
    version_url: "https://releases.example.com/latest.json"
    version_key: "version"
    checksum_url_template: "https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}.sha256"
```

**Version endpoint response formats**:

The provider supports four response formats, auto-detected from the `Content-Type` header (or overridden via `version_format`):

| Format | `Content-Type` | Detection | Extraction |
|--------|---------------|-----------|------------|
| Plain text | `text/plain` | Default if no structured type matches | Entire body, whitespace-trimmed |
| JSON | `application/json` | `application/json` | Value at `version_key` (dot-separated path for nested keys) |
| YAML | `application/yaml`, `text/yaml` | Either YAML content-type | Value at `version_key` |
| XML | `application/xml`, `text/xml` | Either XML content-type | Text content of the element matching `version_key` |

When a structured format is detected but `version_key` is not set, the provider tries `tag_name` then `version` as fallbacks before returning an error.

**Example version endpoint responses**:

```
# Plain text
v1.2.3

# JSON
{"tag_name": "v1.2.3", "prerelease": false}

# YAML
version: v1.2.3
released_at: 2026-03-29

# XML
<release>
  <version>v1.2.3</version>
</release>
```

**Behaviour when version is unavailable**:
- `version_url` absent + `pinned_version` set: `IsLatestVersion` returns `true` (no network call).
- Both absent: `GetLatestRelease` returns `ErrVersionUnknown`. The update command advises the user to specify `--version` explicitly.

**`GetReleaseByTag`**: Constructs a synthetic release using the provided tag as the version. No network call.

**`ListReleases`**: Returns `ErrNotSupported`.

**Authentication**: Bearer token for authenticated endpoints.
- `cfg.direct.token` (config file)
- `DIRECT_TOKEN` environment variable

---

## Project Structure

```
pkg/vcs/release/
├── provider.go          ← EXISTING: Release, ReleaseAsset, Provider interfaces
├── registry.go          ← NEW: Register, Lookup, RegisteredTypes (sync.RWMutex)
├── registry_test.go     ← NEW: registry unit tests
├── source_config.go     ← NEW: ReleaseSourceConfig type
├── constants.go         ← NEW: SourceType* constants (github, gitlab, bitbucket, gitea, codeberg, direct)

pkg/vcs/bitbucket/
├── client.go            ← NEW: HTTP client with Basic auth
├── release.go           ← NEW: BitbucketRelease, BitbucketAsset, BitbucketReleaseProvider, filename pattern matching
├── release_test.go      ← NEW: unit tests with mock HTTP
├── release_integration_test.go  ← NEW: integration tests (INT_TEST_BITBUCKET=1)
├── init.go              ← NEW: func init() { release.Register(release.SourceTypeBitbucket, factory) }

pkg/vcs/gitea/
├── client.go            ← NEW: HTTP client with token auth
├── release.go           ← NEW: GiteaRelease, GiteaAsset, GiteaReleaseProvider
├── release_test.go      ← NEW: unit tests with mock HTTP
├── release_integration_test.go  ← NEW: integration tests (INT_TEST_GITEA=1)
├── init.go              ← NEW: register "gitea" and "codeberg" factories

pkg/vcs/direct/
├── provider.go          ← NEW: DirectReleaseProvider, URL template expansion, version endpoint parsing
├── version.go           ← NEW: version endpoint fetch + format parsing (text/JSON/YAML/XML)
├── provider_test.go     ← NEW: unit tests
├── version_test.go      ← NEW: version format parsing tests
├── init.go              ← NEW: func init() { release.Register(release.SourceTypeDirect, factory) }

pkg/props/
├── tool.go              ← MODIFIED: add Params field to ReleaseSource

pkg/setup/
├── update.go            ← MODIFIED: replace if/else with registry lookup; extend requireReleaseToken
├── update_test.go       ← MODIFIED: registry-driven tests
├── providers.go         ← NEW: blank imports to register all built-in providers

docs/components/
├── setup.md             ← MODIFIED: document new providers and Params config
├── vcs.md               ← MODIFIED: document registry, new packages
```

### Built-in Provider Registration — `pkg/setup/providers.go`

```go
package setup

import (
    _ "github.com/phpboyscout/go-tool-base/pkg/vcs/bitbucket"
    _ "github.com/phpboyscout/go-tool-base/pkg/vcs/direct"
    _ "github.com/phpboyscout/go-tool-base/pkg/vcs/gitea"
    _ "github.com/phpboyscout/go-tool-base/pkg/vcs/github"
    _ "github.com/phpboyscout/go-tool-base/pkg/vcs/gitlab"
)
```

All built-in providers are registered when `pkg/setup` is imported. Downstream consumers that want to add a custom provider call `release.Register(...)` in their `main()` before invoking any setup functions.

---

## Error Handling

All new providers use `github.com/cockroachdb/errors` for all error creation and wrapping.

New sentinel errors in `pkg/vcs/release`:

```go
var (
    // ErrProviderNotFound is returned when Lookup cannot find a factory for the given type.
    ErrProviderNotFound = errors.New("no release provider registered for source type")

    // ErrNotSupported is returned by provider methods that are not applicable
    // for the underlying platform (e.g. ListReleases on Bitbucket).
    ErrNotSupported = errors.New("operation not supported by this release provider")

    // ErrVersionUnknown is returned by the direct provider when neither version_url
    // nor pinned_version is configured and a version check is requested.
    ErrVersionUnknown = errors.New("cannot determine latest version: configure version_url or pinned_version in Params")
)
```

User-facing hints (via `errors.WithHint`) are provided for:
- `ErrProviderNotFound`: lists all registered type strings.
- Authentication failures: names the specific environment variable to set.
- Template expansion failures: includes the template string and the unresolvable placeholder.
- Version format parsing failures: names the detected/configured format and the key attempted.

---

## Testing Strategy

### Unit Tests

All new providers use `httptest.NewServer` for HTTP interactions. No real network calls in unit tests.

| Test | Package | Scenario |
|------|---------|----------|
| `TestRegistry_Register` | `vcs/release` | Register factory → Lookup returns it |
| `TestRegistry_Lookup_NotFound` | `vcs/release` | Lookup unregistered type → `ErrProviderNotFound` |
| `TestRegistry_RegisteredTypes` | `vcs/release` | Returns sorted list including all built-in types |
| `TestRegistry_Concurrent` | `vcs/release` | Concurrent Register + Lookup → no data race |
| `TestBitbucketProvider_GetLatestRelease_WithVersion` | `vcs/bitbucket` | Filename contains version → version extracted |
| `TestBitbucketProvider_GetLatestRelease_NoVersion` | `vcs/bitbucket` | Filename without version → TagName is creation timestamp |
| `TestBitbucketProvider_GetLatestRelease_CustomPattern` | `vcs/bitbucket` | Custom `filename_pattern` in Params → applied correctly |
| `TestBitbucketProvider_DownloadAsset` | `vcs/bitbucket` | Asset URL → bytes streamed |
| `TestBitbucketProvider_GetReleaseByTag` | `vcs/bitbucket` | Returns `ErrNotSupported` |
| `TestBitbucketProvider_ListReleases` | `vcs/bitbucket` | Returns `ErrNotSupported` |
| `TestBitbucketProvider_Auth` | `vcs/bitbucket` | Basic auth header sent when Private=true |
| `TestGiteaProvider_GetLatestRelease` | `vcs/gitea` | Standard release JSON → fields mapped correctly |
| `TestGiteaProvider_GetReleaseByTag` | `vcs/gitea` | Tag → correct endpoint called |
| `TestGiteaProvider_ListReleases` | `vcs/gitea` | Pagination → all releases returned up to limit |
| `TestGiteaProvider_DownloadAsset` | `vcs/gitea` | Streaming download |
| `TestGiteaProvider_Codeberg_DefaultHost` | `vcs/gitea` | `SourceTypeCodeberg` → requests go to codeberg.org |
| `TestGiteaProvider_Codeberg_TokenEnvVar` | `vcs/gitea` | `CODEBERG_TOKEN` used for auth |
| `TestDirectProvider_VersionURL_JSON` | `vcs/direct` | JSON response + version_key → correct version |
| `TestDirectProvider_VersionURL_YAML` | `vcs/direct` | YAML response + version_key → correct version |
| `TestDirectProvider_VersionURL_XML` | `vcs/direct` | XML response + version_key → correct version |
| `TestDirectProvider_VersionURL_PlainText` | `vcs/direct` | Plain text response → trimmed version |
| `TestDirectProvider_VersionURL_AutoDetect` | `vcs/direct` | Content-Type drives format selection |
| `TestDirectProvider_VersionURL_FormatOverride` | `vcs/direct` | `version_format` param overrides Content-Type |
| `TestDirectProvider_VersionURL_FallbackKey` | `vcs/direct` | No `version_key` → tries `tag_name` then `version` |
| `TestDirectProvider_Pinned` | `vcs/direct` | `pinned_version` → no HTTP call, no update |
| `TestDirectProvider_VersionUnknown` | `vcs/direct` | No version config → `ErrVersionUnknown` |
| `TestDirectProvider_GetReleaseByTag` | `vcs/direct` | Synthetic release, no network call |
| `TestDirectProvider_URLTemplate_AllPlaceholders` | `vcs/direct` | All placeholders expanded correctly |
| `TestDirectProvider_DownloadAsset` | `vcs/direct` | Template expanded → asset downloaded |
| `TestDirectProvider_ChecksumURL` | `vcs/direct` | `checksum_url_template` fetched and verified |
| `TestNewUpdater_RegistryLookup_Unknown` | `setup` | Unknown type → error with hint listing registered types |
| `TestNewUpdater_Bitbucket` | `setup` | `type="bitbucket"` → `BitbucketReleaseProvider` created |
| `TestNewUpdater_Gitea` | `setup` | `type="gitea"` → `GiteaReleaseProvider` created |
| `TestNewUpdater_Codeberg` | `setup` | `type="codeberg"` → `GiteaReleaseProvider` at codeberg.org |
| `TestNewUpdater_Direct` | `setup` | `type="direct"` → `DirectReleaseProvider` created |

### Integration Tests

Gated by environment-variable tags following the existing pattern in `internal/testutil`.

| Tag | Env Var | What is tested |
|-----|---------|----------------|
| `bitbucket` | `INT_TEST_BITBUCKET=1` | Bitbucket Cloud Downloads API: list, match, download |
| `gitea` | `INT_TEST_GITEA=1` | A real Gitea/Forgejo instance (URL + token in env): releases, download |

### E2E BDD

The existing `update` command E2E scenarios cover provider-agnostic behaviour. No new Gherkin scenarios are required — new providers do not change command interface or user-visible output.

### Coverage

Target ≥90% for all new `pkg/vcs/*` packages and modified paths in `pkg/setup`.

---

## Migration and Compatibility

### GitHub and GitLab Providers

The existing `githubvcs.NewReleaseProvider` and `gitlabvcs.NewReleaseProvider` constructors are preserved as-is. Each gains a new `init.go` registering a factory wrapper:

```go
// pkg/vcs/github/init.go
func init() {
    release.Register(release.SourceTypeGitHub, func(src release.ReleaseSourceConfig, cfg config.Containable) (release.Provider, error) {
        client, err := NewGitHubClient(cfg.Sub("github"))
        if err != nil {
            return nil, err
        }
        return NewReleaseProvider(client), nil
    })
}
```

### `NewUpdater` Behaviour

The refactored `NewUpdater` is behaviourally identical for `type: "github"` and `type: "gitlab"`. The config override via `vcs.provider` continues to work for all provider types.

### `requireReleaseToken`

Extended with cases for the new providers:

```go
switch vcsProvider {
case "gitlab":
    fallbackEnv = "GITLAB_TOKEN"
case "bitbucket":
    // Bitbucket uses two env vars; handled separately in the Bitbucket factory.
    return nil // token presence check delegated to the provider
case "gitea":
    fallbackEnv = "GITEA_TOKEN"
case "codeberg":
    fallbackEnv = "CODEBERG_TOKEN"
case "direct":
    fallbackEnv = "DIRECT_TOKEN"
default:
    fallbackEnv = "GITHUB_TOKEN"
}
```

The Bitbucket case delegates credential presence checking to its factory (two separate vars: username + app_password), which returns a structured error if either is missing and `Private: true`.

### `props.ReleaseSource` Serialisation

The `Params` field uses `omitempty`. Tools that don't use it produce output identical to today.

### Generator Templates

`internal/generator/` templates that scaffold the `Tool` initialisation block are updated to include an optional `Params` comment example. No breaking change to generated code.

---

## Future Considerations

- **Bitbucket Server / Data Center**: Different API (`/rest/api/1.0/projects/{project}/repos/{slug}/archive`). Lower priority given declining market share.
- **OCI / Container Registry**: Releasing binaries as OCI artefacts (GHCR, Docker Hub) is an emerging pattern. Warrants a separate spec.
- **AWS S3 / GCS / Azure Blob native auth**: The direct provider can reach these via pre-signed URLs, but native IAM authentication would improve private bucket ergonomics.
- **GitLab Package Registry**: Distinct from GitLab Releases; some teams publish binaries there.
- **Mirror / fallback chain**: A `ReleaseSource` that tries multiple providers in order (primary VCS, fallback CDN mirror).
- **Signature verification**: GPG/cosign support, applicable to all providers. Tracked in the offline update spec as a future phase.
- **GoReleaser `checksums.txt`**: Multi-platform checksum file as an alternative to per-file `.sha256` sidecars; relevant for the direct provider.
- **Bitbucket version in `checksums.txt`**: If GoReleaser is used to upload to Bitbucket Downloads, a `checksums.txt` file could provide version information as a side-channel — worth exploring.

---

## Implementation Phases

### Phase 1 — Provider Registry and GitHub/GitLab Migration

1. Add `pkg/vcs/release/registry.go` with `Register`, `Lookup`, `RegisteredTypes` (using `sync.RWMutex`).
2. Add `pkg/vcs/release/source_config.go` with `ReleaseSourceConfig`.
3. Add `pkg/vcs/release/constants.go` with all `SourceType*` constants.
4. Add `init.go` to `pkg/vcs/github` and `pkg/vcs/gitlab` registering their factories.
5. Add `pkg/setup/providers.go` with blank imports.
6. Refactor `NewUpdater` to use registry lookup.
7. Extend `requireReleaseToken` with stubs for new provider cases.
8. Tests: registry unit tests (including concurrent access); verify GitHub and GitLab pass end-to-end.

**Acceptance criteria**: All existing tests pass. `go test -race ./...` clean. `golangci-lint run` clean.

### Phase 2 — Gitea / Forgejo / Codeberg Provider

1. Implement `pkg/vcs/gitea/` (client, release wrappers, provider, init).
2. Register both `"gitea"` and `"codeberg"` factories in `init.go`.
3. Unit tests with `httptest` (including Codeberg host pre-configuration).
4. Integration tests gated by `INT_TEST_GITEA=1`.
5. Complete `requireReleaseToken` cases for `gitea` and `codeberg`.

### Phase 3 — Bitbucket Cloud Provider

1. Implement `pkg/vcs/bitbucket/` (client, filename pattern matching, release wrappers, provider, init).
2. Default and custom regex pattern support via `Params["filename_pattern"]`.
3. Unit tests with `httptest`.
4. Integration tests gated by `INT_TEST_BITBUCKET=1`.
5. Complete `requireReleaseToken` case for `bitbucket`.

### Phase 4 — Direct HTTP Download Provider

1. Implement `pkg/vcs/direct/version.go` — version endpoint fetch with text/JSON/YAML/XML parsing.
2. Implement `pkg/vcs/direct/provider.go` — URL template expansion, `GetLatestRelease`, `GetReleaseByTag`, `DownloadReleaseAsset`.
3. Add `Params` field to `props.ReleaseSource`.
4. Unit tests covering all format variants and template placeholders.

### Phase 5 — Documentation and Generator

1. Update `docs/components/setup.md` with all new providers, `Params` reference tables, and authentication instructions.
2. Update `docs/components/vcs.md` with registry documentation and provider extension guide.
3. Update `docs/concepts/architecture.md` to mention the provider registry pattern.
4. Update `internal/generator/` templates with `Params` comment example.
5. Update `docs/development/integration-testing.md` with `bitbucket` and `gitea` test tags.

---

## Verification

```bash
# After Phase 1
go build ./...
go test -race ./pkg/vcs/release/...
go test -race ./pkg/setup/...
go test -race ./pkg/vcs/github/...
go test -race ./pkg/vcs/gitlab/...
golangci-lint run

# After Phase 2
go test -race ./pkg/vcs/gitea/...
INT_TEST_GITEA=1 go test ./pkg/vcs/gitea/... -v

# After Phase 3
go test -race ./pkg/vcs/bitbucket/...
INT_TEST_BITBUCKET=1 go test ./pkg/vcs/bitbucket/... -v

# After Phase 4
go test -race ./pkg/vcs/direct/...

# Full suite
just ci
```
