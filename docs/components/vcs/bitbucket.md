---
title: Bitbucket
description: Bitbucket Cloud Downloads-based release provider with filename-pattern version detection.
date: 2026-03-31
tags: [components, vcs, bitbucket, releases, downloads]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Bitbucket

**Package:** `pkg/vcs/bitbucket`

Provides a `release.Provider` implementation for Bitbucket Cloud using the Downloads API. Bitbucket has no native "Releases" concept — version information is inferred from asset filenames using a configurable regular expression.

**Source type:** `"bitbucket"`

---

## How It Works

Unlike GitHub and GitLab which have dedicated release APIs with tags and metadata, Bitbucket Cloud only offers a flat Downloads area per repository. The provider:

1. Lists all files in the repository's Downloads via `GET /2.0/repositories/{workspace}/{repo}/downloads`
2. Applies a filename pattern (regex) to extract version numbers from asset names
3. Groups assets by version and synthesises release objects
4. Sorts by creation date (newest first) to determine the latest release

If no version can be extracted from the filename, the file's RFC 3339 creation timestamp is used as the version identifier.

## Supported Operations

| Method | Supported | Notes |
|--------|-----------|-------|
| `GetLatestRelease` | Yes | Fetches downloads, groups by version, returns newest |
| `GetReleaseByTag` | No | Returns `release.ErrNotSupported` — Bitbucket has no versioned releases |
| `ListReleases` | No | Returns `release.ErrNotSupported` |
| `DownloadReleaseAsset` | Yes | Streams asset from download URL with optional Basic Auth |

## Configuration

```go
props.ReleaseSource{
    Type:  "bitbucket",
    Owner: "myworkspace",
    Repo:  "my-tool",
}
```

### Private Repositories

For private repositories, credentials are required:

```go
props.ReleaseSource{
    Type:    "bitbucket",
    Owner:   "myworkspace",
    Repo:    "my-tool",
    Private: true,
}
```

### Authentication

Bitbucket uses HTTP Basic Auth with a username and app password. Each field is resolved independently in the following order, and partial configurations (e.g. username via env-var, app password from the keychain) are supported for rotation scenarios:

1. `bitbucket.<field>.env` — name of an environment variable holding the value (e.g. `bitbucket.username.env: MYTOOL_BB_USER`). Keeps the credential out of the config file.
2. `bitbucket.keychain` — shared `"<service>/<account>"` reference to an OS-keychain (or custom [`Backend`](../credentials.md#backend-interface)) entry whose value is a JSON blob `{"username": "...", "app_password": "..."}`. Only active when a keychain-capable backend is registered (see [`pkg/credentials`](../credentials.md)). Corrupt or incomplete blobs abort resolution rather than falling through — a broken keychain entry is surfaced to the user, not silently masked by a stale literal (R3).
3. `bitbucket.<field>` — literal value stored in config. Viper's `AutomaticEnv` + the tool's env prefix also surfaces `<PREFIX>_BITBUCKET_<FIELD>` through this step.
4. `BITBUCKET_<FIELD>` — well-known unprefixed ecosystem env vars as a final fallback.

| Variable | Description |
|----------|-------------|
| `BITBUCKET_USERNAME` | Bitbucket username |
| `BITBUCKET_APP_PASSWORD` | App password with read access to the repository's Downloads |

When `Private: true` is set, missing credentials return an error instead of proceeding anonymously. `NewReleaseProvider` applies a short internal timeout to the keychain lookup so a misbehaving remote-store backend cannot stall startup.

See [How to configure credentials](../../how-to/configure-credentials.md) for wizard-driven setup and [How to implement a custom credential backend](../../how-to/custom-credential-backend.md) for Vault / AWS SSM / other remote stores.

### Filename Pattern

The default pattern matches GoReleaser-style asset names:

```
tool_v1.2.3_Linux_x86_64.tar.gz
tool_Darwin_arm64.tar.gz
```

Default regex: `^.+?(?:_(v?\d+\.\d+\.\d+[^_]*))?_([A-Za-z]+)_([A-Za-z0-9_]+)\.tar\.gz$`

Override with the `filename_pattern` parameter in `ReleaseSource.Params`:

```go
props.ReleaseSource{
    Type:  "bitbucket",
    Owner: "myworkspace",
    Repo:  "my-tool",
    Params: map[string]string{
        "filename_pattern": `^mytool-(\d+\.\d+\.\d+)-.*\.tar\.gz$`,
    },
}
```

The first capture group in the pattern is used as the version string.

## Version Fallback

When asset filenames don't contain a version (no capture group match), the provider falls back to using the file's creation timestamp in RFC 3339 format (e.g. `2026-03-31T12:00:00Z`). This allows self-update to work even without versioned filenames, using the newest uploaded file.

## Limitations

- `GetReleaseByTag` and `ListReleases` are not supported — returns `release.ErrNotSupported`
- No release metadata (titles, descriptions, changelogs) — only filenames and download URLs
- Version detection depends entirely on filename conventions
- Pagination is handled automatically but large repositories with many downloads may be slow
- Invalid regex patterns in `filename_pattern` return an error at provider creation time

## Related Documentation

- [Release Provider](release.md) — the `Provider` interface and registry
- [Configure Self-Updating](../../how-to/configure-self-updating.md) — wiring up update checks
