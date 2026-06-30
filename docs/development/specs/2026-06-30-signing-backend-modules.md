---
title: "Per-provider signing backend modules (signing-aws-kms first; GCP/Azure/Vault to follow)"
description: "Extract the AWS KMS signing backend out of go-tool-base into its own standalone module gitlab.com/phpboyscout/signing-aws-kms, implementing the gitlab.com/phpboyscout/signing Backend contract. Establishes the per-provider backend-module pattern so consumers selectively include only the backend(s) they need (afmpeg: none; a KMS signer: aws-kms; gtb: all), and keeps each cloud SDK out of every graph that doesn't opt in. Integrated into go-tool-base alongside the signing module in one cut-over. Extends work item #1."
date: 2026-06-30
status: DRAFT
tags:
  - specification
  - signing
  - backend
  - aws-kms
  - module-extraction
  - dependency-inversion
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Per-provider signing backend modules

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   30 June 2026

Status
:   DRAFT

Tracking
:   work item #1 (extends)

Builds on
:   `2026-06-30-release-signature-verification-module.md` (the `signing` module, shipped v0.1.0)

---

## 1. Context & motivation

`gitlab.com/phpboyscout/signing` v0.1.0 defines the `crypto.Signer`-producing
`Backend` contract and ships only the light `local` (PEM) backend; heavy/remote
backends are **injected** by the consumer. The AWS KMS backend still lives inside
go-tool-base at `pkg/signing/kms` — **84 AWS SDK import paths** — implementing the
(now external) contract.

Two forces make it time to extract it:

1. **Reuse.** Consumers beyond gtb will want a ready-made KMS backend rather than
   re-implementing one against the `Backend` interface. It should be importable
   without dragging in go-tool-base.
2. **Selective weight.** GCP KMS, Azure Key Vault, and HashiCorp Vault backends
   are planned. If each lived in one module (or in go-tool-base), every consumer
   would inherit *every* cloud SDK in its module graph. Per-provider modules let
   a consumer include exactly the backend(s) it needs and nothing else.

This spec establishes the **per-provider backend-module pattern** and delivers
its first instance, **`gitlab.com/phpboyscout/signing-aws-kms`**, following the
exact pattern used for the `signing` module. go-tool-base then depends on
`signing` **and** `signing-aws-kms` together in a single cut-over.

## 2. The pattern (and why a separate module per provider)

Each cloud/remote backend is its **own module** `gitlab.com/phpboyscout/signing-<provider>`
that:

- depends on `gitlab.com/phpboyscout/signing` (for the `Backend` contract + `Register`),
- adds **only its own** provider SDK,
- registers itself under a stable backend name via `init()` (blank-import to activate).

**Why a module, not a sub-package.** Go module-graph pruning propagates a
module's *entire* `go.mod` require list to every consumer that imports any of its
packages. A single backend module that bundled all clouds — or backends kept in
go-tool-base — would force AWS+GCP+Azure+Vault SDKs into the graph of a consumer
that wanted just one (or none). Separate modules quarantine each SDK: importing
`signing-aws-kms` pulls AWS and nothing else; not importing it pulls no AWS at
all. afmpeg (verification only) pulls neither go-tool-base nor any cloud SDK.

**Consumer matrix the pattern enables:**

| Consumer | Imports | Cloud SDKs in graph |
|---|---|---|
| afmpeg (verify) | `signing/verify` | none |
| a KMS-signing tool | `signing` + `signing-aws-kms` | AWS only |
| gtb (switches between) | `signing` + all backend modules | all (by design) |

## 3. Goals / non-goals

**Goals**

- Stand up `gitlab.com/phpboyscout/signing-aws-kms` with the current KMS backend,
  implementing `signing.Backend`, blank-import activated as backend `"aws-kms"`.
- `go.mod` carries only `signing` + the AWS SDK (+ `pflag` for the optional flag
  interface) — no go-tool-base, no other cloud SDK.
- A dependency-footprint guard asserting the module pulls **no go-tool-base and
  no non-AWS cloud SDK** (gcp/azure/vault).
- Integrate `signing` **and** `signing-aws-kms` into go-tool-base in one
  cut-over, deleting the in-tree `pkg/openpgpkey`, `pkg/signing` core, `pkg/setup`
  verification, **and** `pkg/signing/kms`; `gtb sign --backend aws-kms` and
  `gtb update` behave identically.
- Establish the naming/structure convention so GCP/Azure/Vault modules are
  drop-in repeats.

