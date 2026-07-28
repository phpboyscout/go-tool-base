---
title: "Forge/repo/setup follow-ups: batched MEDIUM/LOW findings from the architectural review"
description: "One grouped remediation spec for the eleven MEDIUM and LOW findings against the forge provider family, the repo/aferobilly modules, and GTB's setup/self-update subsystem from the 2026-07-23 architectural review — install chmod, tar decompression bounds, SSH key handling, PAT-URL profile ownership, provider pagination and config normalisation, redirect doc drift, capability discovery under wrappers, repo context plumbing and OpenLocal semantics, and aferobilly Chmod delegation. The CRITICAL and HIGH findings are handled by dedicated sibling specs."
date: 2026-07-23
status: IMPLEMENTED
tags:
  - specification
  - forge
  - repo
  - setup
  - followups
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# Forge/repo/setup follow-ups: batched MEDIUM/LOW findings from the architectural review

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   IMPLEMENTED

Open questions resolved
:   **Q1 (pagination):** paginate IN the providers until `limit` is satisfied —
    the real consumer wants "everything up to limit", and Bitbucket already
    behaves this way. Gitea's per-page is capped at its common maximum (50);
    GitHub/GitLab at 100. `limit <= 0` means a single natural page.
    **Q2 (capability discovery):** added an `Unwrap() Provider` convention plus a
    `forge.As(provider, &target)` helper (mirrors `errors.As`, composes with
    wrapper stacks); the four GTB call sites migrated to it.
    **Q3 (`OpenLocal` branch):** call-site audit found no consumer calls
    `OpenLocal` directly or relies on its `branch` (only the internal `Open`
    dispatcher and the `ThreadSafeRepo` wrapper) — so the parameter was
    **removed**; init defaults to `main`, and `InitLocal` remains the
    branch-explicit init entry point.
    **Q4 (decompressed-size bound):** a fixed **1 GiB** constant.

Migration note (repo ctx break)
:   §2.3.1 is an API-breaking signature change (pre-1.0, ships as a minor bump):
    `Open`, `OpenLocal`, `OpenInMemory`, `Clone`, `Commit`, `Push`, and
    `CreateBranch` gain a leading `context.Context`, and `OpenLocal` drops its
    `branch` argument. Documented in `go/repo` at
    `docs/explanation/context-and-cancellation.md`.

Implementation deviation (§2.4.1)
:   The finding proposed delegating aferobilly's `Chmod` to `billy.Change`, on
    the premise that osfs implements it. In go-billy v5.9.1 **no** backend
    implements `billy.Change` (it additionally requires `Chown`/`Lchown`/
    `Chtimes`, which exist nowhere in that release); osfs and memfs implement
    only the narrower `billy.Chmod`. Delegation therefore targets `billy.Chmod`
    — which fixes the osfs executable-bit bug (the primary acceptance). A
    consequence is that memfs, which also implements `billy.Chmod`, now honours
    the mode rather than remaining a strict no-op; this is harmless and arguably
    more correct (go-git commits `100755` from an in-memory worktree too).

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (forge/VCS family + setup section),
    [setup credential-stage context scoping](2026-07-23-setup-credential-stage-context-scoping.md) (the CRITICAL finding),
    [self-update downgrade guard](2026-07-23-self-update-downgrade-guard.md) (HIGH),
    [forge-gitlab redirect token hardening](2026-07-23-forge-gitlab-redirect-token-hardening.md) (HIGH),
    [forge-bitbucket download URL contract](2026-07-23-forge-bitbucket-download-url-contract.md) (HIGH)

---

## 1. Context

The 2026-07-23 architectural review of the forge provider family (`go/forge` + the
four provider adapters), the `go/repo`/`go/aferobilly` modules, and GTB's `pkg/setup`
subsystem surfaced one CRITICAL and three HIGH findings — each covered by the
dedicated sibling specs listed above — plus six MEDIUM and five LOW findings that are
individually small but collectively describe the seams left by the recent extraction
and forge-aware-setup migrations. This spec batches those eleven findings into one
remediation plan, grouped by the repository they land in, so they can be scheduled,
reviewed, and released coherently rather than as a scatter of micro-MRs.

