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

floor=$(<"$FLOOR_FILE")
total=$(go tool cover -func="$PROFILE" | awk '/^total:/ {sub(/%/, "", $3); print $3}')

echo "total coverage: ${total}% (floor: ${floor}%)"

if awk -v t="$total" -v f="$floor" 'BEGIN {exit !(t < f)}'; then
    echo "::error::Coverage ${total}% fell below the floor of ${floor}%." \
         "If this drop is intentional, lower coverage.floor in the same PR."
    exit 1
fi
