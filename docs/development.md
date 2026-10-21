# Local development and testing

Requirements: Go 1.26+ (`go.mod` declares it). No CGO, no external services.

```sh
git clone <this repository> && cd workout-body
cp .env.example .env            # then edit; or export variables directly
export MCP_BEARER_TOKEN=dev-token-change-me
export TRAINING_DB_PATH=./data/dev.db
go run ./cmd/training-mcp       # listens on :8080
```

Smoke test:

```sh
curl -s localhost:8080/health
curl -s -H "Authorization: Bearer $MCP_BEARER_TOKEN" localhost:8080/api/v1/config
```

## Checks

```sh
gofmt -l .                      # prints nothing when clean
go vet ./...
go test -race -count=1 ./...
```

CI (`.github/workflows/ci.yml`) runs formatting, vet, race tests, the coverage
floor on the store and a Docker build.

## Where to test what

* Engine rules and persistence → `internal/integration` (real SQLite file).
* HTTP behaviour and the OpenAPI contract → `internal/apisrv`.
* MCP tool behaviour, annotations and auth → `internal/mcpsrv`.
* Both adapters on one database (the acceptance flow) → `internal/e2e`.

## Running as an MCP server locally

* HTTP: `go run ./cmd/training-mcp` then point a client at `http://localhost:8080/mcp`
  with `Authorization: Bearer $MCP_BEARER_TOKEN`.
* stdio: `go run ./cmd/training-mcp --stdio` (no network, no token; the client
  launches the process). Logs go to stderr.

Use synthetic data only. Never commit `.env`, `*.db` or exports of real training.
