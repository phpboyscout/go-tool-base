# go-tool-base (GTB) — Comprehensive Codebase Audit

_Synthesis of subsystem reviews and cross-cutting assessments. Bug and security findings were adversarially verified; severities below reflect the verifier's adjusted calibration where one was issued. Overlapping findings reported by multiple reviewers have been deduplicated to a single canonical ID._

---

## 1. Executive Summary

**Overall verdict: the codebase is in good shape.** This is a mature, security-conscious framework with strong, documented controls — bounded downloads with the `+1`-byte idiom, constant-time hash comparison, a signature-over-raw-bytes verification chain, central `ValidateBaseURL`/`pkg/browser`/`regexutil` gates, fuzz-tested redaction, and a deliberate fail-open→fail-closed signing rollout plan. The adversarial verification step downgraded several headline "high" security findings to medium once mitigations (HTTPS clients, request timeouts, checksum-before-extract, trusted-operator inputs) were accounted for. The residual risk is concentrated, not diffuse.

The defects that genuinely matter cluster in four places: **config hot-reload** (multiple compounding bugs in a documented feature), the **controls supervisor/lifecycle** (idempotency, busy-spin, restart semantics), **chat cross-provider contracts** (the shared interface papers over real divergences), and two **unvalidated generator inputs** that bypass the otherwise-solid escaping layer.

### Top items to act on

1. **Generator validation perimeter (security, high)** — `ValidateManifest` skips the `Commands` tree and the `ManifestSigning` struct, so a tampered `.gtb/manifest.yaml` drives path-traversal file writes/`RemoveAll` (`command-name-no-validation-path-traversal`) and YAML injection into `.goreleaser.yaml` that executes in CI (`signing-fields-unvalidated-unescaped-yaml-injection`). This contradicts the documented purpose of the validation gate.
2. **Config hot-reload is broken four ways (bugs, high)** — observer error channel deadlocks the watcher (`observer-errs-channel-deadlock`), multi-file reload destroys the merged hierarchy (`multi-file-hot-reload-destroys-merge`), single-file containers never watch at all (`single-file-watch-never-enabled`), and validate-before-reject cannot hold because viper swaps config first (`reload-validation-no-rollback`). A container-owned watcher rewrite fixes most at once.
3. **Controls lifecycle bugs (bugs, high/medium)** — the error/context handler busy-spins a full core after every shutdown (`error-handler-busy-spins-after-cancel`) and the restart policy restarts cleanly-exited services while pushing nil errors (`restart-policy-restarts-clean-exit-and-sends-nil-errors`).
4. **cmd-root data-loss/consent hazards (bugs, high)** — telemetry-consent persistence can truncate a populated user config to a single key (`ensure-minimal-config-overwrites-user-config`), and a failed/aborted update prompt defaults to self-updating in non-TTY environments while reporting exit 0 (`update-prompt-failure-defaults-to-auto-update`).
5. **`LoadFilesContainer` (nil, nil) contract violation (bug, high)** — contradicts its own doc and panics first-run github/bitbucket init paths (`loadfilescontainer-nil-nil-contract`).

### Posture by criterion

- **Best practices** — Strong and consistent; rough edges are doc/code drift and a handful of `Run`+`Fatal` commands that bypass the centralized error funnel and telemetry flush.
- **Idiomatic Go** — Mostly idiomatic; the systemic gap is documented narrow provider interfaces that *nothing* consumes, plus stragglers (mocking hooks, stdlib `errors`) that violate the project's own stated rules.
- **Security** — Genuinely defense-conscious; live risk is narrow — two unvalidated generator inputs, credential-on-redirect/asset-host leaks, and redaction catalogue gaps (GitLab PATs, AWS secrets). Most original "high" items were verified down to medium.
- **Bugs** — Highest-density criterion; concentrated in config hot-reload, controls supervisor, chat provider contracts, and cmd-root telemetry/config persistence.
- **Improvements** — Healthy refactor backlog (credential-wizard dedup, manifest IO, Track-method triplication) with no architectural blockers.
- **Missing features** — A few framework conveniences expected of a "full-lifecycle" framework are absent: signal-aware execution context, `--version`, an enforced `apidiff` CI gate, and a real flag→config binding layer.

---

## 2. Findings by Severity (triage)

High and medium findings are listed in full below. Low and info findings (the majority — refactors, doc drift, latent races, hardening) are tabulated compactly in the per-subsystem appendix at the end of §3 to keep this table actionable.

### High

| ID | Subsystem | Title | Criterion |
|----|-----------|-------|-----------|
| `ensure-minimal-config-overwrites-user-config` | cmd-root | Telemetry-consent persistence can truncate an existing user config to one key | bug |
| `update-prompt-failure-defaults-to-auto-update` | cmd-root | Ignored form error defaults to silent self-update in non-interactive runs | bug |
| `gemini-history-not-persisted-across-chat` | chat | Gemini turns never written back to history — breaks multi-turn memory and Save() | bug |
| `observer-errs-channel-deadlock` | config | Hot-reload observer error channel has no reader; first reported error deadlocks the watcher | bug |
| `multi-file-hot-reload-destroys-merge` | config | Reload watches only the last file and replaces the whole merged config with its contents | bug |
| `loadfilescontainer-nil-nil-contract` | config | Returns (nil, nil) when first file missing, contradicting doc; first-run init paths nil-panic | bug |
| `error-handler-busy-spins-after-cancel` | controls | Error/context handler busy-spins at 100% CPU after shutdown | bug |
| `restart-policy-restarts-clean-exit-and-sends-nil-errors` | controls | Restart supervisor restarts cleanly-exited services and pushes nil errors | bug |
| `command-name-no-validation-path-traversal` | generator | Command names unvalidated; flow into `filepath.Join`/`RemoveAll`; manifest gate skips Commands tree | security |
| `signing-fields-unvalidated-unescaped-yaml-injection` | generator | Signing fields rendered into `.goreleaser.yaml` without `escapeYAML` or validation | security |
| `add-flag-regen-drops-command-metadata` | internal-cmd | `add-flag` regenerates cmd.go from partial data, silently dropping aliases/args/hooks/flag attrs | bug |

### Medium

