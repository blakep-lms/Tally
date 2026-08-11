---
type: concept
title: Core Orchestration Workflow
description: Orchestration layer coordinating synchronization, classification execution, reporting, project management, and rule testing in Tally.
tags: [core, workflow, orchestration]
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Core Orchestration Workflow

The core orchestration layer (`internal/core/core.go`) implements the business logic backing Tally's CLI commands, MCP server, and web dashboard. It acts as the central coordinator between storage (`internal/store`), capture (`internal/capture`), classification (`internal/classify`), reporting (`internal/report`), and external providers.

## Key Core Operations

1. **Synchronization (`Sync`)**:
   - Probes ActivityWatch for buckets and events.
   - Filters AFK intervals.
   - Extracts repository and URL signals.
   - Upserts events into SQLite.
2. **Classification (`Classify`)**:
   - Queries unclassified events.
   - Executes deterministic rules via the classification engine.
   - Optionally invokes the Anthropic Claude LLM fallback when requested.
   - Updates event project mappings.
3. **Project & Rule Management**:
   - Validates and persists project definitions (billable vs. internal).
   - Manages prioritized rules across fields (`app`, `title`, `url`, `repo`).
   - Supports dry-run rule testing (`TestRule`).
4. **Reporting (`Report`)**:
   - Aggregates tracked hours per project across time ranges (today, week, all-time).
   - Computes billable totals and client summaries.

## Data Consistency & Error Handling

- Core functions wrap database transactions and return structured Go errors, ensuring that partial failures during sync or classification do not corrupt state.
- Designed to be fully idempotent: running `tally sync` or `tally classify` multiple times produces identical, consistent results.

## Related Pages

- [Architecture Overview](../architecture/overview.md)
- [Database Schema & Storage](../architecture/database.md)
- [Capture & ActivityWatch Integration](../capture/activitywatch.md)
- [Classification Engine](../classification/engine.md)
- [Report Generator](../reports/generator.md)
