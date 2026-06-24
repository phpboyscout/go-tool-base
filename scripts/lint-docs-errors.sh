#!/bin/bash
set -euo pipefail

DOC_FILE="docs/reference/api/errors.md"
FAILED=0

echo "Linting sentinel errors in pkg/ against $DOC_FILE..."

# Find all sentinel error declarations in pkg/
ERRORS=$(grep -rE 'var\s+Err[A-Z][a-zA-Z0-9]*\s*=' pkg/ | grep -v '_test.go' || true)

if [ -z "$ERRORS" ]; then
    echo "No exported sentinel errors found in pkg/."
    exit 0
fi

# Extract just the error names and files
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
