---
title: "Audit remediation — Plan 1: high & medium security and bugs"
description: "Per-package phased plan addressing every high/medium finding with criterion security or bug from the 2026-06-12 codebase audit. Security prioritised above bugs, except where a bug fix is itself the resolution of a security exposure."
status: DRAFT
date: 2026-06-12
source: ../reports/codebase-audit-2026-06-12.md
---

# Audit remediation — Plan 1: high & medium security and bugs

**Scope rule:** every finding from the [2026-06-12 audit](../reports/codebase-audit-2026-06-12.md) with severity **high or medium** and criterion **security or bug**. Medium findings of other criteria (idiomatic-go, best-practices, improvement, missing-feature) live in [Plan 3](2026-06-12-audit-plan-3-improvements.md); all low/info concerns live in [Plan 2](2026-06-12-audit-plan-2-low-concerns.md).

**Ordering rationale:** phases 1–8 carry security findings and run first, ordered by severity then trust-chain criticality. Phases 9–12 are bug-only clusters, ordered by severity and blast radius. Within each phase, security items are listed first; bug items whose fix closes a security exposure are flagged **[sec-resolving]** and treated with security priority.

**Workflow:** each phase is a single MR scoped to one package (one coherent conventional-commit scope). Phases marked **(spec)** require a `/gtb-spec` draft first per the development lifecycle; the rest are quick fixes. Every phase ends with `/gtb-verify`, and generator-touching phases additionally regenerate a scratch project (`just build && go run ./cmd/gtb generate project -p tmp`) to verify scaffold output.

---

## Phase 1 — `internal/generator` (spec: validation-perimeter extension)

The two highest-rated security findings in the audit. Both are perimeter gaps in an otherwise-sound escaping layer; one spec covers the validator additions since they change the documented template-security model (`docs/development/template-security.md`).

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `command-name-no-validation-path-traversal` | High | security | Add `ValidateCommandName` (kebab class; reject `/`, `.`, `..`); call from generate/remove option validation **and recursively over `m.Commands` inside `ValidateManifest`**. Closes the tampered-manifest → `filepath.Join`/`RemoveAll` traversal. |
| `signing-fields-unvalidated-unescaped-yaml-injection` | High | security | Pipe `Backend`/`KMSRegion`/`KeyID`/`PublicKey` through `escapeYAML` in `skeleton/.goreleaser.yaml`; add `ManifestSigning` validators invoked from `ValidateManifest`. Closes YAML injection into the CI-executed `signs` block. |
| `ai-doc-tools-path-traversal` | Med | security | `filepath.Rel`-containment check (or `BasePathFs`) in `handleReadFileTool`/`handleListDirTool`, matching the already-guarded `handleGoDocTool`. |
| `generator-sentinel-with-format-verb` | Med | bug | Make `ErrNotGoToolBaseProject` placeholder-free; attach the path via `errors.Wrapf` at both call sites; restores `errors.Is`. |

> Rider: the low/partial `remove-generate-command-name-path-traversal` finding is resolved by the same `ValidateCommandName` work — no separate action.

**Exit:** new validator unit tests covering traversal payloads in manifest `Commands` and `Signing`; regenerate scratch project; template-security doc updated.

## Phase 2 — `pkg/setup` (update + signing trust chain)

