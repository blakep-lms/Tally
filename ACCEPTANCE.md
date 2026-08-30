# Tally acceptance test

This procedure tests Tally without touching an existing `~/.tally` database.

## 1. Prerequisites

- Go 1.25 or newer
- ActivityWatch running locally at `http://localhost:5600` for live-capture checks
- A modern browser for dashboard checks

From the repository root:

```bash
export TALLY_HOME="$(mktemp -d)/tally"
go build -o ./dist/tally .
./dist/tally setup
./dist/tally doctor
./dist/tally status
```

`setup` is an alias for `init`. Both must create `$TALLY_HOME/config.toml` and `tally.db` with private file permissions. `doctor` must pass when ActivityWatch is running; without ActivityWatch, it must fail clearly while all non-capture tests remain valid.

## 2. Generic work items and lifecycle

```bash
./dist/tally items add "Client launch" --kind project --context ACME
./dist/tally items add "Tally" --kind product --context LMS
./dist/tally items add "Publish weekly" --kind goal
./dist/tally items add "Operations" --kind other
./dist/tally items list
./dist/tally items update 2 --description "Local-first exact time"
./dist/tally items done 3
./dist/tally items reactivate 3
```

Confirm all four kinds appear, updates preserve omitted fields, and the goal moves from active to done and back. The legacy `tally projects` command may remain available, but `tally items` is canonical.

## 3. Capture, privacy, and classification

If ActivityWatch is running:

```bash
./dist/tally sync --days 1
./dist/tally rules add "tally" --item Tally --field title --match contains
./dist/tally classify
./dist/tally report --today
```

Open `$TALLY_HOME/config.toml` and confirm these defaults:

```toml
store_url_paths = false
auto_sync_interval_seconds = 60
```

Captured browser URLs must contain only origins by default. They must not contain credentials, query strings, or fragments. Windows from ignored password/secret applications and private/incognito browser windows must not be persisted. LLM classification is disabled by default and is not required for acceptance.

The automated regression suite proves half-open range clipping, real AFK fragments, URL sanitization before persistence, privacy-safe LLM signals, completed-item guards, audited corrections, and synchronization across ActivityWatch hostname changes:

```bash
go test ./internal/capture ./internal/classify ./internal/store -count=1
go test ./internal/capture -run TestAWPullIncludesAllHostnameWindowBuckets -count=1
```

## 4. Exact reports and optional billing

Configure an optional global profile using integer minor units:

```bash
./dist/tally billing set --enabled --rate-minor 15000 --currency USD --rounding-minutes 15 --period-mode weekly
./dist/tally billing set --item Operations --enabled=false
./dist/tally billing show
./dist/tally billing show --item Operations
./dist/tally report --week
./dist/tally report --week --billing
./dist/tally report --month --billing --format csv
./dist/tally report --period custom --from 2026-07-01 --to 2026-07-31 --timezone UTC --billing --format json
```

Verify:

- Exact tracked time is unchanged when billing is enabled.
- Every nonzero billable work-item subtotal is rounded upward once for the selected period.
- Non-billable work is not rounded.
- Currency amounts use integer minor units internally.
- Mixed currencies are shown as separate totals.

Finalize and retrieve an immutable snapshot:

```bash
./dist/tally report --month --billing --finalize --label "Acceptance month"
./dist/tally billing snapshots list
./dist/tally billing snapshots show 1
```

A snapshot requires a label, valid IANA timezone, half-open range, and valid JSON payload. Later profile or classification changes must not alter its stored payload.

## 5. Secured HTTP API and dashboard

Start the server with a temporary bearer token for command-line API testing:

```bash
export TALLY_API_TOKEN=acceptance-only
./dist/tally ui --addr 127.0.0.1:7654 --no-open
```

In another terminal, retain the same `TALLY_HOME` and run:

```bash
export TALLY_API_TOKEN=acceptance-only
curl -fsS -H "Authorization: Bearer ${TALLY_API_TOKEN}" http://127.0.0.1:7654/api/status
curl -fsS -H "Authorization: Bearer ${TALLY_API_TOKEN}" http://127.0.0.1:7654/api/items
curl -fsS -X POST http://127.0.0.1:7654/api/items \
  -H "Authorization: Bearer ${TALLY_API_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"API goal","kind":"goal","context":"Acceptance"}'
```

The authorized reads and write must succeed. `/api/session` and every other `/api/*` request without the configured token or an authenticated browser session must return `401`. A write with an external `Origin` must return `403`. Starting with `--addr 0.0.0.0:7654` must fail because Tally is loopback-only.

Open `http://127.0.0.1:7654` and verify:

- Status and synchronization state render.
- Work items can be created, edited, completed, and reactivated for all four kinds.
- Unclassified events can be corrected and correction history can be viewed.
- Rules can be created and deleted.
- Weekly, biweekly, semimonthly, monthly, final, and custom reports render.
- Exact and rounded totals are visually distinct.
- Billing profiles can be configured at global, client/context, and work-item scopes.
- Snapshots can be finalized, listed, and downloaded.
- Empty, loading, validation, and failure states are understandable.
- Keyboard focus is visible; labels and controls work at narrow mobile widths.

When prompted, enter the temporary token once. The browser exchanges it for a server-issued HttpOnly session cookie and per-session CSRF token; the bearer is not stored. Browser writes use that cookie, same-origin checks, and an `X-Tally-CSRF` header. Query-string tokens are not supported.

## 6. Official MCP SDK

The MCP server uses `github.com/modelcontextprotocol/go-sdk` and stdio transport:

```bash
./dist/tally mcp
```

Use the automated official-SDK in-memory client test to verify initialization, tool discovery, tool calls, reports, billing, snapshots, and tool-level errors:

```bash
go test ./internal/mcp -count=1 -v
```

## 7. Migration and release gates

The migration suite builds a legacy v1 database, upgrades it with foreign keys enabled, and checks preservation of work-item IDs, rules, events, classification history, source data, durations, and legacy billing intent:

```bash
go test ./internal/store -run 'Migration|Legacy|MarkWorkItemDone' -count=1 -v
```

Run the complete gate:

```bash
test -z "$(gofmt -l .)"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod verify
go build ./...
git diff --check
```

Optional release tooling:

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

Inspect every archive in `dist/`, run the native binary with `--version` and `--help`, and confirm no database, config, credentials, `.env`, or other local state is packaged.
