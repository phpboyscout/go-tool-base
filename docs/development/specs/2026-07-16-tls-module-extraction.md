---
title: "Extract pkg/tls into a standalone go/tls module"
description: "Extract the hardened TLS plumbing (default config, the typed Pair cert shape, ServerConfig/ClientConfig builders, and the CA cert-pool helpers) out of go-tool-base into gitlab.com/phpboyscout/go/tls. The crypto core is framework-free — only crypto/tls, crypto/x509, os and cockroachdb/errors — so it moves cleanly; the single GTB seam is the config-key Resolve() adapter, which stays behind in a thin pkg/tls facade. First Phase 2 foundation of the transport-stack extraction plan."
date: 2026-07-16
status: IMPLEMENTED
tags:
  - specification
  - tls
  - transport
  - security
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/tls` into a standalone `go/tls` module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-16

Status
:   IMPLEMENTED (2026-07-18) — shipped to main; originally APPROVED (O1–O2 resolved in review 2026-07-16)

## Summary

`pkg/tls` is the shared TLS plumbing every transport in GTB builds on: the hardened
default `*crypto/tls.Config` (TLS 1.2 floor, curated AEAD cipher suites, modern
curve preferences), the typed [`Pair`](../../../pkg/tls/config.go) enabled/cert/key
shape with shared/per-transport resolution, the `ServerConfig`/`ClientConfig`
builders, and the CA cert-pool helpers. It has clear reuse value for any Go service
or client that wants GTB's opinionated, hardened TLS defaults without pulling in the
framework.

This spec extracts it to `gitlab.com/phpboyscout/go/tls` following the
[module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5
per-component procedure, §10/§11 findings) and is the **first Phase 2 foundation**
of the [transport-stack extraction plan](2026-07-13-transport-stack-extraction-plan.md).
Extracting `tls` (with `authn` and `observability`) ahead of the transport stack is
what lets `pkg/http`, `pkg/grpc`, and `pkg/gateway` become framework-free in Phase 5.

Unlike the Phase 1 leaves (`redact`/`regexutil`/`browser`, pure repoints), `tls` is
a **facade** extraction: its crypto core is framework-free, but one function —
`Resolve()` — reads GTB config (`config.Containable`) to materialise a `Pair` from
the `server.tls` / per-transport key hierarchy. That adapter cannot live in a
framework-free module, so it **stays behind** in a thin `pkg/tls` facade (per
[playbook §11.1](2026-07-12-go-module-extraction-playbook.md), "repoint vs facade is
decided by adapter ownership, not surface size"). This mirrors `chat`: consumers keep
importing `pkg/tls` unchanged; the facade re-exports the module's core and retains the
config adapter.

## Motivation

- **Unblocks the transport stack.** `pkg/http`, `pkg/grpc`, and `pkg/gateway` all
  depend on `tls`. It must extract before them (Phase 5) so the transport modules
  can import `go/tls` rather than a still-in-tree `go-tool-base/pkg/tls` their
  depfootprint guard forbids.
- **Reuse.** The hardened default config + cert-pool helpers are genuinely
  general-purpose; `go/tls` gives any client/server GTB's TLS posture in one small,
  dependency-light import (exactly what `go/httpclient`/`go/grpcclient` will want).
- **Low risk, high signal.** The crypto core is 100% framework-free and already at
  full coverage; the only decoupling is confining `Resolve()` to the facade. A cheap,
  clean opener for Phase 2 before the chunkier `observability` and transport work.

## Target module

- **Module path:** `gitlab.com/phpboyscout/go/tls` (bare name in the `go` subgroup,
  per [naming convention](../../../MEMORY.md) and playbook §2).
- **Package name:** stays `tls`.
- **Docs:** `tls.go.phpboyscout.uk` — scope is **O2** (light microsite vs README-only;
  it is a small, single-concept package).
- **Local dir:** `~/workspace/phpboyscout/go/tls` (mirrors the subgroup).
- **Reference scaffold:** the freshest sibling, `~/workspace/phpboyscout/go/controls`
  / `go/chat` (cicd components `v0.22.0`, go `1.26.5`).

## What moves vs. what stays

### Moves into `go/tls` (the crypto core + pure tests)

Production source (framework-free today — only `crypto/tls`, `crypto/x509`, `os`,
`github.com/cockroachdb/errors`):

- `tls.go` — `DefaultConfig`, `Pair.Valid`, `Pair.Certificate`, `Pair.ServerConfig`,
  `CertPool`, `ClientConfig`.
- `config.go` — the typed `Pair`, `PairOverrides`, and the pure typed-merge
  `ResolvePair(shared, transport, overrides)`. No config import; operates on already
  materialised typed values (this is the "core works from typed `Pair` values" half
  of `doc.go`).
- `doc.go` (de-GTB-ed: drop the framework framing, keep the typed-`Pair` vs
  config-integration distinction — the latter now lives in the GTB facade).

Pure tests (move as-is): `tls_test.go`, `coverage_test.go`, `example_test.go` — none
import `pkg/config`, `pkg/logger`, or mocks. These carry the module's coverage on
their own.

### Stays in GTB (the config-key facade)

`pkg/tls` remains as a **thin facade** package (name stays `tls`) containing:

- `config_adapter.go` — `Resolve(cfg config.Containable, transportPrefix string) Pair`
  and its `pairFromConfig` / `resolveLegacy` helpers. This is the only code touching
  `pkg/config`; it materialises typed `Pair`s from the config hierarchy then delegates
  the merge to the module's `ResolvePair`.
- `config_adapter_test.go` (imports `mocks/pkg/config`, `pkg/logger`).
- `const SharedPrefix = "server.tls"` — a **GTB config-key convention**, not a generic
  TLS concept, so it stays with the adapter (see O-resolved below), not in the module.
- Re-exports so the eight transport consumers compile unchanged:
  `type Pair = gtls.Pair`, `type PairOverrides = gtls.PairOverrides`, and thin
  re-exports of `DefaultConfig`, `ClientConfig`, `CertPool`, `ResolvePair`.

## Consumer surface & cut-over strategy

The **only** consumers of `pkg/tls` are the three transport packages (all Phase 5),
importing it as `gtbtls`:

| Symbol | Uses | Source after extraction |
|---|--:|---|
| `gtbtls.Pair` | 63 | facade alias → `go/tls` |
| `gtbtls.Resolve` | 7 | **facade (stays in GTB)** — config adapter |
| `gtbtls.DefaultConfig` | 5 | facade re-export → `go/tls` |
| `gtbtls.ClientConfig` | 1 | facade re-export → `go/tls` |

Because the facade re-exports the core and retains `Resolve`, **no consumer file
changes** in this extraction (zero churn across `pkg/http`, `pkg/grpc`, `pkg/gateway`
production and their ~9 test files). The direct-to-`go/tls` repoint of the core
symbols happens naturally in **Phase 5**, when those packages themselves extract and
`Resolve` stays behind as GTB's tls config adapter (per the transport plan, "config
adapters stay in GTB").

This facade-now / repoint-in-Phase-5 choice is **O1**.

## Repository framework (playbook §3)

Bootstrap from the `go/controls` scaffold:

- **CI** (`.gitlab-ci.yml`): `go-lint`, `go-test` (`enable_e2e: false`),
  `go-security`, `releaser-pleaser`, `zensical-pages`, `renovate-self` — all
  `@v0.22.0`. No `goreleaser` (library, no binaries).
- `.golangci.yaml` v2, local prefix `gitlab.com/phpboyscout/go/tls`, GTB linter set
  (`perfsprint`/`wrapcheck`/`wsl` disabled).
- `depfootprint_test.go`: assert `go list -deps ./...` excludes `go-tool-base`,
  `viper`, `pflag`, `charmbracelet`, OpenTelemetry, and all cloud SDKs. (Trivially
  achievable: the only external dep is `cockroachdb/errors`; verify whether even that
  is reachable — `ResolvePair`/`DefaultConfig` are error-free, only `Certificate`/
  `CertPool`/`ClientConfig` wrap errors.)
- No mockery (no exported interfaces).
- Full coverage retained (crypto core is already exhaustively tested); ≥90% bar.
- `LICENSE`, `CHANGELOG.md` (releaser-pleaser-owned), README with toolkit badge,
  `requirements-lock.txt` for the zensical build.

## Documentation (playbook §4)

- Author `tls.go.phpboyscout.uk` (scope per **O2**) from `pkg/tls`'s `doc.go` +
  any GTB TLS references, de-GTB-ed (the config-`Resolve` story stays in GTB docs;
  the module docs cover the hardened defaults, typed `Pair`, and the builders).
  Reference → pkg.go.dev.
- GitLab Pages custom domain + Cloudflare DNS (CNAME → `phpboyscout.gitlab.io`
  **DNS-only** per the `.go` two-level-subdomain TLS learning + verification TXT),
  Pages visibility = everyone, LE cert re-requested. Mirrors `controls`/`chat`.
- GTB side (§4.1): the GTB TLS component/reference page keeps only the `Resolve`
  config-adapter + `server.tls` key-hierarchy docs and links out to the microsite for
  the core; migration note in `docs/reference/migration/`; components index + README
  badge updated.

## Migration procedure (playbook §5)

1. Bootstrap `phpboyscout/go/tls` repo to §3; trivial-commit CI green; Pages domain
   reserved + DNS created.
2. `git mv` the crypto core (`tls.go`, `config.go`, `doc.go`) + pure tests
   (`tls_test.go`, `coverage_test.go`, `example_test.go`).
3. Add `depfootprint_test.go`; confirm coverage ≥90%; race/lint/vet green.
4. Author the microsite; wire `zensical-pages`; enable custom domain + DNS.
5. Cut `v0.1.0` via the Release-MR flow (Matt merges; releaser-pleaser tags). Verify
   proxy-resolvable.
6. **GTB cut-over (single MR):** add `go/tls v0.1.0`; reduce `pkg/tls` to the facade
   (`config_adapter.go` + `config_adapter_test.go` + `reexport.go` with the aliases +
   `SharedPrefix`); delete the moved core files; `just ci` green (modulo the known
   `internal/agent` buildvcs sandbox test); add the migration note; tick the
   extraction-report checklist.
7. GTB docs cross-reference the microsite (§4.1).
8. **Hand-off (not repoint):** downstream consumers repoint themselves on their own
   schedule, guided by the GTB migration note — we do not edit their repos.

## Resolved decisions

- **`SharedPrefix` stays in GTB.** `"server.tls"` is a GTB config-key convention, not
  a property of TLS; only the config adapter references it (no consumer uses
  `gtbtls.SharedPrefix` directly). Keeping it with `Resolve` keeps the module free of
  GTB config semantics.
- **cicd component version = `v0.22.0`**, matching the `controls`/`chat` siblings and
  GTB main; bump all Go modules together later.
- **No Godog BDD in the module** — unit + `Example` tests suffice for `v0.1.0`
  (as `signing`/`controls`); `enable_e2e: false`.

## Resolved open questions (review 2026-07-16)

1. **O1 — Facade now vs. repoint the transport packages to `go/tls` now.**
   *Resolved: facade now.* Keep `pkg/tls` as a thin facade (core re-exported,
   `Resolve` retained), so this extraction is **zero-consumer-churn** and the Phase-2
   diff stays minimal. The transport packages naturally switch their core-symbol
   imports (`Pair`/`DefaultConfig`/`ClientConfig`) to `go/tls` in **Phase 5** when they
   themselves extract, at which point `Resolve` remains behind as GTB's tls config
   adapter. (Rejected alternative: repointing those three packages' core usages now
   churns ~63 `Pair` references and ~9 files in packages we rewrite in Phase 5 anyway,
   for no interim benefit.)
2. **O2 — Docs: light microsite vs README-only.** *Resolved: light microsite* at
   `tls.go.phpboyscout.uk`, for family consistency with `signing`/`chat`/`controls`.
   Reference still → pkg.go.dev, so it is an overview + one how-to + the security
   threat-model explanation (cipher/curve/TLS-floor rationale), not a large site.

## Acceptance criteria

- `go/tls` builds and resolves under `gitlab.com/phpboyscout/go/tls`;
  `depfootprint_test.go` passes (no go-tool-base / framework / SDK deps); coverage
  ≥90%; race/lint/vet green; `v0.1.0` published and proxy-resolvable.
- GTB consumes `go/tls v0.1.0`; `pkg/tls` is the facade (config adapter + re-exports);
  the moved core files are deleted; **no transport consumer file changed**; `just ci`
  green (modulo the known `internal/agent` buildvcs sandbox test).
- Docs live per O2; the GTB TLS page keeps only the config-adapter story and links
  out; migration note added; extraction-report checklist ticks `tls`.