| ID | Subsystem | Title | Criterion |
|----|-----------|-------|-----------|
| `unbounded-binary-download` | setup | Binary asset download unbounded; `MaxBinaryDownloadSize` + streaming verifier are dead code | security |
| `extract-silent-success-when-binary-missing` | setup | `extract` reports success when archive has no matching binary (breaks Windows self-update) | bug |
| `withauthcheck-reads-global-viper` | setup/cmd-root | `WithAuthCheck` reads global viper that GTB never populates — always fails for real keys | bug |
| `resolve-target-path-requires-path-and-tty` | setup | Update aborts when tool not on PATH; prompts interactively with no non-TTY fallback | bug |
| `exported-mocking-hooks-update-cmd` | setup/cmd-root | `ExportNewUpdater`/`ExportNewOfflineUpdater` are package-level mocking hooks (rule violation) | idiomatic-go |
| `subkey-bypasses-strength-policy` | signing | Key-strength floor enforced on primary keys only; weak signing subkeys validate | security |
| `updatefromfile-bypasses-require-flags` | signing | `UpdateFromFile` ignores `require_checksum`/`require_signature` | bug |
| `flush-gate-ignores-forceenabled-and-env` | cmd-root | `flushTelemetry` gates on config key, ignoring ForceEnabled/env overrides — drops data | bug |
| `second-newcmdroot-panics-after-seal` | cmd-root | Second `NewCmdRoot` in one process panics (registration after Seal) | bug |
| `gtb-install-hint-ships-to-downstream-tools` | cmd-root | Hardcoded GTB `curl\|bash` install hint in `pkg/cmd/docs` ships to all scaffolded tools | security |
| `nil-version-panics-in-prerun` | cmd-root | `buildTelemetryCollector` dereferences `props.Version` without the nil guard used elsewhere | bug |
| `child-persistentprerune-silently-disables-framework-setup` | cmd-root | Downstream `PersistentPreRunE` on a subcommand silently shadows root bootstrap | missing-feature |
| `config-set-cannot-persist-with-embedded-merge` | cmd-root | `config set` cannot write in the embedded-merge config GTB itself produces | bug |
| `gitlab-token-sent-to-arbitrary-asset-host` | vcs | GitLab `PRIVATE-TOKEN` attached to author-controlled asset-link hosts | security |
| `createbranch-fails-on-already-up-to-date` | vcs | `CreateBranch` returns spurious error on `git.NoErrAlreadyUpToDate` | bug |
| `ghlogin-bypasses-pkg-browser` | vcs | Device-flow opens browser via `cli/browser`, bypassing mandatory `pkg/browser.OpenURL` | security |
| `configure-ssh-auth-nil-sub-panic` | vcs | `configureSSHAuth` panics when `github.ssh` exists but `github.ssh.key` subtree absent | bug |
| `repo-auth-hardcoded-to-github` | vcs | `repo` auth bootstrap is GitHub-only; hard-fails GitLab/Bitbucket/Gitea tools | missing-feature |
| `claude-chat-missing-default-model` | chat | `Chat`/`StreamChat` send empty model when `Config.Model` unset (API 400), unlike `Ask` | bug |
| `claude-settools-nil-parameters-panic` | chat | `Claude.SetTools` panics on nil `Tool.Parameters` (OpenAI validates, Gemini tolerates) | bug |
| `openai-unchecked-choices-index` | chat | `res.Choices[0]` indexed without length check — panic on empty choices | bug |
| `maxtokens-ignored-openai-gemini` | chat | `Config.MaxTokens` silently ignored by OpenAI and Gemini | bug |
| `claude-ask-without-schema-leaves-target-untouched` | chat | `Claude.Ask` with no schema returns nil without populating target (silent data loss) | bug |
| `tool-handler-panic-not-recovered` | chat | Tool-handler panics not recovered — fatal from parallel goroutines | improvement |
| `redact-prefix-underscore-leak` | telemetry | `redactAfterPrefix` leaks up to 40 chars of `sk-` tokens containing underscores | bug |
| `redact-missing-gitlab-and-aws-secret-formats` | telemetry | No GitLab PAT / AWS-secret / bare-JWT patterns in a GitLab-first framework | security |
| `collector-buffer-unbounded-when-spill-unavailable` | telemetry | "Bounded" buffer grows unbounded when spill fails; disabled collector still buffers | bug |
| `metadata-values-not-redacted-at-ingest` | telemetry | `Event.Metadata` values bypass redaction while args/errMsg are redacted | security |
| `http-tls-start-fails-silently` | transport | HTTP TLS startup failure is async/silent; `Pair.Valid()` never checked (gRPC fails fast) | bug |
| `client-auth-middleware-leaks-creds-on-redirect` | transport | `WithBearerToken`/`WithBasicAuth` re-inject `Authorization` on cross-host redirects | security |
| `status-func-only-nil-checks` | transport | `Status()` reports healthy after the serve loop has died | bug |
| `rate-limit-broken-under-concurrency` | transport | `WithRateLimit` admits N/interval under concurrency; not the documented token bucket | bug |
| `missing-security-headers-middleware` | transport | No built-in security-headers middleware despite serving an interactive docs UI | missing-feature |
| `observers-slice-data-race` | config | Observer slice mutated without sync while the watcher goroutine iterates it | bug |
| `reload-validation-no-rollback` | config | Reload validation can't reject — viper replaces values before validation runs | bug |
| `start-not-idempotent` | controls | `Controller.Start` not idempotent despite its doc; double-start hangs `Wait()` | bug |
| `nil-start-stop-func-panics` | controls | Services without `WithStart`/`WithStop` nil-panic at start/shutdown | bug |
| `withoutsignals-leaves-notify-registered` | controls | `WithoutSignals` orphans `signal.Notify`, swallowing SIGINT/SIGTERM | bug |
| `no-force-stop-despite-documented-timeout` | controls | Shutdown timeout doesn't force-stop; one blocking StopFunc hangs `Wait()` forever | bug |
| `valid-error-func-never-used` | controls | `ValidErrorFunc` exported and documented but wired to nothing | missing-feature |
| `unrun-async-check-reports-healthy` | controls | Async health checks report OK before first execution completes (fail-open readiness) | bug |
| `ai-doc-tools-path-traversal` | generator | AI `read_file`/`list_dir` join model-supplied paths without containment | security |
| `keys-generate-private-key-clobber-and-perms` | internal-cmd | `keys generate` clobbers existing private key and inherits looser perms | security |
| `spinner-ctrlc-false-success` | support | Spinner ctrl+c returns zero/nil while fn keeps running (false success + goroutine leak) | bug |
| `table-byte-truncation-utf8` | support | Table truncation/width on byte indices corrupts multibyte UTF-8 | bug |
| `renovate-automerge-no-minimum-release-age` | cross-cutting | Auto-merge with `prCreation: immediate` and no `minimumReleaseAge` (own audit H-4) | security |
| `props-provider-interfaces-unused` | cross-cutting | Documented narrow provider interfaces are dead API — every package takes `*props.Props` | idiomatic-go |
| `generator-sentinel-with-format-verb` | cross-cutting | Sentinel contains literal `%s`; breaks `errors.Is` at one site, shows raw `%s` at another | bug |
| `setup-credential-wizard-duplication` | cross-cutting | Five-way copy-paste of credential-wizard helpers, already drifting | improvement |
| `logger-fatalf-bypasses-errorhandler` | cross-cutting | Setup init subcommands use `Logger.Fatalf`, bypassing ErrorHandler + telemetry flush | best-practices |
| `no-signal-aware-execution-context` | cross-cutting | No SIGINT/SIGTERM-aware context for CLI command execution | missing-feature |
| `api-stability-gate-not-enforced-in-ci` | cross-cutting | `apidiff` gate documented as mandatory but absent from CI/justfile | missing-feature |
| `flag-to-config-binding-unimplemented` | cross-cutting | Documented flag precedence layer has no implementation or binding helper | missing-feature |

---

## 3. Detailed Findings (by criterion)

### 3.1 Security

#### `command-name-no-validation-path-traversal` — High (confirmed)
**Location:** `internal/generator/ast.go:642-667`, `internal/generator/removal.go:43-62`, `internal/cmd/generate/command.go:231-237`, `internal/generator/validate.go:443`.
**What's wrong:** The strict `^[a-z][a-z0-9-]{0,63}$` rule is applied only to the project name. Command names get only "not `options`"/"not `root`"/"no spaces" checks, then flow unvalidated into `filepath.Join(g.config.Path, "pkg", "cmd", name)` and into `FS.RemoveAll(cmdDir)`. `filepath.Join` cleans `..`, so a crafted name escapes the project root — arbitrary directory write (generate) and recursive delete (remove). The most serious vector is `regenerate`: `ValidateManifest` validates only Properties + ReleaseSource and never walks `m.Commands`, so a tampered manifest drives writes/deletes outside the tree despite the gate whose documented purpose is to foreclose exactly this.
**Recommendation:** Add a `ValidateCommandName` (kebab class, reject `/`/`.`/`..`) and call it from generate/remove option validation **and recursively over `m.Commands` inside `ValidateManifest`**.
**Confidence:** High. Verifier note: the direct-CLI flag vector is largely self-inflicted; the manifest path is the genuine supply-chain primitive.

#### `signing-fields-unvalidated-unescaped-yaml-injection` — High (confirmed)
**Location:** `internal/generator/assets/skeleton/.goreleaser.yaml:59-67`, `internal/generator/validate.go:443-491`.
**What's wrong:** `Backend`, `KMSRegion`, `KeyID`, `PublicKey` are interpolated into double-quoted YAML scalars with **no `escapeYAML` pipe** (contrast line 2's `project_name: {{ .Name | escapeYAML }}`), and none are validated — `ValidateManifest` never touches `ManifestSigning`; `gtb enable signing` validates only KeySource/Backend. A `KeyID`/`PublicKey` with a quote+newline breaks out of the scalar and injects keys into the GoReleaser `signs` block, which runs in CI at release time.
**Recommendation:** Pipe every `Signing.*` YAML-site value through `escapeYAML`; add Signing-field validators invoked from `ValidateManifest`.
**Confidence:** High. Verifier note: realistic vector is a tampered manifest fed to `regenerate`, with CI execution as the second step.

#### `ai-doc-tools-path-traversal` — Medium (confirmed)
**Location:** `internal/generator/docs.go:613-658`.
**What's wrong:** `handleReadFileTool`/`handleListDirTool` build `filepath.Join(g.config.Path, params.Path)` from LLM-supplied paths with no containment, while the sibling `handleGoDocTool` validates its argument and even carries a `//nolint:gosec // validated input`. A prompt-injection payload in a doc-comment could exfiltrate `../../../../etc/passwd` into generated docs.
**Recommendation:** After joining, `filepath.Rel`-check the result is under `g.config.Path` (or use a `BasePathFs` rooted at the project) for both tools.
**Confidence:** Medium. Exploitation requires attacker-influenced source plus a successful injection; runs at the developer's own privilege.

#### `gitlab-token-sent-to-arbitrary-asset-host` — Medium (was high; verifier-adjusted)
**Location:** `pkg/vcs/gitlab/release.go:164-177` (also `pkg/vcs/gitea/release.go:179-181`).
**What's wrong:** `DownloadReleaseAsset` unconditionally sets `PRIVATE-TOKEN` on a request to the author-controlled asset-link URL with no host check. Aggravated by `pkg/http`'s redirect policy not stripping custom headers cross-host. A malicious/redirecting release leaks the token off-instance.
**Recommendation:** Attach `PRIVATE-TOKEN` only when the URL host matches the configured GitLab instance host; same for the Gitea `Authorization` header.
**Confidence:** High. Downgraded because the release author is already trusted to ship the executed binary, so token theft is incremental; GTB's own releases are same-instance.

#### `ghlogin-bypasses-pkg-browser` — Medium (confirmed)
**Location:** `pkg/vcs/github/login.go:27-35`.
**What's wrong:** `GHLogin` constructs an `oauth.Flow` without `BrowseURL`; `cli/oauth` defaults to `cli/browser.OpenURL`, bypassing GTB's mandated `pkg/browser.OpenURL` scheme/length/control-char validation. Wired into the setup wizard.
**Recommendation:** Set `flow.BrowseURL = browser.OpenURL`; validate the hostname before formatting it into the URL; reconsider the hardcoded `gist` scope.
**Confidence:** High. Same class as the project's prior M-5 audit item.

#### `client-auth-middleware-leaks-creds-on-redirect` — Medium (was high; verifier-adjusted)
**Location:** `pkg/http/client_middleware.go:92-116`.
**What's wrong:** `WithBearerToken`/`WithBasicAuth` set `Authorization` at the RoundTripper level on *every* hop, defeating net/http's cross-host stripping (which only governs initial-request headers). `redirectPolicy` blocks only HTTPS→HTTP downgrades. A cross-host HTTPS redirect receives the credential. Same class as python-requests CVE-2014-1829/CVE-2023-32681 and `x/oauth2`'s Transport.
**Recommendation:** Pin the allowed host (first request or explicit option); inject only when `req.URL.Host` matches.
**Confidence:** High. Downgraded: no internal GTB callers (public library API); requires a cross-host redirect.

