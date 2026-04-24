---
title: "Phase 2 Signing Prep: Pre-implementation Checklist"
description: "Ordered operational decisions and setup tasks that must complete before Phase 2 of the remote-update-checksum-verification spec can be implemented. Covers KMS selection, key generation inside the KMS, embedded public-key distribution, WKD endpoint setup, GoReleaser signs-block integration, and the unsigned-then-signed release rollout."
date: 2026-04-24
tags: [development, security, signing, gpg, wkd, kms, phase-2, prep]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Phase 2 Signing Prep

Phase 1 of the [remote-update-checksum-verification](specs/2026-04-02-remote-update-checksum-verification.md) spec (SHA-256 checksum manifest verification) is implemented. Phase 2 adds a detached GPG signature over the manifest so a VCS-platform compromise cannot produce a passing update: the signature can only be forged by an attacker who also controls the signing key.

Phase 2 has **operational prerequisites** that are out of scope for code changes — key generation inside a KMS, DNS + TLS setup for the WKD endpoint, access-policy decisions. This document captures those in order, so that when Phase 2 kicks off the only remaining work is code.

> This is a planning document. Status lives in the [spec](specs/2026-04-02-remote-update-checksum-verification.md); when Phase 2 transitions to IN PROGRESS the ordered steps here become the implementation playbook.

## Decisions so far

Updated as each gate is answered. Empty cells = still open.