## 2. Items

### 2.1 Self-update hardening (go-tool-base, `pkg/setup`)

#### 2.1.1 Installed binary chmod'ed to `0o111` (MEDIUM)

`filePermExecutable = 0o111` (`pkg/setup/update.go:48`) is applied via
`s.Fs.Chmod(targetPath, filePermExecutable)` after the rename (update.go:1148).
`Chmod` *sets* the mode, so every successful update leaves the binary
`--x--x--x`: it still executes (execve needs only `x`), but the owner can no
longer read, copy, back up, checksum, or `scp` it. **Direction:** chmod to
`0o755`, applied to the temp file *before* the rename so the installed binary is
never observable execute-only. **Acceptance:** after a simulated update, the
installed binary's mode is `0o755` (extract/install unit test).

#### 2.1.2 Tar extraction has no decompressed-size bound (LOW)

The extraction loop (update.go:1126-1136) is commented "Copy file in chunks to
help mitigate a decompression bomb attack" but the `io.CopyN` loop runs to EOF
with no cumulative cap: the compressed download is capped at 512 MiB, yet gzip
expansion is unbounded — relevant precisely when checksum/signature enforcement
is off, the compile-time default. **Direction:** track the running total across
iterations and abort once a decompressed-size bound (proposed 1 GiB, §4 Q4) is
exceeded. **Acceptance:** extracting a crafted over-bound tar entry fails with
the bound error and leaves no temp file behind (unit test).

#### 2.1.3 SSH private key mode 0o700 + passphrase detection by error string (LOW)

`pkg/setup/forge/ssh.go:434` writes the generated private key with
`dirPermUserOnly` (0o700 — a *directory* constant; the convention for key files
is 0o600). Separately, ssh.go:201 and ssh.go:260 detect passphrase-protected
keys by comparing `err.Error() == "ssh: this private key is passphrase
protected"` — one upstream wording change and protected keys stop being
recognised. **Direction:** add a `filePermKey = 0o600` constant for the key
write, and replace both string comparisons with `errors.As` against
`*ssh.PassphraseMissingError` (as `go/repo` already does, repo.go:430-439).
**Acceptance:** generated key files carry mode 0o600; passphrase detection is
tested via the typed error, not its message.

#### 2.1.4 Generic single-token wizard hardcodes GitHub's PAT URL and scopes (LOW)

