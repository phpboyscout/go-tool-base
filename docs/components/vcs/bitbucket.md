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

---

## How It Works

Unlike GitHub and GitLab which have dedicated release APIs with tags and metadata, Bitbucket Cloud only offers a flat Downloads area per repository. The provider:

1. Lists all files in the repository's Downloads via `GET /2.0/repositories/{workspace}/{repo}/downloads`
2. Applies a filename pattern (regex) to extract version numbers from asset names
3. Groups assets by version and synthesises release objects
4. Sorts by version (semver) to determine the latest release

## Configuration

```go
props.ReleaseSource{
    Type:  "bitbucket",
    Owner: "myworkspace",
    Repo:  "my-tool",
}
```

### Authentication

Bitbucket authentication uses HTTP Basic Auth with a username and app password. Configure via environment variables:

| Variable | Description |
|----------|-------------|
| `BITBUCKET_USERNAME` | Bitbucket username |
| `BITBUCKET_APP_PASSWORD` | App password with read access to the repository's Downloads |

Alternatively, configure in the config file under the `bitbucket` subtree.

### Filename Pattern

The default pattern matches GoReleaser-style asset names:

```
tool_v1.2.3_Linux_x86_64.tar.gz
tool_Darwin_arm64.tar.gz
```

Pattern: `^.+?(?:_(v?\d+\.\d+\.\d+[^_]*))?_([A-Za-z]+)_([A-Za-z0-9_]+)\.tar\.gz$`

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

## Limitations

- No release metadata (titles, descriptions, changelogs) — only filenames and download URLs
- Pagination is handled automatically but large repositories with many downloads may be slow
- Version detection depends entirely on filename conventions

## Related Documentation

- [Release Provider](release.md) — the `Provider` interface and registry
- [Configure Self-Updating](../../how-to/configure-self-updating.md) — wiring up update checks
