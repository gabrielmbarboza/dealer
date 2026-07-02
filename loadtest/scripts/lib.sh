#!/usr/bin/env bash
# Shared helpers for loadtest/scripts/*.sh: build+start the gateway and
# origin stubs, tear them down, and mint auth tokens. Source this file, call
# start_env, run your vegeta workload, then stop_env (a trap is set up for
# you so stop_env also runs on error/interrupt).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RESULTS_DIR="${RESULTS_DIR:-$REPO_ROOT/loadtest/results}"
JWT_SECRET="${JWT_SECRET:-loadtest-secret}"
export JWT_SECRET

export PATH="$(go env GOPATH)/bin:$PATH"

GATEWAY_PID=""
STUBS_PID=""

start_env() {
	mkdir -p "$RESULTS_DIR"

	# CONFIG_PATH lets callers (e.g. reload.sh) point the gateway at a
	# scratch copy of config.yml instead of the repo's own, so hot-reload
	# experiments don't touch a tracked file.
	export DEALER_CONFIG_PATH="${CONFIG_PATH:-$REPO_ROOT/config.yml}"

	echo "lib.sh: building gateway and stubs..." >&2
	go build -o "$RESULTS_DIR/dealer" "$REPO_ROOT/cmd/dealer"
	go build -o "$RESULTS_DIR/stubs" "$REPO_ROOT/loadtest/stubs"

	(cd "$REPO_ROOT" && "$RESULTS_DIR/stubs") >"$RESULTS_DIR/stubs.log" 2>&1 &
	STUBS_PID=$!
	(cd "$REPO_ROOT" && "$RESULTS_DIR/dealer") >"$RESULTS_DIR/gateway.log" 2>&1 &
	GATEWAY_PID=$!

	trap stop_env EXIT INT TERM

	for _ in $(seq 1 50); do
		if curl -sf -o /dev/null http://localhost:3000/ && curl -sf -o /dev/null http://localhost:3001/__stats; then
			echo "lib.sh: gateway (pid $GATEWAY_PID) and stubs (pid $STUBS_PID) are up" >&2
			return 0
		fi
		sleep 0.1
	done

	echo "lib.sh: gateway/stubs did not become ready in time, see $RESULTS_DIR/*.log" >&2
	exit 1
}

stop_env() {
	[ -n "$GATEWAY_PID" ] && kill "$GATEWAY_PID" 2>/dev/null || true
	[ -n "$STUBS_PID" ] && kill "$STUBS_PID" 2>/dev/null || true
	wait "$GATEWAY_PID" 2>/dev/null || true
	wait "$STUBS_PID" 2>/dev/null || true
}

# jwt_token prints a fresh JWT signed with JWT_SECRET, for routes guarded by
# the jwt_auth plugin (payments, orders).
jwt_token() {
	(cd "$REPO_ROOT" && go run ./loadtest/jwtgen)
}

# origin_stats prints the /__stats {accepts, requests} JSON for one of the
# stub origins: catalog (3001), payments (3002) or orders (3003).
origin_stats() {
	local port="$1"
	curl -s "http://localhost:$port/__stats"
}
