---
title: "Audit remediation — Plan 2: low/info concerns"
description: "Per-package phased plan covering every low/info-severity defect-class finding (security, bug, best-practices, idiomatic-go) from the 2026-06-12 codebase audit."
status: DRAFT
date: 2026-06-12
source: ../reports/codebase-audit-2026-06-12.md
---

# Audit remediation — Plan 2: low/info concerns

**Scope rule:** every finding from the [2026-06-12 audit](../reports/codebase-audit-2026-06-12.md) with severity **low or info** and criterion **security, bug, best-practices, or idiomatic-go**. Low/info findings whose criterion is *improvement* or *missing-feature* live in [Plan 3](2026-06-12-audit-plan-3-improvements.md). High/medium security & bugs are in [Plan 1](2026-06-12-audit-plan-1-security-and-bugs.md).

**Ordering rationale:** phases with low-severity *security* hardening first, then bug clusters by package, then doc/idiom hygiene. Everything here is a quick fix — no specs required. Phases are independent; each is one small MR with one conventional-commit scope, ended by `/gtb-verify`.

---

## Phase 1 — `pkg/setup` + signing chain hardening

| ID | Crit | Fix |
|---|---|---|
| `wkd-no-uid-filtering` | security | Filter WKD-returned entities by UID-to-email match before trusting (closes the `key_source=external` gap; composite mode already cross-checks). |
| `wkdurls-unvalidated-domain` | security | Reuse the publish-side hostname validator in `WKDURLs` before deriving URLs from the email domain. |
| `direct-version-fetch-unbounded-read` | security | Bound the version-endpoint read (`io.LimitReader`, small cap) like the sibling checksum/signature fetches. |
| `timestamps-recorded-on-failed-extract` | bug | Stamp `UpdatedKey`/check timestamps only after successful extract (≤24h reminder deferral today; cheap correctness). |
| `islatestversion-nil-error-on-empty-version` | bug | Replace `errors.WithStack(nil)` with a real error for empty/unparseable tags. |
| `getdefaultconfigdir-empty-home-fallout` | best-practices | Return `(string, error)` from `GetDefaultConfigDir` (or no-op on empty); stop writing timestamp files into cwd when HOME is unset. |
| `update-timeout-comment-mismatch` | info | Align the comment (5 min) with the value (3 min) — decide which is right for binary-download sizing while in there. |

## Phase 2 — `pkg/vcs`

| ID | Crit | Fix |
|---|---|---|
| `bitbucket-credentials-follow-api-supplied-urls` | security | Host-pin basic auth on pagination/download URLs (defence-in-depth; apiBase already pinned). |
| `getsshkey-nil-deref-on-stat-error` | bug | `if err != nil { return nil, errors.WithStack(err) }` — handle non-ENOENT Stat errors. |
| `pr-list-perpage-exceeds-github-cap` | bug | Clamp `pullRequestsPerPage` to 100; add pagination or document the cap. |
| `enterprise-upload-url-empty-when-api-overridden` | bug | Derive `url.upload` from `url.api` when unset. |
| `repolike-kitchen-sink-weak-typing` | idiomatic-go | Now: convert `var LocalRepo/InMemoryRepo` to consts and type the source state. The 22-method interface split is deliberately deferred (see Plan 3, vcs phase). |
| `ghclient-unused-cfg-field-and-mutable-repotype-vars` | idiomatic-go | Delete the dead `GHClient.cfg` field (overlap with the above consts change). |

## Phase 3 — `pkg/http` / `pkg/grpc` (transport)

