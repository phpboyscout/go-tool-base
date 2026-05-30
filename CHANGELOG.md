# Changelog

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
