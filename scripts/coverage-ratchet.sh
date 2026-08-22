#!/usr/bin/env bash
# Coverage ratchet: fail CI if total coverage drops below the committed floor.
# The floor lives in coverage.floor at the repo root. Raise it as coverage
# grows; CI fails when a change would lower coverage below the floor.
set -euo pipefail

PROFILE="${1:-coverage.out}"
FLOOR_FILE="${2:-coverage.floor}"

if [[ ! -f "$FLOOR_FILE" ]]; then
    echo "No $FLOOR_FILE found, skipping coverage ratchet."
    exit 0
fi

if [[ ! -f "$PROFILE" ]]; then
    echo "::error::Coverage profile $PROFILE not found."
    exit 1
fi

# Packages excluded from the coverage calculation: generated templ output,
# CLI wiring and embedded static assets carry no testable logic, and the test
# helper packages are only ever exercised by other packages' tests.
EXCLUDE_PATTERN='/web/templates/|/cmd/|/internal/static/|/internal/database/databasetest/|/internal/httptestutil/'

filtered=$(mktemp)
trap 'rm -f "$filtered"' EXIT
grep -Ev "$EXCLUDE_PATTERN" "$PROFILE" > "$filtered"

floor=$(<"$FLOOR_FILE")
total=$(go tool cover -func="$filtered" | awk '/^total:/ {sub(/%/, "", $3); print $3}')

if [[ -z "$total" ]]; then
    echo "::error::Unable to parse total coverage from $PROFILE."
    exit 1
fi

echo "total coverage: ${total}% (floor: ${floor}%, excluding ${EXCLUDE_PATTERN})"

if awk -v t="$total" -v f="$floor" 'BEGIN {exit !(t < f)}'; then
    echo "::error::Coverage ${total}% fell below the floor of ${floor}%." \
         "If this drop is intentional, lower coverage.floor in the same PR."
    exit 1
fi
