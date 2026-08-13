# Changelog

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
