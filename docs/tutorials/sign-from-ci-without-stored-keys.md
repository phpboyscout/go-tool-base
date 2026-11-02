---
title: Sign from CI without stored keys
description: Let a GitLab or GitHub release job assume the KMS signer role over OIDC, so no AWS access key is stored anywhere, and avoid the token-audience mismatch that looks like a trust-policy bug.
date: 2026-11-02
tags: [tutorial, signing, aws, oidc, gitlab-ci, github-actions]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Sign from CI without stored keys

By the end of this, a release tag pipeline on your project can become the KMS
signer role for the length of one job, and no AWS access key is stored in
your CI settings or anywhere else. Branch and merge request pipelines can't.

This is the third part of the signing series.
[Put the signing key in AWS KMS](put-the-signing-key-in-kms.md) left you with
a signer role that may call `kms:Sign` and nothing else. This part answers how
a CI job becomes that role.

Allow about twenty minutes. Both GitLab and GitHub are covered: follow the
section for your forge.

## How the job proves who it is

The mechanism is OIDC federation. When a job runs, GitLab or GitHub can issue
it a short-lived signed token (a JWT) that says, for example, "this is a tag
pipeline on `acme/acme-cli`". AWS checks who issued the token and what it
claims, and if the claims match the role's trust policy it swaps the token for
temporary credentials that expire with the job. Nothing is stored, so there's
nothing to leak.

Two things have to line up:

- **The IAM OIDC identity provider**, which tells your AWS account to trust
  tokens from one issuer. One provider serves every role in the account that
  federates from that forge, so it doesn't live in the signing module.
  [`terraform-aws-bootstrap`](https://gitlab.com/phpboyscout/iac/terraform-aws-bootstrap)
  registers it and outputs its ARN.
- **The signer role's trust policy**, which narrows that to particular
  pipelines. The signing module writes it from `ci_subject_filters`.

## Before you start

- **The KMS key and signer role** from
  [Put the signing key in AWS KMS](put-the-signing-key-in-kms.md), and its
  `signer_role_arn` output.
- **An IAM OIDC provider for your forge** in the same account:
  `https://gitlab.com` or `https://token.actions.githubusercontent.com`.

## Trust GitLab tag pipelines

Look the provider up by URL so the ARN is never hard-coded, and scope the role
with a subject filter:

```hcl
data "aws_iam_openid_connect_provider" "gitlab" {
  url = "https://gitlab.com"
}

module "signing_kms" {
  source  = "gitlab.com/phpboyscout/signing-kms/aws"
  version = "0.2.3"

  name              = "acme-release-signing-v1"
  oidc_provider_arn = data.aws_iam_openid_connect_provider.gitlab.arn

  # oidc_issuer_host defaults to "gitlab.com"
  # oidc_audience    defaults to "sts.amazonaws.com"

  ci_subject_filters = [
    "project_path:acme/acme-cli:ref_type:tag:ref:v*",
  ]

  key_administrator_arns = [ /* as before */ ]
  automation_role_arn    = data.aws_iam_role.automation.arn
}
```

GitLab puts the project path, the ref type and the ref in each token's `sub`
claim. This filter reads "tag pipelines on `acme/acme-cli`, for any tag
starting `v`". A branch or merge request pipeline carries `ref_type:branch`,
so it doesn't match and the role refuses it. That's what stops someone opening
a merge request from getting CI to mint a signature.

Apply it as the automation role, as before.

On the pipeline side, the release job asks GitLab for a token with the right
audience, writes it to a file, and points the AWS SDK at the file and the
role:

```yaml
release:
  rules:
    - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/'
  id_tokens:
    AWS_WEB_IDENTITY_TOKEN:
      aud: sts.amazonaws.com
  variables:
    AWS_REGION: eu-west-2
    AWS_ROLE_ARN: ${SIGNER_ROLE_ARN}
    AWS_WEB_IDENTITY_TOKEN_FILE: /tmp/aws-token
  before_script:
    - echo "$AWS_WEB_IDENTITY_TOKEN" > "$AWS_WEB_IDENTITY_TOKEN_FILE"
  script:
    - aws sts get-caller-identity
```

Set `SIGNER_ROLE_ARN` as a CI/CD variable holding the `signer_role_arn`
output. It isn't a secret: the trust policy, not the ARN, decides who can
assume the role.

`AWS_WEB_IDENTITY_TOKEN_FILE` goes in `variables:`, not an `export` in
`before_script`, because GitLab runs each script line in its own shell and an
export doesn't survive to the next one.

The AWS SDK sees `AWS_ROLE_ARN` and `AWS_WEB_IDENTITY_TOKEN_FILE` together and
assumes the role itself. `gtb sign --backend aws-kms` uses that SDK, so it
needs no extra step either. Push a `v` tag and the job prints the assumed
signer role:

```text
{
    "UserId": "AROA...:botocore-session-...",
    "Account": "111122223333",
    "Arn": "arn:aws:sts::111122223333:assumed-role/acme-release-signing-v1-signer/botocore-session-..."
}
```

The `aws` CLI line is only a check that federation works. Signing itself
comes later in the series, in the GoReleaser part.

## Trust GitHub tag workflows

Same shape, with a different issuer and GitHub's own `sub` format:

```hcl
data "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"
}

module "signing_kms" {
  source  = "gitlab.com/phpboyscout/signing-kms/aws"
  version = "0.2.3"

  name              = "acme-release-signing-v1"
  oidc_provider_arn = data.aws_iam_openid_connect_provider.github.arn
  oidc_issuer_host  = "token.actions.githubusercontent.com"

  # oidc_audience still defaults to "sts.amazonaws.com"

  ci_subject_filters = [
    "repo:acme/acme-cli:ref:refs/tags/v*",
  ]

  key_administrator_arns = [ /* as before */ ]
  automation_role_arn    = data.aws_iam_role.automation.arn
}
```

Two things change from GitLab:

- **`oidc_issuer_host`** is the prefix on the trust policy's condition keys,
  so it becomes `token.actions.githubusercontent.com`.
- **The subject** is `repo:<owner>/<repo>:ref:refs/tags/v*`, which scopes the
  role to tag refs the same way.

The workflow uses AWS's official action. It needs `id-token: write` to ask
GitHub for a token at all, and `contents: write` so the release step can
create the release:

```yaml
on:
  push:
    tags: ["v*"]

permissions:
  id-token: write
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: aws-actions/configure-aws-credentials@v6
        with:
          role-to-assume: ${{ vars.SIGNER_ROLE_ARN }}
          aws-region: eu-west-2
          audience: sts.amazonaws.com
      - run: aws sts get-caller-identity
```

Leave out `id-token: write` and GitHub never issues a token, so the action
fails before it reaches AWS. It's the most common GitHub-side mistake.

## Keep the token audience aligned

One failure is worth knowing in advance, because its message points at the
wrong thing. An IAM OIDC provider carries a `client_id_list`, and it rejects
any token whose `aud` claim isn't on it, before AWS looks at the role's trust
policy at all:

```text
InvalidIdentityToken: Incorrect token audience
```

That reads like a trust-policy problem, and editing `ci_subject_filters` won't
fix it, because the trust policy was never consulted. Keep the audience at
`sts.amazonaws.com` in all four places:

- the module's `oidc_audience` default,
- the provider's `client_id_list`, which `terraform-aws-bootstrap` sets,
- the `aud` in GitLab's `id_tokens` block,
- the `audience` passed to `configure-aws-credentials`.

Left alone, they agree. If you override one, override all four.

## Where this leaves you

There's no AWS access key anywhere in either forge. A release tag proves what
it is, AWS hands it the signer role for one job, and the credentials expire
with the runner.

The role can sign, but nobody can verify anything yet, because nobody has the
public half of the KMS key. That's the next part.

## What to read next

- [Release-binary signing](../explanation/concepts/release-binary-signing.md):
  where the signer role sits in the chain of trust.
- [`terraform-aws-bootstrap`](https://gitlab.com/phpboyscout/iac/terraform-aws-bootstrap):
  the OIDC provider and its `client_id_list`.