#### `metadata-values-not-redacted-at-ingest` — Medium (confirmed)
**Location:** `pkg/telemetry/telemetry.go:120-122,209-221`; `pkg/telemetry/backend_otel.go:188-190`.
**What's wrong:** `args`/`errMsg` are unconditionally redacted (M-5 rationale: "ingest boundary is the last chance"), but the `extra` metadata map is shipped verbatim to every backend — and, unlike args/errMsg, *on every event regardless of `extendedCollection`*. The same M-5 rationale applies.
**Recommendation:** Apply `redact.String` to each merged metadata value at ingest; update `redact.md`.
**Confidence:** High. (This deduplicates the cross-cutting `telemetry-metadata-extra-not-redacted` finding; kept at medium per the dedicated subsystem review.)

#### `redact-missing-gitlab-and-aws-secret-formats` — Medium (was high; verifier-adjusted)
**Location:** `pkg/redact/redact.go:37-54`.
**What's wrong:** The prefix catalogue has four GitHub variants but no GitLab (`glpat-`/`glrt-`) — the most likely credential in a GitLab-hosted framework. 40-char AWS secret keys sit one char below the 41-char fuzzy threshold and may contain `/`/`+` the fallback class excludes; bare `Bearer eyJ...` JWTs and JSON `"api_key":"..."` also pass through. All reproduced empirically.
**Recommendation:** Add `glpat-`/`glrt-`/`gldt-` patterns, a JWT pattern, a bearer pattern without the `authorization:` prefix, a JSON-form `queryCred` variant; reconsider 40 vs 41 threshold and the fallback char class.
**Confidence:** High. Downgraded: best-effort defense-in-depth at the telemetry boundary; the dominant `--token=glpat-xxx` shape *is* caught by the key=value rule — only bare tokens slip through.

#### `unbounded-binary-download` — Medium (confirmed)
**Location:** `pkg/setup/update.go:705-736`, `pkg/setup/checksum.go:33,158`.
**What's wrong:** `DownloadAsset` does a plain `io.Copy` into memory with no cap, while the package's own `MaxBinaryDownloadSize` (512 MiB) and the streaming `VerifyChecksumFromManifestReader` exist but have **zero non-test callers**. Manifest/signature downloads are correctly bounded via `downloadBoundedAsset` — only the largest, most attacker-interesting artifact is not. Verification runs *after* the full buffer, so it can't help. (Deduplicates the cross-cutting `update-binary-download-unbounded`.)
**Recommendation:** Wrap the copy in `io.LimitReader(rc, MaxBinaryDownloadSize+1)` with an overshoot error, or switch `Update` to the already-written streaming verifier.
**Confidence:** High. DoS-only, requires hostile/compromised release server, but contradicts the package's own documented protection.

#### `subkey-bypasses-strength-policy` — Medium (confirmed)
**Location:** `pkg/setup/signing.go:174-180,242-247`.
**What's wrong:** `checkKeyStrength` runs only on `ent.PrimaryKey`; `CheckArmoredDetachedSignature` (v1 go-crypto, nil config) resolves issuers including signing-capable subkeys via `KeysByIdUsage`, applying **no** strength floor on the v1 path. A weak signing subkey validates, voiding the documented 3072-bit/Ed25519-only invariant. `TrustSet.Fingerprints()` also hashes only primary keys, so the embedded↔WKD cross-check can't detect subkey divergence.
**Recommendation:** Run `checkKeyStrength` over signing-capable subkeys (or strip them); pass an explicit `*packet.Config{MinRSABits:3072, RejectPublicKeyAlgorithms:...}` as defense-in-depth.
**Confidence:** High. Not directly attacker-exploitable against an honest key owner (binding a subkey needs the primary private key); it silently weakens a documented invariant.

#### `keys-generate-private-key-clobber-and-perms` — Medium (confirmed)
**Location:** `internal/cmd/keys/generate.go:134-145,217-240`.
**What's wrong:** `os.WriteFile` applies 0o600 only on *create* — an existing file at `privPath` keeps its looser mode. And there's no overwrite guard: re-running with the same/default `--output` silently truncates a previously generated, possibly never-backed-up private key (default paths are deterministic).
**Recommendation:** `os.OpenFile(..., O_WRONLY|O_CREATE|O_EXCL, 0o600)` and fail-with-`--force`; same exists-guard for `keys mint`.
**Confidence:** High.

#### `gtb-install-hint-ships-to-downstream-tools` — Medium (confirmed)
**Location:** `pkg/cmd/docs/docs.go:26-34`.
**What's wrong:** A hardcoded `curl -sSL https://raw.githubusercontent.com/phpboyscout/gtb/main/install.sh | bash` in the framework layer ships to every scaffolded tool — wrong product, an unsanctioned `curl|bash` trust escalation, and a stale URL (canonical home is gitlab.com, and the github path is hijackable).
**Recommendation:** Derive the hint from `Tool` metadata (`Tool.InstallHint`) or remove the `curl|bash` line.
**Confidence:** High. Error-path advisory text, nothing auto-executes.

#### `renovate-automerge-no-minimum-release-age` — Medium (own audit H-4, unremediated)
**Location:** `renovate.json:12-22`.
**What's wrong:** `prCreation: immediate`, global `automerge: true`, patch+minor auto-merge, `vulnerabilityAlerts.automerge: true`, and **no `minimumReleaseAge`** — a near-zero detection window for a hijacked upstream release.
**Recommendation:** Add `minimumReleaseAge: "3 days"`; confirm the shared `cicd` preset doesn't already; keep vuln-alert automerge but make govulncheck blocking for those MRs.
**Confidence:** Medium (cannot inspect the shared preset from this repo).

#### Lower-severity security (low/info)
- `wkd-no-uid-filtering` (low, was medium) — WKD resolver trusts every returned entity without UID-to-email filtering; mitigated in composite mode by the always-effective cross-check, so the gap is confined to `key_source=external`.
- `wkdurls-unvalidated-domain` (low) — `WKDURLs` concatenates the email domain without hostname validation; the sibling publish-side validator isn't reused. Operator-trusted config → hardening.
- `direct-version-fetch-unbounded-read` (low, was medium) — version endpoint `io.ReadAll` with no cap, unlike sibling checksum/signature fetches; time-bounded by the 30s client timeout.
- `claudelocal-prompt-in-argv` (low) — full prompt/system-prompt passed as argv to the `claude` subprocess (world-readable `/proc/.../cmdline`, ARG_MAX); pipe via stdin.
- `bitbucket-credentials-follow-api-supplied-urls` (low) — basic auth forwarded to body-supplied pagination/download URLs with no host pinning; defense-in-depth only (apiBase is hard-pinned).
- `keychain-error-wrap-payload-leak` (info, was low; partial) — comment claims go-keyring may quote payload, but the pinned v0.2.6 has no such path; the real defect is a misleading comment, not a live leak.
- `markdown-table-pipe-injection` (low) — `writeMarkdownRow` doesn't escape `|`/newlines; only in-repo caller is operator-controlled config values.
- `client-ip-trusts-spoofable-headers` (low) — access log trusts `X-Forwarded-For`/`X-Real-IP` unvalidated; used only for logging, not access control.
- `retry-after-not-clamped` (low) — server `Retry-After` used verbatim, bypassing `MaxBackoff`; bounded by request timeout.
- `docs-serve-binds-all-interfaces` (low, was medium) — docs server binds `0.0.0.0` while logging `localhost`; serves only embedded static docs.
- `browser-allowedschemes-mutable` (low, was medium) — exported mutable `AllowedSchemes` contradicts its "not configurable" doc; mutation requires first-party code that could bypass the gate anyway.
- `go-get-tool-argument-injection-leading-dash` (low) — agent `go_get` regex permits a leading `-`, allowing flag injection (no value-carrying flags due to `=` exclusion).
- `agent-subprocess-output-not-redacted` (low) — subprocess output reaches AI providers without `pkg/redact` despite the in-code follow-up note (own audit L-1, half-done). Deduplicates `agent-tool-output-redaction-followup-not-done`.
- `update-verification-fail-open-defaults` (info) — documented intentional rollout posture (`DefaultRequireChecksum/Signature = false`), recorded as residual risk; flip to true at the plan's N+3 point.

---

### 3.2 Bugs

#### `observer-errs-channel-deadlock` — High (confirmed)
**Location:** `pkg/config/container.go:247-259`.
**What's wrong:** `watchConfig` creates an unbuffered `errs` channel passed to every observer goroutine, but **nothing reads it**. The documented `AddObserverFunc` pattern (and the project's own how-to examples) send errors on it; the first send blocks forever, `wg.Done` is never reached, `wg.Wait` hangs inside viper's synchronous `OnConfigChange` callback, and *all* subsequent config-change events stall permanently. (Deduplicates the cross-cutting `observer-error-channel-deadlock`.)
**Recommendation:** Drain in a reader goroutine that logs each error and `close(errs)` after `wg.Wait()`, or change the Observable contract to return error.
**Confidence:** High. Scoped to multi-file containers (where watching activates).

#### `multi-file-hot-reload-destroys-merge` — High (confirmed)
**Location:** `pkg/config/config.go:148-157`, `pkg/config/container.go:261`.
**What's wrong:** Files are merged by repeatedly `SetConfigFile`+`MergeInConfig`, leaving viper's configFile pointing at the *last* file. Viper's `WatchConfig` watches only that file and, on change, calls `ReadInConfig()` which **replaces** the whole config map with only that file's contents — silently discarding every earlier merged layer. Documented "later files override earlier ones" is corrupted after the first reload.
**Recommendation:** Container-owned fsnotify watcher over all files that re-reads file[0] then re-merges files[1:], validates, then notifies — never rely on viper's single-file `WatchConfig` for merged configs.
**Confidence:** High.

