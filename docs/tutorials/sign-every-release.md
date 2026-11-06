---
title: Sign every release with GoReleaser
description: Wire KMS signing into a gtb-generated tool's GoReleaser config with one command, so every tagged release ships a checksums.txt.sig made over short-lived OIDC credentials.
date: 2026-11-02
tags: [tutorial, signing, goreleaser, aws, kms, release]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Sign every release with GoReleaser

By the end of this, pushing a `v*` tag produces a release whose
`checksums.txt` is signed through your KMS key, with credentials that exist
only for the length of the job. Nobody runs a signing command by hand.

This is the sixth part of the signing series. The earlier parts gave you a KMS
key, a signer role your CI can assume, a published public key, and a tool that
checks signatures. Nothing produces a signature yet. This part does.

Allow about twenty minutes, plus one release to try it.

## Before you start

- **A gtb-generated tool that releases with GoReleaser** (every generated
  project does), with signing enabled as in
  [Require signed updates](require-signed-updates.md).
- **The KMS key alias and signer role ARN** from
  [Put the signing key in AWS KMS](put-the-signing-key-in-kms.md).
- **The OIDC trust** from [Sign from CI without stored keys](sign-from-ci-without-stored-keys.md).
- **A gtb newer than v0.50.3.** Earlier releases added the `signs:` block but left
  the CI side for you to wire by hand.

## Wire signing into the release config

In the previous part, `gtb enable signing` set up the verifying side. Give the
same command the key the pipeline should sign with, and it wires the signing
side too:

```bash
gtb enable signing --key-id alias/acme-release-signing-v1
```

```text
INFO  Injected the release-signing signs: block into the release config. path=.goreleaser.yaml
INFO  Set the SIGNER_ROLE_ARN CI variable to the KMS signer role, or the release job can't sign. path=.gitlab-ci.yml
INFO  Signing enabled.
```

That records the key in `.gtb/manifest.yaml`, beside the email and enforcement
settings you already had:

```yaml
signing:
  enabled: true
  external_key_email: release@acme.dev
  backend: aws-kms
  key_id: alias/acme-release-signing-v1
  kms_region: eu-west-2
  public_key: internal/trustkeys/keys/signing-key-v1.asc
```

And it adds a `signs:` block to `.goreleaser.yaml` that calls `sigillum sign`:

```yaml
signs:
  - id: checksums
    cmd: sigillum
    args:
      - "--ci"
      - "sign"
      - "--backend"
      - "aws-kms"
      - "--kms-region"
      - "eu-west-2"
      - "--key-id"
      - "alias/acme-release-signing-v1"
      - "--public-key"
      - "internal/trustkeys/keys/signing-key-v1.asc"
      - "--output"
      - "${signature}"
      - "${artifact}"
    artifacts: checksum
    signature: "${artifact}.sig"
    output: true
```

The region defaults to `eu-west-2` and the public key to the one you embedded.
Pass `--kms-region` or `--public-key` if yours differ. Change any of it later
by re-running the command, not by editing the YAML.

## Why only the checksums are signed

`artifacts: checksum` tells GoReleaser to sign once, over the checksums file,
not over every archive. That's enough. `checksums.txt` lists the SHA-256 of
every archive in the release, so change one byte of a binary and its hash no
longer matches. Change `checksums.txt` to cover for it, and the signature
breaks. One signature covers the whole release, and the builds themselves stay
untouched.

`signature: "${artifact}.sig"` names the output `checksums.txt.sig`, which is
the file your tool fetches on every self-update.

## What it does to your CI

The same command updates the file that runs GoReleaser: `.gitlab-ci.yml` on
GitLab, `.github/workflows/goreleaser.yaml` on GitHub. If you've edited that
file, or listed it in `.gtb/ignore`, gtb leaves it alone and tells you, and the
sections below are what to bring across by hand.

### sigillum on the release runner

