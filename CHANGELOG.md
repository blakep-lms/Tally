# Changelog

All notable changes to Tally will be documented here. The project follows Keep a Changelog and will use semantic versioning for public releases.

## [Unreleased]

### Added

- Canonical work items for projects, products, goals, and other work.
- Embedded local dashboard for overview, triage, correction history, reports, billing, rules, lifecycle management, and immutable snapshots.
- MCP server with capability parity and safety annotations.
- Inherited global, client, and work-item billing profiles.
- Authenticated bearer-to-browser-session exchange with per-session CSRF protection.
- Durable classification and correction audit history.
- Reconciliation conflict reporting across store, core, CLI, API, and dashboard.
- One-week local dogfood process manager for macOS.

### Changed

- Report aggregation now uses bounded overlap queries and a single aggregation pass per window.
- Effective billing profiles drive billing and legacy billable/internal summaries.
- Partial HTTP and MCP updates preserve omitted values while allowing explicit clears.
- Work-item reactivation restores rules disabled by completion.
- CLI, HTTP, and MCP custom report ranges use paired, half-open bounds.

### Security

- Configured API tokens gate all reads, writes, and browser-session issuance.
- Malformed JSON, invalid dates, invalid limits, invalid durations, and billing arithmetic overflow are rejected.
- ActivityWatch URLs are sanitized before persistence; sensitive applications and private windows can be excluded.