`promptManualToken` (`pkg/setup/forge/single.go:513`) builds
`https://%s/settings/tokens/new?scopes=repo,read:org,gist&description=gtb-cli`
from `profile.Host` inside the profile-*generic* manual fallback — for any
future GitLab/Gitea profile both the URL path and the scope names are wrong.
**Direction:** move the token-creation URL template and scope list onto
`Profile` (GitHub values become that profile's data), degrading to a generic
"create a personal access token on <host>" message when a profile defines none.
**Acceptance:** the wizard contains no forge-specific URL literals; a table test
covers a GitHub-profile URL and a template-less profile.

### 2.2 Forge provider parity (go/forge + forge-github/-gitlab/-gitea/-bitbucket)

#### 2.2.1 `ListReleases` is single-page in all list-capable providers; release notes truncate; no rate-limit handling (MEDIUM)

GitHub (forge-github/release.go:130-144), GitLab (forge-gitlab/release.go:153-169),
and Gitea (forge-gitea/release.go:138-158) all pass `limit` into a single-page
request with no pagination loop, while `GetReleaseNotes` requests exactly 100
(`releasesPerPage`, GTB update.go:50, 1159). GitHub/GitLab cap `per_page` at 100
and Gitea instances commonly cap at 50, so any history beyond one page silently
yields "No release notes found" or a truncated changelog; no provider handles
rate-limit responses either. **Direction:** decide the `Provider.ListReleases`
contract once (§4 Q1) — document `limit` as "first page, provider-capped" or
paginate until `limit` is satisfied (Bitbucket's `fetchAllDownloads` shows the
loop) — and give providers a shared, documented stance on 429/`Retry-After`.
**Acceptance:** the `forge.Provider` doc comment states the chosen contract and
all four providers conform, with a test per provider.

#### 2.2.2 Gitea provider config scoping and Host normalisation (MEDIUM)

The GitHub/GitLab factories sub-scope config via `forge.SubConfig(cfg, "<name>")`
so `url.api` resolves as `github.url.api`/`gitlab.url.api`; the Gitea factory
passes the unsubbed root (forge-gitea/init.go:12), reading a top-level `url.api`
while auth comes from `gitea.auth.*`. Separately, `ReleaseSource.Host` is a bare
hostname for GitHub/GitLab but must be scheme-qualified for Gitea — an operator
who sets `Host: git.example.com` by symmetry gets a `HostTrusted` pin that can
never match (no scheme → empty parsed host → fail-closed) so tokens are silently
never attached. **Direction:** make the Gitea factory sub-scope like its
siblings, and normalise Host handling in one shared forge helper (accept bare
host or full URL, canonicalise before pinning) used by all four adapters.
**Acceptance:** `gitea.url.api` resolves from the `gitea` section, and a bare
`Host: git.example.com` yields a working `HostTrusted` pin (cross-adapter table
test).

#### 2.2.3 GitHub redirect security boundary is dead code — doc/impl drift (LOW)

`forge/provider.go:56-70` specifies that callers must refuse a non-empty
redirect URL (GTB's update.go:814/994 duly do), but the GitHub adapter passes
`httpclient.NewClient()` as `followRedirectsClient`
(forge-github/release.go:147), so go-github follows the redirect internally and
always returns `redirectURL == ""` — the documented boundary never engages (no
credential leaks; the following client is token-free). **Direction:** align doc
with implementation — amend the contract to allow internal redirect resolution
with a credential-free client, or pass `nil` and vet the returned URL against an
allowlist. **Acceptance:** contract comment and adapter agree, with a pinning
test.

#### 2.2.4 Capability discovery degrades silently under Provider wrappers (LOW)

All capability lookups assert optional interfaces on the concrete provider
(GTB update.go:736, update_signature.go:212, setup/forge/ssh.go:54,
single.go:295). Any decorator that wraps `forge.Provider` and forwards only the
five required methods silently strips the optional capability interfaces — and
because every caller treats "not implemented" as graceful fallback, verification
quietly downgrades instead of failing loudly. **Direction:** establish a wrapper
convention *before* the first decorator appears — an `Unwrap() Provider` method
plus a `forge.As(p, &target)` helper that walks the unwrap chain (or a
`Capabilities()` accessor; §4 Q2) — and migrate the four call sites to it.
**Acceptance:** a forwarding decorator around a capability-bearing provider
still surfaces the capability through the helper (test).

### 2.3 repo module (go/repo)

#### 2.3.1 Role interfaces leak go-git concrete types and carry no `context.Context` (MEDIUM)

Every role interface in repo.go:95-168 traffics in go-git concrete types
(`*git.Repository`, `*git.Worktree`, `*object.File`, `plumbing.ReferenceName`,
`transport.AuthMethod`, `*git.CloneOptions`, …), and the network operations use
the non-context go-git forms (`git.Clone` repo.go:489, `git.PlainClone`
repo.go:572, `Push` repo.go:372, `Pull` repo.go:315) even though
`CloneContext`/`PushContext`/`PullContext` exist. A clone against a hung remote
cannot be cancelled, and on `ThreadSafeRepo` the mutex is held for the entire
uncancellable operation, wedging every other method. **Direction:** accept the
type leakage as a documented "thin veneer over go-git" stance, but add `ctx` as
the first parameter to every network-touching interface method and thread the
`*Context` variants through — cheap now, breaking later. **Acceptance:** all
clone/push/pull/fetch paths honour context cancellation (cancelled-ctx test
against an unresponsive remote), and the veneer stance is stated in the package
doc.

#### 2.3.2 `Repo.OpenLocal` initialises on any open error and ignores `branch` (MEDIUM)

repo.go:510-534 falls into `PlainInitWithOptions` on *any* `git.PlainOpen`
failure — corrupt repo, permission error, not just `ErrRepositoryNotExists` — so
a transient fault on a real project directory gets a fresh empty repository
initialised over it, destroying the evidence of the original problem; when the
open succeeds, the `branch` argument is silently unused. `InitLocal`/
`DiscoverRepository` (repo/init.go:41-52) show the module already distinguishes
these cases correctly. **Direction:** gate the init fallback on
`errors.Is(err, git.ErrRepositoryNotExists)` and return other errors verbatim;
either honour `branch` on the open path or drop the parameter as part of the
§2.3.1 signature break (§4 Q3). **Acceptance:** a permission-denied open
surfaces the original error without initialising; the `branch` semantics are
pinned by test.

### 2.4 aferobilly (go/aferobilly)

#### 2.4.1 `Chmod` silently no-ops, dropping executable bits on real worktrees (MEDIUM)

aferobilly.go:179-183 returns nil unconditionally from `Chmod`/`Chown`/`Chtimes`,
justified by memfs — but the adapter's flagship consumer is a go-git worktree
(repo/worktree_fs.go:39-52), which for local clones is billy **osfs**, where
chmod is meaningful and go-git derives committed file modes (100755 vs 100644)
from worktree modes: a scaffolder that writes a script through `WorkFS()` and
chmods it +x commits a non-executable 100644 blob. **Direction:** delegate
`Chmod` to the underlying billy filesystem when it implements `billy.Change`
(osfs does), keeping the nil no-op only for filesystems that don't.
**Acceptance:** over osfs, `Chmod(path, 0o755)` is observable on disk; over
memfs it remains a silent no-op.

## 3. Scope & release plan

Repositories touched, in landing order:

1. **go/forge + the four provider adapters** (§2.2) — the `ListReleases`
   contract, Host normalisation helper, and capability-unwrap convention live in
   the core module; the adapters conform. Core and providers publish **in
   lockstep** at matching minor versions, per the established convention.
2. **go/repo** (§2.3) — the context plumbing is an **API-breaking** signature
   change to the role interfaces; the module is pre-1.0, so it ships as a
   **minor** bump with the break noted in the commit body and a migration note
   in the module's docs. The `OpenLocal` fix rides the same release.
3. **go/aferobilly** (§2.4) — independent one-file change, `fix` patch release.
4. **go-tool-base** (§2.1 plus dependency bumps for 1-3) — the `pkg/setup` items
   are self-contained `fix(setup)` patches; the go.mod bumps (including the
   `mocks/` regeneration for the repo signature change) land last so GTB adopts
   the batch in one Release MR cycle.

Each item is a separate conventional commit; nothing forces a coordinated "big
bang" beyond the forge-family lockstep rule. All work is TDD with the per-item
acceptance criteria as failing-test starting points; GTB-side changes keep
≥ 90% coverage and `just ci` green.

## 4. Open questions

1. **Pagination contract (§2.2.1):** document `ListReleases(limit)` as "first
   page, provider-capped" (cheap, honest, pushes looping to callers) or make
   providers paginate until `limit` is satisfied? Proposed: paginate in the
   providers — the only real consumer wants "everything up to limit", and
   Bitbucket already behaves this way.
2. **Capability discovery (§2.2.4):** `Unwrap() Provider` + a `forge.As` helper
   (mirrors `errors.As`, composes with arbitrary wrapper stacks) versus an
   explicit `Capabilities()` accessor (no chain-walking, but every wrapper must
   re-plumb it)? Proposed: the `Unwrap` convention.
3. **`OpenLocal` branch semantics (§2.3.2):** honour `branch` on the open path,
   or remove the parameter while §2.3.1 is already breaking signatures?
   Removing is simpler if no consumer relies on it — audit call sites first.
4. **Decompressed-size bound (§2.1.2):** fixed 1 GiB constant, or scale with the
   compressed cap (e.g. 4× 512 MiB)? A fixed constant is simpler and any
   legitimate GTB-family binary is far below it.
