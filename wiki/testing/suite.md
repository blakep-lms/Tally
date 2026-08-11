---
type: concept
title: Testing Suite
description: Overview of unit and integration test coverage across capture, classification, store, core, MCP server, and web server packages in Tally.
tags: [testing, tests, quality, go]
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Testing Suite

Tally maintains comprehensive test coverage across all major internal packages. Tests can be executed via `go test ./...` in the repository root.

## Test Packages

| Package | Path | Focus |
|---|---|---|
| **Capture** | `internal/capture/` | ActivityWatch REST client polling, AFK filtering, and git/URL signal extraction (`activitywatch_test.go`, `signals_test.go`). |
| **Classification** | `internal/classify/` | Deterministic rule matching against app, title, url, and repo fields (`engine_test.go`). |
| **Store** | `internal/store/` | SQLite database migrations, foreign key constraints, project CRUD, rule management, and event upserts (`store_test.go`). |
| **Core** | `internal/core/` | Orchestration workflows for sync, classification, and reporting (`core_test.go`). |
| **MCP Server** | `internal/mcp/` | MCP tool definitions, JSON-RPC communication, and agent parity (`server_test.go`). |
| **Web Server** | `internal/server/` | HTTP endpoints and embedded asset serving (`server_test.go`). |

## Running Tests

```sh
go test -v ./...
```

## Related Pages

- [Architecture Overview](../architecture/overview.md)
- [Database Schema & Storage](../architecture/database.md)
- [Capture & ActivityWatch Integration](../capture/activitywatch.md)
- [Classification Engine](../classification/engine.md)
