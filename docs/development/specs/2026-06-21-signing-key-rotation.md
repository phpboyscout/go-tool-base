---
title: "Emergency signing-key rotation (A5)"
description: "A documented and tooled flow to roll the release signing key and update the trust set that self-update verification relies on, without bricking existing installs. Covers key compromise (break-glass), scheduled rotation (dual-sign overlap), and multi-key trust (old + new valid during the overlap window). Implements Phase 4 of the remote-update-checksum-verification spec, which lists emergency key rotation as a deferred Future Consideration. Centres on the trust-distribution problem: an embedded key baked into already-shipped binaries cannot be edited after the fact, so rotation must propagate through the two trust anchors the verifier already consults — the embedded trust set and the WKD-resolved key — plus a new signed rotation manifest that lets an in-field binary admit a new key on its own next update."
status: DRAFT
date: 2026-06-21
tags:
  - specification
  - signing
  - openpgp
  - key-rotation
  - wkd
  - kms
  - self-update
  - security
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Emergency signing-key rotation (A5)

Authors
:   Matt Cockayne

Date
:   2026-06-21

Status
:   DRAFT

## Summary

GTB self-updates are verified against a `TrustSet` of vetted public keys
resolved per update attempt by a `KeyResolver` (`pkg/setup/signing.go`).
The production default is a `CompositeResolver{Embedded, WKD}` that grants
trust only when both sources agree on the same fingerprint set
(`pkg/setup/signing_composite.go`). The embedded leg reads the
`//go:embed`'d keys in `internal/trustkeys/keys/*.asc`
(`internal/trustkeys/trustkeys.go`); the external leg fetches the release
key from `https://openpgpkey.phpboyscout.uk/...` via `wkdResolver`
(`pkg/setup/signing_wkd.go`). The private signing half lives in AWS KMS
and is wrapped into an armored OpenPGP public key by `gtb keys mint`
(`internal/cmd/keys/mint.go`, `pkg/signing/kms/`).

This trust model defends a single-system compromise but **has no rotation
mechanism**. The
`2026-04-02-remote-update-checksum-verification.md` spec lists "emergency
key rotation" as Phase 4 — explicitly *designed-for but deferred*: it
notes a second embedded "rotation-authority" key and a signed
`rotate-keys.json` manifest "documented but not implemented in Phase 2"
(§Key Rotation, Future Considerations). The structure already exists —
`internal/trustkeys/keys/rotation-authority.asc` is embedded today
alongside `signing-key-v1.asc` — but nothing consumes the rotation
authority, and there is no tooling to roll a key.

The hard part is **trust distribution**, not key generation (`gtb keys
generate` / `gtb keys mint` already mint keys). The embedded key is frozen
into binaries that shipped months ago; we cannot edit a binary in the
field. So a rotation must reach an installed binary through a channel it
already trusts. This spec defines three coordinated paths:

1. **Scheduled rotation (dual-sign overlap)** — both old and new keys are
   valid for a window; releases are signed by both; the new key
   propagates through new binary releases and the WKD endpoint; the old
   key is retired after the support window. No break-glass needed.
2. **Emergency rotation (key compromise)** — the compromised signing key
   must be distrusted *before* the natural release cadence would retire
   it. A `rotate-keys.json` manifest signed by the offline
   **rotation-authority** key, distributed as a release asset, instructs
   an in-field binary to add the replacement key (and optionally
   revoke the compromised one) into its trust set on its next update.
3. **Multi-key trust** — the verifier already accepts *any* key in the
   trust set (`VerifyManifestSignature` iterates entities), so old+new
   simultaneous validity is a property of the resolver's output, not new
   verification logic. This spec makes the *set itself* mutable in a
   controlled, signed way.

