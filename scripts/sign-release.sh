#!/usr/bin/env bash
#
# sign-release.sh — produce an ASCII-armored detached OpenPGP signature
# over a release artifact (in practice, checksums.txt -> checksums.txt.sig).
#
# Invoked by GoReleaser's `signs` block. The output is the same
# armored detached signature the verifier in pkg/setup
# (TrustSet.VerifyManifestSignature, via
# openpgp.CheckArmoredDetachedSignature) expects.
#
# The signing call goes through `sigillum sign --backend aws-kms`, which
# uses the AWS SDK's default credential chain. CI hands the signer the
# OIDC-derived AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY /
# AWS_SESSION_TOKEN via a before_script that runs
# `aws sts assume-role-with-web-identity` — see .gitlab-ci.yml.
#
# Usage (as invoked by GoReleaser):
#   scripts/sign-release.sh <artifact> <signature-output>
#
# Required env:
#   GTB_SIGNING_KEY_ID         KMS key id / ARN / alias to sign with.
#                              Default: alias/gtb-release-signing-v2.
#   GTB_SIGNING_KEY_PUBLIC     Path to the armored public key file
#                              (the .asc embedded in
#                              cli/pkg/trustkeys/keys/). Identity
#                              source for the signature.
#                              Default: cli/pkg/trustkeys/keys/signing-key-v2.asc.
#   AWS_REGION                 KMS region. Default: eu-west-2.
#
# The credential check below IS the gate, not a safety net behind one.
#
# This comment used to say the pipeline skipped signing when no OIDC
# token was present. It does not: there is no `--skip=sign` branch in
# .gitlab-ci.yml, so a tag pipeline that cannot reach KMS fails here
# rather than publishing an unsigned release. That is the behaviour we
# want, and it is worth stating plainly, because a "safety net" reads as
# something you may remove.

set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <artifact> <signature-output>" >&2
	exit 2
fi

artifact="$1"
signature="$2"

key_id="${GTB_SIGNING_KEY_ID:-alias/gtb-release-signing-v2}"
public_key="${GTB_SIGNING_KEY_PUBLIC:-cli/pkg/trustkeys/keys/signing-key-v2.asc}"
region="${AWS_REGION:-eu-west-2}"

if [[ ! -f "${public_key}" ]]; then
	echo "sign-release.sh: GTB_SIGNING_KEY_PUBLIC (${public_key}) not found" >&2
	echo "  The signing-key public half must be present in the working tree" >&2
	echo "  (embedded at cli/pkg/trustkeys/keys/signing-key-v2.asc by default)." >&2
	exit 1
fi

if [[ -z "${AWS_ACCESS_KEY_ID:-}" && -z "${AWS_WEB_IDENTITY_TOKEN_FILE:-}" ]]; then
	echo "sign-release.sh: no AWS credentials are configured; refusing to sign." >&2
	echo "  Either:" >&2
	echo "    - AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY (+ AWS_SESSION_TOKEN for assumed roles), or" >&2
	echo "    - AWS_ROLE_ARN + AWS_WEB_IDENTITY_TOKEN_FILE (OIDC web-identity, used by CI)" >&2
	echo "  must be set so the AWS SDK Go v2 default credential chain can resolve credentials." >&2
	exit 1
fi

if ! command -v sigillum >/dev/null 2>&1; then
	echo "sign-release.sh: sigillum is not on PATH; refusing to sign." >&2
	echo "  CI gets it from the go-tools image. Locally, install it from" >&2
	echo "  https://gitlab.com/phpboyscout/sigillum (sigillum spec 0007 D6)." >&2
	exit 1
fi

# sigillum produces an armored OpenPGP detached signature with the aws-kms
# backend. The private key never leaves KMS: the public key is fetched once,
# and signing is one kms:Sign call. gtb's own signing commands moved to
# sigillum (sigillum spec 0007 D6), and the CI image carries a released,
# verified sigillum, so its version follows the image tag.
sigillum --ci sign \
	--backend aws-kms \
	--kms-region "${region}" \
	--key-id "${key_id}" \
	--public-key "${public_key}" \
	--output "${signature}" \
	"${artifact}"

echo "sign-release.sh: wrote ${signature}"
