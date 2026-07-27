---
title: "forge-gitlab: stop forwarding PRIVATE-TOKEN across cross-host redirects (plus a reusable httpclient guard)"
description: "The GitLab provider attaches PRIVATE-TOKEN to asset downloads after a correct HostTrusted check, but Go's stdlib only strips Authorization/Cookie-class headers on cross-host redirects — a custom header like PRIVATE-TOKEN is copied to every hop, so an object-storage 302 or an open redirect on the GitLab host exfiltrates the token. Add a sensitive-header redirect guard, offered as a reusable go/httpclient option and consumed by forge-gitlab."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - forge
  - gitlab
  - httpclient
  - security
  - credentials
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# forge-gitlab: stop forwarding PRIVATE-TOKEN across cross-host redirects (plus a reusable httpclient guard)

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Shipped in go/httpclient v0.1.3 + go/forge-gitlab v0.2.2 (httpclient MR !9, forge-gitlab MR !11), red-first TDD with the defect reproduced against the module's pre-fix main before the change; GTB picks it up via the config-family / module dependency bumps. Open questions were resolved by the maintainer during triage.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH finding),
    [forge module extraction](2026-07-19-forge-module-extraction.md) (provider security model, `HostTrusted`),
    [transport client modules](2026-07-16-transport-client-modules.md) (`go/httpclient` extraction)

---

## 1. Problem

`go/forge-gitlab/release.go` (`DownloadReleaseAsset`) performs a correct
`forge.HostTrusted` check before attaching the credential, then sets a **custom
header** and sends via the shared client (release.go:186-190):

```go
if p.token != "" && forge.HostTrusted(url, p.apiBase) {
    req.Header.Set("PRIVATE-TOKEN", p.token)
}
resp, err := httpclient.NewClient().Do(req)
```

Go's `net/http` strips only `Authorization`, `Www-Authenticate`, `Cookie` and
`Cookie2` when a redirect crosses hosts; **custom headers are copied to every hop**.
`go/httpclient`'s redirect policy (`client.go:180-192`, wired at client.go:174) only
caps the hop count and refuses HTTPS→HTTP downgrades — it never strips headers or
re-checks the host. So the `HostTrusted` decision made against the *first* URL silently
extends to every host the response chain redirects to.

**Threat model: token exfiltration via redirect.**

- *Benign but real:* a self-managed GitLab with object storage and `proxy_download`
  disabled answers asset downloads with a 302 to S3/GCS — the instance-scoped PAT is
  sent to the storage provider (and lands in its access logs).
- *Malicious:* any open-redirect endpoint on the GitLab host lets a release author
  craft an asset link that is same-host (passes `HostTrusted`), then bounces the
  request — token attached — to an attacker-controlled host. Asset link URLs are
  release-author-controlled, which is exactly why the host pin exists; the redirect
  hop bypasses it.

The other providers are safe **by accident**: Bitbucket uses basic auth
(`Authorization`) and Gitea `Authorization: token …` — both stripped by the stdlib.
That protection is incidental and should become an explicit, tested guarantee.

## 2. Proposed change

Two parts: a reusable primitive in `go/httpclient`, consumed by `forge-gitlab`.

### 2.1 `go/httpclient`: sensitive-header redirect option

Add a `ClientOption` alongside the existing `WithMaxRedirects`/`WithTimeout` family:

```go
// WithSensitiveHeaders names request headers that carry credentials. On any
// redirect that leaves the host (host, scheme or port) of the *initial* request,
// the named headers are removed before the hop is followed. The stdlib already
// does this for Authorization/Cookie; this option extends the same semantics to
// custom credential headers such as PRIVATE-TOKEN.
func WithSensitiveHeaders(names ...string) ClientOption
```

Implementation: compose into `redirectPolicy` — the `CheckRedirect` func already
receives `req` (the upcoming hop) and `via` (the chain); compare `req.URL` against
`via[0].URL` on scheme/host/port and `req.Header.Del(name)` for each configured header
when they differ. Keep the existing hop cap and HTTPS→HTTP refusal.

