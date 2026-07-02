#!/usr/bin/env bash
# Baseline throughput/latency of the gateway on the lightest route
# (catalog: http_log + add_header + request_size_limiting, no auth).
#
# Usage: RATE=200 DURATION=30s ./loadtest/scripts/baseline.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source ./lib.sh

RATE="${RATE:-200}"
DURATION="${DURATION:-30s}"

start_env

before="$(origin_stats 3001)"
echo "GET http://localhost:3000/catalog" |
	vegeta attack -rate="${RATE}/1s" -duration="$DURATION" -timeout=5s |
	tee "$RESULTS_DIR/baseline.bin" |
	vegeta report
after="$(origin_stats 3001)"

echo
echo "catalog origin stats: before=$before after=$after"
