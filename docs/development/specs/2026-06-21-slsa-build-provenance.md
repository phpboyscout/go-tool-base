---
title: "SLSA Build Provenance — signed in-toto attestation per release artifact"
description: "Emit a signed in-toto SLSA provenance attestation for each release artifact, describing how and where it was built, complementing the existing SBOM (GoReleaser `sboms:`) and the signed-checksums chain (`signs:` + pkg/setup verifier). Targets SLSA Build Level 2 on GitLab.com SaaS shared runners, signed with the already-provisioned AWS-KMS key via `gtb sign`, with a documented Build Level 3 path on GitLab Hosted Runners. Item A3; the deferred Phase 5 'SLSA build provenance' of the remote-update-checksum-verification spec."
date: 2026-06-21
status: DRAFT
tags:
  - specification
  - security
  - supply-chain
  - slsa
  - provenance
  - attestation
  - in-toto
  - signing
  - release
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# SLSA Build Provenance — signed in-toto attestation per release artifact

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   21 June 2026

Status
:   DRAFT — for human review. This is item **A3** and the realisation of the
    "SLSA build provenance" line deferred to a follow-up spec in
    `2026-04-02-remote-update-checksum-verification.md` (Phases 3–6 Future
    Considerations) and referenced again as the build-machine-compromise
    mitigation ("Mitigation: SLSA provenance, reproducible builds (Phase 5+)").

---

## Summary

GTB releases today carry two supply-chain artefacts produced at release time on
the tag-gated `goreleaser` job in `.gitlab-ci.yml`:

1. **An SBOM per archive** — GoReleaser's `sboms:` block (`.goreleaser.yaml`),
   describing *what is inside* each artifact.
2. **A signed checksums manifest** — `checksums.txt` plus a detached,
   KMS-signed `checksums.txt.sig` (the `signs:` block invoking
   `scripts/sign-release.sh` → `gtb sign --backend aws-kms`), verified on
   self-update by `pkg/setup`'s `TrustSet.VerifyManifestSignature`. This gives
   *integrity and authorship*.

