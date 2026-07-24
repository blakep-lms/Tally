# Contributing to Tally

Thanks for your interest. Tally is a single static Go binary with a small, layered
codebase.

## Layout

```
main.go                 entrypoint
cmd/                    cobra CLI commands (init, status, sync, projects, rules,
                        classify, report, ui, mcp)
internal/
  model/                domain types (Project, Rule, Event)
  config/               ~/.tally/config.toml loading + paths
  store/                SQLite persistence + migrations (pure-Go driver)
  capture/              capture.Provider interface, ActivityWatch client,
                        signal extraction (repo/domain)
  classify/             ordered rule engine + optional LLM fallback
  core/                 the App service — the single surface CLI/MCP/UI all call
  report/               markdown/csv/json report rendering
  server/               HTTP API + embedded dashboard SPA (web/)
  mcp/                  MCP stdio server
```

**The parity rule:** anything a human can do lives in `internal/core`. The CLI, the MCP
server, and the HTTP API are all thin adapters over `core.App`. New capabilities belong
in `core` first, then get surfaced in all three — that's what keeps human/agent parity
honest.

## Development

```sh
make build     # build ./tally
make test      # run all tests
make lint      # gofmt check + go vet
make fmt       # format
```

Tests run without ActivityWatch or macOS: the ActivityWatch client is exercised against
an `httptest` fake, and the store/core run against an in-memory SQLite database
(`store.Open(":memory:")`).

## Conventions

- Keep the binary cgo-free (pure-Go SQLite) so releases stay static.
- Every CLI command supports `--json`.
- Every core capability is reachable over MCP.
- Run `make lint && make test` before opening a PR.

## Commit / PR

Conventional-ish commit subjects are appreciated (`feat:`, `fix:`, `docs:`). CI runs
gofmt, vet, tests, and a `goreleaser check` on every PR.