| Gate | Decision |
|------|----------|
| **1 — Signing key storage** | **AWS KMS**, RSA-4096 asymmetric key, `eu-west-2` (London) region. OIDC federation from GitHub Actions → IAM role with `kms:Sign` only on the release key. Rationale: easiest onboarding for a greenfield cloud account, native GitHub OIDC support, and the spec explicitly accepts RSA-4096 where Ed25519 isn't available (AWS KMS doesn't expose Ed25519 for asymmetric signing). |
| **2 — WKD domain + release email** | **`openpgpkey.phpboyscout.uk`** subdomain, **`release@phpboyscout.uk`** role address. Static hosting via **Cloudflare Pages in Direct Upload mode** (free plan, auto-provisioned TLS via Cloudflare's CA, custom-domain binding is one DNS record). **No Git integration, no webhook** — the WKD directory is built locally and pushed via the Wrangler CLI authenticated by a Cloudflare API token scoped to `Pages: Edit` only. This intentionally excludes both GitHub and AWS from the deploy path: a compromise of either platform cannot poison the externally-served key. The Cloudflare account is administered with a distinct email + MFA factor from the GitHub and AWS accounts so all three trust anchors are independent. The WKD tree is reproducible from the public key file (which lives in offline storage alongside the rotation-authority backup), so no source-of-truth Git repo is needed. |
| **3 — Access policy** | CI signs via the OIDC-federated IAM role defined in Gate 1; no human holds the signing secret. Trust policy pins the role to `refs/tags/v*` on `phpboyscout/go-tool-base` so only tagged-release workflows can mint a signature. A protected `release` environment in GitHub Actions gates the sign job on manual approval — solo maintainer approves their own runs for now; promotes cleanly to a required-reviewers gate when the project grows. Role policy also scoped to a single action (`kms:Sign`) on a single key ARN. |
| **4 — Rotation-authority key** | Generated once on a trusted offline workstation (`gpg --full-generate-key`, Ed25519, no subkey, no expiry, passphrase-protected). Private half stored two ways, both in a single home safe: one encrypted USB (LUKS or VeraCrypt with a strong passphrase) **and** one printed paper backup produced via `paperkey`. The two-copy rule covers the ways a single copy fails (USB bit-rot / paper physical damage) without the complexity of multi-site storage. Public half: embedded in `internal/version/trustkeys/rotation-authority.asc` alongside the primary signing key, and published via the same WKD endpoint. A written runbook at `docs/operations/key-rotation.md` is produced in the Phase 2 implementation PR — the mechanism that *uses* this key is Phase 4 per the spec's Resolved Decision #10, but the key must exist and be embedded *now* to protect binaries released in Phase 2 and later. |

## Why a prep doc

Three classes of question need answering before any Go code is written:

1. **Trust-anchor decisions** — which KMS holds the private key, which domain serves the external key, who can authorise a signing workflow. These shape the threat model you can credibly defend.
2. **Rollout ordering** — ship the public key in a binary release *before* the first signed release. Miss this and existing installs will fail to verify the new key, bricking self-update for that cohort.
3. **GoReleaser shape** — the `signs` block interacts with every prior decision. Document the intended shape up-front so the `.goreleaser.yaml` diff at implementation-time is a verbatim paste.

## Decision gates (must answer before implementation)

Each gate blocks the one below it.

### Gate 1 — Signing key storage

| Option | Trust anchor | Operator burden | Notes |
|--------|--------------|-----------------|-------|
| AWS KMS with OIDC federation | AWS IAM | Low (IaC-managed) | Recommended for AWS-native operators. OIDC lets the release workflow assume a role without a stored long-lived secret. |
| GCP Cloud KMS | GCP IAM | Low | Equivalent to AWS for GCP shops. |
| Azure Key Vault | Entra ID | Low | Equivalent for Azure. |
| HashiCorp Vault Transit | Vault | Medium | Works offline; portable across clouds. Requires operating Vault. |
| YubiKey (hardware token) | Physical possession | High | Signing tied to a specific machine; good for single-maintainer projects. |
| **GitHub encrypted secret** | GitHub IAM | Low | **Last resort** — the secret is stored plaintext inside GitHub's secret store; a platform compromise defeats Phase 2's core claim. Only use when no KMS is available. Requires `environment` protection + required reviewers. |

**Output of this gate:** a chosen option, an ADR-style note recording *why*, and a small shim script (`scripts/sign-release.sh`) abstracting the provider so the GoReleaser config stays provider-agnostic.

### Gate 2 — External key domain + email

The Web Key Directory (WKD) public-key URL is derived from an email address. This anchors the external trust source on DNS + TLS certificates you administer independently from your VCS.

- **Domain** — you must control DNS and TLS termination for the domain. Recommended: a dedicated `openpgpkey.<yourdomain>` subdomain so the WKD files never collide with site assets.
- **Release email** — the `userid` on the key. WKD hashes the local part to a z-base-32 string. Recommended: a role-based address (`release@<yourdomain>`) not a personal address, so rotation doesn't require re-signing.
- **TLS cert provenance** — public ACME (Let's Encrypt / ZeroSSL) is fine; an internal CA is not, because external users won't trust it.

**Output of this gate:** `DefaultExternalKeyEmail` constant value (e.g. `release@phpboyscout.uk`) and a DNS + TLS plan for `openpgpkey.<domain>`.

### Gate 3 — Access policy

Who can *authorise* a signing run, as distinct from who can *trigger* one:

- **CI signs, humans don't** — the release workflow assumes a role (via OIDC) that has `Sign` permission on the KMS key. No human ever holds the signing secret. Human-approved merges to `main` are the trust fence.
- **Two-person rule** — require a separate approval step before the `signs:` workflow runs (GitHub "required reviewers" on the `release` environment). Closes the "compromised CI runner" threat.
- **Emergency override** — a second "rotation-authority" key, stored offline (paper backup in a safe), able to sign a `rotate-keys.json` manifest that rotates the embedded trust set. Used only when the primary key is lost or compromised.

**Output of this gate:** IaC definitions for KMS key policy + GitHub environment gates; documented emergency-rotation runbook.

### Gate 4 — Rotation-authority key

This is the "break glass" key that authorises key rotation outside the normal sign-with-the-old-key path. It is generated once, never used in normal operation, and its private half is stored offline (paper + hardware-token hybrid is typical).

- Generate at the same time as the primary key.
- Embed its public half alongside the primary in `internal/version/trustkeys/`.
- Keep a written runbook for how to invoke it.

**Output of this gate:** the rotation-authority key exists, its public half is ready to embed, its private half is in offline storage.

## Key lifecycle — do not generate outside the KMS

Once Gate 1 is chosen, the key is generated **inside** the KMS or hardware token. This is non-negotiable: a generate-then-import flow creates an ephemeral plaintext copy of the private half on whatever machine did the generation, defeating the threat model.

**Ed25519 preferred; RSA-4096 acceptable** if the KMS doesn't support Ed25519 (some older HSMs still don't). DSA, 1024-bit RSA, and weak curves are refused by `LoadTrustSet` at binary startup.

### AWS KMS — GTB provisioning walkthrough

The commands below are the concrete steps to run once your new AWS account is active. The provisioning needs to happen once; the GitHub Actions workflow then assumes the role on every release.

**Pre-flight hardening (do this on the fresh root account before anything else):**

1. Enable MFA on the root user. Never use root for day-to-day work thereafter.
2. Create an IAM user with `AdministratorAccess` for day-to-day provisioning; lock the root credentials away. Enable MFA on this user too.
3. Set an account alias so the sign-in page isn't a 12-digit number (`aws iam create-account-alias --account-alias phpboyscout`).
4. Enable CloudTrail with a dedicated S3 bucket — every signing event gets logged and is audit-reviewable.

**Create the release-signing key:**

```bash
# From the IAM user (not root). eu-west-2 = London.
aws kms create-key \
  --region eu-west-2 \
  --key-spec RSA_4096 \
  --key-usage SIGN_VERIFY \
  --description "GTB release signing v1" \
  --tags TagKey=project,TagValue=gtb TagKey=purpose,TagValue=release-signing

# Alias it so the key can be renamed/rotated without changing references.
aws kms create-alias \
  --region eu-west-2 \
  --alias-name alias/gtb-release-signing-v1 \
  --target-key-id <key-id-from-previous-step>

# Export the public half in PEM; wrap into OpenPGP framing later
# (the OpenPGP conversion tooling lives in the first Phase 2 PR).
aws kms get-public-key \
  --region eu-west-2 \
  --key-id alias/gtb-release-signing-v1 \
  --output text --query PublicKey | base64 -d > signing-key-v1.pub.der
```

**OIDC trust for GitHub Actions** (so the release workflow can assume a role without a stored long-lived secret):

```bash
# 1. Register GitHub's OIDC provider in your account (once per account).
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1

# 2. Create an IAM role restricted to the release workflow on the gtb
#    repo's tags only. Replace <ACCOUNT-ID> with your account number.
cat > trust-policy.json <<'JSON'
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": { "Federated": "arn:aws:iam::<ACCOUNT-ID>:oidc-provider/token.actions.githubusercontent.com" },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": { "token.actions.githubusercontent.com:aud": "sts.amazonaws.com" },
      "StringLike": { "token.actions.githubusercontent.com:sub": "repo:phpboyscout/go-tool-base:ref:refs/tags/v*" }
    }
  }]
}
JSON

aws iam create-role \
  --role-name gtb-release-signer \
  --assume-role-policy-document file://trust-policy.json

# 3. Attach a policy that allows ONLY Sign on ONLY this key.
cat > sign-policy.json <<'JSON'
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["kms:Sign", "kms:GetPublicKey"],
    "Resource": "arn:aws:kms:eu-west-2:<ACCOUNT-ID>:key/<KEY-ID>"
  }]
}
JSON

aws iam put-role-policy \
  --role-name gtb-release-signer \
  --policy-name kms-sign-release-key \
  --policy-document file://sign-policy.json
```

The workflow then authenticates with:

```yaml
- uses: aws-actions/configure-aws-credentials@v4
  with:
    role-to-assume: arn:aws:iam::<ACCOUNT-ID>:role/gtb-release-signer
    aws-region: eu-west-2
```

No long-lived AWS credentials exist anywhere; the workflow gets a 15-minute token via OIDC only when running on a `v*` tag.

### GCP Cloud KMS

```bash
gcloud kms keys create release-signing-v1 \
  --keyring=gtb-signing --location=global \
  --purpose=asymmetric-signing --algorithm=ec-sign-ed25519
gcloud kms keys versions get-public-key 1 \
  --key=release-signing-v1 --keyring=gtb-signing --location=global \
  > signing-key-v1.pem
```

### HashiCorp Vault Transit

```bash
vault write -f transit/keys/gtb-release \
  type=ed25519 exportable=false
vault read -format=json transit/keys/gtb-release \
  | jq -r '.data.keys["1"].public_key' > signing-key-v1.pem
```

### YubiKey (OpenPGP applet)

```bash
# Connect the YubiKey, then:
gpg --card-edit
# admin → generate → "sign" slot → 25519 → primary key only, no encrypt/auth
gpg --armor --export release@yourdomain > signing-key-v1.asc
```

In every path the exported file is the **public** half only. The private half never leaves the KMS / hardware.

## Distribution: embed + WKD

Two copies of the public key exist; the verifier requires them to agree (fingerprint equality) before accepting a signature.

### Embedded copy

```
internal/version/trustkeys/
├── README.md
├── signing-key-v1.asc       # current primary key
├── rotation-authority.asc   # break-glass key
```

A CI gate (`make check-no-private-keys` / a pre-commit hook) refuses any file containing `PRIVATE KEY` to prevent accidental commit of the private half during local development.

### External copy — Web Key Directory

WKD serves a public key from a well-known URL derived from the email address's local part:

```
WKD URL: https://openpgpkey.<yourdomain>/.well-known/openpgpkey/<yourdomain>/hu/<z-base-32-hash>?l=<local-part>
```

Generate the URL and file layout with `gpg-wks-client`:

```bash
gpg-wks-client --install-key signing-key-v1.asc release@yourdomain
# produces a directory you rsync/upload to the static host
```

#### GTB-specific: Cloudflare Pages via Direct Upload

The whole point of an external trust anchor is that it must be administered independently from both the codebase and the signing-key store. To hold that property, the deploy path itself must avoid GitHub and AWS — a webhook-driven Git deploy from `phpboyscout/openpgpkey-phpboyscout-uk` would re-introduce a single GitHub compromise as a sufficient condition to poison the WKD-served key.

Cloudflare Pages supports two mutually-exclusive deploy modes; we use the second:

| Mode | Source of truth | Risk for our use case |
|------|-----------------|------------------------|
| Git integration | A connected GitHub / GitLab repo, deployed by Cloudflare on push | A repo compromise (or a Cloudflare → GitHub OAuth compromise) lets the attacker poison the WKD |
| **Direct Upload** | Anywhere — files are pushed via the Wrangler CLI authenticated by a Cloudflare API token | No upstream Git, no webhook, no GitHub coupling. Only an attacker holding both the Cloudflare API token *and* DNS control can poison the WKD. |

**One-time setup** (on a trusted local machine):

```bash
# 1. Create a Cloudflare account on a distinct email address from your
#    GitHub and AWS accounts, with its own MFA factor (different
#    authenticator app, ideally a hardware key).
# 2. In the dashboard: My Profile → API Tokens → Create Token.
#    - Permission: "Account → Cloudflare Pages → Edit" only.
#    - Account resources: scoped to your single Cloudflare account.
#    - No Zone permissions, no DNS edit, no other scopes.
# 3. Create the Pages project (no Git integration):
#    Workers & Pages → Create → Pages → "Upload assets" → name
#    `openpgpkey-phpboyscout-uk`. The project starts empty.
# 4. Bind the custom domain in the project settings:
#    Custom domains → Set up → openpgpkey.phpboyscout.uk
#    Cloudflare provisions the TLS cert and gives you a CNAME target.
# 5. Add the CNAME at your DNS host, pointing
#    openpgpkey.phpboyscout.uk → <project>.pages.dev.

# Install Wrangler locally (one-time):
npm install -g wrangler  # or: brew install cloudflare-wrangler
```

**Per-key-rotation deploy** (rare — once a year or less in steady state):

```bash
# Generate the WKD directory tree from the public key file you keep
# alongside the rotation-authority backup in offline storage.
mkdir -p wkd-staging
cd wkd-staging
gpg-wks-client --install-key /path/to/signing-key-v1.asc release@phpboyscout.uk

# The result is a .well-known/openpgpkey/phpboyscout.uk/hu/... tree.
# Push it directly to Cloudflare Pages — no Git involved.
export CLOUDFLARE_API_TOKEN=...   # from password manager
wrangler pages deploy . \
  --project-name=openpgpkey-phpboyscout-uk \
  --commit-dirty=true \
  --branch=main
```

The API token never lands in CI or in any cloud account other than the Cloudflare one. Rotating it is a 30-second job in the Cloudflare dashboard; the token grants no access to anything except this one Pages project.

**Disaster recovery without a Git repo:** the WKD tree is fully reproducible from the public key file, so backing up the key (already done — paperkey + encrypted USB in the safe per Gate 4) covers re-deploy. There is no need for a second Git source-of-truth, and adding one would re-introduce the GitHub coupling we are deliberately avoiding.

## GoReleaser integration

GoReleaser's `signs:` block is provider-agnostic; it shells out to a command that must produce `checksums.txt.sig` when given `checksums.txt`. A thin shim script keeps `.goreleaser.yaml` unaware of which KMS is in use.

### `.goreleaser.yaml` — the intended block

When Phase 2 kicks off, add this block **after** the existing `checksum:` entry:

```yaml
# Phase 2: GPG signature over the checksums manifest. Clients that
# require_signature reject releases where the signature is missing or
# does not verify against the embedded + WKD-fetched trust set.
# See docs/development/phase2-signing-prep.md for the shim contract.
signs:
  - cmd: "./scripts/sign-release.sh"
    signature: "${artifact}.sig"
    artifacts: checksum   # sign checksums.txt ONLY, not each binary
    args:
      - "--input=${artifact}"
      - "--output=${signature}"
      - "--key-id=${GTB_SIGNING_KEY_ID}"
```

`artifacts: checksum` is deliberate — signing the manifest (not individual binaries) means one signature protects the whole release via the hash chain, and the per-binary build pipeline is unchanged.

### `scripts/sign-release.sh` — the shim contract

The shim takes `--input <file>` and `--output <sig-file>` and writes a detached ASCII-armored signature. The implementation differs per KMS but the interface is stable, letting the GoReleaser config stay the same across providers.

**Local dev variant** (GPG on disk, passphrase-protected):

```bash
#!/usr/bin/env bash
set -euo pipefail
while [[ $# -gt 0 ]]; do
  case "$1" in
    --input=*)  INPUT="${1#*=}" ;;
    --output=*) OUTPUT="${1#*=}" ;;
    --key-id=*) KEY_ID="${1#*=}" ;;
  esac
  shift
done
gpg --batch --yes --armor \
    --local-user "$KEY_ID" \
    --detach-sign \
    --output "$OUTPUT" "$INPUT"
```

**AWS KMS variant** (the release CI replaces the shim at deploy time):

```bash
#!/usr/bin/env bash
set -euo pipefail
# ... parse args like above ...
aws kms sign \
  --key-id "$KEY_ID" \
  --message "fileb://${INPUT}" \
  --message-type RAW \
  --signing-algorithm ECDSA_SHA_256 \
  --output text --query Signature \
  | base64 -d > "${OUTPUT}.raw"
# Wrap the raw signature in OpenPGP framing (detached, ASCII-armored).
# Implementation left to the first Phase 2 PR — this is the one place
# that genuinely needs Go tooling (encoding/hex + OpenPGP packet
# builder from ProtonMail/go-crypto).
```

### Release matrix

The shim is invoked per-release by the CI workflow that has an OIDC token scoped for the KMS. Verify locally with:

```bash
gpg --verify dist/checksums.txt.sig dist/checksums.txt
```

## Rollout ordering — ship the key before the signature

Phase 2 flips `setup.DefaultRequireSignature = true` only after one full release has included the embedded public key without yet requiring it. The cohort matrix:

| Release | Embedded key? | Signed? | `DefaultRequireSignature` | Existing installs see |
|---------|---------------|---------|---------------------------|-----------------------|
| N (current — Phase 1) | No | No | n/a | checksum only |
| N+1 | **Yes** | No | `false` | checksum only (installs the key as a side effect) |
| N+2 | Yes | **Yes** | `false` | signature verified, not required |
| N+3 | Yes | Yes | `true` | signature required; old releases without `.sig` refused |

Skipping N+1 — i.e., embedding the key and shipping a signed release in the same version — breaks self-update for any user whose currently-installed version predates the embedded key. They'd have no trust anchor for the new signature.

## Dual-sign during rotation

When rotating the signing key:

1. Add the new public key to `internal/version/trustkeys/` alongside the old.
2. Ship a release with both keys embedded, still signed by the old key.
3. In the next release, sign with **both** old + new (GoReleaser supports multiple `signs:` entries).
4. Once every supported install has the new key (wait out the support window), drop the old key from both the embedded dir and WKD, sign only with the new.

The trust set is a *set* — signatures pass if any key in the set verifies.

## Implementation order (once gates are clear)

This is the spec's Phase 2a/2b/2c/2d condensed into a single checklist:

- [ ] Gate 1 satisfied: KMS chosen, key generated inside it, public half exported.
- [ ] Gate 2 satisfied: WKD endpoint live, public key served, fingerprint matches the exported half.
- [ ] Gate 3 satisfied: CI role policy set, required-reviewers configured on the `release` environment.
- [ ] Gate 4 satisfied: rotation-authority key exists, private half in offline storage.
- [ ] Embed `signing-key-v1.asc` + `rotation-authority.asc` under `internal/version/trustkeys/`.
- [ ] Add `github.com/ProtonMail/go-crypto` to `go.mod`.
- [ ] Implement `pkg/setup/signing.go` — `TrustSet`, `LoadTrustSet` (with min-strength policy), `VerifyManifestSignature`, `KeyResolver` + three built-ins.
- [ ] Add `SignatureProvider` optional interface to `pkg/vcs/release`.
- [ ] Activate in `SelfUpdater.Update()` — verify order: resolver → signature → parse manifest.
- [ ] Add `scripts/sign-release.sh` (local GPG default; CI replaces).
- [ ] Add the `signs:` block to `.goreleaser.yaml` per the shape above.
- [ ] Ship release N+1: key embedded, not yet signed, not yet required.
- [ ] Ship release N+2: signed but not required.
- [ ] Flip `setup.DefaultRequireSignature = true` → release N+3.

## Related

- [Spec: Remote Update Integrity — Checksums + GPG Signatures](specs/2026-04-02-remote-update-checksum-verification.md)
- [How-To: Secure Releases](../how-to/secure-releases.md) — end-user documentation for both phases
- [Component: Setup Package](../components/setup/index.md#remote-checksum-verification-phase-1)
- [Component: VCS Release Providers](../components/vcs/release.md) — `ChecksumProvider` and (planned) `SignatureProvider` interfaces
