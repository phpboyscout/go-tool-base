---
title: "`gtb keys wkd` — generate the Web Key Directory tree from one or more public keys"
description: "Add `gtb keys wkd` which, given one or more armored OpenPGP public keys and a domain, emits the `.well-known/openpgpkey/<domain>/hu/<z-base-32-hash>` directory structure expected by draft-koch-openpgp-webkey-service. Pure Go; no shell-out to `gpg-wks-client`. Pairs `pkg/setup`'s existing `WKDResolver` (client side, already on MR !9) with a server-side generator."
status: IMPLEMENTED
date: 2026-06-09
tags:
  - specification
  - keys
  - wkd
  - openpgp
  - signing
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# `gtb keys wkd` — generate the Web Key Directory tree

Authors
:   Matt Cockayne

Date
:   2026-06-09

Status
:   IMPLEMENTED (shipped in MR !34, 2026-06-09; live since v0.12.0)

## Summary

Add a third subcommand to the `gtb keys` group (alongside `mint` and `generate`) that takes one or more armored OpenPGP public-key files plus a domain and emits a Web Key Directory (WKD) tree ready to upload to a static host. Implemented entirely in Go — no `gpg-wks-client` dependency.

```sh
gtb keys wkd \
    --domain phpboyscout.uk \
    --output ./wkd-staging \
    rotation-authority.asc signing-key-v1.asc
```

Result:

```
wkd-staging/
└── .well-known/
    └── openpgpkey/
        └── phpboyscout.uk/
            ├── policy                          # empty file (RFC §3.1)
            └── hu/
                └── <z-base-32-hash-of-"release">    # binary OpenPGP packets
```

Then `wrangler pages deploy wkd-staging --project-name=openpgpkey-phpboyscout-uk` ships it.

## Motivation

[Phase2 signing prep doc § Gate 2](../phase2-signing-prep.md) approves Cloudflare Pages Direct Upload as the WKD host. The prep doc's example invocation uses `gpg-wks-client` — a CLI tool that introduces an external dependency `gtb` has otherwise been careful to avoid (we already write our own keys via `gtb keys generate` instead of shelling out to `gpg`).

A native Go implementation closes the loop:

- Pure-Go path consistent with the rest of `gtb keys` — no `gpg` install required to generate the WKD tree.
- The exact same `pkg/openpgpkey` parser that `pkg/setup/signing_wkd.go` (the `WKDResolver` on MR !9) uses to *fetch* WKD bytes is now also used to *emit* them. Server- and client-side share one understanding of OpenPGP packet framing.
- Per-rotation deploy is one command, scriptable.

## Design decisions

### D1 — Subcommand placement

`internal/cmd/keys/wkd.go`, alongside `mint.go` and `generate.go`. Stays a gtb-only command (not scaffoldable to downstream tools — same boundary as the other two; downstream tools rarely need a WKD endpoint and can always invoke `gpg-wks-client` if they do).

### D2 — Library-first split

Per the project's library-first principle, the WKD-tree generator lives in `pkg/openpgpkey/wkd.go` as a public, Beta-tier API. Shape:

```go
// Entry is one published email + its keys. Multiple keys per email
// (typical: rotation-authority + signing-key) are concatenated under
// the same hu/<hash> file.
type Entry struct {
    Email string
    Keys  [][]byte    // armored *or* binary; auto-detected
}

// WriteWKDTree writes the .well-known/openpgpkey/<domain>/{policy,hu/<hash>}
// layout under outDir. Returns the list of paths written for the caller
// to log or assert against in tests.
func WriteWKDTree(outDir, domain string, entries ...Entry) ([]string, error)
```

`internal/cmd/keys/wkd.go` is a thin CLI wrapper: flag parsing → read .asc files → group by email → call `WriteWKDTree`.

### D3 — Method: advanced (subdomain), not direct

WKD has two URL methods:

- **Direct**: `https://<domain>/.well-known/openpgpkey/hu/<hash>` — served from the same domain as the email.
- **Advanced**: `https://openpgpkey.<domain>/.well-known/openpgpkey/<domain>/hu/<hash>` — served from a dedicated subdomain. Required when the main domain serves anything else.

Phase 2 prep doc chose advanced (`openpgpkey.phpboyscout.uk` is a dedicated subdomain). We default to advanced; expose `--method direct` for completeness and to make the verifier-side `WKDResolver` testable against the simpler layout.

Difference in output is one directory level — advanced nests under `<domain>/`, direct does not.

### D4 — Hashing

