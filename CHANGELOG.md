# Changelog

All notable changes to Tally will be documented here. The project follows Keep a Changelog and will use semantic versioning for public releases.

## [Unreleased]

### Added

- Native `tally doctor` checks for private state, SQLite integrity, safe dashboard binding, and ActivityWatch connectivity.
- `tally setup` as a discoverable alias for initialization.
- Canonical work items for projects, products, goals, and other work.
- Embedded local dashboard for overview, triage, correction history, reports, billing, rules, lifecycle management, and immutable snapshots.
- MCP server with capability parity and safety annotations.
- Inherited global, client, and work-item billing profiles.
- Authenticated bearer-to-browser-session exchange with per-session CSRF protection.
- Durable classification and correction audit history.
- Reconciliation conflict reporting across store, core, CLI, API, and dashboard.
- One-week local dogfood process manager for macOS.

### Changed

- Go is the sole canonical Tally implementation; private dogfood installs one compiled binary.
- ActivityWatch synchronization now reconciles every local hostname bucket so host renames cannot hide historical activity.
- Report aggregation now uses bounded overlap queries and a single aggregation pass per window.
- Effective billing profiles drive billing and legacy billable/internal summaries.
- Partial HTTP and MCP updates preserve omitted values while allowing explicit clears.
- Work-item reactivation restores rules disabled by completion.
- CLI, HTTP, and MCP custom report ranges use paired, half-open bounds.

### Removed

- The duplicate Python bucket tracker, PyObjC menu runtime, curl installer, and Python Homebrew formula source.

### Security

- Tally now enforces `0700` on its state directory and `0600` on its SQLite database.

- Configured API tokens gate all reads, writes, and browser-session issuance.
- Malformed JSON, invalid dates, invalid limits, invalid durations, and billing arithmetic overflow are rejected.
- ActivityWatch URLs are sanitized before persistence; sensitive applications and private windows can be excluded.
