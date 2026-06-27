# Changelog

## [v0.24.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.24.0)

### Features

- **http**: cookie credential source for AuthMiddleware
- **generator**: layout-aware nav generation
- **generator**: quadrant-appropriate agentless boilerplate
- **generator**: quadrant-aware, public-conditional doc prompts
- **generator**: regenerate project --force migrates flat docs to Diátaxis
- **generator**: Godog coverage + fix layout-aware index generation
- **generator**: scaffold the neutral Diátaxis docs tree (skeleton)
- **generator**: quadrant-aware doc output paths (diataxis layout)
- **generator**: add manifest docs_layout + module_published fields

### Bug Fixes

- **generator**: validate --package path + review follow-ups
- **generator**: correct Diátaxis index links + persist --public-api
- **generator**: preserve all manifest-only properties on rebuild

## [v0.23.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.23.0)

### Features

- **grpc**: add AuthInterceptor
- **http**: add AuthMiddleware
- **authn**: add JWT/OIDC verifier with bounded JWKS cache
- **authn**: add credential verification core (API-key, mTLS, authorize)
- **gateway**: add WithMiddleware option for the REST surface
- **grpc**: add server rate limiter and client circuit breaker
- **http**: add server rate limiter and client circuit breaker
- **resilience**: add shared circuit-breaker and rate-limit cores

## [v0.22.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.22.0)

### Features

- **repo**: expose the worktree as an afero.Fs (WorkFS/WithWorkFS)
- **repo**: add aferobilly — a safe billy→afero filesystem adapter
- **chat**: add cross-provider fallback ChatClient (E1)
- **chat**: add provider-failover policy and HTTP-status classification
- **doctor**: add 'doctor report' redacted support bundle
- **osinfo**: promote OS-version string to a shared pkg/osinfo
- **man**: generate roff man pages from the command tree
- **config**: add unset, path, and edit subcommands
- **config**: add Container.ConfigFiles() accessor

### Bug Fixes

- **init**: skip credential wizards when stdin is not a terminal

## [v0.21.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.21.0)

### Features

- **cmd**: expose MCP gating via generate flag and enable/disable mcp
- **generator**: thread MCP exposure through manifest, template, and regen
- **root**: gate MCP tool surface via exposure selector
- **setup**: add MCP exposure markers and resolver
- **bitbucket**: thread FormOption through init entry points
- **release**: injectable release source + releasetest double

## [v0.20.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.20.0)

### Features

- **generator**: retire the obsolete --wrap-subcommands flag

### Bug Fixes

- **generator**: regenerate --dry-run logs "Would write" instead of "Writing"
- **generator**: don't persist an unresolved flag default on regenerate manifest

## [v0.19.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.19.1)

### Bug Fixes

- **generator**: key subcommand docs by full command path
- **generator**: preserve command descriptions on regenerate manifest
- **generator**: remove command fully de-registers the command

## [v0.19.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.19.0)

### Features

- **generate**: add-flag --shorthand for single-letter flag shorthands

### Bug Fixes

- **generator**: make regenerate non-destructive on real projects

## [v0.18.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.18.0)

### Features

- **cmd**: toggle built-in features with gtb enable/disable <feature>...
- **update**: configurable self-update check interval baseline
- **generator**: scaffold tools with a self-update policy
- **update**: opt-in three-state ForcedUpdate policy
- **generator**: --template flag and gtb template command group
- **generator**: fetch, cache, and overlay layering for custom templates
- **generator**: custom template overlay engine, descriptor and security model
- **generator**: git-init + initial commit (opt-out) and optional push on generate project
- **generator**: scaffold GitLab CI from phpboyscout/cicd components
- **generator**: richer default README for generated projects
- **props**: add TelemetryProvider interface and GetCollector getter
- **vcs**: split RepoLike into role interfaces (composite preserved)

### Bug Fixes

- **docs**: point docs site_url and generated README links at gtb.phpboyscout.uk
- **generate**: strip a leading host from --repo so projects can regenerate
- **generator**: reject Go reserved words as command names
- **generator**: recognise zensical projects in the docs-nav step
- **generator**: make AI doc-generation opt-in and respect --agentless
- **credentials**: never persist a credential alongside another storage mode

## [v0.17.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.17.0)

### Features

- **http**: add SecurityHeadersMiddleware applied to built-in docs surfaces
- **chat**: surface token usage from all providers
- **props**: add public propstest fixture helper
- **config**: add OnReloadError hook for rejected hot-reloads
- **config**: container-owned hot-reload watcher with candidate-validate-swap
- **config**: bind CLI flags into config precedence
- **vcs**: provider-aware repository auth for clone/push
- **cmd**: signal-aware execution context with graceful cancellation

### Bug Fixes

