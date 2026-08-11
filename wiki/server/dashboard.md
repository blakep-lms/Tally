---
type: concept
title: Web Dashboard & Local HTTP Server
description: Local HTTP server and embedded single-page dashboard for visual time triage and hours review in Tally.
tags: [server, web, dashboard, ui]
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Web Dashboard & Local HTTP Server

Tally includes a local web server (`internal/server/server.go`) and an embedded single-page application (`internal/server/web/`) providing a visual dashboard for reviewing hours and triaging unclassified time.

## Architecture

- **HTTP Server (`server.go`)**: Serves static assets from the embedded `internal/server/web/` directory and exposes REST JSON endpoints for projects, rules, events, and reports.
- **Frontend Assets (`internal/server/web/`)**:
  - `index.html`: Dashboard layout and container.
  - `app.js`: Single-page application logic handling API communication, project views, and interactive triage.
  - `style.css`: Clean, responsive styling optimized for quick review.

```mermaid
graph TD
    Browser[Web Browser] -->|HTTP / HTML & JSON API| Server[Go HTTP Server (internal/server)]
    Server -->|Embedded assets| Assets[internal/server/web]
    Server -->|Core Workflow| Core[Core Layer]
    Core -->|SQLite DB| DB[(Database)]
```

## Launching the UI

Running `tally ui` (backed by `cmd/ui.go`) starts the local server on a configurable port and automatically opens the dashboard in the user's default browser.

## Related Pages

- [Architecture Overview](../architecture/overview.md)
- [CLI Overview](../cli/overview.md)
- [Core Workflow](../core/workflow.md)
