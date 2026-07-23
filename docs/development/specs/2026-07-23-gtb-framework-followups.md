---
title: "GTB framework core follow-ups: project-local config trust, bootstrap robustness, Props contract, and cleanups"
description: "Batched MEDIUM/LOW findings from the 2026-07-23 architectural review of the GTB framework core. Headline item: the project-local .<tool>.yaml layer is fully trusted and can silently downgrade update-verification and telemetry posture from a cloned repository. Also: a dedicated short timeout for the passive pre-run update check, nil-Version defaulting, hot-reload of logging config, a commit-or-prune decision on the Props provider interfaces plus constructor validation, a per-root middleware chain, the hardcoded 'GTB/als' docs-assistant identity, and stranded-remnant cleanups (Assets merge error swallowing, stale validateConfig key, feature-unaware doctor checks, dead pkg/utils symbols, pkg/vcs naming debt, feature-default dedup)."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - framework
  - props
  - bootstrap
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# GTB framework core follow-ups: project-local config trust, bootstrap robustness, Props contract, and cleanups

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (GTB-core
    section). The three HIGHs are covered by sibling specs:
    [bootstrap auxiliary-command exemptions](2026-07-23-bootstrap-auxiliary-command-exemptions.md)
    (which also absorbs the LOW `SkipUpdateCheck` Use-string and `init <provider>`
    config-gating findings),
    [pre-run prompt TTY guard](2026-07-23-prerun-prompt-tty-guard.md), and
    [version command offline degradation](2026-07-23-version-command-offline-degradation.md).
    The LOW "scaffolded telemetry auth env fallback not base64-encoded" finding is
    generator-template work, **out of scope here** — it belongs to the generator
    follow-ups spec drafted separately.

---

## 1. Context

The 2026-07-23 architectural review of the GTB framework core (`pkg/props`, `pkg/cmd`,
`pkg/docs`, internal commands) rated the post-extraction bootstrap as coherent, but
surfaced MEDIUM/LOW findings at its edges: one genuine security-posture gap (the
fully-trusted project-local config layer), robustness gaps in the pre-run/update-check
path, a contract problem in the Props container itself, and remnants from the 27-module
extraction programme. This spec batches everything below HIGH into one reviewable unit
so each item gets a recorded decision rather than living on as review-comment folklore.

## 2. Items

### 2.1 Security posture

#### 2.1.1 Project-local `.<tool>.yaml` is fully trusted — **MEDIUM** (headline)

`discoverProjectConfig` (`pkg/cmd/root/root.go:140-160`) walks from CWD up to `/` and
layers any `.<tool>.yaml` found *above* the user's own config files (`root.go:114-132`).
The resolved view feeds security-relevant resolution directly:
`resolveRequireChecksum`/`resolveRequireSignature`/`resolveKeySource`
(`pkg/setup/update.go:345-361`), `update.policy`, and `telemetry.enabled`. Cloning a
hostile repository that ships `.gtb.yaml` (or `.<anytool>.yaml`) with
`update: {require_signature: false, require_checksum: false}` silently downgrades
self-update verification for every command run inside that tree, and
`telemetry.enabled: true` flips consent the user never gave. The compiled-in release
source limits full exploitation, but posture keys must not be overridable by an
unreviewed file that arrived via `git clone`.

Direction — three candidate models, to be chosen at review:

1. **Denylist (minimal).** Strip protected keys from the project-local layer before it
   enters the store: `update.*` verification/policy keys, credential keys reachable by
   the resolution chain (`*.auth.*`, `*.api.*`), `telemetry.*` consent keys. Cheapest,
   zero-friction — but denylists rot: every future security key must remember to enrol.
2. **Allowlist (safest).** Invert it: the project-local layer may only set approved
   workflow-tuning keys (logging, output, feature toggles, downstream-registered
   non-secret keys). Fails safe for keys added later, at the cost of a registration
   surface and friction when a legitimate key isn't yet enrolled.
