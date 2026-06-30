---
title: Publish public keys via Web Key Directory (WKD)
description: Generate a WKD tree from your public keys and serve it from a dedicated subdomain so the self-update verifier can cross-check against an externally-served copy.
date: 2026-06-09
tags: [how-to, keys, wkd, signing, openpgp]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Publish public keys via WKD

WKD (Web Key Directory) lets a client fetch your public key from a
well-known URL derived from an email address. The verifier uses this
as a second, externally-administered trust anchor: a `gtb update`
fetches the WKD copy and refuses to install a release unless the
embedded key (shipped in the binary) and the WKD copy agree on the
key's fingerprint. A compromise of just the codebase, or just the
WKD host, cannot poison verification.

This how-to walks the end-to-end deploy: generate the WKD tree from
your `.asc` public-key files, host it on Cloudflare Pages, and verify
the round-trip with `gpg --auto-key-locate`.

## What you'll have at the end

- A live WKD endpoint at `https://openpgpkey.<yourdomain>/.well-known/openpgpkey/<yourdomain>/hu/<hash>`
- A Cloudflare Pages project deploying via Direct Upload (no Git, no
  webhook — explicitly disjoint from your code-hosting and signing
  trust anchors)
- A `gpg --locate-keys release@<yourdomain>` lookup that returns the
  same keys you embedded in the binary

!!! info "Why `openpgpkey.<yourdomain>`?"
    The `openpgpkey.` subdomain literal is fixed by
    [draft-koch-openpgp-webkey-service §3.1][wkd-draft] — it is the
    spec's "advanced" URL form. Every WKD client (GnuPG, GTB's
    verifier, Sequoia, etc.) hardcodes this prefix when constructing
    the lookup URL, so we don't choose it. The verifier configuration
    on the consumer side only ever takes the **email**; the hostname
    falls out mechanically. See the
    [Release-binary signing concept][concept] doc's "How the
    verifier finds your WKD endpoint" section for the full derivation
    walk-through.

[wkd-draft]: https://datatracker.ietf.org/doc/draft-koch-openpgp-webkey-service/
[concept]: ../explanation/concepts/release-binary-signing.md#how-the-verifier-finds-your-wkd-endpoint

## One-time setup

Per [phase2-signing-prep doc][prep] § Gate 2, the WKD trust anchor
must live outside both your code-hosting (GitLab/GitHub) and your
KMS (AWS/GCP/Azure). The recipe enforces that with three distinct
account boundaries:

1. **Cloudflare account** on a fresh email (not the address tied to
   your code-hosting or cloud accounts). Set MFA with a different
   authenticator app than your existing accounts — ideally a
   hardware key. Holding two of the three anchors must still leave
   the third uncompromisable.
2. **API token** under `My Profile → API Tokens → Create Token` with
   permission **`Account → Cloudflare Pages → Edit`** only. No Zone,
   no DNS edit, no other scopes. Store it in your password manager.
3. **Pages project** at `Workers & Pages → Create → Pages → Upload
   assets`. Name it after the subdomain (e.g.
   `openpgpkey-yourdomain-uk`). **Do not connect a Git
   repository** — that's what would re-introduce GitHub/GitLab as a
   trust dependency.
4. **Custom domain**: in the project's settings, bind
   `openpgpkey.<yourdomain>`. Cloudflare provisions TLS and gives
   you a CNAME target like `<project>.pages.dev`.
5. **DNS** (Cloudflare or whichever provider hosts your apex
   domain): add `openpgpkey CNAME <project>.pages.dev`. TLS
   provisioning typically takes <2 minutes. Verify with
   `curl -I https://openpgpkey.<yourdomain>/` — expect a 404 with
   valid TLS.

You also need `wrangler` locally:

```sh
# via mise (recommended if you already use it)
mise use -g wrangler@latest

# or via npm
npm install -g wrangler
```

## Generate the WKD tree

`gtb keys wkd` reads one or more armored OpenPGP public keys and
emits the directory structure the WKD spec mandates:

```sh
gtb keys wkd \
    --domain phpboyscout.uk \
    --email release@phpboyscout.uk \
    --submission-address release@phpboyscout.uk \
    --output ./wkd-staging \
    rotation-authority.asc signing-key-v1.asc
```

Result (advanced layout — the default and what Cloudflare Pages
serves under `openpgpkey.<yourdomain>`):

```
wkd-staging/
└── .well-known/
    └── openpgpkey/
        └── phpboyscout.uk/
            ├── policy                  (empty file, RFC §3.1)
            ├── submission-address      (the email you configured)
            └── hu/<z-base-32-hash>     (binary concatenated keys)
```

