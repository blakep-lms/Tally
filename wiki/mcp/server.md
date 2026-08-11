---
type: concept
title: Model Context Protocol (MCP) Server
description: Model Context Protocol (MCP) server implementation and AI agent tooling for Tally.
tags: [mcp, ai, agents, stdio]
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Model Context Protocol (MCP) Server

Tally provides full operational parity for AI agents via a Model Context Protocol (MCP) server implemented in `internal/mcp/server.go`. Invoked via `tally mcp`, the server communicates over `stdio` using the MCP standard.

## Agent Capabilities

Every operation that a human can perform via the CLI or web dashboard is exposed to AI agents as MCP tools and resources:

```mermaid
graph TD
    Agent[AI Agent / LLM] -->|stdio MCP protocol| MCPServer[Tally MCP Server (internal/mcp)]
    MCPServer -->|Core Orchestration| Core[Core Layer]
    Core -->|SQLite DB| DB[(Database)]
```

### Exposed MCP Tools

1. **`tally_status`**: Retrieve capture connectivity status and tracked hours summary.
2. **`tally_sync`**: Trigger synchronization with ActivityWatch.
3. **`tally_projects_list` & `tally_projects_add`**: Manage projects.
4. **`tally_rules_list`, `tally_rules_add`, `tally_rules_test`**: Manage and test classification rules.
5. **`tally_classify`**: Run deterministic classification and optional LLM fallback on unclassified events.
6. **`tally_report`**: Generate per-project hours reports.

## Architecture & Testing

- Implemented using standard JSON-RPC over stdio.
- Extensively tested in `internal/mcp/server_test.go` to ensure agents have robust, reliable, and secure access to time-tracking data without read-only restrictions.

## Related Pages

- [Architecture Overview](../architecture/overview.md)
- [CLI Overview](../cli/overview.md)
- [Core Workflow](../core/workflow.md)
