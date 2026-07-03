# Dealer API Gateway

![alt text](https://github.com/gabrielmbarboza/dealer/blob/main/assets/images/dealer_logo.jpg?raw=true)

## Table of Contents

- [About](#about)
- [Getting Started](#getting_started)
- [Usage](#usage)
- [Contributing](CONTRIBUTING.md)
- [License](#license)

## About <a name = "about"></a>

Dealer is a lightweight API Gateway written in Go. It reads a `config.yml` file describing your internal services and forwards incoming requests to them, preserving HTTP methods, headers and body in both directions, with configuration changes picked up automatically at runtime — no restart needed.

Beyond routing, Dealer handles the concerns a gateway sitting at the edge of a service architecture needs to: TLS termination with hot-reloadable certificates, load balancing with active health checks and a circuit breaker per origin, retry with backoff on transient failures, request size limits, JWT auth, CORS, rate limiting (in-memory or shared across instances), Prometheus metrics, OpenTelemetry tracing, and request-id correlation across services. Every one of these is opt-in and costs nothing when left unconfigured — see [Usage](#usage) for the full list and how to enable each one.

It also supports a small plugin system that runs before a request is forwarded to its service:

- `add_header` — adds HTTP headers to the forwarded request.
- `http_log` — logs every request that hits the gateway.
- `request_size_limiting` — blocks requests whose body exceeds a configured size.
- `jwt_auth` — blocks requests without a valid JWT (accepted via header or query string).
- `rate_limiting` — blocks requests once a client IP exceeds a configured requests-per-second rate, allowing short bursts. Per-instance in-memory by default; can share counters across gateway instances instead (see [Usage](#usage)).
- `cors` — adds CORS response headers for allowed origins and answers preflight `OPTIONS` requests directly, without forwarding them to the service.

## Getting Started  <a name = "getting_started"></a>

These instructions will get you a copy of the project up and running on your local machine for development and testing purposes. See [deployment](#deployment) for notes on how to deploy the project on a live system.

### Prerequisites

- [Go](https://go.dev/dl/) 1.25.11 or later (the workspace's `go.mod`/`go.work` files declare `go 1.25.11`; with `GOTOOLCHAIN=auto`, the `go` command downloads it automatically if you have an older version installed).
- Or [Docker](https://docs.docker.com/get-docker/), if you'd rather not install Go locally.

### Installing

Clone the repository:

```
git clone https://github.com/gabrielmbarboza/dealer.git
cd dealer
```

Run it directly with Go (uses the Go workspace defined in `go.work`):

```
go run ./cmd/dealer
```

Or build and run the container with Docker Compose (copy `.env.example` to `.env` first and set `JWT_SECRET`, required by `config.yml`'s `jwt_auth` plugins):

```
cp .env.example .env
docker compose up --build
```

The gateway listens on `0.0.0.0:3000` by default (override with `DEALER_LISTEN_ADDR`) and reads `config.yml` from the current directory by default. Under Docker Compose, the project directory is bind-mounted read-only, so editing `config.yml` on the host is picked up by hot-reload without rebuilding the image.

## Usage <a name = "usage"></a>

Edit `config.yml` to declare your internal services, their routes and plugins:

```yaml
services:
  - name: "payments"
    path: "/payments"
    origin_url: "http://0.0.0.0:3002"
    methods: ["POST"]
    plugins:
      - name: http_log
      - name: jwt_auth
        config:
          secret_env: JWT_SECRET
```

Routes may include a path parameter, e.g. `path: "/orders/{id}"`, which matches any value in that segment.

Plugins run in the order they're declared, and a plugin that blocks a request (like `jwt_auth`) never calls the next one — so if a service is browser-facing, declare `cors` before any blocking plugin, or its preflight `OPTIONS` requests (which never carry an `Authorization` header) will be rejected before `cors` gets a chance to answer them:

```yaml
services:
  - name: "storefront"
    path: "/storefront"
    origin_url: "http://0.0.0.0:3005"
    methods: ["GET", "POST"]
    plugins:
      - name: cors
        config:
          allowed_origins: ["https://shop.example.com"]
          allowed_methods: ["GET", "POST"]
          allowed_headers: ["Content-Type", "Authorization"]
          allow_credentials: true
          max_age: 600
      - name: jwt_auth
        config:
          secret_env: JWT_SECRET
```

A service with a `cors` plugin gets its `OPTIONS` route registered automatically, even if `OPTIONS` isn't listed in `methods` — otherwise the browser's preflight request would be rejected by the router before the plugin chain runs at all.

A service can forward to more than one origin with `origin_urls` instead of `origin_url`. Requests are round-robined across them, and an origin that just failed is skipped for a cooldown period rather than being retried immediately:

```yaml
services:
  - name: "catalog"
    path: "/catalog"
    origin_urls:
      - "http://0.0.0.0:3001"
      - "http://0.0.0.0:3004"
    methods: ["GET"]
```

A service can also opt into active health checks with `health_check`, so an origin that's already unhealthy is skipped *before* it ever fails a real request, and rejoins automatically once it recovers. This is independent of (and complements) the reactive cooldown above:

```yaml
services:
  - name: "catalog"
    path: "/catalog"
    origin_urls:
      - "http://0.0.0.0:3001"
      - "http://0.0.0.0:3004"
    methods: ["GET"]
    health_check:
      path: "/healthz"   # falls back to DEALER_HEALTH_CHECK_PATH if omitted
      interval: "5s"     # falls back to DEALER_HEALTH_CHECK_INTERVAL if omitted
```

When `DEALER_RETRY_MAX_ATTEMPTS` is set above its default of `1`, a request that fails with a transient network error (connection refused, timeout - not a 4xx/5xx response, which is a valid answer from the origin) is retried with exponential backoff. Only idempotent methods (`GET`, `HEAD`, `OPTIONS`) are retried by default, since retrying a `POST` risks the origin executing it twice; a service can opt into retrying non-idempotent methods if it knows that's safe:

```yaml
services:
  - name: "payments"
    path: "/payments"
    origin_url: "http://0.0.0.0:3002"
    methods: ["POST"]
    retry_unsafe_methods: true
```

When `DEALER_CIRCUIT_BREAKER_THRESHOLD` is set above its default of `0` (disabled), an origin that fails that many *consecutive* requests has its circuit breaker tripped: further requests fast-fail with a 502 without even dialing it, until `DEALER_CIRCUIT_BREAKER_COOLDOWN` elapses, at which point a single trial request is let through (a successful one closes the breaker immediately; a failed one doubles the cooldown, up to a cap). This is stricter than the reactive cooldown above: with only a plain cooldown, a load-balanced service still attempts a known-bad origin as a last resort if every origin is currently in cooldown; once that origin's breaker is open, it's skipped even as a last resort.

By default, `rate_limiting` tracks each client IP in an in-memory map local to that gateway process — fine for a single instance, but each replica behind a load balancer would enforce the limit independently (N replicas effectively multiply the real limit by N). Setting `mode: distributed` backs the same plugin with an embedded [SugarDB](https://github.com/EchoVault/SugarDB) instance instead, so counters can be shared:

```yaml
services:
  - name: "catalog"
    path: "/catalog"
    origin_url: "http://0.0.0.0:3001"
    plugins:
      - name: rate_limiting
        config:
          requests_per_second: 5
          burst: 10
          mode: distributed
```

Distributed mode trades the in-memory mode's smooth, continuously-replenishing token bucket for a simpler fixed one-second window counter (an atomic increment-then-expire per key), since a true distributed token bucket needs atomic read-modify-write scripting that SugarDB's embedded command API doesn't expose — it allows up to `burst` requests in any given second rather than smoothly draining and refilling at `requests_per_second`. Every `rate_limiting` plugin configured with `mode: distributed` shares one embedded SugarDB instance per gateway process, so this is opt-in and costs nothing when unused.

SugarDB can also form a real multi-node replication cluster over RAFT, so counters are shared *across gateway processes* rather than just across services on one process. To form a cluster, the first node bootstraps it and every other node joins by referencing the bootstrap node's `ServerID` and discovery address:

```
# Node 1 (bootstraps the cluster)
DEALER_RATELIMIT_CLUSTER_SERVER_ID=gateway-1
DEALER_RATELIMIT_CLUSTER_BOOTSTRAP=true

# Node 2 (joins node 1)
DEALER_RATELIMIT_CLUSTER_SERVER_ID=gateway-2
DEALER_RATELIMIT_CLUSTER_JOIN_ADDR=gateway-1/<node-1-host>:7946
```

`DEALER_RATELIMIT_CLUSTER_JOIN_ADDR` must be in `<remote-server-id>/<host>:<discovery-port>` form (memberlist, which SugarDB uses for cluster membership, requires the node name prefix) — not just `host:port`. Every node needs a unique `DEALER_RATELIMIT_CLUSTER_SERVER_ID`, matching whatever other nodes reference in their own `JOIN_ADDR`; if running more than one node on the same host, each also needs its own `DEALER_RATELIMIT_CLUSTER_DISCOVERY_PORT`. Single-instance distributed mode (the example above, with no cluster variables set) doesn't need any of this and works standalone. Two-node replication is covered by an automated integration test (`TestSugarDBStore_TwoNodeClusterReplicatesCounters`) that bootstraps one node, joins a second, and confirms a counter incremented on the first is visible on the second.

Every response carries an `X-Request-Id` header, so a request can be correlated across the gateway's own logs (the `http_log` plugin includes it as `request_id=...`) and, if a downstream service also logs it, across services too. By default the gateway always generates a fresh id, ignoring any `X-Request-Id` the client sent — set `DEALER_TRUST_REQUEST_ID=true` to instead reuse an inbound one, which only makes sense when Dealer sits behind a trusted upstream (e.g. a load balancer) that sets this header itself; otherwise a client could inject an arbitrary id into your logs.

Setting `OTEL_EXPORTER_OTLP_ENDPOINT` (e.g. `http://localhost:4318`) turns on distributed tracing: every request gets a span (named `METHOD path`, tagged with the same request id from above) exported over OTLP/HTTP to that endpoint. This — deliberately — uses OpenTelemetry's own standard environment variables instead of `DEALER_*`-prefixed ones, so Dealer plugs into whatever OTel collector/backend you already run without inventing a parallel configuration surface; see the [OpenTelemetry docs](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/) for the full set (headers, protocol, timeouts, etc.) that `OTEL_EXPORTER_OTLP_*` supports. Tracing is entirely opt-in: with the endpoint unset, spans are never created (the OpenTelemetry API's default is a no-op), so there's no cost to leaving it off. The active span's W3C trace context is also injected into the request forwarded to the origin (as a `traceparent` header), so a downstream service that's also instrumented continues the same trace instead of the gateway being a dead end.

Environment variables:

- `DEALER_LISTEN_ADDR` — address the gateway's public HTTP(S) listener binds to (default: `0.0.0.0:3000`).
- `DEALER_CONFIG_PATH` — path to the config file (default: `config.yml`).
- `DEALER_CONFIG_POLL_INTERVAL` — how often the config file is checked for changes, as a Go duration (e.g. `2s`, default: `2s`).
- `DEALER_ORIGIN_TIMEOUT` — how long to wait when dialing/reading response headers from an internal service before failing with a 502, as a Go duration (default: `10s`).
- `DEALER_MAX_BODY_BYTES` — default request body size cap (in bytes) applied to every service, even ones without their own `request_size_limiting` plugin (default: `10485760`, i.e. 10 MiB).
- `DEALER_UNHEALTHY_COOLDOWN` — for services using `origin_urls`, how long an origin is skipped after a failed request before being retried, as a Go duration (default: `10s`).
- `DEALER_HEALTH_CHECK_PATH` — gateway-wide default path probed for active health checks, used by any service with a `health_check` block that doesn't set its own `path`. Empty by default: a service only gets active health checks if it (or this default) resolves to a non-empty path.
- `DEALER_HEALTH_CHECK_INTERVAL` — gateway-wide default polling interval for active health checks, as a Go duration (default: `10s`). Only relevant for services with a `health_check` block.
- `DEALER_HEALTH_CHECK_TIMEOUT` — how long to wait for a health check request to respond, as a Go duration (default: `2s`). Only relevant for services with a `health_check` block.
- `DEALER_RETRY_MAX_ATTEMPTS` — total number of attempts per request (including the first) on a transient network error, gateway-wide (default: `1`, i.e. retries disabled).
- `DEALER_RETRY_BACKOFF_BASE` — base delay between retry attempts, as a Go duration (default: `100ms`); actual delay grows exponentially with jitter added. Only relevant when `DEALER_RETRY_MAX_ATTEMPTS` is above `1`.
- `DEALER_CIRCUIT_BREAKER_THRESHOLD` — number of consecutive failures on an origin that trips its circuit breaker, gateway-wide (default: `0`, i.e. the breaker is disabled and only the reactive cooldown above applies).
- `DEALER_CIRCUIT_BREAKER_COOLDOWN` — how long a tripped breaker stays open before allowing a single trial request through, as a Go duration (default: `30s`); escalates on each further failed trial. Only relevant when `DEALER_CIRCUIT_BREAKER_THRESHOLD` is above `0`.
- `DEALER_RATELIMIT_CLUSTER_BIND_ADDR` — address the embedded SugarDB instance binds to (default: `localhost`). Only relevant when at least one `rate_limiting` plugin uses `mode: distributed`.
- `DEALER_RATELIMIT_CLUSTER_JOIN_ADDR` — `<remote-server-id>/<host>:<discovery-port>` of an existing cluster member to join, for multi-node distributed rate limiting. Leave unset for a single, standalone instance.
- `DEALER_RATELIMIT_CLUSTER_BOOTSTRAP` — set to `true` on the first node to bootstrap a new cluster (default: `false`). Not needed for a single, standalone instance.
- `DEALER_RATELIMIT_CLUSTER_SERVER_ID` — unique identifier for this node within the cluster; required by SugarDB's underlying RAFT layer once clustering (`DEALER_RATELIMIT_CLUSTER_BOOTSTRAP` or `DEALER_RATELIMIT_CLUSTER_JOIN_ADDR`) is used, and is what other nodes reference in their own `DEALER_RATELIMIT_CLUSTER_JOIN_ADDR`.
- `DEALER_RATELIMIT_CLUSTER_DISCOVERY_PORT` — port the embedded SugarDB instance uses for cluster membership gossip (default: `7946`). Only needs overriding when running more than one node on the same host.
- `DEALER_RATELIMIT_CLUSTER_DATA_DIR` — directory for the embedded SugarDB instance's data files (default: a fresh temporary directory per process, since rate-limit counters don't need to survive a restart).
- `DEALER_TRUST_REQUEST_ID` — if `true`, reuse an inbound `X-Request-Id` header instead of always generating a fresh one (default: `false`). Only enable this behind a trusted upstream that sets the header itself.
- `OTEL_EXPORTER_OTLP_ENDPOINT` — OTLP/HTTP endpoint spans are exported to (e.g. `http://localhost:4318`). Unset by default: tracing is entirely opt-in. Deliberately not `DEALER_`-prefixed — see above.
- `OTEL_SERVICE_NAME` — service name attached to exported spans (default: `dealer`). Only relevant when `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
- `DEALER_TLS_CERT_FILE` / `DEALER_TLS_KEY_FILE` — paths to a PEM certificate and private key. When both are set, the gateway terminates TLS itself on `0.0.0.0:3000` instead of serving plaintext HTTP; setting only one of the two fails fast at startup. Both files are re-read periodically so a renewed certificate is picked up without a restart. ACME/Let's Encrypt automation isn't supported — provide the cert/key pair yourself (e.g. from a reverse proxy, `certbot`, or your own CA).
- `DEALER_TLS_RELOAD_INTERVAL` — how often the cert/key files are checked for changes, as a Go duration (default: `30s`). Only relevant when TLS is enabled. The debug/pprof and metrics servers are unaffected by these variables and always serve plaintext HTTP — bind them to a local/internal interface, not the public network.
- `DEALER_DEBUG_ADDR` — if set, starts a `net/http/pprof` server on this address (e.g. `127.0.0.1:6060`), separate from the public gateway port. Disabled by default; bind it to a local/internal interface only.
- `DEALER_METRICS_ADDR` — if set, exposes a Prometheus `GET /metrics` endpoint on this address (e.g. `127.0.0.1:9090`), separate from the public gateway port. Disabled by default; bind it to a local/internal interface only. Metrics are collected regardless of whether this is set, so enabling it later doesn't lose history. Exposes `dealer_http_requests_total` (labels `service`, `method`, `status`) and `dealer_http_request_duration_seconds` (labels `service`, `method`).
- Any variable referenced by a `jwt_auth` plugin's `secret_env` (e.g. `JWT_SECRET`) must be set for that plugin to validate tokens.

Example request, assuming a service is configured as above and a valid JWT is available:

```
curl -X POST http://localhost:3000/payments \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"amount": 100}'
```

## License <a name = "license"></a>

This project is licensed under the [MIT License](LICENSE).
