---
title: Put the signing key in AWS KMS
description: Create a release-signing key inside AWS KMS with the terraform-aws-signing-kms module, so the private half never exists anywhere you could copy it, plus a signer role and a key policy that keeps managing the key apart from signing with it.
date: 2026-11-02
tags: [tutorial, signing, aws, kms, opentofu]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Put the signing key in AWS KMS

By the end of this you'll have a release-signing key that was created inside
AWS KMS and can never leave it, an alias that will outlive the key, and a
signer role that can use it and do nothing else. Your release pipeline never
holds the private key: it asks KMS to sign on its behalf, one call at a time.

This is the second part of the signing series.
[Sign and verify on your laptop](sign-and-verify-on-your-laptop.md) left the
private key in a `.pem` file, and files get copied. This part fixes that.
Wiring a CI job into the signer role comes in the next part.

Allow about twenty minutes, most of it reading the key policy. This part puts
a line on an AWS bill: about a dollar a month for the key.

## Before you start

- **An AWS account** you can apply infrastructure into, and
  [OpenTofu](https://opentofu.org) (Terraform 1.5 or later works too).
- **An IAM OIDC identity provider for your CI issuer**, already registered in
  that account. KMS doesn't need it, but the signer role trusts it, so it has
  to exist. If you haven't got one,
  [`terraform-aws-bootstrap`](https://gitlab.com/phpboyscout/iac/terraform-aws-bootstrap)
  provisions it, and its `oidc_provider_arn` output is what the module below
  takes.
- **An automation role**: the role your infrastructure pipeline assumes to run
  `tofu apply`. The key policy reserves changes to itself for that role, so
  the apply has to run as it. More on why under the key policy.

You don't need gtb, a pipeline or a public key for this part.

## Declare the key

The key, its alias, the signer role and the key policy all come from one
public module,
[`terraform-aws-signing-kms`](https://gitlab.com/phpboyscout/iac/terraform-aws-signing-kms).
Add it to your stack:

```hcl
data "aws_iam_openid_connect_provider" "gitlab" {
  url = "https://gitlab.com"
}

data "aws_iam_role" "automation" {
  name = "acme-automation"
}

module "signing_kms" {
  source  = "gitlab.com/phpboyscout/signing-kms/aws"
  version = "0.2.3"

  name        = "acme-release-signing-v1"
  description = "Acme release binary signing"

  oidc_provider_arn  = data.aws_iam_openid_connect_provider.gitlab.arn
  ci_subject_filters = ["project_path:acme/acme-cli:ref_type:tag:ref:v*"]

  key_administrator_arns = [
    "arn:aws:iam::111122223333:role/acme-operator",
    "arn:aws:iam::111122223333:root",
  ]
  automation_role_arn = data.aws_iam_role.automation.arn
}

# The next two parts need these.
output "signer_role_arn" {
  value = module.signing_kms.signer_role_arn
}

output "signing_key_alias" {
  value = module.signing_kms.key_alias_name
}
```

Swap in your own account ID, role names and project path. Three of those
values do more than they look:

- **`name` ends in `-v1` on purpose.** It derives the role
  (`acme-release-signing-v1-signer`) and the alias
  (`alias/acme-release-signing-v1`). When you rotate to a new key later in the
  series, you'll add a `-v2` beside it, so put the version in now.
- **`ci_subject_filters` says which pipeline may sign.** This one, in GitLab's
  OIDC `sub` format, allows tag pipelines for any `v*` tag on `acme/acme-cli`,
  and nothing else: no branch builds, no merge requests. The next part explains
  it in full. It isn't optional, because an empty list would trust every token
  from the issuer, and the module refuses that.
- **`key_administrator_arns` and `automation_role_arn`** are the two kinds of
  role that manage the key, and they differ in what they're allowed.

## Apply it as the automation role

Run the apply under the automation role's credentials:

```bash
tofu init
tofu plan
tofu apply
```

Running it as anyone else fails, and that's deliberate. The key policy says
only the automation role may change it, and KMS refuses to create a policy
that would stop its caller changing it again. So a misdirected apply fails
outright rather than locking you out of your own key.

When it finishes, note the two outputs:

```bash
tofu output
```

```text
signer_role_arn   = "arn:aws:iam::111122223333:role/acme-release-signing-v1-signer"
signing_key_alias = "alias/acme-release-signing-v1"
```

## What you just built

One asymmetric KMS key with `key_usage = SIGN_VERIFY` and the spec `RSA_4096`,
the module's default.

RSA rather than Ed25519, and the reason is the signature format, not taste.
KMS does offer Ed25519 signing keys now. But the signatures in this series are
OpenPGP, and the Go OpenPGP library can only drive an Ed25519 key it holds
itself. It can't hand the signing off to a key it can't see, which is the
whole point of KMS. RSA-4096 works with both, so leave it alone. (gtb's
`sign --format minisign` is the one place an Ed25519 KMS key fits, and this
series doesn't use it.)

Two more settings on the key itself:

- **Automatic rotation is off.** AWS's yearly rotation applies to symmetric
  keys. A new signing key means a new public half, and every copy of the old
  one embedded in a released binary would stop matching. Rotating a signing
  key is a deliberate, staged job, and it gets its own part later.
- **The deletion window is 30 days**, the longest AWS allows. For a key like
  this, the longest undo window is the right default.

And the alias, `alias/acme-release-signing-v1`, is how you refer to the key
from now on. Never use the raw key ID. When v2 arrives, the alias is what
keeps everything that calls through it working.

## Read the key policy

A KMS key is governed by its key policy, and this module writes one policy
naming four kinds of principal, each with different reach:

- **The account root** has `kms:*`, AWS's recommended way to make sure a key
  can never be orphaned. Two Deny statements below bind the root as well,
  though.
- **The key administrators** can enable and disable the key, manage its
  aliases and tags, and schedule or cancel its deletion. They can't sign.
- **The automation role** can read the key, tag it and change its policy.
  It can't sign. Your infrastructure pipeline runs on every change, so if
  owning it let someone mint signatures, you'd have given away the point of
  all this. Managing the key and using it are separate powers.
- **The signer role** can call `kms:Sign`, `kms:GetPublicKey` and
  `kms:DescribeKey` on this one key, and that's the entire list.

Then the two Deny statements, which hold for every principal, root included:

- **Only the signer role may call `kms:Sign`.**
- **Only the automation role may call `kms:PutKeyPolicy`.**

That's why the apply has to run as the automation role. If the automation
role is ever deleted, recreate it with the same name and path: the Deny
statements match on its ARN as a string, so the new role is excepted again.
The same goes for the signer role. The module's README covers this under
Break-glass.

One detail catches people out: the signer role has **no IAM policy attached
at all**. Everything it may do is in the key policy. That's the design, not an
oversight. With one document deciding who can do what to the key, there's no
second place for permissions to drift, and anyone asking "who can sign?"
reads one file.

## What it costs

An asymmetric KMS key costs about a dollar a month, plus a small charge per
`kms:Sign` call. Release signing signs a handful of checksum files a month, so
the per-call cost rounds to nothing.

If you're only trying this out, remember that destroying the stack doesn't
delete the key at once. It schedules deletion, and the key keeps existing,
unusable, for the deletion window. Set `deletion_window_in_days = 7` on a
throwaway key if you want it gone sooner.

## Where this leaves you

The key exists, it lives somewhere it can't leave, and there's a role that can
sign with it and a separate role that pointedly can't. What there isn't yet is
any way for your release pipeline to become the signer role without a
long-lived credential sitting in CI. That's the next part.

## What to read next

- [Release-binary signing](../explanation/concepts/release-binary-signing.md):
  the chain of trust this key sits at the root of.
- [`terraform-aws-signing-kms`](https://gitlab.com/phpboyscout/iac/terraform-aws-signing-kms):
  every input and output, and the full key policy.
