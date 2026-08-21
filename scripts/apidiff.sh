#!/usr/bin/env bash
#
# apidiff.sh — report public-API changes between the latest release tag
# and the current working tree, for developer visibility.
#
# PRE-1.0 POLICY: GTB is currently v0.x. Breaking changes are permitted
# and ship as a MINOR bump (see docs/reference/api-stability.md). This script
# is therefore ADVISORY only — it always exits 0 so it never fails a build
# or an MR pipeline. Its job is to make API changes *visible* so they are
# seen and intentional. From v1.0 the wrapping CI job becomes a BLOCKING
# gate (see .gitlab-ci.yml).
#
# Usage:
#   scripts/apidiff.sh            # compare against the latest vX.Y.Z tag
#   scripts/apidiff.sh v0.15.0    # compare against an explicit baseline tag
#
# The script:
#   1. ensures `apidiff` (golang.org/x/exp/cmd/apidiff) is on PATH,
#      installing it on demand if not;
#   2. finds the latest semver tag (or uses the one passed as $1);
#   3. checks out that tag into a throwaway git worktree so its module
#      dependencies resolve (apidiff needs to type-check the baseline);
#   4. writes module export data for the baseline and the current tree;
#   5. prints the incompatible/compatible API diff.
#
# It is self-contained: no preinstalled apidiff required.

set -euo pipefail

MODULE="gitlab.com/phpboyscout/go-tool-base"

# Resolve the repository root (this script may be invoked from anywhere).
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# --- 1. Ensure apidiff is available --------------------------------------
APIDIFF="$(command -v apidiff || true)"
if [[ -z "$APIDIFF" ]]; then
  # Honour GOBIN, then GOPATH/bin, when locating the freshly installed binary.
  GOBIN_DIR="$(go env GOBIN)"
  [[ -z "$GOBIN_DIR" ]] && GOBIN_DIR="$(go env GOPATH)/bin"
  if [[ ! -x "$GOBIN_DIR/apidiff" ]]; then
    echo "apidiff not found; installing golang.org/x/exp/cmd/apidiff..." >&2
    go install golang.org/x/exp/cmd/apidiff@latest
  fi
  APIDIFF="$GOBIN_DIR/apidiff"
fi

if [[ ! -x "$APIDIFF" ]]; then
  echo "apidiff: could not install or locate the apidiff binary; skipping (advisory)." >&2
  exit 0
fi

# --- 2. Determine the baseline tag ---------------------------------------
BASELINE="${1:-}"
if [[ -z "$BASELINE" ]]; then
  # Latest vX.Y.Z tag by semver ordering.
  BASELINE="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n1 || true)"
fi

if [[ -z "$BASELINE" ]]; then
  echo "apidiff: no vX.Y.Z baseline tag found; nothing to compare against (first release?)." >&2
  echo "apidiff: skipping (advisory)." >&2
  exit 0
fi

if ! git rev-parse -q --verify "refs/tags/${BASELINE}" >/dev/null; then
  echo "apidiff: baseline tag '${BASELINE}' does not exist; skipping (advisory)." >&2
  exit 0
fi

echo "apidiff: comparing current tree against ${BASELINE} (module ${MODULE})" >&2

# --- 3. Lay out a throwaway worktree for the baseline tag ----------------
WORK="$(mktemp -d)"
OLD_API="$(mktemp)"
NEW_API="$(mktemp)"
cleanup() {
  git worktree remove --force "$WORK" >/dev/null 2>&1 || true
  rm -rf "$WORK" "$OLD_API" "$NEW_API" 2>/dev/null || true
}
trap cleanup EXIT

# The worktree lives outside the repository, so it is a separate path as far as
# git's ownership check is concerned. CI marks only $CI_PROJECT_DIR as a
# safe.directory, which leaves this one unmarked — a difference invisible on a
# developer's machine, where the repository is already owned by the user running
# the script. Mark it here rather than relying on the caller to know.
#
# Deliberately not fatal: `git config` can fail when HOME is unset or read-only,
# and on a machine where the check never fires that failure would be noise.
git config --global --add safe.directory "$WORK" >/dev/null 2>&1 || true

WT_ERR="$(git worktree add --detach "$WORK" "$BASELINE" 2>&1 >/dev/null)" || {
  echo "apidiff: could not create a worktree for ${BASELINE}; skipping (advisory)." >&2
  echo "apidiff: git said: ${WT_ERR}" >&2
  exit 0
}

# --- 4. Export module API for baseline and current tree ------------------
# `-m -w FILE MODULE` must run from the module root so its go.mod (and
# therefore its dependencies) resolve.
#
# stderr is CAPTURED, not discarded. It carries "Ignoring internal package ..."
# noise on the happy path, which is why it used to be sent to /dev/null — but
# that also swallowed the reason on the sad path, so a skip reported that it had
# failed and never why. The noise is worth printing on the rare failure; silence
# is not, because this script always exits 0 and a silent skip is
# indistinguishable from "no API changes".
export_api() {
  local dir="$1" out="$2" label="$3" err

  if ! err="$( cd "$dir" && "$APIDIFF" -m -w "$out" "$MODULE" 2>&1 >/dev/null )"; then
    echo "apidiff: failed to read ${label}; skipping (advisory)." >&2
    echo "apidiff: ---- stderr from ${label} ----" >&2
    echo "${err}" >&2
    echo "apidiff: --------------------------------" >&2

    return 1
  fi

  return 0
}

export_api "$WORK" "$OLD_API" "the baseline API at ${BASELINE}" || exit 0
export_api "$REPO_ROOT" "$NEW_API" "the current-tree API" || exit 0

# --- 5. Print the diff ----------------------------------------------------
echo "===================================================================" >&2
echo " API changes since ${BASELINE} (advisory — pre-1.0, non-blocking)"   >&2
echo "===================================================================" >&2
"$APIDIFF" -m "$OLD_API" "$NEW_API" 2>/dev/null || true

# Always succeed: pre-1.0 this is visibility, not a gate.
exit 0
