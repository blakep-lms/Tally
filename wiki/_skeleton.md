---
type: skeleton
title: Tally Wiki Skeleton
description: Complete structure and page map for the Tally repository wiki.
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Tally Wiki Skeleton

This document outlines the complete documentation structure for the Tally codebase, mapping out subsystems, architecture, CLI commands, classification engine, storage layer, MCP server, web UI, and test suites.

## Page Map

1. `/openwiki/architecture/overview.md`
   - Description: High-level architectural overview, system components, core data flow from ActivityWatch capture through SQLite storage, classification, reporting, and MCP/UI serving. Includes Mermaid architecture and data flow diagrams.
2. `/openwiki/architecture/database.md`
   - Description: SQLite database schema (`schema.go`), persistence stores (`projects.go`, `rules.go`, `events.go`, `cache.go`), and data integrity invariants.
3. `/openwiki/cli/overview.md`
   - Description: Cobra CLI command hierarchy (`cmd/root.go`, `cmd/init.go`, `cmd/sync.go`, `cmd/projects.go`, `cmd/rules.go`, `cmd/classify.go`, `cmd/report.go`, `cmd/ui.go`, `cmd/mcp.go`, `cmd/status.go`), flag parsing, output formats, and JSON support.
4. `/openwiki/capture/activitywatch.md`
   - Description: ActivityWatch integration (`internal/capture/activitywatch.go`), REST client polling, bucket querying, AFK cleaning, and signal extraction (`signals.go` for git repositories and URLs).
5. `/openwiki/classification/engine.md`
   - Description: Deterministic rule engine (`internal/classify/engine.go`) and Anthropic Claude LLM fallback handler (`internal/classify/llm.go`), rule precedence, field matching (app, title, url, repo), and interactive triage.
6. `/openwiki/core/workflow.md`
   - Description: Orchestration layer (`internal/core/core.go`), coordinating synchronization, classification execution, reporting, project management, and rule testing.
7. `/openwiki/mcp/server.md`
   - Description: Model Context Protocol (MCP) server (`internal/mcp/server.go`), tools and resources exposed to AI agents over stdio, and parity with human CLI operations.
8. `/openwiki/server/dashboard.md`
   - Description: Local web server (`internal/server/server.go`) and embedded single-page dashboard (`internal/server/web/index.html`, `app.js`, `style.css`) for browser-based time triage and hours review.
9. `/openwiki/reports/generator.md`
   - Description: Per-project hours report generator (`internal/report/report.go`) supporting Markdown, CSV, and JSON output formats across weekly, daily, and custom ranges.
10. `/openwiki/testing/suite.md`
    - Description: Overview of unit and integration test coverage across capture, classification, store, core, MCP server, and web server packages.
