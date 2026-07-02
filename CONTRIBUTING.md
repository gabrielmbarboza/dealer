# Contributing

Thanks for your interest in contributing to Dealer!

## Development setup

This repository is a [Go workspace](https://go.dev/ref/mod#workspaces) (`go.work`) made up of three modules:

- `cmd/dealer` — the executable entrypoint.
- `config` — static project metadata (`ProjectInfo`).
- `gateway` — the gateway itself: config loading/hot-reload, routing, plugins and reverse proxying.

Requires Go 1.25.11 or later (see `go.work`).

Run the gateway locally:

```
go run ./cmd/dealer
```

## Running tests, vet and lint

Because this is a multi-module workspace, `go build ./...`/`go test ./...` only work from within a module's own directory, not from the repo root. Run them per module:

```
for m in cmd/dealer config gateway; do
  (cd "$m" && go build ./... && go vet ./... && go test ./... -race)
done
```

Lint with [golangci-lint](https://golangci-lint.run/) (config at `.golangci.yml`), also per module:

```
(cd gateway && golangci-lint run ./...)
```

CI (`.github/workflows/ci.yml`) runs all of the above on every push and pull request.

## Test-Driven Development

This project follows TDD (see `AGENTS.md`): write a failing test before writing implementation code, write the minimum code to make it pass, then refactor. Please follow the same flow for new contributions.

## Commit messages

Commits use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>

<body — list every change made>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `ci`. Scope is optional and should name the affected area (e.g. `gateway`, `plugin`, `docker`). The body is required and should list every meaningful change as a bullet point.

## Pull requests

- Keep PRs focused on a single change; unrelated cleanups belong in a separate PR.
- Make sure `go build`, `go vet`, `go test -race` and `golangci-lint run` all pass for every module you touched before opening the PR.
- Describe the "why" behind the change, not just the "what" — the diff already shows what changed.
