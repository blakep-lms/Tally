---
type: concept
title: Report Generator
description: Per-project hours report generator supporting Markdown, CSV, and JSON output formats across custom time ranges in Tally.
tags: [reports, billing, markdown, csv, json]
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Report Generator

The report generator (`internal/report/report.go`) aggregates tracked time events by project to produce clean, professional hours reports suitable for client billing and internal reviews.

## Report Formats

Tally supports three export formats via `tally report`:

1. **Markdown (`--format md` or default)**: Formatted tables displaying projects, billable types, client names, and total hours. Designed to be pasted straight into invoices or billing systems.
2. **CSV (`--format csv`)**: Comma-separated values for spreadsheet import and custom accounting workflows.
3. **JSON (`--format json`)**: Structured JSON for programmatic pipelines and AI agent consumption.

## Time Ranges

- `--week`: Report on the current or previous working week.
- `--today`: Report on today's activity.
- `--all`: Aggregate all historical tracked time.

## Related Pages

- [Architecture Overview](../architecture/overview.md)
- [CLI Overview](../cli/overview.md)
- [Core Workflow](../core/workflow.md)
