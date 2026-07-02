#!/usr/bin/env bash
# Three phases against catalog: baseline -> sudden spike -> back to
# baseline. Reports each phase separately plus the combined run, so a
# spike-induced latency/error bump is visible against the phases around it
# instead of being averaged away.
#
# Usage: BASE_RATE=100 SPIKE_RATE=2000 ./loadtest/scripts/spike.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source ./lib.sh

BASE_RATE="${BASE_RATE:-100}"
SPIKE_RATE="${SPIKE_RATE:-2000}"
BASE_DURATION="${BASE_DURATION:-15s}"
SPIKE_DURATION="${SPIKE_DURATION:-5s}"

start_env

target="GET http://localhost:3000/catalog"

echo "=== warm-up: ${BASE_RATE}/s for $BASE_DURATION ==="
echo "$target" | vegeta attack -rate="${BASE_RATE}/1s" -duration="$BASE_DURATION" -timeout=5s |
	tee "$RESULTS_DIR/spike-1-warmup.bin" | vegeta report

echo
echo "=== spike: ${SPIKE_RATE}/s for $SPIKE_DURATION ==="
echo "$target" | vegeta attack -rate="${SPIKE_RATE}/1s" -duration="$SPIKE_DURATION" -timeout=5s |
	tee "$RESULTS_DIR/spike-2-spike.bin" | vegeta report

echo
echo "=== cool-down: ${BASE_RATE}/s for $BASE_DURATION ==="
echo "$target" | vegeta attack -rate="${BASE_RATE}/1s" -duration="$BASE_DURATION" -timeout=5s |
	tee "$RESULTS_DIR/spike-3-cooldown.bin" | vegeta report

echo
echo "=== combined ==="
vegeta report "$RESULTS_DIR/spike-1-warmup.bin" "$RESULTS_DIR/spike-2-spike.bin" "$RESULTS_DIR/spike-3-cooldown.bin"
