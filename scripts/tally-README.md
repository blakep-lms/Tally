# Tally

Local-first passive time tracking for developers and solo operators. Tally watches
your active window, maps it to a **bucket** you define, shows a live timer in the
menu bar, and exports per-day hours for invoicing. No accounts, no cloud — your
data stays on your machine.

## Install

**Homebrew** (once the tap is published):

```bash
brew tap blakep-lms/tools && brew install tally
```

**Curl installer:**

```bash
curl -sSL https://tally.dev/install.sh | bash
```

Either way the first run takes you through `tally setup` — create your buckets,
and macOS asks to allow Tally to read your screen (System Events). That's the
only permission; everything else is local.

## Commands

| Command | What it does |
|---|---|
| `tally setup` | First-run wizard: create buckets + window patterns, choose timer on/off |
| `tally bucket list` | Show all buckets and their patterns |
| `tally bucket add NAME "patt1, patt2"` | Add a bucket with window-title patterns |
| `tally bucket rm NAME` | Remove a bucket |
| `tally bucket rename OLD NEW` | Rename a bucket |
| `tally bucket start NAME` | Focus on one project only — everything else counts as Other |
| `tally bucket stop` | Clear project focus |
| `tally timer on` | Start the watcher + menu-bar timer + dashboard server |
| `tally timer off` | Stop everything (also pauses the nightly suggestion job) |
| `tally status` | What's running + your current bucket and live time |
| `tally dashboard [--day YYYY-MM-DD]` | Per-bucket totals for today or any day |
| `tally export [--day D] [--format json\|csv]` | Export bucket hours for invoicing |
| `tally timeline` | Generate + open the visual day-by-day timeline |
| `tally suggest` | Show your most-used un-bucketed windows (candidate rules) |

## How it works

1. `watch.py` polls your frontmost window every ~2s and writes live state.
2. Window app+title is matched against `~/.tally/buckets.json` rules (first match wins).
3. The menu bar shows `BUCKET · MM:SS`, switching as you switch windows.
4. ActivityWatch events accumulate in `~/.tally/tally.db` — the source for dashboards, exports, and the timeline.
5. A nightly agent job proposes new bucket rules from your un-bucketed time; you approve them in the editor (`tally timer on`, then http://127.0.0.1:7788).

## Project focus

`tally bucket start "Client A"` makes Tally count only that bucket — anything
else you touch shows as `Other`, so the timer reflects exactly the project you
said you were working on. `tally bucket stop` returns to full tracking.

## Data

| What | Where |
|---|---|
| Bucket rules + focus | `~/.tally/buckets.json` (auto-backed-up on every change) |
| Raw capture | `~/.tally/tally.db` (ActivityWatch source; all tools read-only) |
| Live state | `/tmp/tally-status.json` |

## Privacy

Everything runs locally on loopback. No login, no telemetry, no cloud. The only
OS permission is System Events (reading the active window), which you grant at
setup and can revoke anytime.

## Development

The scripts live in this directory; `tally.py` is the CLI dispatcher.
`python3 tally.py <command>` works in place. All scripts have `--check`
self-checks. Run them with the bundled python (stdlib only, except the menu-bar
timer which needs the `menubar-venv` with pyobjc).
