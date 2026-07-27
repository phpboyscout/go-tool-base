# Architectural Review — go-tool-base and the Extracted Toolkit Modules

- **Date:** 2026-07-23
- **Scope:** go-tool-base (framework core, `internal/generator`, residual `pkg/` glue) plus all 51 extracted toolkit modules under `gitlab.com/phpboyscout/go/*` (config family, chat family, forge family, transport stack, lifecycle/leaf modules), and a cross-cutting ecosystem view (dependency graph, version coupling, convention drift).
- **Method:** nine parallel deep-read reviews, one per subsystem, each grounded in full or substantial source reads. Findings were required to be **verified against the actual code** — several were additionally reproduced empirically with probe tests against the real modules (marked "Verified" in the text). The ecosystem section verified every module's `go.mod` and CI pins against **remote HEAD** rather than local clones.
- **Severity scale:** **CRITICAL** — a core advertised capability is broken or a security control is defeated in the default path; **HIGH** — a real defect with a concrete failure scenario on a mainline path; **MEDIUM** — a defect on a common secondary path, a contract violation, or a design flaw with clear failure modes; **LOW** — edge cases, drift, dead code, ergonomics.

## Executive summary

The extraction programme has produced a structurally sound ecosystem: the 51-module dependency graph is a clean DAG with zero cycles and zero replace directives, layering is honest (no leaf depends on a heavy module), logging/context/options conventions are near-uniform, and the security engineering in the foundational modules (forge's `HostTrusted`, the self-update verify-before-install chain, chat's `ValidateBaseURL`, browser/regexutil/redact's documented invariants, the generator's two-layer template-injection defence) is well above baseline and largely **holds as documented**. Individual module design quality is frequently excellent — the config Store, the transit resilience core, and controls' documented concurrency decisions stand out.

The review found **116 findings: 1 critical, 20 high, 49 medium, 46 low**. They cluster into five themes, and the pattern is consistent: the *cores* are strong; the defects live at the **seams** — between core and adapters, between modules and their consumers, and between what documentation promises and what code does.

**Headline items:**

1. **[CRITICAL] The forge-aware setup's OAuth flow is dead on arrival.** The interactive credential stage runs under a 5-second keychain-timeout context that also spans the human-paced device-code flow, so the headline capability of the forge-aware-setup migration always times out and silently falls back to manual PAT entry (`pkg/setup/forge/single.go:147`). The fallback masks the failure completely.
2. **Self-update has two real security gaps** despite otherwise exemplary verification ordering: no downgrade guard (a validly-signed older release served as "latest" is silently installed — a classic rollback attack, and this project has already experienced a rogue-tag incident), and the GitLab provider forwards `PRIVATE-TOKEN` across cross-host redirects (object-storage 302s and open-redirect endpoints leak the token off the pinned host).
3. **`config.Store.Apply` fails whenever any *other* declared source is absent** — reproduced empirically. GTB deliberately declares its highest-precedence write target even when the file doesn't exist yet, so `tool config set` fails on exactly the fresh-ish installs it's meant to serve.
4. **The ecosystem's own toolchain doesn't parse its own output:** the changelog module's parser cannot parse releaser-pleaser changelogs (returns zero releases silently, so breaking-change warnings never fire), and its generator drops any release whose tag sits on a merge commit — the normal shape of a merged Release MR.
5. **Version propagation has stalled fleet-wide.** GTB pins config v0.4.0 against a v0.9.0 head (skipping the sensitive-write-path hardening minors); the 19 config adapters pin five different core minors (the one GTB uses, config-afero, is furthest behind at v0.3.0); 16 modules still run cicd v0.22.0 against a v0.27.0 head. Nothing is broken *yet*, but pre-1.0 policy licenses breaking minors in exactly the widest-fan-out modules, and every mechanism keeping the fleet coherent is convention-plus-human-merges.
6. **Fresh-install ergonomics of every scaffolded tool are impaired:** cobra's auto-generated `help`, `completion`, and per-keystroke `__complete` commands run the full bootstrap and **fail hard when no config file exists**; `tool version` exits non-zero when the release source is unreachable; interactive consent/update prompts inside the pre-run lack the TTY guard the rest of the codebase standardised on after the MR !157 init-hang incident (MCP stdio is exposed).

## Cross-cutting themes

**1. Seam drift between cores and their satellites.** The chat providers disagree on `Ask` semantics (history retention, tool interaction); the forge providers disagree on config scoping and `Host` semantics (Gitea reads `url.api` from the config root and needs a scheme-qualified host); the config adapters can't satisfy the core's capability probes because those interfaces have unexported methods; `WithPollInterval` means three different things across config backends (and makes azure-appconfig poll a billed API every 2 s). Each family would benefit from a **conformance test suite owned by the core and run by every satellite in CI** — config already has `backendconformance`, which is exactly the right pattern; chat and forge need the equivalent for behavioural contracts.

**2. Documentation that promises what code doesn't do.** `redact` claims generic URL-userinfo stripping but only handles http/https (database connection-string passwords reach telemetry); `output.ParseFormat` documents case-insensitivity but is exact-match; controls headlines "startup ordering" that doesn't exist and documents states that don't exist; retry documents default status codes it never applies; telemetry documents an at-least-once guarantee its spill-pruning violates; config-vault documents a constructor that doesn't exist. Individually small; collectively a pattern worth a doc-audit pass and, where feasible, tests that assert documented behaviour.