This is a `gtb`-author / release-engineering feature. The runtime pieces
(manifest parsing, rotation-authority verification, trust-set merge) land
in `pkg/setup`; the operator tooling (`gtb keys rotate`) lands in
`internal/cmd/keys/` so scaffolded downstream tools do not inherit a
`mytool keys rotate` command — exactly as `gtb keys mint` is gated today.

## Decision-log check

The `2026-04-02-remote-update-checksum-verification.md` spec's status line
records: *"Phases 3–6 (Sigstore/Rekor transparency log, **emergency key
rotation**, SLSA build provenance, checksum pinning / binary transparency)
remain 'Future Considerations' and are each deferred to their own
follow-up specifications."* Its §Key Rotation says emergency rotation is
"deferred to Future Considerations". **No conflicting or superseding
spec exists** (`grep` over `docs/development/specs/` for `rotat`/`A5`
returns only the deferral notes and the two `keys-*-command` specs). This
spec is that follow-up. It does not contradict any IMPLEMENTED decision;
it builds on the Phase-2 primitives (`KeyResolver`, `CompositeResolver`,
`TrustSet`, `internal/trustkeys`, `gtb keys mint/wkd`) as anchors.

## Goals

- Roll the active release signing key **without bricking existing
  installs**: a binary running an old embedded key must still be able to
  verify and apply the update that carries the new key.
- Support **scheduled rotation** (planned, dual-sign overlap, no urgency)
  and **emergency rotation** (compromise, urgency, must distrust the old
  key faster than the release cadence).
- Preserve **multi-key trust during overlap**: old + new key both valid
  for the support window so binaries on either side of the rotation keep
  updating.
- Keep the rotation-authority private key **offline / break-glass** — it
  signs only `rotate-keys.json`, never a release, so it is exercised rarely
  and can live air-gapped.
- Provide operator tooling (`gtb keys rotate`) that produces the signed
  manifest and the WKD tree updates, mirroring the existing
  `gtb keys mint` / `gtb keys wkd` ergonomics.
- Fail **closed**: a malformed, expired, wrongly-signed, or
  downgrade-attempting manifest is ignored; trust is never *narrowed* by
  an attacker and never *widened* except by the rotation authority.

## Non-goals

- Rotating the **rotation-authority** key itself. That key is the root of
  the rotation trust chain; rolling it requires a full binary release that
  embeds a new `rotation-authority.asc` (the same problem the embedded
  signing key has today, minus the urgency, since the authority key never
  touches a release and has a far smaller attack surface). A dedicated
  "authority succession" flow is out of scope; documented as a manual
  release-gated procedure.
- Transparency-log / Rekor-backed revocation (Phase 3) and SLSA
  provenance (Phase 5). Rotation here is signature-anchored, not
  log-anchored.
- Automatic key generation policy / HSM lifecycle automation. KMS key
  creation remains an operator action; this spec consumes a key the
  operator has already minted with `gtb keys mint --backend aws-kms`.
- Changing the wire format of `checksums.txt` / `checksums.txt.sig` or the
  `VerifyManifestSignature` contract.

## Background: where trust comes from today

| Anchor | Source | Mutable after binary ships? | File / type |
|--------|--------|-----------------------------|-------------|
| Embedded key(s) | `//go:embed` in the binary | **No** — frozen at build | `internal/trustkeys/keys/*.asc`, `embeddedResolver` |
| WKD key | Fetched per update from `openpgpkey.<domain>` | Yes — operator controls the static host | `wkdResolver` (`pkg/setup/signing_wkd.go`) |
| Composite | Cross-checks embedded == WKD fingerprints | n/a | `CompositeResolver` (`pkg/setup/signing_composite.go`) |
| Rotation authority | `//go:embed`, embedded today but **unused** | No | `internal/trustkeys/keys/rotation-authority.asc` |

Two facts drive the whole design:

1. **The embedded key cannot be edited in the field.** A binary that
   shipped with `signing-key-v1` embedded will forever have `v1` embedded.
   The only way to change *that binary's* notion of trust is (a) replace
   the binary, or (b) feed it signed data it already trusts that tells it
   to expand its trust set.
