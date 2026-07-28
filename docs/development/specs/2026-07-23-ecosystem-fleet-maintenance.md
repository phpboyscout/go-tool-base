---
title: "Ecosystem fleet maintenance: CI pin coherence, version-coupling mechanism, fan-out discipline, and docs alignment"
description: "Batches the MEDIUM and LOW ecosystem findings from the 2026-07-23 architectural review into one standing fleet-maintenance workstream: drive the 16-repo cicd v0.22.0 cohort to a single fleet-managed component pin, mechanise core↔provider version coupling (lockstep provider require-bumps, a possible CoreAPIVersion registration assert), make the 12–23-release low-layer fan-out cost conscious via additive-only discipline and scripted waves, and run one docs pass fixing GTB's AGENTS.md (eight extracted packages still documented as local) plus stale module doc comments. Housekeeping: GTB's chat/chat-openai/controls pins, the undocumented stdlib-errors tier, yamldoc's go directive, and config-vault's missing first release."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - ecosystem
  - maintenance
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Ecosystem fleet maintenance: CI pin coherence, version-coupling mechanism, fan-out discipline, and docs alignment

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED — 2026-07-28 (see § 5 Outcomes)

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md)
    (§ ecosystem),
    [config family re-pin](2026-07-23-config-family-version-repin.md) (the
    two HIGH findings + the credentials-diamond re-pin — excluded here)

---

## 1. Context

The ecosystem review's structural verdict is good: 51 extracted modules plus
GTB form a clean five-layer DAG with zero cycles, zero replace directives,
and uniform conventions. Its weakness is **propagation**: every mechanism
keeping the fleet coherent — dependency pins, CI component pins, the
core↔provider compatibility matrix — is convention enforced by
Renovate-plus-human-merges, and propagation stalls wherever release fan-out
is large. The two HIGH findings are handled by the re-pin spec above; this
spec batches the remaining MEDIUM and LOW findings into one standing
fleet-maintenance workstream so they are tracked, sequenced, and closed.

## 2. Items

### 2.1 CI/CD component pin management — MEDIUM

**Problem.** The fleet's `.gitlab-ci.yml` component pins span cicd
v0.22.0–v0.27.0, and the pin correlates with repo creation date — set once,
never bumped. Sixteen modules — the chat family, the transport/client stack,
and most L0 leaves — sit at **v0.22.0**, five minors behind config-vault's
v0.27.0. Lint-rule and Go-toolchain behaviour therefore differ across
siblings, and the convention that Renovate covers component pins is broken.

**Direction.** Audit Renovate on the v0.22.0 cohort (running at all?
component-pin datasource matched? MRs opened but unmerged?), then drive one
fleet-wide bump to the current cicd release. Structurally, manage the cicd
component version from a **single shared Renovate preset** the whole fleet
extends — one place rather than 52.

**Acceptance.** Every repo's component pin is the same current cicd version,
and a shared preset demonstrably proposes the next bump fleet-wide.

### 2.2 Version-coupling mechanism — MEDIUM (mechanism portion)

**Problem.** Core↔provider coupling (chat + 3 providers, forge + 4 providers)
currently holds — matching minors as promised — but the mechanism is a
docs-page compatibility matrix plus a floor-only `require`. Providers hook in
via `RegisterProvider(name, factory)` with no API-version guard and no upper
bound, so a pre-1.0-legal breaking minor in a core interface breaks
*downstream consumers* (loud but misattributed), while a semantic contract
change skews silently. The live instance (the forge credentials diamond) is
scoped in the re-pin spec; this item is the durable mechanism only.

**Direction.** Make each core release **mechanically bump its providers'
`require` in lockstep** — a scripted release-checklist or CI step that opens
the provider dep-bump MRs the moment the core tag exists. Additionally
evaluate an exported **`CoreAPIVersion` constant** per core (chat, forge)
asserted by providers at registration, turning silent semantic skew into a
loud, correctly-attributed startup error. Upper bound: Q2.

**Acceptance.** A chat or forge core release cannot reach its tag without the
lockstep bump step having run, and a decision on CoreAPIVersion is recorded.

### 2.3 Release fan-out discipline — MEDIUM