Per [draft-koch-openpgp-webkey-service §3.1](https://datatracker.ietf.org/doc/html/draft-koch-openpgp-webkey-service-15#section-3.1):

1. Lowercase the local part (text before `@`) using **plain ASCII lowercasing** — not Unicode case-folding. (Both keys have ASCII-only local parts here.)
2. Compute `SHA-1` over the lowercased bytes.
3. Encode the 20-byte hash with **z-base-32** (Zooko's base-32 alphabet, [Phil Zimmermann variant](https://philzimmermann.com/docs/human-oriented-base-32-encoding.txt)). Produces a 32-character string.

SHA-1 here is **not** a security primitive — it's the IETF-mandated bucket identifier. The crypto verification still happens via the signing algorithm of the key itself (ed25519 / RSA-4096). The existing `.golangci.yaml` already exempts `pkg/setup/signing_wkd.go` from `gosec G401|G505` for the same reason; we extend that exemption to `pkg/openpgpkey/wkd.go`.

z-base-32 is not in the Go stdlib. Options:

- **(a)** Add a dep on `github.com/tv42/zbase32` (~80 LoC, MIT-licensed, no transitive deps). Used by Tailscale, gpg-tools' Go port, and other OpenPGP-adjacent Go code.
- **(b)** Write the encoder inline. The alphabet is fixed (`ybndrfg8ejkmcpqxot1uwisza345h769`), encoding is ~50 LoC. No external trust to extend.

D4 picks **(b) write inline**. Rationale: the encoder is small enough that an in-tree implementation is less code overall than a vendor dep, easier to audit for the security-tooling audience, and avoids inheriting whatever supply-chain surface tv42/zbase32 collects in the future. The unit tests use the IETF spec's published test vectors so any drift is caught.

### D5 — Input format and multi-email grouping

Accept .asc files (armored OpenPGP) as positional arguments. Internally, dearmor before writing to `hu/` (WKD serves binary packets per §3.1; an armored file at the WKD URL is non-conformant and most clients reject it).

`--email <addr>` is **repeatable**. The CLI takes a list of emails to publish; for each, it walks every parsed entity, matches UIDs against the email, and groups matched keys into that email's `hu/<hash>` bucket. A single key can appear in multiple buckets if it carries multiple matching UIDs (standard WKD behaviour).

Library shape (resolved from Q1 — multi-email enabled in v0.1):

```go
// Entry binds one email to the keys to publish under it. WriteWKDTree
// groups duplicate Emails across entries by concatenating their Keys
// into the same hu/<hash> file.
type Entry struct {
    Email string
    Keys  [][]byte    // armored *or* binary; auto-detected
}

type Method string

const (
    MethodAdvanced Method = "advanced" // default
    MethodDirect   Method = "direct"
)

type Options struct {
    Method            Method // zero-value = MethodAdvanced
    SubmissionAddress string // optional; if non-empty, written to the domain root
}

// WriteWKDTree writes the .well-known/openpgpkey/...{policy,submission-address?,hu/<hash>}
// layout under outDir. Returns the list of paths written for callers to
// log or assert against.
func WriteWKDTree(outDir, domain string, opts Options, entries ...Entry) ([]string, error)
```

Edge cases:

- Two `.asc` files with the same email → keys merge into one hu/ file (per D9 ordering).
- A `.asc` whose UIDs don't match any requested email → error (caller asked to publish but nothing matched).
- `--email` requested but no input file contains a UID matching it → error.

### D6 — Domain metadata files: policy and submission-address (Q2 resolved: emit both)

Per RFC §3.1 the directory `policy` file is "always provided but may be empty"; `submission-address` is optional but useful for clients that want to discover the WKS submission endpoint.

Emit both:

- **`policy`** — zero-byte file by default. A future `--policy-key value` flag could populate it.
- **`submission-address`** — written when `Options.SubmissionAddress` is non-empty (CLI flag `--submission-address <email>`, defaulting to the first `--email` value). The file's content is the email address with no trailing newline.

Both files live in the same directory as `hu/` — i.e. `.well-known/openpgpkey/<domain>/` for the advanced method, `.well-known/openpgpkey/` for direct.

### D7 — Reproducibility + idempotence (Q3 resolved: lexicographic by fingerprint)

- File contents are byte-deterministic given the same inputs (no timestamps in WKD payloads — the OpenPGP packets are already canonical).
- Within a single `hu/<hash>` file, multiple keys are concatenated in **lexicographic order of their primary-key fingerprint** (uppercase hex). Argument order doesn't affect the output bytes, so re-running with reordered `.asc` files produces a bit-identical tree.
- Across multiple emails, `hu/` files are written in lexicographic order of their hash (deterministic; the OS dictates the actual disk ordering, but emission order is stable).
- `WriteWKDTree` overwrites any existing files under `outDir/.well-known/openpgpkey/<domain>/`. It refuses to write outside that subtree (path-traversal guard against malicious domain values, though `gtb keys wkd` only takes `--domain` from a flag).
- A reproducibility smoke test (`sha256sum -c`) lives in `pkg/openpgpkey/wkd_test.go`: emit twice with reordered inputs, compare every file's sha256.

### D8 — Validation

Before generating the tree:

- Each `.asc` must parse via `openpgp.ReadArmoredKeyRing`.
- Each entity must carry at least one UID with a parseable email matching the `--email` filter (if specified) or otherwise providing the email used as the hu/<hash> bucket.
- `--domain` must be a valid hostname-shaped string (letters/digits/hyphens, dot-separated). Refuses paths or schemes.
- Verbose mode (`-v` / `--debug`) logs each fingerprint as it's grouped into its hu/ bucket — same format `gtb keys mint` already uses.

## Verification plan

1. **Unit**: `pkg/openpgpkey/wkd_test.go` — z-base-32 encoder against RFC test vectors; `WriteWKDTree` round-trip with the two real keys produced earlier today (FP `2B26…19C2` + `6E20…AA35`) into a `t.TempDir()`; assert paths, content equality, directory exclusivity.
2. **Cross-check with `gpg-wks-client`**: an `INT_TEST_WKD=1` integration test (gated like the existing integration suites) runs `gpg-wks-client --print-wkd-hash release@phpboyscout.uk` and asserts our z-base-32 output matches. Catches any bug in the encoder that the unit tests miss.
3. **End-to-end (post-deploy)**: `gpg --auto-key-locate clear,wkd --locate-keys release@phpboyscout.uk` against the actual `openpgpkey.phpboyscout.uk` endpoint resolves both keys with matching fingerprints. Documented as a verification step in the how-to.

## Out of scope

- Auto-deploying to Cloudflare via Wrangler. The deploy command lives in `docs/how-to/publish-wkd.md` (to be written) — orchestrating wrangler from inside gtb adds a dep we don't need.
- The Web Key Service (WKS) submission protocol (RFC §3.2). We don't accept third-party key submissions; the only emails we publish are role addresses we mint ourselves.
- A `gtb keys verify` subcommand that fetches via WKD and cross-checks. `pkg/setup/signing_wkd.WKDResolver` already does this at update time; an explicit operator-driven verifier is a separate (likely useful) addition tracked elsewhere.

## Resolutions

Open questions were reviewed with the user on 2026-06-09 before implementation:

1. **Q1 — Multi-email enabled in v0.1.** `--email` is repeatable; each requested address gets its own `hu/<hash>` bucket; a single key with multiple matching UIDs ends up in each matching bucket. Bigger initial surface than the single-email v0.1 the spec originally proposed, but rounds out the RFC coverage in one go. (See D5.)
2. **Q2 — Emit both `policy` and `submission-address`.** `policy` is zero-byte by default (RFC-required). `submission-address` is configurable via `--submission-address <email>` and defaults to the first `--email` value. Future-proofs the endpoint for any WKS-aware client without needing a re-deploy when we add WKS support. (See D6.)
3. **Q3 — Lexicographic by fingerprint.** Multi-key `hu/<hash>` files concatenate keys in uppercase-hex fingerprint order, making re-runs bit-identical regardless of argument order. (See D7.)
4. **Q4 — Keep `--method`.** Default `advanced` (production); `--method direct` available for the root-domain layout. ~10 lines of conditional logic but full RFC coverage in `pkg/openpgpkey`, and gives the library's own test suite a way to exercise both shapes. (See D3.)
5. **Q5 — Stay armored-only in `internal/trustkeys/keys/`** (deferred default). `gtb keys wkd` dearmors `.asc` files on the fly when emitting `hu/<hash>`. No binary copy embedded.

## Related

- [Phase 2 signing prep doc](../phase2-signing-prep.md) — Gate 2 selects Cloudflare Pages Direct Upload as the WKD host.
- [Keys-mint spec](2026-06-08-keys-mint-command.md) — `gtb keys mint` and `gtb keys generate`, the prior two subcommands in the `keys` group.
- [draft-koch-openpgp-webkey-service-15](https://datatracker.ietf.org/doc/html/draft-koch-openpgp-webkey-service-15) — the WKD spec itself.
- [`pkg/setup/signing_wkd.go`](../../../pkg/setup/signing_wkd.go) — the client-side WKD resolver this spec pairs with.
