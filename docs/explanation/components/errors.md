---
title: "Error Catalogue"
description: "Sentinel errors defined across GTB packages, with descriptions and handling guidance."
date: 2026-03-25
tags: [components, errors, error-handling, sentinel]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Error Catalogue

This document lists all sentinel errors defined across GTB packages. All errors
use `github.com/cockroachdb/errors` for wrapping and stack traces.

Use `errors.Is(err, target)` to check for sentinel errors — this traverses
wrapped error chains correctly.

```go
import "github.com/cockroachdb/errors"

if errors.Is(err, config.ErrNoFilesFound) {
    // prompt user to run init
}
```

---

## `pkg/config`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoFilesFound` | no configuration files found please run init, or provide a config file using the --config flag | Prompt the user to run `init` or pass `--config`. Returned by `LoadFilesContainer` when no config files exist at any of the provided paths. |
| `ErrConfigFileNotFound` | config file not found | Returned by `LoadFilesContainer` when the first (explicitly named) config file does not exist on disk. |

---

## `pkg/controls`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrShutdown` | controller shutdown | Signals that the controller has stopped. Returned by `Wait()` in some shutdown paths. Generally expected — log at debug level and exit cleanly. |

---

## `pkg/errorhandling`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNotImplemented` | command not yet implemented | Returned by commands that are scaffolded but not yet implemented. The error handler surfaces an issue-tracker link if one was provided via `NewErrNotImplemented(issueURL)`. |
| `ErrRunSubCommand` | subcommand required | Returned when a parent command is invoked without a subcommand. The error handler prints available subcommands automatically. |

### Constructor Functions

`NewErrNotImplemented(issueURL string) error` — creates an `ErrNotImplemented`
error with an optional issue URL. The error handler detects this and appends
the link to the user-facing output.

---

## `pkg/logger`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidLevel` | invalid level | Returned by `ParseLevel(s string)` when the string does not map to a known log level. Validate user-supplied log level strings at config load time. |

---

## `pkg/vcs/github`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoPullRequestFound` | no pull request found | Returned by `GetPullRequestByBranch` when no open PR exists for the given branch. Check before attempting PR operations. |
| `ErrRepoExists` | repository already exists | Returned when attempting to create a repository that already exists on the remote. |

---

## `pkg/cmd/root`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUpdateComplete` | update complete — restart required | Returned by the `update` command after a successful self-update. The root command's `Execute` detects this and exits with code 0, prompting the user to restart the tool. |

---

## `pkg/authn`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnauthenticated` | unauthenticated | Returned when a request lacks valid authentication credentials. Return a 401 Unauthorized response or prompt for login. |

---

## `pkg/browser`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidURL` | invalid URL | Returned when parsing a malformed URL for browser launch. |
| `ErrDisallowedScheme` | disallowed scheme | Returned when the URL scheme is not HTTP or HTTPS, preventing local file/command execution vulnerabilities. |

---

## `pkg/chat`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidBaseURL` | invalid base url | Returned when configuring an AI provider with a malformed base URL. |
| `ErrInvalidSnapshotID` | invalid snapshot ID | Returned by FileStore when attempting to load a conversation snapshot that doesn't exist or is corrupted. |

---

## `pkg/credentials`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrCredentialUnsupported` | credential unsupported | Returned when the loaded credential backend is a stub or read-only mode. |
| `ErrCredentialNotFound` | credential not found | Returned when the requested account/service is missing from the secret store. |

---

## `pkg/openpgpkey`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnsupportedKeyType` | unsupported key type: only RSA is supported | Returned by `ArmoredPublicKey` when the key is not an RSA key. RSA is the only supported key type for GTB OpenPGP signing. |

---

## `pkg/regexutil`

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
| `ErrSignatureInvalid` | signature verification failed | Returned when no key in the trust set verifies the release signature. The failure path does not name the keys involved. |
| `ErrSignatureMissing` | signature asset not found in release | Returned when `require_signature` is true and the release provides no signature asset. |
| `ErrSignatureTooLarge` | signature download exceeds maximum size | Returned when the signature download exceeds the maximum allowed size. |
| `ErrWeakKey` | public key fails minimum-strength policy | Returned when a public key (embedded or fetched) does not meet the minimum-strength policy. Any weak key in the input aborts the load. |
| `ErrKeyResolverMismatch` | key resolvers returned mismatched trust sets | Returned by `CompositeResolver` when the configured resolvers return mismatched trust sets. |
| `ErrKeyResolverUnavailable` | key resolver unavailable | Returned when a key resolver cannot be reached or is otherwise unavailable. |
| `ErrWKDResponseTooLarge` | WKD response exceeds maximum size | Returned when a WKD (Web Key Directory) response exceeds the maximum allowed size. |

---

## `pkg/signing`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnknownBackend` | unknown backend | Returned when attempting to instantiate an unregistered signing backend from config. |

---

## `pkg/signing/kms`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnsupportedKMSKeyType` | KMS key is not RSA; only RSA SIGN_VERIFY keys are supported | Returned by `NewSigner` when the AWS KMS key is not an RSA SIGN_VERIFY key. RSA is the only supported KMS key type. |
| `ErrUnsupportedHashFunc` | unsupported hash function; KMS RSA Sign accepts SHA-256 / 384 / 512 only | Returned when configuring KMS signing with an unsupported hash algorithm. |
| `ErrPSSUnsupported` | RSASSA-PSS is not supported; this KMS signer only implements RSASSA-PKCS1-v1_5 | Returned when attempting to use RSA-PSS, which is disabled for KMS backends. |

---

## `pkg/signing/local`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrUnsupportedKeyType` | PEM key is not RSA; only RSA private keys are supported | Returned when the local private key is not an RSA key. RSA is the only supported private-key type for local signing. |
| `ErrMissingPEMBlock` | no PEM block found in file | Returned when reading a key file that contains no valid PEM-encoded data. |
| `ErrEncryptedPEMUnsupported` | encrypted PEM private keys are not supported in v0.1; decrypt out-of-band first or use the aws-kms backend | Returned when trying to load a password-protected local key, which is currently not supported for headless signing. |

---

## `pkg/telemetry/otelcore`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidEndpoint` | invalid endpoint | Returned when validating the configured OTLP collector URL endpoint. |

---

## `pkg/vcs/repo`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrAlreadyRepository` | already repository | Returned by Init() when attempting to initialize a git repo inside an existing git repository. |
| `ErrNoRepository` | repository not initialised; call Open, Clone, or SetRepo first | Returned by `WithRepo` (and related methods) when no `*git.Repository` has been set. |
| `ErrNoWorktree` | worktree not initialised; call Open, Clone, or SetTree first | Returned by `WithTree` (and related methods) when no `*git.Worktree` has been set. |

---

## `pkg/vcs/release`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrProviderNotFound` | no release provider registered for source type | Returned by `Lookup` when no factory has been registered for the requested source type. |
| `ErrNotSupported` | operation not supported by this release provider | Returned by provider methods that are not applicable to the configured provider mode. Callers treat it like "provider does not implement this". |
| `ErrVersionUnknown` | cannot determine latest version: configure version_url or pinned_version in Params | Returned by the direct provider when neither a version URL nor a pinned version is configured. |
| `ErrReleaseNotFound` | release not found | Returned when a requested release — by tag, or the latest — cannot be located. |

---

## `pkg/workspace`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNotFound` | not found | Returned when walking up the directory tree fails to locate the project root marker. |

---

## `pkg/http`

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
