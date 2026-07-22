---
title: "Extract pkg/openapi into the transport-openapi companion module"
description: "Extract go-tool-base's pkg/openapi (an OpenAPI-spec + Stoplight Elements docs-site handler, 174 LOC + a ~2.4 MB embedded asset blob) into a standalone companion module gitlab.com/phpboyscout/go/transport-openapi, rather than folding it into go/transport core. pkg/openapi is already framework-free — it injects an *http.ServeMux and depends only on cockroachdb/errors plus the already-published go/transit/http and go/transport/http — and has zero GTB consumers, so the GTB side is a pure deletion. Keeping it a separate module keeps the 2.4 MB Stoplight embed out of every go/transport server binary that does not serve API docs. README-only, docs on the parent transport site, per the provider/companion convention."
date: 2026-07-22
status: IMPLEMENTED
tags:
  - specification
  - openapi
  - transport
  - module-extraction
  - companion
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/openapi` into the `transport-openapi` companion module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-22

Status
:   IMPLEMENTED (2026-07-22). `go/transport-openapi` v0.1.0 published; GTB cut-over
    deletes `pkg/openapi` (pure deletion — zero consumers, no repoint). Q1 (docs
    placement) resolved: **dedicated `transport-openapi.go.phpboyscout.uk`
    microsite**. Coverage 92.3%; gitleaks allowlist added for the vendored Stoplight
    assets; docs live.

Related
:   [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5 procedure; §10.5 provider/companion = README-only),
    [output extraction](2026-07-21-output-module-extraction-redesign.md) (the previous, more involved extraction),
    [extraction report](../reports/2026-07-07-package-extraction-report.md) (openapi row — decision: own `transport-openapi` companion)

---

## 1. Summary

`pkg/openapi` serves an OpenAPI specification and an interactive
[Stoplight Elements](https://stoplight.io/open-source/elements) docs site (with a
"try it" console) from a single `Register(mux, spec, opts...)` call. It is
extracted to a **standalone companion module**
`gitlab.com/phpboyscout/go/transport-openapi` — **not** folded into `go/transport`
core.

This is the cleanest extraction in the programme:

- **Already framework-free.** Zero `go-tool-base` imports. Its only dependencies
  are `cockroachdb/errors` and the *already-extracted* `go/transit/http`
  (the `Middleware` type) and `go/transport/http` (`SecurityHeadersMiddleware` /
  `SecurityHeadersOption`). It injects a caller-provided `*http.ServeMux`.
- **Zero GTB consumers.** Nothing in `go-tool-base` references it — it is a
  downstream-facing library feature. So the GTB side is a **pure deletion**: no
  repoint, no adapter, no new GTB dependency.
- **Small.** 174 LOC of Go (`openapi.go`) + tests, plus a ~2.4 MB embedded asset
  blob.

## 2. Why a companion module, not `go/transport` core

`go/transport` is the server stack (`http`, `grpc`, `gateway`, support code). The
OpenAPI handler *belongs* to that world — it mounts on a transport server's mux —
but it carries a cost the rest of the stack does not:

| Asset | Size |
|---|---|
| `assets/web-components.min.js` (Stoplight Elements) | **2.08 MB** |
| `assets/styles.min.css` | **297 KB** |

That ~2.4 MB is `//go:embed`-ed into the binary. Folding `openapi` into
`go/transport` core would add 2.4 MB to **every** transport-server binary, even
those that never serve API docs. A separate `transport-openapi` module keeps the
embed opt-in: only a tool that imports it pays for it. This mirrors the
`transport-metrics` companion direction and the general `transport-<companion>`
pattern.

## 3. Module shape & naming

Per the playbook §2 and the provider/companion convention (§10.5):

| Concern | Value |
|---|---|
| Module | `gitlab.com/phpboyscout/go/transport-openapi` |
| GitLab project | `phpboyscout/go/transport-openapi` |
| Package name | `package openapi` (bare, single concept) |
| Docs microsite | `transport-openapi.go.phpboyscout.uk` (dedicated, Q1 resolved) |

The public API moves **verbatim** — no redesign:

```go
func Register(mux *http.ServeMux, spec []byte, opts ...Option) error
func WithSpecPath(p string) Option
func WithDocsPath(p string) Option
func WithTitle(t string) Option
func WithSecurityHeaderOptions(opts ...transporthttp.SecurityHeadersOption) Option
func WithoutSecurityHeaders() Option
```

`go.mod` requires (matching GTB's current pins for lockstep):

- `gitlab.com/phpboyscout/go/transport v0.1.1`
- `gitlab.com/phpboyscout/go/transit v0.1.0`
- `github.com/cockroachdb/errors v1.14.0`

## 4. Dependency-footprint guard

The companion legitimately depends on `go/transport` and `go/transit`, so those
are **allowed**. `depfootprint_test.go` forbids what a docs handler should never
pull:

- `gitlab.com/phpboyscout/go-tool-base` (the whole point)
- `spf13/viper`, `spf13/pflag`, `spf13/cobra`
- cloud SDKs (`aws-sdk-go`, `cloud.google.com/go`, `Azure/azure-sdk`)

OpenTelemetry is **not** forbidden here: `go/transport`/`go/transit` may pull an
OTel-compatible seam transitively, exactly as `go/observability` does. The guard
checks `go list -deps ./...` and asserts the forbidden set is absent.

## 5. Migration procedure (playbook §5)

1. **Bootstrap** `phpboyscout/go/transport-openapi` to §3 (branding-free scaffold;
   cicd components at GTB's pin **v0.24.1**, Go **1.26.5**; `enable_e2e: false`;
   dedicated Diátaxis microsite with `zensical.toml` + `zensical-pages` +
   `requirements-lock.txt`, custom domain `transport-openapi.go.phpboyscout.uk`
   with LE cert, per Q1).
2. **Move the code** verbatim: `openapi.go`, `assets/*`, and the tests
   (`openapi_test.go`, `openapi_coverage_test.go` — both external `package
   openapi_test`, importing only the package + stdlib + testify, so they move
   cleanly). Add `depfootprint_test.go`.
3. **Shared-transitive alignment (§10.6/§11.5):** after `go mod tidy`, diff every
   shared transitive against GTB and bump to match; expect the security stage to
   flag anything the lean graph exposes.
4. **Cut `v0.1.0`** via the Release-MR flow (human-gated).
5. **GTB cut-over (single MR, `feat`):** **delete `pkg/openapi`** — no repoint
   (zero consumers), no new GTB dependency. `just ci`; add a
   `docs/reference/migration/v0.x-openapi-extracted.md` note.
6. **GTB docs cross-reference:** restub `docs/explanation/components/openapi.md`
   to point at the module; repoint the `how-to/serve-api-docs.md` guide to import
   `go/transport-openapi`; sweep `components/index.md` and the grpc/gateway/http
   pages' passing mentions.
7. **Downstream adopters** (any tool using `go-tool-base/pkg/openapi`, e.g.
   afmpeg) repoint to `gitlab.com/phpboyscout/go/transport-openapi`.

## 6. Version compatibility

`transport-openapi` depends on `go/transport` (and `go/transit`). Pre-1.0 the
transport API can break in a minor, and MVS can select a newer transport than the
companion was built against. Document the compatibility (`transport-openapi
v0.1.x` ↔ `transport v0.1.x`) in the README, mirroring the core↔sub-module matrix
guidance (§10.10).

## 7. Open question — RESOLVED (2026-07-22)

- **Q1 — docs placement. → Dedicated `transport-openapi.go.phpboyscout.uk`
  microsite**, consistent with the leaf modules (`output`, `workspace`, …), rather
  than README-only. Full Diátaxis nav (tutorial → how-to → explanation →
  pkg.go.dev), `zensical-pages`, custom domain + LE cert, branding-free scaffold.

## 8. Acceptance criteria

1. `gitlab.com/phpboyscout/go/transport-openapi` published at `v0.1.0`; resolves
   from the proxy; builds in a throwaway consumer (`-buildvcs=false`).
2. `depfootprint_test.go` green (no go-tool-base / viper / pflag / cobra / cloud
   SDK); module ≥90% coverage (the existing suite already covers `Register` and
   the option/error paths).
3. GTB cut-over MR (`feat(openapi):`) deletes `pkg/openapi`, `just ci` green,
   migration note added; docs swept.
4. README documents the API and the `transport` compatibility.