2. **`CompositeResolver` requires embedded == WKD agreement by default.**
   If we publish `v2` to WKD but old binaries still embed only `v1`, the
   composite cross-check (`checkAgreement`) sees `{v1}` vs `{v2}` and
   aborts with `ErrKeyResolverMismatch` — *bricking the update for every
   old binary*. This is the central hazard the spec must avoid. The fix is
   that **during overlap, both keys appear in both anchors**: WKD serves
   `{v1, v2}` (`gtb keys wkd` already supports multiple keys per email),
   and old binaries learn `v2` via the rotation manifest so their
   *effective* embedded set becomes `{v1, v2}` too — restoring agreement.

## Design

### Component 1 — `rotate-keys.json` manifest (new, `pkg/setup`)

A small signed JSON document distributed as a release asset
(`rotate-keys.json` + `rotate-keys.json.sig`), parsed by a new
`RotationManifest` type and verified against the **rotation-authority**
key, not the signing key.

```jsonc
{
  "schema": "gtb.rotate-keys/v1",
  "issued_at": "2026-06-21T10:00:00Z",
  "not_after":  "2027-06-21T10:00:00Z",   // manifest validity window
  "reason": "scheduled",                  // "scheduled" | "compromise"
  // Keys to ADD to the trust set (armored public keys, inlined).
  "add": [
    { "fingerprint": "<40-hex>", "armored": "-----BEGIN PGP PUBLIC KEY...-----" }
  ],
  // Keys to DISTRUST. Only honoured when reason == "compromise" and the
  // resulting set is non-empty (never distrust your way to zero keys).
  "revoke": [
    { "fingerprint": "<40-hex>", "since": "2026-06-21T09:00:00Z" }
  ],
  // Monotonic counter; a binary refuses a manifest whose epoch is <= the
  // highest epoch it has already applied (anti-rollback).
  "epoch": 2
}
```

New types and functions in `pkg/setup` (new file `signing_rotation.go`):

```go
// RotationManifest is the parsed, not-yet-verified rotation document.
type RotationManifest struct {
    Schema   string
    IssuedAt time.Time
    NotAfter time.Time
    Reason   RotationReason // RotationScheduled | RotationCompromise
    Add      []ManifestKey
    Revoke   []RevokedKey
    Epoch    uint64
}

// ParseRotationManifest parses and structurally validates the JSON
// (schema string, size bound, well-formed fingerprints, RFC3339 times).
func ParseRotationManifest(raw []byte) (*RotationManifest, error)

// VerifyRotationManifest checks the detached signature over the manifest
// bytes against the rotation-authority TrustSet. Reuses the existing
// CheckArmoredDetachedSignature path with the same MinRSABits floor.
func (a *RotationAuthority) VerifyRotationManifest(raw, sig []byte) (*RotationManifest, error)

// Apply merges a verified manifest into a base TrustSet, returning the new
// effective TrustSet. Enforces: epoch monotonicity, non-empty result,
// add-key strength policy (LoadTrustSet rules), and the revoke-only-on-
// compromise rule. Never widens trust without a valid authority signature.
func (m *RotationManifest) Apply(base *TrustSet, lastEpoch uint64) (*TrustSet, error)
```

Bounds and fail-closed posture mirror the existing signing primitives in
`signing.go`: a new `MaxRotationManifestSize` (e.g. 64 KiB, same order as
`MaxWKDResponseSize`), `ParseRotationManifest` rejecting oversize input,
and added keys passing the same `checkKeyStrength` floor (Ed25519 / RSA ≥
3072) so a manifest cannot smuggle a weak key into the trust set.

### Component 2 — `RotationAuthority` resolver leg (new, `pkg/setup`)

A thin wrapper over the embedded rotation-authority key:

