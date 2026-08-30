# Tally

Tally is a local-first system for understanding exactly where time goes across projects, products, goals, and other work.

It captures focused activity from ActivityWatch, removes idle time without collapsing the timeline, applies deterministic rules and optional confidence-gated LLM classification, preserves correction history, and reports exact time. Optional billing profiles derive invoice-ready projections without changing source events or turning Tally into payroll, payment, tax, or accounting software.

## Product boundary

Tally has one canonical unit: a **work item**.

| Kind | Typical use |
| --- | --- |
| `project` | Client or internal project |
| `product` | Product development and operations |
| `goal` | Outcome-oriented investment |
| `other` | Work that does not fit the first three |

Work items are `active` or `done`. Completing one stops future assignments and deactivates its rules while preserving historical time and audit records. Existing project-only databases migrate automatically; legacy project commands and fields remain compatibility aliases over the same records.

Exact captured time is always authoritative. Billing is an optional projection over exact classified intervals.

## Canonical implementation

Tally has one implementation: the Go CLI in this repository. The temporary Python bucket tracker, its PyObjC menu process, curl installer, and separate Homebrew formula are retired. They used a different data model and must not be installed beside this binary.

During private dogfood, build from source and install the single binary locally. No public package, tag, or release is approved yet.

## Requirements

- Go 1.25 or newer
- ActivityWatch for automatic capture
- macOS or Linux for packaged releases

ActivityWatch remains the capture authority. Tally does not install keyloggers, take screenshots, or send activity to a hosted Tally service.

## Build and initialize

```bash
git clone https://github.com/blakep-lms/Tally.git
cd Tally
go build -o ./dist/tally .
install -m 755 ./dist/tally ~/.local/bin/tally
tally setup
tally doctor
tally status
```

`setup` is a discoverable alias for `init`. `doctor` verifies private file permissions, SQLite integrity, loopback-only dashboard binding, and ActivityWatch connectivity. Tally stores its database and configuration under `~/.tally` by default. Set `TALLY_HOME` to use another directory.

## Core workflow

```bash
# Define work
./dist/tally items add "Client launch" --kind project --context ACME
./dist/tally items add "Tally" --kind product --context LMS
./dist/tally items add "Publish weekly" --kind goal
./dist/tally items add "Operations" --kind other

# Capture and classify
./dist/tally sync --days 1
./dist/tally rules add "tally" --item Tally --field title --match contains
./dist/tally classify
./dist/tally classify --interactive

# Inspect exact time
./dist/tally report --today
./dist/tally report --week
./dist/tally report --period custom --from 2026-07-01 --to 2026-07-31 --timezone America/New_York

# Lifecycle
./dist/tally items done Tally
./dist/tally items reactivate Tally
```

Use `tally --json` for machine-readable command output.

## Dashboard

```bash
./dist/tally ui
```

The embedded dashboard provides work-item lifecycle, synchronization, triage and correction history, rules, exact reports, billing profiles, finalized snapshots, and downloads. It binds to `127.0.0.1:7654` by default, performs an immediate sync/classification pass, and repeats every 60 seconds unless configured otherwise.

Tally refuses non-loopback dashboard addresses.

## Privacy model

Privacy filtering occurs before prohibited persistence and before optional LLM transmission.

Default protections include:

- Password managers, keychain tools, and configured ignored applications are excluded.
- Private/incognito browser windows are excluded.
- URL credentials, query strings, and fragments are always removed.
- URL paths are removed by default; only the origin is retained.
- Optional LLM classification receives minimized signals, including URL host rather than the raw URL.
- LLM classification is disabled by default and requires both configuration and a user-provided API key.
- Low-confidence or invalid LLM answers remain unclassified.
- Cached classifications cannot assign completed or unavailable work items.

Configuration lives at `~/.tally/config.toml`:

```toml
activitywatch_url = "http://localhost:5600"
ui_addr = "127.0.0.1:7654"
auto_sync_interval_seconds = 60
ignored_apps = ["1Password", "Bitwarden", "KeePassXC", "Keychain Access", "Passwords", "Secrets"]
store_url_paths = false

llm_enabled = false
llm_model = "claude-opus-4-8"
llm_min_confidence = 0.80
# anthropic_api_key = "[REDACTED]"
# http_api_token = "[REDACTED]"
```

Environment fallbacks:

- `ANTHROPIC_API_KEY`
- `TALLY_API_TOKEN`
- `TALLY_HOME`

Do not commit real credentials or a populated Tally home directory.

## Exact time and ActivityWatch synchronization

Tally queries ActivityWatch with escaped bucket IDs, explicit half-open `[from,to)` boundaries, and no result limit. Bucket types are authoritative; ID prefixes are fallback compatibility only.

Partially overlapping events are clipped to the requested range. AFK periods produce real active fragments at their original timestamps rather than shortening an event from one edge. Repeated synchronization is idempotent and atomically reconciles changed fragment groups.

Rule and manual assignments produce before/after audit records. Corrections update classification, not source capture data.

## Optional billing projections

Billing profiles can be global, client/context-specific, or work-item-specific. Resolution order is:

1. Work item
2. Client/context
3. Global
4. Disabled default

