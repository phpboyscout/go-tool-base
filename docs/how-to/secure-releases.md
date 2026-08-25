---
title: Secure Releases, Checksum Verification
description: How to publish and consume cryptographically-verifiable releases so self-updates reject tampered binaries. Phase 1 covers same-origin SHA-256 checksum verification; Phase 2 (GPG signature verification) is a planned extension.
tags: [how-to, update, security, checksum, releases]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Secure Releases: Checksum Verification

GTB's self-update flow verifies every downloaded binary against a GoReleaser-produced `checksums.txt` manifest before installing it. A tampered or truncated binary is rejected; a passing check is logged at INFO (`"checksum verified"`) and the update proceeds.

This is **Phase 1** of the release-integrity work from [`0056-remote-update-checksum-verification`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0056-remote-update-checksum-verification). **Phase 2** adds a GPG signature over the manifest, closing the same-origin trust gap (an attacker who can replace the binary on the release platform can also replace `checksums.txt`, only a signature from an off-platform key defeats that). Phase 2's code is implemented and dormant; see [Phase 2 below](#phase-2-gpg-signed-manifests).

## How it fits together

```
Update() →  findReleaseAsset()           = target binary
         →  fetchChecksumsManifest()     = checksums.txt (via ChecksumProvider or asset list)
         →  VerifyChecksumFromManifest() = binary SHA-256 vs manifest entry
         →  extract()                    = only reached when verify succeeds
```

`checksums.txt` is GoReleaser's default manifest. One `<hex-sha256>  <filename>` entry per line. If your `.goreleaser.yaml` uses the defaults, no changes are needed; the file is already attached to every release.

## Producing verifiable releases

### GoReleaser (recommended)

The default GoReleaser `checksum` block generates `checksums.txt` and attaches it to the release. No configuration change is required. Verify locally with:

```bash
just snapshot
ls dist/checksums.txt
```

### Manual / CI pipelines

If you don't use GoReleaser, produce the manifest with standard `sha256sum` output and upload it alongside the binaries. The file format is:

```
<64-hex-hash>  <filename>
<64-hex-hash>  <filename>
```

Blank lines at end-of-file are tolerated; **every other line must match** or the whole manifest is rejected (a truncated manifest never produces a false pass).

### Bitbucket

Upload `checksums.txt` to the repository's **Downloads** alongside the binaries (same upload flow as your release assets). The Bitbucket provider looks it up by exact filename: not via the asset-name regex that the binary uses.

### Direct HTTP releases

Set `checksum_url_template` in the `direct` configuration subtree to a URL template that expands to the manifest location:

```yaml
direct:
  url_template: https://releases.example.com/{tool}/{version}/{tool}_{os}_{arch}.{ext}
  checksum_url_template: https://releases.example.com/{tool}/{version}/checksums.txt
```

The release source itself names only the connection:

```go
props.Tool.ReleaseSource = props.ReleaseSource{Type: "direct"}
```

The same placeholders (`{version}`, `{version_bare}`, `{os}`, `{arch}`, `{tool}`, `{ext}`) are available.

## Consuming (tool author)

### Pick a failure mode

By default, a release without `checksums.txt` logs a warning and the update proceeds. This preserves backward compatibility for tools whose existing releases predate this feature. Once your tool has shipped at least one release with a manifest, flip the default to fail-closed:

```go
requireChecksum := true

props := &props.Props{
    Tool: props.Tool{
        Signing: props.SigningConfig{
            RequireChecksum: &requireChecksum,
        },
    },
}
```

It is a pointer so "unset" stays distinguishable from "explicitly false": nil
leaves the framework default (permissive).

### Overriding at runtime

End users can override via config file:

```yaml
update:
  require_checksum: true          # abort if manifest missing or verification fails
  checksum_asset_name: ""         # override default "checksums.txt" filename
```

