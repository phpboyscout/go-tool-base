---
title: "Config family version re-pin: bring GTB and all 19 adapters to config head, and stop the drift recurring"
description: "GTB pins config core five minors behind (v0.4.0 vs v0.9.0), skipping hardening features shipped specifically for it, and the 19 config adapters pin core across five different minors (v0.3.0–v0.9.0) with no cross-version guarantee — MVS silently runs old-pin adapters against new core in consumer builds. One coordinated workstream: land the Apply absent-source fix in core first, re-pin the whole family and GTB to head, then add the durable mechanism (Renovate audit on the stale cohort plus a cross-repo adapter build in config's CI) so propagation cannot silently stall again. The forge-family credentials diamond is folded in as a secondary re-pin item."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - config
  - ecosystem
  - dependency-management
  - renovate
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Config family version re-pin: bring GTB and all 19 adapters to config head, and stop the drift recurring

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED (2026-07-27). Both parts done. **The re-pin sweep resolved
    organically** — Renovate brought GTB and all 19 config adapters (afero,
    billy, iofs, dotenv, hcl, ini, json, properties, toml, xml, vault, consul,
    aws-s3, aws-ssm, gcp-gcs, gcp-parameter, azure-blob, azure-appconfig, sftp)
    to a uniform config **v0.9.2** (the review's v0.3.0–v0.9.0 spread, and GTB's
    v0.4.0 pin, are gone), which carries the `config-apply-absent-source` fix.
    **The durable guard is in place** — a gating cross-repo canary in `go/config`
    CI (MR !66) builds every adapter against config HEAD before release:
    `canary-subset` (afero + consul + toml) on every MR, `canary-full` (all 19)
    on pre-release surfaces (Release MR / default branch / nightly). Proven to
    catch a break (a renamed `NewStore` in config HEAD failed the afero canary).
    The 19-repo separation is retained by maintainer decision (intentional
    SDK-dependency isolation); the canary — not consolidation — is the permanent
    mechanism (Q1 resolved). Q3 cadence recorded above. The forge-family
    credentials-diamond re-pin (secondary MEDIUM) also cleared via Renovate.

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (§ ecosystem, both HIGH findings + the credentials-diamond MEDIUM),
    [config Apply absent-source fix](2026-07-23-config-apply-absent-source.md) (must land in core first; this sweep carries it),
    [config v0.3 migration](2026-07-20-config-v0.3-migration.md) (the last deliberate GTB config bump — where the pin froze)

---

## 1. Problem

Two HIGH ecosystem findings, one shared root cause: propagation across the
config family stalls because every re-release is a human-gated Release MR and
a config-core change fans out into ~20–23 of them.

**GTB is five minors behind config core.** GTB `go.mod` pins
`gitlab.com/phpboyscout/go/config v0.4.0`; the latest remote tag is
**v0.9.0**. The missed range is not cosmetic — it includes hardening shipped
specifically for consumers like GTB: v0.5.0 exported codec conformance suite,
**v0.6.0 Sensitive enforcement at the write path (`ErrSensitiveLeak`)**,
v0.7.0 sensitive read-only backend refusing routed-beneath writes, v0.8.0
`ErrReadOnlyFS`, v0.9.0 `PollIntervalHinter`. The flagship consumer — the
reason config exists — runs without the sensitive-write enforcement. And
because pre-1.0 policy licenses breaking minors, every skipped minor makes the
eventual catch-up larger and riskier.

**The 19 adapters pin core across five different minors** (all verified
against remote-HEAD go.mods):

| core pin | adapters |
|----------|----------|
| v0.3.0 | **afero** — the one adapter GTB actually uses, furthest behind |
| v0.5.0 | dotenv, hcl, ini, json, properties, toml, xml |
| v0.6.0 | aws-ssm, azure-appconfig, consul, gcp-parameter |
| v0.7.0 | vault (also: created but never released — zero remote tags) |
| v0.9.0 | aws-s3, azure-blob, billy, gcp-gcs, iofs, sftp |

