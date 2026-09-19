#!/usr/bin/env bash
#
# move-pointer.sh — move the static release channel's pointer (spec 0203 D3).
#
# Invoked by GoReleaser's `publishers` block once, after the `blobs` pipe has
# uploaded the tag's archives, checksums.txt, its signature and release.json.
# It PUTs dist/latest.json to the channel's base as a conditional write: the
# ETag the pointer has now (If-Match), or none yet (If-None-Match: *). Two
# publishes racing cannot silently clobber each other: the loser gets a 412
# and this script fails with both ETags in the log, and a person looks.
#
# Usage (as invoked by GoReleaser, which resolves the templated URL):
#   scripts/move-pointer.sh <pointer file> <object URL on the store's S3 endpoint>
#
# Required env:
#   AWS_ACCESS_KEY_ID       The store's credentials, the same ones the blobs
#   AWS_SECRET_ACCESS_KEY   pipe uploads with.
#
# curl signs SigV4 itself (>= 7.75), the way the estate's hand-rolled
# uploaders do (colophon 0025 D19). No SDK, no CLI.

set -euo pipefail

pointer=$1
url=$2

for v in AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY; do
  if [ -z "${!v:-}" ]; then
    echo "move-pointer: $v is not set; the pointer stays where it is" >&2
    exit 1
  fi
done

sign=(--aws-sigv4 "aws:amz:auto:s3" --user "${AWS_ACCESS_KEY_ID}:${AWS_SECRET_ACCESS_KEY}")

# What the pointer is now: its ETag, or nothing when this is the first
# release. A signed GET rather than HEAD, with the empty payload's hash
# given outright: curl before 8.0 (Debian bookworm ships 7.88) signs a
# bodiless S3 request inconsistently, and a HEAD hides the store's answer
# when it refuses (v0.45.1 met a bare 400 that way).
head_out=$(mktemp)
body=$(mktemp)
trap 'rm -f "$head_out" "$body"' EXIT

empty_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

status=$(curl --silent --show-error --request GET "${sign[@]}" \
  --header "x-amz-content-sha256: ${empty_sha256}" \
  --dump-header "$head_out" --output "$body" --write-out '%{http_code}' "$url")

case "$status" in
  200)
    etag=$(tr -d '\r' < "$head_out" | awk 'tolower($1) == "etag:" { print $2 }')
    if [ -z "$etag" ]; then
      echo "move-pointer: $url answered 200 without an ETag; refusing an unconditional write" >&2
      exit 1
    fi
    condition="If-Match: ${etag}"
    ;;
  404)
    condition="If-None-Match: *"
    ;;
  *)
    echo "move-pointer: GET $url returned HTTP $status ($(curl --version | head -1))" >&2
    tr -d '\r' < "$head_out" >&2
    cat "$body" >&2
    echo >&2
    exit 1
    ;;
esac

# The pointer is the channel's one mutable object, so it must not be cached
# the way the immutable objects under a tag are.
status=$(curl --silent --show-error --retry 3 --retry-delay 5 "${sign[@]}" \
  --request PUT \
  --header "$condition" \
  --header "Content-Type: application/json" \
  --header "Cache-Control: no-cache, max-age=0" \
  --upload-file "$pointer" \
  --output "$body" --write-out '%{http_code}' "$url")

case "$status" in
  200)
    echo "move-pointer: $url now names $(sed -n 's/.*"tag": *"\([^"]*\)".*/\1/p' "$pointer") (${condition})"
    ;;
  412)
    echo "move-pointer: $url changed under this publish (${condition} failed with 412); another publish moved it, look before re-running" >&2
    exit 1
    ;;
  *)
    echo "move-pointer: PUT $url returned HTTP $status ($(curl --version | head -1))" >&2
    cat "$body" >&2
    echo >&2
    exit 1
    ;;
esac
