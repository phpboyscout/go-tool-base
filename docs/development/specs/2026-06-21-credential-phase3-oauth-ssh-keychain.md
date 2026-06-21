---
title: "Credential Hardening Phase 3 — OAuth Display-Once Closeout & SSH-Key Keychain Storage"
description: "Close out the deferred Phase 3 of credential-storage hardening (H-1, item C2): confirm the already-landed GitHub OAuth device/display-once flow, and add optional OS-keychain storage for SSH private-key passphrases through the existing credentials.Backend model."
date: 2026-06-21
status: DRAFT
tags:
  - specification
  - security
  - setup
  - credentials
  - ssh
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
---

# Credential Hardening Phase 3 — OAuth Display-Once Closeout & SSH-Key Keychain Storage

Authors
:   Matt Cockayne

Date
:   21 June 2026

Status
:   DRAFT

---

## Overview

The credential-storage hardening work ([2026-04-02-credential-storage-hardening.md](2026-04-02-credential-storage-hardening.md), audit finding **H-1**) shipped Phases 1 and 2. Phase 3 was explicitly **deferred** in `docs/development/security-decisions.md` (§ H-1, "Deferred to Phase 3"). This specification fulfils that prior intent — it does **not** contradict any standing decision. The bundled "secrets-provider registry" remains rejected (feature-decisions, 31 March 2026); everything here stays inside the existing `credentials.Backend` model and adds **no** new provider-injection seam.

The deferral list named five items:

> `config migrate-credentials` command, GitHub OAuth+display-once flow, BDD coverage, migration guide, optional SSH-key keychain storage.

Investigation of the current tree (21 June 2026) shows **four of the five already landed** after the original spec was written — the spec was even marked `IMPLEMENTED`, and the security-decisions deferral note is now stale. This spec's job is therefore narrow:

1. **Reconcile the record**: confirm and document that items (a)–(d) below already exist, so the deferral note can be cleared.
2. **Implement the one genuinely outstanding item**: optional OS-keychain storage for the **SSH private-key passphrase** during `init github`.

### Already-shipped Phase 3 items (verified — EXCLUDED from new implementation)

