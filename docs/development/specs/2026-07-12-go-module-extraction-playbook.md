---
title: "Go module extraction playbook & naming convention"
description: "Codifies how GTB's reusable pkg/ packages become independently versioned Go modules under the gitlab.com/phpboyscout/go/ subgroup: the naming convention (module path, per-provider backends, docs subdomain), the standard repository framework (cicd components, guards, docs+Pages), and the reusable per-component migration procedure (code move, decoupling, docs, cut-over, GTB cross-referencing). Establishes signing/signing-aws-kms relocation as the validation dry-run and pkg/chat as the first greenfield extraction."
date: 2026-07-12
status: APPROVED
tags:
  - specification
  - module-extraction
  - convention
  - documentation
  - gitlab-pages
  - process
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Go module extraction playbook & naming convention

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   12 July 2026

Status
:   APPROVED

Related
:   [`2026-07-07-package-extraction-report.md`](../reports/2026-07-07-package-extraction-report.md) (the strategic map + readiness checklist),
    [`2026-06-30-release-signature-verification-module.md`](2026-06-30-release-signature-verification-module.md) (the `signing` precedent, IMPLEMENTED),
    [`2026-07-05-chat-module-extraction.md`](2026-07-05-chat-module-extraction.md) (first component; to be aligned to this convention)

---

## 1. Context

The [package extraction report](../reports/2026-07-07-package-extraction-report.md)
established *which* `pkg/` packages should become standalone modules and in what
order. The `signing` / `signing-aws-kms` extraction proved the *mechanics*: a
framework-free module implementing a small contract, consumed back by GTB through
adapters, with its own CI, docs microsite, and a dependency-footprint guard.

This spec codifies the *repeatable process and conventions* so every subsequent
extraction is consistent and recognisable, rather than re-decided each time. It is
the meta-spec that per-component extraction specs (starting with
[`chat`](2026-07-05-chat-module-extraction.md)) cite for naming, repository
framework, and migration procedure.

It exists because we are about to extract many modules one package at a time, and
because the first two modules (`signing`, `signing-aws-kms`) were created before
these conventions were settled and now need to be brought into line.

## 2. Naming & namespace convention

All extracted Go modules live in a dedicated **language subgroup** so the family
is recognisable, future non-Go mirrors have a home, and bare component names stay
clean.

| Concern | Convention | Example |
|---|---|---|
| Module path | `gitlab.com/phpboyscout/go/<name>` | `gitlab.com/phpboyscout/go/chat` |
| Per-provider backend module | `gitlab.com/phpboyscout/go/<parent>-<provider>` | `gitlab.com/phpboyscout/go/chat-anthropic` |
| GitLab project | `phpboyscout/go/<name>` (in the `go` subgroup) | `phpboyscout/go/chat` |
| Docs microsite | `<name>.go.phpboyscout.uk` | `chat.go.phpboyscout.uk` |
| Package name | bare, lowercase, single concept | `package chat` |

Rationale:

- **Subgroup, not prefix.** `phpboyscout/go/chat` beats a `gtb-chat` prefix: it
  keeps names bare (matching `signing`), does not tie framework-free modules to
  the GTB brand, and reserves `phpboyscout/rust/…`, `phpboyscout/python/…` for
  language mirrors of the same component names.
- **Docs subdomain mirrors the module path.** `go/chat` → `chat.go.phpboyscout.uk`
  makes the mapping mechanical and scales to `chat.rust.phpboyscout.uk`.
- **Bare component names.** Recognition comes from the subgroup, the shared docs
  domain, and a common README header/badge — not from decorating the import path.

### 2.1 Go module resolution under a subgroup (confirmed)

Public modules resolve correctly at any subgroup depth. Go issues a
`https://gitlab.com/phpboyscout/go/<name>?go-get=1` discovery request; GitLab
returns a `go-import` meta tag naming the exact repository, which disambiguates
"subgroup vs package directory". `proxy.golang.org` / `sum.golang.org` serve the
module normally — no `GOPRIVATE`, auth, or `.netrc`. The only hard requirement is
that each `go.mod` declares the full path (`module gitlab.com/phpboyscout/go/<name>`)
and imports use it verbatim. Cost: one cached discovery request.

## 3. Standard repository framework

Every extracted module is bootstrapped to the same framework as `signing` (the
reference implementation). Track the `phpboyscout/cicd` components at the version
GTB currently tracks (**v0.19.0** at time of writing; bump in lockstep).

**CI (`.gitlab-ci.yml`)** — assembled from cicd components:

- `go-lint`, `go-test` (with `enable_e2e: true` where BDD applies), `go-security`
  — the MR merge gate.