Any consumer combining a new-pin adapter with an old-pin one has MVS force
the old adapter to build and run against config v0.9.0 — a combination no
adapter CI ever tested. GTB does exactly this today: it pins config-afero
v0.1.0 (built against core v0.3.0) alongside core v0.4.0. The first genuinely
breaking core minor turns every stale-pinned adapter into a latent compile
failure that surfaces in *consumers'* builds, not in the adapter's own
pipeline.

**Underlying mechanism failure:** the fleet convention says Renovate proposes
these bumps, yet config v0.4.0→v0.9.0 sat unadopted in GTB across five
releases, and the adapter pins correlate with each repo's creation date.
Renovate is either not running, not configured for the `go/*` group, or its
MRs are not being merged on the stale cohort.

## 2. Proposed change

Three parts, in order.

**A. Sequencing with the Apply fix.** Land
[the Apply absent-source fix](2026-07-23-config-apply-absent-source.md) in
config core *first* and release it (v0.9.1 or v0.10.0). The sweep below then
targets that tag, so one pass both closes the version spread and delivers the
bug fix (plus its new conformance case) to every adapter and to GTB.

**B. The re-pin sweep.**

1. Bump all 19 adapters' `go/config` require to the new core tag; run each
   adapter's full suite (now including the new absent-source conformance
   case); release each via its Release MR (`fix(deps)` or `chore(deps)` +
   `fix` where the conformance suite is newly inherited — whichever
   releaser-pleaser needs to actually cut a tag, see Q2).
2. Release config-vault v0.1.0 as part of the sweep, or explicitly mark it
   WIP in its README (review LOW finding; it is the only adapter with CI but
   no tag).
3. Bump GTB: `go/config` → new core tag, `go/config-afero` (and any other
   config-family requires) → the freshly-released adapter tags. Verify the
   newly-enforced behaviours (`ErrSensitiveLeak` on sensitive writes,
   read-only refusals) against GTB's `config set`/wizard flows and adjust
   error-presentation if any new sentinel needs a user-facing hint.

**C. The durable mechanism (so this does not recur).**

1. **Renovate audit** on the stale cohort: confirm each config-family repo
   (and GTB) has Renovate actually running nightly with the
   `gitlab.com/phpboyscout/go/` group matched, and that its MRs are landing.
   Fold findings into the shared preset rather than per-repo fixes where
   possible.
2. **Cross-repo canary in config core's CI:** a job in `go/config` (MR
   pipelines, or at minimum pre-release) that checks out each adapter,
   applies a local `replace` to config HEAD, and runs `go build ./...` +
   `go test ./...`. This inverts the failure surface: a core change that
   breaks an adapter fails in *config's* pipeline before release, not in a
   consumer's build after. Advisory (`allow_failure: true`) initially, gating
   once stable.

**Secondary item — forge-family credentials diamond (in scope).** The same
review found forge core requiring `credentials v0.1.0` while forge-bitbucket
and GTB require **v0.2.0** — MVS silently runs forge core against a
credentials version it was never built against. This is the identical failure
mode at one-repo scale, and forge core just released v0.2.0 in the
forge-aware-setup wave, so the fix is one require bump in `go/forge` at its
next release. Folded in as a checklist item here rather than a separate spec;
it does *not* gate the config sweep.

## 3. Scope & release plan

Repos that change: `go/config` (canary CI job; the Apply fix ships via its own
spec), all 19 `go/config-*` adapters (dep bump + release; vault additionally
first release), `go/forge` (credentials require bump, next release), GTB (dep
bumps), plus Renovate preset/config repos as the audit dictates.

All releases are human-gated releaser-pleaser Release MRs. Sequencing:

1. **Wave 0:** config core — Apply fix released (companion spec). Canary CI
   job can land in the same or a following MR (CI-only, non-releasing).
2. **Wave 1:** all 19 adapters re-pinned to the wave-0 tag, released. These
   are mutually independent — parallelisable, one Release MR each.
