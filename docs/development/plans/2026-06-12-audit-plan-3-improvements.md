---
title: "Audit remediation — Plan 3: improvements & missing features"
description: "Per-package phased plan for all improvement and missing-feature findings (plus medium-severity refactor-class idiom/best-practice items) from the 2026-06-12 codebase audit, ordered by estimated effort ascending."
status: DRAFT
date: 2026-06-12
source: ../reports/codebase-audit-2026-06-12.md
---

# Audit remediation — Plan 3: improvements & missing features

**Scope rule:** every finding from the [2026-06-12 audit](../reports/codebase-audit-2026-06-12.md) with criterion **improvement or missing-feature** (any severity), plus the medium-severity **idiomatic-go / best-practices** refactor items not eligible for [Plan 1](2026-06-12-audit-plan-1-security-and-bugs.md). Low/info defects are in [Plan 2](2026-06-12-audit-plan-2-low-concerns.md).

**Ordering rationale:** phases are ordered by **estimated effort, ascending** — quick wins first, spec-requiring framework features last. Effort scale: **XS** < 1 h · **S** ≈ half a day · **M** 1–2 days · **L** multi-day, spec required. Each phase is one package (or one tightly-related group), one MR.

---

## Phase 1 — docs accuracy quick wins — **XS**

| ID | Sev | Fix |
|---|---|---|
| `stale-testing-doc-api-example` | low | Update `testing.md`'s `NewReaderContainer` example to the post-functional-options API (make it compile). |
| `stale-builtin-command-inventory` | low | Add `DoctorCmd`/`ChangelogCmd` to `docs/index.md` + CLAUDE.md; consider generating the inventory from `props.DefaultFeatures`. |
| `machine-id-irreversibility-claim-overstated` | low | Soften "anonymous/cannot be reversed" to pseudonymous; note cross-tool correlatability (or add a per-tool salt — then it's an S). |

## Phase 2 — `pkg/cmd` root conveniences — **XS–S**

| ID | Sev | Fix |
|---|---|---|
| `no-root-version-flag` | low | `rootCmd.Version = props.Version.GetVersion()` — `--version` works in every GTB tool. |
| `collector-invariant-not-upheld-for-init` | low | Default `Props.Collector` to a noop at construction (uphold the documented invariant); drop the six nil-guards. |
| `init-detection-by-use-string` | low | Replace `cmd.Use == "init"` comparisons with typed `Feature`/annotations. |
| `flag-setup-creates-config-dir-side-effect` | low | Make `GetDefaultConfigDir` pure; create the dir lazily at first write. *(Coordinate with Plan 2 Phase 1's error-return change — same function.)* |
| `completion-command-ungated-undocumented` | low | Add a `CompletionCmd` feature flag + a how-to page. |

## Phase 3 — `pkg/controls` small wiring — **S**

| ID | Sev | Fix |
|---|---|---|
| `valid-error-func-never-used` | Med | Add `WithValidError` option and consult it in the restart supervisor (exempt e.g. `http.ErrServerClosed`) — or `// Deprecated:` the dead API. Decide alongside Plan 1 Phase 10's restart-semantics spec. |
| `register-after-start-unguarded` | low | Reject/warn `Register` when controller state ≠ Unknown (mirror `RegisterHealthCheck`). |
| `healthcheck-cancel-field-dead` | info | Call `entry.cancel` on deregistration/shutdown, or remove the field. |

## Phase 4 — `pkg/setup` + `pkg/workspace` hygiene — **S**

| ID | Sev | Fix |
|---|---|---|
| `sealregistry-never-called` | low | Call `SealRegistry` alongside `setup.Seal()` — activate the inert guard. |
| `predictable-temp-file-no-cleanup` | low | Randomised temp suffix + `O_EXCL` + `defer Remove` + chmod-before-rename in the install path. |
| `direct-token-skips-credential-precedence` | low | Route the direct provider through `vcs.ResolveToken` (env-ref/keychain chain). |
| `workspace-range-loop-discarded-var` | low | Fix the discarded loop var; make `maxDepth <= 0` include the start dir. |

## Phase 5 — CI gate — **S**

| ID | Sev | Fix |
|---|---|---|
| `api-stability-gate-not-enforced-in-ci` | Med | Add a `just apidiff` target (compare against the latest tag) and a blocking MR job. Documentation already mandates it; this is enforcement only — no spec. |

## Phase 6 — `pkg/output` / `pkg/forms` / `pkg/utils` support polish — **S**

| ID | Sev | Fix |
|---|---|---|
| `duplicate-isinteractive-semantics` | low | One `IsInteractive` with explicit stream parameter; gate spinner/progress/status on the stream they write to (stderr). |
| `writer-ignores-non-json-formats` | low | Make `Writer.Write` honour YAML/CSV or return a clear unsupported-format error — not silent text. |
| `utils-grab-bag-package` | low | Move tool-specific install constants out of the framework; unexport/freeze the mutable `Instructions` map. |

## Phase 7 — `pkg/telemetry` hygiene batch — **S–M**

| ID | Sev | Fix |
|---|---|---|
| `track-methods-duplicated-event-construction` | low | Extract a shared `record()`; this duplication is how the spill inconsistency survived (pairs with Plan 1 Phase 6). |
| `otel-endpoint-parse-lacks-validation` | low | Validate scheme/host in `ParseEndpoint` (fail fast like `chat.ValidateBaseURL`). |
| `at-least-once-defeated-by-error-swallowing-backends` | low | Return transport errors from `Send` (or re-document `DeliveryAtLeastOnce` honestly). |
| `observability-setup-leaks-providers-on-partial-failure` | low | Run collected shutdowns on the error path; don't strand installed global providers. |
| `backendinfo-unsynchronized-despite-concurrency-contract` | low | Mutex/atomic for `BackendInfo`/`SetBackendInfo`. |

## Phase 8 — signing chain hygiene (`pkg/openpgpkey`, `pkg/signing`, `internal/trustkeys`) — **S–M**

| ID | Sev | Fix |
|---|---|---|
| `wkd-hash-encoder-duplicated` | low | Single shared z-base-32/WKD-hash implementation (`pkg/openpgpkey`), consumed by `pkg/setup` — wire-format-critical code must not drift. |
| `manifest-duplicate-entry-last-wins` | low | Reject duplicate manifest filenames. |
| `kms-signer-ignores-pss-opts` | low | Return an explicit error when `*rsa.PSSOptions` is requested (don't silently sign PKCS#1 v1.5). |
| `trustkeys-doc-drift-and-swallowed-walk-error` | low | Fix the "ships empty" package doc; propagate the `WalkDir` error (trust anchors must fail loud). |
| `detachsign-buffering-and-inverted-flag-name` | info | Tidy per the report note. |
| `no-signer-fingerprint-on-success` | info | Log the verifying key fingerprint on successful verification (audit trail). |

## Phase 9 — `pkg/chat` provider polish — **M**

| ID | Sev | Fix |
|---|---|---|
| `tool-handler-panic-not-recovered` | Med | `recover()` in `executeTool` and the parallel path; convert panics to tool-error strings per the documented error-as-content behaviour. |
| `settools-accumulates-stale-handlers` | low | Replace (don't merge) the handler map in `SetTools`; align the Claude-only `ResponseSchema` reset. |
| `claude-stream-empty-tool-args-invalid-json` | low | Normalise empty argBuf to `{}`. |
| `claude-system-prompt-as-user-message` | low | Use the `System` field, not a user turn. |
| `openai-hardcoded-seed-zero` | low | Make seed configurable (omit by default). |
| `no-usage-or-cost-observability` | low | Surface token usage from all three providers (return on response struct + optional telemetry event). |

## Phase 10 — transport + gateway polish — **M**

| ID | Sev | Fix |
|---|---|---|
| `missing-security-headers-middleware` | Med | Add `SecurityHeadersMiddleware` (nosniff, frame-ancestors, referrer-policy, optional HSTS/CSP); apply to the docs/openapi handlers by default. |
| `gateway-register-no-http-option-passthrough` | low | Pass HTTP server options through `gateway.Register`. |
| `response-logger-missing-unwrap` | low | Add `Unwrap()` for `http.ResponseController` compatibility. |
| `openapi-path-options-unvalidated` | low | Validate spec/docs paths (ServeMux panics on invalid patterns). |

## Phase 11 — `internal/generator` + `internal/cmd` refactors — **M**

| ID | Sev | Fix |
|---|---|---|
| `manifest-io-duplication` | low | Extract shared manifest read/encode helpers (4 sites). |
| `skeleton-data-struct-duplicated-inline` | low | Use the named `skeletonTemplateData` everywhere. |
| `unused-escape-helpers-doc-mismatch` | low | Wire `escapeShellArg`/`escapeMarkdownCodeBlock` at their documented sites or correct the docs. |
| `single-dir-tool-discards-exec-error` | low | Surface missing-binary exec errors instead of empty "lint issues found". |
| `run-vs-rune-fatal-inconsistency` | low | Standardise on `RunE` across internal commands. |
| `org-validation-silently-skipped-two-segment-repo` | info | Validate org on two-segment repo paths too. |

## Phase 12 — `pkg/credentials` wizard consolidation — **M**

| ID | Sev | Fix |
|---|---|---|
| `setup-credential-wizard-duplication` | Med | Hoist `isCI()` (×3), `keychainOpTimeout` (×4), `validateEnvVarName` (×4), storage-mode option builders (×2) into `pkg/credentials`. Security-relevant: the CI-literal-refusal logic currently lives in 4–5 drifting copies. |
| `ci-check-duplicated-across-wizards` | low | Subsumed by the hoist above. |
| `fieldschema-children-dead-api` | low | Make `FieldSchema.Children` reachable or remove it. |
| `loadenv-wrong-warn-and-nil-logger` | low | Fix the copy-pasted warning and the nil-logger panic. |
| `loadembed-overrides-caller-format` | low | Honour the caller's format instead of forcing YAML. |
| `credtest-install-parallel-unsafe` | info | Document the no-`t.Parallel` constraint. |

## Phase 13 — `pkg/setup` mocking-hook removal + error-funnel consistency — **M** (API-stability care)

| ID | Sev | Fix |
|---|---|---|
| `exported-mocking-hooks-update-cmd` | Med | Replace `ExportNewUpdater`/`ExportNewOfflineUpdater` with an `UpdateOption` (mirror `WithExecCommand`); `// Deprecated:` the vars for ≥1 minor release per the stability policy. |
| `logger-fatalf-bypasses-errorhandler` | Med | Convert setup `ai`/`bitbucket`/`github` init subcommands to `RunE` (or `ErrorHandler.Fatal`) so hints, `ExitFunc`, and the telemetry flush apply. |

## Phase 14 — `pkg/vcs` repo-layer evolution — **M–L** (small spec)

| ID | Sev | Fix |
|---|---|---|
| `repo-auth-hardcoded-to-github` | Med | Provider-aware clone/push auth via `vcs.ResolveToken`; missing auth non-fatal. Small spec — public API surface. |
| `uninitialised-repo-methods-panic` | low | Sentinel guards on all `RepoLike` methods (match `WithRepo`/`WithTree`). |
| `getsshkey-fragile-passphrase-detection-and-tui-prompt` | low | Use typed `*ssh.PassphraseMissingError`; move the `huh` prompt out of library code into the CLI layer. |
| *(deferred from Plan 2)* `repolike-kitchen-sink-weak-typing` — interface split | low | Role-interface split of the 22-method `RepoLike` (the `controls` precedent) — fold into the same spec as a v2-direction note. |

## Phase 15 — `pkg/props` provider-interface adoption — **L**

| ID | Sev | Fix |
|---|---|---|
| `props-provider-interfaces-unused` | Med | Decide once: adopt the nine narrow provider interfaces in leaf-package signatures (backward-compatible — `*Props` satisfies them), or amend the architecture docs to call them downstream-convenience-only. If adopting: mechanical but wide; do it package-by-package behind the apidiff gate (Phase 5). |

## Phase 16 — framework execution-model features — **L** (spec each)

The three remaining framework features change documented behaviour for every downstream tool — each needs its own `/gtb-spec`.

| ID | Sev | Fix |
|---|---|---|
| `no-signal-aware-execution-context` | Med | `signal.NotifyContext` + `ExecuteContext`; flows Ctrl-C through `cmd.Context()`, the telemetry flush, and update-temp cleanup. Update the skeleton main template (generator regeneration check). |
| `child-persistentprerune-silently-disables-framework-setup` | Med | Set `cobra.EnableTraverseRunHooks = true` (assess side-effects) or move bootstrap into `Execute`/outermost middleware; warn when a downstream hook would shadow bootstrap. |
| `flag-to-config-binding-unimplemented` | Med | Implement the documented flags layer: a `WithBoundFlags` binding facility applied during config load; reconcile the two contradictory precedence docs. |

| ID | Sev | Fix |
|---|---|---|
| `no-downstream-test-fixture-helper` | low | Ship a public `propstest` fixture helper (wired `Props` with noop logger/fs/collector). Natural companion to the signal/context work since downstream tests need the new execution model. |

---

## Coverage

All §3.5 improvement and §3.6 missing-feature findings, plus the four medium refactor-class items excluded from Plan 1 by criterion (`exported-mocking-hooks-update-cmd`, `props-provider-interfaces-unused`, `logger-fatalf-bypasses-errorhandler`, `tool-handler-panic-not-recovered`) and the medium missing-features (`valid-error-func-never-used`, `repo-auth-hardcoded-to-github`, `missing-security-headers-middleware`, `child-persistentprerune-silently-disables-framework-setup`, `no-signal-aware-execution-context`, `api-stability-gate-not-enforced-in-ci`, `flag-to-config-binding-unimplemented`), are assigned above. Phases 1–8 are independent quick wins; 9–14 are package refactors; 15–16 are the long-pole framework decisions.
