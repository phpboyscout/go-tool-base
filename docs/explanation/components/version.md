---
title: Version
description: Semantic version parsing, comparison, and development-build detection for CLI tools.
date: 2026-03-25
tags: [components, version, semver, update]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Version

`pkg/version` provides semantic version handling for GTB-based tools: storing
build-time version information, comparing versions for update checks, and
detecting development builds.

---

## The Version Interface


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/version](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/version) for the full API definition.


---

## Info Struct

`Info` is the concrete implementation of `Version`. It is populated at build
time via `ldflags`:

```go
type Info struct {
    Version string `json:"version" yaml:"version"`
    Commit  string `json:"commit"  yaml:"commit"`
    Date    string `json:"date"    yaml:"date"`
}

func NewInfo(version, commit, date string) Info
```

**Usage in a Go binary:**

```go
// main.go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)

func main() {
    v := version.NewInfo(version, commit, date)
    props := &props.Props{
        Version: v,
        // ...
    }
    root.Execute(props)
}
```

**Setting with GoReleaser / ldflags:**

```yaml
# .goreleaser.yaml
builds:
  - ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}
```

---

## Version Comparison

`CompareVersions` compares two version strings using `golang.org/x/mod/semver`.
Both `v`-prefixed and bare versions are accepted.

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/version"

result := version.CompareVersions("1.2.3", "v1.3.0")
// result == -1  (1.2.3 < 1.3.0 → update available)

result = version.CompareVersions("v2.0.0", "1.9.9")
// result == 1   (2.0.0 > 1.9.9 → already ahead)

result = version.CompareVersions("v1.0.0", "1.0.0")
// result == 0   (equal)
```

Return values follow Go convention: `-1` (less than), `0` (equal), `1` (greater than).

The `Info.Compare(other string) int` method compares the current build version
against a remote version string:

```go
if props.Version.Compare(latestRelease) < 0 {
    p.Logger.Warn("update available", "latest", latestRelease)
}
```

---

## Version Formatting

`FormatVersionString` normalises version strings by adding or removing the `v`
prefix:

```go
version.FormatVersionString("1.2.3", true)   // "v1.2.3"
version.FormatVersionString("v1.2.3", true)  // "v1.2.3"  (idempotent)
version.FormatVersionString("v1.2.3", false) // "1.2.3"
version.FormatVersionString("", true)        // ""         (empty string preserved)
```

---

## Development Build Detection

`IsDevelopment()` returns `true` when:

- The version string is not a valid semver (e.g. `"dev"`, `"unknown"`)
- The version contains `-dev` or `-dirty` suffixes

```go
version.NewInfo("dev", "none", "unknown").IsDevelopment()   // true
version.NewInfo("v0.0.0", "abc", "2026-01-01").IsDevelopment() // false (valid semver, no -dev/-dirty)
version.NewInfo("v1.2.3", "abc", "2026-01-01").IsDevelopment() // false
version.NewInfo("v1.2.3-dev", "abc", "2026-01-01").IsDevelopment() // true
```

The self-updater uses this to require `--force` when updating from a
development build, preventing accidental overwriting of local builds.

---

## Integration with Props

`Props.Version` holds the build version:

```go
p := &props.Props{
    Version: version.NewInfo(buildVersion, buildCommit, buildDate),
}

// In a version command
func runVersionCmd(p *props.Props) {
    p.Logger.Print(p.Version.String())
    // prints: "v1.2.3 (abc1234)"
}
```

---

## Related Documentation

- **[Props](props.md)** — dependency injection container
- **[Setup](setup/index.md)** — self-updater that uses version comparison
- **[Auto-Update Lifecycle](update.md)** — how update checks use version info
