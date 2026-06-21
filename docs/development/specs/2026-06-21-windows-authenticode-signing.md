---
title: "Windows Authenticode code-signing for release binaries"
description: "Authenticode-sign the Windows release executables (gtb_*.exe) so they ship with a verifiable publisher identity and accrue SmartScreen reputation, closing the one platform gap left by Apple notarization (macOS) and OpenPGP checksum signing (cross-platform self-update). Evaluates Azure Trusted Signing (Microsoft cloud HSM, OIDC-federated, mirrors the existing AWS-KMS signing pattern) against a traditional OV/EV certificate driven by signtool/osslsigncode/jsign, and wires the chosen path through GoReleaser's generic per-binary signing hook gated on credentials being present — the same dormant-until-provisioned discipline as the existing notarize: and signs: blocks."
status: DRAFT
date: 2026-06-21
tags:
  - specification
  - signing
  - windows
  - authenticode
  - release
  - security
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Windows Authenticode code-signing for release binaries

Authors
:   Matt Cockayne

Date
:   2026-06-21

Status
:   DRAFT — for human review. Item **A1** from the release-hardening backlog.

---

## Summary

GTB's Windows release artefacts (`gtb_Windows_x86_64.tar.gz` containing
`gtb.exe`, and the `arm64` equivalent — see the `gtb` build in
[`.goreleaser.yaml`](../../../.goreleaser.yaml) lines 26–42) ship
**unsigned**. On Windows the trust signal is **Authenticode**: an
embedded PKCS#7 signature carrying a publisher identity, plus
**SmartScreen reputation** that the publisher accrues over time and
download volume. Without it, Windows users get a "Windows protected your
PC / unknown publisher" SmartScreen prompt and `Get-AuthenticodeSignature`
reports `NotSigned`.

This is the one platform gap left after the two signing tracks already in
the tree:

| Track | Covers | Mechanism | Anchor |
|-------|--------|-----------|--------|
| Apple notarization | macOS Gatekeeper, pre-execution | Developer ID cert + Apple notary, OIDC-issued API key | `notarize: macos:` block, `.goreleaser.yaml` L44–55 |
| OpenPGP checksum signing | self-update integrity, all OSes | detached sig over `checksums.txt`, AWS KMS via `gtb sign` | `signs:` block L92–98; [sign-command spec](2026-06-09-sign-command.md) |
| **Windows Authenticode** | **SmartScreen / publisher trust, pre-execution** | **this spec** | **none yet** |

The honest framing, stated up front so the spec does not over-promise:
**Windows has no notary service analogous to Apple's.** There is no
Microsoft endpoint that scans a binary and returns an approval ticket.
The trust signal is (a) a valid Authenticode signature chaining to a CA
in the Windows Trusted Root Program, and (b) SmartScreen reputation,
which is **earned over time** and is **stronger with an EV certificate**
(EV gets immediate reputation; OV builds it gradually). No code change can
shortcut reputation — this spec delivers the signature; reputation
follows from shipping signed builds consistently.