```go
// RotationAuthority holds the trust set used ONLY to verify
// rotate-keys.json. It is distinct from the release-signing TrustSet:
// the authority key signs manifests, never releases.
type RotationAuthority struct{ ts *TrustSet }

func NewRotationAuthority(armoredAuthorityKeys ...[]byte) (*RotationAuthority, error)
```

Wired from `internal/trustkeys` exactly as the signing keys are: a new
`internal/trustkeys.RotationAuthorityKeys()` returns the contents of
`keys/rotation-authority.asc` (today embedded but unread). Tool authors
supply it through a new option (see Component 4).

### Component 3 — `RotatingResolver` (new, wraps the existing resolver)

The piece that closes the embedded-vs-WKD propagation gap. It decorates
the configured `KeyResolver` (typically the `CompositeResolver`) and,
*before* delegating, checks a locally-cached applied manifest:

```go
// RotatingResolver augments an inner KeyResolver's trust set with keys
// admitted by previously-applied, authority-signed rotation manifests.
// The manifest itself is fetched and verified by the update flow (it is a
// release asset like checksums.txt.sig); this resolver applies the
// already-verified, persisted result so Resolve stays I/O-light and the
// effective set = inner.Resolve() ∪ added − revoked.
type RotatingResolver struct {
    Inner     KeyResolver
    Authority *RotationAuthority
    Store     RotationStore // persists applied epoch + admitted/revoked keys
}
```

Resolution order on update:

1. The update flow downloads `rotate-keys.json(.sig)` if present (a new
   optional release asset, alongside the existing
   `checksums.txt`/`.sig`).
2. `Authority.VerifyRotationManifest` validates the signature and
   `Apply` enforces epoch/strength/non-empty rules.
3. The result is persisted via `RotationStore` (under the tool's config
   dir, see `pkg/setup` config-dir helpers) so it survives across runs and
   is applied even when offline.
4. `RotatingResolver.Resolve` returns `inner.Resolve(ctx)` merged with the
   persisted admitted set, minus revoked fingerprints.

Crucially, this makes the **old binary's effective embedded set** become
`{v1, v2}` after it applies the manifest — which restores
`CompositeResolver` agreement with a WKD endpoint now serving `{v1, v2}`,
so the cross-check passes and the update is **not** bricked.

### Component 4 — wiring & options (`pkg/setup`)

New `SelfUpdater` options, following the existing `WithKeyResolver` /
`WithEmbeddedKeys` pattern (`pkg/setup/update.go`):

```go
func WithRotationAuthority(armoredKeys ...[]byte) Option // enables manifest verification
func WithRotationStore(s RotationStore) Option           // override persistence (tests / custom dir)
```

When a rotation authority is configured, `NewUpdater` wraps the built
`KeyResolver` in a `RotatingResolver`. When it is not, behaviour is
byte-for-byte the current behaviour (rotation is purely additive and
opt-in). New config keys under `update:` mirror the existing ones:

```yaml
update:
  rotation:
    enabled: true            # honour rotate-keys.json (default: true when authority embedded)
    require_signature: true  # an unsigned/badly-signed manifest is ignored (always true; here for visibility)
```

### Component 5 — `gtb keys rotate` (new, `internal/cmd/keys/`)

Operator command that produces the manifest and signs it with the
rotation-authority key. It does **not** generate keys — the new signing
key is minted first with `gtb keys mint --backend aws-kms` (compromise
case) or `gtb keys generate` (tutorial). `rotate` consumes the new
armored public key plus the offline authority private key:

```
gtb keys rotate \
    --add signing-key-v2.asc \
    --reason scheduled \
    --epoch 2 \
    --not-after 2027-06-21T10:00:00Z \
    --authority-key rotation-authority.priv.asc \   # offline / break-glass
    --output rotate-keys.json

# Emergency: also distrust the compromised key
gtb keys rotate \
    --add signing-key-v2.asc \
    --revoke <v1-fingerprint> \
    --reason compromise \
    --epoch 3 \
    --authority-key rotation-authority.priv.asc \
    --output rotate-keys.json
```