The self-update trust chain: two mediums weaken documented security invariants; two bugs directly undermine update correctness (a failed-but-reported-successful update leaves users on stale, potentially vulnerable binaries — flagged sec-resolving).

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `subkey-bypasses-strength-policy` | Med | security | Run `checkKeyStrength` over signing-capable subkeys (or strip them); pass explicit `*packet.Config{MinRSABits: 3072, RejectPublicKeyAlgorithms: …}` as defence-in-depth. |
| `unbounded-binary-download` | Med | security | Wrap the binary download in `io.LimitReader(rc, MaxBinaryDownloadSize+1)` with overshoot error — or switch `Update` to the already-written streaming verifier (preferred; deletes dead code). |
| `updatefromfile-bypasses-require-flags` | Med | bug **[sec-resolving]** | Honour `require_checksum`/`require_signature` in `UpdateFromFile`; fail closed when required artefacts are absent. |
| `extract-silent-success-when-binary-missing` | Med | bug **[sec-resolving]** | Return `ErrBinaryNotInArchive` on fall-through; match `<name>.exe` on Windows; fix the test that enshrines the no-op. Unbreaks Windows self-update. |
| `withauthcheck-reads-global-viper` | Med | bug | Change `WithAuthCheck` to read from `props.Config` (mirror `WithTelemetry`); deprecate the global-viper form. API-stability: additive + deprecation, no break. |
| `resolve-target-path-requires-path-and-tty` | Med | bug | LookPath failure → fall back to `os.Executable`; gate the path-mismatch prompt on a TTY check with a safe non-TTY default (abort, not proceed). |

**Exit:** integration test for archive-without-binary; cross-platform extract test naming; subkey-strength unit tests with a weak-subkey fixture.

## Phase 3 — `pkg/cmd` (root, config, docs)

