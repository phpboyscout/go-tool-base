---
title: "Error Catalogue"
description: "Sentinel errors defined across GTB packages, with descriptions and handling guidance."
date: 2026-03-25
tags: [components, errors, error-handling, sentinel]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Error Catalogue

This document lists the sentinel errors a GTB tool may encounter, both those
defined in GTB's own `pkg/` packages and those from the standalone
`gitlab.com/phpboyscout/go/*` modules it consumes (each such section links to the
owning module). All errors use `github.com/cockroachdb/errors` for wrapping and
stack traces.

Use `errors.Is(err, target)` to check for sentinel errors, this traverses
wrapped error chains correctly.

```go
import "github.com/cockroachdb/errors"

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
| `ErrRegistrySealed` | props: feature registry is sealed | Registration was attempted after the registry had been enumerated. Feature registration belongs in `init()`; anything registering later has already missed the enumeration that built the command tree. |
| `ErrPluginDefaultOn` | props: only builtin features may be default-enabled | A non-builtin feature declared `Default: true`. Adding a blank import must change what is *available*, never what is *on*: otherwise an import list becomes a behavioural file and a downstream that omits a provider cannot reason about what its remaining imports switched on. Ship the feature default-off and let the tool enable it. |

---

## `gitlab.com/phpboyscout/go/controls`

Extracted into the standalone [controls module](https://controls.go.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrShutdown` | controller shutdown | Signals that the controller has stopped. Returned by `Wait()` in some shutdown paths. Generally expected: log at debug level and exit cleanly. |

---

## `gitlab.com/phpboyscout/go/errorhandling`

