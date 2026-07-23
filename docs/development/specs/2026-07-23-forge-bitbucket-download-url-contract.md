---
title: "forge-bitbucket: resolve the contradictory downloads self-link contract (auth host-pin vs URL-shape parsing)"
description: "forge-bitbucket builds assets from the API's links.self.href, but its two consumers assume incompatible URL shapes: setBasicAuthIfHostMatches only attaches credentials to api.bitbucket.org URLs, while parseDownloadURL only accepts browser-shaped bitbucket.org/{workspace}/{repo}/downloads/{file} paths. Whichever shape the live API returns, one half breaks — either private downloads 401 or checksum/signature resolution hard-errors and aborts every Bitbucket update under require_checksum. Carry workspace/repo on the synthetic release instead of re-deriving them from asset URLs."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - forge
  - bitbucket
  - self-update
  - reliability
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# forge-bitbucket: resolve the contradictory downloads self-link contract (auth host-pin vs URL-shape parsing)

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH finding),
    [forge module extraction](2026-07-19-forge-module-extraction.md) (Bitbucket downloads-as-releases model),
    [remote update checksum verification](2026-04-02-remote-update-checksum-verification.md) (the require_checksum policy this breaks under)

---

## 1. Problem

Bitbucket has no releases API; the provider synthesises releases from the repository
Downloads list. Assets are built from the API response's `dl.Links.Self.Href`
(`go/forge-bitbucket/release.go:451-454`). Two consumers of that same URL hold
mutually incompatible assumptions about its shape:

- **`setBasicAuthIfHostMatches`** (release.go:149-159) attaches basic-auth credentials
  only when the URL host matches the pinned `apiBase` —
  `https://api.bitbucket.org/2.0` (release.go:128). It assumes an **API-host** URL.
- **`parseDownloadURL`** (release.go:321-333) requires the path shape
  `/{workspace}/{repo}/downloads/{file}` (it checks `parts[2] == "downloads"`) — a
  **browser-host** `bitbucket.org/…` URL. An API-shaped path
  (`/2.0/repositories/{workspace}/{repo}/downloads/{file}`) fails this check with a
  hard error, not `forge.ErrNotSupported`.

`parseDownloadURL` exists only because the synthetic release drops provenance:
`resolveDownloadURL` (release.go:252-265) re-derives `(workspace, repo)` from the
first asset's URL to look up `checksums.txt`/`checksums.txt.sig` for
`DownloadChecksumManifest` (release.go:225-232) and `DownloadSignature`
(release.go:238-245) — the code comment at release.go:253-256 admits the re-derivation.

The unit tests stub **browser-shaped** hrefs throughout, so the contradiction is never
exercised against the real API, whose v2 `links.self.href` values are
`api.bitbucket.org/2.0/…`-shaped. Whichever shape the live API returns, exactly one
half breaks:

- **API-shaped (the documented live behaviour):** basic auth attaches correctly, but
  `parseDownloadURL` errors → `DownloadChecksumManifest`/`DownloadSignature` return a
  hard error instead of `ErrNotSupported` → under `require_checksum` /
  `require_signature`, `verifyAssetChecksum` aborts (`pkg/setup/update.go:687-689`) —
  **every Bitbucket self-update fails**.
- **Browser-shaped:** checksum lookup parses, but the host never matches `apiBase`, so
  basic auth is never attached — **private-repo downloads 401**.

## 2. Proposed change

Remove the URL re-derivation entirely; make URL parsing a tolerant fallback only.

1. **Carry provenance on the synthetic structs.** `matchAssets`/`GetLatestRelease`
   already know `(workspace, repo)` — they were the function arguments. Store them as
   fields on the synthetic `bitbucketRelease` (and/or `bitbucketAsset`) when the
   release is built, and have `resolveDownloadURL` read them directly. This deletes
   the fragile asset-URL round-trip and is correct for **both** URL shapes.