#### `single-file-watch-never-enabled` — Medium (confirmed)
**Location:** `pkg/config/config.go:148-157`.
**What's wrong:** `watchConfig()` (and the "Loaded Config" log) sit inside the `len(configFiles) > 1` branch, so single-file containers never watch — yet docs claim "automatically enables file watching." The dedicated test passes vacuously (`observed >= 2 && ...` is false when `observed==0`). Reproduced empirically. (Deduplicates the cross-cutting `config-hot-reload-never-starts-single-file`.)
**Recommendation:** Move `watchConfig()` out of the `>1` branch and into `loadFilesContainer`; fix the test to require `observed > 0`.
**Confidence:** High.

#### `loadfilescontainer-nil-nil-contract` — High (confirmed)
**Location:** `pkg/config/config.go:84-96,108-110`.
**What's wrong:** Doc says "returns an error if the first file does not exist," but the implementation returns `(nil, nil)`. Callers written to the contract (`pkg/setup/github/github.go:702`, `pkg/setup/bitbucket/bitbucket.go:484`) check only `err != nil`, so on a fresh machine `c` is a nil `Containable` and subsequent method calls panic. `pkg/setup/ai/ai.go:497` already added an extra nil guard, evidencing the confusion.
**Recommendation:** Return a sentinel error when the first file is missing (the idiomatic never-nil-on-nil-error contract), or fix the docs and audit all callers.
**Confidence:** High.

#### `reload-validation-no-rollback` — Medium (confirmed)
**Location:** `pkg/config/container.go:238-245`.
**What's wrong:** Docs promise "invalid reloads are rejected," but viper calls `ReadInConfig()` (replacing the map) *before* `OnConfigChange`. By the time validation "rejects," every `Get*` already serves the invalid config; only observer notification is skipped — the worst of both worlds.
**Recommendation:** Snapshot last-known-good and restore on validation failure, or validate a candidate viper before swapping (pairs with the container-watcher rewrite).
**Confidence:** High.

#### `observers-slice-data-race` — Medium (confirmed)
**Location:** `pkg/config/container.go:264-277`.
**What's wrong:** `AddObserver`/`AddObserverFunc` append to `c.observers` with no mutex while the watcher goroutine (started during construction for multi-file containers) ranges over it. `GetObservers()`/`SetSchema` share the exposure.
**Recommendation:** Guard `observers`/`schema` with a mutex; copy under lock at the top of the callback.
**Confidence:** High. Scoped to multi-file containers + concurrent registration.

#### `error-handler-busy-spins-after-cancel` — High (confirmed, reproduced)
**Location:** `pkg/controls/controller.go:241-268`.
**What's wrong:** The error/context handler never disables the `ctx.Done()` select case after cancellation — it only sets a bool. Since the context is cancelled on every shutdown, the loop spins hot until process exit. Empirically reproduced burning a full core for 1s of idle after `Stop()`. The "ExitsOnCancel" test only asserts the controller reaches Stopped, not that the goroutine exits.
**Recommendation:** Set the done channel to nil after first receipt; define a real exit condition (e.g. return once Stopped and `errs` drained).
**Confidence:** High. Short window in the Stop→Wait→exit path; bites long-lived embedders and parallel test suites.

#### `restart-policy-restarts-clean-exit-and-sends-nil-errors` — High (confirmed)
**Location:** `pkg/controls/services.go:142-186`.
**What's wrong:** On `srv.Start` returning nil, control falls through `monitorHealth` straight into the restart path: `restarts++`, `errs <- nil` (logged as ERROR `error=nil`), and a restart after backoff — contradicting the documented restart triggers. For `pkg/http`, Start returns nil *immediately* (serves in a goroutine), so a restart-policy http service with no health check enters a restart loop at startup. `errors.Wrap(nil, "max restarts exceeded")` returns nil, clobbering the stored health-failure error. A successful one-shot service with `MaxRestarts==0` loops forever. `restarts` is never reset, so the count is lifetime, not "consecutive."
**Recommendation:** Distinguish clean exit / cancel / error explicitly; check `ctx.Err()` before restarting; never send nil on `errs`; reset the counter after a healthy interval.
**Confidence:** High.

#### `start-not-idempotent` — Medium (was high; verifier-adjusted)
**Location:** `pkg/controls/controller.go:186-205`.
**What's wrong:** Doc claims duplicate `Start()` calls are no-ops, but there's no CAS guard (unlike `Stop`). A second call double-starts every service (e.g. a second `ListenAndServe` on the same address) and adds an extra `wg` count that `handleStopMessage` never decrements — `Wait()` hangs forever.
**Recommendation:** Guard with `compareAndSetState(Unknown, Running)` and return early on failure; snapshot the service count under `services.mu`.
**Confidence:** High. Downgraded because single-Start usage is unaffected; the misleading doc makes misuse plausible.

#### `nil-start-stop-func-panics` — Medium (was high; verifier-adjusted)
**Location:** `pkg/controls/services.go:51,108,145,194`.
**What's wrong:** `Register` validates nothing, so `Start`/`Stop` can be nil. `runOnce`/`runWithRestartPolicy` call `srv.Start` unguarded (panic for a status-only service); `Services.stop`/`monitorHealth` call `s.Stop` unguarded (panic during shutdown, crashing the process). The status/liveness/readiness probes *are* nil-checked and recover-wrapped — the asymmetry confirms oversight. `pkg/telemetry/observability.go:80` already works around this with a dummy Start.
**Recommendation:** Validate in `Register` or default missing funcs to no-ops; nil-check `Stop` at every call site.
**Confidence:** High. Downgraded: requires developer API misuse; all in-repo registrations supply both.

#### `withoutsignals-leaves-notify-registered` — Medium (confirmed)
**Location:** `pkg/controls/controller.go:440-444,480-484`.
**What's wrong:** `NewController` calls `signal.Notify` before applying options; `WithoutSignals` sets `c.signals = nil` but the original channel stays registered, disabling default SIGINT/SIGTERM disposition — the process can't be Ctrl-C'd/SIGTERM'd. `signal.Stop` is never called anywhere. `SetSignalsChannel(custom)` has the mirror bug (new channel never `Notify`'d).
**Recommendation:** Defer `signal.Notify` until after options and only when `c.signals != nil`; call `signal.Stop` in `WithoutSignals`/replacement and at shutdown.
**Confidence:** High.

#### `no-force-stop-despite-documented-timeout` — Medium (confirmed)
**Location:** `pkg/controls/controller.go:21-23,304-310`, `pkg/controls/services.go:189-198`.
**What's wrong:** `DefaultShutdownTimeout`'s comment promises force-stop, but `Services.stop` calls each `StopFunc` sequentially and synchronously on the message goroutine; a context-ignoring StopFunc hangs `Wait()` forever. Sequential stops also share one timeout budget — the last service may get an already-expired context.
**Recommendation:** Run each StopFunc with a `select` against `ctx.Done()`, abandoning over-deadline services; consider concurrent/reverse-order stop.
**Confidence:** High. Well-behaved StopFuncs (http.Server.Shutdown) honor the context.

#### `unrun-async-check-reports-healthy` — Medium (confirmed)
**Location:** `pkg/controls/healthcheck.go:47-53`.
**What's wrong:** `toServiceStatus` returns `OK`/healthy when the cached result is nil. For an async check, `lastResult` is nil until the first run completes, so `/readyz` between Start and first completion reports a never-proven dependency as ready — a load balancer can route to it. The default 5s timeout widens the window on a slow first check.
**Recommendation:** Treat a nil result as not-ready for readiness checks, or run the first async check synchronously before marking Running.
**Confidence:** High. Narrow, self-healing window — but precisely the failure case readiness gating exists for.

#### `ensure-minimal-config-overwrites-user-config` — High (confirmed)
**Location:** `pkg/cmd/root/root.go:555-582`.
**What's wrong:** In the embedded-merge config (tools with `InitCmd` disabled, or `NewCmdRootWithConfig` with paths), the active viper has no config file, so `WriteConfig()` always fails and `ensureMinimalConfig` runs — `WriteConfigAs` truncates the user's existing `~/.toolname/config.yaml` to a single `telemetry.enabled` key. The comment says "used when no config file exists" but never verifies the precondition. Triggered exactly when the user answers the one-time telemetry prompt.
**Recommendation:** Stat the target and refuse to overwrite, or read-modify-write only the `telemetry.enabled` key into the user's real file (as `migrate.go` already does for dotted paths).
**Confidence:** High. Silent local data loss in a common downstream configuration.

#### `update-prompt-failure-defaults-to-auto-update` — High (confirmed)
**Location:** `pkg/cmd/root/root.go:300-311`.
**What's wrong:** `var runUpdate = true` then `_ = form.Run()` discards the error. With no TTY (cron/CI/pipes) or on Ctrl-C abort, `runUpdate` stays true and the binary self-updates without consent; the requested command is then skipped (`ErrUpdateComplete`) while `Execute` exits 0 — a script silently does nothing while reporting success. The `--ci` mitigation is missed by tools with an `EnvPrefix` (bare `CI=true` not picked up).
**Recommendation:** Default `runUpdate = false`; on any `form.Run()` error (including `huh.ErrUserAborted` and TTY-open failure) treat as "No."
**Confidence:** High.

#### `flush-gate-ignores-forceenabled-and-env` — Medium (confirmed)
**Location:** `pkg/cmd/root/execute.go:46-48`.
**What's wrong:** Collection is enabled by config OR `TELEMETRY_ENABLED` OR `Tool.Telemetry.ForceEnabled`, but `flushTelemetry` returns early unless `Config.GetBool("telemetry.enabled")`. The ForceEnabled "contractual" enterprise path is definitively broken (consent prompt returns early without writing the key); the env path is broken for any tool with an `EnvPrefix`. Events buffer all run and `Close` (the only OTLP-flush trigger) never fires.
**Recommendation:** Gate on the collector's resolved enabled state (`Collector.Enabled()`), or make `Close` a cheap no-op and always call it.
**Confidence:** High.

#### `second-newcmdroot-panics-after-seal` — Medium (confirmed, reproduced)
**Location:** `pkg/cmd/root/root.go:450-459`.
**What's wrong:** `registerFeatureCommands` unconditionally calls `RegisterGlobalMiddleware` + `Seal()` on every construction; the second construction panics (registration after seal). Contradicts the `rootState` re-entrancy comment; every test works around it with `ResetRegistryForTesting`. Empirically reproduced.
**Recommendation:** Make built-in middleware registration idempotent (sealed-check skip, or `sync.Once`), or move `Seal` into `Execute`.
**Confidence:** High. Canonical single-construction lifecycle is unaffected.

#### `config-set-cannot-persist-with-embedded-merge` — Medium (confirmed)
**Location:** `pkg/cmd/config/set.go:35-40`.
**What's wrong:** With the embedded-merge container, the viper has no file and no paths, so both `WriteConfig` and `SafeWriteConfig` error — `config set` is unusable in exactly the configuration GTB produces for `InitCmd`-disabled tools. Where it does succeed it serializes the whole merged state.
**Recommendation:** Resolve the user's actual config path and read-modify-write only the requested key (reuse migrate's atomic write).
**Confidence:** High.

