# Changelog

## [v0.45.2](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.45.2)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.45.1...v0.45.2)

### Notes

- v0.45.1, like v0.45.0, published every object but did not move the
  static channel's pointer; this release does, and starts the chain.

### Bug Fixes

- **release**: the pointer is read with a signed GET, and a refusal is shown ([bdba351](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bdba35176c65ea8068e2572c14320773cc548e10))

## [v0.45.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.45.1)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.45.0...v0.45.1)

### Notes

- v0.45.0's binaries reached the store but its pointer did not move, so no
  tool was offered it; this release moves the pointer and starts the
  static channel's chain. A gtb installed before v0.45.0 updates through
  the GitLab release as before.

### Bug Fixes

- **release**: the pointer publisher is handed the store's credentials ([0a5c15e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0a5c15eaf953b1a4a8632f3312870d48b5a7e013))

## [v0.45.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.45.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.44.0...v0.45.0)

### Notes

- A generated project can now self-update from a static location instead
  of its forge: `gtb generate project --release-channel static
  --release-base-url <https URL>`, or "A static location" on the wizard's
  self-update page. The release configuration publishes the pointer and
  per-tag manifests the tool reads (GoReleaser Pro). A project generated
  with `--no-forge` can enable `update` this way. The direct channel's
  flags and manifest keys are withdrawn; an existing `release_source.direct`
  block is dropped on the next regenerate. See the migration note
  docs/reference/migration/v0.x-static-release-channel.md.

- gtb now checks for and downloads updates from
  https://pkg.phpboyscout.uk/go-tool-base directly, reading a small pointer
  and a per-release manifest there, so `gtb update` no longer consults
  GitLab at all. An installed gtb from before this release still updates
  through the GitLab release's asset links, which point at the same store.
  The layout is documented in docs/reference/static-release-channel.md.

- A generated project's release now runs the binary it built, `<tool> version --ci`, before publishing anything, so a release whose binary cannot start fails instead of shipping. Existing projects pick this up on their next `gtb regenerate project`; a project that lists `.goreleaser.yaml` in `.gtb/ignore` adds the `hooks.post` block to its build by hand.

- `update` no longer prints "Update complete" when the running binary is already the latest release; it says nothing was replaced, exits 0, and `--output json` reports `"updated": false`. The post-update config refresh now works on GitLab-hosted tools: it ran `init` with the GitHub profile's `--skip-login` flag, which a GitLab tool's `init` refused, so the refresh had never run for them.

### Features