2. **Make `parseDownloadURL` shape-tolerant** for any remaining or future callers:
   accept both `/{workspace}/{repo}/downloads/{file}` (browser) and
   `/2.0/repositories/{workspace}/{repo}/downloads/{file}` (API), keyed on locating
   the `downloads` path segment rather than a fixed index. If step 1 leaves it with no
   callers, delete it instead.
3. **Preserve the `ErrNotSupported` contract.** With provenance carried on the struct,
   the "no assets / no matching file" paths keep returning `forge.ErrNotSupported`
   (release.go:258-259, 278) and no URL-parse failure can masquerade as a hard
   verification error.
4. **Audit `DownloadReleaseAsset`** (release.go:336-347): it fetches the asset's
   `BrowserDownloadURL` with `setBasicAuthIfHostMatches` — with API-shaped hrefs this
   is consistent; add a test asserting auth is attached for API-shaped asset URLs and
   not for foreign hosts (the existing host-pin tests cover the latter).
5. **Add a live-API integration test** (env-gated per house convention, e.g.
   `INT_TEST_FORGE_BITBUCKET=1`): fetch the downloads list of a real (public) test
   repository and assert the actual `links.self.href` shape matches what the unit
   fixtures encode — pinning the fixtures to reality so the two can never silently
   diverge again. Update the unit fixtures to the live shape as part of this spec.

**Alternative considered:** teaching `setBasicAuthIfHostMatches` to also trust
`bitbucket.org` (widening the pin) so browser-shaped URLs authenticate. Rejected as
the primary fix — it widens a deliberately narrow credential pin and still leaves the
provenance round-trip in place; carrying `(workspace, repo)` is strictly simpler and
shape-independent.

## 3. Scope & release plan

- **`go/forge-bitbucket` only** — release.go (+ struct fields, tests, fixtures,
  integration test). The `go/forge` core contract is unchanged; no other provider is
  affected.
- Ships as `fix(release): …` — a **patch** release via the releaser-pleaser Release
  MR. Forge providers are kept in lockstep at minor versions; a single-provider patch
  does not break parity, so no simultaneous republish of the other adapters is
  required.
- **go-tool-base** consumes via a go.mod bump of forge-bitbucket; no GTB code change.
  Downstream tools distributing via Bitbucket Downloads with `require_checksum`
  enabled are currently hard-broken, so the bump should follow promptly.

## 4. Acceptance criteria

- Unit test with **API-shaped** fixtures (`https://api.bitbucket.org/2.0/repositories/{ws}/{repo}/downloads/{file}`):
  `DownloadChecksumManifest` and `DownloadSignature` resolve and fetch successfully,
  with basic auth attached to every request (asserted at the fake server).
- Unit test with **browser-shaped** fixtures: same behaviour for resolution; auth
  attachment matches the host-pin decision made for that shape (documented in the
  test) — no 401-by-construction path remains for the live shape.
- Regression test: a release whose downloads list lacks `checksums.txt` returns
  `forge.ErrNotSupported` (not a hard error) from `DownloadChecksumManifest`, for both
  URL shapes — so GTB's optional-verification fallback keeps working.
- `resolveDownloadURL` no longer derives `(workspace, repo)` from an asset URL; the
  synthetic release/asset structs carry them (asserted directly or via a
  shape-mismatch test that would previously have errored).
- `parseDownloadURL` (if retained) has table tests covering both shapes plus malformed
  inputs; if deleted, no references remain.
- Env-gated integration test against the live Bitbucket API validates the
  `links.self.href` shape and the end-to-end checksum-manifest resolution on a real
  repository; documented in the module's integration-test inventory.
- Module CI green; coverage on touched code ≥ 90%.

## 5. Open questions

1. Which real Bitbucket repository should back the integration test — a dedicated
   public fixture repo under the project's workspace (preferred, credential-free), or
   an existing tool's release repo? A private-repo auth-path integration test would
   additionally need app-password secrets in CI.
2. Should the fix also normalise `GetBrowserDownloadURL` to always expose the
   browser-shaped URL (cosmetic, what a human would open) while downloads use the
   API URL internally, or keep exposing the API self link verbatim? Verbatim is the
   no-surprises default; normalising touches the asset accessor contract.
