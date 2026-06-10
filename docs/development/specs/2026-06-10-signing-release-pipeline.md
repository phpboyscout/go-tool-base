---
title: "Generator release-signing pipeline — `gtb enable signing` writes the GoReleaser `signs:` block"
description: "Extend `gtb enable signing` to own the produce half of release signing: when a signing key is recorded, scaffold a shim-free GoReleaser `signs:` block into the generated `.goreleaser.yaml` that calls `gtb sign` directly. Records backend, key_id, kms_region and public_key in the manifest signing block; emits the block only when a key id is set; the backend is dynamic (the block emits backend-specific args). Companion to the embed-and-wire spec (2026-06-10-signing-generator-feature.md), which deliberately left the release pipeline out of scope."
status: DRAFT
date: 2026-06-10
tags:
  - specification
  - generator
  - signing
  - goreleaser
  - release
  - openpgp
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Generator release-signing pipeline — `gtb enable signing` writes the GoReleaser `signs:` block

Authors
:   Matt Cockayne

Date
:   10 June 2026

Status
:   DRAFT

---

## Summary

The signing-generator feature (`2026-06-10-signing-generator-feature.md`) taught
`gtb enable signing` the *verify* half of release signing: scaffold
`internal/trustkeys`, wire `Signing: props.SigningConfig{EmbeddedKeys: trustkeys.Keys()}`,
emit a manifest-driven `signing.go`. It explicitly left the *produce* half out of
scope — "Minting and WKD publishing are out of scope; this is only the embed-and-wire
half." In practice that gap means a generated project still can't *create* the
`checksums.txt.sig` its own verifier is now looking for. The release pipeline that
produces the signature is left for the author to hand-write.

This spec closes that gap. When the author records a signing key,
`gtb enable signing` also writes a GoReleaser `signs:` block into the generated
`.goreleaser.yaml`, so the produce side is generated and manifest-driven just like the
verify side. No hand-edited release config.

## Motivation

go-tool-base signs its own releases through a `scripts/sign-release.sh` shim that
GoReleaser's `signs:` block invokes. The shim exists for two reasons that are specific
to go-tool-base and **do not apply to a downstream consumer**:

1. **It signs itself.** The shim runs `go run ./cmd/gtb` because the GoReleaser CI
   image ships Go but not an installed `gtb` — go-tool-base cannot use a `gtb` binary to
   sign the `gtb` binary it is building. A consumer's release pipeline calls the
   *installed* `gtb`; there is no bootstrap problem.
2. **It maps env to flags.** The shim reads `GTB_SIGNING_KEY_ID`,
   `GTB_SIGNING_KEY_PUBLIC` and `AWS_REGION` (with defaults) and threads them, plus
   GoReleaser's positional `${artifact}`/`${signature}`, into `gtb sign`. But because
   the *generator* writes the consumer's `.goreleaser.yaml`, it already knows every one
   of those values and can spell the whole `args:` list out inline.

So a consumer needs no shim at all. The generated `.goreleaser.yaml` calls `gtb sign`
directly. One fewer generated file, no bash, no env indirection, and the signing
invocation is visible in the release config rather than hidden in a script.

## The generated `signs:` block

When signing is enabled **and** a key id is recorded, the generated `.goreleaser.yaml`
carries (shown for the `aws-kms` backend):

```yaml
signs:
  - id: checksums
    cmd: gtb
    args:
      - "--ci"
      - "sign"
      - "--backend"
      - "aws-kms"                                         # signing.backend
      - "--kms-region"
      - "eu-west-2"                                       # signing.kms_region (aws-kms only)
      - "--key-id"
      - "alias/acme-release-signing-v1"                   # signing.key_id
      - "--public-key"
      - "internal/trustkeys/keys/signing-key-v1.asc"      # signing.public_key
      - "--output"
      - "${signature}"
      - "${artifact}"
    artifacts: checksum
    signature: "${artifact}.sig"
    output: true
```

