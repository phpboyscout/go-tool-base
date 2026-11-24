#!/usr/bin/env bash
#
# publish-repos.sh: publish the apt and yum indexes `go tool pkgrepo` wrote
# (spec 0097 D5), and the release key they are signed with (D9).
#
# Usage:
#   scripts/publish-repos.sh <pkgrepo dir> <key file> <base URL on the store's S3 endpoint>
#
# Required env:
#   AWS_ACCESS_KEY_ID       The store's credentials.
#   AWS_SECRET_ACCESS_KEY
#
# Every index is read before anything is written. One that is neither what is
# about to be written nor what the indexes were built from (<dir>/base) is
# another publish's work, and stops this one with nothing written. Each write
# is conditional on what was read. Run it again after a failure, unless the
# failure says to run package-index again first.
#
# curl signs SigV4 itself. It is told each payload's hash, because curl 7.88
# sends none and the store refuses the request (scripts/move-pointer.sh).

set -euo pipefail

if [ $# -ne 3 ]; then
  echo "usage: publish-repos.sh <pkgrepo dir> <key file> <base URL>" >&2
  exit 2
fi

dir=$1
key=$2
base_url=${3%/}

fail() {
  echo "publish-repos: $*" >&2
  exit 1
}

for v in AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY; do
  [ -n "${!v:-}" ] || fail "$v is not set; nothing is published"
done

# The order is D5's. A reader in the middle of it fails to verify, and a
# signature is always written after what it signs.
indexes=(
  rpm/repodata/repomd.xml
  rpm/repodata/repomd.xml.asc
  apt/Packages
  apt/Packages.gz
  apt/Release
  apt/Release.gpg
)

data=()
for kind in primary filelists other; do
  found=("$dir"/rpm/repodata/*-"$kind".xml.gz)
  [ ${#found[@]} -eq 1 ] && [ -s "${found[0]}" ] || fail "$dir does not hold one $kind.xml.gz; nothing is published"
  data+=("rpm/repodata/${found[0]##*/}")
done

for name in "${indexes[@]}"; do
  [ -s "$dir/$name" ] || fail "$dir/$name is missing or empty; nothing is published"
done

[ -s "$key" ] || fail "$key is missing or empty; nothing is published"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

request=(--silent --show-error --retry 3 --retry-delay 5 --connect-timeout 20 --max-time 300
  --aws-sigv4 "aws:amz:auto:s3" --user "${AWS_ACCESS_KEY_ID}:${AWS_SECRET_ACCESS_KEY}")
empty_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

# read_object <name> leaves its bytes in $work/body and its ETag in $etag, or
# returns 1 when the store does not have it.
read_object() {
  local status
  : > "$work/body"
  # This runs inside an if, where set -e does nothing, and a read cut short
  # still has its 200.
  status=$(curl "${request[@]}" --request GET \
    --header "x-amz-content-sha256: ${empty_sha256}" \
    --dump-header "$work/head" --output "$work/body" --write-out '%{http_code}' "$base_url/$1") ||
    fail "GET $1 did not finish; run this again"

  case "$status" in
    200)
      etag=$(tr -d '\r' < "$work/head" | awk 'tolower($1) == "etag:" { last = $2 } END { print last }')
      [ -n "$etag" ] || fail "$1 was read without an ETag; refusing an unconditional write"
      ;;
    404) return 1 ;;
    *) fail "GET $1 returned HTTP $status: $(cat "$work/body")" ;;
  esac
}

# put <name> <file> <condition> <content type> <cache control> <what a conflict means>
put() {
  local status
  : > "$work/answer"
  status=$(curl "${request[@]}" --request PUT \
    --header "$3" \
    --header "x-amz-content-sha256: $(sha256sum "$2" | cut -d' ' -f1)" \
    --header "Content-Type: $4" \
    --header "Cache-Control: $5" \
    --upload-file "$2" \
    --output "$work/answer" --write-out '%{http_code}' "$base_url/$1") ||
    fail "PUT $1 did not finish; run this again"

  case "$status" in
    200) echo "publish-repos: $1 written ($3)" ;;
    412)
      # A retry of a write that had landed, or another run's same bytes.
      if read_object "$1" && cmp -s "$work/body" "$2"; then
        echo "publish-repos: $1 is already there"
      else
        fail "$1 $6 ($3 failed with 412)"
      fi
      ;;
    *) fail "PUT $1 returned HTTP $status: $(cat "$work/answer")" ;;
  esac
}

# put_immutable <name> <file> <content type>
put_immutable() {
  put "$1" "$2" "If-None-Match: *" "$3" "public, max-age=31536000, immutable" \
    "is already published with other bytes, and is never replaced"
}

content_type() {
  case "$1" in
    *.xml) echo application/xml ;;
    *.gz) echo application/gzip ;;
    *.gpg | *.asc) echo application/pgp-signature ;;
    *) echo "text/plain; charset=utf-8" ;;
  esac
}

# condition_for <name> sets $condition: what its write depends on, or nothing
# when the store already has these bytes.
condition_for() {
  condition=""

  if read_object "$1"; then
    cmp -s "$work/body" "$dir/$1" && return
    cmp -s "$work/body" "$dir/base/$1" || fail "$1 is another publish's; run package-index again, then this"
    condition="If-Match: $etag"
  else
    [ ! -e "$dir/base/$1" ] || fail "$1 was published when the indexes were built, and is gone; look before running this again"
    condition="If-None-Match: *"
  fi
}

conditions=()
for name in "${indexes[@]}"; do
  condition_for "$name"
  conditions+=("$condition")
done

put_immutable "keys/${key##*/}" "$key" application/pgp-keys

for name in "${data[@]}"; do
  put_immutable "$name" "$dir/$name" application/gzip
done

for i in "${!indexes[@]}"; do
  name=${indexes[$i]}

  if [ -z "${conditions[$i]}" ]; then
    echo "publish-repos: $name is already there"
  else
    put "$name" "$dir/$name" "${conditions[$i]}" "$(content_type "$name")" "no-cache, max-age=0" \
      "changed under this publish; run package-index again, then this"
  fi
done
