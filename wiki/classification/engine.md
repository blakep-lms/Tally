---
type: concept
title: Classification Engine & LLM Fallback
description: Deterministic rule engine and Anthropic Claude LLM fallback handler for classifying Tally time events.
tags: [classification, rules, llm, anthropic]
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Classification Engine & LLM Fallback

Tally classifies captured time events into projects using a two-tier approach: **ordered deterministic rules** first, followed by an optional **Anthropic Claude LLM fallback** for ambiguous events (`internal/classify/`).

## Classification Pipeline

```mermaid
graph TD
    Event[Unclassified Event] --> Rules[Deterministic Rules Engine]
    Rules -->|Match found| Assigned[Assign Project & Source: rule]
    Rules -->|No match| CheckLLM{--llm flag enabled?}
    CheckLLM -->|No| Unclassified[Leave in Unclassified Bucket]
    CheckLLM -->|Yes| LLM[Anthropic Claude LLM Fallback]
    LLM -->|Suggestion returned| AssignedLLM[Assign Project & Source: llm]
    LLM -->|No suggestion / Error| Unclassified
```

## Deterministic Rule Engine (`engine.go`)

Rules are stored in the database (`internal/store/rules.go`) and evaluated in order of priority:

1. **Target Fields**: Rules match against specific event fields:
   - `app`: Application name (e.g., `Slack`, `Xcode`)
   - `title`: Window title substring
   - `url`: Browser URL pattern
   - `repo`: Extracted git repository name
2. **Evaluation**: When `tally classify` runs, each unclassified event is tested against active rules. The first matching rule assigns the corresponding project to the event, setting `classification_source = 'rule'`.

## LLM Fallback (`llm.go`)

When running `tally classify --llm`, events that fail to match any deterministic rule are sent to Anthropic's Claude API (`github.com/anthropics/anthropic-sdk-go`):

- **Prompt Construction**: Sends the event metadata (app, title, url, repo) along with the active project list and their descriptions.
- **Structured Response**: Requests Claude to select the most appropriate project ID.
- **Assignment**: If Claude returns a valid project, the event is assigned with `classification_source = 'llm'`.
- **API Key**: Requires the `ANTHROPIC_API_KEY` environment variable.

## Interactive Triage (`-i` flag)

Running `tally classify -i` opens an interactive terminal prompt allowing human builders to triage unclassified events manually and optionally save their classification decisions as permanent rules so they never recur.

## Related Pages

- [Architecture Overview](../architecture/overview.md)
- [Database Schema & Storage](../architecture/database.md)
- [Core Workflow](../core/workflow.md)
- [CLI Overview](../cli/overview.md)
