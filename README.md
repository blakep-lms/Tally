# Tally

**Automatic, local-first time tracking for multi-project builders — and their agents.**

Tally passively captures what you're actually focused on, automatically classifies
that time into projects (billable client work vs. internal builds), and shows it in a
dashboard where you can see every project, triage anything ambiguous in a couple of
minutes, and produce a per-project hours report you trust for billing.

No timers. You never start or stop anything.

Everything a human can do at the terminal or in the dashboard, an AI agent can do over
[MCP](#agent-access-mcp) — full parity, no read-only crippling.

> Status: v1, macOS, local-only. "Tally" is a working name pending a collision check.

---

## Why

If you run several client projects and internal builds at once — dozens of tabs,
terminals, and IDE windows — you can't reliably reconstruct where your hours went.
Manual timers fail because they depend on remembering to start/stop them at every
context switch, which is exactly what heavy context-switchers don't do. Tally captures
passively instead, so your invoices are built on data, not memory.

---

## How it works

```
ActivityWatch  ──►  tally sync  ──►  events (AFK-cleaned)  ──►  tally classify  ──►  per-project hours
 (window/web/AFK)                     stored in ~/.tally/tally.db      rules + optional LLM        report / dashboard / MCP
```

Tally layers on [ActivityWatch](https://activitywatch.net/) for capture (window titles,
app names, browser URLs, and AFK/idle detection). It pulls those events, subtracts idle
time, extracts signals (git repo names from titles, domains from URLs), and classifies
each event into a project using **ordered deterministic rules first**, with an optional
**LLM fallback** for anything ambiguous. Whatever's left lands in an **Unclassified**
bucket you triage in the dashboard or the terminal.

---

## Quickstart (≤10 minutes)

```sh
# 1. Install
brew install blakep-lms/tap/tally      # or: go install github.com/blakep-lms/tally@latest

# 2. Set up — detects/points at ActivityWatch, creates ~/.tally
tally init

# 3. Define your projects
tally projects add "SecureAI Experts" --type billable --client "SecureAI"
tally projects add "InstallProsOS"    --type billable
tally projects add "Internal — Death Star" --type internal

# 4. Add a few rules (see the cookbook below)
tally rules add "secureai"     --project "SecureAI Experts" --field repo
tally rules add "installpros"  --project "InstallProsOS"    --field url
tally rules add "Slack"        --project "Internal — Death Star" --field app

# 5. Pull activity, classify it, and look
tally sync
tally classify
tally ui                                # opens the dashboard in your browser
```

You now have classified time on a dashboard. Everything after this is refinement:
triage the Unclassified queue, and every triage decision can be saved as a rule so it
never comes back.

If you don't have ActivityWatch yet, install it from
[activitywatch.net](https://activitywatch.net/), start it, and re-run `tally status` —
it will report `activitywatch connected`.

---

## CLI

Every command supports `--json` for machine consumption.

| Command | What it does |
|---|---|
| `tally init` | Create `~/.tally`, write config, check for ActivityWatch |
| `tally status` | Capture connectivity + tracked-hours snapshot |
| `tally sync` | Pull events from the capture provider (idempotent) |
| `tally projects add\|list\|done` | Create, list, and archive projects |
| `tally rules add\|list\|test\|delete` | Manage classification rules; `test` is a dry-run |
| `tally classify [--llm] [-i]` | Run rules (and optional LLM); `-i` triages interactively |
| `tally report [--week\|--today\|--all] [--format md\|csv\|json]` | Per-project hours |
| `tally ui` | Serve the local dashboard |
| `tally mcp` | Run the MCP server (stdio) for agents |

Examples:

```sh
tally report --week                     # markdown, paste straight into billing
tally report --week --format csv > week.csv
tally sync --since 2026-07-01 --until 2026-07-07
tally rules test "github.com/acme" --field url    # what would this rule catch?
tally classify --interactive            # triage the queue one event at a time
```

---

## Rule cookbook

Rules match on one of four fields — `app`, `title`, `url`, `repo` — with one of three
match kinds: `contains` (default, case-insensitive), `equals`, or `regex`. Rules are
ordered by priority (lower first); **first match wins**.

```sh
# Attribute a client's git repo to their project
tally rules add "acme-web" --project "ACME" --field repo

# Anything on the client's domain is theirs
tally rules add "acme.com" --project "ACME" --field url

# A specific desktop app is internal ops
tally rules add "Slack" --project "Internal" --field app

# A window-title keyword, using a regex, evaluated before broader rules
tally rules add "^Figma.*ACME" --project "ACME" --field title --match regex --priority 10

# Not sure what a rule will catch? Dry-run it first.
tally rules test "acme" --field repo
```

**Triage generates rules for you.** In the dashboard's Triage view (or `tally classify -i`),
assigning an event with "make a rule" ticked creates a `contains` rule from that event's
chosen field — so similar time classifies automatically from then on.

---

## Optional: LLM-assisted classification

Ambiguous window titles that no rule catches can be bucketed by an Anthropic model
instead of piling up in Unclassified. It's **off by default**, batched, and cached (the
same ambiguous signal is never sent twice).

Enable it by editing `~/.tally/config.toml`:

```toml
llm_enabled = true
llm_model = "claude-opus-4-8"
# anthropic_api_key = "sk-ant-..."   # or set ANTHROPIC_API_KEY in your environment
```

Then:

```sh
tally classify --llm
```

Your API key and the classification calls are the **only** network activity Tally ever
initiates, and only when you turn this on.

---

## Agent access (MCP)

`tally mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io) server over
stdio that exposes the full tool set with parity to the CLI: `status`, `list_projects`,
`add_project`, `mark_project_done`, `list_rules`, `add_rule`, `test_rule`, `delete_rule`,
`list_unclassified`, `assign_event`, `classify`, `sync`, and `report`.

Register it in Claude Desktop (`claude_desktop_config.json`) or Claude Code
(`~/.claude.json`):

```json
{
  "mcpServers": {
    "tally": { "command": "tally", "args": ["mcp"] }
  }
}
```

An agent can then, without a single human click: list your projects, pull this week's
hours, classify unclassified events, add a rule, mark a project done, and generate a
report — and the results match what you see in the UI.

---

## The dashboard

`tally ui` serves a local, offline dashboard (default `http://127.0.0.1:7654`):

- **Overview** — every project as a card with total hours, a billable/internal split, and
  today/week/all-time toggles.
- **Triage** — the Unclassified queue; assign in one click, optionally saving a rule.
- **Reports** — pick a range, copy Markdown or CSV.
- **Rules** — add and remove rules.

Mark a project **done** from its card: it archives, its rules deactivate so no new time
lands in it, and its hours stay in historical reports.

---

## Privacy

Tally is local-first and single-user by design.

- All capture data lives in `~/.tally/tally.db` on your machine.
- No telemetry. No accounts. No cloud sync.
- The **only** outbound network call Tally ever makes is the optional LLM classification
  you explicitly enable — and it goes to Anthropic with your own API key.

---

## Configuration

`~/.tally/config.toml`:

```toml
activitywatch_url = "http://localhost:5600"
ui_addr           = "127.0.0.1:7654"
llm_enabled       = false
llm_model         = "claude-opus-4-8"
# anthropic_api_key = "sk-ant-..."
```

Set `TALLY_HOME` to relocate the data directory (handy for testing).

---

## Build from source

```sh
git clone https://github.com/blakep-lms/tally
cd tally
make build      # -> ./tally
make test
```

Tally is a single static Go binary (pure-Go SQLite, no cgo). See
[CONTRIBUTING.md](CONTRIBUTING.md).

---

## Non-goals (v1)

Invoicing/rates, team sync, Windows/Linux, mobile, and a custom capture daemon are all
out of scope for v1 — the code stays portable, but v1 targets macOS + ActivityWatch.

## License

[MIT](LICENSE) © Blake / Linear Marketing Solutions