A successful run logs each `hu/` bucket's hash plus every file
written.

### The submission-address file

`--submission-address` controls the optional WKS `submission-address`
file:

- **omitted / empty** (the default) — the file is **not** written.
- **`--submission-address auto`** — writes the first `--email` value.
- **`--submission-address <email>`** — writes that address verbatim.

Pass `auto` (as opposed to leaving the flag unset) when you want the
file populated from your first `--email` without spelling the address
out twice.

### One-bucket vs multi-bucket

WKD groups by email. If both your keys have the same UID
(`release@phpboyscout.uk`, the standard role-address pattern), they
end up in one `hu/<hash>` file, in lexicographic fingerprint order so
the output bytes are reproducible across deploys.

To publish multiple emails (e.g. `release@` for releases and
`security@` for advisories), pass `--email` repeatedly:

```sh
gtb keys wkd \
    --domain phpboyscout.uk \
    --email release@phpboyscout.uk \
    --email security@phpboyscout.uk \
    --output ./wkd-staging \
    keys/*.asc
```

Each `--email` gets its own `hu/<hash>` bucket; a key with UIDs for
multiple emails appears in every matching bucket.

## Deploy

```sh
export CLOUDFLARE_API_TOKEN=...   # from your password manager
wrangler pages deploy ./wkd-staging \
    --project-name=openpgpkey-phpboyscout-uk \
    --branch=main \
    --commit-dirty=true
```

The `--commit-dirty=true` flag tells Wrangler not to require a Git
working tree (which we don't have for WKD — the source of truth is
the offline-stored signing keys, not a Git repo).

## Verify the round-trip

A WKD-aware GPG client should resolve both your keys:

```sh
gpg --auto-key-locate clear,wkd --locate-keys release@phpboyscout.uk
```

You should see both fingerprints (the rotation-authority and the
signing-key) printed, matching what `gtb keys mint` reported and what
you embedded in `internal/trustkeys/keys/`.

Direct HTTP fetch also works for spot-checking — the hash is logged
by the `gtb keys wkd` run that produced the file. Copy it from there:

```sh
curl -fsSL "https://openpgpkey.phpboyscout.uk/.well-known/openpgpkey/phpboyscout.uk/hu/<the-hash-gtb-logged>" \
  | gpg --list-packets | head -30
```

You should see two `public key packet` blocks (one per embedded
key) and matching `:user ID packet:` entries for
`release@phpboyscout.uk`.

## Per-rotation re-deploy

When you mint a new signing key (or rotation key), re-run `gtb keys
wkd` against the **new public-key file set** plus any keys you want
to keep serving, and `wrangler pages deploy` again. There's no
manual surgery on Cloudflare — the directory is fully reproducible
from the `.asc` files.

The Pages project's deploy history makes rollback trivial: if a bad
key ever ships, redeploy a known-good staging directory in seconds.

## What can go wrong

| Symptom | Cause | Fix |
|---------|-------|-----|
| `curl … /hu/<hash>` returns 404 | DNS still propagating, or you uploaded the wrong layout | Wait 2 min; if it persists, confirm the file is at `.well-known/openpgpkey/<domain>/hu/<hash>`, not `…openpgpkey/hu/…` |
| `gpg --locate-keys` resolves direct but not advanced | Subdomain CNAME missing | Re-check DNS: `dig openpgpkey.<domain> CNAME` |
| `gtb update` says "embedded ↔ WKD fingerprint mismatch" | A different key was uploaded than the one embedded | Redeploy from the same `.asc` set used to populate `internal/trustkeys/keys/` |
| `wrangler` complains about an unknown project | Project not yet created or wrong `--project-name` | Re-check the Cloudflare dashboard project listing |

## Related

- [Spec: gtb keys wkd](../development/specs/2026-06-09-keys-wkd-command.md)
  — design decisions, RFC details, the threat-model rationale.
- [Phase 2 signing prep doc][prep] — the upstream gate decisions for
  domain, email, and host.
- [`openpgpkey`](../explanation/components/openpgpkey.md) — the library
  function `WriteWKDTree` that the CLI wraps (now the standalone
  `gitlab.com/phpboyscout/signing/openpgpkey` module).
- [`gitlab.com/phpboyscout/signing/verify`](https://pkg.go.dev/gitlab.com/phpboyscout/signing/verify)
  — the client-side `WKDResolver` that pairs with this generator (consumed
  by gtb's self-updater).

[prep]: ../development/phase2-signing-prep.md