What is missing is **provenance** — a signed, machine-readable statement of
*how and where* each artifact was built: which pipeline, which commit, which
builder image, which build entry point, on which runner. This spec adds a
**signed [in-toto](https://github.com/in-toto/attestation) SLSA provenance
attestation** for the release, closing the "compromise of the build machine
before signing" gap that the checksum/signature chain explicitly cannot
(`2026-04-02-…` §"What remains unsolved").

The deliverable per release is a `provenance.intoto.jsonl` attestation (the
in-toto Statement wrapping a SLSA Provenance v1 predicate) over the release
archives, **signed with the existing AWS-KMS release key** so verification
reuses the trust anchor we already publish (embedded key + WKD), with **no new
KMS key, no new IAM role, and no new signing dependency**.

### Relationship to existing artefacts

| Artefact | Block / source | Question it answers | Status |
|----------|----------------|---------------------|--------|
| SBOM (per archive) | `.goreleaser.yaml` `sboms:` | *What is inside?* | Shipping |
| `checksums.txt(.sig)` | `signs:` + `scripts/sign-release.sh` + `pkg/setup` | *Is it intact / who signed it?* | Shipping (v0.12.2+) |
| **Provenance attestation** | **This spec** | ***How / where was it built?*** | **Proposed (A3)** |

The three are complementary and non-overlapping. The attestation does **not**
replace the checksums chain — it is signed *alongside* it, by the same key,
and references the same archives by digest.

---

## Decision-log check (no conflict)

Confirmed against `docs/development/feature-decisions.md`:

- **Distributed Tracing** is *rejected* (decision at
  `feature-decisions.md:90`), but that is an unrelated runtime-observability
  concern ("a service-level concern — tools that need it should use
  OpenTelemetry directly"). It says nothing about build-time provenance.
- **SLSA build provenance / attestation** is **open and explicitly deferred** —
  it appears in `2026-04-02-remote-update-checksum-verification.md` as a Phase 5
  Future Consideration, not as a rejected item. There is no decision-log entry
  rejecting it.

There is therefore **no conflict**; this spec converts a deferred future
consideration into a concrete proposal.

---

## What SLSA level is actually achievable on GitLab.com (be honest)

This is the load-bearing design constraint, so it is stated plainly up front.
SLSA v1.0 defines **Build Levels** (L1 = provenance exists; L2 = provenance is
*signed* by the build platform and resistant to tamper after the fact; L3 =
the build runs in an isolated, hardened environment where build steps cannot
influence the provenance or exfiltrate the signing material).

There are three practical mechanisms on GitLab, and they reach different levels:

### Option A — GoReleaser native `attestations:` block — **not viable on GitLab**

GoReleaser's built-in attestations feature is **GitHub-only**: it delegates to
the `actions/attest` GitHub Action and depends on `id-token: write` /
`attestations: write` GitHub permissions
([GoReleaser docs](https://goreleaser.com/customization/publish/attestations/)).
It produces nothing on a GitLab runner. **Rejected** — does not function on our
platform.

### Option B — GitLab native SLSA provenance (`RUNNER_GENERATE_ARTIFACTS_METADATA` / `ATTEST_BUILD_ARTIFACTS`)

GitLab Runner can natively emit a SLSA provenance statement for job artifacts.

- Setting `RUNNER_GENERATE_ARTIFACTS_METADATA: "true"` makes the runner emit an
  `…artifacts-metadata.json` SLSA statement for the job's artifacts. This is
  generated *inside the runner*, outside the user-controlled build steps, so it
  is **not tamperable by the build script** — that is what lets GitLab call it
  **SLSA Build Level 2** ([GitLab: SLSA](https://docs.gitlab.com/ci/pipeline_security/slsa/),
  [GitLab blog: Achieve SLSA Level 2](https://about.gitlab.com/blog/achieve-slsa-level-2-compliance-with-gitlab/)).
- **SLSA Build Level 3** on GitLab additionally requires the
  `slsa_provenance_statement` feature flag, the project be **public**, the build
  use the `build` stage, and the newer `ATTEST_BUILD_ARTIFACTS` /
  `ATTEST_CONTAINER_IMAGES` variables. GitLab itself marks the L3 path
  **experimental / "not ready for production use"**
  ([GitLab: SLSA L3](https://docs.gitlab.com/ci/pipeline_security/slsa/level_3/)).
  L3 in the SLSA sense also requires *increased runner isolation* so build steps
  cannot reach the signing material — in practice GitLab Hosted Runners or a
  hardened self-managed fleet, **not** the default `saas-linux-medium-amd64`
  shared runner this project uses (`.gitlab-ci.yml` `default: tags:`).

The honest read: on **GitLab.com shared SaaS runners**, the *runner-native*
ceiling is **Build Level 2**. The shared runner is a multi-tenant, non-isolated
control plane; we cannot truthfully claim L3 there.

### Option C — `cosign attest-blob` keyless (Sigstore + GitLab OIDC + Rekor)

GitLab can mint an ID token with `aud: sigstore`; `cosign` reads it from
`SIGSTORE_ID_TOKEN` and signs keylessly, recording the certificate in the Rekor
transparency log ([GitLab: Sigstore signing examples](https://docs.gitlab.com/ci/yaml/signing_examples/)).
This is the path most OSS projects use, and it dovetails with **Phase 3**
(Sigstore/Rekor transparency log) of the checksum-verification spec.

**Deliberately deferred here**, for the same reasons GTB chose KMS-GPG over
cosign in `2026-04-02-…` Resolved Decision #2:

- It introduces a *new* trust root (Fulcio/Rekor) and a `cosign` binary in the
  release image — neither of which the current pipeline has.
- The GTB self-updater verifies signatures **offline** against an embedded +
  WKD key; a Rekor-anchored attestation is not consumable by that path without
  the Phase 3 work. Signing the attestation with the **existing KMS key**
  instead keeps verification on the trust anchor we already ship.

Option C is recorded as the **L3-on-shared-runner future** (it raises assurance
via an independent transparency log rather than via runner isolation) and is
explicitly out of scope for A3.

### Decision

**Adopt a hybrid of B + our existing KMS signer**, targeting **SLSA Build
Level 2 on the current shared-runner pipeline**, with a documented, low-effort
upgrade to Build Level 3 on GitLab Hosted Runners:

1. Enable GitLab runner-native provenance (`RUNNER_GENERATE_ARTIFACTS_METADATA:
   "true"`) on the `goreleaser` job so the **runner** vouches for the build
   environment (the L2 anchor — tamper-resistant because the build script does
   not produce it).
2. Additionally emit a **GoReleaser-produced, KMS-signed in-toto SLSA
   Provenance v1 attestation** over the release archives, so the provenance
   travels *with the release assets* (not just as a pipeline artifact) and is
   verifiable by the same key/WKD anchor as `checksums.txt.sig`. This is what
   downstream consumers and `gtb`'s own tooling can fetch and check.

The two reinforce each other: the runner-native statement is the
not-tamperable-by-build-step anchor; the signed, asset-attached attestation is
the *portable, offline-verifiable* artefact on the trust anchor we own.

---

## Goals

- Produce one signed in-toto **SLSA Provenance v1** attestation per release,
  attached to the GitLab Release as an asset (`provenance.intoto.jsonl`).
- Sign it with the **existing** `alias/gtb-release-signing-v1` KMS key via the
  existing `gtb sign` path — no new key, IAM role, or OIDC audience.
- Enable GitLab runner-native provenance to anchor SLSA Build Level 2.
- Provide an in-binary verifier so `gtb` (and downstream tools) can check the
  attestation on self-update, reusing the `pkg/setup` `TrustSet`.
- Document honestly that L2 is the shared-runner ceiling and how to reach L3.

## Non-Goals

- **Reproducible / hermetic builds** — a separate, larger effort. The
  attestation records `-trimpath` and pinned toolchain facts but does not by
  itself make the build bit-for-bit reproducible.
- **Cosign / Sigstore / Rekor keyless signing** (Option C) — deferred to the
  Phase 3 transparency-log spec.
- **Container-image provenance** (`ATTEST_CONTAINER_IMAGES`) — GTB ships
  binaries/archives, not images, today.
- **GitHub/Gitea/Bitbucket provenance emission** in the *generator* output —
  the scaffolded `.gitlab-ci.yml` gets the GitLab path; other platforms get a
  documented TODO, not generated wiring (matches the existing GitLab-first
  release posture).

---

## Provenance content (what the attestation must assert)

The predicate is **SLSA Provenance v1**
(`https://slsa.dev/provenance/v1`), wrapped in an in-toto Statement
(`https://in-toto.io/Statement/v1`). The fields below are populated from GitLab
CI predefined variables available on the tag pipeline:

| Predicate field | Source |
|-----------------|--------|
| `subject[].name` / `.digest.sha256` | each release archive + its SHA-256 (already computed for `checksums.txt`) |
| `predicate.buildDefinition.buildType` | a stable GTB URI, e.g. `https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/.gitlab-ci.yml@goreleaser` |
| `…externalParameters.source` | `$CI_PROJECT_URL` |
| `…resolvedDependencies[].uri` + `.digest.gitCommit` | `$CI_PROJECT_URL` @ `$CI_COMMIT_SHA` (the exact tagged commit) |
| `…internalParameters` | builder image (`goreleaser/goreleaser:v2.16.0`), `GOTOOLCHAIN`, `CGO_ENABLED=0`, `GOLANG_FIPS=1`, `-trimpath` — mirrors `.goreleaser.yaml` `builds:` |
| `predicate.runDetails.builder.id` | `$CI_SERVER_URL/$CI_PROJECT_PATH` + runner tag (`saas-linux-medium-amd64`) — honest about the shared runner |
| `…metadata.invocationId` | `$CI_PIPELINE_URL` / `$CI_JOB_URL` |
| `…metadata.startedOn` / `finishedOn` | job timestamps |

The `builder.id` deliberately names the **shared runner** so the attestation
does not over-claim isolation it does not have.

---

## Design

### D1 — Attestation generator lives in `pkg/provenance` (library-first)

Per the library-first principle, the predicate construction is a reusable
`pkg/` component, not buried in the release script:

```go
// Package provenance builds and verifies SLSA Provenance v1 in-toto
// attestations for release artifacts. The build-environment facts are
// supplied by the caller (read from CI variables); this package owns the
// in-toto/SLSA struct shaping, canonical JSON marshalling, and the
// digest-binding of subjects.
//
// API stability: Beta — additive evolution expected as the SLSA predicate
// schema grows.
package provenance

// Subject binds a named release artifact to its SHA-256 digest.
type Subject struct {
    Name   string
    Digest string // lowercase hex SHA-256, no algorithm prefix
}

// BuildContext is the set of build-environment facts that populate the
// SLSA Provenance v1 predicate. Callers populate it from CI variables.
type BuildContext struct {
    BuildType     string
    SourceURI     string // $CI_PROJECT_URL
    SourceCommit  string // $CI_COMMIT_SHA
    BuilderID     string // server/project + runner tag
    BuilderImage  string // goreleaser image ref
    InvocationID  string // $CI_PIPELINE_URL or $CI_JOB_URL
    StartedOn     time.Time
    FinishedOn    time.Time
    Internal      map[string]string // CGO_ENABLED, GOLANG_FIPS, GOTOOLCHAIN, flags
}

// BuildStatement assembles an in-toto Statement (v1) wrapping a SLSA
// Provenance v1 predicate over the given subjects. The returned bytes are
// canonical JSON (one statement); the caller writes them and signs the file.
func BuildStatement(subjects []Subject, bc BuildContext) ([]byte, error)

// VerifyStatement parses an in-toto Statement, checks it is SLSA Provenance
// v1, and (given the expected archive digests) confirms every expected
// subject is present and digest-bound. Signature verification is the
// caller's job (pkg/setup TrustSet) — this validates *structure and binding*.
func VerifyStatement(statement []byte, expected []Subject) error
```

Signing is **not** re-implemented: the statement file is signed by the existing
`gtb sign` detached-signature path (`pkg/openpgpkey.DetachSign` over a
`crypto.Signer` from the `pkg/signing` registry — see
`2026-06-09-sign-command.md`). The attestation is therefore
`provenance.intoto.jsonl` + `provenance.intoto.jsonl.sig`, identical in shape
to the checksums pair.

> **Open question (OQ1):** in-toto convention is a *DSSE envelope* (the
> signature wraps the statement in a typed JSON envelope) rather than a
> *detached* `.sig` sidecar. DSSE is what `cosign`/Rekor consume. Detached-sig
> reuses our exact existing verifier and trust anchor with zero new code.
> **Proposed:** ship detached-sig now (reuse `TrustSet`), and add a DSSE
> envelope later **only if/when** Option C (Sigstore) lands and a DSSE consumer
> exists. Confirm this trade-off is acceptable, or require DSSE from day one.

### D2 — GoReleaser wiring: extra signed asset, no pipeline restructure

The statement is generated **after** archives+checksums exist (their digests
are the subjects) and **before** the `signs:` stage signs it. Two mechanisms
are possible:

- **Preferred:** a GoReleaser `before`/post-archive hook (or a small
  `extra_files` + custom step) runs `gtb provenance generate` to write
  `dist/provenance.intoto.jsonl`, then a **second `signs:` entry** signs it the
  same way `checksums` is signed (`artifacts: provenance` if/where supported,
  else `extra_files`). The signed pair is attached to the Release.
- The `gtb provenance generate` subcommand reads `dist/checksums.txt` for the
  subject digests, reads the CI variables for `BuildContext`, and calls
  `provenance.BuildStatement`.

This adds **one signed asset pair** to the release and **one `signs:` entry**;
it does not change the build matrix, the notarize gate, the homebrew cask, or
releaser-pleaser's changelog ownership (`mode: keep-existing`).

> **Open question (OQ2):** GoReleaser's `signs:` `artifacts:` enum may not have
> a first-class `provenance` value in v2.16.0; the fallback is to register the
> statement as an `extra_files` artifact and sign it by id. Confirm the exact
> GoReleaser hook during implementation (verify against the pinned
> `goreleaser/goreleaser:v2.16.0` image, not docs alone).

### D3 — `.gitlab-ci.yml`: enable runner-native provenance (L2 anchor)

Add to the `goreleaser` job overlay (alongside the existing `id_tokens` /
KMS-OIDC `before_script`):

```yaml
goreleaser:
  variables:
    RUNNER_GENERATE_ARTIFACTS_METADATA: "true"   # SLSA L2 runner-native statement
```

This is independent of the asset-attached attestation: it makes the **runner**
emit a tamper-resistant SLSA statement for the job artifacts (the L2 assurance
anchor). No new `id_tokens` audience is needed — the KMS-OIDC token
(`aud: sts.amazonaws.com`) that already signs `checksums.txt` also signs the
provenance statement, because signing goes through the same `gtb sign` call.

### D4 — In-binary verification on self-update (`pkg/setup`)

Extend the self-update verification flow to **optionally** fetch and verify the
provenance attestation, mirroring the `require_signature` pattern:

- New config `update.require_provenance` (library default `false`;
  `setup.DefaultRequireProvenance` compile-time override; GTB ships `true` once
  the first attested release exists, per the Phase-1/2 +1-release pattern).
- New `update.provenance_asset_name` (default `provenance.intoto.jsonl`).
- Flow: after checksum+signature verification succeeds, if provenance is
  required/present, verify `provenance.intoto.jsonl.sig` with the **same
  `TrustSet`**, then `provenance.VerifyStatement(stmt, expectedSubjects)` to
  confirm the downloaded archive's digest is a bound subject and the predicate
  is SLSA Provenance v1. New sentinel errors: `ErrProvenanceMissing`,
  `ErrProvenanceInvalid`, `ErrProvenanceSubjectMismatch`.

This makes provenance a *verifiable update-time gate*, not just a published
document — and it does so on the offline embedded+WKD anchor, no Rekor needed.

### D5 — Generator output

`internal/generator/` templates that scaffold a downstream tool's
`.gitlab-ci.yml` / `.goreleaser.yaml` gain the `RUNNER_GENERATE_ARTIFACTS_METADATA`
variable and the second `signs:` entry, **gated on the tool having a signing
key** (same gate as the existing `signs:` block — dormant until a key is
provisioned). Non-GitLab targets get a documented TODO comment, not generated
wiring. Any user-influenced field rendered into these templates routes through
the existing `internal/generator/validate.go` + `template_escape.go` perimeter
(`escapeYAML` at YAML sites).

---

## Public API surface (new)

- `pkg/provenance`: `Subject`, `BuildContext`, `BuildStatement`,
  `VerifyStatement` (above).
- `pkg/setup`: `DefaultRequireProvenance bool`; sentinels `ErrProvenanceMissing`,
  `ErrProvenanceInvalid`, `ErrProvenanceSubjectMismatch`; internal `Update()`
  flow extension (no public signature change, matching the checksum/signature
  precedent).
- `internal/cmd/provenance/` (gtb-only, not scaffoldable — same boundary as
  `internal/cmd/sign/` and `keys/`): `gtb provenance generate` /
  `gtb provenance verify`.

Pre-1.0: any break here is a minor bump (CLAUDE.md §API Stability); prefer the
additive shapes above.

---

## Dependencies

- in-toto / SLSA structs: prefer
  `github.com/in-toto/attestation/go/v1` (the canonical Go bindings) to avoid
  hand-rolling the predicate schema; fall back to local structs if the binding
  pulls an unacceptable transitive graph. **Decide during spike (OQ3).**
- No new signing dependency — reuses `pkg/signing` + `pkg/openpgpkey`.
- No new KMS key, IAM role, or OIDC audience.

---

## Testing (TDD)

- `pkg/provenance`: table-driven `BuildStatement` golden tests (canonical JSON,
  stable with a pinned `BuildContext`/timestamps — leverages the same
  determinism `gtb sign --created` gives, see `2026-06-09-sign-command.md` D4);
  `VerifyStatement` for present/absent subject, wrong predicate type, digest
  mismatch. ≥90% coverage (new `pkg/`).
- `pkg/setup`: provenance-required vs fail-open, signature-on-statement
  verification through `TrustSet`, subject-mismatch abort. Use
  `pkg/vcs/release/releasetest` (`2026-06-19-injectable-release-source.md`) for
  a network-free release source.
- `internal/cmd/provenance`: flag validation, generate→verify round-trip.
- Cross-tool integration (gated `INT_TEST_PROVENANCE=1`): verify the produced
  attestation against an independent in-toto/SLSA verifier (`slsa-verifier` or
  `cosign verify-attestation --type slsaprovenance` in detached mode) to catch
  schema drift, mirroring the `gpg --verify` cross-check in
  `2026-06-09-sign-command.md`.
- E2E: a Godog scenario in `features/` for "release publishes a verifiable
  provenance attestation" + "self-update aborts on provenance subject
  mismatch", per CLAUDE.md (new release/lifecycle behaviour ⇒ Gherkin).

---

## Documentation

- `docs/components/provenance.md` — the new package + threat model + the honest
  L2/L3 table.
- Update `docs/how-to/sign-releases.md` to cover the provenance asset pair.
- Update `docs/development/specs/2026-04-02-remote-update-checksum-verification.md`
  Phase 5 line to point here once IMPLEMENTED.
- A migration note is **not** required (additive; no public break), but record
  the new config keys in the update docs.

---

## Threat coverage (what A3 adds)

| Attacker capability | Before A3 | After A3 |
|---------------------|-----------|----------|
| Replace a binary, re-sign checksums with the real key | Blocked (no key) | Blocked (no key) — unchanged |
| **Tamper with the build environment / inject at build time, then ship** | **Undetected** (checksums sign whatever was built) | Runner-native L2 statement records the true build env; signed attestation binds artifact→commit→pipeline; a build from a different commit/pipeline is detectable |
| Forge a provenance statement | n/a | Blocked — statement is KMS-signed; forging needs the KMS key |
| Build-step exfiltrates signing key (no runner isolation) | Possible (shared runner) | **Still possible on shared runner** — this is the honest L2 ceiling; mitigated only by moving to Hosted Runners (L3) |

A3 raises the bar from "trust that whatever the pipeline built is what it
should be" to "the pipeline cryptographically asserts, on our own trust anchor,
which commit and environment produced each artifact." It does **not** by itself
defend against a compromised shared runner exfiltrating the KMS session — that
needs runner isolation (L3) and is documented as the next step, not claimed.

---

## Phasing

1. **A3.1** — `pkg/provenance` (`BuildStatement`/`VerifyStatement`) + unit
   tests. Library-only; nothing wired into release yet.
2. **A3.2** — `gtb provenance generate|verify` + `.goreleaser.yaml` second
   `signs:` entry + `RUNNER_GENERATE_ARTIFACTS_METADATA` in `.gitlab-ci.yml`.
   First attested release.
3. **A3.3** — `pkg/setup` update-time verification + `DefaultRequireProvenance`;
   flip GTB to require provenance one release after the first attested release.
4. **A3.4** — generator templates + docs.
5. **Deferred (separate spec):** Option C (cosign/Sigstore/Rekor keyless,
   DSSE envelope, transparency log) — the L3-via-transparency path, aligned
   with checksum-spec Phase 3; and GitLab Hosted Runners for L3-via-isolation.

---

## Open questions (resolve before implementation)

1. **OQ1 — envelope format:** detached `.sig` (reuse existing `TrustSet`, zero
   new verifier code) vs DSSE envelope (in-toto/cosign convention). *Proposed:*
   detached now, DSSE when Option C lands. **Confirm.**
2. **OQ2 — GoReleaser signing hook:** does `goreleaser/goreleaser:v2.16.0`
   accept the statement as a signable artifact via `signs:`/`extra_files`, and
   at what stage does the digest become available? **Verify against the pinned
   image.**
3. **OQ3 — in-toto Go binding vs local structs:** acceptable transitive
   dependency graph for `github.com/in-toto/attestation/go/v1`? **Spike.**
4. **OQ4 — L3 ambition:** is moving the release job to **GitLab Hosted Runners**
   (to legitimately claim SLSA Build Level 3) in scope for a later phase, or do
   we accept L2 indefinitely on shared runners? **Product decision.**
5. **OQ5 — predicate `buildType` URI stability:** pin the `buildType` identifier
   now (it becomes a verification key consumers match on) — propose
   `https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/.gitlab-ci.yml@goreleaser`.
   **Confirm wording.**

---

## References

- Deferred-from spec: `docs/development/specs/2026-04-02-remote-update-checksum-verification.md`
  (Phase 5 "SLSA build provenance"; build-machine-compromise gap).
- Signer reused: `docs/development/specs/2026-06-09-sign-command.md`
  (`gtb sign`, `pkg/openpgpkey.DetachSign`, `--created` determinism).
- Anchor files: `.gitlab-ci.yml` (tag-gated `goreleaser` job, KMS-OIDC
  `id_tokens`), `.goreleaser.yaml` (`sboms:`, `signs:`, `notarize:`),
  `pkg/signing/` + `pkg/signing/kms/`, `scripts/sign-release.sh`.
- Decision log: `docs/development/feature-decisions.md` (Distributed Tracing
  rejection — unrelated; no provenance rejection exists).
- External:
  [GitLab SLSA](https://docs.gitlab.com/ci/pipeline_security/slsa/),
  [GitLab SLSA L3](https://docs.gitlab.com/ci/pipeline_security/slsa/level_3/),
  [GitLab Sigstore signing](https://docs.gitlab.com/ci/yaml/signing_examples/),
  [GoReleaser attestations (GitHub-only)](https://goreleaser.com/customization/publish/attestations/),
  [in-toto attestation spec](https://github.com/in-toto/attestation),
  [SLSA Provenance v1](https://slsa.dev/spec/v1.0/provenance).
