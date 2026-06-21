---
title: "Linux package signing & packaging — GPG-signed .deb/.rpm via nfpm and signed repository metadata"
description: "Extend GTB's OpenPGP/WKD trust chain to Linux OS package managers. Add an nfpms: block to the release pipeline that builds GPG-signed .deb/.rpm packages, publish the public key over the existing WKD tree, and assess apt/yum repository-metadata signing (Release/InRelease, repomd.xml) as in-scope or follow-on. Make the self-updater aware of package-manager installs so it defers to the OS package manager rather than overwriting a dpkg/rpm-owned binary."
status: DRAFT
date: 2026-06-21
tags:
  - specification
  - signing
  - packaging
  - linux
  - nfpm
  - deb
  - rpm
  - apt
  - yum
  - openpgp
  - goreleaser
  - update
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# Linux package signing & packaging — GPG-signed `.deb`/`.rpm` via nfpm and signed repository metadata

Authors
:   Matt Cockayne

Date
:   21 June 2026

Status
:   DRAFT

---

## Summary

GTB already mints OpenPGP keys (`pkg/openpgpkey`), publishes them over a Web Key
Directory (`gtb keys wkd`, spec `2026-06-09-keys-wkd-command.md`), and produces a
detached OpenPGP signature over `checksums.txt` in the release pipeline
(`.goreleaser.yaml` `signs:` block → `scripts/sign-release.sh` → `gtb sign
--backend aws-kms`, spec `2026-06-10-signing-release-pipeline.md`). The
self-updater verifies that signature against an embedded trust set before
replacing the running binary (`pkg/setup/update_signature.go`, spec
`2026-04-02-remote-update-checksum-verification.md`).

That trust chain covers the **tarball + self-update** distribution path and the
**macOS** path (Apple notarization, `.goreleaser.yaml` `notarize:` block). It
does **not** cover the **Linux OS package manager** path. There is no `nfpms:`
block in `.goreleaser.yaml` today — GTB ships no `.deb` or `.rpm`, so there is no
`apt install gtb` / `dnf install gtb` story, and no trust equivalent to
notarization for users who install via their distro's package manager.

On Linux the trust equivalent of macOS notarization is two layers of
GPG signing:

1. **Package signing** — each `.deb`/`.rpm` carries an embedded GPG signature
   (`debsigs`/RPM header signature) that `dpkg`/`rpm` verify against an imported
   public key.
2. **Repository metadata signing** — the apt `Release`/`InRelease` files and the
   yum `repomd.xml` are GPG-signed so `apt update`/`dnf` trust the index that
   lists package hashes, closing the gap between "this package is signed" and
   "this is the package the repo says you should get".