It writes `rotate-keys.json` and `rotate-keys.json.sig`. The authority
private key is read locally and used for a single detached signature; like
`gtb keys generate`'s private outputs, the command never transmits it and
documents moving it back to offline storage. (Open question O3 below:
whether to also support signing the manifest through a `pkg/signing`
backend so the authority key can itself live in a *separate* KMS.)

The existing `gtb keys wkd` already accepts multiple keys per email
(`internal/cmd/keys/wkd.go`), so re-publishing WKD with `{v1, v2}` during
overlap needs no new command — the rotation runbook just calls it with
both `.asc` files.

### The two flows end-to-end

**Scheduled rotation (no urgency, `reason: scheduled`):**

1. `gtb keys mint --backend aws-kms --key-id <v2-kms-arn> -o signing-key-v2.asc`.
2. Commit `signing-key-v2.asc` into `internal/trustkeys/keys/` and update
   `expectedFingerprints` in `trustkeys_test.go` (the test is the
   intentional human gate — see its own doc comment). Ship a binary that
   embeds `{v1, v2}`.
3. Re-publish WKD with both keys: `gtb keys wkd --email release@... v1.asc v2.asc`.
4. Sign releases with **both** keys for the support window (GoReleaser
   multiple `signs`).
5. Issue `rotate-keys.json` (`add: v2`, `reason: scheduled`) so even
   binaries that have *not* upgraded learn `v2` and stay in agreement with
   the now-`{v1,v2}` WKD endpoint.
6. After the window: drop `v1` from embedded keys, from WKD, and from the
   signing workflow; issue a manifest with a higher epoch that no longer
   carries `v1` and (optionally) revokes it.

**Emergency rotation (`reason: compromise`):**

1. Mint `signing-key-v2` from a **fresh** KMS key.
2. `gtb keys rotate --add v2.asc --revoke <v1-fp> --reason compromise
   --epoch N --authority-key <offline>` → signed manifest.
3. Publish `rotate-keys.json(.sig)` as a release asset and re-publish WKD
   with `{v2}` (and, briefly, `v1` if any unrevoked binary still needs the
   overlap — see O2). Push an out-of-band advisory.
4. In-field binaries, on next update, verify the manifest against the
   embedded **rotation-authority** key, add `v2`, distrust `v1`, and apply
   the (v2-signed) release. The compromised key is rejected even though it
   is still embedded.
5. Ship a new binary embedding `{v2}` only; retire the authority manifest
   once adoption is sufficient.

## Trust & threat model

| Scenario | Outcome |
|----------|---------|
| Attacker steals the **signing** key (v1) | They can sign releases, but cannot sign a `rotate-keys.json` (needs the offline authority key). Operator issues a `compromise` manifest revoking v1; in-field binaries distrust v1 on next update. Attacker's v1-signed releases stop validating. |
| Attacker steals the **authority** key | Worst case. They can issue manifests that *add* keys, but `Apply` forbids reducing the set to empty and the WKD/embedded cross-check still bounds what a *signing* key can do. Mitigation: authority key is offline / break-glass, never in CI, smallest possible exposure. Recovery is a binary release embedding a new authority key (non-goal to automate). |
| Attacker replays an **old** manifest (rollback) | Rejected: `Apply` refuses any manifest whose `epoch <= lastEpoch` persisted in the `RotationStore`. |
| Attacker strips `rotate-keys.json` from a release | Update proceeds with the un-augmented (last persisted) trust set — fail-safe: a missing manifest never *widens* trust and never reverts a previously-applied revoke. A stripped manifest cannot *un-revoke* a compromised key. |
| Attacker forges an unsigned/badly-signed manifest | Ignored by `VerifyRotationManifest`; the trust set is unchanged. Trust is only ever widened by a valid authority signature. |
| Manifest tries to smuggle a weak key | `Apply` runs added keys through `checkKeyStrength` (Ed25519 / RSA ≥ 3072); weak keys are rejected, the whole manifest fails closed. |
| Old binary, WKD now serves `{v1,v2}`, embedded only `{v1}` | Without rotation: `ErrKeyResolverMismatch`, update **bricked**. With this spec: binary applies the `add: v2` manifest, effective embedded set becomes `{v1,v2}`, agreement restored, update proceeds. This is the core problem the design solves. |

