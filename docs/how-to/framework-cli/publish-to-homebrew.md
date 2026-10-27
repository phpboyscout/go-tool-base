---
title: Publish a generated tool to a Homebrew tap
description: Name a tap on the project's own GitLab or GitHub, grant the release pipeline its push, and install the tool with brew install --cask.
date: 2026-10-25
tags: [cli, generator, release, homebrew]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Publish a generated tool to a Homebrew tap

Name a tap with `--homebrew-tap <owner>/<name>` when you generate the project,
or later with `gtb set homebrew.tap <owner>/<name>` and `gtb regenerate
project`. Grant the pipeline its push to the tap, cut a release, and the tool
installs with `brew install --cask <tool>`. The repository holds no credential.

The tap is a repository on the project's own forge. GitLab and GitHub projects
can name one. A project that is not hosted, or is hosted elsewhere, cannot.

## 1. Name the tap

At generation:

```bash
gtb generate project --name mytool --repo acme/mytool --forge-backend gitlab \
  --homebrew-tap acme/homebrew
```

The project wizard asks the same question on its Homebrew page. Leave it blank
for no tap.

On an existing project:

```bash
gtb set homebrew.tap acme/homebrew
gtb regenerate project
```

`gtb set` records the tap. `regenerate project` renders the release
configuration that uses it. `gtb unset homebrew.tap` followed by `gtb
regenerate project` removes it all again.

A private project is refused unless it releases on the
[static channel](../../reference/static-release-channel.md), because Homebrew
downloads the release without credentials.

## 2. Grant the push

**GitLab.** The `homebrew-tap` job pushes the cask with the CI job token, so
the tap must allow this project's job token. A Maintainer of the tap adds the
project under the tap's **Settings > CI/CD > Job token permissions**. The tap
must be on the same GitLab instance as the project.

**GitHub.** GoReleaser pushes the cask with `HOMEBREW_TAP_TOKEN`. Create a
token that can write to the tap's contents and nothing else, and add it to the
project as an Actions secret named `HOMEBREW_TAP_TOKEN`. The workflow's own
`GITHUB_TOKEN` cannot write to another repository.

## 3. Notarise the macOS binaries (optional)

A macOS binary that is not notarised can fail to open with a "damaged and
cannot be opened" alert. The generated `.goreleaser.yaml` notarises the macOS
binaries when `APPLE_DEV_CERT` is set, and builds exactly as before when it is
not. Set these as CI/CD variables (GitLab) or Actions secrets (GitHub):

| Variable | Holds |
|---|---|
| `APPLE_DEV_CERT` | the Developer ID certificate: a `.p12`, as a path or base64 |
| `APPLE_DEV_CERT_PASSWORD` | its password |
| `APPLE_NOTARY_ISSUER_ID` | the App Store Connect API issuer ID |
| `APPLE_NOTARY_KEY_ID` | the API key ID |
| `APPLE_NOTARY_KEY` | the API key: a `.p8`, as a path or base64 |

## 4. Release and install

Cut a release as usual. On a plain `vX.Y.Z` tag the cask is pushed to
`Casks/<tool>.rb` in the tap. A prerelease tag leaves the cask where it was.

```bash
brew tap acme/homebrew https://gitlab.com/acme/homebrew
brew install --cask mytool
```

The cask carries Linux download URLs as well as macOS ones. Installing it on
Linux has not been tried yet.

## Moving to another tap

Changing `homebrew.tap` points future releases at the new tap. The cask already
pushed to the old one stays there until you delete it by hand.

## See also

- [`generate project` reference](../../reference/cli/generate.md), the
  `--homebrew-tap` flag.
- [Spec 0212](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0212-a-generated-tool-can-publish-to-a-homebrew-tap),
  the decisions behind this.