This spec adds package signing (layer 1) to the release pipeline, reusing the
existing OpenPGP key and WKD publication so there is **one trust root**, and
assesses repository-metadata signing (layer 2) as in-scope or a documented
follow-on (see [Open Questions](#open-questions)). It also makes the self-updater
**install-method aware** so a package-manager install is not clobbered by an
in-place binary swap.

This is feature item **A2** (Linux package signing & packaging). The decision
logs (`docs/development/feature-decisions.md`,
`docs/development/security-decisions.md`) contain **no prior decision on OS
packaging** — this is open territory, explicitly noted as deferred future work in
`2026-04-02-remote-update-checksum-verification.md` ("Package managers (apt, yum,
etc.) — future … when added, they have their own signing requirements"). No
conflict exists with an earlier decision.

## Motivation

- **Trust parity across platforms.** macOS users get a notarized binary;
  Homebrew users get a tap-pinned cask. Linux users who reach for their native
  package manager currently get *nothing* — they must download a tarball and
  trust it out-of-band. A signed `.deb`/`.rpm` brings Linux to parity.
- **The key already exists.** GTB mints and publishes an OpenPGP signing key over
  WKD. Package signing wants exactly such a key. Minting a *second*, unrelated
  GPG key for packaging would fragment the trust root and confuse downstreams who
  have already imported the WKD key to verify `checksums.txt.sig`. We reuse the
  one key.
- **`apt`/`dnf` are how Linux operators actually install software.** A framework
  that scaffolds CLI tools should let those tools ship through the channel their
  operators already trust and automate (`apt-get install`, Ansible `apt`/`dnf`
  modules, immutable-image base layers).
- **The self-updater will otherwise fight the package manager.** Today
  `SelfUpdater.resolveTargetPath` (`pkg/setup/update.go:826`) resolves the
  running executable's path and overwrites it in place. If `gtb` was installed to
  `/usr/bin/gtb` by `dpkg`, an in-place self-update silently desynchronises the
  package database from the on-disk binary — the next `apt upgrade` may revert or
  conflict. The updater must detect this and defer.

## Goals

- Add an `nfpms:` block to `.goreleaser.yaml` that builds `.deb` and `.rpm`
  (and optionally `.apk`) for `linux/amd64` and `linux/arm64`, GPG-signed with
  GTB's existing OpenPGP key.
- Reuse the existing WKD-published OpenPGP key as the single packaging trust
  root; document the import step (`apt-key`-free, keyring-drop pattern) and the
  `rpm --import` step.
- Make `internal/generator` scaffold the same `nfpms:` block into a generated
  tool's `.goreleaser.yaml` when signing/packaging is enabled, manifest-driven,
  consistent with how the `signs:` block is already generated (spec
  `2026-06-10-signing-release-pipeline.md`).
- Make `pkg/setup.SelfUpdater` detect a package-manager-owned install and refuse
  / redirect the in-place update with an actionable hint.
- Decide, and record, whether signed apt/yum **repository metadata** is in this
  spec or a follow-on.

## Non-Goals

- **Hosting an apt/yum repository.** Whether GTB publishes a live signed repo
  (e.g. an `apt` repo on GitLab Pages / object storage with a stable URL) is a
  distribution-infrastructure decision separate from *producing* signed
  artefacts. This spec produces signed packages and (per the open question) may
  produce signed metadata; standing up the served repository is tracked
  separately if pursued.
- **Windows packaging** (MSI/winget/Chocolatey) — out of scope; a sibling item.
- **Replacing the self-update mechanism for package-manager installs.** The
  updater becomes *aware* of package installs and defers; it does not learn to
  drive `apt`/`dnf`.
- **A new signing backend.** This spec reuses `pkg/signing` and the existing
  OpenPGP key material; it does not add a packaging-specific backend (but see the
  KMS tension in [Design](#the-kms-vs-local-key-tension) and the open questions).

## Background: the existing trust chain (anchor files)

| Concern | Where it lives today |
|---------|----------------------|
| OpenPGP key minting | `pkg/openpgpkey/openpgpkey.go` (`ArmoredPublicKey`, `Entity`), `gtb keys mint` |
| Detached OpenPGP signing | `pkg/openpgpkey/sign.go` (`DetachSign`) |
| WKD tree generation | `pkg/openpgpkey/wkd.go` (`WriteWKDTree`), `gtb keys wkd` (spec `2026-06-09-keys-wkd-command.md`) |
| Release-checksum signing (produce) | `.goreleaser.yaml` `signs:` → `scripts/sign-release.sh` → `gtb sign --backend aws-kms` (spec `2026-06-10-signing-release-pipeline.md`) |
| Checksum-signature verification (consume) | `pkg/setup/update_signature.go` (`verifyManifestSignature`, `verifyAgainstTrustSet`) (spec `2026-04-02-remote-update-checksum-verification.md`) |
| macOS notarization | `.goreleaser.yaml` `notarize:` block |
| Self-update target resolution | `pkg/setup/update.go:826` (`resolveTargetPath`) |
| Generator signing wiring | `internal/generator/assets/skeleton/.goreleaser.yaml`, `internal/cmd/enable/signing.go` (spec `2026-06-10-signing-release-pipeline.md`) |

The notable gap: the produce-side signing currently signs **only the checksum
manifest**, with a KMS-backed key, via a custom command. OS package signing
signs **the package files themselves** and (for nfpm) requires a **local GPG
key file**, not a custom command — this is the central design tension below.

## Design

### The `nfpms:` block

Add to `.goreleaser.yaml`:

```yaml
nfpms:
  - id: gtb-packages
    package_name: gtb
    ids:
      - gtb                      # the linux/windows build id; nfpm filters to linux
    formats:
      - deb
      - rpm
      # - apk                    # gated on the apk open question
    maintainer: "Matt Cockayne <matt@phpboyscout.uk>"
    homepage: "https://gitlab.com/phpboyscout/go-tool-base"
    description: "A helper utility for interacting and managing gtb repos and resources"
    license: "<spdx-id>"
    bindir: /usr/bin
    file_name_template: >-
      {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
    deb:
      signature:
        key_file: "{{ .Env.NFPM_GPG_KEY_FILE }}"
        type: origin
    rpm:
      signature:
        key_file: "{{ .Env.NFPM_GPG_KEY_FILE }}"
```

GoReleaser's nfpm support signs packages with a **local GPG private-key file**
(`deb.signature.key_file` / `rpm.signature.key_file`), with the passphrase
supplied via `$NFPM_<ID>_<FORMAT>_PASSPHRASE` / `$NFPM_<ID>_PASSPHRASE` /
`$NFPM_PASSPHRASE`. It does **not** accept a custom signing command, and it does
**not** sign repository metadata.

### The KMS-vs-local-key tension

This is the load-bearing design problem and the reason this spec exists as more
than a one-line config addition.

GTB's checksum signing keeps the private key **in AWS KMS** — it never touches
disk (`scripts/sign-release.sh`, `gtb sign --backend aws-kms`; the signer's IAM
role is OIDC-derived in CI, `.gitlab-ci.yml`). nfpm package signing requires the
private key as a **file on disk** in the CI runner. These are incompatible: nfpm
cannot call out to KMS.

Three resolutions, to be chosen in review (see [Open Questions](#open-questions)):

1. **Separate file-based packaging subkey (recommended).** Mint a dedicated
   OpenPGP *signing subkey* under the same primary identity (the WKD-published
   key), export its private half, and store it as a masked/protected CI variable
   (`NFPM_GPG_KEY_FILE` materialised from a file-type CI variable; passphrase in
   `NFPM_PASSPHRASE`). The packaging key shares the WKD trust root (same primary
   key → users import one key) but is a distinct subkey with a file-based private
   half, isolating the KMS-protected primary/checksum key from disk exposure.
   `pkg/openpgpkey` already builds entities and we have `gtb keys mint`; this
   spec extends minting to emit an exportable packaging subkey.
2. **Reuse the KMS public key only, sign packages out-of-band.** Pre-sign
   packages with a separate `gtb sign`-driven step that wraps each package, then
   feed pre-built signed artefacts to GoReleaser. Rejected as fragile — it
   reimplements nfpm's embedded-signature format (debsigs / RPM header) in GTB
   rather than letting nfpm own it.
3. **Accept a file-based key for all signing.** Drop KMS for checksums too and
   use one on-disk key everywhere. Rejected — it regresses the KMS hardening that
   `2026-06-10-signing-release-pipeline.md` deliberately landed.

Resolution **(1)** keeps the KMS guarantee for the checksum/self-update path and
gives packaging a purpose-built, file-exportable subkey under the same trust
root. The CI job gates nfpm signing on `NFPM_GPG_KEY_FILE` being present, exactly
as checksum signing gates on `AWS_WEB_IDENTITY_TOKEN` and notarization gates on
`APPLE_DEV_CERT` (`--skip` the relevant GoReleaser steps when the secret is
absent, so forks and snapshot builds still succeed).

### Trust root and key import (WKD reuse)

The packaging key is published in the **same WKD tree** already produced by `gtb
keys wkd`. Downstream import documentation (in `docs/`):

- **apt:** drop the dearmored public key into
  `/usr/share/keyrings/gtb-archive-keyring.gpg` and reference it with
  `signed-by=` in the `.sources`/`.list` entry (the modern, `apt-key`-free
  pattern).
- **rpm/dnf:** `rpm --import <public-key.asc>` (or a `gpgkey=` line in the
  `.repo` file), with `gpgcheck=1`.

Because the key is the same primary identity already on WKD, an operator who has
imported the GTB key to verify `checksums.txt.sig` does not import a second key.

### Repository-metadata signing (layer 2) — scope decision

Package signing alone proves *a* package is authentic but not that it is the
*right* one for a given version (downgrade/rollback and index-substitution
defence comes from signed metadata). Full `apt`/`yum` repo-metadata signing
(`Release` → `InRelease`/`Release.gpg`, `repomd.xml` → `repomd.xml.asc`) is a
property of the **served repository**, which nfpm/GoReleaser do not produce.

This spec's position: **package signing (layer 1) is in-scope now**;
repository-metadata signing (layer 2) is **deferred to a follow-on** tied to the
repository-hosting decision (a Non-Goal here), unless review decides otherwise
(see [Open Questions](#open-questions)). Until a served repo exists, users
install a directly-downloaded signed `.deb`/`.rpm` (`apt install ./gtb_*.deb`,
`dnf install ./gtb-*.rpm`), where the embedded package signature is the operative
trust check and metadata signing has no surface to protect.

### Self-updater install-method awareness

`SelfUpdater` must not overwrite a binary owned by `dpkg`/`rpm`. Add a detection
step in the update path (before `resolveTargetPath` overwrites in place):

- Resolve the target path as today (`pkg/setup/update.go:826`).
- Classify the install: query whether the resolved path is owned by a package
  manager. Pure-Go, no shell-out where avoidable:
  - **dpkg:** the path appears under a `*.list` in `/var/lib/dpkg/info/` (read the
    directory; no `dpkg-query` shell-out required), or fall back to
    `dpkg -S <path>` via `internal/exectest`-injectable `exec` if present.
  - **rpm:** `rpm -qf <path>` (injectable exec), or check membership without
    shelling out where feasible.
- If package-manager-owned, **refuse the in-place swap** and emit an
  `errorhandling` hint: e.g. *"gtb was installed by your system package manager
  (dpkg). Update with `apt update && apt install --only-upgrade gtb` instead, or
  reinstall the tarball build to enable self-update."* Honour `--force` to
  override for power users who accept the desync.
- This classification is exposed as a small, testable helper (its own file under
  `pkg/setup/`), injected like the existing `execLookPath`/`osExecutable`
  seams so it is race-safe under `t.Parallel()` (no package-level `var exec…`
  hooks — per the project testing rules, use struct-field/functional-option
  injection and `internal/exectest`).

### Generator parity

`internal/generator` already scaffolds the `signs:` block driven by the manifest
`Signing` block (`internal/generator/assets/skeleton/.goreleaser.yaml`, gated on
`{{ if and .Signing.Enabled .Signing.KeyID }}`). Add a parallel, manifest-driven
`nfpms:` block (e.g. gated on a `Packaging.Enabled` field, populated by a `gtb
enable packaging` command analogous to `gtb enable signing` in
`internal/cmd/enable/signing.go`). The generated block references the consumer's
own packaging key path/env, not GTB's. Any user-influenced field flowing into the
generated YAML (maintainer, description, package name, license) routes through the
existing `escapeYAML` helper per `internal/generator/template_escape.go` and
`docs/development/template-security.md`.

## Public API / surface changes

- `.goreleaser.yaml` — new `nfpms:` block (GTB's own release).
- `.gitlab-ci.yml` — `goreleaser` job materialises `NFPM_GPG_KEY_FILE` from a
  protected file variable and sets `NFPM_PASSPHRASE`; gates nfpm signing on its
  presence (skip otherwise).
- `pkg/openpgpkey` and/or `gtb keys mint` — extend to emit an exportable
  packaging subkey under the existing primary identity (resolution 1).
- `pkg/setup.SelfUpdater` — new install-method classification + refusal path;
  new injectable seam(s) for `dpkg`/`rpm` ownership queries; `--force` override.
- `internal/generator` — new manifest `Packaging` block and scaffolded `nfpms:`
  template; `gtb enable packaging` command (mirrors `gtb enable signing`).
- Docs: a Linux install/verify page (`apt`/`dnf` import + install + verify),
  cross-referenced from `docs/components/` and the signing docs.

## Testing strategy

- **Unit (≥90% for new `pkg/` code):**
  - Packaging-subkey minting in `pkg/openpgpkey` (entity has the expected primary
    identity + subkey; armored export round-trips).
  - `SelfUpdater` install-method classification: table-driven over (dpkg-owned,
    rpm-owned, tarball/unowned, ambiguous) using injected fake exec /
    fake-filesystem (`afero`, `internal/exectest`), asserting refusal + hint vs
    proceed, and `--force` override. `t.Parallel()` throughout, no package-level
    mock hooks.
  - Generator: `nfpms:` block renders only when `Packaging.Enabled`; escaping of
    user fields via `escapeYAML`; golden-file comparison like the existing
    `signing_goreleaser_test.go`.
- **Integration (env-gated, `testutil.SkipIfNotIntegration`):** build a real
  signed `.deb`/`.rpm` via `goreleaser --snapshot` and verify the embedded
  signature with `dpkg-sig --verify` / `rpm --checksig` against the imported
  public key. Desktop/CI-gated (needs `dpkg`/`rpm` tooling), in a
  `*_integration_test.go` file, per `docs/development/integration-testing.md`.
- **E2E (Godog):** a CLI scenario asserting that `gtb update` on a
  package-manager-owned binary prints the defer-to-apt hint and exits without
  swapping the binary (fake-owned path), per the BDD strategy spec.
- **Manual release rehearsal:** `just snapshot` and confirm `dist/` contains
  signed `.deb`/`.rpm`; `goreleaser check` passes with the new block.

## Migration / compatibility notes

- Pre-1.0: additive. No existing public type changes signature; the self-updater
  gains a refusal path that only triggers for package-manager installs (the
  tarball/self-update path is unchanged). Note the updater behaviour change in
  the commit body and a `docs/migration/` note for downstreams that embed
  `pkg/setup` and may now see a refusal where an overwrite previously occurred.

## Open Questions

> Per project convention, resolve or explicitly defer each of these before
> implementation begins.

1. **KMS-vs-local-key resolution.** Adopt resolution (1) — a separate
   file-based packaging subkey under the WKD primary identity — or an
   alternative? This determines the key-management and CI-secret design.
2. **Repository-metadata signing scope.** Is signed `apt`/`yum` repo metadata
   (`InRelease`, `repomd.xml.asc`) in *this* spec, or a follow-on gated on the
   repo-hosting decision? Current proposal: follow-on. If in-scope now, GTB needs
   a metadata-signing step (likely a `gtb sign`-driven post-build hook, since
   nfpm does not do this).
3. **Served repository.** Will GTB host a live apt/yum repo (stable URL, e.g.
   GitLab Pages / object storage), or only attach signed `.deb`/`.rpm` to the
   Release for direct download? Layer-2 signing is moot without a served repo.
4. **`.apk` (Alpine) inclusion.** nfpm supports APK signing too. Ship Alpine
   packages in the initial cut, or defer? (Affects formats list and key-name
   handling.)
5. **Updater refusal vs. silent-defer for non-interactive contexts.** In CI/cron
   (non-interactive), should a package-manager-owned update hard-fail
   (non-zero), or warn-and-no-op? Mirror the existing non-interactive handling in
   `resolveTargetPath` (which warns and proceeds) or invert it for package
   installs?
6. **Single primary key vs. dedicated packaging identity.** Resolution (1) keeps
   one primary identity with a packaging subkey. Is there a compliance reason a
   downstream would want a *fully separate* packaging key (different fingerprint,
   separate WKD entry)? If so, the generator's `Packaging` block must support an
   independent key, not just a subkey of the signing key.
7. **`gtb enable packaging` vs. folding into `gtb enable signing`.** Is packaging
   a separate enable command, or a flag on the existing signing enablement
   (since it shares the trust root)?

## References

- `.goreleaser.yaml` — existing `signs:`, `sboms:`, `notarize:`,
  `homebrew_casks:` blocks; absent `nfpms:`.
- `pkg/openpgpkey/` — `DetachSign`, `WriteWKDTree`, `ArmoredPublicKey`, `Entity`.
- `pkg/setup/update.go` (`resolveTargetPath`, line 826), `update_signature.go`.
- `internal/generator/assets/skeleton/.goreleaser.yaml`,
  `internal/cmd/enable/signing.go`.
- Spec `2026-06-09-keys-wkd-command.md` — WKD generation.
- Spec `2026-06-10-signing-release-pipeline.md` — generated `signs:` block, KMS shim.
- Spec `2026-04-02-remote-update-checksum-verification.md` — checksum verify;
  notes apt/yum as deferred future work with their own signing requirements.
- `docs/development/template-security.md` — `escapeYAML` for generated YAML fields.
- `docs/development/integration-testing.md`, BDD strategy spec — test gating.
- GoReleaser nfpm docs — `deb.signature.key_file` / `rpm.signature.key_file`,
  `$NFPM_*_PASSPHRASE`; signs with a **local GPG key file** (no custom command);
  does **not** sign repository metadata.