- **controls**: cancel health-check contexts on stop
- **controls**: warn when Register is called after Start
- **sign**: drop redundant signature buffer and fix inverted flag name
- **chat**: replace tool handlers on SetTools, fix system prompt, seed, empty tool args
- **generate**: validate org for two-segment repo paths
- **agent**: surface missing-binary exec error in single-dir tools
- **generator**: wire escapeShellArg/escapeMarkdownCodeBlock at render sites
- **trustkeys**: fix stale "ships empty" doc and propagate WalkDir error
- **signing**: harden signing chain — reject dup manifest, refuse PSS, log fingerprint
- **chat**: recover panicking tool handlers as tool-error content
- **telemetry**: honour at-least-once, roll back partial setup, sync BackendInfo
- **telemetry**: validate OTLP endpoint fail-fast in ParseEndpoint
- **config**: make GetDefaultConfigDir pure and create the dir at first write
- **props**: default Collector to a noop to uphold the non-nil invariant
- **setup,chat,generate,regenerate**: audit phase 8 error-idiom sweep
- **config,credentials,cmd-root,controls**: audit phase 7 residuals
- **errorhandling,logger,changelog,version**: audit — bug cluster
- **output,browser**: audit — markdown cell escaping and immutable scheme allowlist
- **generate**: validate type/name in non-interactive add-flag path
- **sign,keys,generate**: sign/wkd/docs CLI-edge bug cluster
- **agent**: reject leading-dash go_get arg and redact subprocess output
- **docs**: bind docs server to loopback and route serve through middleware
- **docs**: guard nil ask callbacks, snapshot search mode, harden renderer
- **http**: clamp retry backoff and refuse unsafe body resends
- **http**: gate client-IP proxy headers and harden server shutdown drain
- **vcs**: clamp GitHub PR per-page and derive empty enterprise upload URL
- **vcs**: host-pin bitbucket basic auth to the API host
- **setup**: correct update timestamp, empty-version, and config-dir handling
- **direct**: bound the version-endpoint read
- **setup**: harden WKD key trust with UID filtering and domain validation
- **output**: UTF-8/width-aware table cell truncation
- **output**: return cancellation when a spinner run is interrupted
- **keys**: refuse to clobber an existing private key without --force
- **generate**: preserve command metadata through add-flag regeneration
- **ci**: add 3-day minimumReleaseAge cooldown to Renovate automerge
- **generator**: tighten signing KeyID and normalize PublicKey ./ prefix
- **cmd**: demote interrupt notice from error to debug
- **controls**: idempotent start, real restart semantics, no busy-spin
- **generator**: close manifest/signing/AI-tool validation gaps
- **root**: bootstrap survives child PersistentPreRunE via EnableTraverseRunHooks
- audit — transport bugs (TLS fail-fast, status, rate-limit, telemetry buffer)
- audit — cmd-root/telemetry bug cluster (flush, nil-version, seal, config-set)
- audit — self-update correctness (Windows extract, offline require-flags, target path)
- audit tier 2 — security quick-fixes (redact, telemetry, vcs, http, setup)
- audit tier 2 — chat cross-provider contract conformance
- **vcs**: only send release token to the configured instance host
- **vcs**: guard nil ssh subtree in configureSSHAuth
- **vcs**: treat already-up-to-date pull as success in CreateBranch
- **config**: honour LoadFilesContainer missing-file contract
- **root**: decline update when the prompt cannot be answered

## [v0.16.2](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.16.2)

### Bug Fixes

- **chat**: log tool-failure stack traces at DEBUG, not WARN

## [v0.16.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.16.1)

### Bug Fixes

- **agent**: make golangci-lint a required verification gate

## [v0.16.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.16.0)

### Features

- **agent**: smarter, safer repair agent
- **generate**: add --max-steps and refresh default AI models

## [v0.15.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.15.1)

### Bug Fixes

- **enable**: only prompt for the email on a first enable
- **enable**: merge signing flags onto the existing posture, don't replace

## [v0.15.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.15.0)

### Features

- **generator**: write the GoReleaser signs block via `gtb enable signing`

## [v0.14.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.14.0)

### Features

- **generator**: scaffold release-signing via `gtb enable signing`

## [v0.13.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.13.1)

### Bug Fixes

- **setup**: wire WKD cross-check by setting DefaultExternalKeyEmail
- **keys**: point keys help at the GitLab repo, not the archived GitHub one

## [v0.13.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.13.0)

### Features

- **setup**: flip DefaultRequireSignature = true (Phase 2 close-out)

## [v0.12.2](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.12.2)

### Bug Fixes

- **release**: align goreleaser OIDC `aud` with IAM provider's client ID

## [v0.12.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.12.1)

### Bug Fixes

- **release**: accept OIDC env vars in sign-release.sh's credential guard

## [v0.12.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.12.0)

### Features

- **setup**: Phase 2 self-update signature verification

## [v0.11.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.11.0)

### Features

- **openpgpkey**: DetachSign + gtb sign command

## [v0.10.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.10.0)

### Features