**3. Timeout/context hygiene at blocking boundaries.** The CRITICAL setup finding, credentials.Probe (keychain backend can't honour its context — wedged D-Bus blocks first-run setup forever), controls' advisory-only health-check timeout, claude-local ignoring `RequestTimeout`, the pre-run update check able to block any command for 5 minutes, and repo's context-free go-git calls holding the `ThreadSafeRepo` mutex through uncancellable clones. The codebase already has the right idiom in two places (regexutil's documented abandon-on-timeout; `Services.stop`'s abandon-at-deadline) — it needs applying at the remaining blocking boundaries.

**4. Silent failure modes in configuration plumbing.** `...any` option variadics silently discard mistyped options (including auth interceptor chains — a security control can vanish without a compile error); GTB adapters turn an invalid port into a silent ephemeral-port bind; assets structured-merge swallows malformed bundles; `verifyProject` swallows manifest decode errors; observability drops configured auth headers on the env-var fallback path. Prefer loud failure (error or WARN) at every one of these sites.

**5. Propagation debt as the ecosystem's principal systemic risk.** The many-small-modules architecture is clean but expensive: a tls change fans out into 12 human-gated releases across 4 sequential waves; a config change into ~20–23. The evidence shows propagation stalls where fan-out is largest. The two highest-leverage moves: a config-family re-pin sweep (fixes both ecosystem HIGHs at once) and fleet-managing the cicd component pin. Longer-term, consider whether the 19 config adapters belong in one multi-module repo with synchronised tagging.

## Priority actions

> **Implementation status (updated 2026-07-27).** **All 21 critical/high findings
> are fixed and merged** — all 19 critical/high specs are now `IMPLEMENTED`. Each
> fix landed via red-first TDD (the defect reproduced against the target repo's
> pre-fix `main` before the change) and shipped in a module release or GTB `main`.
> The config-family version skew resolved via Renovate (GTB + all 19 adapters now
> uniform at config v0.9.2) and is kept from recurring by a gating cross-repo
> canary in `go/config` CI. The 11 grouped medium/low follow-up specs remain
> `DRAFT` for scheduling. The GTB release carrying the framework-side fixes is
> queued (releaser-pleaser Release MR) pending a deliberate cut.

**Every finding in this report has been triaged into a spec** in
[`docs/development/specs/`](../specs/index.md) (2026-07-23 series) — each finding
below carries a **Spec:** pointer. The critical and high findings map to 19
individual specs (two pairs share one spec where the fix is a single workstream:
both changelog findings; both ecosystem re-pin findings, with four closely-coupled
lower-severity findings folded in where a spec's scope naturally contains them).
The remaining medium and low findings are batched into 11 grouped follow-up specs
split by functionality/project:
[config-family-followups](../specs/2026-07-23-config-family-followups.md),
[chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md),
[forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md),
[transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md),
[leaf-module-contract-fixes](../specs/2026-07-23-leaf-module-contract-fixes.md),
[observability-telemetry-followups](../specs/2026-07-23-observability-telemetry-followups.md),
[controls-followups](../specs/2026-07-23-controls-followups.md),
[changelog-yamldoc-workspace-followups](../specs/2026-07-23-changelog-yamldoc-workspace-followups.md),
[gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md),
[generator-followups](../specs/2026-07-23-generator-followups.md), and
[ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md).

| # | Action | Findings addressed | Spec(s) |
|---|--------|--------------------|---------|
| 1 | Fix the setup credential-stage context scoping (5 s keychain timeout must not span interactive flows) | CRITICAL (setup/forge) | [setup-credential-stage-context-scoping](../specs/2026-07-23-setup-credential-stage-context-scoping.md) |
| 2 | Add a self-update downgrade guard; strip/refuse `PRIVATE-TOKEN` on cross-host redirects | 2 HIGH (setup, forge-gitlab) | [self-update-downgrade-guard](../specs/2026-07-23-self-update-downgrade-guard.md), [forge-gitlab-redirect-token-hardening](../specs/2026-07-23-forge-gitlab-redirect-token-hardening.md) |
| 3 | Fix `config.Store.rebuild` to tolerate absent declared sources (mirror `loadAll`) and release; re-pin GTB + all adapters to config head | 3 HIGH (config core, ecosystem ×2) | [config-apply-absent-source](../specs/2026-07-23-config-apply-absent-source.md), [config-family-version-repin](../specs/2026-07-23-config-family-version-repin.md) |
| 4 | Exempt help/completion/`__complete` (and `mcp`) from the bootstrap gate; TTY-guard the pre-run prompts; make `version` degrade gracefully offline | 3 HIGH (GTB core) | [bootstrap-auxiliary-command-exemptions](../specs/2026-07-23-bootstrap-auxiliary-command-exemptions.md), [prerun-prompt-tty-guard](../specs/2026-07-23-prerun-prompt-tty-guard.md), [version-command-offline-degradation](../specs/2026-07-23-version-command-offline-degradation.md) |
| 5 | Rewrite changelog's parser/generator against real releaser-pleaser fixtures and merge-commit tags | 2 HIGH (changelog) | [changelog-real-world-format-compat](../specs/2026-07-23-changelog-real-world-format-compat.md) |
| 6 | Fix the gateway double-middleware application, breaker half-open wedge, and mTLS config clobber | 3 HIGH (transport) | [transport-gateway-single-middleware-application](../specs/2026-07-23-transport-gateway-single-middleware-application.md), [transit-breaker-halfopen-recovery](../specs/2026-07-23-transit-breaker-halfopen-recovery.md), [transport-server-tls-mtls-support](../specs/2026-07-23-transport-server-tls-mtls-support.md) |
| 7 | Extend `ValidateManifest` to cover every field the renderers consume (repo, flags incl. `default_is_code`, signing, descriptions) | 1 HIGH + 1 MEDIUM (generator) | [generator-manifest-validation-hardening](../specs/2026-07-23-generator-manifest-validation-hardening.md) |
| 8 | Widen `redact.String`'s userinfo pattern to all URL schemes | 1 HIGH (redact) | [redact-scheme-agnostic-userinfo](../specs/2026-07-23-redact-scheme-agnostic-userinfo.md) |
| 9 | Fix the Gemini error-chain destruction and OpenAI shared-params bleed | 2 HIGH (chat) | [chat-gemini-error-chain-preservation](../specs/2026-07-23-chat-gemini-error-chain-preservation.md), [chat-openai-request-isolation](../specs/2026-07-23-chat-openai-request-isolation.md) |
| 10 | Preserve/refuse anchors in yamldoc `Set`; bound `controls.Wait` | 2 HIGH (yamldoc, controls) | [yamldoc-anchor-preservation](../specs/2026-07-23-yamldoc-anchor-preservation.md), [controls-bounded-wait](../specs/2026-07-23-controls-bounded-wait.md) |
| 11 | Fix the bitbucket download-URL contract contradiction | 1 HIGH (forge-bitbucket) | [forge-bitbucket-download-url-contract](../specs/2026-07-23-forge-bitbucket-download-url-contract.md) |

Severity distribution by area:

| Area | CRITICAL | HIGH | MEDIUM | LOW |
|------|----------|------|--------|-----|
| config family | — | 1 | 6 | 3 |
| chat family | — | 2 | 7 | 5 |
| forge family + setup | 1 | 3 | 6 | 5 |
| transport stack | — | 3 | 6 | 5 |
| cross-cutting leaves | — | 1 | 4 | 6 |
| controls / changelog / yamldoc / workspace | — | 4 | 4 | 6 |
| GTB framework core | — | 3 | 7 | 7 |
| internal/generator | — | 1 | 5 | 5 |
| ecosystem (cross-cutting) | — | 2 | 4 | 4 |
| **Total** | **1** | **20** | **49** | **46** |

The remainder of this report contains the full per-subsystem reviews, each with an architecture summary, severity-ordered findings with file:line evidence and concrete failure scenarios, and a strengths assessment.

---

## Architectural review: the `go/config` family

### Architecture summary

The family is built around a single-owner `Store` (`config/store.go`) that is the only reader, writer, and watcher of configuration. Sources are `Backend`s contributing `Layer`s in declaration order (last wins); a merged, immutable `Snapshot` is published through an `atomic.Pointer`, so reads are lock-free and coherent while all mutation (load, apply, reload) serialises on one mutex. Provenance is recorded during the merge fold (per-leaf `Origin`, full `Shadowed` chains). Writes are three-phase (`Prepare` → `Verify` → `Commit` with rollback), routed to the highest-precedence writable layer that defines the key, validated as a *change diff* against the schema (so a pre-existing violation never bricks the repair surface), and the next snapshot is built from the staged content rather than a re-read. Capability differences are expressed structurally — `WritableBackend`, `WatchableBackend`, `EditingCodec` are separate interfaces discovered by type assertion — plus a small `Capabilities` struct (`Sensitive` drives an enforced secret-leak guard in routing, `plan.go:224-229`). Observers get pinned views, ordered delivery, and a goroutine-identity re-entrancy guard (`ErrWriteFromObserver`).

Adapters come in three shapes: **filesystem adapters** implementing the six-method `config.FS` (afero, billy, iofs, sftp, s3, gcs, azure-blob) plus optional interfaces (`RealPather`, `LinkReader`, `PollIntervalHinter`); **format codecs** implementing `Codec`/`EditingCodec` (toml, hcl, json, dotenv, ini, properties, xml) — the editing ones do targeted byte-splicing to preserve comments and layout; and **KV/secret backends** implementing `Backend` directly (consul, vault, aws-ssm, azure-appconfig, gcp-parameter), each with an injected narrow client interface, CAS-style writes where the store supports them, and a shared `backendconformance` suite. GTB consumes the core cleanly: `pkg/cmd/root/root.go` builds the layered store (defaults → files → env → flags), takes fresh `View()`s per read, registers no observers, and reconciles its one deliberate bypass (`config edit`) with a `Reload`.

### Findings

#### [HIGH] `Apply` fails outright whenever any *other* declared source is absent

> **Spec:** [2026-07-23-config-apply-absent-source](../specs/2026-07-23-config-apply-absent-source.md)

**Evidence:** `config/store.go:851-879` (`rebuild`) vs `config/store.go:506-539` (`loadAll`). `loadAll` tolerates `fs.ErrNotExist` for optional sources; `rebuild` — introduced by `f6eba9e` "re-derive key-aware backends when building a candidate", shipped in v0.3.0+ — re-loads every non-written backend and treats **any** error, including `fs.ErrNotExist`, as fatal.

**Verified failure scenario** (reproduced with a test against the module): a store with `base.yaml` (exists) and `overlay.yaml` (declared, absent — the ordinary optional-overlay setup). `Apply(Set("foo","baz"))` for a key defined in `base.yaml` routes to `base.yaml`; `rebuild` then re-loads the absent `overlay.yaml` and the whole Apply fails with `overlay.yaml: file does not exist`. GTB is directly exposed: `declaredConfigPaths` (`pkg/cmd/root/root.go:292-304`) deliberately declares the highest-precedence write target even when absent, and `config set` (`pkg/cmd/config/set.go:47,101`) uses unpinned routing — so setting a key that already exists in a lower file fails whenever the top declared file has not been created yet.

**Direction:** in `rebuild`, mirror `loadAll`'s branch — on `errors.Is(err, fs.ErrNotExist)` carry the backend forward with no layers instead of failing. Add a conformance test: "Apply routed at one source succeeds while a sibling declared source is absent."

#### [MEDIUM] Optional capability interfaces are sealed by unexported methods — the extension points don't compose across the module family

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** `config/codec.go:313-317` probes `interface{ preservesComments() bool }`; `config/codec.go:226-234` defines `watchErrorReporter` (`setWatchErrorHandler`) and `watchPathReporter` (`watchPath`) with unexported methods. An unexported interface method is package-qualified, so no type outside package `config` can ever satisfy these.

**Failure scenario:** `config-toml` and `config-hcl` genuinely preserve comments, yet their backends report `Capabilities().PreservesComments == false` — the capability system misreports exactly the sibling modules the codec seam exists for. Likewise, `store.go:1047-1052` claims "a consumer's own backend can route its watch errors the same way" — false: no external backend can register a watch-degradation handler, so their watch failures can never reach `OnReloadError`; and `watchedPaths` for an injected `Watcher` silently omits any file-like backend defined outside the core.

**Direction:** export the three probes as public optional interfaces (e.g. `CommentPreservingCodec`, `WatchErrorReporter`, `WatchPathReporter`). This is exactly the pattern `FS`'s optional interfaces already get right.

#### [MEDIUM] `WithPollInterval` means three different things across the family, and azure-appconfig polls a billed API every 2 seconds by default

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** `Store.Watch` initialises `interval: DefaultPollInterval` (2s, `config/store.go:1000`, `config/watch.go:24`) and passes it to every backend (`store.go:1055`). `config-azure-appconfig/watch.go:29-31` honours the passed interval and only falls back to its 30s default when `interval <= 0` — which the Store never passes. So a default `Store.Watch` over an azure-appconfig backend issues a billed List **every 2 seconds**, against the adapter's own documented intent. Meanwhile `config-aws-ssm/watch.go:33` and `config-gcp-parameter/watch.go:28-31` *ignore* the Store interval entirely (own 60s options), and `config-consul/watch.go` reuses it as blocking-query `WaitTime`. The `PollIntervalHinter` mechanism that solves this for FS adapters has no equivalent for backend adapters.

**Direction:** define the contract once in the core — a sentinel meaning "caller didn't choose" passed through when the user did not set `WithPollInterval`, or a backend-side `PollIntervalHinter` analogue — and fix azure-appconfig to treat the untouched default as "use my own cadence".

#### [MEDIUM] Consul watch loop advances the Load-time index, hollowing out `Verify`'s stated guarantee

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** `config-consul/configconsul.go:118-123` documents `b.index` as "captured at Load so the conflict check compares against what routing saw, not a fresh fetch". But `config-consul/watch.go:52-58` mutates `b.index` to the latest value on every foreign change, *before* the Store's settled reload runs. `Prepare` reads that advanced index (`write.go:29-31`) and `pending.Verify` compares a fresh List against it (`write.go:150-160`) — so a foreign change that the watcher has seen but the Store has not yet reloaded passes Verify. The per-key CAS at Commit still protects the keys being written, but keys that informed routing/shadowing decisions are unguarded.

**Direction:** keep a separate watch cursor and leave the Load-time index untouched for `Verify`; or have Verify compare against a value snapshotted into `pending` at Load.

#### [MEDIUM] Backend ID uniqueness is never checked at construction — duplicate IDs silently misroute writes

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** `adoptBackend` rejects duplicate IDs only for runtime `AddLayer` (`config/store.go:265-278`); `NewStore` (`store.go:183-203`) appends backends unchecked, and `backendByID` returns the first match (`store.go:974-982`). Adapter IDs are bare prefixes/paths: consul `b.prefix`, ssm prefix, vault path, azure `prefix[@label]`.

**Failure scenario:** a store layering a Consul prefix `app/` over an Azure App Configuration prefix `app/` (both normalise to ID `"app/"`) — a write routed at the higher layer is looked up by name and lands on the *first* backend with that ID, i.e. the wrong system, with no error.

**Direction:** validate ID uniqueness in `NewStore`; optionally namespace adapter IDs by kind (`consul:app/`).

#### [MEDIUM] The synthesised source for an empty writable backend hard-codes `Kind: SourceFile`

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** `config/store.go:932-940` — a writable backend that contributed no layer is synthesised into the precedence order as `Source{Kind: SourceFile, …}` regardless of what the backend is. `Plan` output and `Operation.Target` then present e.g. an empty Consul prefix with file semantics, and provenance-driven UX mislabels the destination. `sensitiveSources` also deliberately skips synthesised entries (`store.go:949-953`) — a future writable sensitive backend would silently lose the leak guard while empty.

**Direction:** let the backend declare its `SourceKind` (small optional interface or `Capabilities` field).

#### [MEDIUM] GTB discards `Watch`'s stop function — watcher teardown is contextual, not structural

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** `pkg/cmd/root/root.go:830`: `if _, err := cfg.Watch(cmd.Context()); err != nil {…}` — the stop handle is thrown away; release depends entirely on `cmd.Context()` being the cancel-on-signal context wired in `pkg/cmd/root/execute.go:111-168`. Any embedder that builds the command tree and calls `ExecuteContext(context.Background())` directly leaks the fsnotify/poll goroutines with no way to stop them — and GTB is a *framework* whose command tree is explicitly reusable.

**Direction:** retain the stop func on `rootState` and invoke it in the shutdown path, keeping context cancellation as the backstop.

#### [LOW] config-vault documents a `NewPrefix` mode that does not exist, and carries dead state for a watch it doesn't have

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** `config-vault/configvault.go:8` and `:76` reference `[NewPrefix]`; no such function exists. `KV.List` is public API reachable by nothing. `backend.version` is recorded at every Load for "the watch" — but the module implements no `Watch` at all, so rotated secrets are only picked up on explicit `Reload`, and `Store.Watch` over a vault-only store fails loudly. The godoc links render broken.

**Direction:** either ship `NewPrefix` + a poll `Watch`, or cut the doc claims, the `List` method, and the dead field.

#### [LOW] Value-rendering escape rules are inconsistent across editing codecs

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** the core YAML path routes every written value through yamldoc's emitter (invisible/bidi escaping documented as load-bearing, `config/yaml_codec.go:206-217`). `config-toml/write.go` `quote()` escapes only `" \ \n \t \r` — other control bytes render invalid TOML (caught fail-closed by the staged re-decode, but as a confusing `ErrBackendParse` on the user's own write); bidi/invisible characters pass through unescaped. `config-json` passes `edit.Path` verbatim into sjson/gjson path syntax (`config-json/configjson.go:90-106`), where `*`, `?`, `#`, numeric segments and `\` carry meta-meaning — a key literally named `0` under an array-valued sibling addresses an array index, not the object key.

**Direction:** escape control characters in the TOML quoter; escape gjson path metacharacters; consider a shared "renderable scalar" contract test in the conformance suite.

#### [LOW] Minor error-taxonomy and write-surface inconsistencies across backends

> **Spec:** [2026-07-23-config-family-followups](../specs/2026-07-23-config-family-followups.md)

**Evidence:** unwritable-value errors are `config.ErrBackendUnsafe` in toml but bare module-local sentinels in consul (`configconsul.go:33-35`) and azure-appconfig — a caller cannot `errors.Is` uniformly. The scalar `encode` sets in consul/azure accept `string/[]byte/bool/int/int64/float64` only — an `int32`, `uint`, or `float32` is refused. `config-sftp` `WriteFile` opens with the server's umask and applies `perm` via a second `Chmod` after content is written — a staged file holding credentials is briefly readable at the umask default on the remote host.

**Direction:** wrap adapter unwritable-value errors in a shared core sentinel; widen `encode` via `cast`; on sftp, chmod before writing the payload.

### Test quality

Strong overall. The core covers the genuinely hard paths: concurrent reads vs reloads, a concurrent legitimate write while observers run, out-of-order observer deliveries settling on the newest section, rollback/partial-commit, precedence/provenance, BDD features, and a prior review's findings are regression-locked (`reviewfixes_test.go`, ~1500 lines). The shared `backendconformance` suite is run by the remote adapters and format codecs — a genuinely good pattern for keeping the family honest. Gaps: no test applies a write while a *different* declared source is absent (the HIGH bug), nothing exercises `Store.Watch`'s default interval against a billed-poll backend, and the consul watch-vs-Apply interleaving is untested.

### Strengths

This is one of the most deliberately designed configuration stacks reviewed. The single-owner Store with lock-free snapshot reads, provenance recorded during the merge fold, diff-based schema validation that keeps broken configs repairable, the three-phase write with staged-content snapshot construction, the enforced sensitive-leak guard, and the capability-by-interface pattern are all correct answers to problems most libraries don't even acknowledge. The commentary is exceptional — nearly every non-obvious decision names its alternative and why it lost — and the adapters consistently follow the injected-narrow-client pattern, keeping credentials, retries and endpoints with the consumer while making every unit suite runnable offline. The GTB consuming side is equally disciplined. The findings above are seam defects of a young extraction, not cracks in the model.

---

## Architectural review: chat family (`go/chat`, `chat-anthropic`, `chat-openai`, `chat-gemini`, GTB `pkg/chat`)

### Architecture summary

The core module (`gitlab.com/phpboyscout/go/chat`, ~3k LOC) is deliberately SDK-free: it owns the `ChatClient` interface (Add/Ask/SetTools/Chat/Usage, plus opt-in `StreamingChatClient` and `PersistentChatClient` discovered by type assertion), a mutex-guarded provider registry populated by provider-module `init()` via blank imports, shared tool-dispatch helpers (`ExecuteTool`/`DispatchToolExecution` with panic recovery and bounded parallelism), a five-step credential cascade (`ResolveAPIKey`), BaseURL validation, a usage tracker, a snapshot store (AES-256-GCM, two-layer path-traversal defence), and a fallback composite with a pluggable `FailoverPolicy`. A second registry (`RegisterStatusExtractor`) lets the SDK-free core classify provider HTTP errors for failover without importing any vendor SDK — a nice mirror of the provider registry. The `claude-local` CLI-delegation provider ships in core. Each provider module (anthropic/openai/gemini) implements its own ReAct loop bounded by `Config.MaxSteps` (default 20), with tool errors fed back as conversation content rather than aborting the loop.

The GTB adapter (`pkg/chat`) is genuinely thin: it owns the `ai.*` / `<provider>.api.*` config-key schema, maps Props/config into the module's typed `Settings` (injecting the hardened HTTP client and the keychain lookup as seams), blank-imports the three provider modules, and reimplements three small unexported helpers (default-provider resolution, fallback-primary warning, per-provider config derivation). The security invariants asked about **hold**: every construction path routes through `chat.New`, which calls `ValidateBaseURL` before any factory runs (`client.go:243-256`; factories are unexported and only reachable via the registry; both fallback constructors go through `New`); `AllowInsecureBaseURL` is `json:"-"` and GTB never decodes `chat.Config` from config directly; `logProviderEndpoint` logs hostname only at INFO (`client.go:272-284`, `baseurl.go:189-200`); no code path logs a resolved API key.

### Findings

#### [HIGH] Gemini Chat/StreamChat errors destroy the typed SDK error, breaking cross-provider failover for Gemini

> **Spec:** [2026-07-23-chat-gemini-error-chain-preservation](../specs/2026-07-23-chat-gemini-error-chain-preservation.md)

`chat-gemini/gemini.go:338-345` — `handleGeminiError` rebuilds the error with `errors.Newf("Gemini API Error (%d): %s", apiErr.Code, apiErr.Message)` (no `%w`), and the non-API branch uses `%v`. Every `Chat`/`StreamChat` error path routes through it (`gemini.go:313`, `gemini.go:493`). The registered `geminiHTTPStatus` extractor (`gemini.go:29-36`) does `errors.As(err, &apiErr)`, which can never match a chain that was flattened to a string; `errors.Is(err, context.DeadlineExceeded)` in `DefaultFailoverPolicy` (`chat/fallback_policy.go:73`) is equally defeated. Concrete failure: a fallback chain `[gemini, claude]` receives a Gemini 429/503 during `Chat` → the policy finds no status and no net error → `FailoverFatal` → the composite returns the error instead of failing over. Only `Ask` (which wraps with `%w`, `gemini.go:232-234`) classifies correctly. Direction: make `handleGeminiError` wrap with `%w` (or just `errors.Wrap` the original), matching the Anthropic/OpenAI providers which preserve the chain.

#### [HIGH] OpenAI client: shared mutable `params` bleeds state between call kinds — StreamChat poisons subsequent Chat/Ask, Chat silently disables Ask's schema

> **Spec:** [2026-07-23-chat-openai-request-isolation](../specs/2026-07-23-chat-openai-request-isolation.md)

`chat-openai/openai.go` keeps one `a.params` for all calls.
- `StreamChat` sets `a.params.StreamOptions = …{IncludeUsage: true}` (`openai.go:394-396`) and never clears it, so a later non-streaming `Chat`/`Ask` on the same client sends `stream_options` on a non-streaming request, which the OpenAI API rejects with a 400.
- `Chat` and `StreamChat` permanently clear `a.params.ResponseFormat` (`openai.go:597`, `openai.go:389`), which was set once at construction (`openai.go:135-146`). `Ask` never re-applies it (`openai.go:228-263`), so the sequence `Chat(); Ask()` silently drops JSON-schema enforcement and `Ask` fails (or mis-parses) on free-text output.

Contrast: Gemini clones its config per call (`gemini.go:578-586` `cloneConfig`) — the correct pattern. Direction: build per-call params from a base (or clone), applying `ResponseFormat`/`StreamOptions` only for the call shape that needs them.

#### [MEDIUM] Data race: fallback `toolInvoked` written concurrently by parallel tool handlers

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`chat/fallback.go:497-515` — `instrumentTools` wraps every handler with `f.toolInvoked = true`. With `Config.ParallelTools` enabled, handlers run in bare goroutines (`chat/tools.go:45-50`), so two tool calls in one ReAct step produce concurrent unsynchronised writes to the same bool — a race-detector hit and undefined under the Go memory model (the read in `shouldAdvance` at `fallback.go:434` is safe only because it happens after `wg.Wait`). Failure scenario: any GTB tool using the fallback composite with `ParallelTools: true` fails `just test-race`/CI intermittently. Direction: make `toolInvoked` an `atomic.Bool`.

#### [MEDIUM] `claude-local` subprocess ignores `RequestTimeout` — can hang forever

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`chat/claude_local.go:220-234` — `runClaude` calls `cmd.Output()` with only the caller's context bounding it; `Config.RequestTimeout` (and `DefaultChatRequestTimeout`, which bounds every HTTP provider via `chat/httpclient.go`) is never applied to the subprocess. A `claude` binary stuck on an interactive prompt, a dead network, or a wedged session blocks `Chat`/`Ask` indefinitely whenever the caller passed `context.Background()` (the common CLI case). Direction: derive `context.WithTimeout(ctx, resolveChatTimeout(c.cfg))` around the subprocess invocation. (Argument handling itself is safe: no shell, prompt is always the value of `-p`, and the `--resume` session ID comes from the trusted binary's own JSON output.)

#### [MEDIUM] `claude-local` violates the no-schema `Ask` contract

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`chat/claude_local.go:110` — with no `ResponseSchema`, `Ask` does `json.Unmarshal([]byte(result), target)` on the raw CLI text. The `ChatClient` contract (`chat/client.go:60-64`) says a schema-less `Ask` returns raw text into a `*string` target — but `json.Unmarshal` of plain prose (`"Hello"`) into a `*string` fails with `invalid character 'H'`. The API-based Claude provider implements the contract correctly via `assignText` (`chat-anthropic/claude.go:271-290`). Failure: code written against the contract works on `claude` and breaks on `claude-local`, defeating the drop-in-provider promise. Direction: port `assignText` into the core and share it.

#### [MEDIUM] SSE streams are never closed on early-exit paths (callback error) — leaked HTTP bodies

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

Both SDK stream types expose `Close()` (`anthropic-sdk-go@v1.56.0/packages/ssestream/ssestream.go:234`, `openai-go/v3@v3.41.0/.../ssestream.go:238`) and neither provider ever calls it. In `chat-anthropic/claude.go:523-560` (`streamClaudeStep`) and `chat-openai/openai.go:443-481` (`streamOpenAIStep`), a callback returning an error exits the `stream.Next()` loop mid-stream with no `defer stream.Close()`, leaving the response body (and connection) open until the caller's context ends — which may be process lifetime for CLI tools. A UI that cancels streams by returning an error from the callback (the documented mechanism, `chat/streaming.go:48`) leaks a connection per cancel. Direction: `defer stream.Close()` immediately after creating each stream.

#### [MEDIUM] Module-level fallback constructors strip credentials from every provider — including the primary

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`chat/fallback.go:227-236` — `perProviderConfig` clears `Token`, `Credentials`, `Model`, `BaseURL` for each provider in the chain, so for a standalone module user, `NewWithFallbackSettings` with an explicitly supplied `Config.Token` fails at construction ("API key is required") for the primary — the token the caller just provided is discarded, and only well-known env vars (`ANTHROPIC_API_KEY` etc.) can satisfy any provider. GTB is unaffected only because its adapter re-runs `SettingsFromProps` per provider to reload `<provider>.api.*` credentials (`pkg/chat/config_adapter.go:97-105`) — the module alone has no equivalent re-resolution hook. The `NOTE` at `fallback.go:225` also flags the denylist-maintenance hazard: any new credential-bearing `Config` field silently carries across all providers. Direction: give the module a per-provider credential-resolution hook (mirroring the GTB adapter), or at minimum keep the caller's `Token`/`Credentials` for the provider matching the base `Config.Provider`; convert the denylist to an allowlist of fields that carry over.

#### [MEDIUM] `Ask` semantics diverge across providers: history retention and tool interaction

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

Three inconsistencies that break provider portability:
- **History**: OpenAI's `Ask` appends the assistant answer to history (`openai.go:255`); Gemini persists the full session (`gemini.go:247`); Claude's `Ask` never appends the response (`claude.go:180-202`) — the answer is forgotten, leaving a dangling user turn. `Ask(); Chat("summarise your last answer")` works on two providers and not the third.
- **Tools set via SetTools leaking into Ask**: Claude deliberately omits tools from `buildAskParams` (`claude.go:204-232`); OpenAI leaves `params.Tools` active during `Ask` (`openai.go:244` sends them), so the model may answer with `tool_calls` and empty `Content`, making the `json.Unmarshal` at `openai.go:257` fail; Gemini's `cloneConfig` carries `config.Tools` into an `Ask` that also forces `ResponseMIMEType: "application/json"` (`gemini.go:223-231`) — a combination the API may reject or resolve unpredictably.
Direction: pick one contract (Ask is a tool-free structured call whose answer joins history) and enforce it in all three modules; this is exactly the kind of semantics the core should own as a documented conformance test-suite that provider modules run.

#### [MEDIUM] No conversation-history bounding anywhere

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

Every provider appends to history forever (`claude.go:174`, `openai.go:220`, `gemini.go:207`), each ReAct step re-sends the whole history, and the fallback composite's transcript grows in parallel (acknowledged at `fallback.go:65-68`). There is no token-window management, trimming, or compaction; the only escape hatch is "create a new client" (`client.go:51-53`). A long-lived agent loop (GTB's self-repair use case) degrades monotonically in cost/latency until the provider rejects the request with a context-overflow error — which `DefaultFailoverPolicy` then classifies as fatal (400), correctly but unhelpfully. Direction: at minimum expose history size/token estimates so callers can decide; ideally a pluggable truncation/summarisation policy in the core.

#### [LOW] Provider-specific knobs leak into the core `Config`

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`chat/client.go:132-142` — `ExecLookPath`/`ExecCommand` (claude-local), `Seed` (OpenAI-only), and especially `GenaiNewClient any` (Gemini's constructor override, typed `any` because the core cannot import genai, with a runtime type assertion at `gemini.go:82-91`). The core config is becoming a union of per-provider test seams; a wrong-typed `GenaiNewClient` fails only at construction time. Direction: move provider-specific overrides into per-module option types (e.g. provider modules export their own `Settings` extension resolved via a typed context key or a registration-time option).

#### [LOW] `AllowInsecureBaseURL` hardening is `encoding/json`-specific

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`chat/client.go:157` — the `json:"-"` tag stops JSON config decoding, but mapstructure (Viper) and YAML decoders ignore JSON tags: any host that unmarshals `chat.Config` directly from config would accept `allowinsecurebaseurl: true`. GTB is safe (its adapter only decodes `RuntimeConfig`/`FallbackConfig`/`CredentialConfig`, never `Config` — `pkg/chat/config_adapter.go:178-208`), but the module-level claim "config files cannot enable it" overstates the protection for other hosts. Direction: add `mapstructure:"-" yaml:"-"` tags, and/or document that hosts must not decode `Config` wholesale.

#### [LOW] OpenAI module: media validated as `ProviderOpenAI` even for compatible backends; unconditional empty system message; pointless prompt chunking

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

- `openai.go:193,233` hardcode `chat.ValidateMediaSet(chat.ProviderOpenAI, …)`, so images/PDF are accepted for any `openai-compatible` backend (e.g. a text-only Ollama model) — the core's per-provider capability map (`chat/media.go:55-59`, which has no `ProviderOpenAICompatible` entry) is bypassed by design of the call site.
- `openai.go:108-110` always seeds `SystemMessage(cfg.SystemPrompt)` even when empty — an empty system turn some strict compatible backends reject; Claude and Gemini only set the system prompt when non-empty.
- `Add`'s `chunkByTokens(prompt, chat.DefaultMaxTokensOpenAI, …)` (`openai.go:205`) splits a long prompt into multiple user messages using an **output**-token constant as the chunk size — all chunks are still sent in the same request, so it reduces nothing, changes message-boundary semantics versus the other providers, and adds a tokenizer dependency for no observable benefit.

#### [LOW] Registry and version coupling are convention-only

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`RegisterProvider` silently overwrites duplicates (`client.go:188-193`) — last blank import wins with no log; and the core↔provider compatibility contract ("keep matching minor versions") has no compile- or runtime-assertion, so adding a method to `ChatClient` (as `Usage()` was) forces a lockstep 4-module release, and a mismatched pair fails only at the consumer's compile (or, for behavioural contracts like the Ask semantics above, not at all). All three providers currently pin `chat v0.1.0` and GTB pins all four at v0.1.0, so today's graph is consistent. Direction: log (or reject) duplicate registrations; add a shared provider-conformance test package in the core that provider modules run in CI to make the behavioural contract enforceable.

#### [LOW] `claude-local` drops buffered `Add()` context when a call fails

> **Spec:** [2026-07-23-chat-provider-conformance](../specs/2026-07-23-chat-provider-conformance.md)

`chat/claude_local.go:152-161` — `buildPrompt` clears `c.pending` before the subprocess runs; if `runClaude` fails (`Chat`/`Ask`), the buffered turns are gone, so a retry sends only the bare prompt. The HTTP providers keep the (appended) user turn in history on failure. Direction: clear `pending` only after a successful run.

### Strengths

The core's dependency-inversion discipline is excellent: an SDK-free hub with two parallel registries (provider factories and HTTP-status extractors) that lets the failover policy classify vendor errors without importing a single SDK, and host seams (`HTTPClient`, `KeychainLookup`, `UsageObserver`) that keep GTB's hardened transport and credential store out of the module. The security engineering is unusually thorough for a module this size: `ValidateBaseURL` is genuinely enforced on every construction path with a documented threat model, the snapshot store has two independent path-traversal defences plus fuzz tests, media input is sniffed-not-trusted through a single choke point, tool handlers get panic recovery on both dispatch paths, credential resolution is one auditable cascade, and no path logs a secret or a full endpoint URL. The fallback composite's design decisions (caller-cancellation always terminal, failover only before the first visible stream event, strict-tool-context opt-in, WARN logs that never echo raw errors) show careful reasoning, and the audit-trail comments indicate real findings were fixed and pinned in place. The GTB adapter is exactly as thin as claimed, with the config-key schema cleanly owned on the GTB side.

---

## Architectural review: forge/VCS family + go-tool-base setup

### Architecture summary

The forge family is a small, deliberately narrow release-provider abstraction: `forge` (core) defines a required `Provider` interface (releases + asset download) plus five **optional capability interfaces** — `ChecksumProvider`, `SignatureProvider`, `Authenticator`, `KeyManager` (and a `Prompter` seam for UI-free device flows) — discovered by runtime type assertion, with a shared contract that "does not implement" and "returns `ErrNotSupported`" are treated identically. Providers self-register in a mutex-guarded registry via blank import. Cross-cutting security is centralised well: `HostTrusted` pins credentials to the API host with fail-closed parsing and explicit widening options; `ResolveToken` implements one credential chain (env-ref → keychain → literal → well-known env) reused by all providers; size caps on manifests/signatures are enforced at the provider (trust boundary). `repo` wraps go-git behind role interfaces and a mutex-serialised `ThreadSafeRepo`, with `aferobilly` bridging billy worktrees into afero.

On the consuming side, `pkg/setup`'s self-update path is genuinely verify-before-install: asset bytes are downloaded to memory, the checksums manifest is fetched, the detached signature is verified **over the raw manifest bytes before parsing**, the binary hash is checked against the manifest (constant-time compare, duplicate/malformed manifests rejected wholesale), and only then is the tar entry extracted to a temp file and atomically renamed over the target. The same buffer that was verified is the one extracted — no TOCTOU re-fetch. The new forge-aware initialiser is a single profile-parameterised wizard that negotiates `Authenticator`/`KeyManager` capabilities and enforces a single-credential-key config invariant transactionally. The design is strong; the findings below are mostly seams where recently-migrated pieces contradict each other.

### Findings

#### [CRITICAL] The interactive OAuth/keychain setup flow runs under a 5-second keychain timeout context

> **Spec:** [2026-07-23-setup-credential-stage-context-scoping](../specs/2026-07-23-setup-credential-stage-context-scoping.md)

**Evidence:** `pkg/setup/forge/single.go:147` creates `ctx, cancel := context.WithTimeout(context.Background(), credentials.KeychainOpTimeout)` (5s — `go/credentials/wizard.go:16`) for an already-configured check, then passes that **same ctx** through `runAuthCredentialStage` (single.go:172, 178-209) into `captureToken` → `auth.Login(ctx, i.prompter)` (single.go:274) and into `writeSingleCredential` → `credentials.Store(ctx, …)` (single.go:339). `dual.go:81-93` has the identical shape.

**Failure scenario:** the 5-second clock starts before the storage-mode form, env-var form, fetch-token confirm, device-code prompt, and the OAuth poll loop. GitHub/GitLab `device.Wait(ctx, …)` (forge-github/auth.go:57, forge-gitlab/auth.go:54) honours the deadline, and no human completes a browser device flow in <5s — so the forge-driven OAuth login (the headline capability of the forge-aware-setup migration) **always** deadline-expires and silently falls back to manual PAT entry (single.go:279-282 logs a warn and falls back, masking the bug). Any custom context-honouring credentials backend (Vault/SSM) also fails the store; the built-in OS-keychain backend only survives because it ignores ctx.

**Direction:** scope the 5s timeout to the individual keychain operations only (as `forge-bitbucket/config_adapter.go:13` and `singleStorageModeForm` correctly do); give `Login` the caller's ctx (or a device-code-expiry-derived deadline) and `Store` a fresh `KeychainOpTimeout` ctx at call time.

#### [HIGH] Self-update has no downgrade guard — a stale/rolled-back "latest" release is silently installed

> **Spec:** [2026-07-23-self-update-downgrade-guard](../specs/2026-07-23-self-update-downgrade-guard.md)

**Evidence:** `pkg/setup/update.go:606-613` — when `CompareVersions(current, latest) == 1` (current newer), `IsLatestVersion` returns `false` with the message "…please downgrade to %s". `shouldSkipUpdate` (update.go:939-952) skips only when `isLatestVersion && !force`, so `Update` proceeds to download and **install the older binary** without `--force` and without any confirmation.

**Failure scenario:** classic rollback attack. An attacker who can influence the release listing (compromised forge account, deleted/yanked newest release, or a rogue re-tag — a class of incident this project has already seen) serves an older, *validly signed* release as "latest". Signature and checksum verification both pass (they authenticate the artefact, not its recency), and the tool downgrades itself to a known-vulnerable version. The signing enforcement added in Phase 2 does not close this.

**Direction:** in the no-explicit-version path, refuse (or require `--force`) when the target version compares lower than `CurrentVersion`. Keep explicit `--version` + `--force` as the sanctioned downgrade path.

#### [HIGH] GitLab provider forwards `PRIVATE-TOKEN` across cross-host redirects

> **Spec:** [2026-07-23-forge-gitlab-redirect-token-hardening](../specs/2026-07-23-forge-gitlab-redirect-token-hardening.md)

**Evidence:** `forge-gitlab/release.go:186-190` attaches `req.Header.Set("PRIVATE-TOKEN", p.token)` after a correct `HostTrusted` check, then does `httpclient.NewClient().Do(req)`. Go's stdlib strips only `Authorization`/`Www-Authenticate`/`Cookie`/`Cookie2` on cross-host redirects; a custom header like `PRIVATE-TOKEN` is copied to **every** redirect hop. `go/httpclient/client.go:180-188`'s redirect policy only caps hop count and refuses HTTPS→HTTP — it does not strip headers.

**Failure scenario:** self-managed GitLab with object storage (`proxy_download` off) answers asset downloads with a 302 to S3/GCS — the token is sent to the storage provider. Worse, any open-redirect endpoint on the GitLab host lets a release author craft an asset link that passes `HostTrusted` (same host) and then bounces the request — token attached — to an attacker host. The other providers are safe by accident: Bitbucket uses basic auth and Gitea `Authorization: token …`, both stripped by the stdlib.

**Direction:** set a `CheckRedirect` for this download that either refuses cross-host redirects with the credential attached or re-runs `HostTrusted` per hop and deletes `PRIVATE-TOKEN` when the hop leaves the pinned host. Consider offering this in `httpclient` as an option.

#### [HIGH] forge-bitbucket holds two mutually incompatible assumptions about the downloads `self` link — either private downloads or signature/checksum resolution is broken

> **Spec:** [2026-07-23-forge-bitbucket-download-url-contract](../specs/2026-07-23-forge-bitbucket-download-url-contract.md)

**Evidence:** assets are built from `dl.Links.Self.Href` (`forge-bitbucket/release.go:451-454`). Two consumers of that same URL disagree about its shape:
- `setBasicAuthIfHostMatches` (release.go:149-159) only attaches credentials when the URL host matches `apiBase` = `https://api.bitbucket.org/2.0` — i.e. it assumes an **API-host** URL.
- `parseDownloadURL` (release.go:321-333) requires the path shape `/{workspace}/{repo}/downloads/{file}` — i.e. it assumes a **browser-host** `bitbucket.org/...` URL. An API-shaped path fails this check.

The unit tests stub browser-shaped hrefs throughout, so the contradiction is untested against the real API (whose v2 `links.self.href` values are `api.bitbucket.org/2.0/...`). Whichever shape the live API returns, exactly one half breaks: API-shaped → `DownloadChecksumManifest`/`DownloadSignature` return a hard error (not `ErrNotSupported`), which under `require_checksum`/`require_signature` **aborts every Bitbucket update** (update.go:687-689); browser-shaped → basic auth is never attached and private-repo downloads 401.

**Direction:** carry `workspace`/`repo` on the synthetic release struct instead of re-deriving them from an asset URL; make `parseDownloadURL` tolerant of both shapes; add an integration test against the live API shape.

#### [MEDIUM] Installed binary is chmod'ed to `0o111` (execute-only, unreadable)

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `pkg/setup/update.go:48-49` (`filePermExecutable = 0o111`) and update.go:1148 `s.Fs.Chmod(targetPath, filePermExecutable)`. `Chmod` **sets** the mode — after every successful update the binary is `--x--x--x`. The tool still executes (execve needs only `x`), which is why this survived, but the owner can no longer read, copy, back up, checksum, or `scp` their own binary. **Direction:** chmod to `mode | 0o111` (or fixed `0o755`), before the rename.

#### [MEDIUM] `ListReleases` is single-page in all three list-capable providers; release notes silently truncate

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** GitHub `ListOptions{PerPage: limit}` with no page loop (forge-github/release.go:130-144); GitLab likewise (forge-gitlab/release.go:153-169); Gitea likewise (forge-gitea/release.go:138-158). `GetReleaseNotes` requests exactly 100 (update.go:51, 1159). GitHub/GitLab cap `per_page` at 100 and Gitea instances commonly cap at 50 — so any history >100 releases makes `GetReleaseNotes(from, to)` report "No release notes found" or a truncated changelog. No provider handles rate-limit responses either. **Direction:** document `limit` as "first page, provider-capped" in the `Provider` contract, or paginate until `limit` is satisfied (Bitbucket's `fetchAllDownloads` already shows the pattern).

#### [MEDIUM] `repo` role interfaces leak go-git concrete types and carry no `context.Context`

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `go/repo/repo.go:95-168` — every role interface traffics in `*git.Repository`, `*git.Worktree`, `*object.File`, `plumbing.ReferenceName`, `*ssh.PublicKeys`, `transport.AuthMethod`, `*git.CloneOptions`. Network operations use the non-context go-git forms: `git.Clone` (repo.go:489), `git.PlainClone` (repo.go:572), `repo.Push` (repo.go:372), `tree.Pull` (repo.go:315) — `CloneContext`/`PushContext` exist and are unused. (a) a go-git major bump is a breaking change across every consumer and the `mocks/` tree; (b) a clone/push against a hung remote cannot be cancelled; on `ThreadSafeRepo` the mutex is held for the entire uncancellable clone, wedging every other method. **Direction:** accept as a deliberate "thin veneer" and document it, but add `ctx` to the signatures and thread `CloneContext`/`PushContext`/`PullContext` through — cheap now, breaking later.

#### [MEDIUM] `Repo.OpenLocal` initialises a new repository on *any* open error and ignores `branch` for existing repos

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `go/repo/repo.go:510-534` — `git.PlainOpen` failure of any kind (corrupt repo, permission error, not just `ErrRepositoryNotExists`) falls into `PlainInitWithOptions`; when the open succeeds, the `branch` argument is silently unused. A transient permission problem or corrupted `.git` on a real project directory gets a fresh empty repository initialised over it, destroying the caller's ability to notice the original fault. `init.go`'s `InitLocal`/`DiscoverRepository` (repo/init.go:41-52) show the project already knows this conflation is a problem. **Direction:** gate the init fallback on `errors.Is(err, git.ErrRepositoryNotExists)`; either honour `branch` on the open path or drop the parameter.

#### [MEDIUM] Provider construction semantics diverge: Gitea reads `url.api` from the config root and needs a scheme-qualified Host

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** GitHub/GitLab factories pass `forge.SubConfig(cfg, "<name>")` so `url.api` resolves as `github.url.api`/`gitlab.url.api`. The Gitea factory passes the **unsubbed root** (`forge-gitea/init.go:12`), and its adapter reads the top-level `url.api` while auth comes from `gitea.auth.*`. Separately, `ReleaseSource.Host` means a bare hostname for GitHub/GitLab (scheme prepended) but must be a full URL for Gitea (`CodebergHost = "https://codeberg.org"`). An operator who sets `Host: git.example.com` by symmetry gets a `HostTrusted` pin that can never match (no scheme → parse yields empty host → fail-closed), so tokens are silently never attached. **Direction:** make the Gitea factory sub-scope like the others and normalise Host handling in one place.

#### [MEDIUM] aferobilly silently no-ops `Chmod`, dropping executable bits on real worktrees

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `go/aferobilly/aferobilly.go:179-183` — `Chmod`/`Chown`/`Chtimes` return nil unconditionally, justified by memfs. But the adapter's flagship consumer is a go-git worktree (`repo/worktree_fs.go:39-52`), which for local clones is billy **osfs** where chmod is meaningful, and go-git derives committed file modes (100755 vs 100644) from worktree modes. A scaffolder writes a script through `WorkFS()`, chmods it +x (silently does nothing), commits — the committed blob is 100644 and the script is not executable after clone. **Direction:** delegate `Chmod` to the underlying billy fs when it implements `billy.Change` (osfs does).

#### [LOW] Tar extraction has no decompressed-size bound despite the anti-bomb comment

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** update.go:1126-1136 — "Copy file in chunks to help mitigate a decompression bomb attack", but the `io.CopyN` loop runs to EOF with no cumulative cap. The compressed download is capped at 512 MiB, but gzip expansion is unbounded — relevant when checksum/signature enforcement is off, which is the compile-time default. **Direction:** track a running total against a bound and abort.

#### [LOW] The core contract's redirect security boundary never engages for GitHub — doc/impl drift

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `forge/provider.go:56-70` specifies that callers must *refuse* a non-empty redirect URL, and update.go:814/994 duly do. But the GitHub adapter passes `httpclient.NewClient()` as `followRedirectsClient` (forge-github/release.go:147), and go-github v88 then follows the redirect itself and returns `redirectURL == ""`. Assets are fetched from `objects.githubusercontent.com` by a token-free client (no credential leak), but the "callers vet the redirect" boundary is dead code for GitHub. **Direction:** align the doc with reality, or pass `nil` and apply an explicit allowlist.

#### [LOW] Capability discovery by type assertion silently degrades under Provider wrappers

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `update.go:736`, `update_signature.go:212`, `setup/forge/ssh.go:54`, `single.go:295` all assert optional interfaces on the concrete provider. Any decorator around `forge.Provider` that forwards only the five required methods silently strips the capability interfaces — and because every caller treats "not implemented" as graceful fallback, verification quietly downgrades rather than failing loudly. **Direction:** consider an `Unwrap() Provider` convention or a `Capabilities()` accessor before any wrapper type appears.

#### [LOW] Generic single-token wizard hardcodes GitHub's PAT URL and scopes

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `single.go:513` — `https://%s/settings/tokens/new?scopes=repo,read:org,gist&description=gtb-cli` built from `profile.Host` in the profile-*generic* manual fallback. For any future GitLab/Gitea profile the URL path and scope names are wrong. Given the CRITICAL finding above, this fallback is currently the path every OAuth user actually lands on. **Direction:** move the token-creation URL template (and scope list) into `Profile`.

#### [LOW] SSH key generation writes the private key with mode 0o700 and detects passphrase protection by error-string comparison

> **Spec:** [2026-07-23-forge-repo-setup-followups](../specs/2026-07-23-forge-repo-setup-followups.md)

**Evidence:** `setup/forge/ssh.go:434` writes the private key with `dirPermUserOnly` (0o700 — a *directory* constant; convention for key files is 0600). `ssh.go:201` and `ssh.go:260` compare `err.Error() == "ssh: this private key is passphrase protected"` instead of `errors.As(&ssh.PassphraseMissingError{})` — one upstream wording change and passphrase-protected keys stop being detected. Positives: generated keys are Ed25519, passphrase-enforced (min 12), timestamped filenames prevent overwrites, upload is opt-in per key. **Direction:** use 0o600 and `errors.As` (the repo module already does this correctly — `go/repo/repo.go:430-439`).

### Strengths

The security-sensitive core is unusually well thought through. `HostTrusted` is a model of a fail-closed primitive — scheme-and-port-exact, case-correct, with widening only via named, uncomfortable options. The self-update chain gets the hard ordering right: signature over raw manifest bytes before parsing, checksum before extraction, the verified buffer being the installed buffer, atomic temp-file+rename install, size caps at every network read, and a manifest parser that rejects malformed or duplicate entries wholesale. The optional-capability contract (`ErrNotSupported` ≡ "not implemented") is documented in one place and honoured by every caller traced — there are no nil-capability dereferences anywhere. Credential hygiene is consistent: keychain errors swallowed without leaking backend text, CI refuses literal storage at two layers, tokens prompt-hidden and best-effort cleared, and the transactional `ExclusiveChanges` ordering shows real care about config-corruption edge cases. `aferobilly` is a compact, honest bridge. The codebase's comment discipline (documenting *why*, including past incidents and spec decision numbers) made this review materially easier and is itself an asset.

---

## Transport Stack Architecture Review

**Scope:** `go/transit`, `go/transport`, `go/httpclient`, `go/grpcclient`, `go/transport-metrics`, `go/transport-openapi`, `go/tls`, `go/localca`, plus GTB glue (`pkg/grpc`, `pkg/http`, `pkg/gateway`, `pkg/tls`, `internal/transportcfg`).

#### Architecture summary

The stack is cleanly layered: `transit` holds transport-agnostic middleware/interceptor primitives (chains, logging, rate limiting, retry, circuit breaking) with a shared `resilience` core (one breaker state machine, one bounded LRU token-bucket store) consumed symmetrically by the HTTP and gRPC wrappers, so client and server semantics cannot drift. `transport` is the server stack (HTTP/gRPC/gateway) wired to `go/controls` for lifecycle, `httpclient`/`grpcclient` are deliberately light client factories, `tls` is a small shared hardened-config module, and `transport-metrics` is a genuinely cycle-free Prometheus core with opt-in `grpc`/`otel`/`server` subpackages. Config knowledge lives entirely on the GTB side: `pkg/http`/`pkg/grpc`/`pkg/gateway`/`pkg/tls` are thin config→typed-settings adapters plus `internal/transportcfg.ResolveSection` for the shared resilience-section pattern. There is no duplication between GTB glue and the modules — the doc comments explicitly push pure API users to the modules, and `pkg/http` no longer re-exports the client.

Composition ordering is mostly well-reasoned and documented (breaker outside retry so one logical call = one breaker verdict; OTel ahead of logging so access logs carry trace ids; health endpoints mounted outside the middleware chain so probes are never throttled). The main problems found are: a middleware double-application bug on the gateway `Register` path, a wedgeable half-open state in the shared breaker, server TLS options that are silently clobbered (making mTLS unreachable via the managed path), and the absence of any bind-address concept (everything listens on all interfaces, including the metrics/pprof server whose own docs tell you to bind loopback).

#### [HIGH] Gateway `Register` applies the middleware chain twice

> **Spec:** [2026-07-23-transport-gateway-single-middleware-application](../specs/2026-07-23-transport-gateway-single-middleware-application.md)

**Evidence:** `go/transport/gateway/gateway.go:123-135`. `Register` calls `newWithOptions` (which at lines 94-96 wraps the handler with `o.middleware.Then(handler)` when `hasMiddleware`), then *also* forwards `transporthttp.WithMiddleware(o.middleware)` to `transporthttp.Register`, which wraps the same handler again (`go/transport/http/server.go:413-415`).

**Failure scenario:** every middleware in the chain runs twice per REST request on the managed-gateway path: the rate limiter consumes two tokens per request (halving effective capacity and double-counting toward 429), request logging emits two lines, an OTel middleware nests a duplicate server span, auth verifies the credential twice (and on failure the outer 401 is then processed again by the inner chain's writer path). The test at `gateway_register_test.go` only sets an idempotent header, so it cannot detect this. GTB's `pkg/gateway.RegisterFromConfig` inherits the bug.

**Direction:** on the `Register` path, build the *raw* mux (`o.serveMux`) and let `transporthttp.WithMiddleware` be the single application point (which is what the `WithMiddleware` doc comment at gateway.go:58-60 already describes as the intent).

#### [HIGH] Shared breaker can wedge permanently in HalfOpen; stream trials hold the slot for the stream's lifetime

> **Spec:** [2026-07-23-transit-breaker-halfopen-recovery](../specs/2026-07-23-transit-breaker-halfopen-recovery.md)

**Evidence:** `go/transit/resilience/breaker.go:113-139` — `Allow` in `StateHalfOpen` rejects when `halfOpenInFlight >= HalfOpenMaxRequests`, and there is **no time-based escape from HalfOpen** (the cooldown check at line 119 only applies to `StateOpen`). The slot is released only by the `done` callback. `go/transit/grpc/circuitbreaker.go:220-257` — for streams, `done` fires only from a terminal `RecvMsg`/`SendMsg` error or `io.EOF`.

**Failure scenario:** with the default `HalfOpenMaxRequests: 1`: (a) a long-lived healthy stream (watch/subscribe RPC) admitted as the half-open trial holds the only slot for its entire lifetime — every other RPC through that breaker is rejected with `Unavailable` even though the downstream has recovered; (b) a caller that cancels its context and abandons the stream object without a final `RecvMsg` never triggers `done`, so `halfOpenInFlight` never decrements and the breaker rejects **forever** — a permanent, unrecoverable outage of that client path. The unary paths have the same exposure if `invoker`/`next.RoundTrip` panics (`done` is not deferred: transit/grpc/circuitbreaker.go:164-172, transit/http/circuitbreaker.go:150-159).

**Direction:** add a half-open trial deadline in the core (re-open, or release the slot, after `Cooldown` in HalfOpen); for streams, treat successful stream *establishment* (or first successful `RecvMsg`) as the trial success instead of terminal outcome; invoke `done` via defer-with-recover on the unary paths.

#### [HIGH] `WithServerTLSConfig` is silently clobbered on every managed path — server mTLS is unreachable

> **Spec:** [2026-07-23-transport-server-tls-mtls-support](../specs/2026-07-23-transport-server-tls-mtls-support.md)

**Evidence:** `go/transport/http/server.go:154-158` (option), 186-189 (applied in `newServer`), then 256-262: when `tlsPair.Enabled`, `start` unconditionally replaces `srv.TLSConfig` with a fresh `tlsPair.ServerConfig()` (a bare `gtls.DefaultConfig()` + cert, `go/tls/tls.go:50-64`). When `tlsPair.Enabled` is false, `srv.Serve` is used and the custom config is never consulted.

**Failure scenario:** a caller who configures client-cert auth — `transporthttp.WithServerTLSConfig(&tls.Config{ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert})` plus `WithMTLSVerifier` — gets a server that silently serves *without* client-cert verification: `r.TLS.VerifiedChains` is always empty, so `AuthMiddleware`'s mTLS branch (`transport/http/auth.go:203`) can never succeed and every request 401s (or worse, is admitted by another configured verifier, giving a false sense that mTLS is active). The gRPC path is the same: `wrapTLS` (`transport/grpc/server.go:274-281`) always builds `pair.ServerConfig("h2")` with no override hook, so `WithGRPCMTLSVerifier` is equally dead on the managed path. Neither `tls.Pair` nor any `ServerOption` can express `ClientCAs`/`ClientAuth`.

**Direction:** make `start` respect an explicitly supplied server TLS config (load the pair's cert *into* it rather than replacing it), or extend `gtls.Pair`/`ServerConfig` with client-CA fields; at minimum document that the option only affects self-served (non-`Start`) servers and error on the conflicting combination.

#### [MEDIUM] All servers bind to all interfaces; no bind-address exists anywhere in the stack

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `go/transport/http/server.go:198` (`Addr: fmt.Sprintf(":%d", port)`), `go/transport/grpc/server.go:230` (same for the gRPC listener). `ServerSettings` (`transport/http/config.go:5-8`, `transport/grpc`) carries only `Port` (+`MaxHeaderBytes`/`Reflection`); there is no host/bind field in any settings type, option, or GTB config key. `transport-metrics/server/server.go:30-32` explicitly instructs "Bind it to a loopback or internal address (via settings.Port and your listener configuration)" — which the provided API cannot do.

**Failure scenario:** a management server, gateway, or — worst — a standalone metrics server with `WithPprof()` enabled is reachable on every interface of the host by default. `MountOn` warns when no guard middleware is present (metrics.go:86-90), but the warning is the only control; the pprof profile/trace endpoints on 0.0.0.0 are a real information-disclosure and DoS surface (heap dumps, 30-second CPU profiles). The health endpoints (`/healthz` etc.) also serve the full component health report unauthenticated on the same all-interfaces listener, outside any middleware chain by design.

**Direction:** add `Host`/`BindAddress` to `ServerSettings` (default `""` for compatibility, with the metrics server defaulting to `127.0.0.1`), thread it through `resolvePort`/listen, and expose it in the GTB config adapters.

#### [MEDIUM] Retry transport retries non-idempotent requests by default

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `go/transit/http/retry.go:133-151` — `shouldRetry` has no method/idempotency check; the only guard is `GetBody == nil` (line 142). `net/http` auto-populates `GetBody` for `*bytes.Buffer`/`*bytes.Reader`/`*strings.Reader` bodies, and the default retryable codes (line 47) include 502 and 504.

**Failure scenario:** a `POST` carrying a JSON `bytes.Buffer` through a flaky proxy gets a 502 *after* the origin processed the write; the transport silently resends it, duplicating a non-idempotent operation (double charge, double resource creation). GTB tools built on `httpclient.WithRetry(DefaultRetryConfig())` inherit this for every POST.

**Direction:** default to retrying only idempotent methods (GET/HEAD/OPTIONS/PUT/DELETE per RFC 9110) plus any method on connection-refused-before-send; offer `WithRetryAllMethods`/predicate opt-in. (Note also that `RoundTrip` mutates the caller's request at line 110, `req.Body, err = req.GetBody()`, which technically violates the `http.RoundTripper` contract — cloning the request per attempt would fix both.)

#### [MEDIUM] `RetryConfig.RetryableStatusCodes` documented default is never applied

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `go/transit/http/retry.go:32-34` documents "Default: []int{429, 502, 503, 504}", but `normalized()` (lines 64-82) only clamps `MaxRetries` and the backoff durations — it never defaults `RetryableStatusCodes`.

**Failure scenario:** a caller writes `httpclient.WithRetry(transithttp.RetryConfig{MaxRetries: 5})` (a natural reading of the field docs). `defaultShouldRetry` then does `slices.Contains(nil, code)` — no status code is ever retried; only network errors are. Retry appears configured but silently does ~nothing for 429/503, which is exactly the case people configure it for.

**Direction:** in `normalized()`, default a nil `RetryableStatusCodes` to the documented set (keep an explicit empty non-nil slice as "network errors only").

#### [MEDIUM] `...any` option variadics silently discard mistyped options — including auth interceptors

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `transport/http/server.go:457-464`, `transport/grpc/server.go:96-110` (doc: "other types are ignored"), 311-326, 442-461; the same pattern in GTB's `pkg/http/config_adapter.go:144-159, 209-227` and `pkg/grpc/config_adapter.go`.

**Failure scenario:** `transportgrpc.NewServer(settings, transportgrpc.WithInterceptors(authChain))` compiles and runs — but `NewServer` only extracts `grpc.ServerOption`, so the auth/logging/rate-limit chain is *silently dropped* and the server runs unauthenticated. The type system was traded away (understandably, to mix three option families) but with no runtime backstop, and a security control is among the things that can vanish.

**Direction:** return an error (or at least log at WARN) on any option value whose type is not one of the accepted families; it's a one-line `default:` case in each type switch.

#### [MEDIUM] gRPC `PeerKey` keys the rate limiter on `ip:port`, not client IP

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `go/transit/grpc/ratelimit.go:153-159` — `p.Addr.String()` includes the ephemeral source port, unlike the HTTP counterpart `ClientIPKey` which strips the port via `clientIP` (`transit/http/ratelimit.go:171-173`, `logging.go:372-377`).

**Failure scenario:** each new TCP connection from the same client gets a fresh, full token bucket — a client evades per-client limiting simply by re-dialing, and connection churn also rotates keys through the bounded LRU store, evicting legitimate entries. The two transports' "per-client" semantics silently differ.

**Direction:** split host from `p.Addr` (`net.SplitHostPort`) in `PeerKey`, mirroring `ClientIPKey`.

#### [MEDIUM] GTB adapters turn an invalid explicit port into a silent ephemeral-port bind

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `pkg/http/config_adapter.go:184-188, 225-227` — an out-of-range `WithPort` short-circuits to `ServerSettings{}` (port 0 → OS-assigned port), `require.NoError` + `":0"` asserted deliberately in `config_adapter_test.go:182-192, 208-220`. The transport module itself treats the same input as a hard error (`transport/http/server.go:216-226`).

**Failure scenario:** a tool wired with a bad port value (e.g. an env-var typo materialised through `WithPort`) starts "successfully" listening on a random port; probes and clients pointed at the configured port fail while the controller reports the server healthy. The framework's own transport layer would have failed loudly; the GTB layer suppresses that contract.

**Direction:** propagate the invalid port to the transport (forward it as `transporthttp.WithPort`) or return the error directly from the adapter.

#### [LOW] Circuit-breaker state enums disagree between `resilience` and `transport-metrics`

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `resilience/breaker.go:19-27` (`Closed=0, Open=1, HalfOpen=2`) vs `transport-metrics/breaker.go:12-19` (`Closed=0, HalfOpen=1, Open=2`). No current GTB code bridges them, but the obvious wiring — `cb.SetState(metrics.BreakerState(to))` inside an `OnStateChange` — silently swaps open/half-open on the dashboard.

**Direction:** align the numeric order, or give transport-metrics a `FromResilienceState`-style mapper and document the mismatch.

#### [LOW] `responseLogger` lacks `Unwrap`, breaking `http.ResponseController` passthrough

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `go/transit/http/logging.go:380-422` implements `Flush`/`Hijack` explicitly but no `Unwrap() http.ResponseWriter`; the sibling `statusRecorder` in transport-metrics does it correctly (`transport-metrics/http.go:183-185`).

**Failure scenario:** a handler behind `LoggingMiddleware` calling `http.NewResponseController(w).SetWriteDeadline(...)` (e.g. for streaming) gets `ErrNotSupported`; `io.Copy` also loses the `ReaderFrom` sendfile path.

**Direction:** add `Unwrap`.

#### [LOW] Gateway gRPC client connection is never closed

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `transport/gateway` `Register` receives `conn` but registers only the HTTP server's start/stop with the controller; GTB's `pkg/gateway/config_adapter.go:163-183` dials the conn and, unlike `NewFromConfig` (which closes on error at line 153-156), leaks it when `transportgateway.Register` fails, and neither path closes it on controller shutdown.

**Direction:** close the conn on the Register error path, and register a stop hook (or document that the conn is process-lifetime).

#### [LOW] localca: non-atomic key writes and no root-expiry guard when minting leaves

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `go/localca/store.go:139-145` — `afero.WriteFile` writes in place (a crash mid-write leaves a truncated root key, which `loadRoot`→`decodeRootPEM` then reports as a hard error rather than re-minting); `go/localca/authority.go:252-265` + `ca.go:80-125` — `ensureLeaf` checks only the *leaf's* expiry/coverage, so near the root's 10-year `NotAfter` it will happily mint a 90-day leaf outliving (or postdating) its root, which clients then reject with a confusing chain error. Permissions themselves are correct (dir 0700, keys 0600, 128-bit random serials, `MaxPathLenZero`).

**Direction:** write-to-temp + rename for key material; clamp leaf `NotAfter` to root `NotAfter` and surface a "root expiring, re-run Install" signal.

#### [LOW] `hostPinnedAuth` pins the allowed host from a concurrent first request

> **Spec:** [2026-07-23-transport-stack-followups](../specs/2026-07-23-transport-stack-followups.md)

**Evidence:** `go/transit/http/client_middleware.go:90-106` — `sync.Once` captures whichever request's host wins the race. Two goroutines making the client's first requests to different hosts means one of them (nondeterministically) sends no credential and gets an unexplained 401. The single-host assumption is documented, but the failure is silent.

**Direction:** accept the intended host explicitly in `WithBearerToken`/`WithBasicAuth` (or log at WARN when a request host mismatches the pin).

#### Strengths

This is a well-above-average stack for its size. The shared `resilience` core eliminates HTTP/gRPC breaker drift by construction; retry has full jitter, `Retry-After` honouring with a hostile-server clamp, overflow-proofed backoff, and a correct drain-and-close of response bodies before each retry; the `GetBody`-absent guard prevents silent body corruption. Server defaults are genuinely hardened: TLS 1.2 floor with AEAD-only suites, `ReadTimeout`/`WriteTimeout`/`IdleTimeout` set (covering Slowloris), 1 MiB header/body/gRPC-message caps, security headers and a fail-closed auth middleware with ambiguity rejection, redacted failure logging, and untrusted-by-default `X-Forwarded-For`. The rate-limit store is bounded with LRU eviction against key-churn memory exhaustion; ingress limiting is non-blocking while egress limiting blocks — the right asymmetry. transport-metrics is exemplary on cardinality (route *templates* with a bounded unmatched bucket, optional status-class collapsing, exemplars instead of trace-id labels, private registries, errors instead of panics on duplicate registration, `Unwrap` on its recorder). localca mirrors mkcert's proven design with tighter ECDSA key usage, and the graceful-then-forced stop pattern is consistent across both server transports. The GTB glue is a genuinely thin, non-duplicative config layer.

---

## Cross-cutting leaf modules — architectural review

### Architecture summary

The extraction has produced a genuinely clean set of leaf modules. Each has a single, crisp responsibility: `redact` (pure stdlib + regexp, zero non-test deps), `regexutil` (compile bounding only), `browser` (validate-then-delegate to `cli/browser`), `errorhandling` (cockroachdb/errors presentation), `credentials` (mode taxonomy + pluggable backend + prompter seam), `authn` (verifier primitives, no transport imports beyond the JWKS HTTP client), `observability` (thin OTLP provider factories over a shared `otelcore` endpoint parser), and `output` (rendering, the only intentionally heavy module — Charm TUI stack). go.mod files match the "dependency-light" claim; the only surprise weight is `cockroachdb/errors` dragging `getsentry/sentry-go` and `gogo/protobuf` as indirects into nearly every module (build-graph noise, not linked code). Every module ships a `depfootprint_test.go` guarding this, which is an excellent pattern.

The consuming glue in go-tool-base is correctly layered: `pkg/telemetry` redacts at the ingest boundary (`record()` redacts merged metadata; `TrackCommandExtended` redacts args/errMsg unconditionally when extended collection is on, and drops them entirely when off — `pkg/telemetry/telemetry.go:191-213`), the built-in middleware only ever calls the non-extended `TrackCommand` (`pkg/setup/middleware_builtin.go:89`), transit HTTP logging force-redacts sensitive header values even when explicitly requested (`go/transit/http/logging.go:341`), and the keychain blank-import mechanism is sound: the `credentials` package `init()` installs the stub, the `keychain` subpackage `init()` necessarily runs after it (import-dependency order), the handoff is an `atomic.Pointer`, and DCE genuinely excludes go-keyring/godbus/wincred when `cmd/gtb/keychain.go`'s blank import is omitted. The documented invariants for `browser.OpenURL` (length 8 KiB, control-char rejection covering tabs/newlines, case-insensitive scheme allowlist — whitespace and `javascript:`/`file:`/scheme-relative bypass attempts all verified rejected) and `regexutil.CompileBounded` (1 KiB cap, 100 ms timeout, `CompileBoundedTimeout` correctly min-clamps via nested `context.WithTimeout`) all hold as implemented.

### Findings

#### [HIGH] redact.String only strips URL userinfo for http/https — connection-string passwords ship unredacted

> **Spec:** [2026-07-23-redact-scheme-agnostic-userinfo](../specs/2026-07-23-redact-scheme-agnostic-userinfo.md)

`go/redact/redact.go:22-24` — the userinfo pattern is anchored to `(https?://)`. Verified empirically: `postgres://admin:s3cretpw@db.internal:5432/app` and `redis://:hunter2@cache.internal:6379` pass through `redact.String` **unchanged**, while the https equivalent is redacted. The module doc (`doc.go`) and GTB's docs claim generic "URL userinfo" stripping. Concrete failure: a failed database/AMQP/Redis connection error (`dial tcp ... postgres://user:pass@host refused`) flows through `TrackCommandExtended`'s `errMsg`, doctor-report messages, or event metadata — all of which trust `redact.String` as the last line of defence — and the password reaches the telemetry vendor. Direction: widen the scheme group to the RFC 3986 shape `[A-Za-z][A-Za-z0-9+.\-]*://` (the existing `[^/\s:@]+:[^/\s@]+@` tail already keeps false positives low).

#### [MEDIUM] errorhandling.Fatal on special errors never exits — process continues and terminates 0

> **Spec:** [2026-07-23-leaf-module-contract-fixes](../specs/2026-07-23-leaf-module-contract-fixes.md)

`go/errorhandling/handling.go:92-99` — `Check` returns early when `handleSpecialErrors` handles `ErrRunSubCommand` / `ErrNotImplemented`, so the `LevelFatal` branch (and `h.Exit`) is never reached. Verified empirically: `h.Fatal(ErrRunSubCommand)` and `h.Fatal(NewErrNotImplemented(...))` do not call the exit func. A CLI whose main does `handler.Fatal(err)` on "subcommand required" prints usage and then exits **0** — `if tool; then …` in a script treats invalid invocation as success, contrary to universal CLI convention (cobra exits 1 here). Direction: after handling the special presentation, still honour a fatal level with a non-zero exit (usage errors conventionally exit 2/64).

#### [MEDIUM] credentials.Probe can hang the setup wizard — the shipped keychain backend cannot honour its context

> **Spec:** [2026-07-23-leaf-module-contract-fixes](../specs/2026-07-23-leaf-module-contract-fixes.md)

`go/credentials/mode.go:139-156` (Probe doc: "pass a context with a short timeout … so a misbehaving remote backend cannot stall the setup flow") vs `go/credentials/keychain/keychain.go:40` — the go-keyring backend ignores the context entirely (documented at `Store`), and `Probe` calls `Store`/`Retrieve`/`Delete` synchronously. `KeychainOpTimeout` (`wizard.go:16`) is purely advisory. Concrete failure: headless Linux with a wedged D-Bus Secret Service, or a locked macOS keychain waiting on a GUI unlock dialog, blocks `Probe` — and therefore first-run setup — indefinitely, regardless of any timeout the caller derives. Direction: enforce the deadline in the `credentials` package itself (run the backend call in a goroutine, select on `ctx.Done()`, accept the abandoned-goroutine tradeoff exactly as `regexutil` does and documents).

#### [MEDIUM] output.ParseFormat documents case-insensitivity but is exact-match

> **Spec:** [2026-07-23-leaf-module-contract-fixes](../specs/2026-07-23-leaf-module-contract-fixes.md)

`go/output/format.go:29-37` — doc: "maps a **case-insensitive** string to a Format". Verified: `ParseFormat("JSON")` → `("text", false)`. Any CLI that trusts the doc for its `--output` flag silently degrades `--output JSON` to text output (breaking scripted consumers expecting JSON on stdout) instead of erroring or honouring it. Direction: `switch Format(strings.ToLower(s))` — one line.

#### [MEDIUM] telemetry: pruneSpillFiles silently violates the documented at-least-once guarantee

> **Spec:** [2026-07-23-observability-telemetry-followups](../specs/2026-07-23-observability-telemetry-followups.md)

`pkg/telemetry/spill.go:196-212` — `pruneSpillFiles` deletes the oldest spill files without any send attempt whenever the count reaches `maxSpillFiles`, while `telemetrytypes.DeliveryAtLeastOnce` is documented as "deletes spill files only after a successful send … **no data loss**". A long-offline machine (backend unreachable, 10 spill files accumulated) permanently discards events on every subsequent spill. Bounded disk usage is the right call — but the guarantee's docs must carry the bound, and note that prune frees room for exactly one file while `spillToDisk` may write several chunks per spill. Direction: document the cap as part of the delivery-mode contract and log the prune at WARN (it currently routes through `removeSpillFile` only).

#### [LOW] errorhandling: WithWriter / the Writer field is a dead no-op

> **Spec:** [2026-07-23-leaf-module-contract-fixes](../specs/2026-07-23-leaf-module-contract-fixes.md)

`go/errorhandling/options.go:18-22`, `handling.go:64,75` — `Writer` is initialised to `os.Stderr` and settable via `WithWriter`, but no production code path ever writes to it (all output goes through `h.Logger`; usage goes through the `Usage` seam). Callers configuring it get silent nothing. Remove it or route something through it.

#### [LOW] observability: endpoint trailing slash produces `//v1/<signal>` export paths

> **Spec:** [2026-07-23-observability-telemetry-followups](../specs/2026-07-23-observability-telemetry-followups.md)

`go/observability/otelcore/endpoint.go:59-64` keeps `u.Path` verbatim; every consumer concatenates blindly — `logs/logs.go:72`, `metrics/metrics.go:83`, `tracing/tracing.go:85`, and GTB `pkg/telemetry/backend_otel.go:144` (`ep.BasePath + "/v1/logs"`). `endpoint: https://collector:4318/` (a very natural config value) yields URL path `//v1/logs`, which strict collectors 404 — and the failure only surfaces as a DEBUG-level SDK error. Direction: `strings.TrimRight(u.Path, "/")` in `ParseEndpoint`.

#### [LOW] observability: Headers and Insecure silently dropped on the env-var fallback path

> **Spec:** [2026-07-23-observability-telemetry-followups](../specs/2026-07-23-observability-telemetry-followups.md)

`logs/logs.go:56-63` (same shape in metrics/tracing): when `Settings.Endpoint == ""` the exporter is built with **no options**, so a configured `s.Headers` auth token and `s.Insecure` are silently ignored. An operator who sets `telemetry.headers` but supplies the endpoint via `OTEL_EXPORTER_OTLP_ENDPOINT` exports unauthenticated with no warning. Direction: still pass `WithHeaders`/`WithInsecure` on the fallback path, or log the drop.

#### [LOW] redact: JSON-form credentials are not covered

> **Spec:** [2026-07-23-redact-scheme-agnostic-userinfo](../specs/2026-07-23-redact-scheme-agnostic-userinfo.md) (secondary scope item)

Verified: `{"access_token":"shorttok123"}` passes through unchanged — `queryCredPattern` requires `=`, and only tokens ≥ 41 chars / JWTs / prefixed tokens are caught by other rules. OAuth token-endpoint JSON bodies quoted in HTTP client errors are a common shape at exactly the surfaces this module guards. Direction: add a `"(access_token|token|secret|password|api_?key)"\s*:\s*"[^"]+"` rule mirroring the query-param key list.

#### [LOW] credentials.RegisterBackend(nil) poisons the process

> **Spec:** [2026-07-23-leaf-module-contract-fixes](../specs/2026-07-23-leaf-module-contract-fixes.md)

`go/credentials/backend.go:74-76` — `RegisterBackend` stores `&b` unconditionally; a nil `Backend` interface makes `currentBackend()` return nil and the next `Store`/`Retrieve`/`KeychainAvailable` call nil-panic. One `if b == nil { return }` (or restore the stub) closes it.

#### [LOW] authn package doc overstates its purity

> **Spec:** [2026-07-23-leaf-module-contract-fixes](../specs/2026-07-23-leaf-module-contract-fixes.md)

`go/authn/authn.go:2-3` claims the package "carries no HTTP or gRPC imports" — `jwt.go` and `jwks.go` import `net/http` for JWKS/OIDC fetching. The intended claim (no server-transport coupling) is true; the wording is not. Reword.

### Strengths

The security-invariant engineering here is well above baseline. `browser.OpenURL` validates in the right order and every bypass attempted (scheme case, leading/embedded whitespace, `javascript:`, `file:`, scheme-relative, control characters) is rejected; `regexutil`'s goroutine-leak tradeoff is real but bounded (Go's RE2 compiler always terminates via its program-size cap), correctly documented, and its `CompileBoundedTimeout` min-clamp actually works via nested-context semantics; `redact` is idempotent by construction (`keepPrefix` anchors to the pattern, not the match), fuzz-tested for its stated invariants, and correctly ordered (specific rules before the long-token fallback). The authn module is notably careful: constant-time fixed-width API-key comparison with a branchless scan, alg-confusion defence rejecting `none`/`HS*`, HTTPS enforced on both discovery and JWKS URLs, bounded JWKS documents/keys, and rate-limited single-flight refresh. `errorhandling` hints propagate correctly through wrapping (cockroachdb `FlattenHints`), and `observability.Setup`'s partial-failure teardown of already-installed global providers is a detail most codebases miss. The `depfootprint_test.go` convention and the stub-backend/blank-import DCE design in `credentials` are patterns worth writing up.

---

## Review: controls, workspace, changelog, yamldoc

### Architecture summary

The four modules are small, single-purpose extractions with clean, dependency-light public APIs. **controls** (~1.5k LOC production) is a one-shot lifecycle supervisor: a `Controller` owns a CAS-guarded state machine (`Unknown → Running → Stopping → Stopped`), spawns one supervisor goroutine per registered service (with optional restart policy, exponential backoff, and health-triggered restarts), runs interval-based async health checks with cached results, and drives a bounded, reverse-registration-order shutdown from a single message-processing goroutine. The concurrency design is unusually well documented — decision markers (D3–D9) in comments map to a `docs/explanation/concurrency.md`, and there are dedicated `-race` stress tests and goroutine-leak guards. **workspace** is a trivial marker-file walker. **changelog** has two halves: a go-git-based generator that walks the commit DAG in committer-time order and buckets commits between semver tags, and a regex-based parser for changelog markdown. **yamldoc** is a comment-preserving YAML surgeon over goccy/go-yaml's AST, with a re-parse-before-return safety net, a source-level scanner for un-round-trippable constructs, and a genuinely thoughtful comment-ownership model for removals.

The main structural weaknesses are not in the concurrency plumbing (which is careful) but at the semantic edges: changelog's two halves each fail against the release format the surrounding toolchain actually produces; controls' bounded-shutdown promise does not extend to `Wait()`; and yamldoc's safety net (re-parse) cannot see semantic damage (dangling aliases). Notably, there is no dependency/startup-ordering system at all — services start concurrently with no ordering (only shutdown is ordered), so there is nothing to cycle-detect; the README headline overstates this.

### Findings

#### [HIGH] changelog: Parse() cannot parse the ecosystem's own releaser-pleaser CHANGELOG format — returns zero releases silently

> **Spec:** [2026-07-23-changelog-real-world-format-compat](../specs/2026-07-23-changelog-real-world-format-compat.md)

`parse.go:16-19`. `releaseHeaderRe` requires a single `#` and a bare version (`^#\s+((?:v?\d+\.\d+\.\d+|Unreleased).*)`), and both entry regexes require `*` bullets (`^\*\s+…`). The changelogs produced by releaser-pleaser — including this module's own `CHANGELOG.md` — use `## [v0.1.0](url)` level-2 link-wrapped headings and `-` bullets. **Verified:** `Parse(controls/CHANGELOG.md)` returns `releases=0, from="", to=""`. Failure scenario: the self-update flow calls `ParseFromArchive`/`Parse` on real release notes, gets an empty `Changelog`, and `HasBreakingChanges()` is always `false` — breaking-change warnings silently never fire, which is the parser's whole purpose. Direction: accept `##` release headings, optional `[...]`(link) wrapping, and `-` bullets; add a fixture test using a real releaser-pleaser changelog (the current tests only round-trip the module's own `formatGroups` output, a self-consistent blind spot).

#### [HIGH] changelog: a release tag on a merge commit is never detected as a release boundary — the release vanishes

> **Spec:** [2026-07-23-changelog-real-world-format-compat](../specs/2026-07-23-changelog-real-world-format-compat.md)

`generate.go:217-230`. In `bucketCommits`, merge commits are skipped (`if c.NumParents() > 1 { return nil }`) **before** the tag-boundary lookup (`tagByCommit[c.Hash]`). A tag pointing at a merge commit (the normal shape on any repo using non-FF merges — e.g. GitLab merging a Release MR) therefore never closes a group. **Verified:** a repo with `feat` + `fix` commits, a tagged merge commit, and one post-tag commit generates a changelog with no `# v1.0.0` heading at all — all released commits are reported under `# Unreleased`. Related: bucketing by `LogOrderCommitterTime` interleaves commits from parallel branches, so a commit merged after a tag but committed before it is bucketed into the wrong release even without merge-commit tags. Direction: check `tagByCommit` before the merge-skip; longer-term, bucket by ancestry (`tagN-1..tagN` range walks) rather than committer-time linearisation. Tests only exercise linear history (`generate_test.go:27-76`).

#### [HIGH] controls: Wait() hangs forever when a service's StartFunc ignores cancellation — the bounded-shutdown guarantee doesn't cover it

> **Spec:** [2026-07-23-controls-bounded-wait](../specs/2026-07-23-controls-bounded-wait.md)

`services.go:152-162` (supervisor decrements the WaitGroup only via deferred `markStarted` when `supervise` returns, which for the no-policy path requires `srv.Start(ctx)` to return) and `controller.go:259-261` (`Wait()` = `wg.Wait()`). `Services.stop` (`services.go:326-353`) carefully abandons a context-ignoring **StopFunc** at the deadline — but if a **StartFunc** blocks ignoring `ctx.Done()` and its StopFunc doesn't unblock it (e.g. wrapping a third-party blocking `Run()` with no cancellation support), the supervisor goroutine never exits and `Wait()` hangs forever despite the controller reporting itself stopped. The README/docs promise "bounded graceful shutdown" with force-stop; the bound applies to stops, not to the wait. Direction: after `services.stop`, wait for supervisor exits with the same deadline (e.g. a `WaitContext(ctx)` variant), or document loudly that `Wait()` requires context-respecting StartFuncs.

#### [HIGH] yamldoc: replacing an anchored value silently drops the anchor, emitting a dangling alias that the re-parse safety net does not catch

> **Spec:** [2026-07-23-yamldoc-anchor-preservation](../specs/2026-07-23-yamldoc-anchor-preservation.md)

`set.go:39-56` (`setExisting` replaces `entry.Value` wholesale via `buildValueNode`; an `*ast.AnchorNode` matches neither `assignScalar` nor any anchor-aware path) and `yamldoc.go:119-131` (`Bytes()` only re-**parses**; goccy accepts an alias to an undefined anchor at parse time). **Verified:** on `defaults: &d\n  retries: 3\nprod:\n  settings: *d\n`, `Set("defaults", map[string]any{"retries": 9})` succeeds and `Bytes()` returns without error — the `&d` anchor is gone, `*d` now dangles (any consumer decoding this fails or silently changes meaning), and the replacement is mis-indented because `blockAnchor` aligned against the old `&d` token position. This violates the package's core promise. Direction: detect anchors on the node being replaced and either preserve the anchor on the new value or refuse with `ErrUnsupported`; make `Bytes()`'s validation also resolve aliases (a full decode, not just a parse). The otherwise-strong adversarial suite has no anchor-mutation cases.

#### [MEDIUM] controls: health-check Timeout is advisory only — a blocking check hangs health endpoints or wedges the async loop behind a stale "healthy" cache

> **Spec:** [2026-07-23-controls-followups](../specs/2026-07-23-controls-followups.md)

`healthcheck.go:19-31`: `runCheck` derives a timeout context but then calls `e.check.Check(ctx)` synchronously with no enforcement. A check that ignores `ctx` and blocks: (a) sync path — `Status()`/`Readiness()` (`healthcheck.go:35-44`) run it inline, so every health HTTP request hangs; (b) async path — the ticker goroutine (`controller.go:461-478`) wedges on the call, `lastResult` is never refreshed, and readiness keeps serving the last cached healthy result indefinitely — a dead dependency reported healthy forever. There is also no staleness check on cached results' `Timestamp`. Direction: run the check in a goroutine and select on `ctx.Done()`, recording a timeout `CheckResult`; optionally fail readiness when a cached result is older than N×Interval.

#### [MEDIUM] controls: unguarded blocking send in Stop() can hang the caller forever after shutdown completes

> **Spec:** [2026-07-23-controls-followups](../specs/2026-07-23-controls-followups.md)

`controller.go:272`: `Stop()` does CAS `Running→Stopping` then `c.messages <- Stop` on an unbuffered channel whose only receiver, `processControlMessages` (`controller.go:367-385`), exits once `shutdownComplete` closes. Interleaving: caller A wins the CAS in `Stop()` and is descheduled before its send; a direct-channel Stop message drives the full shutdown; `shutdownComplete` closes; the processor exits; A's send now blocks forever. The supervisors' `sendErr` (`services.go:136-141`) got exactly this select-on-`shutdownComplete` guard (D9); `Stop()` did not. Direction: apply the same `select { case c.messages <- Stop: case <-c.shutdownComplete: }` guard.

#### [MEDIUM] controls: shutdown holds the services mutex for its full duration, blocking all health reporting

> **Spec:** [2026-07-23-controls-followups](../specs/2026-07-23-controls-followups.md)

`services.go:326-353`: `stop` takes `q.mu` and holds it while sequentially awaiting every StopFunc — up to the whole `shutdownTimeout`. `status()`/`liveness()`/`readiness()` (`services.go:378, 407, 441`) all take the same mutex, so during shutdown every health/readiness probe blocks instead of failing fast — exactly when a load balancer most needs a prompt not-ready answer. Direction: snapshot the service slice under the lock, release it, then run the stop sequence.

#### [MEDIUM] yamldoc: the unsupported-construct scanner misreads block-scalar content as structure, refusing valid documents

> **Spec:** [2026-07-23-changelog-yamldoc-workspace-followups](../specs/2026-07-23-changelog-yamldoc-workspace-followups.md)

`unsupported.go:41-99`: `flowScanner` is line-based and knows nothing about block scalars. **Verified:** a block scalar containing JSON with a `#` (`template: |` … `"a": 1, # not a yaml comment` … `}`) — a very common shape for embedded templates — is flagged `multi-line flow collection with interior comments`, and `Bytes()` then refuses the whole document with `ErrUnsupported`. Failure direction is safe (refusal, not corruption) but it makes the library unusable on legitimate config files. Direction: track block-scalar regions (`|`/`>` headers plus indentation) in the scanner and skip their content lines.

#### [LOW] controls: documentation claims features and states that don't exist

> **Spec:** [2026-07-23-controls-followups](../specs/2026-07-23-controls-followups.md)

- `README.md:5` and `docs/index.md:4` headline "startup ordering" — there is no ordering mechanism: `Services.start` (`services.go:143-150`) launches all supervisors concurrently, and `docs/how-to/register-services.md:56` admits it.
- `controls.go:77`: `State` doc comment lists "(Created, Starting, Running, Stopped)" — none of `Created`/`Starting` exist; actual states are `Unknown`/`Running`/`Stopping`/`Stopped`.

#### [LOW] controls: SIGINT/SIGTERM are swallowed between NewController and Start

> **Spec:** [2026-07-23-controls-followups](../specs/2026-07-23-controls-followups.md)

`controller.go:647-649` registers `signal.Notify` at construction, but the reader goroutine only starts inside `Start()` (`controller.go:282-309`). A controller that is constructed but never started leaves the process permanently ignoring Ctrl-C/SIGTERM. Direction: move `signal.Notify` into `Start`, or `signal.Stop` on any pre-Start teardown path.

#### [LOW] controls: minor supervisor/API blemishes

> **Spec:** [2026-07-23-controls-followups](../specs/2026-07-23-controls-followups.md)

- `services.go:266-318`: after a healthy run resets the `restarts` counter, `timings.backoff` is never reset to `InitialBackoff` — a service that ran healthily for hours still waits the accumulated `MaxBackoff` before its next restart.
- `controller.go:377-380`: the `Status` control message computes `c.services.status()` and discards it — sending `Status` has no observable effect.
- The `HealthMessage` channel (`controller.go:59-65`) is dead plumbing: created, exposed via `Health()`/`SetHealthChannel`, never read or written anywhere in the package.

#### [LOW] changelog: two tags on the same commit collide — one release silently disappears

> **Spec:** [2026-07-23-changelog-yamldoc-workspace-followups](../specs/2026-07-23-changelog-yamldoc-workspace-followups.md)

`generate.go:115-118`: `tagByCommit` is `map[hash]name`; when v1.0.0 and v1.0.1 point at the same commit, one overwrites the other (iteration-order dependent). Also: an invalid `WithSinceTag` value silently disables the filter, and an empty repo returns an error from `repo.Head()` rather than an empty changelog.

#### [LOW] workspace: WithMaxDepth(0) skips even the start directory

> **Spec:** [2026-07-23-changelog-yamldoc-workspace-followups](../specs/2026-07-23-changelog-yamldoc-workspace-followups.md)

`workspace.go:67-84`: the loop is `for depth := range cfg.maxDepth`, so `WithMaxDepth(0)` performs zero iterations and returns `ErrNotFound` without ever checking `startDir` itself.

#### [LOW] yamldoc: editing inside an anchored mapping is refused with a misleading error

> **Spec:** [2026-07-23-changelog-yamldoc-workspace-followups](../specs/2026-07-23-changelog-yamldoc-workspace-followups.md)

`path.go:80-93` + verified: `Set("defaults.retries", 5)` on `defaults: &d\n  retries: 3` fails with `"defaults" is not a mapping` (`ErrNotFound`) because `asMappingNode` doesn't unwrap `*ast.AnchorNode`. The refusal is safe, but the message is wrong, and there's no supported way to edit through an anchor.

### Test quality

controls' test suite is the strongest of the four for race paths (`lifecycle_race_test.go`: 20-iteration pre-loaded-signal stress, 40-iteration goroutine-leak guard, white-box cancel-wiring spy, broad lifecycle coverage). The gaps are exactly where the findings live: no ctx-ignoring StartFunc test, no blocking-health-check test, no post-shutdown channel-send test. changelog's tests are thorough for linear history and its own emitted format, but have zero merge/branch fixtures and no real-world changelog fixture — which is how both HIGH findings survived. yamldoc's adversarial/hardening suites are excellent but contain no anchor-mutation or block-scalar-with-braces cases.

### Strengths

These are well-crafted extractions. controls' concurrency work is exemplary in its documentation discipline — every non-obvious ordering decision carries a D-number tracing to an explanation doc, the `sendErr`/`shutdownComplete`/CAS patterns are correct where applied, panic recovery guards every user callback, and the fail-closed readiness default is the right call. yamldoc's design philosophy (policy-free, refuse-rather-than-corrupt, re-parse before returning, comment ownership as a positional question) is unusually mature for a 1.3k-LOC module. workspace is exactly as small as it should be. changelog's parser/generator split and archive extraction are clean; its defects are fixture blindness, not structural.

---

## Architectural review: GTB framework core (props, cmd, docs, internal commands)

### Architecture summary

GTB's framework core is now a genuinely thin layer over ~27 extracted modules. `pkg/props` provides the DI container: a plain struct (`Props`) with a `Tool` metadata block (feature flags, release source, update/telemetry/signing policy), a live `*config.Store`, a union-filesystem `Assets` abstraction with structured-merge semantics, and a set of narrow provider interfaces. `pkg/cmd/root` is the composition root: `NewCmdRootWithOptions` builds the cobra tree, registers feature-gated built-ins (update/init/mcp/docs/doctor/config/telemetry/changelog/man/version), seals a process-global middleware registry (recovery/timing/telemetry), and installs a single `PersistentPreRunE` that — with `cobra.EnableTraverseRunHooks` — performs the full bootstrap for every command: flag extraction → config-store construction (embedded defaults → tool assets → files + project-local `.<tool>.yaml` → env → changed flags) → watch/hot-reload wiring → logging configuration → telemetry consent + collector → update check. `Execute` adds signal-aware context handling (128+signum exits, second-signal force-quit) and a once-only telemetry flush on every exit path.

The design is coherent and unusually well-commented; the ordering (config before logging, one pinned `View` per bootstrap, missing-config gate with per-command relaxation via annotations) is deliberate and documented against specs. The residual `pkg/` surface (`pkg/vcs`, `pkg/tls`, `pkg/gateway`, `pkg/http`, `pkg/grpc`, `pkg/chat`) is consistently "GTB config-key adapters + re-export glue", which is the right shape post-extraction. The main structural weaknesses are at the edges of the bootstrap: cobra's auto-generated commands (help/completion/`__complete`) fall through every gate the built-ins were carefully exempted from, interactive prompts live inside the pre-run without the TTY guard used everywhere else, and the Props contract ("which fields may be nil when") is enforced only by convention, with the narrow provider interfaces almost entirely unused in practice.

### Findings

#### [HIGH] Framework bootstrap runs (and fails) for cobra's auto-generated help, completion and `__complete` commands

> **Spec:** [2026-07-23-bootstrap-auxiliary-command-exemptions](../specs/2026-07-23-bootstrap-auxiliary-command-exemptions.md)

- Evidence: `pkg/cmd/root/root.go:784-867` (pre-run does config-gate, consent prompt, update check), `pkg/setup/update.go:572-585` (skip list is only `{"update","auth","init","version"}` matched against leaf `cmd.Use`), and cobra v1.10.2 `command.go:955-997` (persistent pre-run hooks execute for any runnable command — the `help`, `completion` and hidden `__complete` commands are all runnable).
- Failure scenarios (verified by code path): on a fresh install of a scaffolded tool (InitCmd enabled by default, `AutoInitialise` opt-in), `resolveBootstrapConfig` returns `ErrNoConfigFile`, so **`tool help` and `tool completion bash` exit non-zero with "failed to load configuration: no config file found"** (no hint to run `init` is attached), and shell tab-completion (`__complete`) errors on every keystroke. Where config exists, every tab-completion still pays the full bootstrap including the throttled-but-sometimes-real network update check and (for telemetry-enabled tools) the consent prompt attempt.
- Direction: exempt `helpCommandName`, `cobra.ShellCompRequestCmd`/`ShellCompNoDescRequestCmd` and `completion` in the pre-run (fast-path return like the `InitCmd` branch at root.go:796), and match skippable commands by `Name()`/annotation rather than `Use`. Attach a `WithHint("run '<tool> init' first")` to the `ErrNoConfigFile` path while there.

#### [HIGH] Interactive huh prompts inside the root pre-run have no TTY guard — MCP stdio and piped-stdin hang/corruption risk

> **Spec:** [2026-07-23-prerun-prompt-tty-guard](../specs/2026-07-23-prerun-prompt-tty-guard.md)

- Evidence: `promptTelemetryConsent` (`pkg/cmd/root/root.go:1019-1076`) and the update prompt in `handleOutdatedVersion` (`root.go:546-599`) call `form.Run()` relying on the comment "without a usable TTY … form.Run returns an error". Everywhere else in the codebase interactive flows are explicitly gated on `utils.IsInteractive()` (`pkg/setup/init.go:170`, `pkg/setup/update.go:314`, `pkg/cmd/config/edit.go:62`, `internal/cmd/feature_toggle.go:72`, …) — a convention adopted precisely because huh forms hung on non-terminal stdin in the resolved e2e init-hang incident (MR !157).
- Failure scenario: `tool mcp start` is spawned by an MCP client with stdin as the open JSON-RPC pipe. On a machine where telemetry is feature-enabled but unconfigured (gtb ships exactly this), the consent form runs against the pipe — it can block indefinitely or consume protocol bytes; the same applies to the update prompt for `UpdatePolicyPrompt` tools. Note also the consent skip checks only the `--ci` config flag (`view.GetBool("ci")`, root.go:1042), not the `CI` env var that the forge initialisers honour (`pkg/setup/forge/profile.go:191`), so real CI runs re-attempt the prompt on every invocation.
- Direction: gate both prompts on `utils.IsInteractive()` (and consider honouring `CI=true`), and add `mcp` to the bootstrap fast-path exemptions.

#### [HIGH] `tool version` fails hard (exit non-zero, nothing printed) when the release source is unreachable

> **Spec:** [2026-07-23-version-command-offline-degradation](../specs/2026-07-23-version-command-offline-degradation.md)

- Evidence: `pkg/cmd/version/version.go:79-82` — `GetLatestVersionString` failure is returned as an error instead of degrading, even though the `NewUpdater` failure five lines earlier *does* degrade to printing local info. Timeout is 60s (version.go:21).
- Failure scenario: any offline/air-gapped machine, or a release-source outage: `tool version` waits up to 60s then exits non-zero without printing the version/commit/date it already has in hand — the one command users run to diagnose exactly such situations.
- Direction: mirror the `NewUpdater`-failure branch — log a warning and print local version info; the network check should be best-effort (and arguably behind a `--check` flag).

#### [MEDIUM] Startup update check can block any command for up to 5 minutes

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `checkForUpdates` (`pkg/cmd/root/root.go:465-495`) runs `IsLatestVersion` on `cmd.Context()` with no dedicated deadline; the underlying release lookup uses `updateTimeout = 5 * time.Minute` (`pkg/setup/update.go:54-58`, `update.go:1066-1084`) — a budget sized for downloads, shared by the passive pre-run check.
- Failure scenario: a slow/black-holing network (captive portal, firewalled release host) stalls every command past the 24h throttle for up to 5 minutes before the error is logged and execution continues. First run after install is never throttled.
- Direction: wrap the pre-run check in its own short `context.WithTimeout` (a few seconds); keep the 5-minute budget for explicit `update` runs.

#### [MEDIUM] Nil-`Version` dereference in the update check path, despite guards everywhere around it

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `pkg/cmd/root/root.go:490` — `props.Logger.Debug("Version check results", "version", props.Version.GetVersion(), …)` dereferences the interface unconditionally (args are evaluated regardless of log level). The surrounding code explicitly anticipates nil: `shouldSkipUpdateCheck` guards (root.go:509), `warnIfBehindCached` guards (root.go:633), `resolveVersionString` exists for exactly this (root.go:1078-1086), and `setup.NewUpdater` guards (`pkg/setup/update.go:335-339`). `pkg/cmd/version/version.go:47` has the same unguarded deref.
- Failure scenario: a downstream tool hand-builds `Props` without `Version` (documented as nilable, nothing defaults it): the nil threads past the guard at root.go:509 straight into a panic at root.go:490 once the update interval elapses.
- Direction: use `resolveVersionString` at both sites — or better, default `props.Version` in `NewCmdRootWithOptions` the way `Collector` and `ErrorHandler` are defaulted.

#### [MEDIUM] Props is a god object in practice; the narrow provider interfaces are ornamental

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: ~148 production call sites take `*props.Props` wholesale; of the 12 interfaces in `pkg/props/interfaces.go`, `ConfigReader`, `ConfigFSProvider`, `FileSystemProvider`, `VersionProvider`, `ErrorHandlerProvider`, `TelemetryProvider`, `CoreProvider` and `LoggingConfigProvider` have **zero** production consumers outside `pkg/props` itself; only `LoggerProvider` (10), `ConfigProvider` (8), `AssetProvider` (1) and `ToolMetadataProvider` (1) are used. There is no constructor: which fields may be nil is contract-by-comment (`Config` nil until the pre-run and permanently nil on the `init` path; `Logger` nil ⇒ panic on the first `props.Logger.Debug` in `projectConfigPaths` root.go:129).
- Failure scenario: any code reachable from the `init` command tree (or from a tool that builds its tree without `NewCmdRoot*`) touching `props.Config.View()` panics — `Store.View()` is not nil-receiver-safe. `ViewOrNil`/`SlogLogger` exist but nothing forces their use.
- Direction: either commit to the interfaces (adopt them at the ~20 highest-traffic sites and prune the rest as dead API pre-1.0), or drop the unused ones. Add a `props.New(...)`/validation step that makes the nil-field contract checkable at construction.

#### [MEDIUM] Process-global middleware registry binds the first root's Props forever

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `registerGlobalMiddlewareOnce` (`pkg/cmd/root/root.go:894-906`) registers `WithRecovery(props.Logger)`, `WithTiming(props.Logger)`, `WithTelemetry(props)` and seals; a second `NewCmdRoot` in the same process silently reuses closures over the *first* Props (`pkg/setup/middleware.go:40-69`). Any downstream `setup.RegisterMiddleware` call made after the first root is built panics.
- Failure scenario: tests or an embedding process building two roots get root B's commands timed/telemetered/recovered against root A's logger and collector; a tool author registering feature middleware after `NewCmdRoot` gets a runtime panic with no compile-time signal about the required ordering.
- Direction: move the middleware chain onto `rootState`/the `Command` tree (per-root), or at minimum have `Chain` resolve Props via the command context instead of closure capture.

#### [MEDIUM] Project-local `.<tool>.yaml` is fully trusted and can weaken update-verification and telemetry posture

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `discoverProjectConfig` (`pkg/cmd/root/root.go:140-160`) walks from CWD to `/` and layers any `.<tool>.yaml` found *above* the user's own config (root.go:114-132). The same resolved view feeds `resolveRequireChecksum`/`resolveRequireSignature`/`resolveKeySource` (`pkg/setup/update.go:345-361`), `update.policy`, `update.check_interval`, and `telemetry.enabled`.
- Failure scenario: cloning a hostile repository that ships `.gtb.yaml` (or `.<anytool>.yaml`) with `update: {require_signature: false, require_checksum: false}` silently downgrades self-update verification for every command run inside that tree; `telemetry.enabled: true` flips consent. The compiled-in release source limits full exploitation, but the security-posture keys should not be overridable by an unreviewed repo file.
- Direction: apply an allowlist (or denylist covering `update.*` security keys, credentials and telemetry consent) to the project-local layer, or require a one-time trust acknowledgement à la `direnv allow`.

#### [MEDIUM] The docs AI assistant hardcodes GTB's identity into every downstream tool

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `pkg/docs/ask.go:80` — the system prompt begins `"You are a helpful assistant for 'GTB' (also known as 'als'). …"` in framework code used by every scaffolded tool's `docs`/`docs ask` commands (`pkg/cmd/docs/docs.go:62-64`). "als" appears to be a leftover from a predecessor tool.
- Direction: interpolate `p.Tool.Name` (and drop the "als" alias).

#### [MEDIUM] Config hot-reload does not re-apply logging configuration

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `configureLogging` runs once in the pre-run (`pkg/cmd/root/root.go:844`); `cfg.Watch` is wired (root.go:825-835) with only `OnReloadError` — no reload hook re-reads `log.level`/`log.format`.
- Failure scenario: for long-running commands (`docs serve`, controls-based services — the main beneficiaries of hot-reload), editing `log.level: debug` reloads the store but the logger level never changes, contradicting the hot-reload story the store wiring exists for.
- Direction: register a reload callback that re-runs `configureLogging` against a fresh view (debug flag still winning).

#### [LOW] Assets structured-merge silently swallows malformed bundles; minor fs-contract violations

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `openMergedStructured` ignores per-bundle errors (`pkg/props/assets.go:162-166` — `processAssetFile` error → `continue`), so a syntactically broken embedded `config.yaml` in one bundle silently vanishes from merged defaults with no log. `mergedFileInfo.Name()` returns the full path, not the base name (`assets.go:557`), violating the `fs.FileInfo` contract; `mountedFS` implements only `Open`, so `ReadDir`/`Glob` over a mounted prefix quietly return nothing (`assets.go:456-471`). Also `Slice`, `Merge`, `For`, `Mount` have no production callers.
- Direction: at minimum log skipped-with-error bundles; consider trimming the unused `Assets` surface pre-1.0.

#### [LOW] `SkipUpdateCheck` matches fragile `Use` strings

> **Spec:** [2026-07-23-bootstrap-auxiliary-command-exemptions](../specs/2026-07-23-bootstrap-auxiliary-command-exemptions.md) (folded in)

- Evidence: `pkg/setup/update.go:572-577` compares `cmd.Use` against `{"update","auth","init","version"}`. `Use` may carry an args suffix and collides with any downstream command coincidentally named `auth`; `mcp`, `doctor`, `help`, `completion` are not exempt. The codebase already has the right mechanism — the `FeatureAnnotation` stamped by `setup.Wrap`, used for exactly this reason at root.go:796.
- Direction: match by `FeatureOf(cmd)`/annotation.

#### [LOW] `validateConfig` checks the pre-forge-migration key `github.token`

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `pkg/cmd/root/root.go:1189-1195` warns on `github.token`, while doctor and the credential system use `github.auth.value` (`pkg/cmd/doctor/checks.go:102`). The stale key means the warning never fires for the current schema.
- Direction: align the key list with the credential-resolution chain, or derive it from `literalCredentialKeys`.

#### [LOW] Doctor default checks are not feature-aware

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `DefaultChecks` (`pkg/cmd/doctor/doctor.go:117-126`) always includes `checkAPIKeys`, which warns "no AI provider API keys configured" even for tools with `AiCmd` disabled (the default); `checkGit` warns whenever CWD isn't a git repo regardless of whether the tool uses git; `checkGoVersion` reports the Go version *the binary was compiled with*, which no end user can act on.
- Direction: gate the AI-key check on `AiCmd`/chat feature enablement, make git a registered check of the features that need it, and reconsider the Go-version check's audience.

#### [LOW] Scaffolded telemetry auth env fallback is not base64-encoded but is sent as `Basic <value>`

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md) (template fix batched with the generator follow-ups)

- Evidence: the generator emits `otelAuth = v` raw from `OTEL_API_KEY` (`internal/generator/templates/skeleton_root.go:64-78`) yet the header is rendered `"Authorization": "Basic " + otelAuth` (`skeleton_root.go:281-283`). gtb's own root encodes `base64(instanceID + ":" + raw)` (`internal/cmd/root/root.go:49-56`).
- Direction: encode in the generated `init()` (or document that the env var must be pre-encoded).

#### [LOW] `init <provider>` is config-gated on a fresh install; stranded remnants in `pkg/utils`

> **Spec:** [2026-07-23-bootstrap-auxiliary-command-exemptions](../specs/2026-07-23-bootstrap-auxiliary-command-exemptions.md) (init-gating part folded in)

- Evidence: the pre-run's InitCmd exemption matches only the init leaf (`pkg/cmd/root/root.go:796` via `FeatureOf`), while `init github`/`init bitbucket` are wrapped with their *provider* feature (`pkg/cmd/initialise/init.go:104-113`), so on a machine with no config file they fail the missing-config gate instead of running. Separately, `pkg/utils/main.go:10-54` — the `Instructions` map (kubectl/az/kubelogin/terraform/terragrunt/aws/gh) and `GracefulGetPath` have **zero** callers anywhere in the repo; leftovers from a pre-extraction internal DevOps tool. `IsInteractive` is the only live symbol in the package.
- Direction: treat any command in the init subtree as exempt; delete `Instructions`/`GracefulGetPath` or move `IsInteractive` somewhere honest. The other remnants reviewed — `pkg/vcs` (+`/repo`), `pkg/tls` (facade + `Resolve`), `pkg/gateway`, `pkg/http`, `pkg/grpc`, `pkg/chat`, `pkg/osinfo`, `pkg/telemetrytypes` — are genuine config-key/adapter glue with clear doc comments explaining why they stay; they should **stay** (though `pkg/vcs`'s name now oversells a 37-line config adapter — a rename to something like `pkg/forgecfg` at the next breaking window would help).

#### [LOW] Feature-default truth is duplicated

> **Spec:** [2026-07-23-gtb-framework-followups](../specs/2026-07-23-gtb-framework-followups.md)

- Evidence: `DefaultFeatures` (`pkg/props/tool.go:37-44`) and `isDefaultEnabled` (`tool.go:256-265`) encode the same enabled-by-default set independently, with a comment asking humans to keep them (plus `AllFeatures`) in sync.
- Direction: derive one from the other.

### Strengths

The bootstrap pre-run is exceptionally well-reasoned for its scale: layer precedence is a declaration rather than a merge sequence, the missing-config gate distinguishes "no file" from "empty file" to drive auto-initialise, snapshot-pinned views prevent read-straddle across reloads, and `EnableTraverseRunHooks` plus the shadow-hook debug warning closes the classic cobra footgun of a child pre-run silently replacing the framework's. `Execute`'s signal handling (graceful-then-force, 128+signum, once-only telemetry flush on every exit path) is textbook. The feature/annotation system (`setup.Wrap`, `FeatureOf`, `SkipConfigCheck`, MCP exposure tri-state) gives commands typed, rename-safe identity instead of string matching — where bugs were found above, it is mostly where that mechanism *wasn't* used. Self-update is defence-in-depth done properly, the nil-safety adapters (`ViewOrNil`, `SlogLogger`, `ToSlog`) show real care at the boundaries, and the post-extraction adapter packages are uniformly small, documented, and honest about why they remain in GTB.

---

## Architectural review: `internal/generator/`

#### Architecture summary

The generator is a manifest-centred scaffolding engine with four deliberate rendering subsystems, split by output type rather than by accident: (1) **jennifer** (`templates/*.go`) builds generated Go files (`cmd.go`, `init.go`, root command) as ASTs, giving free correct quoting of injected strings; (2) **`text/template` embedded assets** (`assets/skeleton*`) render non-Go files (CI, goreleaser, docs, justfile) with a shared `templateFuncMap` of per-sink escape helpers; (3) **dave/dst** performs surgical edits on *existing* user files (subcommand (de)registration in a parent's `cmd.go`, hook-stub injection into a preserved `main.go`); (4) a **restricted overlay engine** (`templatesource*.go`) renders operator-supplied template overlays with containment checks, a protected-path denylist, and a metadata-only data contract. The split is coherent — each mechanism matches the risk profile of what it writes — and the shared `CommandPipeline` (pipeline.go) genuinely unifies the generate/regenerate tails. The ~15.4k production LOC is mostly justified by breadth (docs engine, AI repair loop, template sources, dry-run, git init); the real structural waste is a family of near-duplicate manifest tree-walkers.

The manifest (`.gtb/manifest.yaml`) is the single source of truth; SHA-256 hashes distinguish generated content from user edits, driving a prompt/deny/allow overwrite protocol. Operator-owned seed files are preserved unconditionally, even under `--overwrite allow` (skeleton.go:709). Properties the renderer cannot recover from source are recorded in an annotated `pkg/cmd/root/provenance.go`, enabling the from-scratch `regenerate manifest` reconstruction. Considerable, test-backed effort has gone into byte-stable round-trips. Real-FS generation, however, is *not* atomic: files are written in place one-by-one, with the manifest/hash persistence at the end — a mid-run failure leaves a half-updated tree whose next regenerate flags every already-written file as a "user modification" conflict.

#### Findings

#### [HIGH] `ValidateManifest` does not cover several fields that render into CI-executed files and generated Go, contradicting its own contract

> **Spec:** [2026-07-23-generator-manifest-validation-hardening](../specs/2026-07-23-generator-manifest-validation-hardening.md)

**Evidence:** `validate.go:995-1009` (`validateManifestReleaseSource` checks only `Host` and `Owner`); `validate.go:905-917` (`validateManifestCommands` checks only `Name` — no flags, no descriptions); `validate.go:921-935` (`validateManifestSigning` skips `ExternalKeyEmail` and `KeySource`). Meanwhile the regenerate path renders, unescaped:

- `assets/skeleton-gitlab/.gitlab-ci.yml:57` — `repositories: '["{{ .Repo }}"]'` where `Repo = ReleaseSource.Owner + "/" + ReleaseSource.Repo` (regenerate.go:501); `ReleaseSource.Repo` is **never validated on the manifest path** (only the CLI `--repo` flag goes through `ValidateRepo`). `.ModulePath` (also containing `Repo`) renders raw into `.goreleaser.yaml:22-24`, `.golangci.yaml:81-83`, `.mockery.yml:16`.
- `templates/command.go:664` — `jen.Id(flag.Default)` emits a manifest flag's `default` **verbatim as Go code** when `default_is_code: true`; manifest flags are entirely unvalidated (`ValidateFlagName`/`ValidateFlagType` run only on the `generate add-flag` CLI path).
- `provenance.go:35,115-127` — `Signing.ExternalKeyEmail`/`KeySource` are written raw into the `// gtb:signing` annotation line of the generated `provenance.go`.

**Failure scenario:** the codebase's own stated threat model (validate.go:868-872: "a tampered manifest fails fast before driving file writes") is not met. A manifest with `repo: x", "hostile/repo` or a quote/newline-bearing value passes `ValidateManifest`, and `gtb regenerate project` writes it into `.gitlab-ci.yml` — a CI-executed file — and `.goreleaser.yaml`. A tampered flag `default` + `default_is_code` compiles arbitrary Go into the regenerated tool.

**Direction:** extend `validateManifestReleaseSource` to run `ValidateRepo`-style segment rules on `Repo` (and restrict `Type` to `github|gitlab` unconditionally); walk `ManifestFlag`s through `ValidateFlagName`/`ValidateFlagType` in `validateManifestCommands`; add an email/token character-class check for `ExternalKeyEmail` and an enum check for `KeySource`. Alternatively pipe `{{ .Repo }}`/`{{ .ModulePath }}` through `escapeYAML` where the sink allows it.

#### [MEDIUM] `gtb remove command` deletes protected commands — protection is enforced on generate and MCP-toggle but not on removal

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

**Evidence:** `removal.go:11-62` (`Remove` → `performRemoval` → `FS.RemoveAll(cmdDir)`) contains no `Protected` check; contrast `commands.go:242-274` (`checkProtection` blocks generation) and `generator.go:209-212` (`SetMCPEnabled` refuses with `ErrCommandProtected`).

**Failure scenario:** an operator marks a command protected precisely because it carries hand-written logic, then `gtb remove command --name x` deletes the entire directory — including the protected `main.go` — without any prompt.

**Direction:** in `performRemoval`, look up the manifest entry first and refuse (or require `--force`) when `Protected` is true, mirroring `SetMCPEnabled`.

#### [MEDIUM] `cleanupDocumentation` is doubly broken: it resolves the parent path *after* the manifest entry was deleted, and only knows the legacy flat layout

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

**Evidence:** `removal.go:64-79`. (a) It calls `g.FindCommandParentPath(g.config.Name)` — but `performRemoval` (step 2, `removeFromManifest`) has already deleted the command from the manifest, so the lookup always fails; for a nested command `parent/child` it therefore removes `docs/commands/child` instead of `docs/commands/parent/child`. (b) The path is hardcoded to `docs/commands/…`, but the Diátaxis layout — the **default** for all newly generated projects (`skeleton.go:809`) — stores command docs under `docs/reference/cli/<path>.md` (`docs.go:707-721`). So on every current-default project, removal cleans up nothing, and the commands index / mkdocs nav regeneration then runs against a tree still containing the dead page.

**Direction:** capture the parent path (it is already in `g.config.Parent`) before mutating the manifest, and route the doc path through the same `prepareDocsContext`/`commandDocRelPath` machinery generation uses.

#### [MEDIUM] Provenance KV encoding silently corrupts values containing spaces, breaking the from-scratch manifest reconstruction it exists for

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

**Evidence:** `provenance.go:184-209`. `encodeKV` joins `key=value` pairs space-separated and `decodeKV` splits on `strings.Fields`. A **local** template source `Location` is a filesystem path and `validateTemplateLocation` (templatesource_validate.go:80-108) **permits spaces**. An operator adds a local overlay at `/home/me/My Templates/gtb` — the provenance file records it, and a later from-scratch `gtb regenerate manifest` reconstructs `location=/home/me/My` and silently drops the rest. The unvalidated `ExternalKeyEmail` has the same round-trip fragility.

**Direction:** percent-encode or quote values in `encodeKV`, or reject spaces in `validateTemplateLocation` for local sources.

#### [MEDIUM] Generation and regeneration are non-atomic on the real filesystem; a mid-run failure leaves a half-written tree that the hash system then misclassifies

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

**Evidence:** `skeleton.go:458-483` and `regenerate.go:94-140` write files directly to the target FS in sequence; project hashes are persisted only at the end (`regenerate.go:570`). The dry-run path proves a copy-on-write overlay (`dryrun.go:99-131`) is already available but the real path never uses it. Pipeline step 4 treats the manifest write as *advisory* (`pipeline.go:87-90`).

**Failure scenario:** regenerate dies halfway: files 1..N carry new content while the manifest still stores old hashes. The next `regenerate` flags every one of those *generator-written* files as "manually modified" and interrogates the user file-by-file (or, under `--overwrite allow`/`--force`, indistinguishably overwrites genuinely user-modified files in the same batch).

**Direction:** run the real generation through the existing overlay FS and materialise atomically at the end, persisting the manifest as part of the same commit step; or at minimum persist hashes incrementally.

#### [MEDIUM] Boilerplate docs render command/flag descriptions into Markdown with no escaping and no validation

> **Spec:** [2026-07-23-generator-manifest-validation-hardening](../specs/2026-07-23-generator-manifest-validation-hardening.md) (folded into the validation-gate hardening scope)

**Evidence:** `docs.go:401,412,436,452` — `fmt.Fprintf(sb, "| `--%s` | %s | … |", f.Name, f.Description)` builds Markdown via `strings.Builder`, so the `templateFuncMap` escape pipes never apply; `ManifestCommand.Description`, `LongDescription`, and `ManifestFlag.Description` are never validated on the manifest path (CLI `--short`/`--long` aren't validated either).

**Failure scenario:** a description containing `|` or a newline breaks the flags table of every generated doc; a hostile manifest description injects arbitrary Markdown/HTML into the docs site the project publishes — potential stored XSS in a downstream artifact. This directly contradicts the CLAUDE.md claim that "every user-influenced field … is piped through one of the helpers at non-code render sites".

**Direction:** route these builder sites through `escapeMarkdown` and a table-cell variant (escape `|`, collapse newlines), and/or add description validation to `ValidateManifest`.

#### [LOW] `verifyProject` swallows manifest decode errors

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

`generator.go:259-262` — `m, _ := g.loadManifest(); if m == nil { return nil }`. A present-but-corrupt manifest passes project verification (and skips the version-compatibility gate). Similarly `regenerate.go:82-85` silently skips all post-processing when `collectSkeletonHashes` errors, with no log.

#### [LOW] Duplicate encode helpers with a stale, now-false comment

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

`manifest.go:64-81` — `MarshalManifestFile`'s doc claims a difference from `EncodeManifestFile`, but both now delegate to the same `marshalManifestBytes`. Collapse to one helper.

#### [LOW] Stream-of-consciousness scratch comments and dead fallback code left in production

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

`generator.go:158-179` (`SetProtection`: "Wait, we can modify the Generator receiver… But sticking to the plan…"), `manifest.go:277-280`, and `skeleton.go:122-137` — `extractReleaseProvider`'s reflection fallback is dead per its own companion comment (regenerate.go:488-491).

#### [LOW] Seven hand-rolled manifest tree-walkers plus twice-duplicated dedupe logic

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

`findCommandAt`, `findCommandRecursive`, `findCommandPathRecursive` (manifest_query.go:32,136,53), `updateCommandRecursive` (manifest_update.go:140), `updateCommandHashRecursive` (manifest_update.go:262), `updateProtectionRecursive` (manifest_query.go:194), `removeFromCommandRecursive` (manifest_query.go:252) — all reimplement the same walk-parent-path-then-act recursion, each dragging its own `//nolint:gosec // G602` annotations. The duplicate-name rename block appears verbatim twice (manifest_scan.go:132-146 and 289-299). One generic `walkCommandPath` plus a visitor collapses ~150 LOC and five nolint suppressions.

#### [LOW] Misleading version-gate error for dev-generated manifests, unbounded port digits

> **Spec:** [2026-07-23-generator-followups](../specs/2026-07-23-generator-followups.md)

`generator.go:281-283` — a manifest with `gtb: dev` yields "current gtb version (X) is lower than the version specified in the manifest (dev)"; `validate.go:471-482` accepts port `99999`.

#### Strengths

The security engineering here is well above typical generator code: a two-layer defence (tight NFC-normalised character-class validators *plus* per-sink escape helpers, both fuzz-tested), a single audited chokepoint in front of the command-directory write/RemoveAll sinks (`containedCommandPath`/`joinContained`, ast.go:666-691), and a template-overlay engine with pre-render containment, a protected-path denylist, size bounds, and a scheme-restricted fetch validator. The sanitise-not-abort regenerate is a thoughtful availability/security balance, and the manifest byte-stability work shows real discipline — as does the codebase's habit of naming its own past defects in comments beside the fix. Test volume is substantial (389 test functions), and the escape-helper coverage across the embedded skeleton assets is, with the docs-builder exception above, systematic and correctly sink-matched. The findings are mostly at the seams — the manifest validation gate lagging behind the fields the renderers actually consume — rather than in the core mechanisms.

---

## Ecosystem review: cross-cutting system view (GTB + 51 extracted modules)

Data sources: every `go.mod` verified against **remote HEAD** (`git fetch origin HEAD` + `git show FETCH_HEAD:go.mod` — local clones were stale), remote tags via `git ls-remote --tags`, GTB `go.sum`, and `.gitlab-ci.yml` component pins across all 51 module repos plus GTB.

### Dependency-graph summary

The graph is a clean **DAG — no cycles, no near-cycles, no replace directives anywhere**. Five layers:

- **L0 (leaves, no internal deps):** tls, redact, yamldoc, aferobilly, credentials, regexutil, authn, controls, observability, output, browser, changelog, workspace, errorhandling, chat (core)
- **L1:** transit→redact; grpcclient→tls; localca→tls; config→yamldoc; repo→aferobilly; chat-{anthropic,gemini,openai}→chat
- **L2:** httpclient→{tls, transit}; 19 config adapters→config (3 of them also →config-json)
- **L3:** forge→{credentials, httpclient}; transport→{authn, controls, grpcclient, redact, tls, transit}
- **L4:** forge-{github,gitlab,gitea,bitbucket}→{forge, httpclient}; transport-{metrics,openapi}→transport
- **L5:** GTB — 28 direct `gitlab.com/phpboyscout/go/*` requires + signing/signing-aws-kms

No layering violations found (no leaf depends on a heavy module). The high-fan-in chokepoints are **config** (19 adapters + GTB), **tls** (httpclient, grpcclient, localca, transport → transitively the entire forge and transport families), **httpclient** (forge + all 4 providers), and **yamldoc** (config's only internal dep, hence under everything config touches).

```mermaid
graph BT
    yamldoc --> config
    configadapters["config-* adapters (x19)"] --> config
    chatproviders["chat-anthropic/openai/gemini"] --> chat
    redact --> transit
    tls --> httpclient
    transit --> httpclient
    tls --> grpcclient
    tls --> localca
    credentials --> forge
    httpclient --> forge
    forge --> forgeproviders["forge-github/gitlab/gitea/bitbucket"]
    httpclient --> forgeproviders
    authn --> transport
    controls --> transport
    grpcclient --> transport
    transit --> transport
    tls --> transport
    redact --> transport
    transport --> tsat["transport-metrics / transport-openapi"]
    aferobilly --> repo
    leaves["browser · changelog · workspace · output · errorhandling · observability · regexutil · credentials"] --> GTB
    config --> GTB
    configadapters --> GTB
    chat --> GTB
    chatproviders --> GTB
    forge --> GTB
    forgeproviders --> GTB
    transport --> GTB
    repo --> GTB
    httpclient --> GTB
```

### Findings

#### [HIGH] GTB pins config core five minors behind (v0.4.0 vs v0.9.0), skipping security-relevant enforcement features

> **Spec:** [2026-07-23-config-family-version-repin](../specs/2026-07-23-config-family-version-repin.md)

**Evidence:** GTB `go.mod` (remote HEAD): `gitlab.com/phpboyscout/go/config v0.4.0`. Remote tags on go/config: latest **v0.9.0**. The config CHANGELOG for the missed range includes: v0.5.0 exported codec conformance suite, **v0.6.0 "enforce Sensitive at the write path with ErrSensitiveLeak"**, v0.7.0 "sensitive read-only backend refuses the routed-beneath write", v0.8.0 `ErrReadOnlyFS` sentinel, v0.9.0 `PollIntervalHinter`. **Failure scenario:** GTB — the flagship consumer and the reason config exists — runs without the sensitive-value write-path enforcement the module shipped specifically as hardening; meanwhile the pre-1.0 policy explicitly allows breaking minors, so every minor GTB skips makes the eventual catch-up MR larger and riskier. **Direction:** bump GTB to config v0.9.0 now while the delta is still all-features/no-documented-breaks; ensure Renovate on GTB actually proposes go/config bumps (v0.4.0→v0.9.0 sat unadopted across five releases).

#### [HIGH] config adapter family pins config core across five different minors (v0.3.0–v0.9.0) with no cross-version guarantee

> **Spec:** [2026-07-23-config-family-version-repin](../specs/2026-07-23-config-family-version-repin.md)

**Evidence (all remote-HEAD go.mods):** config-afero → config **v0.3.0**; dotenv/hcl/ini/json/properties/toml/xml → **v0.5.0**; aws-ssm/azure-appconfig/consul/gcp-parameter → **v0.6.0**; vault → **v0.7.0**; aws-s3/azure-blob/billy/gcp-gcs/iofs/sftp → **v0.9.0**. Notably **config-afero — the one adapter GTB actually uses — is the furthest behind at v0.3.0** (six minors). **Failure scenario:** any consumer combining a new-pin adapter with an old-pin adapter has MVS force the old adapter to build and run against config v0.9.0 — a combination no adapter CI ever tested. Because the pre-1.0 policy permits breaking minors in config core, the first such break turns every stale-pinned adapter into a latent compile failure that surfaces in *consumers'* builds, not in the adapter's own pipeline. **Direction:** either (a) a scheduled family-wide "re-pin all adapters to config head" sweep after each config minor, or (b) a cross-repo CI job in go/config that builds all 19 adapters against config HEAD before release.

#### [MEDIUM] CI component pins span v0.22.0–v0.27.0 across the fleet; 16 modules stuck at v0.22.0

> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md)

**Evidence (first component pin per `.gitlab-ci.yml`):** v0.22.0 — authn, browser, chat, chat-anthropic, chat-gemini, chat-openai, controls, grpcclient, httpclient, localca, observability, redact, regexutil, tls, transit, transport (16 repos); v0.23.0 — aferobilly, credentials, errorhandling, forge, repo, yamldoc; v0.23.1 — config + 8 codec adapters; v0.24.1 — changelog, output, transport-openapi, workspace; v0.25.0 — transport-metrics, GTB; v0.26.0 — 10 cloud config adapters + 4 forge providers; v0.27.0 — config-vault only. **Failure scenario:** the pin correlates with repo creation date — pins are set once and never bumped; the stated convention that Renovate covers CI/CD component pins is not holding on the older half of the fleet. Lint-rule and Go-toolchain behaviour now differ by five component minors between sibling modules, so "green CI" means different things per repo. **Direction:** audit Renovate config on the v0.22.0 cohort and drive one fleet-wide bump; consider having the cicd component version managed by a single shared Renovate preset.

#### [MEDIUM] Core↔provider version coupling is convention-only, with a live credentials diamond in the forge family

> **Spec:** [2026-07-23-config-family-version-repin](../specs/2026-07-23-config-family-version-repin.md) (covers the credentials-diamond re-pin as a secondary item)
> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md) (coupling mechanism; the credentials-diamond re-pin stays with the re-pin spec)

**Evidence:** the coupling *currently holds* — chat family all at minor 0.1 (core v0.1.2, anthropic v0.1.0, gemini v0.1.0, openai v0.1.2; all providers require `chat v0.1.0`), forge family all at v0.2.0. But the mechanism is a docs-page compatibility matrix plus a floor-only `require`: chat providers hook in via `chat.RegisterProvider(name, factory)` with **no API-version guard**, and nothing expresses an upper bound. Separately, **forge core requires `credentials v0.1.0` while forge-bitbucket and GTB require `credentials v0.2.0`** — MVS silently runs forge core against a credentials version it was not built or tested against. **Failure scenario:** a pre-1.0-legal breaking minor in chat core's `ProviderFactory`/`ChatClient` produces a compile break *in downstream consumers* (loud but misattributed), while a semantic contract change skews silently. **Direction:** have each core re-release bump its providers' `require` in lockstep (mechanised); bump forge core's credentials require to v0.2.0 at next release; consider an exported `chat.CoreAPIVersion` constant that providers assert at registration.

#### [MEDIUM] A change to a low-layer module fans out into 12–23 human-gated releases

> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md)

**Evidence (from the verified graph):** a config-core change that adapters must adopt forces re-releases of **19 adapters + config-json's 3 dependents + GTB ≈ 20–23 releases**. A tls change propagates through httpclient, grpcclient, localca (wave 2) → forge + 4 forge providers + transport (wave 3) → transport-metrics, transport-openapi (wave 4) → GTB (wave 5): **12 module releases across 4 sequential waves**, each wave blocked on the previous one's tag existing, and every single release is a releaser-pleaser Release-MR requiring a human merge. **Failure scenario:** this is exactly the mechanism behind the two HIGH findings above — the coordination cost is high enough that propagation simply stops partway. It will recur on the next tls or transit change. **Direction:** accept the cost consciously: keep L0/L1 modules maximally stable (additive-only where possible), batch lower-layer changes, and script the wave ("bump dep, wait for tag, next layer"). Alternatively revisit whether the 19 config adapters need independent repos versus one multi-module repo with synchronized tagging.

#### [MEDIUM] GTB's AGENTS.md still documents at least eight extracted or deleted `pkg/` packages as if they were local

> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md)

**Evidence:** `pkg/` today contains only: chat, cmd, docs, gateway, grpc, http, logger, osinfo, props, setup, telemetry, telemetrytypes, tls, utils, vcs, version (grpc/http/gateway/vcs being 1–2-file config-adapter glue). AGENTS.md still instructs agents about `pkg/controls/`, `pkg/grpc/`+`pkg/http/` as "two transports", `pkg/vcs/` "abstracts GitHub and GitLab APIs", `pkg/forms/` (deleted), `pkg/browser.OpenURL`, `pkg/regexutil.CompileBounded`, `pkg/redact`, `pkg/errorhandling`, `pkg/credentials`/`pkg/credentials/keychain` — none of which exist as described; all now live in go/* modules. **Failure scenario:** this file is the primary steering input for every AI agent session in the repo; agents will look for, import, or "restore" packages that were deliberately clean-broken. **Direction:** one docs pass updating AGENTS.md package references to the module paths (the same drift exists in module doc comments — see LOW below).

#### [LOW] GTB holds stale pins on chat, chat-openai, and controls

> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md)

**Evidence:** GTB requires `chat v0.1.0` / `chat-openai v0.1.0` (remote tags: both **v0.1.2**) and `controls v0.1.0` (remote tag **v0.1.1**). All patch/minor-internal — no compile risk — but bug fixes shipped in those patches are not reaching GTB. **Direction:** routine Renovate merge sweep.

#### [LOW] Error-handling convention splits cleanly but silently: 23 modules do not use cockroachdb/errors

> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md)

**Evidence:** cockroachdb/errors is a direct dep of 28 modules (chat, forge, transport families, etc.) but absent from the **entire config family (core + 19 adapters), aferobilly, redact, yamldoc**. The split looks deliberate (dependency-light leaves), and it does not conflict at runtime — but GTB's stated rule is "cockroachdb/errors for all error creation and wrapping", and nothing records which modules are exempt or why. **Direction:** one line in the toolkit conventions doc naming the stdlib-errors tier. (Logging, by contrast, is genuinely consistent: `*slog.Logger` everywhere it appears in public API; repo's local `Logger` interface is explicitly documented as slog-satisfiable. Context propagation sampled clean. Functional-options construction is near-universal. Mocks uniformly use a `mocks/` subpackage in the 6 modules that ship them.)

#### [LOW] yamldoc is the only module on go 1.26.4; config-vault is created but never released

> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md)

**Evidence:** all go.mods declare `go 1.26.5` except **yamldoc: go 1.26.4**. **config-vault** exists with CI (at the newest cicd v0.27.0) and requires config v0.7.0 but has **zero remote tags** — an unreleased repo that consumers cannot use yet. **Direction:** bump yamldoc's go directive at its next release; either release config-vault v0.1.0 or mark it WIP in its README.

#### [LOW] Module doc comments reference deleted GTB packages

> **Spec:** [2026-07-23-ecosystem-fleet-maintenance](../specs/2026-07-23-ecosystem-fleet-maintenance.md)

**Evidence:** `chat/config.go:10` ("go-tool-base wires **pkg/credentials**.Retrieve" — extracted), `chat/client.go:114` ("go-tool-base supplies **pkg/http**'s" — deleted), plus similar narrative references in httpclient/transport doc comments. Harmless but misleading on pkg.go.dev. **Direction:** fold into the same docs pass as the AGENTS.md fix.

### Overall assessment

The extraction programme's structural quality is genuinely good: a 50-module ecosystem with **zero dependency cycles, zero replace directives, 100% LICENSE/CI/lint/Renovate presence, uniform slog-based logging, ctx-first APIs, and a consistent mocks convention** is rare. The core↔provider families are, today, exactly at their promised matching minors. The system's real weakness is not structure but **propagation**: every mechanism keeping the fleet coherent (dep pins, CI component pins, the provider compatibility matrix) is convention enforced by Renovate-plus-human-merges, and the evidence shows propagation stalling wherever the release fan-out is large — config core raced to v0.9.0 while its own adapters froze at five different minors and GTB froze at v0.4.0, and the oldest third of the fleet still runs cicd v0.22.0. None of this is broken *yet*; all of it is the precondition for the first genuinely painful break, because pre-1.0 policy explicitly licenses breaking minors in exactly the modules (config, tls, transit) with the widest fan-out. The two moves with the best payoff-to-effort ratio are a config-family re-pin sweep (fixing both HIGH findings at once) and making the cicd component pin fleet-managed rather than per-repo.