| Deferred item | Status | Evidence in tree |
|---------------|--------|------------------|
| `config migrate-credentials` command | **DONE** | `pkg/cmd/config/migrate.go`, `migrate_scan.go`, `migrate_test.go`, `migrate_coverage_test.go`. Supports `--dry-run`, `--target env\|keychain`, `--yes`, `--env-var`, `--skip-verify`, `--keychain-service`; atomic temp+rename rewrite; registered under `NewCmdConfig`. |
| GitHub **OAuth device flow** | **DONE** | `pkg/vcs/github/login.go` (`GHLogin` → `github.com/cli/oauth` device flow, browser routed through `pkg/browser.OpenURL`). |
| GitHub **display-once** token flow | **DONE** | `pkg/setup/github/github.go`: `GitHubAuthConfig.StorageMode` three-mode selector, `runEnvVarAuth`, `defaultDisplayOnceForm` (note + acknowledge confirm, token never written to disk in env-var mode), `captureToken`, `promptManualGitHubToken` headless fallback, `writeGitHubCredential` (`github.auth.env`/`.keychain`/`.value`). Tested in `github_auth_test.go`. |
| **BDD coverage** + **migration guide** | **DONE** (per the 2026-04-02 spec's "IMPLEMENTED" close-out) | Gherkin in `features/`, `docs/migration/v1.12-credential-storage.md`. |

Because GitHub OAuth + display-once is already complete, **item C2(a) requires no code** — only a verification pass and a one-line correction to `security-decisions.md`. The remainder of this spec addresses **item C2(b): optional SSH-key keychain storage**, the single outstanding gap.

### Problem (SSH-key passphrase storage)

`pkg/setup/github/ssh.go` discovers SSH keys by scanning `~/.ssh` (a directory listing — accepted in security-decisions **L-4**), lets the user select an existing key, point at a path, defer to `ssh-agent`, or **generate a new passphrase-protected Ed25519 key** (`generateKey` → `keygen.WithPassphrase`). The config records only:

```yaml
github:
  ssh:
    key:
      path: /home/user/.ssh/id_mytool_20260621120000
      type: file   # or "agent"
```

The **passphrase is collected, used once to encrypt the key on disk, then discarded** (`generateKey`'s local `passphrase` string falls out of scope). There is no GTB-managed place to keep it. In practice the user must either:

- re-type the passphrase on every git/SSH operation, or
- add the key to `ssh-agent` manually, or
- store the passphrase in their own OS keychain by hand (macOS `ssh-add --apple-use-keychain`, etc.).

GTB already owns a perfectly good OS-keychain abstraction (`credentials.Backend`, activated by the opt-in `pkg/credentials/keychain` blank import). The Phase 3 gap is simply: **offer to stash a freshly-generated key's passphrase in that keychain**, so the user gets the convenience of an encrypted key without a manual keychain dance, and the passphrase still never lands in the plaintext config.

### Threat model addendum

| Threat | Vector | Impact | This spec |
|--------|--------|--------|-----------|
| SSH passphrase typed repeatedly / cached insecurely | User stores passphrase in a note, shell history, or weakens it to avoid friction | Private-key compromise → repo/org access | Mitigated: passphrase held in OS keychain, never on disk, never in config |
| SSH passphrase written to config | A naïve future change could persist it as plaintext | Same as H-1 for tokens | Prevented: SSH passphrase has **no literal config mode** — keychain or nothing |
| Keychain unavailable / opted-out | Headless Linux without Secret Service, regulated build with no keychain import | Feature simply absent | Graceful: option hidden when `Probe` fails; passphrase discarded as today |

### Goals

- Offer, **only when a keychain backend is registered and a live `Probe` succeeds**, to store the passphrase of a **newly generated** SSH key in the OS keychain.
- Record a **keychain reference** (not the passphrase) in config so the passphrase can be retrieved later without re-prompting.
- Confirm SSH-key passphrases fit the existing `credentials.Backend` string-secret contract (they do — `Store(ctx, service, account, secret string)`).
- Add a `Resolve…` helper so consumers (git transport, `ssh-agent` loading) can fetch the passphrase through the same three-source discipline used elsewhere.
- Keep the default (no-keychain) build and behaviour **byte-for-byte unchanged**.
- Clear the stale Phase 3 deferral note in `security-decisions.md`.

### Non-Goals

- **No literal-mode passphrase storage.** Unlike API tokens, an SSH passphrase has only two outcomes here: stored in the keychain, or not stored at all (status quo). There is no `github.ssh.key.passphrase` literal config key, ever.
- **No env-var mode for the passphrase.** An env-var holding a long-lived SSH passphrase is worse than the keychain and barely better than literal; out of scope.
- **No storage of the private-key bytes themselves** in the keychain. The key stays an encrypted file on disk at `github.ssh.key.path`; only its passphrase is offered to the keychain. (Discussed and rejected — see Design Decisions.)
- **No changes** to the OAuth/display-once flow, `config migrate-credentials`, or the AI/Bitbucket wizards.
- **No new `Backend` method.** SSH passphrases use the existing `Store`/`Retrieve`/`Delete`.
- **No ssh-agent re-implementation.** Loading the decrypted key into an agent is a follow-up consideration, not this spec.

---

## Design Decisions

**D1 — Store the passphrase, not the key bytes.** The `Backend` contract is a string secret keyed by `service/account`. An SSH passphrase is a short string — a natural fit. The private key is already an encrypted file with `0600` perms at a path the config records; duplicating its bytes into the keychain doubles the secret's footprint and the blast radius without benefit. Storing only the passphrase means the on-disk artefact remains the canonical key and the keychain holds the one piece that unlocks it.

**D2 — Offer keychain storage only for *generated* keys.** When the user selects a pre-existing key or `ssh-agent`, GTB neither knows nor should ask for the passphrase — prompting for an existing key's passphrase just to cache it is surprising and risky. The offer appears **only** on the `generate` branch (`handleSSHKeySelection` → `generateKey`), where the wizard already holds the passphrase it just used to encrypt the key.

**D3 — Reuse the existing keychain gate.** The offer is shown only when `credentials.Probe(ctx)` returns true (backend registered **and** a live canary round-trip succeeds), exactly as the AI/GitHub-token wizards gate `ModeKeychain`. No new availability logic.

**D4 — Keychain account naming follows the resolved Phase 2 convention.** Service = tool name, account = a stable SSH-scoped identifier. Per Resolved Decision #1 of the parent spec (`<toolname>/<key-path>`, no `gtb/` prefix), the account is `github.ssh.passphrase`. The config records `github.ssh.key.keychain: "<toolname>/github.ssh.passphrase"`. A single account is sufficient because the wizard manages one GTB-generated key per tool; regenerating overwrites (Store overwrites by contract).

**D5 — No literal/env fallback for the passphrase.** Resolution is keychain-only (see Public API). If the keychain entry is missing or unreachable, the resolver returns `("", ErrCredentialNotFound)`/wrapped error and the caller falls back to **interactive prompt or ssh-agent** — i.e. today's behaviour. This deliberately departs from the token three-source cascade because the other two sources are explicit non-goals here.

**D6 — Best-effort passphrase zeroing, documented limits.** Consistent with security-decisions **M-4**: Go string immutability makes reliable scrubbing impractical. The wizard clears its last reference after Store; we make no stronger claim.

**D7 — Stale deferral reconciliation.** The 2026-04-02 spec is `IMPLEMENTED` but `security-decisions.md` § H-1 still lists Phase 3 as deferred and its status line reads "Remediated — Phases 1 and 2 of 3". On completion of *this* spec, that entry becomes "Remediated — Phase 3 complete" and the deferral bullet is removed.

---

## Public API

### New file: `pkg/setup/github/ssh_keychain.go`

A small helper, in the same package as the existing SSH flow, so it can reach the wizard's forms and the package-private `generateKey` plumbing without widening any public surface.

```go
package github

// sshPassphraseAccount is the keychain account under which a
// GTB-generated SSH key's passphrase is stored. Combined with the
// tool name as the service, per Resolved Decision #1 of the parent
// hardening spec (no gtb/ prefix).
const sshPassphraseAccount = "github.ssh.passphrase"

// offerSSHPassphraseKeychain asks the user — only when a keychain
// backend is registered and a live Probe succeeds — whether to store
// the just-generated key's passphrase in the OS keychain. On accept it
// stores the secret and records a keychain reference in config under
// "github.ssh.key.keychain". On decline (or when keychain is
// unavailable) it is a no-op and the passphrase is discarded as today.
//
// The passphrase is never written to config in any mode. Errors from
// the keychain Store are surfaced with a hint but are non-fatal: the
// user keeps a working (passphrase-protected) key either way.
func offerSSHPassphraseKeychain(
    ctx context.Context,
    p *props.Props,
    cfg config.Containable,
    toolName, passphrase string,
    confirmForm func(*bool) *huh.Form, // injectable for tests; nil → default
) error
```

### New: exported resolver in `pkg/setup/github` (or `pkg/vcs/github`, TBD — see Open Questions)

```go
// ResolveSSHPassphrase returns the passphrase for a GTB-managed SSH key
// from the OS keychain, using the reference recorded at
// "github.ssh.key.keychain". Resolution is keychain-only by design (no
// literal or env-var fallback for SSH passphrases — see spec Non-Goals):
//
//   * No keychain reference in config            → ("", nil)  [not configured]
//   * Reference present, entry missing/unreachable → ("", err) wrapping
//     credentials.ErrCredentialNotFound / the backend error
//
// Callers that get ("", nil) or an error should fall back to an
// interactive passphrase prompt or ssh-agent, exactly as before this
// feature existed.
func ResolveSSHPassphrase(ctx context.Context, cfg config.Containable) (string, error)
```

### Modified: `pkg/setup/github/github.go` / `ssh.go`

- `generateKey` (in `ssh.go`) gains a call to `offerSSHPassphraseKeychain` after the key is written and before returning, threading the `passphrase` it already holds. The passphrase local is zeroed (best-effort) after the offer.
- `IsConfigured` / `hasAnyGitHubCredential` are **unchanged** — SSH-key presence is already tracked via `github.ssh.key.path` / `type`; the keychain reference is supplementary.
- A new config key `github.ssh.key.keychain` is written **only** when the user opts in.

### No change

- `credentials.Backend`, `Store`/`Retrieve`/`Delete`, `Probe`, `Mode` taxonomy — all reused as-is.
- `pkg/vcs/auth.go` token resolution — untouched (SSH passphrase is a separate concern from VCS tokens).
- AI / Bitbucket wizards, `config migrate-credentials` — untouched.

---

## Internal Implementation

### Generated-key flow (the only changed path)

```
handleSSHKeySelection(targetKey == "generate")
  └─ generateKey(props, cfg)
       1. prompt passphrase  (existing, min 12 chars)
       2. generateAndSaveSSHKey → encrypted key file @ keypath (0600), pub @ 0644  (existing)
       3. prompt "Upload key to GitHub?" + optional upload  (existing)
       4. NEW: offerSSHPassphraseKeychain(ctx, props, cfg, props.Tool.Name, passphrase, nil)
            ├─ if !credentials.Probe(ctx)        → return nil  (option not shown)
            ├─ confirm form: "Store this key's passphrase in your OS keychain
            │                 so you aren't prompted each time?"  (default: Yes)
            ├─ if declined                        → return nil  (passphrase discarded)
            ├─ credentials.Store(ctx, toolName, "github.ssh.passphrase", passphrase)
            │     └─ on error: log.Warn + WithHint, return nil  (non-fatal)
            └─ cfg.Set("github.ssh.key.keychain", toolName+"/github.ssh.passphrase")
       5. NEW: passphrase = ""   (best-effort zero, per M-4)
```

The `ctx` carries `credentials.KeychainOpTimeout` (5 s) so a locked or slow keychain cannot stall `init`.

### Resolution

```go
func ResolveSSHPassphrase(ctx context.Context, cfg config.Containable) (string, error) {
    ref := strings.TrimSpace(cfg.GetString("github.ssh.key.keychain"))
    if ref == "" {
        return "", nil // not configured for keychain — caller prompts / uses agent
    }

    service, account, ok := strings.Cut(ref, "/")
    if !ok || service == "" || account == "" {
        return "", errors.Newf("malformed keychain reference %q", ref)
    }

    secret, err := credentials.Retrieve(ctx, service, account)
    if err != nil {
        // Wrapped — never embeds the secret. ErrCredentialNotFound flows
        // through so callers can errors.Is and fall back cleanly.
        return "", errors.Wrap(err, "retrieve SSH passphrase from keychain")
    }

    return secret, nil
}
```

### Config output

**Opted in (keychain available):**
```yaml
github:
  ssh:
    key:
      path: /home/user/.ssh/id_mytool_20260621120000
      type: file
      keychain: "mytool/github.ssh.passphrase"   # reference only; passphrase in OS keychain
```

**Declined / keychain unavailable (status quo):**
```yaml
github:
  ssh:
    key:
      path: /home/user/.ssh/id_mytool_20260621120000
      type: file
```

---

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| Keychain backend not registered (default build) | `Probe` false → offer never shown; passphrase discarded as today. No error. |
| Keychain registered but `Probe` fails (locked / headless / no Secret Service) | Offer never shown; no error. |
| User declines the offer | No-op; passphrase discarded; no `keychain` key written. |
| `Store` fails after user opted in | `log.Warn` + `WithHint` ("Keychain storage failed; your key still works — you'll be prompted for the passphrase, or add it to ssh-agent."); **non-fatal**, no `keychain` key written. |
| `github.ssh.key.keychain` present but entry missing at resolve time | `ResolveSSHPassphrase` returns wrapped `ErrCredentialNotFound`; caller falls back to prompt/agent. |
| Malformed `keychain` reference | `ResolveSSHPassphrase` returns descriptive error (no secret content). |

All errors use `cockroachdb/errors` with `WithHint`; no error message ever embeds the passphrase (R2-equivalent).

---

## Testing Strategy

### Unit tests (`pkg/setup/github`)

| Area | Test |
|------|------|
| `offerSSHPassphraseKeychain` | Probe-false → no-op, no config key, no Store call (fake backend asserts zero calls). |
| `offerSSHPassphraseKeychain` | Probe-true + confirm Yes → `Store` called with `(toolName, "github.ssh.passphrase", passphrase)`; config gains `github.ssh.key.keychain`. |
| `offerSSHPassphraseKeychain` | Probe-true + confirm No → no Store, no config key. |
| `offerSSHPassphraseKeychain` | `Store` error → non-fatal, warns, no config key written. |
| `ResolveSSHPassphrase` | No ref → `("", nil)`. |
| `ResolveSSHPassphrase` | Valid ref + present entry → returns secret. |
| `ResolveSSHPassphrase` | Valid ref + missing entry → wrapped `ErrCredentialNotFound` (assert `errors.Is`). |
| `ResolveSSHPassphrase` | Malformed ref → error, no panic, no secret in message. |
| `generateKey` | End-to-end with injected forms + fake backend: key written, passphrase offered, config shape correct. Existing `generateKey` tests must still pass unchanged when keychain is absent. |

Use the in-memory fake from `pkg/credentials/credtest` (registered via `RegisterBackend` in the test, reset in cleanup) — mirrors the existing keychain-mode wizard tests. `t.Parallel()` where backend registration allows; serialise the registration-sensitive cases.

### Security-specific tests

| Test | Purpose |
|------|---------|
| `TestSSHPassphraseNeverInConfig` | After every generate path (opt-in and decline), assert the config contains **no** key whose value equals the passphrase; only `github.ssh.key.keychain` (a reference) may appear. |
| `TestSSHPassphraseNotLogged` | Run generate with a recognisable passphrase under a capturing DEBUG logger; assert the passphrase string appears in **no** log entry at any level. |
| `TestResolveSSHPassphraseErrorRedaction` | Resolver errors never contain the secret. |

### Integration tests

| Tag | Scope |
|-----|-------|
| `INT_TEST_CREDENTIALS` | Real keychain round-trip for the SSH passphrase on macOS / Linux-with-Secret-Service; skipped when `DBUS_SESSION_BUS_ADDRESS` unset (same gate as existing keychain integration tests). |

### E2E / BDD

Extend `features/` (e.g. `features/cli/credentials.feature` or a new `ssh.feature`) with a scenario: generating a key with keychain available stores a reference and not the passphrase; with keychain unavailable, the config omits the `keychain` key. Gated by `INT_TEST_E2E_CLI`.

### Quality gates

- `pkg/setup/github` keeps its current coverage policy (≥ 90% for new code in `ssh_keychain.go`).
- Race detector clean: `go test -race ./pkg/setup/github/... ./pkg/credentials/...`.
- Both the default build **and** a build that blank-imports `pkg/credentials/keychain` compile and pass.
- golangci-lint: no new findings, no `//nolint` beyond the package norms.

---

## Project Structure

### New files

| File | Purpose |
|------|---------|
| `pkg/setup/github/ssh_keychain.go` | `offerSSHPassphraseKeychain`, `ResolveSSHPassphrase`, `sshPassphraseAccount`. |
| `pkg/setup/github/ssh_keychain_test.go` | Unit + security tests above. |

### Modified files

| File | Change |
|------|--------|
| `pkg/setup/github/ssh.go` | `generateKey` calls `offerSSHPassphraseKeychain`; zero passphrase after. |
| `docs/development/security-decisions.md` | § H-1: status → "Remediated — Phase 3 complete"; remove the deferral bullet; note SSH-passphrase keychain storage. |
| `docs/development/specs/2026-04-02-credential-storage-hardening.md` | Cross-reference this closeout spec from its Phase 3 section. |
| `docs/components/credentials.md` / `docs/components/setup.md` | Document SSH-passphrase keychain storage + `github.ssh.key.keychain`. |
| `docs/how-to/configure-credentials.md` | Add SSH-passphrase keychain section. |
| `CLAUDE.md` | One line under Credential Storage: SSH passphrases for generated keys may be stored in the keychain (reference recorded at `github.ssh.key.keychain`); never literal, never env-var. |

### Generator impact

None structural. Scaffolded tools that enable the GitHub init feature and blank-import `pkg/credentials/keychain` inherit the offer automatically — no template change. Tools that omit the keychain import simply never see it (Probe false).

---

## Migration & Compatibility

- **Backward compatible.** No existing config key changes meaning. `github.ssh.key.keychain` is new and additive; absence = today's behaviour.
- **No migration required.** Existing generated keys keep working; users who want the convenience can regenerate or set the keychain entry manually (documented in the how-to).
- **API stability.** `ResolveSSHPassphrase` and the `github.ssh.key.keychain` key are new (Beta tier). `offerSSHPassphraseKeychain` is package-private. No existing signature changes. Consistent with the pre-1.0 posture.

---

## Security Invariants

1. The SSH passphrase is **never** written to the config file in any mode (no literal, no env-var) — only a keychain *reference*.
2. The passphrase never appears in log output at any level.
3. Keychain `Store`/`Retrieve` errors never embed the passphrase.
4. The offer appears only when a keychain backend is registered **and** a live `Probe` succeeds — never a dead option.
5. A `Store` failure is non-fatal: the user retains a working passphrase-protected key.
6. `~/.ssh` discovery remains a directory listing only (unchanged from L-4); file contents are read only for the user-selected key (unchanged).
7. Best-effort passphrase zeroing after use, with M-4's documented limits.

---

## Open Questions

1. **Resolver package placement.** `ResolveSSHPassphrase` is proposed in `pkg/setup/github`. If a runtime git-transport consumer needs it without importing the setup wizard, it may belong in `pkg/vcs/github` or a neutral `pkg/credentials` helper instead. Confirm the intended consumer before fixing the location. **(Recommend resolving before implementation.)**
2. **Default of the confirm form.** Proposed default is **Yes** (store in keychain), matching the parent spec's "first-time users want the convenience" rationale for OAuth. Confirm this is the desired nudge, or whether SSH passphrases warrant a **No** default (more conservative).
3. **Existing/selected keys.** This spec deliberately offers keychain storage only for *generated* keys (D2). Should there be an explicit, separate opt-in to cache an *existing* key's passphrase (prompting for it)? Currently out of scope — confirm that is acceptable.
4. **ssh-agent loading.** Should a follow-up load the decrypted key into `ssh-agent` after retrieval, or is keychain-retrieval-then-prompt sufficient? Out of scope here; flag for a future spec.
5. **`config migrate-credentials` awareness.** The migrate command operates on token/API literals. Should it learn to surface "SSH passphrase not yet in keychain" as an advisory? Likely a separate, optional enhancement — confirm it is out of scope.

## Resolutions (open questions confirmed with user 2026-06-21)

1. **Resolver placement** — RESOLVED: put `ResolveSSHPassphrase` in a **neutral
   `pkg/credentials` helper** (transport- and wizard-agnostic), not
   `pkg/setup/github`. The generic credentials package owns the SSH-passphrase
   resolution so both the setup wizard and any future runtime git-transport
   consumer use the same path. (Departs from the draft's recommendation.)
2. **Confirm-form default** — RESOLVED: **default No (conservative).** SSH
   passphrases protect long-lived private keys, so storage is opt-in — the user
   actively chooses Yes. Does not inherit the OAuth convenience nudge.
3. **Existing/selected keys** — RESOLVED: **out of scope.** Keychain storage is
   offered only on the *generate* branch (D2), where the passphrase is already in
   hand. Caching an existing key's passphrase is a later, separate opt-in.
4. **ssh-agent loading (Q4)** and **`migrate-credentials` SSH advisories (Q5)** —
   RESOLVED: **both out of scope**, flagged as future follow-ups; this spec stays
   tight.

Reminder for the approval decision: 4 of the 5 original H-1 Phase-3 items already
shipped (OAuth device flow, display-once, `config migrate-credentials`, BDD), so
the *implementation* scope of this spec is just the SSH-passphrase keychain
helper plus clearing the now-stale Phase-3 deferral note in
`docs/development/security-decisions.md`.

---

## Decision-Log Alignment

- **H-1 / security-decisions.md**: Phase 3 was explicitly *Deferred*; this spec **fulfils** that intent. On completion the deferral note is cleared and the H-1 status advances to "Phase 3 complete". No standing decision is contradicted.
- **Rejected secrets-provider registry (feature-decisions, 31 Mar 2026)**: **honoured.** This spec adds no provider-injection seam, no new interface, no Vault/SSM/registry abstraction — it reuses the existing `credentials.Backend` `Store`/`Retrieve`/`Delete` exactly as Phase 2 established.
- **L-4 (~/.ssh scanning)**: unchanged; this spec does not broaden discovery.
- **M-4 (string zeroing limits)**: explicitly acknowledged for the passphrase local.
- **Pre-1.0 API posture (project memory)**: new keys/helpers ship as additive minor changes; no breaking change.
</content>
</invoke>
