#!/usr/bin/env bash
# Compares two real routes from config.yml at the same rate/duration to
# estimate what each plugin stack costs on top of the bare proxy:
#   catalog:  http_log, add_header,         request_size_limiting
#   payments: http_log, jwt_auth (HS256),   request_size_limiting
# The delta between the two reports is dominated by jwt_auth's per-request
# HMAC parse/verify (add_header just sets a static header). The stub
# origins normally simulate different latencies per service, which would
# confound that comparison, so this script pins both to the same value.
#
# Usage: RATE=200 DURATION=30s ./loadtest/scripts/plugin-overhead.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source ./lib.sh

RATE="${RATE:-200}"
DURATION="${DURATION:-30s}"

# Pin both origins to the same simulated latency so the report delta below
# reflects plugin cost, not the stubs' (deliberately different) defaults.
ORIGIN_LATENCY="${ORIGIN_LATENCY:-15ms}"
export CATALOG_LATENCY="$ORIGIN_LATENCY"
export PAYMENTS_LATENCY="$ORIGIN_LATENCY"

start_env

token="$(jwt_token)"

echo "=== catalog (http_log, add_header, request_size_limiting) ==="
echo "GET http://localhost:3000/catalog" |
	vegeta attack -rate="${RATE}/1s" -duration="$DURATION" -timeout=5s |
	tee "$RESULTS_DIR/plugin-overhead-catalog.bin" |
	vegeta report

echo
echo "=== payments (http_log, jwt_auth, request_size_limiting) ==="
printf 'POST http://localhost:3000/payments\nAuthorization: Bearer %s\n' "$token" |
	vegeta attack -rate="${RATE}/1s" -duration="$DURATION" -timeout=5s |
	tee "$RESULTS_DIR/plugin-overhead-payments.bin" |
	vegeta report
