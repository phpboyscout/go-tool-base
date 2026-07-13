---
title: "Extract pkg/controls into a standalone go/controls module"
description: "Extract the service-lifecycle supervisor (startup ordering, health monitoring, graceful shutdown, signal handling, restart policy) out of go-tool-base into gitlab.com/phpboyscout/go/controls. The package is already framework-free — its only external dependency is cockroachdb/errors and its only seam is a nil-safe *slog.Logger — so this is a clean git-mv extraction with a direct caller repoint (no facade, no Props adapter). Extracting controls early is the foundation the transport stack (http/grpc/gateway) later sits on."
date: 2026-07-13
status: IN PROGRESS
tags:
  - specification
  - controls
  - lifecycle
  - service-orchestration
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/controls` into a standalone `go/controls` module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-13

Status
:   IN PROGRESS (open questions resolved in review 2026-07-13)

## Summary

`pkg/controls` is a standalone service-lifecycle supervisor: startup ordering,
health monitoring (liveness/readiness), graceful shutdown with a bounded timeout,
OS-signal handling, and a self-healing restart policy. It has clear reuse value
for any long-running Go service, independent of the rest of GTB.

This spec extracts it to `gitlab.com/phpboyscout/go/controls` following the
[module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5
per-component procedure, §10 findings from the `chat` extraction). Unlike `chat`,
`controls` is **already framework-free**: production `pkg/controls` imports **zero**
go-tool-base packages, its only non-stdlib dependency is `github.com/cockroachdb/errors`,
and its only logging seam is a nil-safe `*slog.Logger` (default `slog.DiscardHandler`).
There is no `config.Containable` coupling and no `*FromProps` adapter to leave
behind. This is therefore the simplest kind of extraction — a `git mv` of the
source plus a **direct caller repoint** — markedly less involved than `chat` (no
per-provider modules, no vendor SDKs, no security-scanner surprises, no facade).

The [pre-extraction lifecycle hardening](../implementation-notes/2026-07-13-controls-preextraction-lifecycle-races.md)
(D8 startup cancel race, D9 error-send guard) is **already complete** on this
branch, alongside the earlier D1–D7 supervisor hardening
([2026-06-12-controls-supervisor-lifecycle.md](2026-06-12-controls-supervisor-lifecycle.md)).
The package is race-clean at 96.6% coverage. Nothing further blocks the move.

## Motivation

- **Reuse:** the supervisor is genuinely general-purpose; framework-free packaging
  lets any service adopt it without pulling in GTB.
- **Unblocks the transport stack:** `pkg/http`, `pkg/grpc`, and `pkg/gateway` all
  build on `controls`. Extracting `controls` first gives that stack a stable
  external foundation to sit on when it is extracted later (per the extraction
  report's roadmap). It is the recommended next substantial pick post-`chat`.
- **Low risk, high signal:** because the package is already decoupled, this
  extraction exercises the playbook procedure on a clean core with almost no
  decoupling work — a good, cheap confidence-builder before the chunkier
  transport-stack and VCS-tree extractions.

## Target module

- **Module path:** `gitlab.com/phpboyscout/go/controls` (bare name in the `go`
  subgroup, per [naming convention](../../../MEMORY.md) and playbook §2).
- **Package name:** stays `controls`.
- **Docs microsite:** `controls.go.phpboyscout.uk` (core module ⇒ full Diátaxis
  microsite, like `signing`/`chat`; not README-only).
- **Local dir:** `~/workspace/phpboyscout/go/controls` (mirrors the subgroup).
- **Reference scaffold:** the freshest sibling, `~/workspace/phpboyscout/go/chat`
  (cicd components `v0.22.0`, go `1.26.5`).

## What moves vs. what stays

### Moves into `go/controls` (the whole production package + pure tests)

Production source (all framework-free today):

- `controller.go`, `services.go`, `healthcheck.go`, `controls.go` (types:
  `Controller`, `Service`, `RestartPolicy`, `HealthCheck`, `HealthReport`,
  `ServiceStatus`, `ServiceInfo`, `State`, the `Runner`/`Controllable`/
  `HealthCheckReporter`/… interfaces, all `With*` options), `doc.go`.
- `README.md` content → seeds the microsite.

Pure tests (move as-is, with a one-line seam swap — see below):

- `controls_test.go`, `example_test.go`, `healthcheck_test.go`,
  `healthcheck_cancel_internal_test.go`, `lifecycle_test.go`,
  `lifecycle_race_test.go`.

These reach 96.6% coverage on their own, comfortably clearing the ≥90% bar, so the
module is self-sufficient for coverage without any transport-coupled tests.

**Test seam swap:** the pure tests currently obtain a logger via
`logger.ToSlog(logger.NewNoop())` and (in one place) `logger.NewCharm(buf)` for
log-capture assertions. In the module these become stdlib-only:
`slog.New(slog.DiscardHandler)` and a small `slog` text/JSON handler writing to a
`bytes.Buffer`. This removes the last `pkg/logger` reference and tests the *real*
seam (`*slog.Logger`) directly.

### Stays in GTB (transport-coupled integration tests)

Three integration tests drive **real `pkg/grpc` + `pkg/http` servers** through a
controller (`internal/testutil`, `mocks/pkg/config`, `pkg/grpc`, `pkg/http`):

- `controls_integration_test.go`, `server_integration_test.go`,
  `shutdown_integration_test.go`.

They test GTB's **composition** of controls with its transport servers, not the
supervisor in isolation, so they cannot live in the framework-free module (it
would create a forbidden go-tool-base dependency and a cycle). They stay in GTB,
repointed to the `go/controls` import path. **Open question O1** covers their new
physical home, since `pkg/controls/` is deleted.

The GTB e2e controls scenarios (`features/`, `test/e2e/support/controller.go`,
`test/e2e/steps/controls_steps_test.go`) likewise stay in GTB — they exercise the
CLI/server wiring, a GTB concern.

## Consumer surface & cut-over strategy

GTB consumers of `pkg/controls` (≈10 production files, ≈79 references across ≈17
symbols): `pkg/http` (`server.go`, `config_adapter.go`), `pkg/grpc` (`server.go`,
`config_adapter.go`), `pkg/gateway` (`gateway.go`, `config_adapter.go`),
`pkg/telemetry` (`observability.go`, `observability_config_adapter.go`),
`test/e2e/support/controller.go`, and one generated mock
(`mocks/pkg/grpc/healthSource.go`).

Per playbook §10.8 this surface is **moderate and alias-free-repointable** — there
are no GTB-owned constants and no `*FromProps` adapter to preserve. The cut-over is
therefore a **direct repoint** (like `signing`), **not a facade** (as `chat`
needed):

- Change every `gitlab.com/phpboyscout/go-tool-base/pkg/controls` import to
  `gitlab.com/phpboyscout/go/controls`. Symbol references are unchanged (package
  name stays `controls`).
- Delete the in-tree `pkg/controls/` package.
- Hand-edit the one generated mock's import (per the `just mocks` churn note —
  do not regenerate the whole tree).
- No `reexport.go`, no `constants.go`, no adapter helpers, no coverage backfill
  (there is no adapter package to re-cover — §10.9 does not apply).

## Repository framework (playbook §3)

Bootstrap from the `go/chat` scaffold:

- **CI** (`.gitlab-ci.yml`): `go-lint`, `go-test` (`enable_e2e: false` — see O2),
  `go-security`, `releaser-pleaser`, `zensical-pages`, `renovate-self` — all
  `@v0.22.0`. No `goreleaser` (library, no binaries).
- `.golangci.yaml` v2, local prefix `gitlab.com/phpboyscout/go/controls`, GTB
  linter set (`perfsprint`/`wrapcheck`/`wsl` disabled).
- `depfootprint_test.go`: assert `go list -deps ./...` excludes `go-tool-base`,
  `viper`, `pflag`, `charmbracelet`, OpenTelemetry, and all cloud SDKs. (Confirmed
  achievable now: the only external dep is `cockroachdb/errors`.)
- `.mockery.yml` for the exported interfaces (`HealthCheckReporter`, `Runner`,
  `Controllable`, etc.) if downstream mocking is wanted.
- ≥90% coverage (96.6% today).
- `cockroachdb/errors` throughout; `*slog.Logger` the only logging seam.
- `LICENSE`, `CHANGELOG.md` (releaser-pleaser-owned), README with the toolkit
  badge, `requirements-lock.txt` for the zensical build.

## Documentation (playbook §4)

- Author the `controls.go.phpboyscout.uk` Diátaxis microsite by porting GTB's
  existing controls docs (`docs/explanation/components/controls*`, plus the
  package `README.md`) and de-GTB-ing them (drop `Props`, reference the option
  API and `*slog.Logger` seam). Reference → pkg.go.dev.
- GitLab Pages custom domain + Cloudflare DNS (CNAME + verification TXT), mirroring
  `chat`/`signing`.
- GTB side (§4.1): replace the GTB controls component page with a stub linking to
  the microsite; migration note in `docs/reference/migration/`; components index +
  README badge updated.

## Migration procedure (playbook §5)

1. Bootstrap `phpboyscout/go/controls` repo to §3; trivial-commit CI green; Pages
   domain reserved.
2. `git mv` the production source + pure tests; apply the test seam swap.
3. Add `depfootprint_test.go`; regenerate mocks; confirm ≥90%; race/lint/vet green.
4. Author the microsite; wire `zensical-pages`; enable the custom domain + DNS.
5. Cut `v0.1.0` via the Release-MR flow (Matt merges; releaser-pleaser tags).
6. **GTB cut-over (single MR):** add the `go/controls v0.1.0` dependency; delete
   `pkg/controls/`; repoint all imports (no aliases); relocate the three
   transport-coupled integration tests (O1); hand-edit the mock import; run
   `just ci`; add the migration note.
7. GTB docs cross-reference the microsite (§4.1).
8. **Hand-off (not repoint):** downstream consumers (`haileys-app` and others
   outside this workspace) repoint themselves on their own schedule, guided by the
   GTB migration note — we do not edit their repos (O3).

## Resolved decisions (review 2026-07-13)

1. **O1 — Home for the transport-coupled integration tests.** *Resolved:* a new
   dedicated `test/integration/controls/` package holds
   `controls_integration_test.go` / `server_integration_test.go` /
   `shutdown_integration_test.go`, keeping the controls-lifecycle integration
   story cohesive in one place (repointed to `go/controls` + still importing GTB's
   `pkg/grpc`/`pkg/http`).
2. **O2 — Godog BDD in the module.** *Resolved:* **no** — unit tests + runnable
   `Example` tests suffice for `v0.1.0` (as with `signing`); `enable_e2e: false`.
   GTB retains the user-facing e2e controls scenarios.
3. **O3 — Downstream consumers.** *Resolved:* there are **multiple** downstream
   consumers of `pkg/controls` beyond this workspace (e.g. `haileys-app`, and
   others not visible here). We do **not** repoint them; the extraction is a clean
   pre-1.0 break and each consumer repoints itself, guided by the GTB migration
   note (import path `…/go-tool-base/pkg/controls` → `…/go/controls`, symbols
   unchanged). Step 8 is therefore a **hand-off via the migration guide**, not
   edits to downstream repos.
4. **O4 — cicd component version.** *Resolved:* pin `v0.22.0`, matching the `chat`
   sibling for lockstep; bump all Go modules together later.

## Acceptance criteria

- `go/controls` builds and resolves under `gitlab.com/phpboyscout/go/controls`;
  `depfootprint_test.go` passes (no go-tool-base / framework / SDK deps); ≥90%
  coverage; race/lint/vet green; `v0.1.0` published and proxy-resolvable.
- GTB consumes `go/controls v0.1.0`; `pkg/controls/` deleted; all callers
  repointed; the relocated integration tests pass; `just ci` green (modulo the
  known-unrelated `internal/agent` buildvcs sandbox test).
- Docs live at `controls.go.phpboyscout.uk`; GTB controls page is a stub linking
  out; migration note added; extraction report checklist ticks `controls`.
