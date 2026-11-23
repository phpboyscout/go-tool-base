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
| `ErrNoConfigFile` | no config file found | Gates auto-initialise. The root pre-run heals it by running a non-interactive `init` when `Tool.Bootstrap.AutoInitialise` is set; otherwise it surfaces for a run at a terminal so the tool can prompt the user to run `init` or pass `--config`, and an unattended run starts on the defaults instead. This is GTB's own sentinel: config v0.4.0's Store treats a missing optional file as an empty layer, not an error, so the framework owns the "no config at all" distinction (it replaces config v0.2.0's `ErrNoFilesFound`). |
| `ErrUpdateComplete` | update complete: restart required | Returned by the `update` command after a successful self-update. The root command's `Execute` detects this and exits with code 0, prompting the user to restart the tool. |

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

Raised by `props.New` (through `Props.Validate`) when `Tool.Config` declares
something the store cannot honour. Like the registry errors they are mistakes
in the tool's own code, found when the tool starts rather than when a user hits
them. The wrapped message names the value refused, and most carry a hint.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrConfigLayerOrder` | config layer order | `Tool.Config.Layers` breaks one of the ordering rules (spec 0204 D1): `defaults` lowest, `flags` highest, `project` below `env`. Reorder the declaration, lowest precedence first. |
| `ErrDuplicateConfigLayer` | duplicate config layer | A layer appears more than once in `Tool.Config.Layers`. Remove the repeat. |
| `ErrUnknownConfigLayer` | unknown config layer | `Tool.Config.Layers` names a layer the framework does not know. Check the spelling against the layer constants in `pkg/props`. |
| `ErrConfigSource` | config source declaration | A slot in `Tool.Config.Sources` is declared in a way spec 0204 D15 refuses. The wrapped message names the slot and the rule. |
| `ErrConfigFormat` | config format cannot be a tool's own | `Tool.Config.Format` names a format the tool cannot write, so `init` could not create its own file. Use `yaml`, `toml`, `json` or `hcl`. |
| `ErrConfigKeyCase` | unknown config key case | The key case is something other than folded or exact. |
| `ErrConfigFileOnly` | file-only config pattern | A `Tool.Config.FileOnly` pattern could never match a key, so it protects nothing. Fix or remove the pattern. |

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

## `pkg/setup`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrChecksumAssetNotFound` | asset not found in checksums manifest | The release's checksums.txt has no line for the asset being installed, so its hash can't be checked. |
| `ErrChecksumManifestMalformed` | checksums manifest is malformed | Returned when parsing an invalid checksums.txt file. |
| `ErrChecksumManifestDuplicate` | checksums manifest contains a duplicate filename | Returned when the checksum file contains multiple entries for the same binary. |
| `ErrChecksumTooLarge` | download exceeds maximum size | The checksums manifest or the binary download passed its size bound, and the update stops before installing anything. It usually points at a hostile or misbehaving server; raise the bound only if the asset really is that large. |
| `ErrBinaryTooLarge` | binary download exceeds maximum size | Returned during extraction if the update binary is dangerously large. |
| `ErrBinaryNotInArchive` | expected binary not found in archive | Returned when extracting an update tarball/zip that doesn't contain the expected executable. |
| `ErrDowngradeRefused` | refusing to downgrade | Returned by the implicit (no `--version`) update path when the resolved release is older than the running binary. Signature and checksum verification authenticate an artefact, not its recency, so a stale or rolled-back release listing must not silently downgrade the tool. Deliberate downgrades go through `update --force` or `update --version`. |
| `ErrDowngradeNotConfirmed` | a downgrade needs confirmation | A rollback or pinned downgrade that nobody confirmed: declined at the prompt, or asked for unattended without `--force`. Re-run with `--force` to downgrade without a prompt. |
| `ErrNoEarlierRelease` | no earlier release to roll back to | A rollback found no release below the running version among the newest ones the channel lists. There is nothing to roll back to; name a version with `update --version` if an older one exists. |
| `ErrAlreadyCurrent` | already running the current version; nothing replaced | Not a failure. The running binary is already the latest release (or a development build without `--force`), and nothing was replaced. The `update` command maps it to a no-op result; a caller of `Update` must not report an update. |
| `ErrInvalidPinnedVersion` | the pinned version is not a release tag | The pinned version does not name a release tag. Pin a tag such as `v1.2.3`. |
| `ErrPinInCheckout` | the config directory is inside a git checkout | A version pin was refused because the config directory is inside a git checkout, where the repository could have supplied it. Keep the tool's config directory outside any checkout. |
| `ErrManagedInstall` | refusing to replace a package-managed binary | The binary was installed by a package manager, and replacing it would leave the manager's record describing a file no longer on disk. Upgrade through the package manager instead. |
| `ErrRotationSignatureMissing` | the rotation manifest has no signature | A release lists a rotation manifest without the authority signature beside it, so the update stops. The fix is on the publishing side: release the manifest with its `.minisig`. |
| `ErrRotationStoreCorrupt` | the rotation record cannot be read | The local record of applied rotations cannot be read. It is never treated as empty, because an empty record forgets every revoke. Restore or remove the file deliberately; the wrapped error names it. |
| `ErrTrustStoreInCheckout` | the config directory is inside a git checkout | Trust would be recorded in a config directory inside a git checkout, where the repository could have supplied it. Keep the config directory outside any checkout. |
| `ErrDuplicateTrustKey` | a key is embedded more than once | `CheckTrustKeys` found one key twice among a tool's release and anchor-only keys, or bound as another key's subkey, so the trust set cannot match the WKD copy, and a generated project's trustkeys test fails. Keep one copy: under `internal/trustkeys/keys/` if it signs releases, under `internal/trustkeys/authority/` if it must not, and bound as no other key's subkey. |
| `ErrNoHomeDir` | no home directory found | A home directory is needed and neither `HOME` nor the user's account names one that exists. Raised where the SSH key setup needs `~/.ssh`. Set `HOME`. |
| `ErrNonInteractive` | an interactive terminal is needed | A wizard was asked for where nobody can answer it: stdin is not a terminal and accessible mode is off. Run it at a terminal, or pass `--accessible` to answer it line by line over a pipe. |
| `ErrInputEnded` | input ended before every question was answered | An accessible form's stdin ran out before every question was answered. Supply an answer for each question. |
| `ErrUnlinkedConfigFormat` | config file format not linked | A config file's extension names a format the binary does not link (spec 0204 D2). Rename the file to a linked format, or have the tool's author link the format's package. |
| `ErrReadOnlyConfigFormat` | config format is read-only | A write was asked of a config file whose format's codec cannot edit. Make the change by hand, or keep that setting in a writable file. |
| `ErrAmbiguousProjectConfig` | more than one project-local config file | Two project-local config files sit in one directory (spec 0204 D16), so neither can be chosen. Remove one. |
| `ErrConfigFormatChanged` | config file is in the tool's previous format | The tool's own config file exists only in the tool's previous format (spec 0204 D14). Convert it with `config convert`. |
| `ErrConfigSourceKindNotLinked` | config source kind not linked | A config source slot names a kind this binary does not link. The tool's author links the kind's package from `main`. |
| `ErrConfigSourceUnconfigured` | config source not configured | A required config source slot has no settings. Configure it under `config.sources.<name>`. |
| `ErrConfigSourceUnavailable` | config source unavailable | A required config source's backend could not be built. The wrapped error is the backend's own reason. |
| `ErrConfigSourceNeedsOverride` | config source needs an author override | A slot declared override-only has no factory from `OverrideConfigSource`. The fix is in the tool's code, not its config. |
| `ErrConfigSourceOverrideUndeclared` | override for an undeclared config source | `OverrideConfigSource` names a slot the tool does not declare. It is refused so a stale override cannot add a layer; remove it or declare the slot. |

`ErrSignatureInvalid`, `ErrSignatureMissing`, `ErrWeakKey` and the other
key-resolution sentinels live in `gitlab.com/phpboyscout/go/signing/verify`;
`SelfUpdater` re-surfaces them during `Update()`. Full list on the
[module's reference](https://signing.go.phpboyscout.uk/reference/).

---

## `pkg/setup/ai`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrProviderNotLinked` | this tool does not link the chosen chat provider | `init ai` was given a provider the binary does not register, which would only fail at first use. Choose a linked provider. |
| `ErrNoKeyEntered` | no API key was entered | The key prompt closed with no key and no existing key to keep. The password prompt needs a terminal, so over a pipe it reads nothing. Run `init ai` at a terminal, or choose the env-var storage mode. |

---

## `pkg/setup/forge`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrForgeNotLinked` | forge feature enabled but its adapter is not linked | A forge feature is on in a binary that does not link its adapter. It is a build mistake, raised before configuration is read: link the forge's package from `main`. |
| `ErrNoTokenEntered` | no token was entered | The manual token prompt closed with no token. Enter one, or answer no to fetching a token and supply it yourself, through the variable the config names. |
| `ErrCredentialsIncomplete` | a username and an app password are both required | A Bitbucket credential mode was chosen, but the username or the app password was left empty. |
| `ErrPassphraseTooShort` | passphrase is too short | A generated SSH key's passphrase is too short. This includes accessible mode with no terminal, where no passphrase could be read. |

---

## `pkg/setup/flags`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNotABool` | flags: a feature's enabled value must be a boolean | A `features.<id>.enabled` value is not a boolean. Set it to `true` or `false`. |

---

## `pkg/chat`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrProviderUnset` | no AI provider is configured | A chat client was asked for with no provider in the caller's `Config`, `ai.provider` or `AI_PROVIDER`. The framework names no default vendor (spec 0196 D5): the tool's author sets one in its embedded defaults, and a user sets one with `init ai`. |

---

## `pkg/cmd/config`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrConvertTargetExists` | the file to convert to already exists | `config convert` will not overwrite the file it would write. Move or remove that file, then run it again. |

---

## `pkg/cmd/doctor`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrDoctorFoundProblems` | health checks reported problems | `doctor` found a failed check, or under `--fail-on warn` a gating warning, and has already printed its report. Advisory warnings never fail a run. It exits 1 and reports at warn; fix what the report names. |

---

## `pkg/credentialposture`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrMalformedKeychainRef` | a keychain reference is "service/account" | A `*.keychain` setting is not in `service/account` form. Correct the reference. |
| `ErrSecureStoreRegressed` | keychain unavailable; refusing to fall back to a plaintext credential | A credential established in the keychain would have fallen back to a plaintext copy, for example in a locked session or a container without a Secret Service. Unlock or restore the keychain, or remove the literal copy if it is stale. |

---

## `pkg/mcp`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrWithheldPathNotFound` | a withheld MCP path names no command | A path on the tool's MCP withhold list names no command, so it withholds nothing (spec 0213 D8). Correct or remove the path. |

---

## `pkg/release/static`

The static release channel's reader. The update command surfaces these with the
URL that failed.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrInvalidBaseURL` | release channel base URL must be an absolute http(s) URL | The configured channel base is not an absolute `http` or `https` URL. Fix `release_source.base_url`. |
| `ErrUnreachable` | the release channel could not be reached | No answer came back from the channel's host. Usually the network; retry. |
| `ErrUnexpectedStatus` | the channel answered with an unexpected HTTP status | A status that is neither success nor 404. The wrapped error carries the status. |
| `ErrNoReleasesPublished` | no release has been published on this channel yet | The `latest.json` pointer does not exist yet. Nothing to update to. |
| `ErrReleaseNotFound` | no release with that tag on this channel | No manifest exists for the tag asked for. Check the version. |
| `ErrNotFound` | the channel has no such object | An object a manifest names is not there: the release was published incompletely. |
| `ErrBrokenChain` | the release chain does not resolve | A manifest the pointer or a previous link names does not resolve, or the chain loops. The channel needs repairing on the publishing side. |
| `ErrTooLarge` | a channel document exceeded its size bound | A document exceeded its bound and was not read further. |
| `ErrInvalidManifest` | release manifest is not well formed | A document parses but is not a well-formed manifest. |
| `ErrUnsupportedSchema` | unsupported release manifest schema | A document's schema is newer than this binary knows. Install a newer release by hand. |
| `ErrEscapesBase` | release manifest names a URL outside the channel's base | A manifest named a URL outside the channel's base, so it was refused: a manifest cannot send the tool to a host the author did not name. Treat it as tampering until shown otherwise. |

---

## Config sources

Each config source kind is a link package under `pkg/config/sources/`, and a
slot of that kind is configured under `config.sources.<name>`. Most of these
errors are a slot missing a setting its kind needs. They surface while the
root builds the store, so the fix is the slot's settings rather than code.

### `pkg/config/sources/file`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoPath` | file config source has no path | Set `path`. |

### `pkg/config/sources/aws-s3`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoObject` | aws-s3 config source needs a bucket and a key | Set both `bucket` and `key`. |

### `pkg/config/sources/aws-secrets`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoName` | aws-secrets config source needs one of name or prefix | Set exactly one of `name` and `prefix`. |

### `pkg/config/sources/aws-ssm`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoPrefix` | aws-ssm config source has no prefix | Set `prefix` to the parameter path. |

### `pkg/config/sources/azure-appconfig`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoEndpoint` | azure-appconfig config source has no endpoint | Set `endpoint` to the store's endpoint. |

### `pkg/config/sources/azure-blob`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoBlob` | azure-blob config source needs a service_url, container and blob | Set all three of `service_url`, `container` and `blob`. |

### `pkg/config/sources/azure-keyvault`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoVault` | azure-keyvault config source has no vault_url | Set `vault_url`. |

### `pkg/config/sources/consul`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoPrefix` | consul config source has no prefix | Set `prefix` to the KV prefix. |

### `pkg/config/sources/vault`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoPath` | vault config source needs one of path or prefix | Set exactly one of `path` and `prefix`. |

### `pkg/config/sources/gcp-gcs`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoObject` | gcp-gcs config source needs a bucket and an object | Set both `bucket` and `object`. |

### `pkg/config/sources/gcp-parameter`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoParameter` | gcp-parameter config source needs one of parameter or prefix | Set exactly one of `parameter` and `prefix`. |

### `pkg/config/sources/internal/gcpsource`

Shared by the `gcp-gcs`, `gcp-parameter` and `gcp-secret` kinds.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoProject` | gcp config source has no project | Set `project`. Application Default Credentials name a principal, not the project to read, and guessing one could read another project's data. |

### `pkg/config/sources/internal/sourceauth`

Shared by the `consul` and `vault` kinds, which take a token.

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrLiteralUnderCI` | a literal source token is refused under CI | The slot's token is a literal in config and the run is under CI. Name a variable in `config.sources.<name>.auth.env` instead. |

### `pkg/config/sources/keychain`

| Error | Message | Typical Handling |
|-------|---------|-----------------|
| `ErrNoService` | keychain config source has no service | Set `service`. |
| `ErrNoKeychain` | no keychain is available | The binary links no keychain backend. The tool's author links `pkg/setup/keychain` from `main`. |
| `ErrKeychainUnreachable` | keychain did not answer its probe | The keychain failed its reachability probe, for example behind a wedged D-Bus session or awaiting an unlock. Unlock it, or raise the slot's `timeout`. |

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
| `cli/pkg/cmd/generate` | `ErrEmptyCommandPath`, `ErrCommandNotFound`, `ErrUpdateManifestFailed` |
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
2. Add a row to this catalogue with its message and handling guidance, under
   the heading that names its package (`## \`pkg/mypackage\``). Two packages
   can share a sentinel name, so a row elsewhere does not count.
   A test in `internal/repopolicy` checks every exported `Err` variable in
   `pkg/` has a row, and that every row names one that exists.
3. Use `errors.Wrap(err, "context")` to add call-site context when returning
   the error through multiple layers.