#### `nil-version-panics-in-prerun` — Medium (confirmed)
**Location:** `pkg/cmd/root/root.go:591,616,626,651`.
**What's wrong:** `buildTelemetryCollector` (in `PersistentPreRunE`) calls `props.Version.GetVersion()` unguarded on all paths, while `shouldSkipUpdateCheck` and `doctor` guard `Version != nil`. A downstream tool that hand-constructs `Props` without `Version` panics on every command; recovery middleware wraps RunE, not PreRunE.
**Recommendation:** Resolve the version once with a nil check, or validate `Props` at `NewCmdRoot` with a clear error listing required fields.
**Confidence:** High. No in-repo path triggers it (the scaffold always sets a value-type `Version`).

#### `getsshkey-nil-deref-on-stat-error` — Low (was medium; verifier-adjusted)
**Location:** `pkg/vcs/repo/repo.go:288-295`.
**What's wrong:** Only `os.IsNotExist` is handled; any other `Stat` error leaves `fileHandle` nil and the following `IsDir()` panics.
**Recommendation:** `if err != nil { return nil, errors.WithStack(err) }`.
**Confidence:** High. Downgraded: uncommon non-ENOENT failure, would have errored anyway.

#### `createbranch-fails-on-already-up-to-date` — Medium (confirmed)
**Location:** `pkg/vcs/repo/repo.go:241-250`.
**What's wrong:** `tree.Pull` returns `git.NoErrAlreadyUpToDate` as a non-nil error in the common up-to-date case, which `CreateBranch` wraps as a failure. The project's own integration test demonstrates go-git surfaces this sentinel for Push.
**Recommendation:** `if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate)`.
**Confidence:** High. The existing test uses in-memory repos, so the buggy Pull path is untested.

#### `configure-ssh-auth-nil-sub-panic` — Medium (confirmed, reproduced)
**Location:** `pkg/vcs/repo/repo.go:610-619`.
**What's wrong:** `NewRepo` gates on `Has("github.ssh")` (true for a scalar like `github.ssh: true`), then `configureSSHAuth` calls `Sub("github.ssh.key")` which returns nil and `sshCfg.GetString("type")` panics. Reproduced with `github.ssh: true`.
**Recommendation:** Nil-check after `Sub` and fall back to `setSSHAgent`, or gate on `Has("github.ssh.key")`.
**Confidence:** High.

#### `extract-silent-success-when-binary-missing` — Medium (confirmed)
**Location:** `pkg/setup/update.go:747-765`.
**What's wrong:** `extract` matches `header.Name == s.Tool.Name` exactly and `return nil` on fall-through. Both callers then report success (logs "Update complete", `Updated: true`, stamps timestamps) while the old binary stays. GoReleaser names the Windows inner binary `<name>.exe`, so the asset lookup succeeds but extract never matches — **Windows self-update is a permanent silent false-success.** The offline test enshrines the no-op.
**Recommendation:** Return `ErrBinaryNotInArchive` on fall-through; match `<name>.exe` on Windows; fix the test.
**Confidence:** High.

#### `resolve-target-path-requires-path-and-tty` — Medium (confirmed)
**Location:** `pkg/setup/update.go:639-665`.
**What's wrong:** `exec.LookPath(Tool.Name)` failure is fatal even though `os.Executable` already gave a valid path, so `./mytool` or off-PATH execution can't self-update. When paths differ (raw compare, no symlink normalization), an unconditional `huh.NewSelect` prompt runs with no TTY/CI fallback.
**Recommendation:** Treat LookPath failure as non-fatal (fall back to `os.Executable`); gate the prompt on a TTY check.
**Confidence:** High.

#### `withauthcheck-reads-global-viper` — Medium (confirmed)
**Location:** `pkg/setup/middleware_builtin.go:101-117`.
**What's wrong:** Reads `viper.GetString(key)` on the package-global singleton, which GTB never populates (all config is per-instance `viper.New()`). Any downstream `WithAuthCheck("github.token")` always fails, and the error's suggested `config set` fix writes where the check never looks. The only production call site passes zero keys, so it's latent but broken-as-shipped.
**Recommendation:** Change the signature to take `*props.Props`/`ConfigProvider` and read from `props.Config` (mirror `WithTelemetry`); deprecate the old form.
**Confidence:** High. (Reported by two reviewers; single canonical finding.)

#### chat provider-contract bugs — Medium (all confirmed)
The shared `ChatClient` interface masks real divergences:
- `gemini-history-not-persisted-across-chat` (**High**) — `Chat`/`StreamChat`/`Ask` create a throwaway session from `g.history` and never write turns back; a second `Chat()` forgets the first and `Save()` marshals an empty conversation. Claude/OpenAI append correctly. **Fix:** copy the session's accumulated turns back into `g.history` after each call.
- `claude-chat-missing-default-model` — `Chat`/`StreamChat` send `anthropic.Model(c.cfg.Model)` with no fallback (API 400), unlike `Ask` and unlike OpenAI/Gemini which default at construction; the built-in `docs ask` feature hits this. **Fix:** resolve the default once in `newClaude`.
- `claude-settools-nil-parameters-panic` — `Claude.SetTools` dereferences `t.Parameters` unconditionally; OpenAI validates, Gemini tolerates. **Fix:** add the same nil guard.
- `openai-unchecked-choices-index` — `res.Choices[0]` indexed without a length check; OpenAI-compatible backends (Ollama/vLLM) can 200 with empty choices → panic. **Fix:** length guard in `Ask` and `Chat`.
- `maxtokens-ignored-openai-gemini` — `Config.MaxTokens` honored only by Claude; OpenAI/Gemini never set the cap, contradicting documented per-provider defaults. **Fix:** wire `MaxCompletionTokens`/`MaxOutputTokens` or correct the docs.
- `claude-ask-without-schema-leaves-target-untouched` — with no `ResponseSchema`, `Claude.Ask` returns nil without populating target (silent data loss); deterministic, not edge-case. **Fix:** collect text blocks and unmarshal/assign per the documented contract.

#### `redact-prefix-underscore-leak` — Medium (was high; verifier-adjusted)
**Location:** `pkg/redact/redact.go:109-115`.
**What's wrong:** `redactAfterPrefix` keeps everything up to the *last* underscore, before the first-hyphen check — so `sk-...` tokens with underscores in the body leak the prefix-through-last-underscore. Reproduced: `sk-proj-abc123_SECRETBODYPART_***`.
**Recommendation:** Anchor the keep-length to the known literal prefix per pattern; add regression tests for `sk-`/`github_pat_` with underscores.
**Confidence:** High. Downgraded: the 41-char `longOpaqueTokenPattern` hard-caps leakage at 40 chars and fully redacts standard-length modern keys; GitHub fine-grained PAT secrets are always fully redacted.

#### `collector-buffer-unbounded-when-spill-unavailable` — Medium (confirmed)
**Location:** `pkg/telemetry/telemetry.go:84-141`, `pkg/telemetry/spill.go:24-53`.
**What's wrong:** `spillToDisk` early-returns *without clearing the buffer* when `dataDir == ""`, on marshal failure, or on chunk-write failure — so the "bounded" buffer grows without bound. The disabled collector has `dataDir == ""` and a non-nil noop backend, so `Track` still allocates and appends despite documented no-op semantics — a slow leak in long-running services. Partial chunk-write failure also duplicates events on the next spill.
**Recommendation:** Make the disabled collector a true no-op (`enabled` flag or nil backend); enforce the bound even on failure (ring-buffer drop-oldest); compact already-written chunks.
**Confidence:** High. Nil impact for one-shot CLI (flushed at exit); real for long-running services.

