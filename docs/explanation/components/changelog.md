---
title: Changelog
description: Changelog generation and parsing have been extracted to the standalone go/changelog module; GTB wires it into the changelog command, the generator tool, and self-update.
date: 2026-07-21
tags: [components, changelog, release-notes, conventional-commits, module-extraction]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Changelog

Conventional-Commits changelog **generation** (`GenerateFromRepo`, via go-git) and
**parsing** (`Parse`, `ParseFromArchive`, `FormatSummary`, and the `Changelog` / `Release`
/ `Entry` / `Category` model) now live in the standalone module
**[`gitlab.com/phpboyscout/go/changelog`](https://gitlab.com/phpboyscout/go/changelog)**.

It is framework-free. Its graph is go-git, the leodido conventional-commits parser,
`x/mod`, and `cockroachdb/errors`.

- **Docs:** [changelog.go.phpboyscout.uk](https://changelog.go.phpboyscout.uk)
- **API reference:** [pkg.go.dev](https://pkg.go.dev/gitlab.com/phpboyscout/go/changelog)
- **Migration:** [changelog generation/parsing → `go/changelog`](../../reference/migration/v0.x-changelog-extracted.md)

## How GTB uses it

Three GTB surfaces consume the module; none changed behaviour in the move:

- **The embedded `changelog` command** (`pkg/cmd/changelog`) parses and displays release
  notes at runtime. See the [CLI reference](../../reference/cli/changelog.md).
- **The `cmd/changelog` generator tool** wraps `GenerateFromRepo` as a Go tool directive,
  used in CI to write `CHANGELOG.md`:

  ```bash
  go tool changelog generate --output assets/CHANGELOG.md --since v1.5.0
  ```

  ```
  tool gitlab.com/phpboyscout/go-tool-base/cmd/changelog
  ```

- **Self-update** (`pkg/setup`): `SelfUpdater.GetStructuredReleaseNotes` fetches release
  notes: preferring a `CHANGELOG.md` bundled in the release archive via
  `ParseFromArchive`, falling back to per-release API calls, and renders breaking changes
  prominently with `FormatSummary`.

  ```go
  cl, err := updater.GetStructuredReleaseNotes(ctx, "v1.0.0", "v1.3.0", archive)
  if err == nil && cl.HasBreakingChanges() {
      fmt.Print(changelog.FormatSummary(cl))
  }
  ```

## Related

- **[Setup / Update](../../reference/cli/update.md)**: the self-update lifecycle that consumes the parser.
- **[Version](version.md)**: version comparison utilities used alongside changelog diffing.
