#!/usr/bin/env bash
#
# coverage-policy.sh — enforce the per-package ≥90% coverage policy advisorily.
#
# Reads .coverage-policy.yaml (the machine-readable form of the rule) and the
# coverage profile the test run already produced. A package is FLAGGED when it is
# below the threshold AND is neither in the `excluded` list nor matched by a
# `not_counted` prefix. This is the enforcement half of
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
# It reads cover.out ($COVER_PROFILE to override) rather than running the suite.
# In CI that file is go-test's artifact, so this job costs seconds instead of a
# second full `go test ./...`; outside CI the profile is generated on demand if
# it is missing. Measured 2026-09-05 at 1b40357: the per-package percentages
# from the profile match `go test ./... -cover` for all 69 packages that carry a
# number, with or without -race (cicd spec 0079 D8).
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

COVER="${COVER_PROFILE:-cover.out}"

# The profile is read, not regenerated. go-test has already run
# `go test -race -coverprofile=cover.out ./...` and publishes it, so running the
# suite again here cost a second full run of the module for a number that
# already existed (60 runs, 229 runner minutes over the 21 days to 2026-09-03).
#
# Outside CI there is no artifact, so generate it once. In CI, never: a missing
# or empty profile is a failure to report, not a gap to paper over. This used to
# be `go test ./... -cover 2>/dev/null | grep "coverage:"`, and when the test run
# itself failed — as it did from v0.39.0, when go.mod's directive outran the job
# image's Go and GOTOOLCHAIN=local refused to fetch one — stdout was empty, the
# reason was gone, the loop below never ran, and the script reported "every
# countable package is >= 90%". A gate that answers OK because it measured
# nothing is worse than one that is switched off, because nobody goes looking.
# Reading an artifact can fail the same way by a different route, so every route
# is checked below.
if [ ! -f "$COVER" ]; then
	if [ -n "${CI:-}" ]; then
		echo "coverage-policy: $COVER is missing." >&2
		echo "coverage-policy: it comes from the go-test job's artifact (needs: go-test, artifacts: true)." >&2
		echo "coverage-policy: refusing to report a pass on no data." >&2
		exit 1
	fi
	echo "coverage-policy: $COVER not found, generating it (this can take a few minutes)"
	test_err=$(mktemp)
	trap 'rm -f "$test_err"' EXIT
	if ! go test -race -coverprofile="$COVER" ./... >/dev/null 2>"$test_err"; then
		echo "coverage-policy: the coverage run failed." >&2
		echo "coverage-policy: ---- go test stderr ----" >&2
		cat "$test_err" >&2
		echo "coverage-policy: ------------------------" >&2
		echo "coverage-policy: refusing to report a pass on no data." >&2
		exit 1
	fi
fi

if [ ! -s "$COVER" ]; then
	echo "coverage-policy: $COVER is empty." >&2
	echo "coverage-policy: refusing to report a pass on no data." >&2
	exit 1
fi

if ! grep -q "^${MODULE}/" "$COVER"; then
	echo "coverage-policy: $COVER carries no line for ${MODULE}." >&2
	echo "coverage-policy: it is empty of this module, or belongs to another one." >&2
	echo "coverage-policy: refusing to report a pass on no data." >&2
	exit 1
fi

echo "coverage-policy: reading $COVER ($(grep -c . "$COVER") profile lines)"

# Aggregate the profile per package, the way `go test -cover` does: covered
# statements over total statements, counting a block once however many times it
# ran. Emitted in `go test` output shape so the loop below is unchanged.
cover_out=$(awk '
	/^mode:/ { next }
	{
		split($1, a, ":")
		file = a[1]
		idx = match(file, /\/[^\/]*$/)
		if (idx == 0) next
		pkg = substr(file, 1, idx - 1)
		total[pkg] += $2
		if ($3 + 0 > 0) covered[pkg] += $2
	}
	END {
		for (p in total)
			if (total[p] > 0)
				printf "ok  \t%s\tcoverage: %.1f%% of statements\n", p, 100 * covered[p] / total[p]
	}
' "$COVER")

if [ -z "$cover_out" ]; then
	echo "coverage-policy: $COVER parsed to no package results." >&2
	echo "coverage-policy: refusing to report a pass on no data." >&2
	exit 1
fi

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
