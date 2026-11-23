#!/bin/bash
set -euo pipefail

# comm and sort must agree on one collation, whatever the caller's locale.
export LC_ALL=C

DOC_FILE="docs/explanation/components/errors.md"

if [ ! -f "$DOC_FILE" ]; then
    echo "❌ Error catalogue not found at $DOC_FILE"
    exit 1
fi

echo "Linting sentinel errors in pkg/ against $DOC_FILE..."

# Every exported sentinel in pkg/, as "<package dir> <name>": both single-line
# `var Err... = errors....` and entries inside a `var ( ... )` block. The CI
# image's grep is BusyBox, which has no --include, so find picks the files.
mapfile -t GO_FILES < <(find pkg -name '*.go' ! -name '*_test.go' | sort)

set +e
MATCHES=$(grep -HnE '(^|[[:space:]])Err[A-Z][a-zA-Z0-9]*[[:space:]]*=[[:space:]]*errors\.' "${GO_FILES[@]}")
GREP_STATUS=$?
set -e

if [ "$GREP_STATUS" -gt 1 ]; then
    echo "❌ grep failed (status $GREP_STATUS) while looking for sentinels."
    exit 1
fi

SENTINELS=$(printf '%s\n' "$MATCHES" \
    | sed -nE 's|^([^:]*)/[^/:]*:[0-9]+:.*(Err[A-Z][a-zA-Z0-9]*)[[:space:]]*=[[:space:]]*errors\..*|\1 \2|p' \
    | sort -u)

# pkg/ has sentinels, so finding none means the scan itself broke.
if [ -z "$SENTINELS" ]; then
    echo "❌ No exported sentinel errors found in pkg/; the scan found nothing to check."
    exit 1
fi

# Every `Err...` the catalogue names, as "<package dir> <name>", taken from the
# "## `pkg/...`" or "### `pkg/...`" heading it sits under. A sentinel counts as
# documented only under its own package, so two packages sharing a name
# (ErrNoPath) each need an entry.
DOCUMENTED=$(awk '
    /^##+ / { pkg = ""; if (match($0, /`pkg\/[^`]+`/)) pkg = substr($0, RSTART + 1, RLENGTH - 2); next }
    pkg != "" {
        line = $0
        while (match(line, /`Err[A-Z][a-zA-Z0-9]*`/)) {
            print pkg " " substr(line, RSTART + 1, RLENGTH - 2)
            line = substr(line, RSTART + RLENGTH)
        }
    }
' "$DOC_FILE" | sort -u)

# awk rather than comm, which BusyBox builds can lack, and with both lists in
# variables, since a failed process substitution would read as an empty list.
MISSING=$(awk -v documented="$DOCUMENTED" '
    BEGIN { n = split(documented, rows, "\n"); for (i = 1; i <= n; i++) seen[rows[i]] = 1 }
    !($0 in seen)
' <<< "$SENTINELS")

if [ -n "$MISSING" ]; then
    while read -r pkg name; do
        echo "❌ Undocumented error: $name (in $pkg; it needs a row under the \`$pkg\` heading)"
    done <<< "$MISSING"
    echo "Lint failed: Some sentinel errors are missing from the Error Catalogue."
    exit 1
fi

echo "✅ All sentinel errors are documented!"
