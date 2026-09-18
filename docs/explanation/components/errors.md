---
title: "Error Catalogue"
description: "Sentinel errors defined across GTB packages, with descriptions and handling guidance."
date: 2026-03-25
tags: [components, errors, error-handling, sentinel]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Error Catalogue

This document catalogues the sentinel errors defined in GTB's own `pkg/`
packages: the ones a tool built on GTB is most likely to check for with
`errors.Is`. GTB uses `gitlab.com/phpboyscout/go/errors` for wrapping and stack
traces (a stdlib-only package with the same symbols as `cockroachdb/errors`,
which GTB no longer depends on). Every extracted module GTB consumes has its
own sentinels, documented on its own microsite; see
[Module error catalogues](#module-error-catalogues) below for the links.

Use `errors.Is(err, target)` to check for sentinel errors, this traverses
wrapped error chains correctly.

```go
import "gitlab.com/phpboyscout/go/errors"

if errors.Is(err, root.ErrNoConfigFile) {
    // prompt user to run init
}
```

---

## `pkg/cmd/root`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoConfigFile` | no config file found | Gates auto-initialise. The root pre-run heals it by running a non-interactive `init` when `Tool.Bootstrap.AutoInitialise` is set; otherwise it surfaces so the tool can prompt the user to run `init` or pass `--config`. This is GTB's own sentinel: config v0.4.0's Store treats a missing optional file as an empty layer, not an error, so the framework owns the "no config at all" distinction (it replaces config v0.2.0's `ErrNoFilesFound`). |

---

## `pkg/props`

Raised by the feature registry while a tool's feature set is being assembled at
`init()` time. Every one is a programming error in the tool or a plugin it
imports, not a runtime condition, they surface as a panic through
`RegisterFeature` rather than something a command can recover from.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidDescriptor` | props: feature descriptor is incomplete | A `FeatureDescriptor` is missing one of `ID`, `ConstName`, `ConstPackage` or `Kind`. All four are required: the generator needs the constant's name *and* its package to emit a qualified reference. Fix the descriptor at its registration site. |
| `ErrDuplicateFeature` | props: feature is already registered | Two registrations claim the same feature ID. Usually two plugins colliding on a name; rename one, since the ID is the key everything else keys off. |
| `ErrPluginDefaultOn` | props: only builtin features may be default-enabled | A non-builtin feature declared `Default: true`. Adding a blank import must change what is *available*, never what is *on*: otherwise an import list becomes a behavioural file and a downstream that omits a provider cannot reason about what its remaining imports switched on. Ship the feature default-off and let the tool enable it. |

---

## `gitlab.com/phpboyscout/go/controls`

`ErrShutdown` (returned by `Wait()` in some shutdown paths, generally expected:
log at debug and exit cleanly) is the sentinel a GTB command may see. Full
catalogue on the [module's reference](https://controls.go.phpboyscout.uk/reference/).

---

## `gitlab.com/phpboyscout/go/errorhandling`

`ErrNotImplemented` (constructed via `NewErrNotImplemented(issueURL string) error`
for a command stubbed with a tracking issue) and `ErrRunSubCommand` (a parent
command invoked without a subcommand) are the two sentinels a GTB command
commonly returns. Full catalogue on the
[module's error reference](https://errorhandling.go.phpboyscout.uk/reference/api/).

---

## `pkg/logger`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidLevel` | invalid level | Returned by `ParseLevel(s string)` when the string does not map to a known log level. Validate user-supplied log level strings at config load time. |
| `ErrInvalidFormat` | invalid format | Returned by `ParseFormatter(s string)` when the string does not map to a known formatter. Validate user-supplied log format strings at config load time. |

---

## `pkg/cmd/root`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUpdateComplete` | update complete: restart required | Returned by the `update` command after a successful self-update. The root command's `Execute` detects this and exits with code 0, prompting the user to restart the tool. |

---

## `pkg/setup`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrChecksumAssetNotFound` | checksum asset not found | Returned when the release source does not provide a checksums.txt file. |
| `ErrChecksumManifestMalformed` | checksum manifest malformed | Returned when parsing an invalid checksums.txt file. |
| `ErrChecksumManifestDuplicate` | checksum manifest duplicate | Returned when the checksum file contains multiple entries for the same binary. |
| `ErrChecksumTooLarge` | checksum too large | Returned when the checksums.txt file exceeds the maximum allowed size. |
| `ErrBinaryTooLarge` | binary too large | Returned during extraction if the update binary is dangerously large. |
| `ErrBinaryNotInArchive` | binary not in archive | Returned when extracting an update tarball/zip that doesn't contain the expected executable. |
| `ErrDowngradeRefused` | refusing to downgrade | Returned by the implicit (no `--version`) update path when the resolved release is older than the running binary. Signature and checksum verification authenticate an artefact, not its recency, so a stale or rolled-back release listing must not silently downgrade the tool. Deliberate downgrades go through `update --force` or `update --version`. |

`ErrSignatureInvalid`, `ErrSignatureMissing`, `ErrWeakKey` and the other
key-resolution sentinels live in `gitlab.com/phpboyscout/go/signing/verify`;
`SelfUpdater` re-surfaces them during `Update()`. Full list on the
[module's reference](https://signing.go.phpboyscout.uk/reference/).

---

## Module error catalogues

Every other extracted module GTB consumes defines its own sentinels,
documented on its own microsite rather than duplicated here:

| Module | GTB consumer | Error reference |
|--------|--------------|------------------|
| `go/authn` | HTTP/gRPC auth, via `go/transport` | [authn.go.phpboyscout.uk](https://authn.go.phpboyscout.uk/reference/) |
| `go/browser` | `browser.OpenURL` call sites | [browser.go.phpboyscout.uk](https://browser.go.phpboyscout.uk/reference/errors/) |
| `go/chat` (+ providers) | `pkg/chat` | [chat.go.phpboyscout.uk](https://chat.go.phpboyscout.uk/reference/) |
| `go/credentials` | `pkg/setup`, `pkg/cmd/config` | [credentials.go.phpboyscout.uk](https://credentials.go.phpboyscout.uk/reference/errors/) |
| `go/signing`, `go/signing/openpgpkey`, `go/signing/local`, `go/signing-aws-kms` | `pkg/setup` (`gtb keys`/`gtb sign`) | [signing.go.phpboyscout.uk](https://signing.go.phpboyscout.uk/reference/) |
| `go/regexutil` | any bounded-compile call site | [regexutil.go.phpboyscout.uk](https://regexutil.go.phpboyscout.uk/reference/errors/) |
| `go/observability` | `pkg/telemetry` | [observability.go.phpboyscout.uk](https://observability.go.phpboyscout.uk/reference/) |
| `go/workspace` | the generator commands | [workspace.go.phpboyscout.uk](https://workspace.go.phpboyscout.uk/reference/) |
| `go/transit` | `pkg/http`, `pkg/grpc` clients | [transit.go.phpboyscout.uk](https://transit.go.phpboyscout.uk/reference/) |

`go/controls` and `go/errorhandling` each surface one sentinel a GTB command
commonly checks for; see the sections above.

---

## Notes

### Internal package errors

The `internal/` packages define additional sentinel errors for generator and
code-generation use. These are not part of the public API and may change
without notice:

| Package | Errors |
|---------|--------|
| `cli/pkg/generator` | `ErrNotGoToolBaseProject`, `ErrCommandProtected`, `ErrInvalidPackageName`, `ErrParentCommandFileNotFound` |
| `cli/pkg/cmd/generate` | `ErrRepositoryInvalidFormat`, `ErrEmptyCommandPath`, `ErrCommandNotFound`, `ErrUpdateManifestFailed` |
| `cli/pkg/cmd/regenerate` | `ErrInvalidOverwriteValue` |
| `cli/pkg/generator/verifier` | `ErrVerificationFailed` |
| `cli/pkg/agent` | `ErrInvalidPackageName` |

### Adding new errors

When adding a sentinel error to a `pkg/` package:

1. Define it as a package-level `var` using `NewSentinel(kind, msg)`, not
   `New`: at package scope `New` captures its stack at initialisation, which
   points at `runtime.doInit` rather than anywhere the error was returned.
   Kinds are namespaced `gtb.<package>.<name>`:
   ```go
   var ErrMyCondition = errors.NewSentinel("gtb.mypackage.my_condition", "description of the condition")
   ```
2. Add an entry to this catalogue with a description and handling guidance.
3. Use `errors.Wrap(err, "context")` to add call-site context when returning
   the error through multiple layers.
