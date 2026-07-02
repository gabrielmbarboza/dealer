#!/usr/bin/env bash
# Sustained moderate load over a longer window, to catch things a short
# burst won't show: latency drift, GC pressure, or interaction with the
# background config-file poller (default every 2s) under continuous
# traffic. Samples the catalog origin's connection stats every 10s so
# churn trends (see the MaxIdleConnsPerHost fix) are visible over time,
# not just as a single before/after snapshot.
#
# Usage: RATE=100 DURATION=5m ./loadtest/scripts/soak.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source ./lib.sh

RATE="${RATE:-100}"
DURATION="${DURATION:-5m}"

start_env

stats_log="$RESULTS_DIR/soak-stats.log"
: >"$stats_log"
(
	while true; do
		echo "$(date +%H:%M:%S) $(origin_stats 3001)" >>"$stats_log"
		sleep 10
	done
) &
sampler_pid=$!
trap 'kill "$sampler_pid" 2>/dev/null || true; stop_env' EXIT INT TERM

echo "GET http://localhost:3000/catalog" |
	vegeta attack -rate="${RATE}/1s" -duration="$DURATION" -timeout=5s |
	tee "$RESULTS_DIR/soak.bin" |
	vegeta report

kill "$sampler_pid" 2>/dev/null || true
echo
echo "connection stats over time (accepts should flatten out, not track requests 1:1):"
cat "$stats_log"
