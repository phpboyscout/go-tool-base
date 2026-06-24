---
title: Changelog Command
description: Display version history from the embedded CHANGELOG.md with version filtering.
date: 2026-04-01
tags: [components, commands, changelog, versioning]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Changelog Command

The `changelog` command displays version history from a CHANGELOG.md embedded in the tool's assets. The changelog is baked into the binary at build time, so it always reflects the version the user is running.

## Usage

```bash
mytool changelog                    # show full changelog
mytool changelog --latest           # show only the most recent release
mytool changelog --version v1.2.0   # show changes for a specific version
mytool changelog --since v1.1.0     # show changes since a version (exclusive)
mytool changelog --output json      # structured output for scripting
```

## Feature Flag

The `changelog` command is **enabled by default**. Disable it via `props.SetFeatures`:

```go
props.SetFeatures(props.Disable(props.ChangelogCmd))
```

## Embedding the Changelog

The command reads CHANGELOG.md from `props.Assets`. Tool authors must include it in their embedded assets:

```go
//go:embed all:assets
var assets embed.FS

// Include CHANGELOG.md at the root of the embed
//go:embed CHANGELOG.md
var changelogFS embed.FS

p := &props.Props{
    Assets: props.NewAssets(props.AssetMap{
        "root":      &assets,
        "changelog": &changelogFS,
    }),
}
```

If CHANGELOG.md is not present in the assets, the command shows a helpful error message rather than failing silently.

## Generating the Changelog

GTB uses a pure-Go changelog generator (`go tool changelog generate`) to produce CHANGELOG.md from conventional commits. The goreleaser workflow runs it before building, so it's available for embedding.

For generated tools, the CI pipeline runs `go tool changelog generate --output CHANGELOG.md` before `go build` so the embed picks it up. The tool is declared as a Go `tool` directive in `go.mod`.

## Integration with Update

After a successful self-update, the update command suggests viewing the changelog:

```
Updated to v1.3.0.
Run 'mytool changelog --latest' to see the full changelog.
```

## Flags

| Flag | Description |
|------|-------------|
| `--latest` | Show only the most recent release |
| `--version v1.2.0` | Show changes for a specific version |
| `--since v1.1.0` | Show all changes after this version (exclusive) |
| `--output json` | Output as structured JSON |

## Related Documentation

- [Changelog Package](../changelog.md) — the underlying parser
- [Update Command](update.md) — self-update with changelog display
