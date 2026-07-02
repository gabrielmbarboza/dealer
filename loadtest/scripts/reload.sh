#!/usr/bin/env bash
# Fires a steady attack against catalog and, mid-attack, rewrites the
# config file the gateway is watching, forcing a hot-reload while traffic
# is in flight. The routing table swap (gw.mux atomic.Pointer) should be
# invisible to clients: the key assertion is the success ratio for the
# *whole* run staying effectively 100%, i.e. no request dropped during the
# swap. Runs against a scratch copy of config.yml, never the tracked one.
#
# Usage: RATE=200 DURATION=20s ./loadtest/scripts/reload.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source ./lib.sh

RATE="${RATE:-200}"
DURATION="${DURATION:-20s}"

CONFIG_PATH="$RESULTS_DIR/reload-config.yml"
cp "$REPO_ROOT/config.yml" "$CONFIG_PATH"
export CONFIG_PATH
export DEALER_CONFIG_POLL_INTERVAL="${DEALER_CONFIG_POLL_INTERVAL:-500ms}"

start_env

case "$DURATION" in
*s) mid_sleep=$(("${DURATION%s}" / 2)) ;;
*) mid_sleep=5 ;; # DURATION isn't plain seconds (e.g. "1m") - fall back to a fixed mid-point
esac

(
	sleep "$mid_sleep"
	echo "reload.sh: rewriting config mid-attack to trigger hot-reload" >&2
	sed 's/X-Gateway: "dealer"/X-Gateway: "dealer-reloaded"/' "$REPO_ROOT/config.yml" >"$CONFIG_PATH"
) &
mutator_pid=$!

echo "GET http://localhost:3000/catalog" |
	vegeta attack -rate="${RATE}/1s" -duration="$DURATION" -timeout=5s |
	tee "$RESULTS_DIR/reload.bin" |
	vegeta report

wait "$mutator_pid" 2>/dev/null || true

echo
echo "success ratio should be ~100% above - a dip would mean requests were"
echo "dropped while the routing table was swapped during hot-reload."
