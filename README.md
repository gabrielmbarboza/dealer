# Dealer

![alt text](https://github.com/gabrielmbarboza/dealer/blob/main/assets/images/dealer_logo.jpg?raw=true)

## Table of Contents

- [About](#about)
- [Getting Started](#getting_started)
- [Usage](#usage)
- [Contributing](CONTRIBUTING.md)
- [License](#license)

## About <a name = "about"></a>

This project was developed as part of my Golang studies, but the aim is to develop an Api Gateway and, along the way, learn about the language.

Dealer reads a `config.yml` file describing your internal services and forwards incoming requests to them, preserving HTTP methods, headers and body in both directions. Configuration changes are picked up automatically at runtime — no restart needed. It also supports a small plugin system that runs before a request is forwarded to its service:

- `add_header` — adds HTTP headers to the forwarded request.
- `http_log` — logs every request that hits the gateway.
- `request_size_limiting` — blocks requests whose body exceeds a configured size.
- `jwt_auth` — blocks requests without a valid JWT (accepted via header or query string).

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

The gateway listens on `0.0.0.0:3000` and reads `config.yml` from the current directory by default. Under Docker Compose, the project directory is bind-mounted read-only, so editing `config.yml` on the host is picked up by hot-reload without rebuilding the image.

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

Environment variables:

- `DEALER_CONFIG_PATH` — path to the config file (default: `config.yml`).
- `DEALER_CONFIG_POLL_INTERVAL` — how often the config file is checked for changes, as a Go duration (e.g. `2s`, default: `2s`).
- `DEALER_ORIGIN_TIMEOUT` — how long to wait when dialing/reading response headers from an internal service before failing with a 502, as a Go duration (default: `10s`).
- `DEALER_MAX_BODY_BYTES` — default request body size cap (in bytes) applied to every service, even ones without their own `request_size_limiting` plugin (default: `10485760`, i.e. 10 MiB).
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