- **openpgpkey**: WKD tree generator + gtb keys wkd command
- **keys**: add gtb keys {mint,generate} commands; revise D12 to RSA-only openpgpkey
- **signing/local**: add PEM-file backend, registers as local
- **signing/kms**: add AWS KMS backend, registers as aws-kms
- **openpgpkey**: add Ed25519 support (D12)
- **signing**: introduce pkg/signing — Backend registry for HSM/KMS signing keys
- **openpgpkey**: add pkg/openpgpkey — mint armored OpenPGP key from crypto.Signer

## [v0.9.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.9.0)

### Features

- **grpc**: add ServerOption pattern for multi-server config prefixes
- **http**: add ServerOption pattern to NewServer/Start/Register

## [v0.8.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.8.0)

### Features

- **http**: add WithCertPool client option

## [v0.7.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.7.1)

### Bug Fixes

- **gateway**: propagate trace context through the gateway's gRPC dial
- **telemetry**: register a controller-safe telemetry service

## [v0.7.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.7.0)

### Features

- **telemetry**: OTel-native observability (traces, metrics, logs) over OTLP

## [v0.6.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.6.0)

### Features

- **openapi**: serve OpenAPI spec + embedded Stoplight Elements docs
- **gateway**: grpc-gateway as a first-class transport

## [v0.5.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.5.0)

### Features

- **generator**: command composition emission (slices 3+4+6)
- **setup**: command composition foundation (slices 1+2+5)

## [v0.4.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.4.1)

### Bug Fixes

- **generator**: drop unused imports from generated command files
- resolve gtb install from GitLab releases, not GitHub

## [v0.4.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.4.0)

### Features

- **config**: add generic ValidateStruct[T] / SchemaOf[T] helpers

## [v0.3.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.3.0)

### Features

- **generator**: scaffold releaser-pleaser instead of semantic-release

### Bug Fixes

- **release**: add "# Changelog" header for releaser-pleaser
- **telemetry**: downgrade OTel sensitive-header advisory to DEBUG
- **release**: resolve public releases config-less via ReleaseSource
- **deps**: bump x/crypto, x/net, go-git for security advisories

## [0.2.3](https://gitlab.com/phpboyscout/go-tool-base/compare/v0.2.2...v0.2.3) (2026-05-14)


### Bug Fixes

* **deps:** regenerate hash-pinned lockfile after renovate version bumps ([f46a44f](https://gitlab.com/phpboyscout/go-tool-base/commit/f46a44fd77e04b4f7a2eef635395b133ca27e9ca))

## [0.2.2](https://gitlab.com/phpboyscout/go-tool-base/compare/v0.2.1...v0.2.2) (2026-05-14)


### Bug Fixes

* **ci:** drop missing-file coverage_report block from tests job ([792c5d2](https://gitlab.com/phpboyscout/go-tool-base/commit/792c5d2aceb2a413ae62a8c951402ae47981c331))

## [0.2.1](https://gitlab.com/phpboyscout/go-tool-base/compare/v0.2.0...v0.2.1) (2026-05-13)


### Bug Fixes

* **release:** switch homebrew tap push to SSH deploy key ([0d9d592](https://gitlab.com/phpboyscout/go-tool-base/commit/0d9d5924714b3ac8daa55977b6c6f67cc0239ad2))

# [0.2.0](https://gitlab.com/phpboyscout/go-tool-base/compare/v0.1.5...v0.2.0) (2026-05-13)


### Features

* **release:** restore homebrew_casks block pointing at gitlab.com/phpboyscout/homebrew ([cc08abf](https://gitlab.com/phpboyscout/go-tool-base/commit/cc08abf2a53acd3e5fab2f7fc56d5a21dd44baa3))

## [0.1.3](https://gitlab.com/phpboyscout/go-tool-base/compare/v0.1.2...v0.1.3) (2026-05-12)


### Bug Fixes

* **release:** drop homebrew_casks block pending tap decision ([a7e913e](https://gitlab.com/phpboyscout/go-tool-base/commit/a7e913ecf8fa99b05572eabf434090ff664bb844))

## [0.1.2](https://gitlab.com/phpboyscout/go-tool-base/compare/v0.1.1...v0.1.2) (2026-05-12)


### Bug Fixes

* **release:** drop the skip-ci directive so tag pipeline runs goreleaser ([b174ba1](https://gitlab.com/phpboyscout/go-tool-base/commit/b174ba11399d67dce279970f0ce4676bc2d40edf))

## [0.1.1](https://gitlab.com/phpboyscout/go-tool-base/compare/v0.1.0...v0.1.1) (2026-05-12)


### Bug Fixes

* **ci:** set GOTOOLCHAIN=auto on goreleaser so it can fetch go1.26 at runtime ([20ffd03](https://gitlab.com/phpboyscout/go-tool-base/commit/20ffd0300bd3f33f6e16ca7d3bbb9fb3df0950a4))