3. **Trust acknowledgement (direnv-style).** On first encountering a `.<tool>.yaml`
   whose content hash isn't in a per-user trust store, skip it with a one-line warning
   naming a `<tool> config trust` command; trusted files get full power, edits
   invalidate trust. Strongest and familiar from direnv/mise, but adds state, a new
   subcommand, and a first-run speed bump in every fresh clone — including CI.

A hybrid (allowlist by default, trust unlocking the rest) is also viable. Whichever
model wins, unhonoured keys must be logged as ignored at WARN, never silently dropped.
*Acceptance:* a cloned repo's `.<tool>.yaml` cannot change update-verification,
credentials, or telemetry consent without explicit user action, and ignored keys warn —
store-construction unit tests plus a hostile-repo Gherkin scenario.

### 2.2 Bootstrap robustness

#### 2.2.1 Startup update check can block any command for up to 5 minutes — **MEDIUM**

`checkForUpdates` (`pkg/cmd/root/root.go:465-495`) runs `IsLatestVersion` on
`cmd.Context()` with no dedicated deadline; the release lookup shares
`updateTimeout = 5 * time.Minute` (`pkg/setup/update.go:54-58`, `update.go:1066-1084`),
a budget sized for downloads. A captive portal or firewalled release host stalls every
command past the 24h throttle (and the never-throttled first run) for up to 5 minutes.
Direction: wrap the passive pre-run check in its own short `context.WithTimeout`
(proposed 5s, a constant beside `updateTimeout`); keep the 5-minute budget for explicit
`update` runs.
*Acceptance:* with a black-holing release source, any non-`update` command proceeds
within the short deadline; explicit `tool update` retains the full budget.

#### 2.2.2 Nil-`Version` dereference in the update-check path — **MEDIUM**