…or via env var (respects the tool's configured env prefix):

```bash
export MYTOOL_UPDATE_REQUIRE_CHECKSUM=true
```

Config wins over env var; env var wins over the tool-author baseline in `props.Tool.Signing.RequireChecksum`.

> **GTB itself** sets `Signing.RequireChecksum`: every `gtb update` verifies. Override with `GTB_UPDATE_REQUIRE_CHECKSUM=false` or `update.require_checksum: false` in config only if you need to update across a legacy release that predates the manifest (all GoReleaser-built releases have it, so this should rarely apply).

### Size bounds

The manifest download is capped at `setup.DefaultMaxChecksumsSize` (1 MiB); the
binary download at `setup.DefaultMaxBinaryDownloadSize` (512 MiB). A hostile
server streaming beyond those bounds aborts with `ErrChecksumTooLarge`.

Those are constants. A tool legitimately shipping larger artefacts raises the
bound **per updater** rather than by reassigning a package variable:

```go
setup.NewUpdater(ctx, props, "", false,
    setup.WithMaxBinaryDownloadSize(2<<30), // 2 GiB
)
```

The previous mutable globals raced under `t.Parallel()` and scoped a bound to
the whole process rather than to the updater that needed it.

## Phase 2: GPG-signed manifests

Phase 1 defends against accidental corruption and single-asset tampering, but a full VCS compromise can replace both the binary and `checksums.txt` on the release. Phase 2 closes that gap by signing the manifest with a project-controlled GPG key. An attacker who replaces the files on the VCS still cannot produce a valid `checksums.txt.sig` without access to the private key.

!!! info "Verifier API extracted into the signing module"
    The verification primitives, `TrustSet`, the `KeyResolver` chain
    (embedded, WKD, composite), `LoadTrustSet`, the minimum-strength
    policy, and the `DefaultRequireSignature` / `DefaultKeySource` /
    `DefaultExternalKeyEmail` / `DefaultRequireExternalCrosscheck`
    variables: now live in the standalone **signing** module at
    **`gitlab.com/phpboyscout/go/signing/verify`** (v0.1.0). go-tool-base's
    `SelfUpdater` (still in `pkg/setup`) consumes them, injecting an
    `*slog.Logger` and a hardened `*http.Client`. Where the snippets
    below show these symbols with a `setup.` prefix, read them as
    `verify.`, `setup.NewUpdater` / `setup.WithKeyResolver` remain in
    `pkg/setup`, and `DefaultRequireChecksum` (Phase 1) stays there too.
    See the [Signature Verification component reference][svdocs] and the
    [signing module docs](https://signing.phpboyscout.uk).

> **Status**: the rollout is complete for `gtb` itself. Every release carries a
> detached `checksums.txt.sig` alongside the manifest, produced by the GoReleaser
> `signs` block in `.goreleaser.yaml`, and `internal/cmd/root/signing.go` sets
> `verify.DefaultRequireSignature = true`, so `gtb update` refuses an unsigned
> release rather than warning about one.
>
> **For a tool you build on GTB, the default is still off.** The framework ships
> `verify.DefaultRequireSignature = false` deliberately: a tool that demanded a
> signature before its users had received an embedded key in a prior release
> would lock those users out of the very update that fixes it. Flip it in your own
> `main()` once a signed release is out and a key has shipped ahead of it. The
> [Phase 2 Signing Prep](../development/phase2-signing-prep.md) checklist has the
> N+1 / N+2 / N+3 ordering, and the [Signature Verification component](../explanation/components/setup/signature-verification.md)
> has the verifier API.

### Producing signed releases

GoReleaser signs `checksums.txt` via a `signs` block that shells out to `scripts/sign-release.sh`, producing an ASCII-armored detached `checksums.txt.sig` (the exact shape `TrustSet.VerifyManifestSignature` expects):

```yaml
# .goreleaser.yaml
signs:
  - id: checksums
    cmd: scripts/sign-release.sh
    artifacts: checksum
    signature: "${artifact}.sig"
    args: ["${artifact}", "${signature}"]
    output: true
```

`scripts/sign-release.sh` does not touch a local gpg keyring. It shells out to
`gtb sign --backend aws-kms`, so the private half never leaves KMS and nothing on
the runner can export it. Three variables select the key, all with defaults:

| Variable | Default | What it is |
| :--- | :--- | :--- |
| `GTB_SIGNING_KEY_ID` | `alias/gtb-release-signing-v1` | the KMS key alias to sign with |
| `GTB_SIGNING_KEY_PUBLIC` | `internal/trustkeys/keys/signing-key-v1.asc` | the public half, which must be present in the working tree |
| `AWS_REGION` | `eu-west-2` | where the key lives |

**There is no signing gate, and the script fails closed.** If neither
`AWS_ACCESS_KEY_ID` nor `AWS_WEB_IDENTITY_TOKEN_FILE` is set, `sign-release.sh`
refuses to sign and exits non-zero rather than quietly producing an unsigned
release. A tag pipeline that cannot reach KMS fails; it does not degrade.

CI resolves credentials through **OIDC web identity** rather than a stored
secret. GitLab injects a token with `aud: sts.amazonaws.com`, the job writes it to
a file, and the AWS SDK assumes the signer role. That role's trust policy pins it
to `project_path:phpboyscout/go-tool-base:ref_type:tag:ref:v*`, so a leaked role
ARN still only lets this project's **tag** pipelines sign anything.

> **Dual-signing window.** Since the 2026-07-24 key rotation the job also sets
> `GTB_SIGNING_KEY_ID_2` and `AWS_ROLE_ARN_2`, and `sign-release.sh` merges the
> second signature into the same `checksums.txt.sig`. Binaries already in the
> field trust v1 only; new ones trust v1 and v2, so both verify. The window
> closes by deleting those variables from the `goreleaser` job.

The **sign→verify contract**. That a signature `gtb sign` produces is accepted by
the same trust set self-update enforces, is covered by
`TestSignVerifyContract_*` in `internal/cmd/sign`. Those tests sign a manifest
through the real `runSign` path, verify it via `verify.LoadTrustSet`, and assert
that both a tampered manifest and an untrusted signing key are rejected with
`ErrSignatureInvalid`. They need no credentials and run in the normal unit suite.

!!! warning "Coverage gap: the KMS path in `scripts/sign-release.sh`"
    The script itself is not exercised by any test. It is KMS-only, it shells out
    to `gtb sign --backend aws-kms` and refuses to run without AWS credentials, so
    covering it requires a real KMS key and OIDC-derived credentials.

    The previous end-to-end test (`TestSignReleaseScript_VerifiesViaTrustSet`, gated
    `INT_TEST_SIGNING=1`) drove the script with **gpg** and was lost when signing
    moved to [`go/signing`](https://signing.go.phpboyscout.uk). It cannot simply be
    restored: the gpg path it depended on no longer exists.

    What remains untested is therefore the script's argument wiring and its KMS
    round-trip, not the cryptographic contract above.

### Customised `.goreleaser.yaml` and `.gtb/ignore`

`gtb enable signing --key-id …` (and `gtb disable signing`) adds or removes the
top-level `signs:` block in your `.goreleaser.yaml`. It does this **without
re-rendering the whole file**, so a hand-customised release config, extra
builds, `app_bundles:`, `dmg:`, deliberate platform exclusions, is preserved.
The precedence is:

1. **Listed in [`.gtb/ignore`](configure-generator-ignore.md)** → the file is
   never written (this takes precedence over the command's overwrite mode). The
   command prints the exact `signs:` block to paste and continues with the rest
   of enable signing.
2. **Present and safely editable** → only the top-level `signs:` key is
   injected (enable) or removed (disable); every other block, comment, and
   scalar is left byte-for-byte intact. Disable removes **only** a block gtb
   itself wrote (identified by its `# Release signing: gtb enable signing wired
   this.` marker); an author-written `signs:` block is never touched.
3. **A `signs:` block already exists, the file is unparseable, or absent-then-
   customised** → the file is not modified; the command prints the block to
   paste (advisory) and still scaffolds `internal/trustkeys`, wires the root
   command, and updates the manifest.

Because the release-config edit can degrade to an advisory, always check the
command output: it states clearly when `.goreleaser.yaml` was **not** modified
and manual action is required. Only a brand-new (absent) `.goreleaser.yaml` is
rendered from the full skeleton.

### Trust model at a glance

A signature is only as trustworthy as the key used to verify it. Phase 2 uses a **composite trust set**: the verifier loads public keys from two independent sources and requires their fingerprints to agree before accepting a signature.

```
┌─────────────────────┐      ┌──────────────────────────────┐
│  embedded in binary │      │   external: Web Key Directory │
│  (//go:embed)       │      │   or custom HTTPS endpoint    │
└──────────┬──────────┘      └──────────────┬───────────────┘
           │                                │
           └──────────►  CompositeResolver ◄┘
                             fingerprints must match
                                     │
                                     ▼
                              TrustSet ──► verify(checksums.txt.sig)
```

- **Embedded key**: baked into each binary at build time via `//go:embed`. Works offline and in air-gapped environments. Rotates only when a new binary is shipped.
- **External key (third-party source)**: fetched from an HTTPS endpoint under a domain you control. For a VCS compromise to produce a valid signature, the attacker must *also* control your DNS and TLS termination; the two trust anchors are administered independently. The canonical implementation is [Web Key Directory (WKD)](https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/), an OpenPGP RFC-draft serving public keys from a well-known path. Other HTTPS endpoints (self-hosted, Vault, a static S3 bucket) are supported via a custom `KeyResolver`.

### Resolver implementations

```go
// Interface — implement this to plug in any key source.
type KeyResolver interface {
    Resolve(ctx context.Context) (*TrustSet, error)
}
```

Three ship with GTB:

| Resolver | Source | Offline? | Primary use |
|----------|--------|----------|-------------|
| `setup.NewEmbeddedResolver(...)` | `//go:embed` of `*.asc` files in `internal/trustkeys/keys/` | ✅ Yes | Always available; the fallback that keeps air-gapped updates working. |
| `setup.NewWKDResolver(cfg)` | `https://openpgpkey.<domain>/.well-known/openpgpkey/<domain>/hu/<z-base-32>?l=<email>` | ❌ No | The project's public key published via the GPG WKD standard; cross-checks the embedded copy. |
| `setup.CompositeResolver{Resolvers: []KeyResolver{embedded, wkd}}` | Both, with fingerprint-equality enforcement | ⚠️ Partial | The production default. Offline builds still work via `update.key_source=embedded`. |

### Configuration surface

```yaml
update:
  require_signature: false               # library default; flip on via DefaultRequireSignature
  key_source: both                       # "embedded" | "external" | "both"
  external_key_email: release@example.com  # drives the WKD URL
  require_external_crosscheck: false     # true → WKD failure aborts update
  signature_asset_name: ""               # override default "checksums.txt.sig"
```

Compile-time overrides (tool authors in `main`), set on the
`gitlab.com/phpboyscout/go/signing/verify` package:

```go
verify.DefaultRequireSignature = true
verify.DefaultKeySource = "both"
verify.DefaultExternalKeyEmail = "release@example.com"
verify.DefaultRequireExternalCrosscheck = true
```

### Publishing a public key

1. **Generate** an Ed25519 signing keypair (RSA-4096 is acceptable if your KMS doesn't support Ed25519). DSA, 1024-bit RSA, and weak curves are refused at load time.
2. **Embed** the public half. Drop the ASCII-armored file at `internal/trustkeys/keys/signing-key-v1.asc` in your repo: `go:embed` picks it up at build time. Tests gate a CI check that refuses any accidentally committed private key.
3. **Publish** the same key via your chosen external source:
   - **WKD**: serve the ASCII-armored key at the WKD path under `openpgpkey.<yourdomain>`. DNS and TLS cert are your trust anchors, administered independently from your VCS.
   - **Custom HTTPS**: implement `KeyResolver` with your own endpoint (Vault, static S3, internal CA-served HTTPS). Register it via `setup.WithKeyResolver` on `SelfUpdater`.
4. **Store** the private half in a KMS (AWS/GCP/Azure), Vault Transit, or a hardware token. GitHub encrypted secrets are a last resort. See the spec's Key Management section.

### Diagnosing live updates from logs

Every `gtb update` (or your tool's equivalent) emits structured log lines that name the concrete resolver used:

```
INFO update signature verification configured resolver=composite[embedded,wkd:openpgpkey.<yourdomain>]
INFO signature verified resolver=composite[embedded,wkd:openpgpkey.<yourdomain>]
```

The `resolver=` value is the most useful single field for support triage. `composite[embedded,wkd:…]` means both trust anchors were consulted and agreed; `embedded` or `wkd:…` alone means only one anchor was consulted (cryptographically sound but lower defence-in-depth). Full interpretation table (including failure-side log shapes for active-tampering signals) lives in the [Signature Verification component reference][svdocs].

[svdocs]: ../explanation/components/setup/signature-verification.md#interpreting-verifier-log-output

### Custom resolvers (third-party key source)

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/setup"

type VaultResolver struct { /* ... */ }

func (r *VaultResolver) Resolve(ctx context.Context) (*setup.TrustSet, error) {
    // Fetch ASCII-armored key from Vault KV, call setup.LoadTrustSet,
    // return the resulting TrustSet (enforces the minimum-strength policy).
}

func main() {
    embedded := setup.NewEmbeddedResolver(/* embedded trust set */)
    resolver := setup.CompositeResolver{
        Resolvers: []setup.KeyResolver{embedded, &VaultResolver{ /* ... */ }},
    }
    // Wire it in at SelfUpdater construction:
    //   setup.NewUpdater(ctx, props, version, force, setup.WithKeyResolver(resolver))
}
```

Any implementation must:

- Return a `*TrustSet` containing only keys that passed the minimum-strength policy.
- Honour the context's deadline and cancellation.
- Cap response bodies at `setup.MaxWKDResponseSize` (64 KiB) or an equivalent bound.
- Not leak private material anywhere: `log.Fatal` if it ever sees a secret key at load time.

### Key rotation

The trust set is a *set*, not a single key. During a rotation window, ship releases signed by both the old and new key; the verifier accepts either. Once all supported versions of the tool include the new key in their trust set, drop the old key from both the embedded `trustkeys` directory and the WKD endpoint.

For emergency rotation (compromise of the primary signing key), the design reserves a second "rotation-authority" key whose private half is stored offline. A release signed by the rotation-authority carries a `rotate-keys.json` manifest; the next update rewrites the embedded trust set from that manifest. This is documented in the spec and deferred to Phase 4.

---

## Testing

Run the Phase 1 tests:

```bash
go test ./pkg/setup/ -run "TestVerifyChecksum|TestVerifyAssetChecksum|TestFindChecksumsAsset"
```

…and the manifest fuzzer:

```bash
go test ./pkg/setup/ -run "^$" -fuzz=FuzzParseChecksumManifest -fuzztime=30s
```

## Related

- [Setup Package Reference](../explanation/components/setup/index.md): `VerifyChecksumFromManifest`, `VerifyChecksumFromManifestReader`, and the updater options.
- [VCS Release Providers](https://forge.go.phpboyscout.uk/reference/providers/): the `ChecksumProvider` optional interface and per-provider behaviour.
- [Custom Release Source](custom-release-source.md): implementing a custom `release.Provider` (and optionally `release.ChecksumProvider`) for a proprietary release backend.
- [Credential Storage Hardening Spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0054-credential-storage-hardening): the related defence-in-depth spec that covers credential storage during update and setup.
- [Remote Update Integrity Spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0056-remote-update-checksum-verification): the full design including Phase 2 (GPG) and Phase 3 (cosign).