Data-loss and consent hazards in the framework entry point. Two highs; the update-prompt bug is the resolution of a real security exposure (unconsented self-update).

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `gtb-install-hint-ships-to-downstream-tools` | Med | security | Replace the hardcoded `curl\|bash` GitHub hint in `pkg/cmd/docs` with `Tool`-derived metadata (`Tool.InstallHint`) or remove the line. |
| `update-prompt-failure-defaults-to-auto-update` | High | bug **[sec-resolving]** | Default `runUpdate = false`; treat any `form.Run()` error (incl. `huh.ErrUserAborted`, TTY-open failure) as "No". Fix the `EnvPrefix` CI-detection miss (`CI=true` must be honoured regardless of prefix). |
| `ensure-minimal-config-overwrites-user-config` | High | bug | Stat the target before `WriteConfigAs`; if a config exists, read-modify-write only `telemetry.enabled` (reuse `migrate.go`'s dotted-path write). |
| `flush-gate-ignores-forceenabled-and-env` | Med | bug | Gate `flushTelemetry` on the collector's resolved enabled state (`Collector.Enabled()`), not the raw config key. |
| `second-newcmdroot-panics-after-seal` | Med | bug | Make built-in middleware registration idempotent (sealed-check skip or `sync.Once`); drop test workarounds that mask it. |
| `config-set-cannot-persist-with-embedded-merge` | Med | bug | Resolve the user's real config path and read-modify-write only the requested key (atomic write, reuse migrate). |
| `nil-version-panics-in-prerun` | Med | bug | Validate `Props` at `NewCmdRoot` (clear error listing required fields) or nil-guard `props.Version` in `buildTelemetryCollector` like its siblings. |

**Exit:** regression tests for non-TTY update prompt, populated-config consent write, and double-`NewCmdRoot`.

## Phase 4 — `pkg/vcs`

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `gitlab-token-sent-to-arbitrary-asset-host` | Med | security | Attach `PRIVATE-TOKEN` only when the asset URL host matches the configured GitLab instance; same host-pin for the Gitea `Authorization` header. |
| `ghlogin-bypasses-pkg-browser` | Med | security | Set `flow.BrowseURL = browser.OpenURL`; validate hostname before URL interpolation; reconsider the hardcoded `gist` scope. |
| `createbranch-fails-on-already-up-to-date` | Med | bug | `if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate)`. Add a non-in-memory test for the Pull path. |
| `configure-ssh-auth-nil-sub-panic` | Med | bug | Nil-check after `Sub("github.ssh.key")`, fall back to `setSSHAgent`; or gate on `Has("github.ssh.key")`. |

## Phase 5 — `pkg/http` (transport)

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `client-auth-middleware-leaks-creds-on-redirect` | Med | security | Pin the allowed host (first request or explicit option); inject `Authorization` only when `req.URL.Host` matches. |
| `http-tls-start-fails-silently` | Med | bug | Load the cert / build `ServerConfig()` synchronously before spawning the serve goroutine; reject `Enabled` with empty paths. Aligns with gRPC's fail-fast. |
| `status-func-only-nil-checks` | Med | bug | Record the serve goroutine's exit error in a mutex-guarded slot; surface it from `Status()` (both transports). |
| `rate-limit-broken-under-concurrency` | Med | bug | Replace with `golang.org/x/time/rate.Limiter` (or loop the elapsed re-check under lock); correct the "token bucket" naming/doc. |

## Phase 6 — `pkg/telemetry` + `pkg/redact`

One phase: the redaction catalogue and its enforcement boundary are one functional area split over two packages.

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `metadata-values-not-redacted-at-ingest` | Med | security | Apply `redact.String` to each merged metadata value at ingest; update `docs/components/redact.md`. |
| `redact-missing-gitlab-and-aws-secret-formats` | Med | security | Add `glpat-`/`glrt-`/`gldt-`, JWT, prefix-less bearer, and JSON-form `queryCred` patterns; revisit the 40 vs 41 fuzzy threshold and fallback char class. Extend the fuzz corpus. |
| `redact-prefix-underscore-leak` | Med | bug **[sec-resolving]** | Anchor keep-length to the known literal prefix per pattern; regression tests for `sk-`/`github_pat_` with underscore bodies. |
| `collector-buffer-unbounded-when-spill-unavailable` | Med | bug | Disabled collector → true no-op; enforce the bound on spill failure (ring-buffer drop-oldest); compact already-written chunks to avoid duplicate replay. |

## Phase 7 — `internal/cmd` (keys, generate)

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `keys-generate-private-key-clobber-and-perms` | Med | security | `os.OpenFile(..., O_WRONLY\|O_CREATE\|O_EXCL, 0o600)`; fail with `--force` to override; same exists-guard for `keys mint`. |
| `add-flag-regen-drops-command-metadata` | High | bug | Populate `CommandData`/`CommandFlag` from the full manifest record (reuse the `regenerate project` mapping); register persistent flags; write back the new cmd.go hash. Test: aliases + required shorthand flag + pre-run hook survive `add-flag`. |

**Exit:** BDD scenario covering `add-flag` on a command with aliases/hooks (per the Godog strategy: CLI behaviour change ⇒ Gherkin).

## Phase 8 — repository configuration

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `renovate-automerge-no-minimum-release-age` | Med | security | Add `minimumReleaseAge: "3 days"` to `renovate.json`; confirm the shared `cicd` preset doesn't already set it; keep vuln-alert automerge but make `just vuln` blocking on those MRs. |

*Smallest phase — can be merged any time, independent of the rest.*

---

## Phase 9 — `pkg/config` (spec: container-owned watcher rewrite)

The highest-leverage bug cluster in the audit: five compounding defects in one documented feature. One spec, one rewrite.

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `observer-errs-channel-deadlock` | High | bug | Reader goroutine drains + logs observer errors; `close(errs)` after `wg.Wait()` — or change the Observable contract to return error. |
| `multi-file-hot-reload-destroys-merge` | High | bug | Container-owned fsnotify watcher over **all** files; on change re-read file[0], re-merge files[1:], validate, then notify. Never viper's single-file `WatchConfig` for merged configs. |
| `loadfilescontainer-nil-nil-contract` | High | bug | Return a sentinel error when the first file is missing (never-nil-on-nil-error); audit the github/bitbucket/ai callers. |
| `single-file-watch-never-enabled` | Med | bug | Move `watchConfig()` out of the `len(files) > 1` branch; fix the vacuous test (`observed > 0` required). |
| `reload-validation-no-rollback` | Med | bug | Validate a candidate viper before swapping (natural fit with the container-owned watcher), or snapshot last-known-good and restore on failure. |
| `observers-slice-data-race` | Med | bug | Mutex-guard `observers`/`schema`; copy under lock at callback entry. |

**Exit:** race-detector run over new watcher tests; hot-reload integration test with 2+ merged files; doc update (`docs/components/config.md`, hot-reload how-to).

## Phase 10 — `pkg/controls` (spec: supervisor lifecycle)

One coordinated spec for the supervisor/goroutine-lifecycle cluster — the fixes interlock.

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `error-handler-busy-spins-after-cancel` | High | bug | Nil the done channel after first receipt; define a real exit condition (Stopped + `errs` drained). |
| `restart-policy-restarts-clean-exit-and-sends-nil-errors` | High | bug | Distinguish clean exit / cancel / error; check `ctx.Err()` before restarting; never send nil on `errs`; reset the restart counter after a healthy interval. |
| `start-not-idempotent` | Med | bug | CAS guard (`compareAndSetState(Unknown, Running)`), early-return on failure; snapshot service count under `services.mu`. |
| `nil-start-stop-func-panics` | Med | bug | Validate in `Register` or default missing funcs to no-ops; nil-check `Stop` at all call sites. |
| `withoutsignals-leaves-notify-registered` | Med | bug | Defer `signal.Notify` until after options; `signal.Stop` in `WithoutSignals`/`SetSignalsChannel` and at shutdown. |
| `no-force-stop-despite-documented-timeout` | Med | bug | Run each StopFunc under `select` vs `ctx.Done()`; abandon over-deadline services; consider concurrent/reverse-order stop. |
| `unrun-async-check-reports-healthy` | Med | bug | Nil cached result ⇒ not-ready for readiness; or run the first async check synchronously before marking Running. |

**Exit:** goroutine-leak test (count before/after Stop), busy-spin regression (CPU/iteration counter), restart-policy table test for clean-exit/cancel/error paths; E2E controls scenarios re-run (`INT_TEST_E2E_CONTROLS=1`).

## Phase 11 — `pkg/chat` (cross-provider contract alignment)

One coherent batch: make every provider honour the documented `ChatClient` contract.

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `gemini-history-not-persisted-across-chat` | High | bug | Copy the session's accumulated turns back into `g.history` after each `Chat`/`StreamChat`/`Ask`. |
| `claude-chat-missing-default-model` | Med | bug | Resolve the default model once in `newClaude` (as `Ask` and other providers do). |
| `claude-settools-nil-parameters-panic` | Med | bug | Nil-guard `Tool.Parameters` (match OpenAI's validation). |
| `openai-unchecked-choices-index` | Med | bug | Length-guard `res.Choices` in `Ask` and `Chat` (empty-choices 200s from Ollama/vLLM). |
| `maxtokens-ignored-openai-gemini` | Med | bug | Wire `MaxCompletionTokens` / `MaxOutputTokens`; or correct the per-provider docs (prefer wiring). |
| `claude-ask-without-schema-leaves-target-untouched` | Med | bug | Collect text blocks and unmarshal/assign per the documented contract. |

**Exit:** cross-provider contract test matrix (same scenario table run against all provider fakes); doc cross-check `docs/components/chat.md`.

## Phase 12 — `pkg/output` (support)

| ID | Sev | Crit | Fix |
|---|---|---|---|
| `spinner-ctrlc-false-success` | Med | bug | `WithCancel` around the fn ctx; cancel on Ctrl-C; return `context.Canceled`/`ErrAborted`; add the missing test. |
| `table-byte-truncation-utf8` | Med | bug | Rune/width-aware measurement and truncation (`charmbracelet/x/ansi.Truncate` is already in-tree); fixes `pkg/docs/tui.go:1163` too. |

---

## Coverage

All 11 high findings and all 36 medium security/bug findings from the audit are assigned above. Medium findings excluded by criterion (handled in Plan 3): `exported-mocking-hooks-update-cmd`, `props-provider-interfaces-unused`, `logger-fatalf-bypasses-errorhandler`, `tool-handler-panic-not-recovered`, `setup-credential-wizard-duplication`, `repo-auth-hardcoded-to-github`, `child-persistentprerune-silently-disables-framework-setup`, `missing-security-headers-middleware`, `no-signal-aware-execution-context`, `api-stability-gate-not-enforced-in-ci`, `flag-to-config-binding-unimplemented`, `valid-error-func-never-used`.