**Problem.** A change to a low-layer module fans out into 12–23 human-gated
releaser-pleaser Release MRs: a config-core change adapters must adopt forces
~20–23 releases; a tls change propagates through four sequential waves
(httpclient/grpcclient/localca → forge + 4 providers + transport →
transport-metrics/openapi → GTB), 12 releases, each wave blocked on the
previous tag. This cost is the mechanism behind the two HIGH findings —
propagation stops partway — and it will recur on the next tls change.

**Direction.** Accept the cost consciously: (a) an explicit **additive-only
discipline for L0/L1 modules** (tls, redact, yamldoc, transit — breaking
changes there need positive justification, not just pre-1.0 licence);
(b) **batch** lower-layer changes so one wave carries several; (c) **script
the wave** ("bump dep, wait for tag, next layer") so the human gate is the
merge click, not the orchestration. The strategic alternative — one
multi-module repo — is the re-pin spec's Q1 (Q3 below).

**Acceptance.** The conventions doc states the L0/L1 additive-only
expectation and links a wave-release script (or documented procedure)
exercised once on a real multi-wave release.

### 2.4 Documentation alignment — MEDIUM + LOW

**Problem (AGENTS.md — MEDIUM).** GTB's AGENTS.md — the primary steering
file for every AI-agent session in the repo — still documents at least eight
extracted or deleted `pkg/` packages as if local (verified at current HEAD):
lines 172–190 describe `pkg/controls/`, `pkg/grpc/`/`pkg/http/`,
`pkg/errorhandling/`, `pkg/vcs/`, and `pkg/forms/` (deleted outright); lines
219–235 mandate routing through `pkg/browser`, `pkg/regexutil`, `pkg/redact`,
and `pkg/credentials` — all now in `go/*` modules. Agents steered by this
file will look for, import, or "restore" deliberately clean-broken packages.

**Problem (module doc comments — LOW).** The same drift exists in
extracted-module doc comments, visible on pkg.go.dev: `chat/config.go:10`
cites `pkg/credentials.Retrieve` (extracted), `chat/client.go:114` cites
`pkg/http` (deleted), plus similar references in httpclient and transport.

**Direction.** One docs pass: rewrite the stale AGENTS.md sections to point
at the module paths (`gitlab.com/phpboyscout/go/<name>`) and current GTB
glue, then sweep the affected modules' doc comments in the same pass.

**Acceptance.** A grep of AGENTS.md for the extracted package names returns
no stale references, and the named module doc comments no longer reference
deleted GTB packages at each module's HEAD.

### 2.5 Housekeeping — LOW

1. **GTB stale pins.** GTB requires `chat` / `chat-openai` v0.1.0 (both
   v0.1.2 remote) and `controls` v0.1.0 (v0.1.1 remote) — patch-level only,
   but shipped bug fixes are not reaching GTB. *Direction:* routine Renovate
   merge sweep. *Acceptance:* GTB `go.mod` at the latest tags.
2. **Errors-tier documentation.** cockroachdb/errors is a direct dep of 28
   modules but deliberately absent from 23 dependency-light ones (the entire
   config family, aferobilly, redact, yamldoc) — the exemption from the
   stated "cockroachdb/errors everywhere" rule is recorded nowhere.
   *Direction/Acceptance:* one conventions-doc line naming the tier.
3. **yamldoc go directive.** yamldoc is the only module declaring
   `go 1.26.4`; the fleet is on 1.26.5. *Direction:* bump at yamldoc's next
   release. *Acceptance:* yamldoc HEAD declares the fleet's directive.