`pkg/cmd/root/root.go:490` logs `props.Version.GetVersion()` unconditionally (slog args
evaluate regardless of level), and `pkg/cmd/version/version.go:47` has the same
unguarded deref — while the surrounding guards (`shouldSkipUpdateCheck` root.go:509,
`resolveVersionString` root.go:1078-1086, `setup.NewUpdater` update.go:335-339) all
explicitly anticipate nil. A downstream tool that
hand-builds `Props` without `Version` panics once the update interval elapses.
Direction: default `props.Version` in `NewCmdRootWithOptions` the way `Collector` and
`ErrorHandler` are defaulted, and use `resolveVersionString` at both sites as
belt-and-braces (interacts with 2.3.1's constructor validation).
*Acceptance:* a `Props` built without `Version` runs the update check and
`tool version` without panicking; a regression test constructs exactly that.

#### 2.2.3 Config hot-reload does not re-apply logging configuration — **MEDIUM**

`configureLogging` runs once in the pre-run (`pkg/cmd/root/root.go:844`); `cfg.Watch`
is wired (root.go:825-835) with only `OnReloadError` — no reload hook re-reads
`log.level`/`log.format`. For long-running commands (`docs serve`, controls-based
services — the main beneficiaries of hot-reload), editing `log.level: debug` reloads
the store but the logger never changes. Direction: register a reload callback that
re-runs `configureLogging` against a fresh `View`, with the `--debug` flag still
winning.
*Acceptance:* editing `log.level` during a long-running command changes emitted
verbosity without restart; `--debug` is never downgraded by a reload.

### 2.3 Props contract

#### 2.3.1 Props is a god object; provider interfaces ornamental; no constructor — **MEDIUM**

~148 production call sites take `*props.Props` wholesale; eight of the 12 interfaces
in `pkg/props/interfaces.go` (`ConfigReader`, `VersionProvider`, `CoreProvider`, …)
have zero production consumers. There is no
constructor, so which fields may be nil is contract-by-comment — `Config` is nil until
the pre-run and permanently nil on the `init` path, and `Store.View()` is not
nil-receiver-safe, so init-reachable code touching `props.Config.View()` panics;
`ViewOrNil`/`SlogLogger` exist but nothing forces them. Direction: make an explicit
commit-or-prune decision — adopt the interfaces at the ~20 highest-traffic sites, or
delete the eight unused ones as dead API pre-1.0 (the review leans prune) — and add a
`props.New(...)`/`Validate()` making the nil-field contract checkable at construction,
called by `NewCmdRootWithOptions`.
*Acceptance:* every remaining interface has a production consumer; the blessed
construction path defaults or rejects nil required fields, with the init-path
nil-`Config` exemption documented in code.

#### 2.3.2 Process-global middleware registry binds the first root's Props forever — **MEDIUM**

`registerGlobalMiddlewareOnce` (`pkg/cmd/root/root.go:894-906`) registers
`WithRecovery`/`WithTiming`/`WithTelemetry` as closures over the first root's Props and
seals the registry (`pkg/setup/middleware.go:40-69`). A second `NewCmdRoot` in the same
process (tests, embedding) silently reuses root A's logger and collector, and any
`setup.RegisterMiddleware` call after the first root panics with no compile-time
ordering signal. Direction: move the middleware chain onto `rootState`/the command tree
(per-root); at minimum have `Chain` resolve Props from the command context.
*Acceptance:* two roots in one process each use their own Props; late
`RegisterMiddleware` works or fails at registration with a clear error — never a
mid-command panic.

#### 2.3.3 Feature-default truth is duplicated — **LOW**

`DefaultFeatures` (`pkg/props/tool.go:37-44`) and `isDefaultEnabled`
(`tool.go:256-265`) encode the same enabled-by-default set independently, with a
comment asking humans to keep them (plus `AllFeatures`) in sync. Direction: derive
`isDefaultEnabled` from `DefaultFeatures`; drop the keep-in-sync comment.
*Acceptance:* one declaration of the default set remains; a test asserts agreement for
every member of `AllFeatures`.

### 2.4 Cleanups

#### 2.4.1 Docs AI assistant hardcodes GTB's identity — **MEDIUM**

`pkg/docs/ask.go:80` starts the system prompt with `"You are a helpful assistant for
'GTB' (also known as 'als'). …"` in framework code executed by every scaffolded tool's
`docs ask` (`pkg/cmd/docs/docs.go:62-64`); "als" is a leftover from a predecessor tool,
so every downstream tool's assistant introduces itself as GTB. Direction: interpolate
the tool name (`Tool.Name`, threaded through to `Ask`) and drop the alias.
*Acceptance:* a scaffolded tool's `docs ask` prompt names that tool; neither "GTB" nor
"als" appears in framework prompt text.

#### 2.4.2 Assets structured-merge swallows malformed bundles; fs-contract violations — **LOW**

`openMergedStructured` ignores per-bundle errors (`pkg/props/assets.go:162-166` —
`processAssetFile` error → `continue`), so a broken embedded `config.yaml` silently
vanishes from merged defaults. `mergedFileInfo.Name()` returns the full path, not the
base name (`assets.go:557`), violating the `fs.FileInfo` contract; `mountedFS`
implements only `Open`, so `ReadDir`/`Glob` over a mounted prefix quietly return
nothing (`assets.go:456-471`); `Slice`, `Merge`, `For`, `Mount` have no production
callers. Direction: log skipped bundles at WARN, fix `Name()`, implement or document
the `mountedFS` limitation, trim the caller-less surface pre-1.0 (→ §3 window).
*Acceptance:* a malformed bundle logs a WARN naming it; `Name()` returns the base name;
removed symbols get migration notes.

#### 2.4.3 `validateConfig` checks the pre-forge-migration key `github.token` — **LOW**

`pkg/cmd/root/root.go:1189-1195` warns on `github.token`, but doctor and the credential
system use `github.auth.value` (`pkg/cmd/doctor/checks.go:102`) since the forge
migration — the warning can never fire against the current schema. Direction: derive
the warned key set from the credential-resolution chain (share `literalCredentialKeys`
with doctor's `credentials.no-literal` check) instead of a hardcoded list.
*Acceptance:* a literal `github.auth.value` triggers the warning; no stale
pre-migration keys remain.

#### 2.4.4 Doctor default checks are not feature-aware — **LOW**

`DefaultChecks` (`pkg/cmd/doctor/doctor.go:117-126`) always includes `checkAPIKeys`,
which warns "no AI provider API keys configured" even when `AiCmd` is disabled (the
default); `checkGit` warns whenever CWD isn't a git repo regardless of need; and
`checkGoVersion` reports the compile-time Go version, which no end user can act on.
Direction: gate the AI-key check on `AiCmd`/chat enablement, make git a check
registered by features that need it, demote or drop the Go-version check.
*Acceptance:* `doctor` on a default-features tool emits no AI-key or git warnings
unless a feature needing them is enabled.

#### 2.4.5 Dead `pkg/utils` symbols; `pkg/vcs` naming debt — **LOW**

`pkg/utils/main.go:10-54` — the `Instructions` map (kubectl/az/terraform/…) and
`GracefulGetPath` have zero callers in the repo, leftovers from a pre-extraction
internal DevOps tool; `IsInteractive` is the package's only live symbol. Separately,
`pkg/vcs` now oversells a 37-line config adapter — the review suggests renaming it
`pkg/forgecfg` at a breaking window. Direction: delete the dead symbols, consider
relocating `IsInteractive` somewhere honest, and take the rename decision explicitly
(open question 3), batching into the §3 window if approved.
*Acceptance:* `pkg/utils` contains only live symbols; the rename decision is recorded
(done with migration note, or explicitly declined).

## 3. Scope & release plan

All items are GTB-repo only; no extracted-module changes.

- **Non-breaking, ship first** (each as its own `fix`/`feat` MR as sized): 2.2.1–2.2.3,
  2.4.1, 2.4.3, 2.4.4, 2.3.3, and the logging/`Name()` halves of 2.4.2. 2.1.1 is
  behaviourally significant but additive (`feat(root)`) — it lands with its own Gherkin
  coverage and a `docs/` security note.
- **API-breaking, batch at one deliberate pre-1.0 breaking window**: the 2.3.1
  interface prune + `props.New`, the 2.3.2 registry redesign (public
  `RegisterMiddleware` semantics), the 2.4.2 Assets surface trim, and the 2.4.5
  `pkg/vcs` rename if approved. Per the pre-1.0 policy these ship as a **minor** bump
  (no `BREAKING CHANGE:` footer), each with a commit-body note and a `docs/migration/`
  entry; run `just apidiff` so the advisory CI job shows reviewers the intentional
  surface change.
- Every functional item updates `docs/components/`/`docs/concepts/` alongside the code;
  `/gtb-verify` before each MR. 2.1.1 and 2.2.3 warrant E2E/BDD scenarios (hostile-repo
  clone; live log-level reload, run with `-count=1`); the rest are unit-test territory.

## 4. Open questions

1. **Project-local trust model (2.1.1):** denylist, allowlist, or direnv-style trust
   acknowledgement (or the allowlist+trust hybrid)? Key tension: clone-and-run
   ergonomics — and CI, where a trust store is awkward — versus fail-safe coverage of
   security keys added in future releases.
2. **Commit or prune (2.3.1):** adopt the narrow provider interfaces at high-traffic
   call sites, or delete the eight unused ones? Pruning is less churn and honest about
   how the codebase actually works; committing makes the DI story real but touches
   ~20+ signatures.
3. **Is the `pkg/vcs` → `pkg/forgecfg` rename worth the churn now?** Pure naming debt
   on a 37-line adapter with a clear doc comment; deferring to the next
   already-scheduled breaking window (or the v1.0 reshuffle) may beat spending a
   migration note on it today.