This mirrors the proven invocation in go-tool-base's own shim
(`scripts/sign-release.sh`) and the canonical example in `internal/cmd/sign/sign.go`:
`--backend`, `--kms-region` (a flag registered by the kms backend,
`pkg/signing/kms/kms.go`, default `eu-west-2`), `--key-id` (an id / ARN / alias / PEM
path), `--public-key` the embedded `.asc`, the positional input, `--output` the detached
signature. `${artifact}` / `${signature}` are GoReleaser's own placeholders and pass
through Go templating untouched; `{{ .Signing.* }}` is rendered at generate time.

### Dynamic backend

`gtb sign` already supports more than one backend (`signing.Names()` — today `aws-kms`
and `local`), and more are expected. The `signs:` block is therefore **backend-driven**,
not hardcoded: the `--backend` value comes from `signing.backend`, and backend-specific
args are emitted conditionally. `--kms-region` is emitted only for `aws-kms`:

```yaml
    args:
      - "--ci"
      - "sign"
      - "--backend"
      - "{{ .Signing.Backend }}"
      {{- if eq .Signing.Backend "aws-kms" }}
      - "--kms-region"
      - "{{ .Signing.KMSRegion }}"
      {{- end }}
      - "--key-id"
      - "{{ .Signing.KeyID }}"
      - "--public-key"
      - "{{ .Signing.PublicKey }}"
      - "--output"
      - "${signature}"
      - "${artifact}"
```

`--key-id` and `--public-key` are common to every backend (both are required flags on
`gtb sign`). A new backend with its own knobs (a GCP project, a Vault address) adds one
more `{{- if eq .Signing.Backend "…" }}` branch here and a matching manifest field; the
shape doesn't change. `key_id` is deliberately generic (not `kms_key_id`) for the same
reason — it maps straight to `--key-id` whatever the backend.

Notes:

- **The public-key path is configurable** (`signing.public_key`), defaulting to the
  `internal/trustkeys/keys/signing-key-v1.asc` convention — the same default
  go-tool-base's shim uses and the filename the tutorial's Part 5 establishes. Consumers
  name keys all sorts of ways and add `-v2`, `-v3` on rotation, so the path is recorded
  rather than assumed.
- **AWS credentials are the CI pipeline's job, not the generator's.** The signs job
  resolves credentials via the AWS SDK default chain (OIDC web-identity in CI — see the
  KMS/OIDC specs and the tutorial's Part 3). GoReleaser runs with `--skip=sign` when
  there is no signing context, so the block is inert on snapshot/local builds.

## Manifest

The `signing` block (`ManifestSigning`) gains four fields:

```yaml
properties:
  signing:
    enabled: true
    external_key_email: release@acme.dev
    key_source: both
    backend: aws-kms                                      # new (default aws-kms)
    key_id: alias/acme-release-signing-v1                 # new
    kms_region: eu-west-2                                  # new (aws-kms; default eu-west-2)
    public_key: internal/trustkeys/keys/signing-key-v1.asc # new (default the convention)
```

All are `omitempty`. `backend` defaults to `aws-kms`; `kms_region` defaults to
`eu-west-2` when a key id is supplied for the aws-kms backend without a region;
`public_key` defaults to the convention path. So the generated block is always complete.

## CLI

`gtb enable signing` gains four flags:

- `--backend <name>` — the `gtb sign` backend (default `aws-kms`), validated against the
  registered backends (`signing.Names()`).
- `--key-id <id|arn|alias|path>` — the key the release pipeline signs with. Recording it
  is what turns the `signs:` block on.
- `--kms-region <region>` — default `eu-west-2` (used by the `aws-kms` backend).
- `--public-key <path>` — default `internal/trustkeys/keys/signing-key-v1.asc`.

```bash
# verify half only (as today) — no signs block yet
gtb enable signing --email release@acme.dev

# verify + produce: also writes the signs block
gtb enable signing --email release@acme.dev --key-id alias/acme-release-signing-v1
```

The `generate project` interactive wizard's signing step gains an optional "signing key
id (leave blank to wire the signs block later)" prompt (with backend/region/public-key
following when a key id is given), and `generate project` gains the matching
`--signing-backend` / `--signing-key-id` / `--signing-kms-region` / `--signing-public-key`
flags, so a key recorded at creation produces the block immediately.