The signs block calls [sigillum](https://gitlab.com/phpboyscout/sigillum), a
standalone signer with the same `sign` command as gtb, so the job needs no gtb.

On GitLab there's nothing to install, because the goreleaser component's image
carries sigillum. If you mirror the components with `ci.component_source`, or
override the component's `image` input, your image has to carry it too. The image
is what sets the version here.

On GitHub a tag build installs it, at a version gtb pins:

```bash
go install gitlab.com/phpboyscout/sigillum/cmd/sigillum@vX.Y.Z
```

It builds from the module rather than downloading an archive, because this job
holds your signing role. The Go checksum database vouches for the module, so
write access to a release store can't swap the code that signs your release.
Running `gtb regenerate project` moves the pin along with your gtb.

### Credentials for the job

`sigillum sign` asks KMS to sign, and KMS only obliges a caller with credentials.
They come from the OIDC exchange you set up earlier, on tag builds only. You
supply one thing: a CI variable called `SIGNER_ROLE_ARN`, holding the role ARN.

On GitLab, set it under **Settings > CI/CD > Variables**. The generated overlay
on the `goreleaser` job reads it:

```yaml
goreleaser:
  id_tokens:
    AWS_WEB_IDENTITY_TOKEN:
      aud: sts.amazonaws.com
  variables:
    AWS_REGION: "eu-west-2"
    AWS_ROLE_ARN: ${SIGNER_ROLE_ARN}
    AWS_WEB_IDENTITY_TOKEN_FILE: /tmp/oidc-token
```

The AWS SDK inside `gtb` exchanges the token for the signer role the first
time it touches KMS, so there's no `aws` CLI step. The `signs:` block also
blanks `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` for the sign command,
because the goreleaser component hands the job the release store's keys, and
static keys outrank a web-identity token.

On GitHub, set it under **Settings > Secrets and variables > Actions >
Variables**. The generated workflow reads it in a step that only runs for tags:

```yaml
      - name: Assume the signer role
        if: ${{ startsWith(github.ref, 'refs/tags/v') }}
        uses: aws-actions/configure-aws-credentials@v6
        with:
          role-to-assume: ${{ vars.SIGNER_ROLE_ARN }}
          aws-region: "eu-west-2"
          audience: sts.amazonaws.com
```

The signer role's trust policy only accepts tag pipelines on your project, so
even a leaked role ARN gets nobody else a signature.

### Builds that aren't releases

A pull request build has no OIDC token and shouldn't reach for KMS. GoReleaser
still runs the signs block in snapshot mode, so the generated GitHub workflow
adds `--skip=sign` to every build that isn't a tag:

```yaml
      - if: ${{ !startsWith(github.ref, 'refs/tags/v') }}
        run: echo "flags=--snapshot --skip=sign" >> $GITHUB_ENV
```

On GitLab the GoReleaser job only runs on tags, so merge requests never get
there. Locally, pass it yourself:

```bash
goreleaser release --snapshot --skip=sign --clean
```

Without it, a build with no credentials stops at the signing step:

```text
  • signing artifacts
      │ ERROR constructing signer: kms GetPublicKey: ... get credentials: failed to refresh cached credentials ...
  ⨯ release failed after 10s
```

## Cut a release

With that in place, a release is a tag:

```bash
git tag v1.4.0
git push origin v1.4.0
```

If your project releases through colophon, as generated projects do, merging
the release merge request pushes the tag for you.

The tag pipeline builds the binaries, writes `checksums.txt`, and calls
`sigillum sign`, which signs through KMS over the job's OIDC credentials.
`checksums.txt.sig` is attached to the release beside the checksums. No
private key was anywhere near the runner.

To check one by hand, download both files and verify with `gpg`, as you did in
[Sign and verify on your laptop](sign-and-verify-on-your-laptop.md).

## Add an approval gate, if you want one

A compromised runner during a release could, in principle, use the job's
credentials to get one malicious thing signed. An approval in front of the
release job closes that window. On GitLab, use a protected environment with a
required approval, or a manual job. On GitHub, add required reviewers to the
release environment. It adds a step to every release, so weigh it against how
exposed your runners are. For a release where a forgery would be a real
incident, it's cheap insurance.

## Where this leaves you

Every release you cut now carries a signature made by a key you control,
checkable by anyone. Your tool verifies it, and now that signed releases are
shipping, the [enforcement step](require-signed-updates.md#require-signatures-in-the-right-order)
can follow once one has gone out.

The last part covers what everyone skips: rotating the key, and recovering if
one ever goes bad, without locking users out.

## What to read next

- [Sign release artefacts](../how-to/sign-releases.md): every `sign` flag.
- [Release-binary signing](../explanation/concepts/release-binary-signing.md):
  how the signature, the trust anchors and the update fit together.