## API & file surface

| Path | Change |
|------|--------|
| `pkg/setup/signing_rotation.go` | **New.** `RotationManifest`, `ParseRotationManifest`, `RotationAuthority`, `VerifyRotationManifest`, `Apply`, `RotatingResolver`, `RotationStore`, sentinels (`ErrRotationManifestInvalid`, `ErrRotationRollback`, `ErrRotationWouldEmptyTrust`). |
| `pkg/setup/update.go` | `WithRotationAuthority`, `WithRotationStore` options; `NewUpdater` wraps resolver in `RotatingResolver`; download `rotate-keys.json(.sig)` if present. |
| `pkg/setup/signing.go` | Add `MaxRotationManifestSize`; reuse `checkKeyStrength`, `CheckArmoredDetachedSignature`. No change to `VerifyManifestSignature` contract. |
| `internal/trustkeys/trustkeys.go` | Add `RotationAuthorityKeys()` returning `keys/rotation-authority.asc` (already embedded; currently unread). |
| `internal/cmd/keys/rotate.go` | **New.** `gtb keys rotate` (mints the signed manifest from `--add`/`--revoke` + offline authority key). |
| `internal/cmd/keys/keys.go` | Register the `rotate` subcommand. |
| `docs/how-to/key-rotation.md` | **New.** Operator runbook for both flows. |
| `docs/components/setup.md` (or signing component doc) | Document the rotation resolver + manifest. |
| `features/cli/key-rotation.feature` | **New.** Gherkin E2E: dual-sign overlap + compromise revoke + rollback rejection. |

## Testing strategy (TDD, ≥90% for new `pkg/` code)

- **Unit (`pkg/setup`)**: manifest parse (valid/oversize/malformed/bad
  times); signature verify against authority key (valid / wrong key /
  tampered / unsigned); `Apply` (epoch monotonic / rollback rejected /
  revoke-to-empty rejected / weak-key rejected / scheduled cannot revoke);
  `RotatingResolver.Resolve` returns `inner ∪ add − revoke`; the
  **brick-avoidance** case — embedded `{v1}` + WKD `{v1,v2}` + applied
  `add:v2` manifest → composite agreement restored, update succeeds.
- **Round-trip**: `gtb keys rotate` output parses and verifies under
  `RotationAuthority` built from the matching public key.
- **`internal/cmd/keys`**: `rotate` flag validation, no-clobber output,
  refusal to revoke without `--reason compromise`, refusal to emit a
  manifest that would empty the trust set.
- **E2E (Godog)**: per `features/`, the dual-sign overlap and the
  compromise-revoke scenarios, gated by `INT_TEST_E2E_CLI=1`.
- **Persistence**: `RotationStore` survives across `Resolve` calls and
  applies an admitted key when offline (no WKD reachable).

## Open questions (resolve before implementation)

- **O1 — Manifest distribution channel.** Ship `rotate-keys.json` as a
  *release asset* (simple, same-origin as the binary, already-trusted
  fetch path) or also via *WKD-adjacent static host* (uncorrelated origin,
  stronger against VCS compromise but more moving parts)? Default proposal:
  release asset for v1, since the authority signature — not the origin — is
  what's trusted. Revisit alongside Phase 3 (Rekor).