- `releaser-pleaser` — Release-MR flow (no manual tags; Conventional Commits).
- `zensical-pages` — docs deploy on default branch + tags.
- `renovate-self` — scheduled dependency updates (`repositories: ["phpboyscout/go/<name>"]`).
- No `goreleaser` component for libraries (no binaries; integrity via `go.sum` +
  the Go checksum DB). Backend/tool modules that ship binaries add it.

**Quality guards:**

- `.golangci.yaml` — v2 format, local-prefix `gitlab.com/phpboyscout/go/<name>`,
  the GTB linter set (`perfsprint`/`wrapcheck`/`wsl` disabled as in GTB).
- `depfootprint_test.go` — asserts `go list -deps ./...` excludes `go-tool-base`,
  `spf13/viper`, `spf13/pflag`, `charmbracelet`, OpenTelemetry, and any cloud SDK
  not intrinsic to the module. This is the enforceable statement of "framework-free".
- `.mockery.yml` — mocks for any exported interface.
- godog `features/` + `test/e2e/steps/` where user-facing behaviour warrants BDD.
- **≥90% coverage** on the module's own packages.

**Conventions carried from GTB:**

- `cockroachdb/errors` for all error creation/wrapping.
- `*slog.Logger` as the only logging seam (optional, nil-safe); never a concrete
  logger or `pkg/logger`.
- Typed config structs owned by the module; **never** `config.Containable`. GTB
  does any Viper decoding in its own adapter (see §5).
- `LICENSE`, `CHANGELOG.md` (releaser-pleaser-owned), `README.md` with the shared
  toolkit header/badge, `docs/development/specs/` for the module's own specs.

## 4. Documentation & GitLab Pages

Docs are a first-class deliverable, structured per Diátaxis, matching `signing`:

- `zensical.toml` with `site_url = "https://<name>.go.phpboyscout.uk"`, curated
  Diátaxis nav (tutorial → how-to → explanation), and **Reference → pkg.go.dev**
  (the Go API reference is never hand-maintained).
- Content: `getting-started.md`, `how-to/*`, `explanation/*` (including a threat
  model where security-relevant). Godoc + runnable `Example` tests are the API
  reference surface.
- **Pages enablement (per repo, one-time infra):**
  1. `zensical-pages` CI job publishes the site on default-branch/tag pipelines.
  2. In GitLab **Settings → Pages**, add the custom domain `<name>.go.phpboyscout.uk`
     with Let's Encrypt TLS.
  3. DNS: a wildcard `*.go.phpboyscout.uk` record (or per-project CNAME) pointing
     at the GitLab Pages target, plus the per-project `_gitlab-pages-verification`
     TXT record GitLab issues.

### 4.1 GTB docs cross-reference the microsites

Once a component is extracted, GTB stops documenting its internals and instead
**points to the microsite**:

- The component's GTB explanation page (`docs/explanation/components/<name>.md`)
  becomes a short "GTB uses the standalone `<name>` module" stub linking to
  `https://<name>.go.phpboyscout.uk` and to the GTB-side adapter, not a copy of
  the module's docs.
