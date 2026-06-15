# {{ .Name }}

{{ .Description | escapeMarkdown }}

Built on [gtb](https://gitlab.com/phpboyscout/go-tool-base) — an opinionated,
batteries-included framework for Go CLI tools and services.

## What is this?

<!-- TODO: Replace this block with a description of your product.
     What problem does it solve? Who is it for? What can it do today?
     Everything below this block is generic framework scaffolding you can
     keep, trim, or expand. -->

> **Describe your tool here.** `{{ .Name }}` is a command-line tool built on the
> GTB framework. Tell readers what it does and why they would reach for it. The
> sections below explain how to install, run, and develop on it.

## Prerequisites

- **Go {{ .GoVersion }}** or newer — required to build and install.

The following tools are only needed for **development** (they are not required
to install or run the binary). Each is invoked by a `just` recipe:

- [`just`](https://github.com/casey/just) — the task runner used by this repo.
- [`golangci-lint`](https://golangci-lint.run) — linting (`just lint`).
- [`pre-commit`](https://pre-commit.com) — repo hooks (`just check`).
- [`zensical`](https://gitlab.com/phpboyscout/go-tool-base) — docs site (`just docs-serve`).
- [`goreleaser`](https://goreleaser.com) — local snapshot builds (`just snapshot`).

## Install

Install the latest release with `go install`:

```bash
go install {{ .ModulePath | escapeMarkdownCodeBlock }}/cmd/{{ .Name | escapeMarkdownCodeBlock }}@latest
```

The `main` package lives at `cmd/{{ .Name }}/`, so the install path ends in
`/cmd/{{ .Name }}` — installing the module root would fail because it has no
`main` package.

## Build & run

This repository ships a [`justfile`](justfile) with the common tasks. Build the
binary with the default recipe:

```bash
just            # tidy + generate + build → bin/{{ .Name }}
just build      # the same, explicitly
```

Then run it:

```bash
./bin/{{ .Name | escapeMarkdownCodeBlock }} --help
```

To install the built binary into `$GOPATH/bin`:

```bash
just install
```

## Develop

### Clone & verify

```bash
just test        # unit tests with coverage
just test-race   # tests under the race detector
just lint        # golangci-lint
just check       # pre-commit hooks across the tree
just ci          # the full local CI suite (tidy, generate, test, test-race, lint)
```

Run `just ci` before opening a pull/merge request — it mirrors what CI runs.

### Project layout

| Path | What lives there |
| --- | --- |
| `cmd/` | The `main` entry point (`main.go`). |
| `pkg/cmd/root/` | The root command — wiring, config loading, feature flags. |
| `pkg/cmd/root/assets/init/config.yaml` | The default configuration shipped with the tool. |
| `internal/version/` | Build-time version metadata (injected via ldflags). |
| `docs/` | The documentation site source (served by `zensical`). |
| `.gtb/manifest.yaml` | The generator manifest (see below). |

Add your own commands under `pkg/cmd/<command>/` and your reusable packages
under `pkg/`.

### The manifest & regeneration model

`{{ .Name }}` is scaffolded and kept in sync by GTB's generator. The
`.gtb/manifest.yaml` file is the source of truth for the command tree and
project settings. Two flows operate on it:

- **`gtb generate command <name>`** — scaffold a new command, recording it in
  the manifest.
- **`gtb regenerate`** — re-render the project from the manifest and the current
  GTB templates.

Generated files are **hash-tracked** in the manifest. If you hand-edit a
generated file, the next `regenerate` detects the change as a conflict and
prompts before overwriting it — so your edits are never silently lost. This
README is one of those files: it is yours to edit, and regeneration will ask
before replacing it.

> **Do not hand-edit generated files expecting silent re-generation.** Put
> custom logic in your own packages, or accept the conflict prompt on the next
> regenerate.

### Configuration

Configuration is layered (highest precedence first): command-line flags →
environment variables → config file → embedded defaults. The shipped defaults
live in `pkg/cmd/root/assets/init/config.yaml`.

Any setting can be overridden from the environment using the **`{{ .EnvPrefix }}`**
prefix. For example, a `log.level` setting is overridden by
`{{ .EnvPrefix }}_LOG_LEVEL`.

### Enabled built-ins

GTB provides built-in commands. The following are always available in this
project: self-update, first-run init, MCP server, embedded documentation,
doctor checks, and changelog.

{{ if .EnabledFeatures -}}
The following opt-in built-ins are also enabled:
{{ range .EnabledFeatures }}
{{- if eq . "ai" }}
- **AI chat** — interactive AI assistance.
{{- else if eq . "config" }}
- **Config management** — inspect and edit configuration.
{{- else if eq . "telemetry" }}
- **Telemetry** — usage and diagnostics reporting.
{{- else }}
- **{{ . }}**
{{- end }}
{{- end }}
{{ else -}}
No opt-in built-ins (AI chat, config management, telemetry) are enabled. Enable
them by regenerating with the relevant feature flag.
{{ end }}
Run `{{ .Name }} --help` to see the full command list.

## Documentation

The documentation site source lives in `docs/` and is built with `zensical`.
Serve it locally with:

```bash
just docs-serve
```

Then open the printed local URL in your browser.

## Releasing

Releases are driven by [Conventional Commits](https://www.conventionalcommits.org/)
and run through the scaffolded CI pipeline ({{ if eq .ReleaseProvider "github" }}GitHub
Actions under `.github/`{{ else }}GitLab CI under `.gitlab-ci.yml`{{ end }}) together
with [GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`). Merging a
release cuts the tag and publishes the build artefacts.

See the GTB release guide for the full workflow:
{{ if eq .ReleaseProvider "github" -}}
<https://phpboyscout.gitlab.io/go-tool-base/how-to/secure-releases/>
{{- else -}}
<https://phpboyscout.gitlab.io/go-tool-base/how-to/custom-release-source/>
{{- end }}

## Contributing

- Follow [Conventional Commits](https://www.conventionalcommits.org/) for every
  commit — the changelog and version bumps are computed from them.
- Run `just ci` before opening a pull/merge request.
- Do not hand-edit generated files (those tracked in `.gtb/manifest.yaml`); the
  next `gtb regenerate` will flag them as conflicts.

## Go deeper

GTB documentation:

- Framework docs — <https://phpboyscout.gitlab.io/go-tool-base/>
- Generating commands — <https://phpboyscout.gitlab.io/go-tool-base/cli/command/>
- Regeneration & the manifest — <https://phpboyscout.gitlab.io/go-tool-base/concepts/regeneration/>
- Configuration — <https://phpboyscout.gitlab.io/go-tool-base/concepts/config/>
- Testing — <https://phpboyscout.gitlab.io/go-tool-base/how-to/testing/>
- Repository (source & issues) — <https://gitlab.com/phpboyscout/go-tool-base>
