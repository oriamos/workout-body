# Contributing

Thanks for helping. This project is a small, single-user training engine; keep
changes focused.

## Ground rules

* **Business rules live in `internal/training`.** Adapters (`mcpsrv`, `apisrv`,
  `websrv`) translate protocols only. If a rule is needed by both MCP and HTTP,
  it belongs in the service.
* **Missing is not zero.** Never default an unrecorded weight, rep count, RPE or
  RIR.
* **No new services or databases** without a concrete need.
* **Update the docs and the OpenAPI spec in the same change** as the behaviour.
  `internal/apisrv`'s contract test fails if routes and spec drift.
* **Synthetic data only** in tests, fixtures and docs. No real workout history,
  tokens or personal data.
* Write documentation in English.

## Workflow

```sh
go build ./...
gofmt -l .          # must print nothing
go vet ./...
go test -race ./...
```

Add a migration as the next numbered file in
`internal/sqlitestore/migrations` (never edit an applied one). See
[docs/migrations.md](docs/migrations.md). Add tests with the change; engine
behaviour is tested against a real SQLite file in `internal/integration`.

Commit messages: imperative summary line, a short body explaining *why*.

## Licensing

By contributing you agree your contribution is licensed under the MIT license
of this repository. This project builds on
[Training MCP](https://github.com/FullFran/training-mcp) (MIT, © 2026 Francisco
Manuel Olmedo Cortés); see [NOTICE.md](NOTICE.md).