- **O2 — Overlap requirement in the compromise flow.** When revoking v1,
  must WKD briefly keep serving `{v1, v2}` so binaries that have not yet
  applied the manifest can still cross-check, or do we accept that
  un-updated binaries are *meant* to fail closed during a compromise?
  Trade-off: availability vs. distrust speed. Proposal: serve `{v2}` only
  and accept that a compromise *should* fail closed for un-updated
  binaries; document loudly.
- **O3 — Authority key custody.** Offline armored private key read by
  `gtb keys rotate` (simple, matches `gtb keys generate` outputs) vs.
  routing the manifest signature through a `pkg/signing` backend so the
  authority key can itself live in a *second*, separate KMS account
  (stronger custody, but couples rotation tooling to KMS availability).
  Proposal: support **both** — `--authority-key <file>` and
  `--authority-backend <name> --authority-key-id <id>` — reusing the
  `pkg/signing` registry that `gtb keys mint` already drives.
- **O4 — Epoch source of truth.** Operator-supplied `--epoch` (explicit,
  auditable, but fat-finger risk) vs. derived from a monotonic counter the
  tooling tracks. Proposal: operator-supplied, with `rotate` refusing an
  epoch `<=` the one in the previous manifest it is handed via an optional
  `--previous rotate-keys.json`.
- **O5 — Authority-key succession.** Confirm that rolling the
  rotation-authority key stays a non-goal (release-gated manual procedure),
  or whether a minimal "authority succession manifest signed by the old
  authority" is in scope for v1. Proposal: out of scope; document the
  manual procedure.
- **O6 — Interaction with `require_external_crosscheck=true`.** In
  locked-down deployments that already abort on WKD failure, does the
  rotation manifest change the cross-check semantics (it augments the
  embedded leg, so agreement is computed against the augmented set)?
  Confirm the augmented-set comparison is the intended behaviour and add an
  explicit test.

## Resolutions (open questions confirmed with user 2026-06-21)

- **O1 — Manifest distribution** — RESOLVED: **release asset for v1.** Same-origin
  as the binary on the already-trusted fetch path; the authority signature (not
  the origin) is what's trusted. Revisit an uncorrelated second origin alongside
  Phase 3 (Rekor).
- **O2 — Compromise overlap** — RESOLVED: **serve `{v2}` only; fail closed.**
  Un-updated binaries are meant to fail closed during a compromise — prioritise
  distrust speed over availability; document loudly. (Scheduled, non-compromise
  rotations still use a normal dual-sign overlap.)
- **O3 — Authority-key custody** — RESOLVED: **support both** `--authority-key
  <file>` and `--authority-backend <name> --authority-key-id <id>`, reusing the
  `pkg/signing` registry that `gtb keys mint` drives, so the authority key can
  optionally live in a second, separate KMS account.
- **O4 — Epoch source** — RESOLVED: **operator-supplied `--epoch`**, with
  `rotate` refusing an epoch `<=` the one in a supplied `--previous
  rotate-keys.json` (replay guard).
- **O5 — Authority-key succession** — RESOLVED: **out of scope for v1**; document
  the release-gated manual procedure. Rolling the authority key re-introduces the
  frozen-embedded-key problem and is a rare break-glass event.
- **O6 — `require_external_crosscheck=true` semantics** — RESOLVED: **yes**,
  embedded/WKD agreement is computed against the **manifest-augmented embedded
  set** (`{embedded ∪ manifest-added}`); add an explicit test for this.

## Rollout

Additive and opt-in: with no `WithRotationAuthority` configured, behaviour
is unchanged. GTB's own binary already embeds `rotation-authority.asc`, so
enabling is a one-line wiring change in `internal/cmd/root` plus flipping
`update.rotation.enabled`. The first shipped manifest should be a no-op /
low-epoch `scheduled` manifest to exercise the path before a real
rotation is ever needed (a fire-drill), per the project's preference for
verifying break-glass paths before depending on them.