**Non-goals**

- Implementing the GCP/Azure/Vault backends (future modules; only the pattern is
  set here).
- Changing the KMS signing logic or the `Backend` contract (packaging only).
- A CLI (gtb is the CLI; these are mechanics).

## 4. Design — `signing-aws-kms`

### 4.1 Contents

Move `pkg/signing/kms/{kms.go,signer.go,kms_test.go}` into the new module,
repointing the `signing` import to `gitlab.com/phpboyscout/signing`:

- `backend` implements `signing.Backend`: `Name() == "aws-kms"`; `NewSigner(ctx,
  keyID)` returns a `crypto.Signer` backed by an AWS KMS RSA key (KMS RSA_4096
  SIGN_VERIFY → `*rsa.PublicKey`, satisfying `openpgpkey`'s RSA requirement and
  the verify package's ≥3072-bit strength policy). `keyID` is the KMS key ARN or
  alias.
- `init()` calls `signing.Register(&backend{})`.

### 4.2 The `--kms-region` flag → optional interface

The current backend implements `RegisterFlags(fs *pflag.FlagSet)` to add
`--kms-region`. The `signing.Backend` contract no longer includes `RegisterFlags`
(it is CLI-agnostic). Per the `signing` spec's decision, **the CLI front-end
defines its own optional flag interface and type-asserts**:

- The KMS backend **keeps** a `RegisterFlags(fs *pflag.FlagSet)` method (so it
  depends on `pflag` — acceptable; the module is heavy regardless).
- gtb's `sign` command type-asserts `b.(interface{ RegisterFlags(*pflag.FlagSet) })`
  and calls it when present (replacing today's unconditional call). Region also
  resolves from `AWS_REGION` via the SDK default chain, so a non-flag CLI still
  works.

### 4.3 Seams & credentials

Credentials/region resolve through the AWS SDK Go v2 default chain (env, profile,
web-identity/OIDC) — unchanged; the module owns no credential logic. If the
backend logs, it accepts an optional `*slog.Logger` (consistent with `signing`);
otherwise no logging seam is needed.

### 4.4 Footprint

`go.mod`: `gitlab.com/phpboyscout/signing`, `github.com/aws/aws-sdk-go-v2/...`,
`github.com/spf13/pflag`. The dep-guard forbids `go-tool-base`, and any
`cloud.google.com`/`gcp`, `azure`, or `hashicorp/vault` import — locking the
"AWS only" boundary so a careless import can't turn this into a multi-cloud blob.

## 5. Combined go-tool-base integration (the cut-over)

Done as one change (this is the Stage 8 the roadmap referred to, now covering
both modules):

- go-tool-base depends on `signing` **and** `signing-aws-kms`.
- **Delete** from go-tool-base: `pkg/openpgpkey`, `pkg/signing` (core + `local`),
  `pkg/setup/signing*.go` (verification), **and `pkg/signing/kms`**.
- **Repoint** (no aliases — pre-1.0 clean break):
  - `cmd/gtb` blank-imports `gitlab.com/phpboyscout/signing-aws-kms` (aws-kms) +
    `gitlab.com/phpboyscout/signing/local` (local).
  - `internal/cmd/sign` resolves backends via `signing.Get(...)`; its
    `RegisterFlags` call becomes the optional-interface type-assert (§4.2).
  - `SelfUpdater` / update verification use `signing/verify`; gtb injects its
    logger via `slog.New(<handler>)` and its hardened `*http.Client`.
- Verify `gtb sign --backend aws-kms`, `gtb sign --backend local`, `gtb update`,
  and the full suite (incl. the existing KMS integration test, re-homed to the
  new module — see §6).
- Migration note in `docs/reference/migration/` for the moved import paths.

**What shrinks:** go-tool-base sheds the openpgpkey/signing/verify/kms source it
no longer owns (less bespoke code to test and maintain); the shared mechanics now
live in reusable modules. gtb's *binary* still contains AWS (it blank-imports all
backends by design), but every other consumer's intersection with that code is
now a versioned dependency, not a copy.

## 6. Development model (TDD / BDD)

Mirrors `signing` and go-tool-base:

- Test-first; table-driven + `t.Parallel()`; `cockroachdb/errors`; **≥90%
  coverage** on unit-testable surface.
- The KMS backend's real-AWS path is an **integration test** gated behind real
  credentials (as in go-tool-base today): `INT_TEST=1` / `INT_TEST_SIGNING_AWS=1`,
  skipped otherwise. Unit tests cover construction, flag/region resolution, keyID
  handling, and error mapping with the AWS calls faked at the SDK seam (no
  package-level mocking hooks).
- The **dependency-footprint guard** (§4.4) runs as a CI gate.
- A small Godog suite is *optional* for a backend adapter (the signing contract
  is already BDD-covered in `signing`); see open question 4.

## 7. Documentation (Diátaxis)

Proportionate to a backend adapter:

- **Reference → pkg.go.dev** (godoc + an `Example` showing
  `signing.Get("aws-kms")` → sign).
- **How-to:** *sign a release with AWS KMS* (key setup, IAM/OIDC, `keyID` ARN,
  region, wiring into a CLI); *add another provider backend* (points back to the
  pattern).
- **Explanation:** a short page on the per-provider module pattern + the AWS IAM
  trust/credentials model.
- README as entry point. A full zensical site is optional (open question 5) — a
  strong README + pkg.go.dev may suffice for an adapter.

## 8. Repository & project framework

New repo `gitlab.com/phpboyscout/signing-aws-kms`, bootstrapped exactly like
`signing` (it is a library; no binaries, no goreleaser):

- cicd `go-lint` / `go-test` / `go-security`; releaser-pleaser; renovate-self;
  `zensical-pages` only if a site is published. `.golangci.yaml` (v2, local
  prefix `gitlab.com/phpboyscout/signing-aws-kms`), `justfile`, `.mockery.yml`,
  `renovate.json`, `requirements-lock.txt` (if docs site).
- Same CI-runner reality as `signing`: group disables shared runners → the
  `homelab` group runner; MR pipelines may need a manual trigger; releaser-pleaser
  uses the group token; never disable the must-succeed gate (arm auto-merge +
  trigger a fresh pipeline).
- `go 1.26.x`; depends on `signing` at its released tag.

## 9. Resolved decisions (carried from the `signing` spec)

- Keep `cockroachdb/errors`. Registry + injection both supported. Minimal
  `Backend` contract; per-backend CLI flags via the CLI's optional interface.
  Hard cut-over in gtb (no aliases, pre-1.0) + migration note. FF merges.

## 10. Open questions

1. **Module name / package name.** Module `gitlab.com/phpboyscout/signing-aws-kms`
   (proposed); package `awskms` vs `kms`. (Proposed: package `awskms` for an
   unambiguous import; backend name string stays `"aws-kms"` for `gtb sign`
   compatibility.)
2. **Backend name string.** Keep `"aws-kms"` (no change to `gtb sign --backend`).
   (Proposed: yes.)
3. **Future backends in this spec?** Keep this spec aws-kms-only and let GCP/Azure/
   Vault be drop-in repeats of the pattern, or enumerate stubs now. (Proposed:
   aws-kms only; pattern documented for the rest.)
4. **Godog for the adapter?** Full suite vs unit + gated integration only.
   (Proposed: unit + gated AWS integration; skip Godog — contract is covered in
   `signing`.)
5. **Docs site vs README + pkg.go.dev.** (Proposed: README + pkg.go.dev + the
   two how-to/explanation pages in-repo; full zensical site optional.)
6. **Optional logging seam.** Does the KMS backend log enough to warrant an
   injectable `*slog.Logger`? (Proposed: add only if it currently logs.)
7. **Release ordering.** Cut `signing-aws-kms v0.1.0` before the gtb cut-over so
   gtb depends on a tag, not a pseudo-version. (Proposed: yes.)

## 11. Work items (once approved)

1. Bootstrap `signing-aws-kms` repo to the standard framework (§8).
2. Move `kms.go`/`signer.go`/tests; repoint the `signing` import; implement the
   optional `RegisterFlags`; add the dependency-footprint guard (no go-tool-base,
   no non-AWS cloud SDK).
3. Unit tests (≥90%) + re-home the gated AWS integration test; docs + `Example`.
4. Cut `signing-aws-kms v0.1.0`.
5. **Combined gtb cut-over (§5):** depend on `signing` + `signing-aws-kms`; delete
   the four in-tree trees incl. `pkg/signing/kms`; repoint callers + blank-imports
   + the sign command's flag type-assert; verify `gtb sign`/`gtb update` + suite;
   migration note.
6. afmpeg + ffmpeg-wasi (the original Stage 9): afmpeg → `signing/verify`;
   ffmpeg-wasi → `gtb sign`.
