#!/bin/bash
set -euo pipefail

DOC_FILE="docs/explanation/components/errors.md"
FAILED=0

if [ ! -f "$DOC_FILE" ]; then
    echo "❌ Error catalogue not found at $DOC_FILE"
    exit 1
fi

echo "Linting sentinel errors in pkg/ against $DOC_FILE..."

# Find all exported sentinel error declarations in pkg/ — both single-line
# `var Err... = errors....` and entries inside a `var ( ... )` block (which have
# no `var` keyword on the line). Excludes test files.
ERRORS=$(grep -rnE '(^|[[:space:]])Err[A-Z][a-zA-Z0-9]*[[:space:]]*=[[:space:]]*errors\.' pkg/ \
    | grep -v '_test.go' || true)

if [ -z "$ERRORS" ]; then
    echo "No exported sentinel errors found in pkg/."
    exit 0
fi

# Extract each error name and the file it lives in, and check the catalogue.
while IFS= read -r line; do
    FILE=$(echo "$line" | cut -d: -f1)
    ERR_NAME=$(echo "$line" | grep -oE 'Err[A-Z][a-zA-Z0-9]*' | head -n1)

    if ! grep -q "\`$ERR_NAME\`" "$DOC_FILE"; then
        echo "❌ Undocumented error: $ERR_NAME (found in $FILE)"
        FAILED=1
    fi
done <<< "$ERRORS"

if [ $FAILED -eq 1 ]; then
    echo "Lint failed: Some sentinel errors are missing from the Error Catalogue."
    exit 1
fi

echo "✅ All sentinel errors are documented!"
exit 0