| ID | Crit | Fix |
|---|---|---|
| `client-ip-trusts-spoofable-headers` | security | Trust `X-Forwarded-For`/`X-Real-IP` only behind an opt-in trusted-proxy option; log `RemoteAddr` otherwise. |
| `retry-after-not-clamped` | security | Clamp server `Retry-After` to `MaxBackoff`. |
| `http-stop-no-force-close-after-timeout` | bug | `srv.Close()` after a context-error `Shutdown` (mirror gRPC's force-stop). |
| `retry-resends-consumed-nonrewindable-body` | bug | Refuse retry in `shouldRetry` when body consumed and `GetBody == nil`. |
| `base-context-cancellation-undermines-drain` | bug | `context.WithoutCancel` for `BaseContext` (or a doc warning) so shutdown drain isn't cancelled mid-request. |
| `retry-backoff-overflow-nil-deref` | bug | Clamp before multiplying; guard `crypto/rand.Int` argument `<= 0`; validate `RetryConfig`. |

## Phase 4 — `pkg/docs` (TUI + serve)

| ID | Crit | Fix |
|---|---|---|
| `docs-serve-binds-all-interfaces` | security | Bind `127.0.0.1` by default (flag to widen); fix the `localhost` log line to print the actual bind. |
| `askai-nil-logfn-panic` | bug | Guard `logFn`/callbacks at entry per the documented "may be nil" contract. |
| `tui-performsearch-model-race` | bug | Capture `m.useRegex` by value into the search Cmd closure. |
| `tui-renderer-nil-deref` | bug (latent) | Guard `NewTermRenderer` nil return; refresh renderer on width change while there. |
| `docs-serve-bypasses-middleware-and-error-path` | best-practices | Convert the serve command to `RunE`; route through recovery/timing/telemetry middleware like every other built-in. |

## Phase 5 — `internal/agent` + `internal/cmd` CLI edges

| ID | Crit | Fix |
|---|---|---|
| `go-get-tool-argument-injection-leading-dash` | security | Reject leading `-` in the `go_get` argument regex. |
| `agent-subprocess-output-not-redacted` | security | Pipe subprocess output through `pkg/redact` before sending to AI providers (completes own-audit L-1). |
| `wkd-keyring-file-duplicates-raw-bytes-per-entity` | bug | Re-serialize each entity individually in `parseKeyFiles` instead of attaching whole-file bytes. |
| `wkd-submission-address-help-contradicts-behaviour` | bug | Implement the documented empty-means-omit behaviour (or fix the help text); prefer an explicit opt-out. |
| `sign-output-equals-input-guard-bypassed-by-path-spelling` | bug | Compare `filepath.Clean` paths / `os.SameFile` in the clobber guard. |
| `docs-deprecated-source-flag-dead` | bug | Use `MarkDeprecated` for `--source`; fix `MarkFlagsOneRequired` so a source-only invocation isn't rejected (or remove the flag). |
| `add-flag-noninteractive-skips-type-name-validation` | best-practices | Apply the interactive path's `--type`/`--name` validation in the non-interactive path. |
| `nolint-decorators-violate-project-rule` | best-practices | Restructure the six `//nolint` sites in `sign`/`agent`/`root` (or document a sanctioned-exception process in CLAUDE.md — decide once). |

## Phase 6 — `pkg/output` + `pkg/errorhandling` + small support packages

| ID | Crit | Fix |
|---|---|---|
| `markdown-table-pipe-injection` | security | Escape `\|` and newlines in `writeMarkdownRow`. |
| `errorhandler-unknown-level-swallows-error` | bug | Add a default-to-Error case in the `logError` switch. |
| `bufferlogger-level-race` | bug | Make `level` atomic (`prefix`/`fields` are construction-only). |
| `changelog-breaking-hyphen-footer` | bug | Recognise `BREAKING-CHANGE` as synonym per the conventional-commits spec. |
| `version-trimleft-cutset` | bug | `strings.TrimPrefix(version, "v")`. |
| `browser-allowedschemes-mutable` | security | Unexport `AllowedSchemes` (or make it a function returning a copy) to match the "not configurable" doc. Check API-stability tier before unexporting; if Stable, deprecate first. |

## Phase 7 — `pkg/config` + `pkg/credentials` + `pkg/cmd` residuals

| ID | Crit | Fix |
|---|---|---|
| `has-docstring-env-claim-wrong` | bug | Correct the `Container.Has` docstring (viper `InConfig` is file-only) — or make it env-aware if callers rely on it. |
| `keychain-error-wrap-payload-leak` | security (info) | Fix the misleading comment (pinned go-keyring v0.2.6 has no payload-quoting path); keep the defensive wrap. |
| `configpaths-closure-append-accumulates` | bug | `slices.Clone(configPaths)` per `PersistentPreRunE` invocation; stop aliasing the caller's slice. |
| `fatal-exit-skips-deferred-telemetry-flush` | best-practices | Flush telemetry before the fatal `ErrorHandler.Check` exits. |
| `controller-goroutines-never-terminate` | best-practices | Exit the message-processor/signal goroutines at Stop; add second-signal force-exit. *(Coordinate with Plan 1 Phase 10 — land after the controls spec.)* |
| `unsynchronized-channel-logger-setters` | best-practices | Synchronise (or forbid post-Start) the `Configurable` setters. *(Same coordination note.)* |

## Phase 8 — error-idiom and doc-accuracy sweep (cross-package)

Mechanical, repo-wide; one MR.

| ID | Crit | Fix |
|---|---|---|
| `stdlib-errors-sentinel-stragglers` | idiomatic-go | Switch `internal/generator/errors.go`, `internal/cmd/generate/errors.go`, `internal/cmd/regenerate/errors.go` to cockroachdb/errors; decide + document the `pkg/logger` exception. |
| `init-newf-drops-error-cause` | idiomatic-go | Replace the four `errors.Newf("…: %s", err)` sites with `errors.Wrap`. |
| `gemini-unchecked-type-assertion` | idiomatic-go | Comma-ok assertion on `GenaiNewClient` + wrapped error. |
| `kms-signer-context-in-struct` | idiomatic-go (info) | Document the construction-ctx lifetime constraint on `kmsSigner`. |
| `constructors-return-interfaces-vs-doc` | idiomatic-go (info) | Reconcile `interface-design.md` with the actual (reasonable) return-interface convention. |
| `update-verification-fail-open-defaults` | security (info) | No code action — documented intentional rollout posture. Re-confirm the N+3 flip-to-fail-closed milestone is still tracked in the signing rollout plan. |

---

## Coverage

35 low/info defect-class findings assigned across 8 phases. Low/info items deferred to Plan 3 by criterion (improvement / missing-feature): the §3.5 hygiene/refactor list (`predictable-temp-file-no-cleanup`, `sealregistry-never-called`, `direct-token-skips-credential-precedence`, `uninitialised-repo-methods-panic`, `getsshkey-fragile-passphrase-detection-and-tui-prompt`, `register-after-start-unguarded`, `healthcheck-cancel-field-dead`, `workspace-range-loop-discarded-var`, telemetry/signing/transport/generator/config/support/chat hygiene batches) and the §3.6 low items (`stale-testing-doc-api-example`, `stale-builtin-command-inventory`, `no-root-version-flag`, `completion-command-ungated-undocumented`, `no-downstream-test-fixture-helper`, `collector-invariant-not-upheld-for-init`, `init-detection-by-use-string`, `flag-setup-creates-config-dir-side-effect`).
