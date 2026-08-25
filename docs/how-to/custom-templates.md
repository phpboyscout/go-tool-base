---
title: Author and apply custom template overlays
description: Extend the generator with your own files (org SECURITY.md, CODEOWNERS, a bespoke CI pipeline) from a local folder or a git repo, layered over the embedded skeleton and pinned in the manifest.
date: 2026-06-16
tags: [how-to, generator, templates, overlay, ci, security]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Author and apply custom template overlays

The generator renders a fixed embedded skeleton. **Custom template overlays** let you extend it with your own files: an organisation `SECURITY.md`, a house-style `CODEOWNERS`, an `.editorconfig`, a Dockerfile, or a wholesale-replacement CI pipeline, without forking GTB.

A template **source** is a directory tree (a local folder or a git repo). The generator **walks every file** and **renders each one** through the same `text/template` engine to the **identical relative path** in your project:

- A path that does **not** exist in the skeleton **adds** a file.
- A path that **also** exists in the skeleton **overwrites** it: your deviation wins.

Two reserved root meta files are **never rendered**: `README.md` (a human explainer of the set) and `gtb-template.yaml` (the descriptor).

## Apply a source at generate time

```bash
gtb generate project -n mytool -r acme/mytool \
  --template ./house-templates        # local folder
  # repeatable; layered in order:
  # --template acme/gtb-templates@v1.2.0   # forge repo, pinned to a ref
```

`<src>@<ref>`: `<src>` is a local path or a forge repo (`org/repo`, nested GitLab groups, or a full `https://`/`ssh://` URL); `@<ref>` is optional and defaults to the source's default branch. A **remote** source prompts a one-time trust confirmation before it is fetched (skipped under `--ci`).

## Manage sources on an existing project

```bash
gtb template add ./house-templates --name house   # add, pin, regenerate
gtb template list                                 # show sources, refs, resolved commits
gtb template update house                          # re-resolve a git ref to a new commit
gtb template remove house                          # drop the source (restoring any scaffold it replaced)
```

All four resolve to the same manifest representation: hand-editing the `properties.templates:` block and running `gtb regenerate` is an equivalent, supported path.

## Replace a whole embedded CI scaffold

An overlay can only **add or overwrite**, never **delete**. To fully replace an embedded forge-CI scaffold (where your pipeline has fewer/differently-named files than the embedded tree), declare it in a root `gtb-template.yaml`:

```yaml
# gtb-template.yaml at the source root — NOT rendered
contract: 1                  # the data-contract version this set targets
description: "ACME house CI + extra scaffolding"
replaces:
  - gitlab-ci                # suppress the whole embedded .gitlab/ + .gitlab-ci.yml + renovate.json5
  # - github-ci              # suppress the whole embedded .github/
```

`replaces:` suppresses the named embedded scaffold **before** the overlay renders, so your source supplies the complete CI with no stranded embedded files. The two named sets are `gitlab-ci` and `github-ci`. Removing the source later **restores** the embedded scaffold.

## The data contract

Overlay templates render against a **stable, metadata-only, secret-free** subset of the generator's data. Never any token, credential, env var, or absolute path:

`{{ .Name }}` `{{ .Description }}` `{{ .Repo }}` `{{ .Host }}` `{{ .Org }}` `{{ .RepoName }}` `{{ .ModulePath }}` `{{ .ReleaseProvider }}` `{{ .GoVersion }}` `{{ .GoToolBaseVersion }}` `{{ .EnvPrefix }}` `{{ .EnabledFeatures }}` `{{ .DisabledFeatures }}` `{{ .Private }}` `{{ .SigningEnabled }}` `{{ .HelpType }}`

The restricted template FuncMap exposes the escape helpers (`escapeYAML`, `escapeMarkdown`, …) plus pure string helpers (`lower`, `upper`, `join`, `replace`, …). There is **no** file/exec/env/network function. Declare the `contract:` version in your descriptor; GTB rejects an unknown version.

## Reproducibility and offline behaviour

- A **git** source is pinned: `ref` (what you asked for) plus `resolved` (the commit SHA it resolved to). `gtb regenerate` reads the SHA, so output is byte-stable across time even as `main` moves. Sources are cached at `$XDG_CACHE_HOME/gtb/templates/<host>/<owner>/<repo>@<sha>`; a warm cache makes regenerate fully offline. Only `gtb template update` advances the pin.
- A **local** source has no SHA; GTB records a content fingerprint and `gtb regenerate` **warns** if the on-disk source has drifted.
- An offline **cold** cache for a `replaces:`/overwriting git source is a **clear error**: GTB never silently restores the suppressed embedded scaffold.

## Security: custom templates run as trusted input

Custom overlays execute `text/template` content GTB did not author. The posture is **trusted-source with bounded blast radius**, not a sandbox. GTB enforces:

- **Write-path containment** under the project root: a source rendering to `../escape` or an absolute path is refused.
- A **protected-path denylist**: an overlay may never write `.gtb/**`, `internal/trustkeys/**`, or `go.mod`/`go.sum`, even when it otherwise overwrites freely.
- A **restricted FuncMap** and a **metadata-only data contract** (no secrets reachable).
- **Inert fetch** (no hooks, filters, or submodule recursion) and a per-file size bound.

A custom template's **output correctness is the author's responsibility**: GTB guarantees *where* output lands and *what data* a template sees, not the bytes it emits. See [Template Security](../development/template-security.md#custom-template-overlays-a-different-threat-model) for the full threat model.
