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

> **Status: EXTRACTED (2026-07-13).** `go/chat` + `chat-anthropic`/`openai`/
> `gemini` are published at `v0.1.0`; go-tool-base consumes them via the `pkg/chat`
> facade (cut-over MR !215). Docs live at `chat.go.phpboyscout.uk`. The concrete
> lessons are codified in [§10](#10-findings-from-the-first-greenfield-extraction-chat-2026-07-13).

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

## 10. Findings from the first greenfield extraction (`chat`, 2026-07-13)

The `chat` extraction (core `go/chat` + `chat-anthropic`/`openai`/`gemini`, all
`v0.1.0`, GTB cut-over in MR !215) validated §5 end-to-end and surfaced concrete
gotchas. Fold these into the procedure for every subsequent extraction.

### 10.1 Core must export a *provider-authoring API* (Stage-1 addendum)

For a package with sub-components that become **sibling modules** (chat's
providers; a signing backend), the sub-modules call helpers that were
*unexported* in the monolith. The move fails until the core **exports** them.
Before Stage 2, grep each sub-component for unexported core identifiers it uses
and export that set as a deliberate authoring API. For chat this was
`ResolveAPIKey`, `NewUsage`, `UsageTracker` (+`RecordUsage`/`SetObserver`),
`ChatHTTPClient`, `DispatchToolExecution`/`ExecuteTool`/`ExecuteToolsParallel`,
`ResolvedMedia`, `ValidateMediaSet`. Also have the core's `New()` hand the
factory a fully-resolved value (e.g. a non-nil `Settings.Logger`) so sub-modules
never need an unexported accessor.

### 10.2 Invert cross-provider SDK-type logic into a registry

Where the core inspects **SDK-specific types** of a sub-component (chat's
failover read `errors.As` against `*anthropic.Error`/`*openai.Error`/
`*genai.APIError` for the HTTP status), it cannot stay SDK-free. Invert it to a
registry (`RegisterStatusExtractor`) that each sub-module populates in `init()`,
mirroring the provider registry. Same pattern for any "the core must know a
vendor detail" seam.

### 10.3 Test redistribution is the bulk of the work — inventory first

The test suite is far more intertwined than the source (shared helpers, external
`*_test` packages driving real sub-components, one file testing several). Build a
**master inventory** of every `Test*`/`Fuzz*`/`Example*` func and assign each a
destination: core / a specific sub-module / stays-in-GTB-adapter. Cross-component
integration tests and the config-adapter tests stay in GTB. Verify by diffing the
monolith's test-func set against the union of the new modules — the only
un-migrated funcs should be the ones that legitimately stay. Nothing should
silently vanish.

### 10.4 Shared test infrastructure can't cross module boundaries

Test files aren't importable across modules, so any shared harness — mock HTTP
server, exec fakes, image fixtures, an integration-gate helper, a slog capture
handler — must be **vendored per module** (each keeps its own copy). Budget for
this; it's mechanical but easy to miss.

### 10.5 CI framework gotchas (bootstrap checklist)

- **`requirements-lock.txt`** is required for the `zensical-pages` docs build
  (copy from `signing`); its absence fails `zensical-build`.
- **`enable_e2e: false`** for library modules with no `test/e2e/` dir. Otherwise
  `go-test-e2e` runs `go test ./test/e2e/...`, fails on the missing path, and (as
  a stage dependency) **skips the security stage** — masking real findings.
- **Provider/adapter modules are README-only**: no `zensical-pages` component, no
  `docs/`, no `zensical.toml` (docs live on the parent's site).
- Pipeline shape (matches `signing`): a **main-branch push** runs only
  `releaser-pleaser` + `zensical-pages`; **content MRs** run the full
  lint/test/security gate; **release MRs** (the releaser-pleaser branch) run
  security-only. So the initial import to `main` is *not* gated by lint/test —
  validate locally, and let the first content MR exercise the full gate.

### 10.6 Local gates miss dependency vulnerabilities — CI catches them

`go build`/`test`/`lint`/`depfootprint` do **not** run `govulncheck`/osv-scanner.
A vendor SDK can drag in vulnerable transitives (chat-gemini's `genai` pulled
`x/crypto`/`x/net` with 9–10 CVSS advisories). Expect the security stage to flag
these; fix by bumping the transitive dep (`go get golang.org/x/crypto@latest …`)
and re-verify. Track `phpboyscout/cicd` at the latest version so the scanners
themselves are current.

### 10.7 Dependency-ordered release

Cut the core `v0.1.0` **first** (Release-MR flow); only then can the sub-modules
drop their local `replace ../core` directive, `go get core@v0.1.0`, and release.
`proxy.golang.org` serves the new tag within seconds of tagging, so no wait is
needed. Prove the whole family with a throwaway consumer module that imports the
core + sub-modules `@v0.1.0` from the proxy and builds (`-buildvcs=false` in a
non-git temp dir).

### 10.8 GTB cut-over: facade for large surfaces, repoint for small

`signing` had a small consumer surface, so its cut-over **repointed callers**
directly to the module (no aliases, per §5.6). `chat` had ~100s of references
across 14 files **plus** GTB-owned config-key constants that were deliberately
left out of the config-agnostic module — so its cut-over kept `pkg/chat` as a
**facade**: `reexport.go` type/const/func aliases forwarding to the module (a
*generic* func like `GenerateSchema[T]` needs a wrapper func, not a `var` alias),
`constants.go` re-homing the GTB config-key schema, `adapter_helpers.go`
reimplementing the few helpers that were unexported in the module,
`config_adapter.go` keeping the `*FromProps` adapters, and `providers.go`
blank-importing every provider. Choose per surface size; both are valid.

### 10.9 Cut-over coverage: re-cover the adapter

Deleting the monolith's tests (they moved with the code) drops the GTB
adapter package **below the ≥90% `pkg/` bar** and fails `coverage-policy`. Add
direct unit tests for the re-homed adapter helpers to restore it before the MR's
pipeline can pass.

### 10.10 Ship a core↔sub-module version-compatibility matrix

Pre-1.0 the authoring API can break in a *minor*, and Go's MVS can select a
**newer** core than a sub-module was built against (if the consumer, or any dep,
requires it) — a silent break. Release the family in **lockstep** at matching
minors and document the matrix on the core docs site (+ a pointer in each
sub-module README).

### 10.11 Workflow & infra notes

- **Maintainer merges; agents raise MRs.** Do not merge/tag — releaser-pleaser
  owns tagging via the Release MR. Auto-merge (merge-when-pipeline-succeeds) is
  **cancelled by any new commit** pushed to the MR, so a late fix means the
  maintainer must re-arm it.
- **Creating public repos and merging are gated** by the assistant's safety
  classifier and need explicit human authorisation (name the *public* visibility
  and the release intent). Repo settings mirror `signing` (public, `main`,
  ff-merge, squash-off, pipeline-must-pass).
- **Tokens** for `glab` + Cloudflare live in `~/.profile` (`GITLAB_TOKEN`,
  `CLOUDFLARE_API_TOKEN`); `source` it first.
- **Pages DNS** (zone `phpboyscout.uk`): CNAME `<name>.go` →
  `phpboyscout.gitlab.io` (proxied, orange-cloud — matches `signing`) + TXT
  `_gitlab-pages-verification-code.<name>.go` → the code returned when you
  `POST .../pages/domains` with `auto_ssl_enabled=true`. GitLab verifies
  asynchronously; the Let's Encrypt cert provisions once verified.
