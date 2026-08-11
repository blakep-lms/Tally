---
type: derived-code-map
authority: derived-noncanonical
canonical: false
xtrace_ingest: deny
generated_by: openwiki@0.3.1
source_commit: de61c427a73a66edab4f3125e1c28cb3b578b571
---

# Tally code-map instructions

Generate a source-grounded implementation map for Tally.

## Required coverage

- Application or tool entry points and executable boundaries
- Route, command, job, event, and runtime flow hierarchy visible in tracked source
- Internal modules, services, data models, and their dependencies
- External providers and the exact integration boundary visible in code
- Security, authentication, authorization, validation, and trust boundaries visible in code
- Implemented scaffolding versus missing, stubbed, inferred, generated, historical, or planned behavior
- A small number of high-signal Mermaid diagrams for architecture, runtime flow, and data relationships

## Evidence rules

- Describe only behavior supported by tracked source at the recorded commit.
- Never call generated documentation canonical; it is always derived and replaceable.
- Source presence is not runtime verification. Use "present in tracked source; runtime unverified" unless committed test evidence proves execution.
- Label inference explicitly.
- Do not convert README, TODO, research, product language, fixtures, archives, or generated output into implementation claims.
- Do not include secret values, local environment values, client/customer data, deployment credentials, or private operational records.
- Do not change requirements, lifecycle, handoff, task state, ownership, or business decisions.
- Treat this wiki as derived and replaceable. LMS-Vault owner `silas/products/tally` remains canonical project authority.