```bash
./dist/tally billing set --enabled --rate-minor 15000 --currency USD --rounding-minutes 15 --period-mode weekly
./dist/tally billing set --item 2 --enabled --rate-minor 20000 --currency USD --rounding-minutes 15 --period-mode monthly
./dist/tally billing show --item 2
./dist/tally report --week --billing
```

Rates and amounts use integer minor units. Currency output is formatted without binary floating point.

For each nonzero billable work-item subtotal in the selected period:

```text
rounded_seconds = ceil(exact_seconds / increment_seconds) * increment_seconds
```

Rounding is always upward in v1 and occurs exactly once per work item per period. It never changes events, intervals, classifications, corrections, or exact report totals. Non-billable work is exact and unrounded.

Supported periods:

- Weekly
- Biweekly
- Semimonthly
- Monthly
- Final work-item period
- Custom half-open range

Timezone and daylight-saving boundaries use local calendar arithmetic, not fixed-duration assumptions.

## Reports, exports, and snapshots

```bash
./dist/tally report --month --billing --format markdown
./dist/tally report --month --billing --format csv
./dist/tally report --month --billing --format json
./dist/tally report --month --billing --finalize --label "July final"
./dist/tally billing snapshots list
./dist/tally billing snapshots show 1
```

Reports display exact and adjusted values separately. Mixed currencies remain separate totals.

Finalized snapshots are immutable stored payloads. Each snapshot records its label, effective period and half-open bounds, timezone, exact and rounded duration, increment, upward policy, rate, currency, amount, and report/classification state represented at finalization time. Later profile or classification changes do not rewrite it.

## Local HTTP API security

With no configured token, loopback reads remain frictionless and every mutation still requires an authenticated browser session plus CSRF. When `http_api_token` or `TALLY_API_TOKEN` is set, the token gates session issuance and every API read and write.

Browser writes require:

- Server-issued HttpOnly session cookie; with a configured token, the browser exchanges the bearer once to obtain it
- Same-origin `Origin` or `Referer`
- `X-Tally-CSRF` header
- `application/json`

Non-browser local clients use `Authorization: Bearer <token>` when a token is configured. Tokens in query strings are not accepted. API requests with a non-loopback host are rejected, and the CLI refuses non-loopback listeners.

Primary endpoint groups:

- `/api/status`, `/api/sync`
- `/api/items` and compatibility `/api/work-items`, `/api/projects`
- `/api/rules`, `/api/unclassified`, `/api/classify`, `/api/audit`
- `/api/report`
- `/api/billing/profile`, `/api/billing/snapshots`

## MCP

```bash
./dist/tally mcp
```

The MCP server uses the official `github.com/modelcontextprotocol/go-sdk` and stdio transport. Tools cover work items, compatibility projects, rules, unclassified triage, corrections, synchronization, reports, billing profiles, report finalization, and snapshots. The integration suite uses the SDK’s in-memory client/server transport rather than testing a custom JSON-RPC loop.

## Architecture

```text
ActivityWatch
    |
    v
privacy filter -> exact capture fragments -> SQLite source events
                                            |
                                            v
                              rules -> optional LLM -> corrections
                                            |
                                            v
                                  exact work-item reports
                                            |
                                            +-> optional billing projection
                                            |       |
                                            |       +-> immutable snapshot
                                            |
                      CLI / secured loopback API / dashboard / MCP
```

Important boundaries:

- Capture, privacy, classification, correction history, lifecycle, reporting, and billing remain separate concerns.
- SQLite is the local system of record.
- Billing never mutates source truth.
- Tally does not collect payments, calculate payroll/payables, apply taxes, perform accounting, or deliver legal invoices.

## Development and verification

```bash
test -z "$(gofmt -l .)"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod verify
go build ./...
git diff --check
```

CI runs formatting, vet, unit, race, build, and GoReleaser configuration checks on Go 1.25.

For the complete migration, privacy, API, browser, MCP, billing, and artifact acceptance procedure, see [ACCEPTANCE.md](ACCEPTANCE.md).

## Private macOS dogfood

After building Tally and installing ActivityWatch, install the dogfood helpers:

```bash
install -m 755 tally ~/.local/bin/tally
install -m 755 scripts/tally-dogfood ~/.local/bin/tally-dogfood
tally-dogfood start
```

Use `tally-dogfood status`, `sync`, `open`, `restart`, `logs`, or `stop` to operate the local stack. The dashboard binds to `127.0.0.1:7760`; obtain its one-time bearer value with `tally-dogfood token`. The token is exchanged for a 12-hour HttpOnly browser session and is not persisted by the dashboard.

The macOS helper is intentionally a private-dogfood convenience rather than a release daemon. Run `tally-dogfood start` after a reboot. Public release remains a separate acceptance decision.

## Releases and community

- [CHANGELOG.md](CHANGELOG.md) records user-visible changes.
- [SECURITY.md](SECURITY.md) explains private vulnerability reporting and Tally’s security boundary.
- [CONTRIBUTING.md](CONTRIBUTING.md) covers development contributions.
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) defines community expectations.

Release automation exists but is dormant during private dogfood. **Do not push a `v*` tag or publish a package without separate approval.** After approval, a `v*` tag runs GoReleaser and requires `HOMEBREW_TAP_GITHUB_TOKEN` with write access to `blakep-lms/homebrew-tap`. Until then, validate artifacts without publishing:

```bash
goreleaser release --snapshot --clean
```

## License

MIT. See [LICENSE](LICENSE).