Extracted into the standalone [errorhandling module](https://errorhandling.go.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNotImplemented` | command not yet implemented | Returned by commands that are scaffolded but not yet implemented. The error handler surfaces an issue-tracker link if one was provided via `NewErrNotImplemented(issueURL)`. |
| `ErrRunSubCommand` | subcommand required | Returned when a parent command is invoked without a subcommand. The error handler prints available subcommands automatically. |

### Constructor Functions

`NewErrNotImplemented(issueURL string) error`, creates an `ErrNotImplemented`
error with an optional issue URL. The error handler detects this and appends
the link to the user-facing output.

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

## `gitlab.com/phpboyscout/go/authn`

Extracted into the standalone [authn module](https://authn.go.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnauthenticated` | unauthenticated | Returned when a request lacks valid authentication credentials. Return a 401 Unauthorized response or prompt for login. |

---

## `gitlab.com/phpboyscout/go/browser`

Extracted into the standalone [browser module](https://browser.go.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidURL` | invalid URL | Returned when parsing a malformed URL for browser launch. |
| `ErrDisallowedScheme` | disallowed scheme | Returned when the URL scheme is not HTTP or HTTPS, preventing local file/command execution vulnerabilities. |

---

## `gitlab.com/phpboyscout/go/chat`

Extracted into the standalone [chat module](https://chat.go.phpboyscout.uk) (+ per-provider modules); gtb's `pkg/chat` is a thin adapter that consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidBaseURL` | invalid base url | Returned when configuring an AI provider with a malformed base URL. |
| `ErrInvalidSnapshotID` | invalid snapshot ID | Returned by FileStore when attempting to load a conversation snapshot that doesn't exist or is corrupted. |
| `ErrMediaRejected` | media rejected | Returned when an attachment fails the safety filter: empty, oversized, over the per-message count limit, or a disallowed MIME type. Surface to the user so they can drop or replace the attachment. |
| `ErrMediaUnsupported` | media not supported by provider | Returned when the selected provider or model cannot accept the attachment's media type. Fall back to text or select a multimodal provider. |

---

## `gitlab.com/phpboyscout/go/credentials`

Extracted into the standalone [credentials module](https://credentials.go.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrCredentialUnsupported` | credential unsupported | Returned when the loaded credential backend is a stub or read-only mode. |
| `ErrCredentialNotFound` | credential not found | Returned when the requested account/service is missing from the secret store. |

---

## `gitlab.com/phpboyscout/go/signing/openpgpkey`

Extracted into the standalone [signing module](https://signing.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnsupportedKeyType` | unsupported key type: only RSA is supported | Returned by `ArmoredPublicKey` when the key is not an RSA key. RSA is the only supported key type for GTB OpenPGP signing. |

---

## `gitlab.com/phpboyscout/go/regexutil`

Extracted into the standalone [regexutil module](https://regexutil.go.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrPatternTooLong` | pattern too long | Returned by the ReDoS defense checks when a regex string exceeds safe length limits. |
| `ErrPatternCompileTimeout` | pattern compile timeout | Returned when compiling a complex regex takes too long. |
| `ErrPatternInvalid` | pattern invalid | Returned when the provided regex pattern has invalid syntax. |

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

The signature-verification sentinels below were extracted into `gitlab.com/phpboyscout/go/signing/verify`; gtb's `SelfUpdater` re-surfaces them during `Update()`.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrSignatureInvalid` | signature verification failed | Returned when no key in the trust set verifies the release signature. The failure path does not name the keys involved. |
| `ErrSignatureMissing` | signature asset not found in release | Returned when `require_signature` is true and the release provides no signature asset. |
| `ErrSignatureTooLarge` | signature download exceeds maximum size | Returned when the signature download exceeds the maximum allowed size. |
| `ErrWeakKey` | public key fails minimum-strength policy | Returned when a public key (embedded or fetched) does not meet the minimum-strength policy. Any weak key in the input aborts the load. |
| `ErrKeyResolverMismatch` | key resolvers returned mismatched trust sets | Returned by `CompositeResolver` when the configured resolvers return mismatched trust sets. |
| `ErrKeyResolverUnavailable` | key resolver unavailable | Returned when a key resolver cannot be reached or is otherwise unavailable. |
| `ErrWKDResponseTooLarge` | WKD response exceeds maximum size | Returned when a WKD (Web Key Directory) response exceeds the maximum allowed size. |

---

## `gitlab.com/phpboyscout/go/signing`

Extracted into the standalone [signing module](https://signing.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnknownBackend` | unknown backend | Returned when attempting to instantiate an unregistered signing backend from config. |

---

## `gitlab.com/phpboyscout/go/signing-aws-kms`

The `awskms` backend: a separate module consumed by the standard gtb binary.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnsupportedKMSKeyType` | KMS key is not RSA; only RSA SIGN_VERIFY keys are supported | Returned by `NewSigner` when the AWS KMS key is not an RSA SIGN_VERIFY key. RSA is the only supported KMS key type. |
| `ErrUnsupportedHashFunc` | unsupported hash function; KMS RSA Sign accepts SHA-256 / 384 / 512 only | Returned when configuring KMS signing with an unsupported hash algorithm. |
| `ErrPSSUnsupported` | RSASSA-PSS is not supported; this KMS signer only implements RSASSA-PKCS1-v1_5 | Returned when attempting to use RSA-PSS, which is disabled for KMS backends. |

---

## `gitlab.com/phpboyscout/go/signing/local`

The `local` PEM backend, part of the standalone signing module.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnsupportedKeyType` | PEM key is not RSA; only RSA private keys are supported | Returned when the local private key is not an RSA key. RSA is the only supported private-key type for local signing. |
| `ErrMissingPEMBlock` | no PEM block found in file | Returned when reading a key file that contains no valid PEM-encoded data. |
| `ErrEncryptedPEMUnsupported` | encrypted PEM private keys are not supported in v0.1; decrypt out-of-band first or use the aws-kms backend | Returned when trying to load a password-protected local key, which is currently not supported for headless signing. |

---

## `gitlab.com/phpboyscout/go/observability`

Extracted into the standalone [observability module](https://observability.go.phpboyscout.uk) (the OTel core, in its `otelcore` subpackage); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidEndpoint` | invalid endpoint | Returned when validating the configured OTLP collector URL endpoint. |

---

## `gitlab.com/phpboyscout/go/workspace`

Extracted into the standalone [workspace module](https://workspace.go.phpboyscout.uk); gtb consumes it.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNotFound` | not found | Returned when walking up the directory tree fails to locate the project root marker. |

---

## `gitlab.com/phpboyscout/go/transit`

Extracted into the standalone [transit module](https://transit.go.phpboyscout.uk) (shared transport middleware + resilience); gtb consumes it via the HTTP/gRPC clients. gtb's `pkg/http` retains only the config adapters.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrCircuitOpen` | circuit open | Returned by the resilient HTTP client when consecutive failures trigger the circuit breaker. Fallback to cached responses or queue the request. |

---

## Notes

### Internal package errors

The `internal/` packages define additional sentinel errors for generator and
code-generation use. These are not part of the public API and may change
without notice:

| Package | Errors |
|---------|--------|
| `internal/generator` | `ErrNotGoToolBaseProject`, `ErrCommandProtected`, `ErrInvalidPackageName`, `ErrParentCommandFileNotFound` |
| `internal/cmd/generate` | `ErrRepositoryInvalidFormat`, `ErrEmptyCommandPath`, `ErrCommandNotFound`, `ErrUpdateManifestFailed` |
| `internal/cmd/regenerate` | `ErrInvalidOverwriteValue` |
| `internal/generator/verifier` | `ErrVerificationFailed` |
| `internal/agent` | `ErrInvalidPackageName` |

### Adding new errors

When adding a sentinel error to a `pkg/` package:

1. Define it as a package-level `var` using `errors.New`:
   ```go
   var ErrMyCondition = errors.New("description of the condition")
   ```
2. Add an entry to this catalogue with a description and handling guidance.
3. Use `errors.Wrap(err, "context")` to add call-site context when returning
   the error through multiple layers.
