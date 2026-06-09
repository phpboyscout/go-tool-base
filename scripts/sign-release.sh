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
# The signing call goes through `gtb sign --backend aws-kms`, which
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
#                              Default: alias/gtb-release-signing-v1.
#   GTB_SIGNING_KEY_PUBLIC     Path to the armored public key file
#                              (the .asc embedded in
#                              internal/trustkeys/keys/). Identity
#                              source for the signature.
#                              Default: internal/trustkeys/keys/signing-key-v1.asc.
#   AWS_REGION                 KMS region. Default: eu-west-2.
#
# The release pipeline gates invocation on AWS_WEB_IDENTITY_TOKEN
# being set (i.e. CI is in an OIDC-capable context); goreleaser runs
# with `--skip=sign` when it isn't, so this script erroring on a
# missing token is a safety net, not the gate.

set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <artifact> <signature-output>" >&2
	exit 2
fi

artifact="$1"
signature="$2"

key_id="${GTB_SIGNING_KEY_ID:-alias/gtb-release-signing-v1}"
public_key="${GTB_SIGNING_KEY_PUBLIC:-internal/trustkeys/keys/signing-key-v1.asc}"
region="${AWS_REGION:-eu-west-2}"

if [[ ! -f "${public_key}" ]]; then
	echo "sign-release.sh: GTB_SIGNING_KEY_PUBLIC (${public_key}) not found" >&2
	echo "  The signing-key public half must be present in the working tree" >&2
	echo "  (embedded at internal/trustkeys/keys/signing-key-v1.asc by default)." >&2
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

# gtb sign produces an armored OpenPGP detached signature using the
# pkg/signing aws-kms backend. The private key never leaves KMS —
# only the public key's RSA bytes are downloaded once via GetPublicKey,
# and the signing operation is a single kms:Sign round-trip per
# invocation. See docs/development/specs/2026-06-09-sign-command.md.
#
# `go run` rather than an installed binary so this script works
# unmodified inside the goreleaser CI image (which ships Go but not
# gtb). Local invocations can also `go run` from the repo root.
go run ./cmd/gtb --ci sign \
	--backend aws-kms \
	--kms-region "${region}" \
	--key-id "${key_id}" \
	--public-key "${public_key}" \
	--output "${signature}" \
	"${artifact}"

echo "sign-release.sh: wrote ${signature}"