3. **Wave 2:** GTB bumps core + adapter pins in one MR; e2e/integration run
   validates the newly-enforced core behaviours end-to-end.
4. **Independent:** forge core credentials bump rides forge's next scheduled
   release; Renovate audit proceeds in parallel with all waves.

## 4. Acceptance criteria

- [ ] Every `go/config-*` adapter's remote-HEAD `go.mod` requires the same
      config core version, and that version is the latest core tag.
- [ ] GTB `go.mod` requires the latest config core tag and the latest
      config-afero tag; `go mod graph` shows no config-family module resolved
      above its own tested pin.
- [ ] GTB exercises `ErrSensitiveLeak` (sensitive write-path enforcement) in
      at least one test, proving the v0.6.0 hardening is now active.
- [ ] The Apply absent-source regression test runs green in every adapter
      that consumes the conformance suite.
- [ ] config-vault either has a v0.1.0 remote tag or a README WIP notice.
- [ ] `go/forge` HEAD requires `credentials v0.2.0` (released at forge's next
      tag); `go mod graph` in GTB shows no credentials version skew.
- [ ] The config-core canary job exists, runs on config MR pipelines, and
      demonstrably fails when a deliberate breaking change is introduced
      against a stale adapter (verified once by a scratch branch).
- [ ] Renovate audit written up: for each config-family repo + GTB, evidence
      that a `go/*` dep bump MR was opened by Renovate within its schedule
      window after wave 0/1 tags appeared.

## 5. Open questions

- **Q1 — 19 repos vs one multi-module repo.** The review's deeper question:
  does the config adapter family need 19 independent repos, or would one
  multi-module repo with synchronised tagging (Go multi-module monorepo,
  per-adapter tags `config-afero/v0.x.y`) collapse the propagation problem
  structurally? This spec deliberately does **not** decide it — the sweep and
  canary are worth having either way — but the answer determines whether the
  canary is a stopgap or the permanent mechanism. Maintainer decision;
  candidate for its own spec if the answer is "consolidate".
- **Q2 — releasing commit types for pure dep bumps.** `chore(deps)`/
  `refactor` do not cut a releaser-pleaser release. Do the adapter re-pins
  ship as `fix(deps)` (a tag per adapter, honest changelog entry), or as
  `chore` + `rp-next-version::*` label per Release MR? Precedent from
  previous cut-overs suggests `fix(deps)`.
- **Q3 — canary trigger scope.** Run the 19-adapter canary on every config MR
  (slow, ~19 checkouts + test runs) or only on a pre-release schedule /
  Release-MR pipelines? A tag-filtered subset (fs adapters + one KV + one
  codec) on MRs with the full matrix pre-release may be the right balance.
    - **RESOLVED (2026-07-27, this spec's recommended balance).** Implemented in
      `go/config`'s `.gitlab-ci.yml` as two `parallel:matrix` jobs:
      `canary-subset` runs a representative slice — one filesystem adapter, one
      KV backend, one format codec (`config-afero`, `config-consul`,
      `config-toml`) — on **every config MR** (change-gated to Go/module/CI
      edits, skipped on the Release MR); `canary-full` runs all **19** adapters
      on the pre-release surfaces only — the releaser-pleaser Release MR,
      default-branch pipelines, and the nightly schedule. Both are gating (a red
      canary blocks the merge/release), with a documented `allow_failure: true`
      escape hatch to downgrade an individual job to advisory if adapter-side
      flake makes it noisy. The 19-repo split being permanent (Q1 deferred but
      the adapters intentionally isolate one SDK/backend/codec dep tree each)
      makes this canary the **durable guard**, not a stopgap.
- **Q4 — who owns the fleet Renovate preset.** The CI-component-pin drift
  (MEDIUM, 16 repos on cicd v0.22.0) has the same shape; should the Renovate
  audit here expand to cover component pins in the same pass, or stay
  config-scoped with the component sweep as a separate ops task?
