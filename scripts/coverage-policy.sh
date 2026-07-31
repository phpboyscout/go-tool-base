#!/usr/bin/env bash
#
# coverage-policy.sh — enforce the per-package ≥90% coverage policy advisorily.
#
# Reads .coverage-policy.yaml (the machine-readable form of the rule) and runs
# unit coverage over the whole module. A package is FLAGGED when it is below the
# threshold AND is neither in the `excluded` list nor matched by a `not_counted`
# prefix. This is the enforcement half of
# https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0090-coverage-gap-closure.
#
# It exits non-zero when there are violations so the wrapping CI job surfaces
# them; that job is `allow_failure: true`, so this is ADVISORY — it never blocks
# an MR. The remedy for a flagged package is one of:
#   1. add tests to reach the threshold, or
#   2. add it to .coverage-policy.yaml `excluded:` with a one-line rationale.
#
# Usage:
#   scripts/coverage-policy.sh                  # uses .coverage-policy.yaml
#   scripts/coverage-policy.sh path/to/policy.yaml
#
set -uo pipefail

MODULE="gitlab.com/phpboyscout/go-tool-base"
POLICY="${1:-.coverage-policy.yaml}"

if [ ! -f "$POLICY" ]; then
	echo "coverage-policy: policy file not found: $POLICY" >&2
	exit 2
fi

threshold=$(awk -F': *' '/^threshold:/ {print $2; exit}' "$POLICY")
[ -z "$threshold" ] && threshold=90

# Parse the `not_counted:` YAML list (items: "  - prefix").
mapfile -t not_counted < <(
	awk '
		/^not_counted:/ {f=1; next}
		/^[A-Za-z_]+:/  {f=0}
		f && /^[[:space:]]*-[[:space:]]*/ {
			sub(/^[[:space:]]*-[[:space:]]*/, "");
			sub(/[[:space:]]*(#.*)?$/, "");
			if (length($0)) print
		}
	' "$POLICY"
)

# Parse the `excluded:` package keys ("  - { pkg: X, reason: ... }").
mapfile -t excluded < <(
	grep -oE 'pkg:[[:space:]]*[^,}]+' "$POLICY" | sed -E 's/pkg:[[:space:]]*//; s/[[:space:]]+$//'
)

echo "coverage-policy: threshold ${threshold}%, ${#excluded[@]} excluded package(s), ${#not_counted[@]} not-counted prefix(es)"

is_not_counted() {
	local rel="$1" nc
	for nc in "${not_counted[@]}"; do
		case "$nc" in
			*/) case "$rel/" in "$nc"*) return 0 ;; esac ;;
			*)  [ "$rel" = "$nc" ] && return 0 ;;
		esac
	done
	return 1
}

is_excluded() {
	local rel="$1" ex
	for ex in "${excluded[@]}"; do
		[ "$rel" = "$ex" ] && return 0
	done
	return 1
}

echo "coverage-policy: running unit coverage over ./... (this can take a few minutes)"
cover_out=$(go test ./... -cover 2>/dev/null | grep "coverage:")

violations=0
while IFS= read -r line; do
	[ -z "$line" ] && continue
	pkg=$(printf '%s\n' "$line" | grep -oE "${MODULE}/[^[:space:]]+" | head -1)
	[ -z "$pkg" ] && continue
	rel=${pkg#"${MODULE}/"}
	pct=$(printf '%s\n' "$line" | grep -oE 'coverage: [0-9.]+%' | grep -oE '[0-9.]+')
	[ -z "$pct" ] && continue

	# Above threshold → fine.
	if awk "BEGIN{exit !($pct >= $threshold)}"; then
		continue
	fi
	# Below threshold but allowed → fine.
	is_not_counted "$rel" && continue
	is_excluded "$rel" && continue

	printf 'VIOLATION: %s at %s%% (< %s%%) is not on the coverage exclusion list (.coverage-policy.yaml)\n' "$rel" "$pct" "$threshold"
	violations=$((violations + 1))
done <<< "$cover_out"

echo ""
if [ "$violations" -gt 0 ]; then
	echo "coverage-policy: ${violations} package(s) below ${threshold}% and not excluded."
	echo "  Fix: add tests to reach ${threshold}%, OR add the package to .coverage-policy.yaml 'excluded:' with a rationale (keep it in sync with the spec's Bucket A/B)."
	exit 1
fi

echo "coverage-policy: OK — every countable package is ≥ ${threshold}% or explicitly excluded."
exit 0