**Emission gate:** the `signs:` block is templated only when
`signing.enabled && signing.key_id != ""`. Enabling signing without a key id (the N+1
rollout step, where you ship the embedded key before you produce signatures) leaves the
release config untouched. Recording the key later — `gtb enable signing` re-run with
`--key-id` — adds the block.

## Implementation

- **`ManifestSigning`** (`internal/generator/manifest.go`): add `Backend`, `KeyID`,
  `KMSRegion`, `PublicKey` (all `omitempty`).
- **Skeleton asset** (`internal/generator/assets/skeleton/.goreleaser.yaml`): add the
  conditional `{{ if and .Signing.Enabled .Signing.KeyID -}} signs: … {{ end -}}` block
  (with the backend-conditional args above) after the `checksum:` block. The asset is
  already a `text/template` with conditionals (`{{ if eq .ReleaseProvider "github" }}`)
  and the generate-path data struct already carries `Signing`
  (`internal/generator/skeleton.go`).
- **Regenerate-path data** (`internal/generator/regenerate.go`,
  `regenerateSkeletonFiles`): add `Signing ManifestSigning` to the reconstructed
  template `data` struct so the asset re-renders correctly on regenerate.
- **Defaulting** (`internal/generator/manifest.go` or the enable command): when a key id
  is set, default `backend`→`aws-kms`, `kms_region`→`eu-west-2`,
  `public_key`→the convention path, so a manifest written by hand or an older `enable`
  still renders a complete block.
- **Selective re-render on enable/disable** (`internal/generator/signing.go`):
  `applySigningPosture` re-renders the `.goreleaser.yaml` skeleton asset (through the
  existing hash-protected skeleton-asset render path, so a hand-edited `.goreleaser.yaml`
  is never clobbered) alongside the root command and the trustkeys/signing.go sync.
  Disabling signing (or re-running without a key id) removes the block.
- **Enable command** (`internal/cmd/enable/signing.go`): add the four flags, validate
  `--backend` against `signing.Names()`, apply defaults, pass into `ManifestSigning`.
- **Generate command** (`internal/cmd/generate/project.go`): add the matching flags +
  the optional wizard prompts.

## Testing

- Unit: manifest round-trip carries the four new fields; the skeleton asset renders the
  `signs:` block when a key id is set and omits it otherwise; `--kms-region` appears for
  `aws-kms` and not for `local`; backend validation rejects an unregistered backend;
  defaulting.
- Integration (gated, build-tagged like the existing signing integration tests): a
  `generate project --signing --signing-key-id alias/x` produces a `.goreleaser.yaml`
  whose `signs:` block contains the backend, key id and region, and the project still
  builds; `gtb enable signing --key-id` on an existing project adds the block; running
  `goreleaser check` against the generated config passes (if the binary is available in
  CI, else assert on the rendered YAML).

## Out of scope

- Wiring AWS OIDC credentials into the generated CI workflow (`.gitlab-ci.yml` /
  GitHub Actions). The signs job needs credentials, but those are forge- and
  account-specific and belong to the OIDC setup (a separate concern; the tutorial's
  Part 3). The generated `signs:` block is credential-agnostic; GoReleaser `--skip=sign`
  covers the no-credentials case.
- Per-backend config beyond `aws-kms`'s `kms_region`. The backend field is dynamic and
  the template is structured to extend, but only the `aws-kms` and `local` backends ship
  today, so only `aws-kms`'s region knob is wired now.
- Minting and WKD publishing (already out of scope in the companion spec).