- **cli**: the wizard and flags offer the static release channel ([4ed5d1a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ed5d1a80183ba9f56f5f78a29229edceac3bee1))
- **generator**: a project can release on the static channel ([92355ec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/92355ec5260b1087bc897cf71088c951a75bf30d))
- **cli**: gtb publishes and self-updates from the static release channel ([86c75af](https://gitlab.com/phpboyscout/go-tool-base/-/commit/86c75af9ff8277a249c1063cdbe1e1c3df8031c5))
- **setup**: the updater takes the static channel from the tool's release source ([451e924](https://gitlab.com/phpboyscout/go-tool-base/-/commit/451e9240e39c042541a046aac522b5f253cccdab))
- **release**: static.Channel reads the pointer, the chain and the files they name ([49aafd5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/49aafd51f21fbda7396aadfada17e00f1f88084c))
- **release**: releasemanifest reads the dist goreleaser has before publishing ([dbbfe20](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dbbfe202ebab0282f2b2161ca6cf93def41eb23f))
- **props**: ReleaseSource.BaseURL and the static channel layout reference ([fb2bbc0](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fb2bbc0fe61d0c8091f97f94f86b1ff3227bfe14))
- **release**: go tool releasemanifest writes a tag's manifest from goreleaser's dist ([8716168](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8716168466cd0f378220cceff924acef32b6d6cc))
- **release**: the static channel's two documents, shared by writer and reader ([18fb54b](https://gitlab.com/phpboyscout/go-tool-base/-/commit/18fb54b38a233805c4987f6c514d6838eac5b8d8))

### Bug Fixes

- **release**: the build runs the binary it made before anything is published ([c0a0742](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c0a0742883676f2f1b9fbba24873250e19424143))
- **update**: say when nothing was replaced, and refresh config with no profile's flag ([ea04f5f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ea04f5fe3d5f850f3a398b2d29eacf3e18333b81))

### Other

- **setup**: the updater reads releases through a ReleaseChannel seam ([0359d41](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0359d417097175b54d60d4205087d17bcc11130a))

## [v0.44.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.44.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.43.0...v0.44.0)

### Notes

- Release archives, SBOMs and signatures are now published to https://pkg.phpboyscout.uk/go-tool-base/<tag>/ and the release links and the Homebrew cask point there; GitLab's package registry no longer receives new releases.

- The project wizard always offers the Chat providers page; the AI defaults page (default provider and model) appears only with the `ai` feature selected.

- `docs ask` is now gated on the `ai` feature, as the feature's description always said; a tool with `docs` and without `ai` no longer offers `ask`. `doctor` on a tool without `ai` reports the chat providers it links as information rather than warning about `ai.provider`.

- `chat.providers` in the manifest (and `--chat-providers` on `generate project`) now wires the named go/chat provider modules into the binary whether or not the `ai` feature is enabled, so a tool that uses chat from its own code declares its providers without enabling `ai`. `--chat-providers` defaults to none; the `ai` feature with no list still links every provider. `gtb disable ai` no longer removes `cmd/<name>/chat.go`; it keeps the list and tells you `gtb unset chat.providers` drops it.

- `regenerate project` now drops the legacy `tool` lines whatever path or major an older scaffold recorded them at (`cmd/gtb` as well as `cli/cmd/gtb`, golangci-lint v2 as well as v1), so a project scaffolded before the CLI moved to `cli/` no longer fails `go mod tidy` on a package that no longer exists.

- The generated `.gitignore` now ignores `pkg/cmd/root/assets/site` beside `assets/docs`; a project that added the line by hand can drop its edit on the next regenerate.

- A project scaffolded before forge features existed (its manifest names the forge only under `release_source.type`) now has that forge enabled by its first `regenerate project`, so `cmd/<name>/forge.go` links the adapter and `update` works without `gtb enable <forge>`. A project that carried `cmd/<name>/keychain.go` with no `keychain` entry in its manifest keeps the file: the first regenerate records `keychain: true` instead of removing it.

- A generated tool without the `ai` feature no longer carries an empty `cmd/<name>/chat.go`, and one hosted on no forge no longer carries an empty `forge.go`; the next `gtb regenerate project` removes them. Both are `DO NOT EDIT` files the generator owns, so nothing of yours is touched. `gtb enable ai` writes `chat.go` back, `gtb disable ai` removes it.

### Features

- **release**: publish archives to the release store, not the package registry ([d53ec32](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d53ec32e8b2a260214a0c0cbd04a1014e61ba804))
- **wizard**: the chat providers page is permanent, the AI defaults follow the feature ([071f215](https://gitlab.com/phpboyscout/go-tool-base/-/commit/071f215e6a113cd9744627ee39e0687120b6a9fe))
- **ai**: docs ask follows the ai feature, and doctor names links without judging them ([4b4cbd5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4b4cbd53da4d9925fb55ae5ae6b82dd55be36ee7))
- **generator**: chat providers are linked from the manifest whether or not ai is on ([eec3dba](https://gitlab.com/phpboyscout/go-tool-base/-/commit/eec3dbacf9749e10d0e09fedd971a35a007b89d2))

### Bug Fixes

- **generator**: the legacy tool directives are dropped at any path or major ([6776687](https://gitlab.com/phpboyscout/go-tool-base/-/commit/67766877433f662d6ef516fb6e4301a9930cc743))
- **generator**: the skeleton .gitignore covers the site go generate builds ([04518b7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/04518b70797d5a8d2cbe50ae273b5c020349f3b3))
- **generator**: a pre-0195 manifest gains its backend's forge, and a keychain file is recorded ([da0d9f9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/da0d9f9323f2eed28b7aebc9be8af30d3efccc01))
- **generator**: chat.go and forge.go exist only for a feature the tool uses ([294cb90](https://gitlab.com/phpboyscout/go-tool-base/-/commit/294cb90d7a20a1e64fd54cecaa2eb8b604971981))

## [v0.43.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.43.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.42.0...v0.43.0)

### Notes

- `init <forge>` now runs its credential wizard when the bundled `auth.env` default names a variable that is not set, instead of treating the default as configured.

- `gtb regenerate project` now runs golangci-lint without `--fix`, so it never rewrites a file it did not generate; `gtb generate project` still fixes what it emits. A lint blocked by another running golangci-lint is retried once and then reported as such, not as a verification failure.

- `version` no longer contacts the release source in CI (`--ci`, the `ci` config key, or `CI=true`); it says the live check was skipped. `version --check` still asks.

- A tool whose release source is Bitbucket now builds its release client from the shipped defaults and from the wizard's env-reference mode; the bundled `username.env` and `app_password.env` pointers are read by GTB and handed to the adapter, which no longer reports them as stale.

- `gtb generate command --parent <cmd>` on a hand-edited parent registers the child and keeps the edit, reporting the divergence instead of recording the edited file as generated.

- `gtb set release_source.backend` refuses a forge that contradicts `release_source.type`.

- `changelog` lists releases newest first, writes plain Markdown when its output is not a terminal, and its JSON carries `version`, `category` (by name), `scope` and `description`.

- A binary built from a commit rather than a tag no longer runs the self-update check or warns that it is out of date.

- `init ai` in literal or keychain mode now refuses a blank API key when there is none to keep, instead of reporting success and storing nothing.

- `config validate` no longer reports `azure.api.*` or `features.<id>.enabled` as unknown keys.

- A tool that links one chat provider needs no `ai.provider` even when that provider's module registers others; the default is chosen from the providers the tool declares.

- `gtb generate` and `gtb regenerate` exit 2 when they refuse an invocation (a missing or invalid input), as the reference documents; verification failures still exit 3.

- `gtb regenerate manifest` rebuilding a manifest from scratch now recovers the module path from `go.mod`, so a project not hosted on a forge no longer regenerates with an empty module path in its lint, release and mockery configuration.

- `gtb regenerate project` no longer empties the command table in `docs/reference/cli/index.md` or writes a stray `docs/commands/` tree on a Diátaxis project.

- `gtb regenerate project` now raises the project's `go-tool-base` requirement to the version of the gtb running it, so a project scaffolded by an older release upgrades with that one command and no `go get`.

- `init --accessible` on a piped stdin now runs the credential wizards as line prompts instead of skipping them for want of a terminal.

- A wizard run with `--accessible` on a piped stdin now receives every answer; only the first used to arrive.

- The gtb generator's wizards (`generate project`, `gtb wizard`, `generate command`, `generate add-flag`) now honour `--accessible` and `GTB_ACCESSIBLE=true`, running as line prompts that can be piped.

- A generated tool now embeds its documentation: `go generate ./...` fills `pkg/cmd/root/assets/docs` beside the changelog, so the `docs` command works in a release build. An existing project picks the directive up on its next `gtb regenerate project`.

- `gtb regenerate project` no longer overwrites, on its second run, a hand-edited command file it kept on the first; and a project with the external adapter attached keeps compiling after `generate command` and `regenerate project`.

- Wizards now render with a small margin from the terminal's edge instead of flush against it.

- The generate wizard's environment-variable-prefix page now defaults to the prefix derived from the project name (`my-app` gives `MY_APP`), with None and Other as the alternatives, instead of an empty field.

- Known issue: a tool whose release source is Bitbucket cannot build its release client from the shipped defaults, because the adapter reports the bundled `username.env` and `app_password.env` pointers as stale (go-tool-base #89). Export `BITBUCKET_USERNAME` and `BITBUCKET_APP_PASSWORD` under those names until the fix ships.

- The direct URL release channel offered by the wizard and `--release-channel direct` is withdrawn from this release. It was recorded but never rendered into the generated tool, and its intended shape (discovery and retrieval from a static location through a published manifest, independent of any forge) is being designed under go-tool-base #90. Self-update in this release requires a project hosted on a forge.

- A self-update check that fails (an unreachable release source, a credential that cannot be read) is now retried at the check interval rather than on every command.

- A forge credential that is configured but cannot be read (a malformed keychain reference, a locked keychain) now fails the update client's construction with the reason, where it used to build an unauthenticated client that the forge refused later.

- `gtb regenerate manifest` no longer drops `release_source.backend`; a manifest rebuilt from scratch derives it, so a generate, delete-manifest, regenerate round trip reproduces the manifest exactly.

- `gtb regenerate project` now brings a project scaffolded by an older gtb to a tidy `go.mod` on its own. It drops the `tool` directives for gtb, golangci-lint and mockery that scaffolds carried before they became installed binaries, and raises the forge and chat adapters, keychain and signing links it owns to at least the versions this gtb was built against, reporting each change. A module you added yourself is never touched. With the entry-point fix already in this release, a v0.42.0 project upgrades with `gtb regenerate project` and no hand edit.

- `gtb regenerate project` now rewrites `cmd/<name>/main.go`, `internal/version/version.go` and `pkg/cmd/root/generate.go` to the current skeleton on every run, as it already did the root command. A project scaffolded by v0.42.0 regenerates to a building entry point without a hand edit. These files carry no hash and never prompt; add a `.gtb/ignore` rule to keep a hand-edited one.

- The `mcp` feature is now a link, like `keychain`: a tool has the `mcp` command only when its `main` blank-imports `gitlab.com/phpboyscout/go-tool-base/pkg/mcp`, and a tool without the import ships without `go/mcp` and the MCP SDK. A generated project gets the import as `cmd/<name>/mcp.go` on its next `gtb regenerate project`; a hand-wired tool adds the one line. Until then the tool builds but has no `mcp` command. See the migration note "the mcp feature is a link".

- A generated project's go.mod is now edited in place rather than re-rendered: the generator seeds the direct require lines its imports imply at the versions gtb was built with, your own lines survive every regenerate, and a run without Go on PATH (or with --no-verify) keeps every requirement instead of leaving an empty require block. go mod tidy still runs where it can and owns the result.

- Generated cmd/<name>/chat.go and forge.go now declare their chat providers and forge adapters as link features, so doctor and init ai report the providers the author chose rather than every one the linked modules register. Run gtb regenerate project to pick the declarations up in an existing project.

- The feature core is now the `gitlab.com/phpboyscout/go/features` module (docs at https://features.go.phpboyscout.uk); `pkg/features` is removed and every symbol is in the module unchanged, so a consumer changes the import path and nothing else. Sentinel kinds are `features.<name>`.

- Dynamic feature flags have a seam: `features.Backend`, `features.Dynamic`, `Props.Flags`, and `pkg/setup/flags` with a config-store backend (`features.<id>.enabled`) and a controls registration. A feature opts in with `Dynamic: true` on its descriptor; built-ins are static. No vendor SDK ships; a vendor is an adapter module implementing `Backend`. See docs/how-to/dynamic-feature-flags.md.

- `templates.FeatureCatalogue` is `templates.Catalogue()` (rows are `props.FeatureDescriptor`, `ID` not `Cmd`). Import `gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain` rather than `go/credentials/keychain` to link the OS keychain: it also declares the `keychain` feature (`props.KindLink`) so doctor and the generator see it. `credentialposture.Register` contributes to the feature registry; `DeclaredFor`, `ReportFor`, `RegisterOn` and `RegisteredIn` are added. `GTB_FRAMEWORK_REPLACE=<dir>` makes a generated `go.mod` replace the framework with a working tree, for development. See docs/reference/migration/v0.x-features-as-a-value.md, phase 3.

- `gtb generate project` refuses an unknown `--help-type`, a Slack or Teams help type with no channel, and `--signing-key-id` without `--signing`.

- Correction to the `.goreleaser.yaml` `main:` fix above: an existing project applies it by editing the `main:` line to `./cmd/<name>` by hand. `gtb regenerate project` re-emits the file only where it is unmodified; a customised one (a `signs:`, `notarize:` or `uploads:` block) is a conflict, and `--overwrite allow` would replace those blocks with the skeleton's.

- `Tool.IsEnabled`/`IsDisabled` are removed; read `p.GetFeatures().Enabled(id)`. `props.New` is the construction path (the scaffolded root returns its error and `main` exits 2); `setup.InitialiserProvider` takes the run's `*pflag.FlagSet`; `setup.Chain` and the `setup.Get*` readers give way to the root's `Chainer` and `features.ContributionsOf`; `--skip-key` is the init command's own flag. Enabling a feature no import declares now fails construction. See docs/reference/migration/v0.x-features-as-a-value.md, phase 2.

- The generated `.goreleaser.yaml` now builds `./cmd/<name>` (the package) instead of `cmd/<name>/main.go` (one file), so released binaries link the keychain and signing backends the generated `cmd/<name>/*.go` files declare. An existing project picks the fix up on `gtb regenerate project` where its `.goreleaser.yaml` is unmodified, or by editing the `main:` line by hand.

- The feature registries no longer seal: `props.SealFeatures`, `props.ErrRegistrySealed`, `setup.SealRegistry`, `setup.Seal`, `setup.IsSealed` and `setup.ResetRegistryForTesting` are removed. Readers take an immutable snapshot, so a late registration is not seen rather than a panic; tests build `features.NewRegistry()` instead of resetting the process's. `props.FeatureID` and `props.FeatureKind` are now aliases of `features.ID` and `features.Kind`. See docs/reference/migration/v0.x-features-as-a-value.md.

- `Props.IO` carries an invocation's stdin, stdout and stderr with the process's as the default, and `--accessible` (or `GTB_ACCESSIBLE=true`) runs every wizard as line prompts. A wizard now refuses a stdin that is neither a terminal nor accessible instead of failing inside the form.

- `gtb wizard` runs the generation wizard again over an existing project, every page pre-filled from the manifest, and applies the answers with the same validation and sync as `gtb set`; `--dry-run` shows what would change. On a revisit the signing page also offers `require_signature`.

- A generated project's `go.mod` no longer carries `tool` lines for gtb, golangci-lint or mockery; the README says how to install the pinned gtb (`go install …@<version.gtb>`), and the justfile and CI already run golangci-lint and mockery as installed binaries. Existing projects lose the lines on their next regenerate, with the golangci-lint v1 pin and the dependency that made adding `chat-gemini` fail to tidy.

- `gtb generate project` and `gtb regenerate project` exit 3 when the files were written but `go mod tidy` or `golangci-lint` failed afterwards, naming the step; `--no-verify` skips verification and exits 0. Exit 2 remains the usage error.

- `gtb set <path> <value>`, `gtb unset <path>` and `gtb get <path>` change, clear and read a generated project's author settings by manifest path, with the same validation as the generate flags and the same sync as `regenerate`; editing the manifest by hand is no longer the documented way.

- `gtb enable`, `gtb disable`, `gtb enable signing` and `gtb attach` now leave the project tree in line with the manifest in one command; no `regenerate` is needed afterwards. `keychain` is toggleable (`gtb disable keychain` removes `cmd/<name>/keychain.go`), and a hand-deleted generated file comes back on the next regenerate: a durable override is a manifest field.

- The manifest now records the Go version (`version.go`), so `regenerate` no longer rewrites `go.mod` when the author's toolchain moves. `gtb generate project` gains flags for telemetry endpoints, bootstrap posture, config layers and update enforcement, and the wizard a Telemetry page. Every author setting has one manifest home, and a test holds the table and the code to each other.

- `init ai` offers only the providers the binary links. `doctor` gains a **Chat providers** check that names the module to import for an unlinked `ai.provider` or fallback member, and drops the old **API keys** count; the credential resolution check reports a chat credential only when its provider is linked.

- A tool that links exactly one chat provider needs no `ai.provider`: the framework uses the one it links. With several linked and none configured, the error names them.

- The `gtb generate project` wizard asks the AI decision on one page: which providers to link, which is the default (narrowed to what is ticked, and never chosen for you), and a model, with an endpoint page only for the providers that need one.

- `init ai` offers every provider the framework knows, skips the credential forms for a local CLI or Bedrock, and its `AI_PROVIDER` note now describes the variable as the fallback it is.

- The framework no longer defaults an unset AI provider to Claude (or `docs ask` to OpenAI). With no `ai.provider` in config and no `AI_PROVIDER`, constructing a chat client fails with a hint naming what to set. A generated tool ships its author's default; an end user runs `init ai`.

- `gtb generate project` records the author's default chat provider, model and endpoint in the manifest (`chat.default`) and ships them as the generated tool's embedded defaults beside `chat.go`. One linked provider is its own default; several require `--chat-default-provider`. An existing project that links several providers regenerates unchanged and warns until its author names one.

- The whole `ai:` section now reaches the chat client: `ai.model`, `ai.base_url`, `ai.api_version`, `ai.project` and `ai.location` configure the primary provider, so `openai-compatible`, `azure-openai`, `gemini-vertex` and `bedrock` are configurable from a file. Azure OpenAI has a credential root (`azure.api.*`). The fallback chain is built by go/chat, which keeps the primary's model and endpoint and lets every other member resolve its own.

- `init` and `doctor` now follow the tool's enabled features: a tool without a forge feature no longer offers `--skip-login`, and `doctor` reports only the credentials of features the tool enables.

- the generate wizard now refuses an invalid project name, env prefix or repository, and an empty chat provider list, at the field rather than after the last page.

- the `gtb generate project` wizard asks whether the project is hosted on a forge, then the forge details or a Go module path, and puts the release channel, update policy and check interval on one self-update page shown only when the update feature is selected.

- `gtb generate project` no longer accepts forge names in `--features`; the forge is chosen with `--forge-backend` (which replaces `--git-backend`, removed) and further forges for credentials with `--forge-credentials`. A default project now links its backend's adapter and can update itself. `doctor` reports a release source with no registered provider.

- tools with a forge feature enabled can check for updates again when no token is in the environment (every CI image); the update check reported the configuration as stale since go/forge v0.8.0 stopped reading `auth.env`.

- in the generate wizard, answering No to release signing after entering details on the signing page now wins; the details are discarded.

- a manifest with `chat.providers: []` now keeps that choice across regenerates instead of reverting to every provider; regenerate refuses a provider name no module registers.

- the generate wizard no longer blocks on the Git Host page when the default host is wanted; leave it empty. The env prefix is offered as a suggestion rather than pre-filled; press tab to accept it.

- `gtb regenerate project` now fails, writing nothing, when the manifest's signing block is invalid. It used to log an error, remove the generated signing wiring and exit 0.

- gtb v0.41.1, v0.41.2 and v0.42.0 shipped without their embedded documentation and changelog (`gtb docs` and `gtb changelog` failed). This release restores both; no action is needed beyond updating.

### Breaking Changes

- **features**: the feature core is the go/features module ([47f52e8](https://gitlab.com/phpboyscout/go-tool-base/-/commit/47f52e84e245431cba38571f93c7b322fe254305))
- **generator,props,credentialposture**: the catalogue derives from the registry, keychain is a link kind, credentials are a slot ([66cbfaa](https://gitlab.com/phpboyscout/go-tool-base/-/commit/66cbfaa569b77863975cb9dd47b255bc7602f98d))
- **props,setup,root**: the root owns its feature set, middleware chain and flag state; props.New is the construction path ([4a2dfde](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4a2dfded3cdcd66a9523d9c954fc5e3c434416e0))
- **features**: the feature registries snapshot instead of sealing, and tests build their own ([2e26247](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2e2624751bacc70ba55ca3398742737088314715))
- **utils**: utils.IsInteractive and pkg/utils are removed; the gtb CLI asks its Props ([6b3b759](https://gitlab.com/phpboyscout/go-tool-base/-/commit/6b3b759d8e7b7810c3093ea18a307d6c8d9eb99e))
- **setup**: the dependency-substitution options give way to props.Tool fields ([55c6397](https://gitlab.com/phpboyscout/go-tool-base/-/commit/55c6397ec81858174ace08a86209ca82053fd9b3))

### Features

- **generator**: the wizard asks the MCP mode, and a revisit asks the surface ([d0b49fd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d0b49fdcec2d3455e0ba8d0d9ac68ce1996dcd10))
- **generator**: framework links share one artefact convention, and mcp is the second ([1481343](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1481343e9a58c80d794557527a2ca21f5e59b9dc))
- **mcp**: pkg/mcp is the link that declares the feature and contributes the command ([3a702ed](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3a702ed1dfd6baa52713d2109c1ac1224d5c55fb))
- **setup**: a root-command contribution slot ([824e0e3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/824e0e39e603e5d46758a53034b0e8e27b23d626))
- **setup**: a protocol-stdout marker, and the consent guard reads it ([da83354](https://gitlab.com/phpboyscout/go-tool-base/-/commit/da83354e5da42cb1233c3d854411aeb9fb3bb4bf))
- **props**: the root's log level is Props.LogLevel ([e17dc19](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e17dc194a88ff81cabbc95855ecd066d16e19320))
- **mcp**: the mcp command is served by go/mcp, and ophis goes ([5c2d886](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5c2d886419de75d3145e8d98cc7eef3985494ff5))
- **generator**: properties.mcp.mode is recorded and rendered into the generated root ([f02b3f9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/f02b3f9cda78ffb12e264a6bd2bf90a51e80a534))
- **setup**: a pure command group stays recognisable after the chain wraps it ([d12082b](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d12082ba125c489f2d5bf809507307fe38252bda))
- **props**: a tool declares its MCP publication mode ([273a000](https://gitlab.com/phpboyscout/go-tool-base/-/commit/273a00075d61cbb43124233787f956d9bb3ed1c4))
- **generator**: gtb annotate records a command's MCP hints in the manifest and its cmd.go ([13aaf4c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/13aaf4c9211f8643f487f2725ade84d2f44d3c89))
- **setup**: a command declares its MCP tool annotations, and every built-in declares its own ([3d13f9e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3d13f9e19d2cdadd8380b8d71514aaed821770e2))
- **generator**: verification declines with a reason when Go or golangci-lint is not on PATH ([3e2e4a7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e2e4a7320c3a32817705e16dde55359070db9c1))
- **generator**: the scaffold's go.mod is seeded in place, and tidy stays the owner ([00e8688](https://gitlab.com/phpboyscout/go-tool-base/-/commit/00e8688b3ac2328c4971e7a9149a36f4ada5b91d))
- **generator**: the gomod package seeds a go.mod with the requirements the generator implies ([dcd20c1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dcd20c1a9b26c608caaed6ec9de70b7a734fd5df))
- **generator,props,chat,forge**: generated adapter links are declared link features ([a6ddc40](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a6ddc40d6938eaa8d449ef0e3775e10bad968b4d))
- **features,setup**: the dynamic-flag seam, with the config store as the first backend ([9c3a788](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9c3a788f7933c8bfac3255dbdb5306cc7273b19a))
- **props,setup**: Props carries the invocation's streams, and one helper runs every form on them ([01a01a3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/01a01a3f5a43b0fe53e1a8d8437e0c8ef8bfc875))
- **generator**: gtb wizard runs the generation wizard over an existing project ([0f05dc5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f05dc51dedc912e2bf7b36e7cbb2bdadeb4478d))
- **generator**: the generated go.mod names no gtb, golangci-lint or mockery tool line ([868c905](https://gitlab.com/phpboyscout/go-tool-base/-/commit/868c9055b333c5f9b64dfa51c65e73e8548a3519))
- **generator**: generate and regenerate exit 3 when the files they wrote could not be verified ([2b63595](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2b6359579b3bf212f8465c0875f3c64380a8a8f4))
- **generator**: gtb set, unset and get change an author setting after generation ([f4950b9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/f4950b940588e599db2be2fc41b97e39f449d93e))
- **generator**: one sync runs after every manifest write, and the keychain follows the manifest ([5a7a01a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5a7a01a2032827a65004c3b7148a6107a77d8f3d))
- **generator**: the manifest owns every author setting, and a table proves it ([2b16db4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2b16db461f34396154ca11b58d2009c39db91381))
- **props**: BoolPtr for the tri-state author baselines a generated root sets ([a44d6b6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a44d6b6b265b9853c4e652335561dd507d06802d))
- **setup,doctor**: init ai offers only the linked providers, and doctor checks them ([cf47e4e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cf47e4e5093b963fa39bdd96fb0d58d84ea6e264))
- **chat**: a tool that links one provider needs no ai.provider, and the registry miss is a sentinel ([70d6d5c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/70d6d5c8c676dbe97212c6a0ab41a0ce1a4d1582))
- **generator**: the wizard asks the AI decision on one page, with endpoint pages where a provider needs one ([4946a50](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4946a509ac2ac620214c64292b5625a56f699fa0))
- **setup**: init ai offers every known provider and asks for a key only where one exists ([702fd17](https://gitlab.com/phpboyscout/go-tool-base/-/commit/702fd17d23ba76264da40236874ec22c59ff6c9d))
- **chat**: one display and one credential-key table, and no vendor as the default ([87c1d1e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/87c1d1e8f16cfd266f44781ecfee2d91f3cfca2c))
- **generator**: the manifest records the author's chat default, and the tool ships it ([4e248fb](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4e248fb404e80f4c3aaca26bbdf09ce9ca1397f9))
- **chat**: the whole ai section reaches the client, and the fallback chain is the module's ([4190c6b](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4190c6b5e7f05211282a58bd123835457f45aa74))
- **generator**: the wizard asks three separable questions, each on its own page ([fb78761](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fb7876183f45733ce81bb884809e28a3ee2d1c00))
- **generator**: the forge backend implies the forge, and a default project can update itself ([9597276](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9597276d90b6b91a8ed7c492022c31c81eca1d8f))

### Bug Fixes

- **cmd**: man is listed once enabled, and a broken keychain reference has a kind ([674de73](https://gitlab.com/phpboyscout/go-tool-base/-/commit/674de7394c2dae98c59ca899ac2eb6ed8d4a1b9e))
- **config**: list masks declared literals, not the pointers and settings beside them ([05daf34](https://gitlab.com/phpboyscout/go-tool-base/-/commit/05daf343a020569e983fbc3c58d5a3c0853f47b6))
- **telemetry**: status honours --output json ([9395083](https://gitlab.com/phpboyscout/go-tool-base/-/commit/93950831bd3c7087b351a220c147d3522d6288b0))
- **setup**: the resolver-configured line logs at debug, not on every invocation ([63e77ef](https://gitlab.com/phpboyscout/go-tool-base/-/commit/63e77ef4c50481314a2e69c73a489ade0b7a76d3))
- **doctor**: runs without a config file and reports the first-run state as skips ([130e7dd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/130e7dd30f339920e7b245ce000f4247d4c27d3f))
- **setup/forge**: an env reference counts as configured only when it resolves ([14a3ddf](https://gitlab.com/phpboyscout/go-tool-base/-/commit/14a3ddf9a3be1669df5b1d3e4c1f769484a0d89a))
- **generator**: removing a parent's last child reshapes it back to a leaf ([803a8d8](https://gitlab.com/phpboyscout/go-tool-base/-/commit/803a8d8b79fdcb0d8bf3e13620d199f7cdd387ba))
- **generator**: regenerate lints without --fix, and a lint lock earns one retry ([999d046](https://gitlab.com/phpboyscout/go-tool-base/-/commit/999d046b3f1a718f8cc140bdbb261c5552829489))
- **generator**: a plain-HTTP telemetry endpoint is accepted and warned about ([fd03ba4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fd03ba4ce59a8a6dd570158eb9d36c7f16bdacc4))
- **version**: no live release lookup in CI ([08084b0](https://gitlab.com/phpboyscout/go-tool-base/-/commit/08084b0a709d4d2ed5c0ac8c8502a68ee76d50c5))
- **generator**: set refuses to rename the project ([4edc30e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4edc30e5a83a90a60ae2e7cdfd485f1dce41a9f9))
- **root,docs**: an unknown command gets the usage hint and exit code, and two log and help niggles ([68744cb](https://gitlab.com/phpboyscout/go-tool-base/-/commit/68744cbe9ef74445bbb8a16589a7152b564f31a9))
- **generator**: the seed writes the framework version without build metadata ([28c5146](https://gitlab.com/phpboyscout/go-tool-base/-/commit/28c5146a393823ceb781f4be856fab4da28e83bc))
- **generator,setup**: three niggles from the manual round ([a3f6f08](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a3f6f089ba592c154b6614f631eeb8ccf06981da))
- **generator**: set refuses the withdrawn direct channel's keys ([7b88ce7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/7b88ce7385aa53a0c679869c952c13ebf6f89dc6))
- **generator**: an integer default renders as one conversion, not int(int64(n)) ([0ffe660](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0ffe66088051b2fc7fbaaff54b1a4dcccda74a0e))
- **vcs,setup**: Bitbucket's two halves reach the factory through GTB's chain ([536e9d3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/536e9d33164f20b067fa58a28c1db62c68ffcce8))
- **generator**: a child added under a hand-edited parent leaves the parent's hash and edit alone ([b1b55bc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b1b55bc3ac11ce1cf4bb1ea17a60ddd84ef8f37c))
- **generator**: a backend that contradicts the release type is refused ([1ab48ad](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1ab48ad3126f292dff58fba1ff1ad166bb37633c))
- **changelog**: newest release first, plain Markdown on a pipe, named fields in JSON ([d1e62ec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1e62ec7daa8f1ca4a1bcc385a550107337c06ad))
- **version**: a Go pseudo-version is a development build ([277f2f0](https://gitlab.com/phpboyscout/go-tool-base/-/commit/277f2f0328f9d988a63e9b66d2f831b08165bbb6))
- **setup/ai**: a blank key with none to keep is refused, not saved as success ([2e179f2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2e179f2e70cb268bf68d560e5fe25b2179a6d4ed))
- **config**: validate derives the framework's sections from what the tool declares ([dcd4fb1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dcd4fb1f557178b1a82f533ccfea7daeb9646189))
- **chat**: the default provider is chosen from the providers the tool declares ([db8346f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/db8346fafac4ccdf18580e1b11adc7deef7b9be2))
- **generate**: a refused invocation exits 2, as the reference says ([c793aae](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c793aae037d8727cb3d0515d71628ef1243c6fa5))
- **generator**: a from-scratch manifest recovers the module path from go.mod ([b934cd9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b934cd901494247974d5dd04d24ea8b9cedf6088))
- **generator**: regenerate keeps the Diátaxis command table and writes no legacy docs tree ([1194ef8](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1194ef845e7f67461891ed339a9531713a306333))
- **generator**: the seed raises the framework's own require line to the running gtb ([947eeb1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/947eeb1f4cbde0f16b7e3ae6607030a6c4b7c6da))
- **setup,cmd**: one prompt rule everywhere: a terminal, or accessible on any stdin ([1d7e223](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1d7e223f1a57a7792b3df75434867dc6b8b0b070))
- **setup**: an accessible form on a pipe receives every answer, not only the first ([37d3501](https://gitlab.com/phpboyscout/go-tool-base/-/commit/37d35017e8c86119517b19e945aa08859467a99e))
- **generate**: the generator's wizards honour --accessible and run on the invocation's streams ([c1fc2b8](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c1fc2b8ce644c818720034448758f7c0fb663502))
- **generator**: a generated tool embeds its documentation ([bd9486d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd9486da399a2b09377df96bdeb718f45f982eb3))
- **generate**: the module page accepts empty as the project name, so it never traps a change of mind ([13d9b81](https://gitlab.com/phpboyscout/go-tool-base/-/commit/13d9b816184dcd64e144482844e24e57f9f54573))
- **generator**: a kept hand edit on a parent command survives the next regenerate, and a command joins an adapter root inside its literal ([09ecd32](https://gitlab.com/phpboyscout/go-tool-base/-/commit/09ecd3234e79e5c2e41afd9c3751edc6c4fd0ba4))
- **setup,cmd**: every wizard renders set in from the terminal's edge ([5bf19ef](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5bf19ef10d001f3a16b68f94379924a1fd07f783))
- **generate**: the release-channel row names the chosen forge and says what the channel is ([94cf48c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/94cf48c734613c15e1c5d97f22cc15914c5cd7f0))
- **generate**: the env-prefix page is a select with the derived name as its default ([3d162d0](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3d162d0c7a4c71afd3fb579151d3e50b1933fb75))
- **generate**: withdraw the direct release channel until its design lands ([f5803e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/f5803e65130723f12d4d0b24c21bb45aa7a9a257))
- **root**: a failed update check is stamped, so it retries at the interval ([1db0d6b](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1db0d6b5e96922718ad56d102317b55514fe4f85))
- **vcs,setup**: hand the forge factory GTB's credential chain through forge.WithCredential ([e550a46](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e550a46d8e3d2360faa3927d4a098841157eef4f))
- **generator**: regenerate manifest keeps release_source.backend and derives it from scratch ([fbda8db](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fbda8dbc24a85091fd26abafd51304292b0fe68f))
- **generator**: the seed drops the pre-phase-4 tool directives and keeps owned adapters in lockstep ([9a5be69](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9a5be69bde8e9b6226935377f4be5a8db17a87f0))
- **generator**: a pre-0200 manifest's go.mod hash is dropped, not refreshed ([c885bfa](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c885bfaee0a51e5bce38d6177eaf56e3c33f4a1e))
- **generator**: regenerate brings the entry point, version package and generate directives to the current skeleton ([2a791f2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2a791f22dee7c9db9a304afaafaf5b97fc9c745f))
- **deps**: every module to its latest release ([97f8ed9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/97f8ed98cb833de7a28f14341cbeae7cb3e4b10d))
- **generator**: a sealed root command is not rewritten, and the run reports what it did ([16f88da](https://gitlab.com/phpboyscout/go-tool-base/-/commit/16f88daa861ee280898df608706bfc9de2ebd746))
- **generator**: the agentless docs flag table is read from the command's source, shorthands included ([8c33e61](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8c33e61c14a6f434445472ba6a100bc576dbde39))
- **generator**: regenerate consults a chat provider only when --update-docs asks, and never under CI ([d5543be](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d5543be8e38decef8c23a4e021056f3d0701c721))
- **generator**: a command's main.go imports only what it uses, and CI compiles and lints a scaffold ([a886dd3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a886dd3dba23df5616500b046f4d389d9d2ac0bd))
- **docs,setup,props**: the missing-assets hint and key note say what is true, and the reference facts match the code ([944cc37](https://gitlab.com/phpboyscout/go-tool-base/-/commit/944cc37e5f9b330072e92e96e79baa230302b2f6))
- **chat,setup**: the ai link test no longer registers a provider the wizard tests then count ([4b013e7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4b013e78afe01484175c2e18ae6f03d8c73ea8ee))
- **generator**: the provenance file is written beside the manifest it records ([8d79af0](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8d79af09cff5e5fee8abc345b49ece412235d51f))
- **generator**: closed-set flags are refused outside their set, and a flag with no effect is refused rather than dropped ([9f71b7c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9f71b7cbe8651646f57f29a71dcdcf8c2a7d3239))
- **generator**: the generated .goreleaser.yaml builds the main package, not main.go ([6c3c17e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/6c3c17ef81c86cc844b9aca34ab4dc71d52d10da))
- **generator**: the staged regenerate commit rolls back when it fails part-way ([d755770](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d755770f62ad3f8e81f5200c61b858c5ff01fdd4))
- **init,doctor**: a narrow tool advertises only the flags and credentials of its enabled features ([7cb3df1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/7cb3df1f8a6e6760a2a201b3848d5afe300adaf7))
- **generator**: the wizard refuses at the field what the generator refuses afterwards ([b0f3efd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b0f3efd81dfa41216abb450ab9f2b31c0741dae0))
- **generator**: the forge backend, module path and release channel are fields the manifest records ([bd3e833](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd3e833d27eb38281b5d9f849022473755109dfe))
- **generator**: one flag-type table behind every site that renders a flag ([13caf07](https://gitlab.com/phpboyscout/go-tool-base/-/commit/13caf073dcd55fe4f495778589d127354d056580))
- **vcs**: hand provider factories a config that resolves GTB's credential instead of the keys it dereferences ([9dc1580](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9dc1580711e90be2b7c809c9107686b7e1e5af5b))
- **generator**: a page hidden at the end of the wizard contributes no answers ([ae9125a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ae9125a0ba4bc436f460ac8cd146e3ea9eec6cb3))
- **generator**: keep an explicit empty provider list, and refuse unknown providers at regenerate ([c1c8502](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c1c85022190a7ef6e41a799c5fbb41fb434d16e1))
- **generator**: size every wizard multi-select to show all its options on first paint ([08070fb](https://gitlab.com/phpboyscout/go-tool-base/-/commit/08070fbd4526f0c377082b89f8be672a62828ae4))
- **generator**: persist telemetry endpoints in the manifest so regenerate keeps them ([7137183](https://gitlab.com/phpboyscout/go-tool-base/-/commit/71371835c0474b4e14923b142620c3f86f9fbbd0))
- **generator**: the wizard ticks the current selection, not the defaults plus it ([1c2e206](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1c2e206685fde672742e039fa8676ceb293553d3))
- **generator**: the wizard offers the derived host and env prefix instead of seeding values huh cannot show ([48dbc48](https://gitlab.com/phpboyscout/go-tool-base/-/commit/48dbc48c5de777c249f6b6cb3bad881e65166dc0))
- **generator**: refuse to regenerate on an invalid signing block instead of dropping enforcement ([a0b35ad](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a0b35ad474632056ff54b3d252dc8a29d2df97e7))
- **release**: generate the gtb CLI's embedded docs and changelog where the build can find them ([22e0c45](https://gitlab.com/phpboyscout/go-tool-base/-/commit/22e0c4556332905285a95db2462c1c4a006951e4))
- **version**: a +dirty build of the latest release is a development build, and is not behind it ([f3e7392](https://gitlab.com/phpboyscout/go-tool-base/-/commit/f3e739254ddbd615748a7640f2e8b209abde003f))
- **props**: serve .xml assets verbatim, and build the one stray sentinel with NewSentinel ([bfb0388](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bfb038849a919f90d7bda6fe86c4dc16027976a2))

### Other

- **config,credentialposture,chat**: declare the remaining config keys once ([2a8468e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2a8468e66a30afc9c103b547b8a83c3bd816590f))
- **setup,cmd**: nothing in pkg/ names the process's streams ([8ec4349](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8ec4349aae55944f83d021e3d9d7be68f6df07c1))
- **setup/forge**: the device-code prompt runs through RunForm on Props.IO ([45a3380](https://gitlab.com/phpboyscout/go-tool-base/-/commit/45a338076c1f1240e8c49538e865fe945c4a909a))
- **cmd/root**: the update and consent prompts run through RunForm on Props.IO ([be00d78](https://gitlab.com/phpboyscout/go-tool-base/-/commit/be00d7840bb1bc7c8b26bb72fb99c76e5f7e325a))
- **setup/telemetry**: the consent prompt runs through RunForm, and its tests answer it ([afcfd73](https://gitlab.com/phpboyscout/go-tool-base/-/commit/afcfd734235c2374aade6af4cffd2115ebe59280))
- **setup/forge**: the SSH stage is one real form, and its tests drive it ([d5f666e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d5f666ed652954060d6d26c6fb1f0c52623df467))
- **setup/forge**: the dual-credential wizard is one real form, and its tests drive it ([14f480a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/14f480a9666c36254f37a2b8c23350d454960dbb))
- **setup/forge**: the single-token wizard is one real form, and its tests drive it ([d28981d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d28981d6f36b5dd610d2c4f18a0d5861224b6fd7))
- **setup/ai**: the ai wizard is one real form, and its tests drive it ([ea3fb37](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ea3fb3769a8f83435c3ddf52f3b294c16bebd105))
- sweep the parameters nobody read, and let revive keep it that way ([35fe36a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/35fe36a07510f717cca84363e4fc12c48ff2d83a))
- **transport**: share the config-selection plumbing between the http and grpc adapters ([73f5df2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/73f5df216c926b46caf8b1a450186f120b850c7d))
- **props**: Version and Assets are the concrete types their one implementation always was ([cbb200a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cbb200afdb6e90264d9af699f52a706ef3859b60))
- **setup**: resolve the update trust settings from config.Reader, not two ad hoc interfaces ([57471de](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57471de8a281ec1ffbc53459ddc356a127407257))
- **generator**: type the overwrite mode, and validate the help type in the manifest ([49ccb65](https://gitlab.com/phpboyscout/go-tool-base/-/commit/49ccb657383e7759ba78d54c71e523b60c5b72ee))
- **config**: declare the update, telemetry and ai config keys once ([5cbbc1a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5cbbc1acee695366260bfbe28fe6cd164339e033))
- **pkg**: remove exports nothing calls and fix three comments left behind by the moves ([93f7be9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/93f7be94e4e0d13931e6fd10a338fd3b0d472b8b))

## [v0.42.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.42.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.41.2...v0.42.0)

### Features

- **generator**: explain each wizard option, and offer every chat provider a module registers ([7fd9df6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/7fd9df6a50590dae68123b299e9406da3f5da560))
- **generator**: emit a tool's chat providers and forge adapters from its manifest ([d571684](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d57168441a9bd045501bcb20346a2d9e77aea153))

### Bug Fixes

- **cli**: build the generator against framework v0.41.0 ([2e1cbc2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2e1cbc24994055eaae6964640d92bac3408b2121))
- **generator**: write the chat block into a new project's manifest ([612d686](https://gitlab.com/phpboyscout/go-tool-base/-/commit/612d686379da63b075bded640c8b6e47e7f83703))

## [v0.41.2](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.41.2)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.41.1...v0.41.2)

### Bug Fixes

- **release**: publish the assets where colophon looks, SBOMs included ([a54a314](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a54a31480a01cd0787cc84bde99ab5cc63e64c39))

## [v0.41.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.41.1)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.41.0...v0.41.1)

### Bug Fixes

- **release**: give the cask a download URL, and fan out on root tags only ([2e77b2d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2e77b2ddbae9e091d9f971c2fda9519f5b6a09b6))

## [v0.41.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.41.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.40.0...v0.41.0)

### Notes

- The framework no longer registers chat providers or forge adapters. A tool that builds its own main must blank-import the modules it uses (chat-anthropic, forge-github, and so on); gtb-generated tools get them from the manifest. The gtb CLI is a nested module: install with go install gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb@latest. See docs/reference/migration/v0.x-adapters-registered-by-main.md.

### Features

- **cli**: move the gtb CLI to a nested module and stop registering adapters in pkg ([369844e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/369844e98b0e6553369b70ba5b95be4a72fb6ebb))
- **setup**: report a forge feature whose adapter is not linked ([29ede15](https://gitlab.com/phpboyscout/go-tool-base/-/commit/29ede15c57845b0dc9b23b921f435e8f6aff05f8))
- **setup**: say which refusal a forge returned ([87a7e68](https://gitlab.com/phpboyscout/go-tool-base/-/commit/87a7e68e38b802449a8deb5f7c112e7259563c16))

### Bug Fixes

- **release**: sign with the public key at its moved path, and keep generated-tool paths in the docs ([c4adc5a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c4adc5a0d943f813d6fcd135a300b7b5ca716a88))
- **deps**: follow the controls and chat contracts the toolkit round brought ([b2fc134](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b2fc134f904f78d98fcc63b0518b5aec470f2726))
- **deps**: update go modules ([bc11031](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bc11031847e8fcefcc0520c32b03567b975b86e9))
- **deps**: update module google.golang.org/grpc to v1.83.2 [security] ([73678a9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/73678a93b112b3bac281527d5da5c2b1c21388bc))
- **deps**: update module golang.org/x/crypto to v0.56.0 [security] ([8928b22](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8928b2249abca4893b761b0a8ddc4f859f5388bf))
- **deps**: update module go.opentelemetry.io/contrib/bridges/otelslog to v0.20.1 ([c8bd5bd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c8bd5bd38f420cfffdddead2b9c3c12897838fd2))
- **deps**: update opentelemetry-go monorepo ([eeeb5e5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/eeeb5e51915b99f3addfa7bb80c6daa955a26f28))
- **deps**: take the forge family to the current round ([29f61ac](https://gitlab.com/phpboyscout/go-tool-base/-/commit/29f61ac2eddb30a556d7fe6e38afe6da9a07d10d))

### Performance Improvements

- **ci**: read the coverage profile go-test already produced ([551488c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/551488cfa5b1ab07d72913fd7ff381f0298c583f))
- **generator**: let a generation skip the golangci-lint pass ([15734d1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/15734d14ef1883ed6e831df878b0fe3712b1621a))

## [v0.40.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.40.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.39.1...v0.40.0)

### Features

- **vcs**: address a release source by forge endpoint ([7f0c036](https://gitlab.com/phpboyscout/go-tool-base/-/commit/7f0c03636b7f4e604986528936c76354d2d52d66))

## [v0.39.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.39.1)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.39.0...v0.39.1)

### Bug Fixes

- **deps**: take the signing modules to the named-instance releases ([508a8e1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/508a8e1cd1937a3ae2589ac5d0b174a9b3b08dd1))
- **deps**: take moby/go-archive to v0.3.0 for GHSA-hfg8-hc9c-6c3h ([b42456f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b42456f850d38af8f96fa1d69494556fb1465316))
- **cmd**: drop the local group wiring for keys, now upstream carries it ([97d205b](https://gitlab.com/phpboyscout/go-tool-base/-/commit/97d205b8f31168d534e14bd06c60aa3295f84881))
- **setup**: take the unknown-verb wording from errorhandling ([50a6de4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/50a6de4d9c0f5c850fc4dd10e7efac4208296ca2))
- **generator**: state a command's positional arguments in its usage line ([5fb23a3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5fb23a360bde72134376779f1f61298462641717))
- **cmd**: inject generate and regenerate's persistent flags ([2ab8559](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2ab8559f643faba25cccd32cbc0cb69811a76ad7))

## [v0.39.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.39.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.38.0...v0.39.0)

### Features

- **setup**: add GroupRunE, the behaviour of a pure command group ([c600671](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c600671e8abed95b93705cbf3271041b21b8b841))
- adopt the chat family at chat v0.10.1 and adapters v0.9.1 ([3e8130f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e8130f0ebd880da0a71deae0e3633b647091dd6))

### Bug Fixes

- **cmd**: wire the group default into gtb's own command groups ([6211f2c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/6211f2c4fc784eb34299c827f5077281e875d51e))
- **generator**: say so when a regeneration changes what a command does ([9ff7af2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9ff7af2d2e0d289ad667b37ad2823b7d86d00f22))
- **generator**: emit the framework default for a group with no run logic ([2b90739](https://gitlab.com/phpboyscout/go-tool-base/-/commit/2b907399f3c8d644baad35ba17d5fe0a01a077d1))
- **generator**: classify a command group that has no run logic of its own ([d0f9f58](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d0f9f586c6dd8ffdac7c231d09af48a8315d5390))
- **deps**: update module gitlab.com/phpboyscout/go/config to v0.17.0 ([5aec217](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5aec217f0ad18697cc4cc2bf47c41766c09874b2))
- **deps**: update go modules ([a1ef6a9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a1ef6a93a4f23fad6216d00b12974145fca13372))
- **deps**: track the image pinned as a component input, and bump it ([aa39cd8](https://gitlab.com/phpboyscout/go-tool-base/-/commit/aa39cd81838e5cf6d5f724bba0795bbb4dfd44a0))

## [v0.38.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.38.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.37.1...v0.38.0)

### Features

- **config**: finish a credential migration properly ([f87f70c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/f87f70c73c47073f391cc1cb21ef631e87008984))
- **doctor**: let health checks decide the exit code ([a2f9114](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a2f91145d3184c6524011071e83e021a9805e29e))
- **credentials**: refuse a plaintext fallback when a keychain regresses ([55dba9d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/55dba9da099d416c1db5bf71b001d0123c410391))
- **credentials**: choose the default storage mode from the environment ([db9fc1a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/db9fc1a9e1488c432d43e087f94e6055e86f54ae))
- **doctor**: read the credential inventory from declarations, not a list ([1d5095c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1d5095cde7caf4142c787e5518606072cf328e6e))
- **credentialposture**: report which source supplies each credential ([577496b](https://gitlab.com/phpboyscout/go-tool-base/-/commit/577496be2500d4b879f87af1558d5272301ac460))

### Bug Fixes

- **deps**: take golang.org/x/mod to v0.40.0 ([ada049d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ada049d8cbc90c6243abef0696faaee42ba0b360))
- **ci**: bump the cicd components to v0.36.0 for Go 1.26.6 ([e6833b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e6833b250387ddf68b2980befdc90e95a0a8f4ae))
- **generator**: give golangci-lint a cache scoped to the linted directory ([d75e0a7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d75e0a72b235b5e661ee7c7ca5f6edf18882e145))
- **generator**: do not call a Run stub a seal prevents from existing ([5253e59](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5253e592cb43fc829ce3da9d44bc8bd1a8d0aa43))
- **doctor**: only a gating warning fails a run ([c0c35b4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c0c35b4ef3c13c80a16b30906f1254f526f36b04))
- **credentials**: exclude CI from the interactive storage-mode default ([e28a7c2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e28a7c28a887ba6090f232cb13b61b63b780c801))

## [v0.37.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.37.1)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.37.0...v0.37.1)

### Bug Fixes

- **generator**: call the Run stub a command group is given ([f3ef266](https://gitlab.com/phpboyscout/go-tool-base/-/commit/f3ef266237bd554ea917bfd3a01aa1c1ba50f120))
- **deps**: update go modules ([b367af1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b367af1de65aabd8046bb6de6d6bedd83ed0fc8b))
- **deps**: update config to v0.14.0 ([5ce9020](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5ce9020df5c0c1e2f1849f486b4cf7a4c671851c))
- **generator**: stop a sealed rule being ignored when creating main.go ([1e455da](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1e455dade96f1274c10be7220882184444444120))
- **generator**: reconcile the manifest rebuild instead of replacing it ([8c914b0](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8c914b0436393c6d33b2caba80379932bc73432d))
- **generator**: record command file hashes after post-processing ([3577ed2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3577ed2be360191619dbb8ffb95febed916ba1e3))
- **deps**: update go modules ([e7143c4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e7143c47a06d3656b8260ba593bced72bf00211b))

## [v0.37.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.37.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.36.1...v0.37.0)

### Features

- **generator**: split regeneration from wiring in .gtb/ignore, and add sealed rules ([fd0d7a3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fd0d7a31778702c0b215a43f11bc95175cf2b02a))
- **ci**: announce releases to Discord ([89d4d8c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/89d4d8c6e54edde78e995dc0a5a4e34d8dabee47))

### Bug Fixes

- **deps**: update forge-gitlab to v0.8.0, pairing it with forge core v0.11.0 ([41ccf06](https://gitlab.com/phpboyscout/go-tool-base/-/commit/41ccf0669499dbdd778dea34cfd54f3fed23e501))
- **deps**: update forge-github to v0.8.1, dropping the duplicated go-github major ([dff2590](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dff25905c56056498a39beba92e3bd98a31e6d3c))
- **deps**: update module github.com/testcontainers/testcontainers-go to v0.44.0 ([175f918](https://gitlab.com/phpboyscout/go-tool-base/-/commit/175f91851164ab19747bd70811b4b1815810d077))
- **deps**: bump the otel core and contrib families together ([7174bfe](https://gitlab.com/phpboyscout/go-tool-base/-/commit/7174bfe294929f5f0c134d56192db625d12abc29))
- **deps**: update module github.com/google/go-github/v89 to v90 ([576776f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/576776f1ddd2e1ef83e13ba06da09343d911e2c3))
- **deps**: update module github.com/grpc-ecosystem/grpc-gateway/v2 to v2.30.0 ([517158b](https://gitlab.com/phpboyscout/go-tool-base/-/commit/517158b9a6ab4b67800970eea7a876bab1fcd522))
- **deps**: update go modules ([d7c2e17](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d7c2e173310350f0f5282770ab6f7d370b887ae6))
- **generator**: record the commands index hash when it is rewritten ([821ad81](https://gitlab.com/phpboyscout/go-tool-base/-/commit/821ad8125f696593b718a7f5d221f7cef94157ef))
- **generator**: honour .gtb/ignore in docs writes and keep a kept file's hash ([43341de](https://gitlab.com/phpboyscout/go-tool-base/-/commit/43341def92272842a89ac57508f9566b4ccda0a6))
- **generator**: complete regeneration on a project with hand-modified files ([e9c0284](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e9c02841be93f9752c1d12a43cc7e03e694e6014))
- **deps**: update the forge family to forge v0.10.0 ([4a70c21](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4a70c21763ca88d93b981c3c8baa34f46707c84c))
- **deps**: update go modules ([07d5a5e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/07d5a5e16d695e9f91b4cc49d5b7d462f7164151))
- **deps**: update go modules ([dda8084](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda808431dbd470978f58a2bca814f8e38a3b628))
- **deps**: update go modules ([9f90479](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9f90479e0110ea8f13809dbc7aa486f0de2297bf))

## [v0.36.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.36.1)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.36.0...v0.36.1)

### Bug Fixes

- **deps**: update go modules ([8f65a46](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8f65a4642def2e4203d191e8cbc347e0da4761a3))

## [v0.36.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.36.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.35.0...v0.36.0)

### Features

- **logger**: present an error's hints where a person can read them ([ef46e81](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ef46e81d856c37d2ed3ff3e18f15eb2c80e1ed08))
- **errors**: adopt go/errors and errorhandling v0.2.0 ([ac93b15](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ac93b1568eb01047ddaca5b7e7a5a40085a2f8b5))
- **doctor**: report whether a forge credential resolves, and from which rung ([506e62d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/506e62d958f36f37873e0a5ef59e2fdec752e388))
- **forge**: add Codeberg as a first-class forge with its own credentials ([c9efc47](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c9efc470dcc0f2c4426a0736e953bf25db445e79))
- **generator**: declare a project's config layer set in the manifest ([8300a5d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8300a5d3d701ffdb977ca6e27cf21de1497ce92d))
- **props**: let a tool declare which config layers it wires ([b1ccad7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b1ccad748f4879d0bdf8462f84b0f23353a82ac9))
- **vcs**: own forge credential precedence in GTB's config stack ([a00a9df](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a00a9df2800f29e87ef172eb1d0728262ae0ec47))
- **forge**: offer SSH keys to every forge that can accept them ([111dd9a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/111dd9ae13a3141126fa292b8d11991d7e760267))
- **generator**: make forge features scaffoldable ([a92428f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a92428ff6773a620be9cef7391109fdd138ca7de))
- **setup**: add GitLab and Gitea forge profiles with per-forge config bundles ([4cebb19](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4cebb1938a88c0fc0130d687759f807f4b2a3000))
- **props**: complete the feature descriptor and scope the catalogue guard ([c688fc4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c688fc4bd0fea96921011c08eb2c833b6ee99537))
- **props**: make features self-registering, and fix the doctor blind spot ([a76696e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a76696e834b519358484f4bfaf82d8fcd7ceb821))
- **props**: rename FeatureCmd to FeatureID ([8502185](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8502185aac1635533cd7c5d98aa9cb49af8b7834))

### Bug Fixes

- **deps**: update go modules ([948ada5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/948ada54583c5e27cb74860c65085f8940b4b816))
- **generator**: build scaffolded releases with -trimpath ([ed22af4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ed22af4b1869b109de6d799bfab64937bf3ac37c))
- **deps**: update go modules ([28847ea](https://gitlab.com/phpboyscout/go-tool-base/-/commit/28847eabec37bd0d343c1e3ce0b6fa57752e780a))
- **root**: stop warning that a subtree-shaped credential is empty ([0b57e56](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0b57e56baec5d47ce9b91c0532e18caf99f5c0b7))
- **forge**: honour --skip-key for every forge that offers SSH ([e8fc9f0](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e8fc9f0a7fe8f7b24447673f93bc8d6cb1c63dbc))
- **generate**: validate --features and derive the selectable set ([e7041b1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e7041b1838d051253691f626d9e0e6a41ee60c85))
- **generator**: give the scaffolded initialiser its context parameter ([b59ae5a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b59ae5a6862470723cf2fc952cf86dc871b466ff))

## [v0.35.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.35.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.34.0...v0.35.0)

### Features

- **controls**: adopt go/controls v0.2.0 single-owner signal handling ([dcc77d3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dcc77d32b3dcdaf10371adb5ff89ced6f5145598))
- **root**: let a tool opt out of the framework's signal handling ([eda2247](https://gitlab.com/phpboyscout/go-tool-base/-/commit/eda22473e57eda29dfef7e9e90ca36d1e699862e))

### Bug Fixes

- **setup**: stop rendering an empty host in manual-token guidance ([fe9c5d5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fe9c5d5ef509ce7ba06f42b41f5556cf2c83877e))
- **generator**: bring the gitlab skeleton back to fleet standard ([34405bf](https://gitlab.com/phpboyscout/go-tool-base/-/commit/34405bfd00fdedfe48face114e6c73206e7c1bb1))
- **transport**: adapt to the WithTLSPair register option ([a552fa1](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a552fa1737d86baf0da26282ac54b45b039752cd))
- **deps**: update go modules ([6d482d3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/6d482d356a22a0556832daea738de9856e4a4547))
- **e2e**: give the signal scenarios their signal channel back ([9ab3b09](https://gitlab.com/phpboyscout/go-tool-base/-/commit/9ab3b091d322ad2ce12f33c238ae4db7395b1ae3))

## [v0.34.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.34.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.33.0...v0.34.0)

### Features

- **cli**: add gtb attach/detach commands with how-to and BDD coverage ([fe96ee5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fe96ee5a61ce39ea9fb307105a72381f307cb252))
- **generator**: render external command attachments into the root ([641443e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/641443e9fbb2cb282ddc1dad227c2c6c6eabb38a))
- **generator**: add external_commands manifest schema and validation ([c2ca9a5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/c2ca9a547925e3b9735ac2e8150885efe565ffeb))
- **signing**: source sign/keys from the extracted go/signing-cli module ([cb73d1c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cb73d1cf7ffee3cee2d497c393e63e6ecc5f618b))
- **generator**: add gtb ignore command and .gtb/ignore discoverability (#3) ([fc2b1bd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/fc2b1bd1decbe8bef86e52d421f8e2d6971acdcd))
- **root**: project-local config trust, bootstrap robustness, and cleanups ([b615038](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b61503881a3b2d582d235506ed4c7b1929d829cf))
- **props**: add validating New constructor, prune dead provider interfaces ([b6a5acb](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b6a5acbccaa2798824036a47f17755d30a903479))
- **grpc**: expose host bind-address key and warn on unknown options ([09cefec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/09cefec268b89567999632662ef24d6b33c0d57d))
- **http**: expose host bind-address key; reject invalid ports and unknown options ([df9e300](https://gitlab.com/phpboyscout/go-tool-base/-/commit/df9e3009b84648c75592f434f4385a1b1be00a32))

### Bug Fixes

- **deps**: update go modules ([64c9251](https://gitlab.com/phpboyscout/go-tool-base/-/commit/64c9251c3ccab3032c03de9dca56171de429c130))
- **generator**: track scaffold version pins with Renovate and bump to head ([d76c90c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d76c90ceaf19f41039dfb06fe4588e9e66df79f7))
- **generator**: conflict-check the CLI index and TTY-guard the conflict prompt (#6) ([84edc2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/84edc2f9527e20a5a113e9a180aec6fad1bb6325))
- **generator**: enable/disable signing must respect .gtb/ignore and inject safely (#4) ([f5e719a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/f5e719a9697cc2bbab1ee2188cca72485ac4e402))
- **generator**: frontmatter-first docs output and --no-ai-attribution flag (#7) ([ff3d1ba](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ff3d1bab0516e746d8c107a6f2be03008df91690))
- **setup**: harden self-update, SSH-key, PAT-wizard, and capability discovery ([8a7e1b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8a7e1b20a526f427655eaed58298614349ec4517))
- **generator**: batched MEDIUM/LOW follow-ups from the architectural review ([95ec27f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/95ec27fbf78ee38576fbf0feeb0fbec90484c381))
- **telemetry**: make the spill-cap prune part of the at-least-once contract ([b02e51f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b02e51f46bb0154774205d8d60fee1436765188f))
- **setup**: resolve middleware Props from command context; add project trust store ([82f12bb](https://gitlab.com/phpboyscout/go-tool-base/-/commit/82f12bb906611456b78dc9aa65fb8109a6471199))
- **deps**: update go modules ([bbd0c3d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bbd0c3d5b8bb7a789c356849ec93d63ef8294249))
- **cmd/root**: retain and invoke the config watcher stop handle on shutdown ([7c0c43d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/7c0c43d51ec577ed969c886f78c4ae65c31bcb4a))
- **root**: TTY-guard the pre-run telemetry consent and update prompts ([59e2e69](https://gitlab.com/phpboyscout/go-tool-base/-/commit/59e2e69dd6e6ee6fa7395baf5f277bd63e537c7e))
- **deps**: complete go-github v89 migration ([26803ab](https://gitlab.com/phpboyscout/go-tool-base/-/commit/26803ab06920492b88687576295e61795ec22f10))
- **deps**: update module github.com/google/go-github/v88 to v89 ([abac6e3](https://gitlab.com/phpboyscout/go-tool-base/-/commit/abac6e32ab1aabefbf08ac83df91ac17a1147545))
- **deps**: update go modules ([7a4fc47](https://gitlab.com/phpboyscout/go-tool-base/-/commit/7a4fc473dd1ccf6dfe14bc701a3def41b536ab59))

## [v0.33.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.33.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.32.0...v0.33.0)

### Features

- **trustkeys**: rotate trust anchors to the v2 dual-trust set ([5036845](https://gitlab.com/phpboyscout/go-tool-base/-/commit/5036845120523a4c96f7721935b484db4e5b9cfb))
- **sign**: add --append to merge signatures for dual-sign windows ([991646c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/991646cdda8541b2071acb739f71365467f0e6ba))
- **root**: exempt auxiliary commands from the framework bootstrap ([a3c8b3e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a3c8b3ecf0a00d1f91b6443a3329d3fdf4074f28))

### Bug Fixes

- **version**: degrade gracefully when the release source is unreachable ([54358d9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/54358d9d901707b5422275c7217908158077fb4a))
- adapt to changelog.Parse now returning (*Changelog, error) ([b2e672f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b2e672fa0a0466985375ee079e4db681daea9710))
- **deps**: update go modules ([67baaeb](https://gitlab.com/phpboyscout/go-tool-base/-/commit/67baaeb30f9ba43dbc87531bfc9d4cff676e1238))
- **setup**: refuse implicit self-update downgrades without --force ([72db4a8](https://gitlab.com/phpboyscout/go-tool-base/-/commit/72db4a832079f2d9bd5edc3008e6333da73d3df4))
- **generator**: close manifest-validation gaps behind CI-executed and code-generating sinks ([0585fac](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0585fac9f1bb3dddf5a0cccefa3852abf120a5d9))
- **deps**: adopt device-expiry-bounded forge providers v0.2.1 ([0b389b7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0b389b744bdcd49a4d8184e22956c6ed5deb325b))
- **setup**: scope credential-stage contexts per operation ([822875a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/822875a9cca30b71d411288aa879b6ab8f623986))

## [v0.32.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.32.0)

[Compare to previous version](https://gitlab.com/phpboyscout/go-tool-base/-/compare/v0.31.1...v0.32.0)

This release migrates GTB's configuration subsystem from the Viper-backed container to the extracted **`go/config` Store**, removing Viper from the dependency graph entirely. It is a breaking change on the pre-1.0 line: `Props.Config` is now a `*config.Store` — reads go through a pinned `props.Config.View()` (which satisfies `config.Reader`), writes go through the store's transactional `Apply`, and hot reload is explicit via `Store.Watch`. Downstream tools should follow the [configuration Store migration guide](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/docs/reference/migration/v0.x-config-store.md).

Highlights beyond the API change:

- **Segregated, always-on defaults** — embedded `assets/config.yaml` defaults merge per feature bundle and always apply, so a key absent from your file resolves to the shipped default rather than a zero value.
- **Corrected write routing** — the per-user config now overrides the system `/etc` file (the Unix convention, previously inverted), and `config set`/`unset`/`edit` land in the user config (created on first write, re-hardened to `0600`), never an un-writable system path.
- **Credential safety** — switching storage mode removes the previous mode's keys atomically, and `config set` warns before writing a recognised credential into a committable project-local `.<tool>.yaml`.
- **Quieter first run** — config-independent commands (`version`, `changelog`, `man`, `docs`) run before any config file exists, and `config validate` no longer flags recognised framework keys as "unknown".

### Suffix / End

This will be added to the end of the release notes.

```rp-suffix

### Features

- **vcs**: remove the superseded pkg/vcs/github wide client ([e0abfe7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e0abfe712c89d870fb4fb263058560f6f0b64048))
- **setup**: unify GitHub and Bitbucket setup into one forge-driven initialiser ([b0bbab5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b0bbab5d8eb004ccbc54beccb9e52a13339b0152))
- **deps**: adopt forge v0.2.0 provider account capabilities ([cf579cd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cf579cd1611838903c51f7c7d8b6983bf2cfe17a))
- **openapi**: consume the extracted go/transport-openapi module ([30e6154](https://gitlab.com/phpboyscout/go-tool-base/-/commit/30e61541dff94a24a73277071ec76d11b1d9decb))
- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **vcs**: remove the superseded pkg/vcs/github wide client ([e0abfe7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e0abfe712c89d870fb4fb263058560f6f0b64048))
- **setup**: unify GitHub and Bitbucket setup into one forge-driven initialiser ([b0bbab5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b0bbab5d8eb004ccbc54beccb9e52a13339b0152))
- **deps**: adopt forge v0.2.0 provider account capabilities ([cf579cd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cf579cd1611838903c51f7c7d8b6983bf2cfe17a))
- **openapi**: consume the extracted go/transport-openapi module ([30e6154](https://gitlab.com/phpboyscout/go-tool-base/-/commit/30e61541dff94a24a73277071ec76d11b1d9decb))
- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **vcs**: remove the superseded pkg/vcs/github wide client ([e0abfe7](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e0abfe712c89d870fb4fb263058560f6f0b64048))
- **setup**: unify GitHub and Bitbucket setup into one forge-driven initialiser ([b0bbab5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b0bbab5d8eb004ccbc54beccb9e52a13339b0152))
- **deps**: adopt forge v0.2.0 provider account capabilities ([cf579cd](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cf579cd1611838903c51f7c7d8b6983bf2cfe17a))
- **openapi**: consume the extracted go/transport-openapi module ([30e6154](https://gitlab.com/phpboyscout/go-tool-base/-/commit/30e61541dff94a24a73277071ec76d11b1d9decb))
- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **openapi**: consume the extracted go/transport-openapi module ([30e6154](https://gitlab.com/phpboyscout/go-tool-base/-/commit/30e61541dff94a24a73277071ec76d11b1d9decb))
- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **openapi**: consume the extracted go/transport-openapi module ([30e6154](https://gitlab.com/phpboyscout/go-tool-base/-/commit/30e61541dff94a24a73277071ec76d11b1d9decb))
- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **output**: consume the extracted go/output module ([57af634](https://gitlab.com/phpboyscout/go-tool-base/-/commit/57af634462f80f6efbc799a7b19daa95b047efd0))
- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **generate**: rewrite the wizards on native huh v2 and delete pkg/forms ([8b5732c](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8b5732c077150a19513728346a035440ee45699d))
- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

### Features

- **config**: warn before writing a credential to a project-local config file ([ebb082d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/ebb082d1cf8e3a8d2bcb6436c9057b5f1f2ebd09))
- **setup**: commit credential mode switches as one exclusive transactional write ([e4e5758](https://gitlab.com/phpboyscout/go-tool-base/-/commit/e4e5758b2384a86c6301de95dfb52a71026d5a6f))
- **root**: watch the config store for external changes ([8dfd129](https://gitlab.com/phpboyscout/go-tool-base/-/commit/8dfd12958283d183bb55271a96cafc3bcd2ab98e))
- **generator**: emit store-era initialiser and validation signatures ([b29119a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/b29119aadb21391a6809174d1ec90d621bad3038))
- **root**: the segregated defaults layer always applies ([cc6305d](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6305d79c7f36e6957aa6d8a0007e32a2acb8d9))
- **root**: build one config Store instead of load-then-merge ([dda0f2f](https://gitlab.com/phpboyscout/go-tool-base/-/commit/dda0f2f20043dc942a157202bb592b9307904001))
- **props**: hold a config Store rather than a Containable ([33ef674](https://gitlab.com/phpboyscout/go-tool-base/-/commit/33ef67465f90bd617738ee2cf1bec037c122bf32))
- **vcs**: consume the extracted forge modules ([0478abc](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0478abcd1df4a4ddf8f275bf41b11fe44e95a829))
- **vcs/repo**: consume the extracted go/repo and go/aferobilly modules ([950b151](https://gitlab.com/phpboyscout/go-tool-base/-/commit/950b151fea1f83f410eb3f1bcf075ea8aa1330b4))

### Bug Fixes

- **config**: quiet fresh-tool config noise and let config-free commands run ([3e7bdd4](https://gitlab.com/phpboyscout/go-tool-base/-/commit/3e7bdd411da9e87c5455ef6241cd4ba66921959a))
- **config**: route config writes to the user file, not a missing /etc path ([d983cda](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d983cda33059066c7466805021931868218d025b))
- **ai**: commit the AI provider and its credential in one write ([766a119](https://gitlab.com/phpboyscout/go-tool-base/-/commit/766a1196e4a8f5d4b2e85838a50e7bfbf358a72d))
- **config**: re-harden config file permissions to 0600 on set and migrate ([d1dfef5](https://gitlab.com/phpboyscout/go-tool-base/-/commit/d1dfef52bcb3bae1cb77fd4460290c93a99c6c90))
- **config**: resolve unset and --writable through the store's own routing ([cc6e7b2](https://gitlab.com/phpboyscout/go-tool-base/-/commit/cc6e7b2fb311781bb9fe706c42f92a45043c49a2))
- **config-cmd**: defaults-layer provenance for validate and migrate ([bb5a2e6](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bb5a2e64c07258be347c90532a5511f42a50973b))
- **config-cmd**: interleave migrate's removes and sets per credential ([bd0706e](https://gitlab.com/phpboyscout/go-tool-base/-/commit/bd0706efed2f8c3bde43bc89e189979b2b95bbbf))
- **setup**: WithAuthCheck reads the live store instead of the global viper ([0f3c949](https://gitlab.com/phpboyscout/go-tool-base/-/commit/0f3c9490ced0b7e8e3d4fa2d5d7bf7bfe8ed0175))
- **root**: repair the three phase-2 defects in the config bootstrap ([4ee53c9](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4ee53c9cb3e4d2c3cfa47a17d5f2265f5631082e))
- **root**: read embedded assets through fs.ReadFile, and correct the spec ([a114dec](https://gitlab.com/phpboyscout/go-tool-base/-/commit/a114dec4c3dc33b398226d8930d22245ff7765c4))
- **config**: let an explicit --config suppress the project-local layer ([4df7507](https://gitlab.com/phpboyscout/go-tool-base/-/commit/4df75076fa804e51b8b0019a099665416474021e))
- **deps**: update gomod-weekly ([1fc795a](https://gitlab.com/phpboyscout/go-tool-base/-/commit/1fc795a58cbca0bb43aeab1493ef48c3342a5c72))

```

## [v0.31.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.31.1)

### Bug Fixes

- **generator**: drop global-only platform option from GitLab skeleton renovate config
- guard workflow dedup rule so release tag pipelines fire
- **deps**: update module github.com/google/go-github/v88 to v89
- **controls**: close two pre-extraction lifecycle races
- **deps**: update gomod-weekly

## [v0.31.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.31.0)

### Features

- **generator**: recover signing and template provenance via an annotated file
- **generator**: reconstruct the full manifest from source on a from-scratch rebuild

## [v0.30.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.30.0)

### Features

- **logger**: add slog-first Charm construction and capture handler
- **logger**: add typed Config with slog-ready construction options
- **otelcore**: observe resolved signal settings
- **gateway**: observe composed transport settings
- **grpc**: observe typed server settings
- **http**: observe typed server settings
- **config**: detect observed section changes
- **config**: observe typed config sections
- **config**: add typed section unmarshalling

### Bug Fixes

- **generator**: run go mod tidy during regenerate post-processing
- **generator**: convert logger to *slog.Logger in scaffolded ErrorHandler
- **grpc,http**: stop request logging from exiting on fatal level
- **chat**: suppress spurious fallback override warning
- **agents**: import AGENTS.md into CLAUDE.md instead of linking it
- **ci**: stop osv-scanner failing when it has nothing to report
- **security**: clear the reachable advisories and waive the unreachable one

## [v0.29.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.29.0)

### Features

- **generator**: scaffold and round-trip Tool.Bootstrap policy
- **cmd/root**: honour Tool.Bootstrap policy in the root pre-run
- **props**: add BootstrapPolicy and Tool.Bootstrap field

## [v0.28.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.28.0)

### Features

- **chat**: persist media across snapshots (content-addressed cache)
- **chat**: PDF input for Claude and OpenAI
- **chat**: wire OpenAI media (images)
- **chat**: wire Claude media (images)
- **chat**: extend ChatClient with media input; wire Gemini
- **chat**: media detect + safety-filter core

## [v0.27.2](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.27.2)

### Bug Fixes

- **generator**: converge incremental command rendering with regenerate (keryx follow-ups)
- **generator**: resolve keryx manifest/regen defects (dry-run, const defaults, round-trip)

## [v0.27.1](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.27.1)

### Bug Fixes

- **release**: restore binary assets via syft-enabled build image

## [v0.27.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.27.0)

### Features

- **config**: project-local .<tool>.yaml config layer (repo-root, overrides global)
- **generator**: add stringArray flag type (non-splitting repeatable string)

## [v0.26.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.26.0)

### Features

- **update**: remove deprecated ExportNew* test seams
- **grpc**: remove deprecated ConfigKey* constants

## [v0.25.0](https://gitlab.com/phpboyscout/go-tool-base/-/releases/v0.25.0)

### Features

- **props**: remove .hcl/.tf asset format support

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
