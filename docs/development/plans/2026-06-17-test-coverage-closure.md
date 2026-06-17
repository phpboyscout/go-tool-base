---
title: "Test-coverage closure plan (unit · integration · Godog/BDD)"
description: "Prioritised plan to close critical coverage gaps and uncovered failure scenarios across unit, integration, and E2E BDD tests, from the 2026-06-17 three-part coverage audit. Ordered by risk, then effort."
status: DRAFT
date: 2026-06-17
---

# Test-coverage closure plan

Consolidated from a three-part read-only coverage audit (unit, integration, Godog/BDD) run on 2026-06-17 against the v0.20.0 tree.

**Prioritisation:** by RISK first (a security/correctness failure path that could ship a real defect), then by the project's `pkg/` ≥90% policy, then effort-ascending within each tier. **Effort scale:** XS < 1 h · S ≈ half a day · M 1–2 days · L multi-day. Each phase is intended to be one MR.

## Snapshot

| Layer | State |
|---|---|
| Unit | Repo 55.3% overall (diluted by `mocks/`, entrypoints, TUI). Meaningful `pkg/` libs higher but **many under the 90% policy**. 29 zero-test packages (1 that matters: `internal/generator/verifier`). |
| Integration | Env-var-gated (`INT_TEST*`). Controls + generator-build + signing-script are strong. **Self-update, GitLab, keychain, chat, WKD have no real-dependency coverage**; the one GitHub-API test is non-functional. Doc drift + 4 filename-convention violations + 2 undocumented tags. |
| Godog/BDD | ~108 scenarios / 23 features — strong for controls, generator, config, telemetry, keys. **`sign`, `template` lifecycle, real-update, signing enable/disable, init wizards have no scenarios.** |

---

## P0 — Critical: security/correctness failure paths (do first)

### Phase 1 — signing & redaction reject-branch unit tests — **XS**
| Target | Now | Uncovered scenario |
|---|---|---|
| `pkg/setup/signing.go:subkeyCanSign` | 60% | `sub.PublicKey == nil` → reject; `Sig.FlagsValid && !FlagSign` → reject. Only the permissive path is tested — an inverted guard would admit a non-signing/empty subkey into the update trust set silently. Table-test crafted `openpgp.Subkey` values. |
| `pkg/redact/redact.go:keepPrefix` | 66.7% | Boundary token lengths (shorter than / exactly at the kept-prefix length). Redaction primitive feeding telemetry. |

### Phase 2 — self-update error/guard paths (unit) — **S**
`pkg/setup/update.go`: `Update` 73%, `shouldSkipUpdate` 71%, `resolveTargetPath` 69%, `requireReleaseToken` 0%, `GetCurrentVersion` 0%. The uncovered statements are the binary-replacement **failure** branches — the most dangerous place for an untested path (a bad branch can brick the installed binary). Inject fake VCS provider + afero fs; drive download/extract/replace failures, assert no partial overwrite; table-test `requireReleaseToken` across the provider switch.

### Phase 3 — keychain-availability probe (unit) — **S**
`pkg/credentials/mode.go:Probe` 22%, `probeAccount` 0%. The probe decides whether the wizard offers keychain storage; its error/unavailable branches are unexercised, so a broken-keychain platform could be mis-reported as available. Inject a failing backend via `credtest`.

### Phase 4 — generator verifier (the zero-test package that matters) — **M**
`internal/generator/verifier` is 0% (no test files): `legacy.go: VerifyAndFix / verifyGeneratedCode / cleanupForRegeneration`, `agent.go: VerifyAndFix / NewAgentVerifier`. This layer compiles/repairs scaffolded output and cleans up on regeneration; an undetected failure ships broken scaffolds or leaves partial state after a failed regen. afero-backed tests over a deliberately-broken tree + mocked AI client.