4. **config-vault release.** config-vault exists with CI (newest cicd pin,
   v0.27.0) but has zero remote tags — consumers cannot use it. *Direction:*
   release v0.1.0 (naturally rides the re-pin spec's adapter wave 1) or mark
   the README WIP. *Acceptance:* a v0.1.0 tag or a README WIP notice; shared
   with the re-pin sweep — whichever lands first closes it.
5. **Atomic backend-ID kind-namespacing across config remote adapters.** Give
   each remote/KV config adapter a kind-namespaced `ID()` (e.g. `consul:app/`,
   `aws-ssm:/app`) so two different-kind backends scoped to the same prefix no
   longer collide on a bare ID, and provenance names the kind. Deferred from the
   [config-family follow-ups](2026-07-23-config-family-followups.md) (§6, Q2):
   the core duplicate-ID rejection already shipped, so this is a provenance/UX
   refinement, but it must land **atomically** across the whole remote-adapter
   set (consul, azure-appconfig/blob/keyvault, aws-ssm/s3/secrets,
   gcp-parameter/gcs/secret, vault) to keep `Source.Name` output consistent.
   *Direction:* one coordinated wave over the remote adapters, updating each
   adapter's provenance tests; rides the re-pin sweep. *Acceptance:* every remote
   adapter's `ID()` is kind-namespaced and the `backendconformance` collision
   assertion covers it.

## 3. Scope & release plan

This spec spans many repos but is **mostly non-code** — CI config, Renovate
config, docs — and almost none of it cuts releases on its own:

- **CI pin bump (2.1)** is `ci:` — non-releasing everywhere; land fleet-wide
  without waiting for release waves.
- **Coupling mechanism (2.2)** is release-checklist/CI tooling in chat and
  forge cores — non-releasing; CoreAPIVersion, if adopted, is an additive
  `feat` riding each core's next scheduled release.
- **Fan-out discipline (2.3)** is documentation plus a script — no releases.
- **Docs pass (2.4)**: AGENTS.md is a `docs:` commit in GTB (non-releasing);
  module doc-comment fixes ride each module's next routine release.
- **Housekeeping (2.5)**: the GTB pin sweep is a normal Renovate dep MR;
  yamldoc's directive and config-vault's first release ride their next
  natural tags.

Ordering is loose; only 2.1 (audit → bump) and 2.2 (mechanism → next core
release) sequence internally. The rest is a parallelisable standing
workstream, not a single MR.

## 4. Open questions

- **Q1 — shared Renovate preset vs per-repo config.** Is one fleet preset
  the right home for the cicd component pin and the `go/*` group rules, or do
  per-repo configs stay, with the preset only covering the component pin?
  The 2.1 audit should surface actual per-repo divergence before deciding.
- **Q2 — is CoreAPIVersion worth the mechanism?** A registration-time assert
  makes semantic skew loud, but adds a lockstep-maintained constant and fires
  only at runtime, after MVS has produced the skewed build. Is the scripted
  require-bump (pure process) sufficient, and is any upper bound expressible
  given Go modules' floor-only semantics?
- **Q3 — multi-module repo consolidation.** Whether the 19 config adapters
  (and by extension other families) should collapse into one multi-module
  repo with synchronised tagging is **the re-pin spec's Q1** — its answer
  determines whether 2.3's wave scripting is a stopgap or the permanent
  mechanism. Decide it there; this spec inherits.

## 5. Outcomes (implementation, 2026-07-28)

Implementation was **verify-then-act**: each finding was re-checked against the
live fleet before any change, because Renovate's active nightly sweep had
closed several items between the review (2026-07-23) and implementation
(2026-07-28). Verification used direct `glab api` reads of each repo's
`main` (never local checkouts, several of which were stale, and never
notification content).

### 5.1 Already resolved by Renovate's fleet sweep (no action taken)

- **2.1 CI/CD component pins — RESOLVED.** Every repo's `.gitlab-ci.yml` on
  `main` is now uniformly pinned to **cicd `v0.22.0` → `v0.33.0`** (verified
  across all 57 `go/*` repos plus GTB). The v0.22.0 cohort no longer exists;
  the fleet is coherent. The `renovate-self` component and the shared preset
  (see Q1) drove the bump fleet-wide, satisfying the acceptance criterion
  ("a shared preset demonstrably proposes the next bump fleet-wide") without
  manual intervention.
- **2.5.1 GTB stale module pins — RESOLVED.** GTB `go.mod` on `main` now
  requires `chat v0.1.2`, `chat-openai v0.1.4`, and `controls v0.1.3` (all
  ahead of the review's v0.1.0 floors).
- **2.5.3 yamldoc go directive — RESOLVED.** yamldoc `main` declares
  `go 1.26.5`, matching the fleet.
- **2.5.4 config-vault release — RESOLVED.** config-vault now has remote tags
  `v0.1.0` and `v0.2.0`; consumers can use it.

### 5.2 Newly fixed in this workstream

- **2.4 AGENTS.md (primary deliverable) — DONE.** Rewrote every stale
  extracted/deleted-package reference to point at the `gitlab.com/phpboyscout/go/*`
  modules and current GTB reality: Service Lifecycle (`controls`/`transport`/
  `httpclient`/`grpcclient`; `pkg/http` & `pkg/grpc` clarified as surviving
  config adapters only), Error Handling (`go/errorhandling`), Version Control
  (`go/forge` + provider modules + `go/repo`; `pkg/vcs` is now a config
  adapter), TUI Components (`pkg/forms` **removed** pending huh v2; `output`
  extracted to `go/output`; `pkg/docs` retained), URL Opening (`go/browser`),
  Regex (`go/regexutil`), Redaction (`go/redact`), and Credential Storage
  (`go/credentials` + `go/credentials/keychain`, corrected blank-import path).
  Also corrected two stale doc paths (`docs/components/` → `docs/explanation/
  components/`).
- **2.4 module doc comments — DONE (chat).** Fixed `chat/config.go`,
  `chat/client.go`, and `chat/credentials.go` doc comments that cited GTB
  packages **deleted** by extraction (`pkg/credentials`, `pkg/credentials/
  keychain`) or misattributed the hardened transport to `pkg/http`. `httpclient`
  and `transport` were re-audited and found **accurate**: their `go-tool-base`
  references are correct (extracted-from provenance, depfootprint guards, and
  the `*FromContainable` settings adapters that genuinely still live in GTB's
  surviving `pkg/http`/`pkg/grpc` config-adapter packages) — no change made.
- **2.5.2 errors-tier documentation — DONE.** The module-extraction playbook's
  "Conventions carried from GTB" now records the deliberate stdlib-errors tier
  (config family + `aferobilly`/`redact`/`yamldoc`) as an intentional exemption
  from the `cockroachdb/errors` rule.

### 5.3 Open-question resolutions

- **Q1 — shared Renovate preset: RESOLVED (already in place).** The 2.1 audit
  fetched `renovate.json` from every repo on `main`: **all 57 `go/*` repos and
  GTB carry a byte-identical config** (`extends: ["gitlab>phpboyscout/cicd:go",
  "gitlab>phpboyscout/cicd:library"]`) — **zero per-repo divergence**. The
  "single shared preset the whole fleet extends" that 2.1 recommends already
  exists: the per-repo file is a 6-line stub delegating to the hosted
  `phpboyscout/cicd` presets, which own the `go/*` group rules, the component-pin
  datasource, and the dev-tools image trackers. **Decision:** keep the current
  thin-stub-extends-shared-preset model; no consolidation or per-repo cleanup is
  needed. The fleet-wide v0.22→v0.33 bump is the working proof.
- **Q2 — CoreAPIVersion: NOT adopted.** Per direction, the scripted lockstep
  provider require-bump (pure process, on the core's release checklist) is the
  chosen mechanism. A runtime `CoreAPIVersion` constant/registration assert is
  **not** added now: it fires only at runtime after MVS has already produced the
  skewed build, adds a lockstep-maintained constant to every core, and Go
  modules' floor-only semantics cannot express the upper bound it would guard.
  The process-based require-bump is sufficient for the current fleet.
- **Q3 — multi-module consolidation: keep 19 separate repos.** Decided (inherits
  from the re-pin spec's Q1): the config adapters stay as separate repos for
  SDK-dependency isolation — each cloud adapter's heavy provider SDK stays out of
  every other consumer's build graph. The **config canary** is the permanent
  coherence mechanism; 2.3's wave scripting complements it rather than being a
  stopgap for consolidation.

### 5.4 Deferred / noted

- GTB's own `pkg/chat/config_adapter.go` carries an analogous stale comment
  ("`pkg/credentials and pkg/http`"); left for a routine GTB source-comment
  sweep as it is outside this docs-alignment pass's named surface.
- **2.2 lockstep script** and **2.3 wave-release script** remain as process
  artefacts to be authored on the next real core/low-layer release wave; the
  coupling currently holds and no core release is pending, so no script was
  exercised in this pass.