#### transport bugs — Medium/Low (confirmed)
- `http-tls-start-fails-silently` (**Medium**, was high) — HTTP `start()` never checks `tlsPair.Valid()`/loads the cert before returning; `ServeTLS` fails async in a goroutine while `/healthz` and the controller report healthy. gRPC loads TLS synchronously and fails fast. **Fix:** load the cert (or `ServerConfig()`) synchronously before spawning the serve goroutine; reject `Enabled` with empty paths. Downgraded: the failed server hosts `/healthz` itself, so external probes get connection-refused.
- `status-func-only-nil-checks` (**Medium**) — both transports' `Status()` only check `srv == nil` (invariantly false), so a dead serve loop reports healthy indefinitely. **Fix:** record the serve goroutine's exit error in a guarded slot read by `Status`.
- `rate-limit-broken-under-concurrency` (**Medium**) — N concurrent goroutines read the same `lastRequest`, sleep in parallel, then all proceed at once (N/interval); no re-check after re-locking. Mislabeled "token bucket." **Fix:** use `x/time/rate.Limiter` or loop the elapsed check.
- `http-stop-no-force-close-after-timeout` (low, was medium) — `http.Stop` logs the Shutdown error but never calls `srv.Close()`, unlike gRPC's force-stop; leaks connections past the deadline. Limited blast radius (process usually exits). **Fix:** `srv.Close()` after a context-error Shutdown.
- `retry-resends-consumed-nonrewindable-body` (low, was medium) — retry re-sends a consumed body when `GetBody == nil`. **Fix:** refuse to retry non-rewindable bodies in `shouldRetry`.
- `base-context-cancellation-undermines-drain` (low, partial) — `BaseContext` tied to the construction ctx can cancel in-flight requests at shutdown *if* a caller passes the controller context; no in-repo wiring does. **Fix:** `context.WithoutCancel` or a doc warning.
- `retry-backoff-overflow-nil-deref` (low, partial) — `MaxRetries >= 36` or negative `InitialBackoff` overflows the duration and `crypto/rand.Int` panics (`<= 0`). Caller-reachable via unvalidated `RetryConfig`. **Fix:** clamp before multiplying, guard `<= 0`.

#### `add-flag-regen-drops-command-metadata` — High (confirmed)
**Location:** `internal/cmd/generate/flag.go:208-238`.
**What's wrong:** `regenerateCommand` builds `CommandData`/`CommandFlag` from a partial manifest projection, dropping Aliases, Hidden, Args, PersistentPreRun/PreRun, MutuallyExclusive, RequiredTogether, initializers, and per-flag Shorthand/Default/Required/Hidden. Worse: persistent flags go into `data.Flags` but `addFlagsToCommand` skips `Persistent==true` entries there, so persistent flags aren't registered at all; and the new cmd.go hash is never written back, causing a spurious manual-modification conflict next regeneration. Manifest data survives (so `regenerate project` restores it), but silently until then.
**Recommendation:** Populate `CommandData`/`CommandFlag` from the full manifest record (reuse the `regenerate project` mapping). Add a test with aliases + required shorthand flag + pre-run hook.
**Confidence:** High.

#### `generator-sentinel-with-format-verb` — Medium (confirmed)
**Location:** `internal/generator/errors.go:6`, `generator.go:148`, `internal/cmd/generate/flag.go:147`.
**What's wrong:** `ErrNotGoToolBaseProject` embeds a literal `%s`. One call site concatenates the text into a new `Newf` without `%w` (breaks `errors.Is`); the other wraps with `%w` but supplies no path, rendering literal `at '%s'` plus a duplicated suffix.
**Recommendation:** Make the sentinel placeholder-free; attach the path via `errors.Wrapf` at call sites.
**Confidence:** High.

#### `spinner-ctrlc-false-success` — Medium (was high; verifier-adjusted)
**Location:** `pkg/output/spinner.go:102-107,137-144`.
**What's wrong:** Ctrl-C quits the TUI without cancelling the context or recording an error, so `Spin`/`SpinWithResult` return `(zero, nil)` while the fn goroutine leaks. In-repo consequences: the version-check branch proceeds as "outdated," and `docs ask` returns an empty answer treated as success.
**Recommendation:** Wrap the ctx with `WithCancel`, cancel on Ctrl-C, return `context.Canceled`/`ErrAborted`; add a test (none exists).
**Confidence:** High. Downgraded: in-repo impact is non-destructive; the "critical op reports success" case is a public-API hazard for downstream users.

#### `table-byte-truncation-utf8` — Medium (confirmed)
**Location:** `pkg/output/table.go:290-296,331-338,522-527` (and `pkg/docs/tui.go:1163`).
**What's wrong:** Cell truncation and width calc operate on byte indices, cutting runes mid-sequence (invalid UTF-8) and misaligning columns for non-ASCII/CJK/emoji data — compounded because `%-*s` padding counts runes.
**Recommendation:** Use `go-runewidth` (or `charmbracelet/x/ansi.Truncate`, already in the tree) for measurement and truncation.
**Confidence:** High. Display-correctness only.

