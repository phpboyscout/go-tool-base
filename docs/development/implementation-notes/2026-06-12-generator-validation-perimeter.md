---
title: "Implementation notes — generator validation perimeter"
description: "What was implemented for the 2026-06-12 generator-validation-perimeter spec, deviations from the letter of the spec, and open questions for review."
date: 2026-06-13
tags: [development, security, generator, implementation-notes]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Implementation notes — generator validation perimeter

Spec: [2026-06-12-generator-validation-perimeter](../specs/2026-06-12-generator-validation-perimeter.md)

## What was implemented

### D1 — `ValidateCommandName` at every entry point

- `internal/generator/validate.go` gains `ValidateCommandName` enforcing NFC-normalised `^[a-z][a-z0-9_-]{0,63}$`, with `/`, `\`, and `.`/`..` rejected explicitly (before the character-class check, for a clearer message) and `root` / `options` reserved.
- `ValidateManifest` now walks `m.Commands` recursively (`validateManifestCommands`) and the four rendered `Signing` fields (`validateManifestSigning`).
- **Skip-not-abort on the regenerate path**: `regenerateProjectFiles` calls `sanitiseManifest` before `ValidateManifest`. Invalid commands are dropped from the *in-memory* manifest with an ERROR log naming the (truncated) command and the rule it failed; an invalid signing block is zeroed the same way. Valid entries still regenerate; the on-disk manifest is untouched. `ValidateManifest` then hard-fails only on structural fields (`Properties`, `ReleaseSource`).
- **CLI hard errors**: `gtb generate command` (non-interactive and interactive form) and `gtb remove command` validate the typed name via `ValidateCommandName`, and the `--parent` path via a new `ValidateParentPath` (each `/`-separated segment must be a valid command name; the literal `root`/empty is the root command).
- **Chokepoint defence (beyond the spec letter)**: `getCommandPath` (`internal/generator/ast.go`) now resolves through `containedCommandPath`, which rejects any result that is not strictly under `<project>/pkg/cmd` (`filepath.Rel` check). This forecloses the `MkdirAll`/`RemoveAll` sink for *every* caller — including `gtb generate flag` and `SetProtection`, which read command names from the manifest without going through `ValidateManifest`.

### D2 — `ManifestSigning` escaped and validated

- `internal/generator/assets/skeleton/.goreleaser.yaml`: `Backend`, `KMSRegion`, `KeyID`, `PublicKey` are now piped through `escapeYAML` (which double-quotes, so the rendered output is byte-identical for clean values — golden assertions unchanged).
- New validators: `ValidateSigningBackend` (`^[a-z][a-z0-9-]{0,31}$`), `ValidateSigningKMSRegion` (`^[a-z][a-z0-9-]{0,31}$`), `ValidateSigningKeyID` (`^[a-zA-Z0-9:/_.=+,@-]{1,256}$`, plus a literal `..` substring is rejected as defence-in-depth), `ValidateSigningPublicKey` (clean relative path under the project, slash-separated, a single leading `./` normalised away, segment class `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`, no absolute/`..`/backslash). All accept empty (optional fields).
- Hard-error call sites for typed input: `gtb generate project` (`validateSigningFields` extended; backend was already checked against the registered-backend set) and `Generator.EnableSigning` (covers `gtb enable signing` flags and the interactive path).
- `GenerateSkeleton` itself deliberately does **not** validate signing (the CLI boundary does), which is what lets the defence-in-depth escaping test (`TestGenerateSkeleton_SignsBlock_EscapesHostileValues`) exercise the render path with hostile values.

### D3 — AI doc-tool containment

`handleReadFileTool` and `handleListDirTool` (`internal/generator/docs.go`) now resolve through `containedProjectPath`, the same `filepath.Abs` + `filepath.Rel` containment the go-doc path uses; any result escaping the project root is rejected. No `BasePathFs` introduced.

### D4 — placeholder-free sentinel

`ErrNotGoToolBaseProject` no longer embeds `%s` (and `internal/generator/errors.go` now uses `cockroachdb/errors`). Call sites attach the path with `errors.Wrapf`, so `errors.Is` matches and no literal `at '%s'` can render.

## Verification

- `just test` — green (includes the new traversal-regression, signing-injection, containment, and sentinel tests).
- `just test-race` — green. One unrelated pre-existing flake observed once and not reproduced: `TestRunSign_RefusesOutputEqualsInput` panicked with `signing: duplicate Backend registration: fake` (global registry + test ordering in `internal/cmd/sign`; passes in isolation and on a clean re-run, also occurs without these changes).
- `just lint` — 0 issues (after `golangci-lint cache clean`; the cache held results from a deleted sibling worktree).
- Scaffold check performed with the real binary: `generate project --signing --signing-key-id …` → well-formed signs block; `regenerate project` reproduces it; a hand-tampered manifest (`commands: [{name: "../../evil-escape"}]`, `key_id: "x\"\n  - artifact: pwned"`) regenerates the valid command, writes nothing outside the tree, drops the signs block, and emits two ERROR logs; `tmp/` deleted afterwards.

## Deviations from the spec (and why)

1. **`sanitiseManifest` + `ValidateManifest`, not a softer `ValidateManifest`.** The spec asks both that `ValidateManifest` walk `Commands`/`Signing` *and* that the manifest path skip rather than abort. Implemented as: sanitise first (skip + ERROR log), then run the unchanged-semantics `ValidateManifest` (which now walks everything) over the sanitised manifest. Callers that want a hard verdict on a raw manifest still get one from `ValidateManifest` alone.
2. **`ValidateParentPath` and the `getCommandPath` containment chokepoint are additions.** The spec's named entry points leave the `gtb generate flag` path (manifest-sourced command names, no `ValidateManifest` call) able to reach the join sink. The chokepoint closes that class without behavioural impact on valid names.
3. **Interactive form validation messages**: the huh form fields surface the validator *hint* (field + rule + truncated input) rather than the bare `invalid generator input` sentinel text, via a small adapter in `internal/cmd/generate/command.go`.

## Open questions for review

1. **`Backend` character class** — `^[a-z][a-z0-9-]{0,31}$` (matches `aws-kms`, `local`). The CLI additionally checks membership of the registered-backend set; `ValidateManifest` checks only the class so manifests written for a newer gtb with more backends don't fail on older binaries. Tight enough?
2. **`KeyID` character class** — `^[a-zA-Z0-9:/_.=+,@-]{1,256}$` admits KMS ids, ARNs, aliases, and the local backend's PEM paths (e.g. `./release.pem`, which the existing test suite uses). It therefore also admits `..` sequences — accepted deliberately because `KeyID` is never used as a generator-side filesystem path, only rendered (escaped) into the signs block. Reject `..` anyway? **RESOLVED (Matt's review): reject `..`.** `ValidateSigningKeyID` now rejects any value containing a literal `..` substring (defence-in-depth, mirroring `ValidateCommandName`), checked before the character-class match. Legitimate KMS ids/ARNs/aliases and `./release.pem` never contain `..`, so this is a no-op for real values.
3. **`KMSRegion`** — `^[a-z][a-z0-9-]{0,31}$` rather than a strict AWS-region grammar (`^[a-z]{2}(-[a-z0-9]+)+$`), to avoid breaking on future region formats. Strict enough?
4. **`PublicKey` cleanliness** — `./key.asc` is rejected (not a *clean* path: `path.Clean` ≠ input). The default and all documented values pass. Acceptable, or should a leading `./` be normalised instead? **RESOLVED (Matt's review): normalise a leading `./`.** `ValidateSigningPublicKey` now strips a single leading `./` (`strings.TrimPrefix`) before the cleanliness check, so `./key.asc` and `./sub/key.asc` are accepted (treated as `key.asc` / `sub/key.asc`). Absolute paths, backslashes, `..` escapes, and any residual unclean form (e.g. `././key.asc`, `./../escape.asc`) are still rejected — the path must resolve cleanly inside the project root after normalisation. The default (`internal/trustkeys/keys/signing-key-v1.asc`) is unaffected.
5. **Skip log shape** — ERROR level, message `Skipping command with invalid name in manifest` / `Skipping signing-block rendering: invalid signing configuration in manifest`, with `command` (truncated to 32 runes) and `reason` (the validator hint) attributes. Note the hint includes the truncated offending value, which slightly relaxes validate.go's "never echo the offending value above DEBUG" doc comment — the spec explicitly asked for the ERROR log to name the command. OK?
6. **Stricter command-name rule vs existing manifests** — `ValidateCommandName` rejects names with dots or uppercase that the old (`no spaces`, not-`options`/`root`) checks allowed. Any pre-existing project whose manifest has such a command will now see that command skipped on regenerate (with an ERROR log) and a hard error if re-typed at the CLI. No manifest in this repo or its test fixtures is affected. Does this need a migration note in the changelog?
7. **`ExternalKeyEmail` is not validated** — it renders only into generated Go source via jennifer (`jen.Lit`, auto-escaped) and was out of the spec's four fields. Confirm leaving it.