The recommendation (see [D1](#d1--signing-backend-azure-trusted-signing))
is **Azure Trusted Signing** (Microsoft's cloud-HSM signing service,
formerly Azure Code Signing): it is cheap (~US$10/month), holds the
private key in an FIPS 140-2 Level 3 HSM, issues **short-lived
certificates** per signing request, and authenticates via **OIDC
workload-identity federation** — which mirrors, almost one-for-one, the
AWS-KMS OIDC pattern this project already runs for the OpenPGP signer
(`.gitlab-ci.yml` L121–147). The traditional OV/EV-cert-on-disk path is
evaluated as the rejected alternative.

---

## Motivation

1. **User-facing friction.** An unsigned `.exe` triggers SmartScreen's
   "unknown publisher" warning. For a developer CLI this is survivable but
   unprofessional, and it actively deters first-run adoption on locked-down
   corporate Windows estates where unsigned executables are blocked by
   policy (AppLocker / WDAC / SmartScreen-for-Explorer enforced).

2. **Parity with the other platforms.** macOS already gets a
   Gatekeeper-clean experience via notarization. Windows is the outlier.
   The [remote-update spec](2026-04-02-remote-update-checksum-verification.md)
   (L66) explicitly named Windows Authenticode as **out of scope, separate
   future work that could complement** the OpenPGP track. This spec is that
   follow-up.

3. **The pattern is already in the building.** The OpenPGP signer
   established the entire shape we need: a cloud-HSM-held key, OIDC
   federation from GitLab CI, a credentials-gated GoReleaser hook that runs
   `--skip` when the secret is absent, and a thin pluggable backend in
   `pkg/signing/`. Windows Authenticode is the same problem with a
   different output format (PKCS#7 embedded in a PE file rather than a
   detached OpenPGP packet). Reuse, not reinvention.

### Relationship to existing signing (no overlap)

| Property | Apple Notarization | OpenPGP checksum sig | **Windows Authenticode** |
|----------|--------------------|----------------------|--------------------------|
| Platform | macOS | all | Windows |
| Trust anchor | Apple Developer ID / notary | project GPG key (embedded + WKD) | Authenticode CA in Windows Trusted Root Program |
| What it asserts | not-known-malware, registered dev | provenance of the self-update manifest | publisher identity of the `.exe` itself |
| Checked when | macOS Gatekeeper, pre-exec | GTB self-update, pre-install | Windows SmartScreen/loader, pre-exec |
| Reputation? | n/a | n/a | **yes — earned over time, instant with EV** |

All three are retained and complementary. Authenticode signs the PE
binary so Windows trusts it at launch; OpenPGP signs the checksums
manifest so `gtb update` trusts the download chain. A fully-protected
Windows release wants **both** — Authenticode for the Explorer/SmartScreen
launch path, OpenPGP for the in-binary self-update path.

---

## Decision-log check (no conflict)

Neither [`docs/development/feature-decisions.md`](../feature-decisions.md)
nor [`docs/development/security-decisions.md`](../security-decisions.md)
contains any entry addressing Windows signing, Authenticode, SmartScreen,
signtool, osslsigncode, jsign, or Azure Trusted Signing (verified by
grep, 2026-06-21). This is **open territory**, consistent with the
remote-update spec's explicit deferral. No rejected-feature or
accepted-risk entry conflicts with proceeding. On acceptance, a
**security-decisions** entry should record the chosen trust anchor (which
CA / signing service) and the residual risks in
[Trust & threat model](#trust--threat-model).

---

## Background: how Authenticode works (and what GoReleaser does/doesn't do)

Authenticode embeds a PKCS#7 SignedData structure into the PE file's
certificate table (the security directory). Signing tools compute a hash
over the PE's authenticode-relevant bytes, sign that hash with the
publisher's private key, and append the signature + cert chain + a
**timestamp countersignature** (RFC 3161) so the signature stays valid
after the signing certificate expires.

The tooling landscape:

- **`signtool.exe`** — Microsoft's native tool. Windows-only. The
  canonical signer; required for some EV/HSM flows.
- **`osslsigncode`** — cross-platform OpenSSL-based reimplementation.
  Runs on Linux CI. Historically the go-to for signing Windows binaries
  from a Linux runner, but the ecosystem is moving away from it because
  modern signing increasingly uses **short-lived certificates** that
  osslsigncode's static-cert model does not fit.
- **`jsign`** — Java-based, cross-platform, and the tool with first-class
  **Azure Trusted Signing** support (it speaks the Trusted Signing REST
  API and handles the per-request short-lived cert).

**GoReleaser support — verified (2026-06-21).** GoReleaser v2 (this repo
is on `goreleaser/goreleaser:v2.16.0`, `.gitlab-ci.yml` L41) has **no
native Authenticode support**. What it offers is a **generic per-binary
signing hook**, the `binary_signs:` block, with a `cmd` + `args` contract
and four placeholders: `${artifact}` (path to the binary), `${artifactID}`,
`${certificate}`, `${signature}`. The publisher wires an external tool
(`signtool` / `osslsigncode` / `jsign`) through `cmd`/`args`. The existing
`signs:` block (L92, used for the OpenPGP detached sig over the checksums
archive) is the *archive/checksum* signer; `binary_signs:` is the
*per-binary* signer and is the correct hook for embedding Authenticode
into each `.exe` **before** it is placed into its `.tar.gz` archive.

> Open verification note resolved: the original brief asked whether
> GoReleaser supports Windows signing natively. It does **not** — it
> supports an external-tool hook. This is now a confirmed design input,
> not an open question.

One ordering subtlety worth flagging for implementation: GoReleaser runs
the UPX stanza (if any — GTB does not currently use UPX) *after* build
hooks, which can break embedded signatures. GTB has no UPX step, so this
does not bite today, but a note belongs in the how-to guide so downstream
tools that enable UPX know to sign after compression.

---

## Design decisions

### D1 — Signing backend: Azure Trusted Signing (recommended)

**Decision: Azure Trusted Signing, signed via `jsign`, gated on
credentials, OIDC-federated from GitLab CI.** Traditional
OV/EV-cert-on-disk is the rejected alternative (see
[Alternatives](#alternatives-considered)).

Rationale, point-by-point against the AWS-KMS pattern this project already
operates:

| Concern | Azure Trusted Signing | Traditional OV/EV cert |
|---------|------------------------|-------------------------|
| Private-key custody | Microsoft cloud HSM (FIPS 140-2 L3); key never leaves | `.pfx`/token on disk or USB HSM; must reach CI |
| Cert lifetime | **short-lived, per-request** (3-day certs minted on demand) | 1–3 year static cert; rotation is manual & painful |
| Cost | ~US$10/month + identity-validation fee | OV ~US$100–300/yr; EV ~US$250–700/yr + hardware token |
| CI auth | **OIDC workload-identity federation** (no long-lived secret) | inject `.pfx` + passphrase as CI secrets (long-lived, high blast radius) |
| SmartScreen reputation | accrues normally; Microsoft-operated CA | OV accrues gradually; **EV gets instant reputation** |
| Eligibility | requires a verified org / individual (identity validation) | same identity-validation bar for OV/EV |
| Mirrors existing infra | **yes — same OIDC-from-CI shape as AWS-KMS signer** | no — reintroduces the long-lived-secret-in-CI risk Phase 2 deliberately removed |

The deciding factor is **operational symmetry**. The OpenPGP signer's
trust policy already pins an OIDC role to
`project_path:phpboyscout/go-tool-base:ref_type:tag:ref:v*`
(`.gitlab-ci.yml` L107–113), so only tag pipelines on this project can
sign. Azure Trusted Signing's federated-credential model expresses the
*same* constraint (subject = the GitLab tag pipeline's OIDC token) against
an Azure AD app registration. We get the identical "only a release tag can
mint a signature, no long-lived key material in CI" property on the
Windows side for free, with the same mental model maintainers already hold.

The one genuine asymmetry: Azure auth is its own SDK/CLI, **not** the AWS
SDK, so this does **not** route through `pkg/signing`'s `crypto.Signer`
abstraction (see [D2](#d2--no-new-pkgsigning-backend-out-of-process-tool)).

### D2 — No new `pkg/signing` backend; out-of-process tool

**Decision: do NOT add an `azure-trusted-signing` backend to
[`pkg/signing`](../../../pkg/signing/backend.go). Drive the existing
external tool (`jsign`) directly from a release script, mirroring
`scripts/sign-release.sh`.**

The `pkg/signing.Backend` registry (`backend.go` L58–62) exists to
produce a Go `crypto.Signer` that `pkg/openpgpkey.DetachSign` consumes to
build an **OpenPGP** packet. Authenticode is a fundamentally different
output: a PKCS#7 structure embedded in a PE file's certificate table.
There is no `crypto.Signer`-shaped seam — the PE-rewriting, timestamp
countersignature, and cert-chain embedding are exactly what `jsign` /
`signtool` already do correctly, and reimplementing PE Authenticode
embedding in Go would be a large, security-sensitive surface with no
reuse benefit.

So the boundary is: `pkg/signing` stays OpenPGP-key-shaped; Windows
Authenticode is an **external-tool integration at the release layer**,
not a library primitive. Concretely:

- A new `scripts/sign-windows.sh` (sibling of `scripts/sign-release.sh`)
  wraps the `jsign` invocation against Azure Trusted Signing.
- GoReleaser's `binary_signs:` block calls it once per Windows `.exe` via
  `${artifact}`.
- No `pkg/` change. No new `gtb` subcommand. (A future `gtb sign-pe`
  command is explicitly out of scope — see
  [Out of scope](#out-of-scope) — and would only be justified if a
  downstream GTB-built tool needed Authenticode self-service, which no
  one has asked for.)

This keeps the library-first principle intact *by correctly identifying
that this is not a library concern*: there is no reusable cross-tool
primitive here, only a release-pipeline wiring that each tool author
configures with their own publisher identity.

### D3 — GoReleaser wiring: `binary_signs:`, gated, Windows-only

**Decision: add a `binary_signs:` block scoped to the Windows builds,
enabled only when the signing credential is present** — the exact
dormant-until-provisioned discipline already used by `notarize:`
(`enabled: '{{ isEnvSet "APPLE_DEV_CERT" }}'`, L46) and `signs:`
(gated via `--skip=sign` in CI when `GTB_SIGNING_KEY` is unset, L86–91).

Sketch (illustrative; exact `jsign` flags resolved at implementation):

```yaml
binary_signs:
  - id: windows-authenticode
    # Only the windows .exe binaries; the cross-OS `gtb` build id
    # produces linux+windows, so filter on the artifact, not the build.
    ids:
      - gtb
    # Dormant unless the Trusted Signing endpoint is configured, mirroring
    # the notarize: isEnvSet gate.
    disable: '{{ not (isEnvSet "AZURE_TRUSTED_SIGNING_ENDPOINT") }}'
    cmd: scripts/sign-windows.sh
    args:
      - "${artifact}"
    # signtool/jsign embed the signature in-place; no separate output file.
    signature: "${artifact}"
```

Two GoReleaser facts that shape the wiring and need confirming against the
v2.16.0 schema at implementation time (flagged as
[open questions](#open-questions)):

1. Whether `binary_signs:` can filter to the Windows `.exe` only (the
   `gtb` build id emits both linux and windows binaries from one id). If
   `ids:` alone is insufficient, the `sign-windows.sh` script must
   no-op on non-`.exe` artifacts by inspecting the extension — a trivial
   and safe guard to add regardless.
2. Whether `binary_signs:` runs **before archiving** so the signed `.exe`
   is what lands inside the `.tar.gz` (`archives:` L57–72). Per GoReleaser
   docs `binary_signs` operates in the build phase, i.e. before archiving
   — to be re-verified.

`disable:` is preferred over the `--skip` CLI approach the OpenPGP signer
uses, because `binary_signs:` supports a per-block template gate directly,
keeping the gating declarative in `.goreleaser.yaml` rather than split
into the CI job. (If the v2.16.0 schema names the field `enabled:` rather
than `disable:`, adjust accordingly — confirmed at implementation.)

### D4 — CI: OIDC workload-identity, no long-lived secret

**Decision: federate GitLab CI → Azure AD via OIDC, mirroring the AWS-KMS
`id_tokens:` block.** The `goreleaser` job (`.gitlab-ci.yml` L121) already
mints an `AWS_WEB_IDENTITY_TOKEN` id_token with `aud: sts.amazonaws.com`.
Add a second id_token for Azure with the Azure-expected audience (the
`api://AzureADTokenExchange` convention, to be confirmed at
implementation), and have `sign-windows.sh` exchange it for an Azure
access token scoped to the Trusted Signing account.

The Azure AD app registration's **federated credential** pins the subject
to this project's tag pipelines — the direct analogue of the AWS role
trust policy's `project_path:…:ref:v*` condition. Net effect: **no
`.pfx`, no passphrase, no long-lived Azure secret ever enters CI**; a
leaked config reveals only the (public) endpoint and account names, not
anything that can sign.

The job remains tag-gated by the goreleaser component's existing `rules:`,
so MR / branch / scheduled pipelines never reach the signing step — same
containment the OpenPGP signer relies on.

### D5 — Timestamping is mandatory

**Decision: always pass an RFC 3161 timestamp authority to `jsign`.**
Without a timestamp countersignature, the Authenticode signature becomes
invalid the moment the signing certificate expires — fatal for binaries
users may run years after release. Azure Trusted Signing provides a TSA;
the script pins it explicitly rather than relying on a default. This is
non-negotiable and not a configurable knob.

### D6 — Verification is out-of-pipeline (operator-facing)

**Decision: no in-binary Authenticode verification.** Unlike the OpenPGP
track (where `pkg/setup` verifies the self-update signature in-process),
Windows itself verifies Authenticode at load time — that *is* the
consumer. The release pipeline's own check is an operator-facing
`Get-AuthenticodeSignature` (or `osslsigncode verify` / `jsign --verify`)
smoke test, documented in the how-to, run manually against a published
artefact during the first signed release. GTB's `gtb update` path already
covers download integrity via the OpenPGP+checksum chain; it does not need
to re-validate Authenticode.

### D7 — Generator / scaffolding scope

**Decision: out of scope for the generator in this iteration.** Windows
Authenticode is a release-infrastructure concern requiring a per-publisher
identity (an Azure tenant or a purchased cert) that the framework cannot
provision on a tool author's behalf. The scaffolded `.goreleaser.yaml`
template in `internal/generator/` should gain a **commented-out
`binary_signs:` stub with a pointer to the how-to**, but no active
wiring — exactly as the notarize/OpenPGP blocks are tool-author opt-in.
This keeps generated tools building out of the box while signposting the
path to signed Windows releases.

---

## Trust & threat model

What Authenticode signing **does** provide:

- **Publisher authenticity at launch.** Windows confirms the `.exe` was
  signed by the holder of a cert chaining to a trusted CA, and that the
  bytes were not modified post-signing.
- **SmartScreen reputation accrual.** Each signed, downloaded release
  builds reputation under the stable publisher identity. EV certs start
  with reputation; OV/Trusted-Signing builds it over time and volume.
- **Corporate-policy compatibility.** Estates that block unsigned
  executables (AppLocker/WDAC publisher rules) can allow GTB by publisher.

What it **does not** provide (stated to avoid over-claiming):

- **It is not malware scanning.** There is no Microsoft notary equivalent
  to Apple's. A signature asserts identity, not safety.
- **It does not protect the self-update download chain** — that is the
  OpenPGP+checksum track's job. A signed `.exe` served by a compromised
  CDN with a stale-but-valid signature is still caught only by the
  checksum/OpenPGP layer, not by Authenticode.
- **Reputation is not instant for OV/Trusted Signing.** Early signed
  releases may still draw a (milder, "less common" rather than "unknown
  publisher") SmartScreen notice until volume accrues.

Residual risks to record in `security-decisions.md` on acceptance:

- **Trusted Signing account compromise** = ability to sign as GTB. Blast
  radius is bounded by: short-lived per-request certs (a stolen cert
  expires in days), OIDC-only CI access (no static secret to steal), and
  the tag-pipeline-pinned federated credential (only release tags can
  mint). An attacker would need to compromise the GitLab tag pipeline
  itself — the same residual "malicious release author / pipeline
  compromise" risk already accepted for the OpenPGP signer.
- **CA trust dependency.** Trust roots in Microsoft's Trusted Root
  Program; a CA mis-issuance is an industry-wide risk we inherit, not one
  we introduce.

---

## Implementation plan (high level)

Library-first does not apply (see [D2](#d2--no-new-pkgsigning-backend-out-of-process-tool));
this is release-infra wiring. TDD applies to the script's guard logic.

1. **Provision** (operator, one-time, out-of-band): create the Azure
   Trusted Signing account + certificate profile; complete identity
   validation; register the GitLab-OIDC federated credential pinned to
   `phpboyscout/go-tool-base` tag pipelines.
2. **`scripts/sign-windows.sh`**: exchange the GitLab OIDC token for an
   Azure token; invoke `jsign` against the Trusted Signing endpoint with a
   mandatory RFC 3161 timestamp ([D5](#d5--timestamping-is-mandatory)); no-op
   on non-`.exe` artifacts; refuse to run with no credentials (safety net,
   mirroring `sign-release.sh` L56–63). Bats/shell-test the guards.
3. **`.goreleaser.yaml`**: add the gated `binary_signs:` block
   ([D3](#d3--goreleaser-wiring-binary_signs-gated-windows-only)).
4. **`.gitlab-ci.yml`**: add the Azure `id_tokens:` entry and the
   token-file/before_script plumbing on the `goreleaser` job
   ([D4](#d4--ci-oidc-workload-identity-no-long-lived-secret)).
5. **Generator stub**: commented `binary_signs:` block + how-to pointer in
   the scaffolded `.goreleaser.yaml` template
   ([D7](#d7--generator--scaffolding-scope)).
6. **Docs**: `docs/how-to/sign-windows-releases.md`; update
   `docs/how-to/secure-releases.md` to add the Windows row; add a
   `security-decisions.md` entry recording the trust anchor and residual
   risks.
7. **First signed release verification**: manual
   `Get-AuthenticodeSignature` / `osslsigncode verify` smoke test
   ([D6](#d6--verification-is-out-of-pipeline-operator-facing)).

### Verification plan

- **Script unit/guard tests**: no-credentials refusal, non-`.exe` no-op,
  timestamp flag always present.
- **`goreleaser check`** passes with the new block; **`just snapshot`**
  produces archives (signing skipped locally because the gate is unset).
- **End-to-end (first real signed tag)**: confirm
  `Get-AuthenticodeSignature gtb.exe` reports `Valid`, the timestamp is
  present, and the chain resolves on a clean Windows host; confirm
  SmartScreen no longer shows "unknown publisher".

---

## Alternatives considered

### A — Traditional OV/EV certificate + signtool/osslsigncode (rejected)

Buy an OV or EV code-signing certificate from a CA, store it (OV: `.pfx`
+ passphrase as CI secrets; EV: hardware token / cloud-HSM), and sign with
`signtool` (Windows runner) or `osslsigncode` (Linux runner).

Rejected because:

- **Long-lived secret in CI.** The OV `.pfx`+passphrase path reintroduces
  exactly the long-lived high-blast-radius CI secret that the Phase 2
  OpenPGP work deliberately eliminated by moving to KMS+OIDC. A leaked
  `.pfx` is a multi-year signing capability.
- **EV hardware tokens don't fit headless CI** cleanly (USB-token EV certs
  historically can't run unattended; cloud-HSM EV variants exist but cost
  significantly more than Trusted Signing for the same short-lived-cert
  benefit).
- **osslsigncode is on the way out.** The ecosystem is migrating to
  short-lived certs that osslsigncode's static-cert model doesn't serve;
  GoReleaser community guidance now points at Azure CLI + jsign.
- **Cost & rotation.** OV/EV certs cost more and require painful manual
  rotation; Trusted Signing mints short-lived certs automatically.

EV's one genuine edge — **instant SmartScreen reputation** — is the only
reason to revisit this. If first-run SmartScreen friction proves
unacceptable during the OV/Trusted-Signing reputation-accrual window, an EV
cloud-HSM cert is the documented escalation. Recorded here so the trade-off
is explicit, not lost.

### B — Do nothing / ship unsigned (rejected)

The status quo. Rejected: it leaves the documented platform gap open,
blocks adoption on policy-locked Windows estates, and is the outlier
against the macOS and cross-platform tracks already shipped.

### C — Reimplement PE Authenticode in Go as a `pkg/signing` consumer (rejected)

Build PE-certificate-table embedding + PKCS#7 + RFC 3161 timestamping in
Go so it slots behind a `crypto.Signer`. Rejected: large, security-
sensitive surface; no cross-tool reuse benefit; `jsign`/`signtool` already
do this correctly. See [D2](#d2--no-new-pkgsigning-backend-out-of-process-tool).

---

## Out of scope

- **In-binary Authenticode verification** in `gtb update` — Windows
  verifies at load time; the self-update chain is already covered by
  OpenPGP+checksums.
- **A `gtb sign-pe` / `gtb sign --authenticode` subcommand** — no
  downstream tool has asked for self-service PE signing; the release-layer
  script is sufficient. Revisit only on a concrete asker.
- **Generator-active wiring** — a commented stub only ([D7](#d7--generator--scaffolding-scope)).
- **MSI / Appx / NuGet packaging and their signing** — GTB ships a
  `.tar.gz`-archived `.exe`, not an installer. Out of scope unless/until a
  Windows installer format is added.
- **Linux package signing** (deb/rpm GPG) — separate concern, not this
  item.

---

## Open questions (for human review)

1. **Eligibility & identity validation.** Azure Trusted Signing requires a
   verified individual or organisation (3+ years of verifiable history for
   individuals, or a registered org). Does PHP Boy Scout / Matt Cockayne
   meet the validation bar, and under which identity should the publisher
   string read? This gates everything and is operator-only.
2. **GoReleaser `binary_signs:` schema specifics on v2.16.0** — exact
   field names (`disable:` vs `enabled:`), whether `ids:` can isolate the
   Windows `.exe` from the dual-OS `gtb` build, and confirmation that it
   runs before archiving so the *signed* `.exe` lands in the `.tar.gz`.
   Verified conceptually from docs; pin against the actual v2.16.0 schema
   at implementation. (Flagged in [D3](#d3--goreleaser-wiring-binary_signs-gated-windows-only).)
3. **Azure OIDC audience value** — the GitLab→Azure id_token `aud` (the
   `api://AzureADTokenExchange` convention) and the federated-credential
   subject format for GitLab tag pipelines. Analogous to the AWS
   `aud: sts.amazonaws.com` gotcha already documented
   (`.gitlab-ci.yml` L126–137).
4. **`jsign` availability in the goreleaser CI image.** `jsign` is
   Java-based and not bundled in `goreleaser/goreleaser:v2.16.0`. Options:
   install it in `before_script`, use a custom image, or use the Azure
   `trusted-signing` CLI/SDK directly. Decide the least-friction path.
5. **OV/Trusted-Signing reputation acceptance.** Is the early
   reputation-accrual SmartScreen notice acceptable, or is the EV-cert
   escalation (Alternative A, instant reputation) wanted from day one
   despite cost? Product call.
6. **arm64 Windows.** The `gtb` build emits `windows/arm64`. Confirm the
   signing path covers both arches (it should — `binary_signs:` iterates
   all matching artifacts — but verify the toolchain handles arm64 PE).

---

## Related

- [`.goreleaser.yaml`](../../../.goreleaser.yaml) — `notarize:` (L44),
  `signs:` (L92), `archives:` (L57), the `gtb` windows build (L26).
- [sign-command spec](2026-06-09-sign-command.md) — the OpenPGP signer and
  `pkg/signing` registry this spec deliberately does **not** extend.
- [remote-update-checksum spec](2026-04-02-remote-update-checksum-verification.md)
  — L66 names Windows Authenticode as the deferred future work this spec
  fulfils.
- [`.gitlab-ci.yml`](../../../.gitlab-ci.yml) L121–147 — the AWS-KMS OIDC
  `goreleaser` job overlay that the Azure OIDC plumbing mirrors.
- [`scripts/sign-release.sh`](../../../scripts/sign-release.sh) — the
  sibling script `scripts/sign-windows.sh` will mirror.
- [`docs/how-to/secure-releases.md`](../../how-to/secure-releases.md) —
  gains a Windows row.