- The GTB components index and README badge/table link out to each microsite.
- API reference for the module points to its `pkg.go.dev`, not GTB's.
- GTB retains only the docs for its **adapter** (how GTB maps `Props`/config into
  the module's typed constructors).

## 5. Per-component migration procedure

The repeatable sequence for each package (applied per the readiness order in the
extraction report):

1. **Bootstrap** the `phpboyscout/go/<name>` repo to §3 (empty scaffold, CI green
   on a trivial commit, Pages domain reserved per §4).
2. **Move the code** into the module. Sever any residual seams to `*slog.Logger`
   + typed config; keep `cockroachdb/errors`. For first-wave packages this is
   largely done in-tree already (the config-section-adapters + slog-first work) —
   the GTB coupling is confined to `*_config_adapter.go`/`*_adapter.go`, which
   **stay in GTB**, not the module.
3. **TDD/BDD + guards:** carry existing tests (test-first for new seams), keep
   ≥90% coverage, generate mocks, add the `depfootprint_test.go` guard, add BDD
   scenarios where behaviour is user-facing.
4. **Docs (Diátaxis) + Pages:** author the microsite; wire `zensical-pages`;
   enable the custom domain.
5. **Cut `v0.1.0`** (via the Release-MR flow).
6. **GTB cut-over (single MR):** add the module dependency; **delete the in-tree
   package**; repoint every caller to the new import path **with no aliases**;
   keep the GTB adapter code (`*_config_adapter.go`, `*FromProps`) in GTB, now
   constructing the module's exported types; fix blank-imports; run the full GTB
   suite + `just ci`; add a `docs/reference/migration/` note.
7. **GTB docs cross-reference** the new microsite (§4.1).
8. **Downstream adopters** (afmpeg, other tools) repoint as needed.

Each step is verifiable; a component is not "extracted" until step 7 is complete
and GTB is green consuming the published module.

## 6. Step 0 — relocate `signing` + `signing-aws-kms` into the subgroup

Before any new extraction, move the two shipped modules into the subgroup as a
**validation dry-run** on known-green code. This exercises the whole mechanism
(subgroup creation, Go resolution, Pages domain, GTB + afmpeg import repointing)
without the added risk of a fresh extraction.

Tasks:

1. Create the `phpboyscout/go` subgroup with the same group-level guards as other
   phpboyscout Go projects (protected branches, MR approvals, CI/CD variables:
   `RELEASER_PLEASER_TOKEN`, renovate, Pages).
2. **Transfer** `phpboyscout/signing` → `phpboyscout/go/signing` and
   `phpboyscout/signing-aws-kms` → `phpboyscout/go/signing-aws-kms` (GitLab keeps
   redirects, but we repoint imports explicitly rather than rely on them).
3. Change module paths: `gitlab.com/phpboyscout/signing` →
   `gitlab.com/phpboyscout/go/signing` (and the aws-kms module + its `signing`
   require). Update `renovate.json` repo scoping, `.golangci.yaml` local prefix,
   `zensical.toml` `site_url`/`repo_url`, README badges.
4. Move docs domain `signing.phpboyscout.uk` → `signing.go.phpboyscout.uk`
   (add new Pages custom domain; keep a redirect from the old host if cheap).
5. Cut new tags under the new path (module-path change ⇒ effectively a new module
   path; pre-1.0, so `v0.1.x` continues — consumers must update imports, which is
   a normal pre-1.0 break).
6. **Repoint consumers:** GTB (`go.mod` + every `gitlab.com/phpboyscout/signing…`
   import) and afmpeg; verify `gtb sign`/`gtb update` + suites, and afmpeg's clean
   module graph.
7. Migration notes in GTB (`docs/reference/migration/`) and the module READMEs.

Acceptance: both modules resolve and build under the new path; GTB + afmpeg green;
docs live at `signing.go.phpboyscout.uk`.

## 7. First component — `pkg/chat`

`chat` is the first greenfield extraction (highest value; core already decoupled —
props/config/logger confined to `chat/config_adapter.go`, only the `pkg/http`
transport seam remains). The existing
[`2026-07-05-chat-module-extraction.md`](2026-07-05-chat-module-extraction.md)
spec is **re-based onto this convention**:

- Core module `gitlab.com/phpboyscout/go/chat`; per-provider modules
  `go/chat-anthropic`, `go/chat-openai`, `go/chat-gemini` (init()/blank-import
  registration).
- Docs at `chat.go.phpboyscout.uk`.
- Replace the residual `pkg/http` dependency with an injected `*http.Client` /
  `HTTPClientFactory` (the last non-config seam).
- GTB consumes via the existing Props-mapping adapter; GTB docs point to the
  microsite.

That spec owns the chat-specific design; this playbook owns the convention and
process it follows.

## 8. Ongoing sequence

Per the extraction report's readiness checklist, after chat: the ready-now leaf
utilities (`redact`, `regexutil`, `browser`, `workspace`, `forms`, `output`,
`logger`), then `tls` + the `observability` group, then `credentials`/`authn`,
then the transport stack, the VCS tree, and finally root telemetry after its
analytics/observability split. Each application of §5 updates the checklist.

## 9. Resolved decisions (2026-07-12)

1. **Old-domain redirects — keep them.** `signing.phpboyscout.uk` →
   `signing.go.phpboyscout.uk` gets a redirect (cheap insurance for the few
   existing links); same policy for any future relocations.
2. **Shared toolkit landing — yes, build it.** A `go.phpboyscout.uk` index site
   lists every Go module (name, one-liner, docs link, pkg.go.dev link) and is the
   canonical family home. Stand it up alongside Step 0 so `signing` is its first
   entry; each subsequent extraction adds a row.
3. **README badge/header — yes, define once and reuse.** A shared header/badge
   marking a repo as "part of the phpboyscout Go toolkit" (linking `go.phpboyscout.uk`)
   is drafted during Step 0 on `signing` and copied to every module.
4. **Group-level guards — inherited.** Most protected-branch / approval / CI
   variable settings already exist at the `phpboyscout` group level and are
   inherited by the `phpboyscout/go` subgroup; no per-repo duplication needed.
   Only module-specific settings (Pages custom domain, `renovate.json` scoping)
   are set per repo.

### DNS automation

Subdomain records under `*.go.phpboyscout.uk` are managed in Cloudflare. Where a
scoped Cloudflare API token with DNS-edit permission is available, the per-module
record (`<name>.go` → GitLab Pages target) plus the GitLab Pages verification TXT
record are created via the Cloudflare API rather than by hand.
