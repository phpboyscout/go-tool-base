---
title: "Local development CA with automatic trust-store installation"
description: "A reusable, mkcert-style local certificate authority for GTB-built tools: on first run, generate a per-machine root CA, install it into the OS and NSS (Firefox/Chromium) trust stores, and mint short-lived leaf certificates so a tool can serve browser-trusted HTTPS on localhost and LAN IPs with zero manual setup. Feeds the existing pkg/tls.Pair so every transport consumes it unchanged. This is the 'local' half of a two-layer TLS strategy for the phpboyscout tool family; the 'public' half (ACME against Let's Encrypt) is a separate, downstream concern."
date: 2026-07-12
status: DRAFT
tags:
  - specification
  - tls
  - certificate-authority
  - trust-store
  - developer-experience
  - feature-request
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Local development CA with automatic trust-store installation

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   12 July 2026

Status
:   **SUPERSEDED (2026-07-16)** — the research and motivation below stand, but the
    implementation home changed. Rather than a GTB `pkg/tls/localca` feature, the local-CA
    is a **standalone, framework-free module**,
    [`gitlab.com/phpboyscout/go/localca`](https://gitlab.com/phpboyscout/go/localca) (spec
    `go/localca/docs/development/specs/2026-07-16-localca-module.md`), consumed **directly**
    by keryx/krites so no go-tool-base version bump is forced. Reason: `pkg/tls` is itself
    being extracted to `go/tls`, and coupling a consumer's TLS work to a framework upgrade is
    undesirable. Open questions Q1–Q6 here are **resolved** in the module spec §9. A GTB
    `trust` command feature wrapping the module remains a possible later add.

---

## 1. Motivation

Several GTB-built tools need to serve **browser-trusted HTTPS on a local machine** —
`localhost`, `127.0.0.1`, and LAN IPs — with no manual certificate fuss:

- **keryx** — a local "studio" web UI (served on the LAN for a mobile cockpit; needs a
  `Secure` session cookie, which requires HTTPS) and an OAuth callback loopback server
  (Meta rejects non-HTTPS redirects, even `http://localhost`). Today both use in-memory
  self-signed certs → a browser "untrusted certificate" warning. See keryx spec
  [`0027-trusted-ca-and-studio-https.md`].
- **krites** — packaged as a macOS `.dmg`; a "now open a terminal and run `mkcert
  -install`" step is unacceptable for a double-click app.
- Further tools in the same family are planned with the same need.

Each tool re-solving cross-OS trust-store installation is wasteful and error-prone. This
is exactly the kind of **framework-quality primitive that belongs in GTB** — a single,
tested component every tool consumes, mirroring how `pkg/tls` already centralises TLS
config plumbing.

### 1.1 Why local, and why not "just get a public cert"

Context from the downstream design (keryx 0027 §3, three research passes):

- **Operating a publicly-trusted CA — root or issuing/subordinate — is not achievable
  for a small org** (CA/Browser-Forum + WebTrust/ETSI audits; post-Trustwave-2013 even a
  name-constrained sub-CA needs CCADB disclosure + a contract with an issuing CA;
  commercial "dedicated intermediate" products are quote-only enterprise deals where the
  vendor holds the key).
- The **trilemma**: you can have at most two of {publicly trusted (no install) ·
  be-your-own-CA · no per-name external request}. The two achievable layers are:
  - **Layer 1 — a local CA** (this spec): mint any cert offline, instantly; must install
    a root into the machine's trust store. Zero infra, works offline forever.
  - **Layer 2 — public ACME** (Let's Encrypt for real hostnames resolving to local IPs):
    publicly trusted, nothing installed, but needs org-run DNS/broker infra + a live
    dependency. **Out of scope here** — a separate downstream/infra concern.

Layer 1 is the immediate need and the permanent dev/offline/CI fallback even once Layer 2
exists. This spec is Layer 1 only.

## 2. Goals & non-goals

**Goals**
- G1 — A `pkg/tls` sub-component that **generates a per-machine local root CA** and
  **installs/uninstalls it** into the system + NSS trust stores across macOS, Linux, and
  Windows (mkcert's model, as a library — not by shelling out to the mkcert binary).
- G2 — **Mint short-lived leaf certs** signed by that root for caller-supplied
  hostnames/IPs (`localhost`, `127.0.0.1`, `::1`, LAN IPs), and surface them as a
  `tls.Pair` (or `*tls.Certificate`) so existing transports consume them unchanged.
- G3 — A **first-run provisioning flow**: a tool calls one entry point; if no root
  exists it is minted + installed (with the one unavoidable OS elevation prompt), then a
  leaf is returned. Idempotent on subsequent runs.
- G4 — **Reversible**: uninstall the root from trust stores and purge the stored key.
- G5 — Fits GTB norms: `props`/DI-friendly, `afero.Fs`-backed storage, functional
  options, no package-level hooks, mockable interface, generated mocks, table-driven
  tests, docs page.

**Non-goals**
- N1 — The **public ACME / Let's Encrypt path** (Layer 2). Separate spec, likely partly
  in `phpboyscout/infra`.
- N2 — A **shared/organisation CA** whose key is distributed or shipped in a binary. A
  bundled shared CA key is a universal MITM key against every user — explicitly forbidden
  (§5). Every install generates its **own** root.
- N3 — Being a publicly-trusted CA in any form (§1.1).
- N4 — Server certificate *rotation/renewal at scale* beyond simple expiry-driven
  re-minting (Layer 2's ACME concern).

## 3. Proposed design

### 3.1 Package & surface

A new sub-package, provisionally **`pkg/tls/localca`** (name TBD — could be `pkg/certs`),
built so its output flows into the existing **`pkg/tls.Pair`**:

```go
// Authority is a per-machine local CA that can install itself into the host's
// trust stores and issue leaf certificates for local hosts.
type Authority interface {
    // Installed reports where the root is currently trusted (system, NSS, none).
    Installed(ctx context.Context) (TrustState, error)

    // Install mints the root if absent and installs it into the requested trust
    // stores. Installing into the system store triggers the OS elevation prompt.
    Install(ctx context.Context, stores ...Store) error

    // Uninstall removes the root from trust stores; Purge also deletes the key.
    Uninstall(ctx context.Context, opts ...UninstallOption) error

    // Leaf returns a short-lived server certificate valid for the given hosts,
    // signed by the root, minting/caching as needed. Errors if the root is absent
    // (caller decides whether to Install first — see EnsureServed).
    Leaf(ctx context.Context, hosts ...string) (*tls.Certificate, error)
}