**Alternative (stricter):** `WithRefuseCrossHostRedirects()` — fail the request
outright instead of stripping. Offer **both**; stripping preserves the working
object-storage download path (the hop proceeds unauthenticated, which S3 pre-signed
URLs need anyway), while refusal suits callers that would rather fail closed.

### 2.2 `go/forge-gitlab`: use it

In `DownloadReleaseAsset`, build the client as
`httpclient.NewClient(httpclient.WithSensitiveHeaders("PRIVATE-TOKEN"))` (constructed
once on the provider, not per call, if practical). Same-host redirects on the pinned
instance keep working; a hop to object storage proceeds without the token; a hop to an
attacker host carries nothing.

A per-hop `HostTrusted` re-check inside a custom `CheckRedirect` was considered as an
alternative to stripping. Stripping is preferred: it needs no forge dependency in
httpclient, and "credential never leaves the pinned host" is the actual invariant —
the destination host's trustworthiness for *unauthenticated* fetches is already bounded
by the existing downgrade/hop-count policy.

### 2.3 Sibling providers

Add regression tests (not code changes) to `forge-bitbucket` and `forge-gitea`
asserting their credentials are not forwarded cross-host, converting today's
accidental stdlib protection into a pinned contract. Document the requirement in the
`go/forge` provider-authoring notes: credential headers on downloads MUST be either a
stdlib-stripped header or guarded by `WithSensitiveHeaders`.

## 3. Scope & release plan

- **`go/httpclient`** — new option: `feat(client): …`, a **minor** release via its
  releaser-pleaser Release MR. No behaviour change for existing callers (opt-in).
- **`go/forge-gitlab`** — adopt the option: `fix(release): …`. Forge providers are
  kept in lockstep at **minor** versions; a patch on one provider does not break
  parity, so this can ship as a forge-gitlab patch alone. If the doc contract in
  `go/forge` (§2.3) warrants a core minor, publish the providers' next lockstep minor
  as usual.
- **`go/forge-bitbucket` / `go/forge-gitea`** — test-only additions (no release
  needed until their next scheduled bump).
- **go-tool-base** — consumes via go.mod bumps of httpclient + forge-gitlab; no GTB
  code change expected.

Order: httpclient minor first (forge-gitlab's go.mod needs the released option), then
forge-gitlab.

## 4. Acceptance criteria

- `httpclient` unit test: client with `WithSensitiveHeaders("PRIVATE-TOKEN")` against
  an `httptest` chain A→B (different hosts): header present on A, **absent** on B;
  same-host redirect (path-only 302): header **retained**.
- `httpclient` unit test: port and scheme changes count as leaving the host; existing
  hop-cap and HTTPS→HTTP tests still pass unchanged.
- If `WithRefuseCrossHostRedirects` is adopted: test that the request errors on the
  cross-host hop with a descriptive message.
- `forge-gitlab` regression test: `DownloadReleaseAsset` against a fake instance that
  302s to a second `httptest.Server` — asserts the second server receives **no**
  `PRIVATE-TOKEN` header, and the download still succeeds when the second server
  serves the asset unauthenticated (object-storage simulation).
- `forge-bitbucket`/`forge-gitea` tests: cross-host redirect on asset download carries
  no `Authorization` header (pinning the stdlib behaviour).
- Provider-authoring documentation in `go/forge` updated with the credential-header
  rule; module docs (microsites) regenerated.
- ≥ 90% coverage on new httpclient code; `just ci` equivalents green in each module.

## 5. Open questions

1. Should `WithSensitiveHeaders` strip on any host *change between consecutive hops*,
   or only relative to the initial request's host? (Initial-host comparison is
   simpler and matches the stdlib's model; consecutive-hop comparison would re-attach
   nothing anyway since we only ever delete.)
2. Ship both `WithSensitiveHeaders` and `WithRefuseCrossHostRedirects`, or only the
   stripping variant for now? Refusal is trivial to add later without breaking
   anything.
3. Should `httpclient.NewClient` gain a **default** sensitive-header set (empty today)
   — e.g. automatically treating `PRIVATE-TOKEN` and `X-Api-Key` as sensitive for all
   consumers? Proposed: no — keep it explicit and opt-in to avoid surprising callers
   that intentionally follow authenticated redirects within one trust domain.