### Phase 5 — fix the non-functional GitHub-API integration test — **S**
`pkg/vcs/github/client_integration_test.go` hardcodes `t.Setenv("GITHUB_TOKEN","test-token")`, overriding any real token, so it cannot authenticate — **false confidence**. Remove the override, gate properly, and assert real PR/label lookups (or delete it if a live GitHub project isn't viable and replace with a documented gap).

### Phase 6 — self-update end-to-end integration test — **M**
No `SkipIfNotIntegration` test pulls a real release asset through download → checksum → **signature-verify** → extract → replace. This is the security-critical path (signature enforcement live since v0.13.0); a regression in enforcement or checksum-manifest parsing passes all current gated tests. Gate under a new `INT_TEST_UPDATE` (or reuse `signing`), fetch a known GitLab release asset, and drive the full pipeline against a temp install dir.

---

## P1 — Important: policy compliance + critical user workflows

### Phase 7 — BDD: `gtb sign` workflow — **M**
The signing subsystem's whole purpose is signing artifacts, yet `sign` has **zero** scenarios (`keys` is well covered). Add a mint → sign → verify (+ tamper-reject) feature — a textbook multi-step BDD fit. Postdates the BDD strategy spec, but CLAUDE.md's "new CLI commands must include Gherkin scenarios" applies.

### Phase 8 — BDD: signing enable/disable + `template` overlay lifecycle — **M**
- `enable signing`/`disable signing` end-to-end (today only asserts `Signing:` lands in `cmd.go`).
- `template add → regenerate → update → remove` success path (today only the security rejections + group listing are covered).

### Phase 9 — BDD: real self-update outcomes — **S**
`update.feature` only checks semver validation + help. Add stub-release-source scenarios for: already-latest → no-op; newer → applies; release-not-found / checksum / signature mismatch → non-zero exit + clear message. Keep BDD to user-visible exit-code/message outcomes (download mechanics stay in Phase 6).

### Phase 10 — `pkg/` ≥90% policy closure — **M** (splittable)
Bring sub-90% `pkg/` packages to policy. Priority within: `pkg/cmd/changelog` (0%), `pkg/cmd/docs` (21%), `pkg/cmd/telemetry` (29%), `pkg/tls` (38%, security-adjacent), `pkg/cmd/root` (65%, the middleware every command runs through), `pkg/setup/bitbucket` (47%), `pkg/setup/github` (61%). Then sweep the 78–89% near-misses (each needs a handful of error-branch cases): `credentials`, `workspace`, `setup/ai`, `cmd/config`, `version`, `props`, `config`, `output`, `vcs/*`, `chat`, `openpgpkey`, `grpc`. Most are pure error-branch additions.

### Phase 11 — integration: GitLab + keychain + WKD + hot-reload — **M** (splittable)
- **GitLab** nested-group / Enterprise auth + PR/release-asset integration (headline capability, currently unit-only).
- **OS keychain** real round-trip + runtime resolution-precedence (env→keychain→literal→fallback) — today mock-only (`keyring.MockInit`).
- **WKD resolver** against a real static `openpgpkey` host (today httptest-only).
- **Config hot-reload** (`Observable`/fsnotify) — mutate a real file, assert observers fire (today unit-only).

---

## P2 — Hygiene & lower-risk

### Phase 12 — generator AST unit tests — **S**
`internal/generator/ast_extract.go` pure AST walkers at 0%: `evaluateTimeBinaryExpr`, `containsCall`, `extractCallTarget`, `fallbackFindTargetFunction`, `isCobraCommandConstructor`, `processDeclStmt`, `markFlagHidden`, `processCobraKey` (33%). No I/O — cheap table tests over parsed-source fixtures; a silent mis-parse here produces subtly wrong scaffolds.

### Phase 13 — BDD smoke for remaining commands — **S**
`changelog`, `mcp` (default-enabled, no scenarios); `init ai`/`init github`/`init bitbucket` non-interactive `--skip-*` outcome paths (the e2e binary already enables these wizards for exactly this); controls shutdown-timeout-**exceeded** forced-termination.

### Phase 14 — integration-test hygiene/doc drift — **XS–S**
- Rename the 4 convention-violating files to `*_integration_test.go` (`pkg/config/integration_test.go`, `pkg/controls/{integration,shutdown}_test.go`, `pkg/cmd/root/integration_test.go`) so the `find` sweep the convention relies on works.
- Document the `signing` and `generator_build` tags (the project's best real-dependency coverage, currently invisible to reviewers).
- Correct `docs/development/integration-testing.md`: chat tests do **not** require API keys (httptest/in-process); fix the `INT_TEST_E` env-table parsing artifact; mark Bitbucket/Gitea sections honestly as unimplemented.

### Phase 15 — chat live-provider coverage (optional, gated) — **M**
Real Anthropic/OpenAI/Gemini SSE + auth-mode integration behind keys (today zero live coverage). Lower priority because providers are external/costly; pair with the Phase 14 doc correction so the gap is at least honestly recorded.

---

## Notes
- Phases 1–3 + 12 are quick, pure, high-value — recommend bundling the truly-tiny ones (Phase 1) into the immediate cleanup.
- Phases 5/6/11/15 require live credentials/runners — schedule against CI secrets availability.
- `pkg/forms`, `pkg/docs` (Bubble Tea), wizard/AI-orchestration paths, and skeleton string-emitters are intentionally left to E2E/integration rather than unit — not policy violations to chase.
- `pkg/signing/kms.NewSigner` and `(b *backend).NewSigner` (both 0%) are thin AWS-config-load wrappers around the already-100%-covered `newSigner(ctx, client, keyID)`; like `browser.defaultOpener`, they are untestable in unit without real AWS and are excluded from the policy chase.
- Cross-reference: this plan supersedes the testing-related items noted in the 2026-06-12 audit plans for the areas it covers.