// EnsureServed is the first-run one-liner (G3): ensure the root exists + is
// installed, then return a Pair ready to hand to any GTB transport.
func EnsureServed(ctx context.Context, deps Deps, hosts ...string) (tls.Pair, error)
```

- **`Deps`** carries the GTB primitives: `afero.Fs`, `logger.Logger`, the config
  container (`config.Containable`, post-v0.31.0), and a data dir. No globals.
- **Root**: ECDSA-P256, `IsCA`, ~10-year validity, CN `<tool> local CA (<user>@<host>)`
  — a per-machine identity, self-evidently not a shared authority.
- **Leaf**: short TTL (e.g. 90 days or per-run), SANs = caller's hosts + IPs; regenerated
  on expiry or SAN drift. Cached under the data dir.
- **Storage**: key + cert under the tool data dir, key file `0600`. (Open question §7:
  whether to optionally back the key with `pkg/credentials/keychain` — but keychain is
  unavailable/hangs in some headless environments, so file storage is the reliable
  default.)

### 3.2 Trust-store integration

Reuse mkcert's proven cross-OS logic as a **library**, almost certainly
**`github.com/smallstep/truststore`** (the same logic mkcert uses; MIT/Apache), rather
than shelling out to a `mkcert` binary or hand-rolling per-OS code:

| OS | System store | NSS (Firefox/Chromium) | Elevation |
|---|---|---|---|
| macOS | Keychain (`security add-trusted-cert`) | via `certutil` if present | Keychain admin prompt |
| Linux | `/usr/local/share/ca-certificates` + `update-ca-certificates` / `trust` | `certutil` per NSS DB | `sudo` (system) |
| Windows | CryptoAPI root store | via `certutil` | UAC (system) |

- **NSS** install is user-level (no elevation) but needs `certutil` (`libnss3-tools`);
  degrade gracefully with a clear message if absent.
- **Scopeable**: `Install(ctx, StoreSystem, StoreNSS)` — a caller (or CI) can pick.

### 3.3 First-run UX & consent (G3)

- A tool calls `EnsureServed(...)` at studio/server bind (or auth). First time: mint +
  install + one **OS elevation prompt** (Keychain/UAC/sudo). That prompt **is** the
  consent gate — there is no separate manual command, but trust is never installed
  silently without the OS asking. Subsequent runs are silent no-ops.
- The library must **log clearly** what it will touch before prompting ("installing a
  local development CA into your system trust store so <tool> can serve HTTPS locally;
  reverse with `<tool> trust uninstall`").
- **Consumption patterns** (tool's choice, both supported):
  - **Auto** (krites `.dmg`): call `EnsureServed` on first launch; the app surfaces the
    OS prompt natively.
  - **Explicit**: expose a generated `trust install|uninstall|status` command
    (`gtb generate command`) that wraps `Authority`, for tools/users who want the step
    visible. GTB could ship this as an optional **feature** (manifest-gated, like other
    GTB features) so a tool opts in with `gtb enable trust`.

### 3.4 Consuming from a tool

keryx (spec 0027) resolves a `CertSource` from config; the `localca` source is
`EnsureServed(...)`. Because the result is a `tls.Pair`, the studio's `http.Server` and
the OAuth loopback server consume it with no transport changes. krites does the same at
its server bind. Downstream tools set `tls.enabled=true` + `tls.source=localca` (or the
tool's equivalent) and get trusted HTTPS.

## 4. Feature / manifest shape

- Ships as a **library** (`pkg/tls/localca`) always available, plus an **optional GTB
  feature** for the generated `trust` command surface (so tools that don't serve HTTPS
  don't carry it). Mirrors how other GTB features are enable/disable-gated in
  `.gtb/manifest.yaml`.
- No new external runtime dependency beyond the trust-store library and (optionally) the
  platform `certutil` for NSS.

## 5. Security posture

- **Per-machine root, key `0600`**: authority never leaves the machine; blast radius = one
  machine; nothing central to revoke. The documented mkcert trade-off, stated plainly.
- **Never ship a CA key.** No tool may bundle a shared root; `localca` only ever
  *generates* one locally. (mkcert's own guidance: its `rootCA-key.pem` "gives complete
  power to intercept secure requests" and must never be shared/shipped.)
- **Consent via OS elevation** + reversible uninstall/purge. No silent system-store
  install.
- **Short-lived, SAN-scoped leaves** — no long-lived wildcards.
- Enables downstream `Secure`/`HttpOnly`/`SameSite` cookies once HTTPS is served
  (keryx 0018 §5).

## 6. Testing / DoD (GTB standards)

- Unit, table-driven, `t.Parallel()`, `logger.NewNoop()`: root mint (IsCA, validity, key
  perms), leaf issuance (SAN coverage for loopback + LAN, expiry/drift re-mint),
  `TrustState` reporting, `EnsureServed` idempotency, config resolution via
  `config.Containable`.
- **Trust-store install/uninstall is env-gated integration** (`INT_TEST=1`) — it mutates
  the real OS/NSS store and can't run in ordinary CI; assert idempotency + clean
  uninstall per-OS where a runner allows.
- Interface mocked into `mocks/`; `just ci` green; `-race`.
- **Docs**: a `pkg/tls` component page section ("Local development CA") + a how-to
  ("Serving your tool over HTTPS locally") + the security posture. Docs-as-you-go.

## 7. Open questions

- Q1 — Package name/placement: `pkg/tls/localca` vs `pkg/certs` vs folding into `pkg/tls`.
- Q2 — Key storage: file-only (reliable, headless-safe) vs optional
  `pkg/credentials/keychain` backing (note: keychain hangs in some headless/dev-server
  setups — file must remain the default).
- Q3 — Ship the `trust` command as a first-class GTB feature, or leave each tool to
  scaffold its own around the library?
- Q4 — Default leaf TTL (90d vs per-run) and root validity (~10y, mkcert default).
- Q5 — How much of mkcert's logic to take via `smallstep/truststore` vs vendor minimal
  per-OS code (dependency surface vs control).
- Q6 — Windows/Linux CI story for the integration tests (which runners can mutate a
  trust store).

## 8. Relationship to the public (Layer 2) path

This component is deliberately independent of the eventual **ACME / Let's Encrypt**
provisioning (publicly-trusted certs for `*.local.phpboyscout.uk`-style names resolving to
local IPs, via an org-run acme-dns broker). Both layers ultimately hand a tool a
`tls.Pair`; a tool picks its source by config. Layer 2 has its own spec (downstream /
infra) and does not block this work. Even after Layer 2 ships, this local CA remains the
**offline / air-gapped / CI / no-network** fallback — it is not superseded.

---

*Raised from keryx spec 0027 (studio HTTPS + the `Secure` gate). Consumers at time of
writing: keryx, krites, and further planned phpboyscout tools.*
