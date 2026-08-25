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
# invocation. See https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0070-sign-command.
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

# Dual-sign overlap window (2026-07-24 key rotation, infra spec
# 2026-07-24-prod-rebuild-and-rekey D2a): when the secondary key's env
# is present, a SECOND signature is merged into the SAME armored file
# via `gtb sign --append`. In-field binaries embed only the old key and
# skip signature packets from issuers they don't know, so the one .sig
# verifies for both binary generations. The secondary signer role lives
# in a different AWS account; AWS_ROLE_ARN is overridden per-invocation
# (the web-identity token file works for any role trusting the same
# OIDC sub).
#
# Remove GTB_SIGNING_KEY_ID_2 / _PUBLIC_2 / AWS_ROLE_ARN_2 from
# .gitlab-ci.yml to close the window (then this block is inert).
if [[ -n "${GTB_SIGNING_KEY_ID_2:-}" ]]; then
	public_key_2="${GTB_SIGNING_KEY_PUBLIC_2:?GTB_SIGNING_KEY_ID_2 set but GTB_SIGNING_KEY_PUBLIC_2 missing}"

	if [[ ! -f "${public_key_2}" ]]; then
		echo "sign-release.sh: GTB_SIGNING_KEY_PUBLIC_2 (${public_key_2}) not found" >&2
		exit 1
	fi

	AWS_ROLE_ARN="${AWS_ROLE_ARN_2:-${AWS_ROLE_ARN:-}}" \
	go run ./cmd/gtb --ci sign \
		--backend aws-kms \
		--kms-region "${region}" \
		--key-id "${GTB_SIGNING_KEY_ID_2}" \
		--public-key "${public_key_2}" \
		--output "${signature}" \
		--append \
		"${artifact}"

	echo "sign-release.sh: dual-signed ${signature} (primary ${key_id} + secondary ${GTB_SIGNING_KEY_ID_2})"
else
	echo "sign-release.sh: wrote ${signature}"
fi