#### Lower-severity bugs (low/info)
- `timestamps-recorded-on-failed-extract` (low, partial) — markers stamped before extract; `UpdatedKey` is never read in non-test code, so impact is a ≤24h reminder deferral, not the claimed silent "freshly updated" report.
- `islatestversion-nil-error-on-empty-version` (low) — `errors.WithStack(nil)` returns nil for an empty tag, so the update proceeds against an unparseable release; rare trigger.
- `errorhandler-unknown-level-swallows-error` (low, was medium) — `logError` switch has no default; an unrecognized level string drops the error. In-repo callers all use the constants. **Fix:** add a default-Error case.
- `askai-nil-logfn-panic` (low, was medium) — `AskAI` calls `logFn` unconditionally despite "either callback may be nil"; deterministic first-call panic, discovered immediately. **Fix:** guard at entry.
- `tui-renderer-nil-deref` (info, partial) — unreachable with pinned glamour v1.0.0 (`NewTermRenderer` can't return nil there); latent + a mild stale-width cosmetic issue.
- `has-docstring-env-claim-wrong` (low) — `Container.Has` doc claims env-var awareness but `viper.InConfig` searches only file config; misleading docstring.
- `bufferlogger-level-race` (low, partial) — only `level` is a real race (`prefix`/`fields` are construction-only); latent, untriggered today. **Fix:** atomic `level`.
- `tui-performsearch-model-race` (low) — search Cmd closure reads `m.useRegex` from a goroutine while Update mutates it; worst case is one search with the wrong mode. **Fix:** capture by value.
- `changelog-breaking-hyphen-footer` (low) — `BREAKING-CHANGE` (hyphenated) footer not recognized as synonymous with `BREAKING CHANGE`.
- `version-trimleft-cutset` (low) — `TrimLeft(version, "v")` strips all leading `v`s; canonical `TrimLeft`/`TrimPrefix` misuse.
- `pr-list-perpage-exceeds-github-cap` (low) — `pullRequestsPerPage = 300` (GitHub clamps 100), single List, no pagination; Head filter makes a miss unlikely.
- `enterprise-upload-url-empty-when-api-overridden` (low) — `url.api` set without `url.upload` yields a host-less upload URL; GTB itself never uses UploadURL.
- `wkd-keyring-file-duplicates-raw-bytes-per-entity` (low) — `parseKeyFiles` attaches the whole file's bytes per entity, duplicating packets / cross-publishing for ring files; one-key-per-file (the tested path) is correct.
- `wkd-submission-address-help-contradicts-behaviour` (low) — help says empty means "do not emit," but the code always defaults to the first email; no opt-out.
- `sign-output-equals-input-guard-bypassed-by-path-spelling` (low) — refuse-to-overwrite guard uses raw string compare; `./file` vs `file` bypasses it. **Fix:** compare `filepath.Clean`/`os.SameFile`.
- `docs-deprecated-source-flag-dead` (low) — `--source` is unusable because `MarkFlagsOneRequired(command,package)` rejects a source-only invocation; not `MarkDeprecated`.

---

### 3.3 Best practices

- `logger-fatalf-bypasses-errorhandler` (**Medium**) — setup `ai`/`bitbucket`/`github` init subcommands use `Run` + `Logger.Fatalf` (charm `os.Exit`), skipping cockroachdb hints/help channel, the injectable `ExitFunc`, and the deferred telemetry flush. **Fix:** convert to `RunE` or use `ErrorHandler.Fatal`.
- `fatal-exit-skips-deferred-telemetry-flush` (low, was medium) — `Execute`'s fatal `ErrorHandler.Check` calls `os.Exit`, skipping `defer flushTelemetry`; drops generic failing-command events (telemetry-only, recovered next run). **Fix:** flush before the fatal Check.
- `docs-serve-bypasses-middleware-and-error-path` (low) — the only built-in command using `Run`+`ErrorHandler.Fatal` instead of `RunE`; no recovery/timing/telemetry middleware.
- `nolint-decorators-violate-project-rule` (low) — six `//nolint` in `sign`/`agent`/`root` production code against CLAUDE.md's "never add `//nolint`." Restructure or document a sanctioned-exception process.
- `getdefaultconfigdir-empty-home-fallout` (low) — `GetDefaultConfigDir` swallows `UserHomeDir`/`MkdirAll` errors and returns `""`, so timestamp files land in cwd when HOME is unset. **Fix:** return `(string, error)` or no-op when empty.
- `update-timeout-comment-mismatch` (info) — comment says 5 min, value is 3 min; matters for sizing the binary-download timeout.
- `add-flag-noninteractive-skips-type-name-validation` (low) — `--type`/`--name` unvalidated in the non-interactive path; bad values corrupt the manifest.
- `controller-goroutines-never-terminate` (low) — message processor / signal handler never exit; per-controller 2-3 goroutine leak (pairs with the busy-spin finding); no second-signal force-exit.
- `unsynchronized-channel-logger-setters` (low) — `Configurable` setters mutate channels/logger read by controller goroutines after Start, with no synchronization.

---

### 3.4 Idiomatic Go

- `props-provider-interfaces-unused` (**Medium**) — the nine documented narrow provider interfaces (`LoggerProvider`, `ConfigProvider`, …) have **zero consumers**; every signature takes concrete `*props.Props`. Documented-architecture drift. **Fix:** either adopt them in leaf packages (backward-compatible — `*Props` satisfies them) or amend the docs to call them downstream-convenience-only.
- `exported-mocking-hooks-update-cmd` (**Medium**) — `ExportNewUpdater`/`ExportNewOfflineUpdater` are exported package-level mutable function hooks ("Tests may replace this"), the exact pattern CLAUDE.md forbids; they're in the API-stability surface. **Fix:** inject via an `UpdateOption` (mirror `WithExecCommand`); deprecate the vars. (Reported by two reviewers; single canonical finding.)
- `init-newf-drops-error-cause` (low) — four `init.go` sites use `errors.Newf("...: %s", err)` instead of `Wrap`, losing the cause chain and `errors.Is`/hints.
- `repolike-kitchen-sink-weak-typing` (low) — 22-method `RepoLike` interface, untyped `int` source state, `type RepoType = string` alias (no type safety), and mutable `var LocalRepo/InMemoryRepo`. **Fix:** consts now; plan a v2 role-interface split (the controls precedent).
- `ghclient-unused-cfg-field-and-mutable-repotype-vars` (info) — dead `GHClient.cfg` field + the same mutable `RepoType` vars (overlaps the above).
- `stdlib-errors-sentinel-stragglers` (low) — `internal/generator/errors.go`, `internal/cmd/generate/errors.go`, `internal/cmd/regenerate/errors.go`, `pkg/logger` use stdlib `errors`/`fmt.Errorf` against the cockroachdb/errors rule. **Fix:** switch, or document the `pkg/logger` exception. (Deduplicates `stdlib-errors-in-generate-errors`.)
- `kms-signer-context-in-struct` (info) — `kmsSigner` stores construction-time ctx (forced by `crypto.Signer.Sign` having no ctx param); document the lifetime constraint.
- `gemini-unchecked-type-assertion` (low) — `GenaiNewClient.(func(...))` without comma-ok panics on a wrong signature; use comma-ok + wrapped error.
- `constructors-return-interfaces-vs-doc` (info) — ~20 constructors return interfaces while `interface-design.md` calls that an anti-pattern; reconcile the doc to the actual (reasonable) convention.

---

### 3.5 Improvements

- `tool-handler-panic-not-recovered` (**Medium**) — `executeTool` runs caller handlers (with model-generated, adversarial input) with no `recover()`; in `executeToolsParallel` a panic in a bare goroutine is fatal. **Fix:** recover and convert to a tool-error string, matching the documented error-as-content behavior.
- `setup-credential-wizard-duplication` (**Medium**) — `isCI()` (×3), `keychainOpTimeout` (×4), `validateEnvVarName`/regex (×4), storage-mode option builders (×2) copy-pasted across four setup packages and **already drifting**. Security-relevant (CI-literal-refusal logic lives in 4-5 places). **Fix:** hoist into `pkg/credentials`.
- `repo-auth-hardcoded-to-github` (**Medium**, missing-feature) — `NewRepo` auth is GitHub-only and hard-fails non-GitHub tools; release providers have a clean multi-provider abstraction but git clone/push auth has no provider dimension. **Fix:** provider-aware auth via `vcs.ResolveToken`; make missing auth non-fatal.
- `predictable-temp-file-no-cleanup` (low) — install temp file uses a fixed `targetPath+"_"` name via `Create` (no `O_EXCL`, no cleanup on failure); concurrent updates race. **Fix:** randomized suffix + `defer Remove` + chmod-before-rename.
- `sealregistry-never-called` (low) — `SealRegistry` exists but has no production caller; the feature-registry seal guard is inert. **Fix:** call it alongside `setup.Seal()`.
- `direct-token-skips-credential-precedence` (low) — direct provider reads only `direct.token`/`DIRECT_TOKEN`, bypassing the `vcs.ResolveToken` env-ref/keychain chain. **Fix:** route through `ResolveToken`.
- `uninitialised-repo-methods-panic` (low) — most `RepoLike` methods deref `r.repo`/`r.tree` directly and panic if not opened, while `WithRepo`/`WithTree` have the proper sentinel guards. **Fix:** add the guards everywhere.
- `getsshkey-fragile-passphrase-detection-and-tui-prompt` (low) — passphrase detection by error-string match (use the typed `*ssh.PassphraseMissingError`) plus a `huh` prompt inside library code (move to the CLI layer).
- `valid-error-func-never-used` (**Medium**, missing-feature) — `ValidErrorFunc` is documented but wired to nothing, so the restart supervisor can't exempt expected errors (e.g. `http.ErrServerClosed`). **Fix:** add the `WithValidError` option + consult it, or `// Deprecated:` it.
- `register-after-start-unguarded` (low) — `Register` accepts services after Start (never started, but stopped), unlike `RegisterHealthCheck`. **Fix:** reject/warn when not Unknown.
- `healthcheck-cancel-field-dead` (info) — `entry.cancel` stored but never called.
- `workspace-range-loop-discarded-var` (low) — discarded loop var + `maxDepth <= 0` silently skips even the start dir.
- redaction/telemetry hygiene: `at-least-once-defeated-by-error-swallowing-backends` (low) — `DeliveryAtLeastOnce` is meaningless for HTTP/OTel (Send always returns nil); document or return the transport error. `observability-setup-leaks-providers-on-partial-failure` (low) — installed global providers stranded when a later signal builder fails; run collected shutdowns on the error path. `backendinfo-unsynchronized` (low) — `BackendInfo`/`SetBackendInfo` unsynchronized despite the "safe for concurrent use" contract. `machine-id-irreversibility-claim-overstated` (low, partial) — docs call the hashed machine ID "anonymous"/"cannot be reversed"; it's pseudonymous and cross-tool-correlatable (no per-tool salt) — soften the docs. `track-methods-duplicated-event-construction` (low) — three near-identical Track methods (this is *how* the spill inconsistency survived); extract a shared `record`. `otel-endpoint-parse-lacks-validation` (low) — `ParseEndpoint` accepts empty hosts/arbitrary schemes; fail fast like `chat.ValidateBaseURL`.
- signing hygiene: `manifest-duplicate-entry-last-wins` (low) — duplicate manifest filenames silently resolve last-wins; reject. `wkd-hash-encoder-duplicated` (low) — z-base-32/WKD-hash implemented twice (`pkg/setup` and `pkg/openpgpkey`); a wire-format-critical hash must not drift — share one implementation. `kms-signer-ignores-pss-opts` (low) — `kmsSigner.Sign` silently signs PKCS#1 v1.5 when `*rsa.PSSOptions` is requested (fail-safe but a silent contract violation in an exported `crypto.Signer`). `trustkeys-doc-drift-and-swallowed-walk-error` (low) — package doc says "ships empty" but ships keys; `Keys()` swallows the WalkDir error (wrong failure direction for trust anchors). `detachsign-buffering-and-inverted-flag-name` (info), `no-signer-fingerprint-on-success` (info — surface the verifying key fingerprint for audit).
- transport: `gateway-register-no-http-option-passthrough` (low), `response-logger-missing-unwrap` (low — add `Unwrap()` for `http.ResponseController`), `openapi-path-options-unvalidated` (low — validate spec/docs paths, ServeMux panics on invalid patterns).
- generator/internal-cmd: `unused-escape-helpers-doc-mismatch` (low — `escapeShellArg`/`escapeMarkdownCodeBlock` documented as in-use, no call site), `skeleton-data-struct-duplicated-inline` (low — 20-field anon struct duplicated vs the named `skeletonTemplateData`), `manifest-io-duplication` (low — manifest read/encode boilerplate in 4 methods), `single-dir-tool-discards-exec-error` (low — missing-binary surfaces as empty "lint issues found"), `run-vs-rune-fatal-inconsistency` (low), `org-validation-silently-skipped-two-segment-repo` (info), `remove-generate-command-name-path-traversal` (low, partial — the single-step `remove --name ../..` exploit is refuted by the manifest membership check; the generate file-write traversal is real, covered by `command-name-no-validation-path-traversal`).
- config/credentials: `fieldschema-children-dead-api` (low — `FieldSchema.Children` exported/documented but unreachable), `loadenv-wrong-warn-and-nil-logger` (low — copy-pasted warning + nil-logger panic), `loadembed-overrides-caller-format` (low — silently forces YAML), `ci-check-duplicated-across-wizards` (low — overlaps the wizard-dedup finding), `credtest-install-parallel-unsafe` (info — document the no-`t.Parallel` constraint).
- support: `duplicate-isinteractive-semantics` (low — two `IsInteractive` with different semantics; spinner/progress/status gate on stdout but write to stderr), `writer-ignores-non-json-formats` (low — `Writer.Write` silently treats YAML/CSV/etc. as text), `utils-grab-bag-package` (low — tool-specific install constants + exported mutable `Instructions` map in a framework package).
- chat: `settools-accumulates-stale-handlers` (low — `SetTools` merges into the handler map instead of replacing, plus a Claude-only `ResponseSchema` reset), `claude-stream-empty-tool-args-invalid-json` (low — empty argBuf yields invalid JSON on the next request; normalize to `{}`), `claude-system-prompt-as-user-message` (low — Claude sends SystemPrompt as a user turn instead of the `System` field), `openai-hardcoded-seed-zero` (low — unconfigurable `Seed: 0`), `no-usage-or-cost-observability` (low — token usage discarded by all three providers).

---

### 3.6 Missing features

- `child-persistentprerune-silently-disables-framework-setup` (**Medium**) — Cobra runs only the closest `PersistentPreRunE`, so a downstream subcommand's hook silently shadows the root bootstrap (config/telemetry/update); GTB sets neither `EnableTraverseRunHooks` nor a warning, and the docs steer authors straight into it. **Fix:** set `EnableTraverseRunHooks = true`, or move bootstrap into `Execute`/outermost middleware, and warn.
- `missing-security-headers-middleware` (**Medium**) — no built-in security-headers middleware despite `pkg/openapi` serving an interactive try-it console; downstream tools each reinvent it. **Fix:** add `SecurityHeadersMiddleware` (nosniff, frame-ancestors, referrer-policy, optional HSTS/CSP) applied at least to the docs handler.
- `no-signal-aware-execution-context` (**Medium**) — `rootCmd.Execute()` has no `signal.NotifyContext`; Ctrl-C kills via Go's default handler, skipping `cmd.Context()` cancellation, the telemetry flush, and update-temp cleanup. **Fix:** `signal.NotifyContext` + `ExecuteContext`; update the skeleton main template.
- `api-stability-gate-not-enforced-in-ci` (**Medium**) — `apidiff` is documented as mandatory but appears nowhere in CI/justfile; the backward-compat guarantee relies on contributors remembering. **Fix:** add a `just apidiff` target and a blocking MR job.
- `flag-to-config-binding-unimplemented` (**Medium**) — the documented "Flags" precedence layer has **no `BindPFlag`** anywhere and no binding hook; built-ins like `--debug` are special-cased. The two docs also contradict each other on whether flags beat env vars. **Fix:** add a binding facility (`WithBoundFlags`) applied during config load, and reconcile the precedence docs.
- `stale-testing-doc-api-example` (low) — `testing.md` shows a `NewReaderContainer(logger, "yaml", ...)` call that no longer compiles (post functional-options refactor).
- `stale-builtin-command-inventory` (low) — `docs/index.md`/CLAUDE.md omit `DoctorCmd`/`ChangelogCmd` from the default feature set; generate the inventory from `props.DefaultFeatures`.
- `no-root-version-flag` (low) — root command never sets cobra `Version`, so `--version`/`-v` do nothing in every GTB tool. **Fix:** `rootCmd.Version = props.Version.GetVersion()`.
- `completion-command-ungated-undocumented` (low) — cobra's auto `completion` command bypasses the `FeatureCmd` model and is undocumented. **Fix:** add a `CompletionCmd` feature + a how-to.
- `no-downstream-test-fixture-helper` (low) — no exported `propstest`/`testkit` to assemble a wired `Props`; consumers hand-assemble field-by-field and hit nil-deref surprises. **Fix:** add a public fixture helper; `internal/exectest` and `internal/testutil` are internal-only.
- `collector-invariant-not-upheld-for-init` (low) — `Props.Collector` documented "always non-nil" but is nil on the init and help paths, and all six internal consumers nil-guard it anyway. **Fix:** default it to a noop at construction, or weaken the doc. (Deduplicates the cross-cutting `collector-invariant-vs-nil-guards`.)
- `init-detection-by-use-string` (low, improvement) — command identity decided by fragile `cmd.Use == "init"` comparisons that match any tree command and break on arg suffixes; use the typed `Feature`/annotations.
- `flag-setup-creates-config-dir-side-effect` (low, improvement) — building the command tree (`--help`, completions) creates `~/.toolname` via `GetDefaultConfigDir`'s hidden `MkdirAll`; make the getter pure and create lazily.
- `configpaths-closure-append-accumulates` (low, bug) — `PersistentPreRunE` appends to the captured `configPaths` slice each invocation and can alias the caller's slice; idempotent merge so no value corruption, but unbounded growth on re-Execute. **Fix:** `slices.Clone` per invocation.

---

## 4. Recommended Action Plan

### Phase 1 — Immediate (correctness/data-loss/security, mostly quick fixes)
- **Config hot-reload rework (spec-worthy):** `observer-errs-channel-deadlock`, `multi-file-hot-reload-destroys-merge`, `single-file-watch-never-enabled`, `reload-validation-no-rollback`, `observers-slice-data-race`. These compound; a single container-owned-watcher spec (`/gtb-spec`) addresses all five and is the highest-leverage work in the report.
- **Generator validation perimeter (spec-worthy, security):** `command-name-no-validation-path-traversal`, `signing-fields-unvalidated-unescaped-yaml-injection` — extend `ValidateManifest` over `Commands` + `Signing` and pipe signing fields through `escapeYAML`. Touches the documented template-security model, so spec the validator additions.
- **Controls lifecycle (spec-worthy):** `error-handler-busy-spins-after-cancel`, `restart-policy-restarts-clean-exit-and-sends-nil-errors`, `start-not-idempotent`, `no-force-stop-despite-documented-timeout`, `nil-start-stop-func-panics`, `withoutsignals-leaves-notify-registered`, `unrun-async-check-reports-healthy`. The supervisor/goroutine-lifecycle cluster warrants one coordinated spec.
- **Quick fixes (no spec):** `loadfilescontainer-nil-nil-contract`, `ensure-minimal-config-overwrites-user-config`, `update-prompt-failure-defaults-to-auto-update`, `gemini-history-not-persisted-across-chat`, `add-flag-regen-drops-command-metadata`, `keys-generate-private-key-clobber-and-perms`, `extract-silent-success-when-binary-missing`.

### Phase 2 — Near-term (medium, mostly quick fixes)
- **Chat contract alignment:** `claude-chat-missing-default-model`, `claude-settools-nil-parameters-panic`, `openai-unchecked-choices-index`, `claude-ask-without-schema-leaves-target-untouched`, `maxtokens-ignored-openai-gemini`, `tool-handler-panic-not-recovered` — a coherent batch ("normalize cross-provider contracts," matches the docs).
- **Security quick fixes:** `gitlab-token-sent-to-arbitrary-asset-host`, `ghlogin-bypasses-pkg-browser`, `client-auth-middleware-leaks-creds-on-redirect`, `metadata-values-not-redacted-at-ingest`, `redact-missing-gitlab-and-aws-secret-formats`, `redact-prefix-underscore-leak`, `gtb-install-hint-ships-to-downstream-tools`, `renovate-automerge-no-minimum-release-age`, `ai-doc-tools-path-traversal`, `unbounded-binary-download`, `subkey-bypasses-strength-policy`.
- **cmd-root/telemetry:** `flush-gate-ignores-forceenabled-and-env`, `second-newcmdroot-panics-after-seal`, `config-set-cannot-persist-with-embedded-merge`, `nil-version-panics-in-prerun`, `withauthcheck-reads-global-viper` (signature change — justify under API stability), `resolve-target-path-requires-path-and-tty`.
- **Transport:** `http-tls-start-fails-silently`, `status-func-only-nil-checks`, `rate-limit-broken-under-concurrency`, `collector-buffer-unbounded-when-spill-unavailable`.
- **vcs:** `createbranch-fails-on-already-up-to-date`, `configure-ssh-auth-nil-sub-panic`, `repo-auth-hardcoded-to-github` (provider-aware auth — small spec).
- **Framework features (spec-worthy):** `no-signal-aware-execution-context`, `child-persistentprerune-silently-disables-framework-setup`, `missing-security-headers-middleware`, `flag-to-config-binding-unimplemented`. **CI:** `api-stability-gate-not-enforced-in-ci` (no spec).

### Phase 3 — Backlog (low/info: refactors, doc drift, latent races, hygiene)
The `setup-credential-wizard-duplication`, `props-provider-interfaces-unused`, `track-methods-duplicated-event-construction`, `wkd-hash-encoder-duplicated`, `manifest-io-duplication`, and `exported-mocking-hooks-update-cmd` items are pure-addition refactors. The remaining low/info findings (doc drift, dead API, latent races, missing `--version`/completion docs/test fixtures, the various `Run`-vs-`RunE` and stdlib-errors stragglers) can be batched by subsystem. None require a spec.

---

## 5. What's Healthy

This codebase earns its security-conscious reputation; the following were verified as well-built:

- **Update trust chain (when enabled):** detached-signature verification over *raw* manifest bytes before parsing, constant-time hash compare, key-strength policy failing closed on unknown algorithms, fatal embedded↔WKD fingerprint cross-check, and `+1`-byte-bounded manifest/signature/WKD downloads with redirect refusal. The fail-open default is documented intentional rollout, not an oversight.
- **Chat security:** central `ValidateBaseURL` before any credential construction, a single audited five-step API-key cascade, AES-256-GCM snapshot store with two-layer path-traversal defence, and `MaxSteps`-bounded ReAct loops across all providers and streaming paths.
- **Credentials package:** clean atomic backend registry, well-specified sentinel errors, context-aware contract, PID+nonce probe isolation, mutex-guarded test backend, and a coherent blank-import dead-code-elimination pattern.
- **Transport baseline:** shared hardened TLS defaults (1.2+, AEAD suites), config-prefix cascading, health endpoints mounted outside middleware, body/message size caps, redaction-sourced sensitive header names, and the verified gRPC `GracefulStop`-with-forced-`Stop` shutdown fix.
- **Generator escaping layer:** the `validate.go` character-class rules + `template_escape.go` context-aware helpers are well-designed, pure, and correctly applied at the project-name/description/host/org render sites — the gaps are perimeter (which inputs get validated), not the escaping itself.
- **DI/command wiring:** narrow provider interfaces with compile-time satisfaction checks, sealed middleware registry with per-feature chaining, and deliberately per-root mutable state.
- **VCS release-provider layer:** clean registry with proper locking, consistent cockroachdb/errors usage, bounded reads, `regexutil` for the config-supplied Bitbucket pattern, and a well-documented `vcs.ResolveToken` credential chain.
- **Telemetry/redact engineering:** audit-driven hardening (bounded reads, args/errMsg redaction, basename-only spill logging), fuzz-tested invariants, 0600 file perms, clean opt-in/consent separation.
- **Support primitives:** `pkg/browser` and `pkg/regexutil` implement their documented threat models cleanly; errorhandling's hint/stacktrace plumbing is well-factored; changelog parsing is careful with archive limits.
- **Workspace package:** small, idiomatic, healthy apart from one cosmetic loop-var and a `maxDepth<=0` edge.

The verification step's repeated downgrades from "high" to "medium" — and several "partial"/refuted sub-claims — are themselves a signal: the dangerous-sounding findings mostly turned out to be bounded by controls the codebase already has in place. Prioritize the config-hot-reload, controls-lifecycle, and generator-validation clusters; the long tail is genuine but low-stakes.