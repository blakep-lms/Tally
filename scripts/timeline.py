#!/usr/bin/env python3
"""Timeline — a raw active-window timeline from Tally's captured ActivityWatch data.

Reads ~/.tally/tally.db (events: app, title, start, duration), merges adjacent
same-context segments, and renders one self-contained timeline.html grouped by day.
Usage: python3 scripts/timeline.py [--db PATH] [--days N] [--no-open] [--check]
"""
import argparse
import hashlib
import json
import os
import sqlite3
import sys
import webbrowser
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo

DB = os.path.expanduser("~/.tally/tally.db")
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "timeline.html")
LOCAL = ZoneInfo("America/Los_Angeles")
UTC = ZoneInfo("UTC")

# System chrome that is never real focus.
JUNK_APPS = {"loginwindow", "UserNotificationCenter", "SecurityAgent", "WindowServer", "Dock"}
MIN_SECONDS = 5  # ignore sub-5s blips

PALETTE = ["#c65f37", "#3a7ca5", "#5a9e6f", "#9a6fb0", "#d4a13e", "#c05a6e", "#4e8f8f", "#8a7a5c", "#6f7fc0", "#b07a3a"]


def parse_ts(s: str) -> datetime:
    s = s.strip().replace(" UTC", "")
    # Normalize " +0000" / "+0000" to "+00:00", pad microseconds to 6 digits.
    import re
    m = re.match(r"^(.+?)(?:\.(\d+))?(?: ([+-]\d{4})|([+-]\d{2}:\d{2}))?$", s)
    if not m:
        raise ValueError(f"unparseable timestamp: {s!r}")
    base, frac, off4, off6 = m.groups()
    frac = (frac or "").ljust(6, "0")
    off = off6 or (off4[:3] + ":" + off4[3:] if off4 else "")
    norm = f"{base}.{frac}{off}" if frac else f"{base}{off}"
    dt = datetime.strptime(norm, "%Y-%m-%d %H:%M:%S.%f%z") if frac else datetime.strptime(norm, "%Y-%m-%d %H:%M:%S%z")
    return dt.astimezone(UTC)


def load_events(db: str, days: int) -> list[dict]:
    if not os.path.exists(db):
        return []  # fresh user, no capture yet — empty timeline
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    con.row_factory = sqlite3.Row
    if days > 0:
        since = (datetime.now(UTC) - timedelta(days=days)).isoformat()
        rows = con.execute(
            "SELECT app, title, start, duration FROM events WHERE start >= ? ORDER BY start",
            (since,),
        ).fetchall()
    else:
        rows = con.execute(
            "SELECT app, title, start, duration FROM events ORDER BY start"
        ).fetchall()
    con.close()
    events = []
    for r in rows:
        app = (r["app"] or "").strip()
        if app in JUNK_APPS:
            continue
        dur = float(r["duration"] or 0)
        if dur < MIN_SECONDS:
            continue
        events.append({
            "app": app or "Unknown",
            "title": (r["title"] or "").strip() or app or "Unknown",
            "start": parse_ts(r["start"]),
            "dur": dur,
        })
    return events


def merge_segments(events: list[dict]) -> list[dict]:
    """Merge adjacent events with the same app+title into one segment."""
    segs: list[dict] = []
    for e in events:
        if segs and segs[-1]["app"] == e["app"] and segs[-1]["title"] == e["title"]:
            segs[-1]["dur"] += e["dur"]
        else:
            segs.append(dict(e))
    return segs


def app_color(app: str) -> str:
    h = int(hashlib.md5(app.encode()).hexdigest()[:6], 16)
    return PALETTE[h % len(PALETTE)]


def fmt_dur(s: float) -> str:
    if s < 60:
        return f"{int(s)}s"
    m = int(round(s / 60))
    return f"{m // 60}h {m % 60}m" if m >= 60 else f"{m}m"


def esc(s: str) -> str:
    return (s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
             .replace('"', "&quot;").replace("'", "&#39;"))


def build(db: str, days: int) -> str:
    events = merge_segments(load_events(db, days))
    days_map: dict[str, list[dict]] = {}
    for s in events:
        local = s["start"].astimezone(LOCAL)
        s["start_local"] = local
        days_map.setdefault(local.strftime("%Y-%m-%d"), []).append(s)

    # Per-day and overall app totals.
    def app_totals(segs: list[dict]) -> dict[str, float]:
        t: dict[str, float] = {}
        for s in segs:
            t[s["app"]] = t.get(s["app"], 0.0) + s["dur"]
        return t

    def totals_html(t: dict[str, float]) -> str:
        total = sum(t.values())
        if total <= 0:
            return ""
        bars = "".join(
            f'<span class="tb"><i style="background:{app_color(a)}"></i>{a} '
            f'<b>{fmt_dur(v)}</b></span>' for a, v in
            sorted(t.items(), key=lambda kv: kv[1], reverse=True)
        )
        return f'<div class="totals"><b>{fmt_dur(total)}</b> total · {bars}</div>'

    overall = totals_html(app_totals(events))

    day_html = ""
    apps = sorted({s["app"] for segs in days_map.values() for s in segs})
    legend = "".join(
        f'<span class="lg"><i style="background:{app_color(a)}"></i>{a}</span>' for a in apps
    )
    for day, segs in sorted(days_map.items(), reverse=True):
        total = sum(s["dur"] for s in segs)
        day_label = "Today" if day == datetime.now(LOCAL).strftime("%Y-%m-%d") else \
                    ("Yesterday" if day == (datetime.now(LOCAL) - timedelta(days=1)).strftime("%Y-%m-%d") else day)
        # Strip: segments positioned by time-of-day, width proportional to duration.
        strip = ""
        details = ""
        for s in segs:
            start_min = s["start_local"].hour * 60 + s["start_local"].minute
            left = start_min / (24 * 60) * 100
            width = max(0.4, s["dur"] / (24 * 60 * 60) * 100)
            tooltip = f"{s['app']} — {s['title']} ({fmt_dur(s['dur'])})"
            strip += (f'<div class="seg" style="left:{left:.2f}%;width:{width:.2f}%;'
                      f'background:{app_color(s["app"])}" title="{esc(tooltip)}"></div>')
            details += (f'<li><b>{s["start_local"].strftime("%H:%M")}</b> '
                        f'<span class="a" style="color:{app_color(s["app"])}">{esc(s["app"])}</span> '
                        f'<span class="t">{esc(s["title"])}</span>'
                        f'<span class="m">{fmt_dur(s["dur"])}</span></li>')
        day_html += f"""
<section>
  <h2>{day_label} <small>{fmt_dur(total)} tracked</small></h2>
  {totals_html(app_totals(segs))}
  <div class="strip">{strip}</div>
  <ul class="detail">{details}</ul>
</section>"""

    return f"""<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Timeline</title>
<style>
  body{{font:14px -apple-system,sans-serif;max-width:860px;margin:0 auto;padding:24px;color:#1d1c1a}}
  h1{{font-size:20px}} h2{{font-size:15px;margin:28px 0 8px}} h2 small{{color:#888;font-weight:400}}
  .legend{{display:flex;flex-wrap:wrap;gap:10px;margin:10px 0 0;color:#555}}
  .lg i{{display:inline-block;width:10px;height:10px;border-radius:2px;margin-right:5px}}
  .strip{{position:relative;height:34px;background:#f1efec;border-radius:6px;overflow:hidden}}
  .seg{{position:absolute;top:0;bottom:0;border-right:1px solid rgba(255,255,255,.7)}}
  .totals{{display:flex;flex-wrap:wrap;gap:4px 14px;margin:6px 0;color:#555;font-size:13px}}
  .totals .tb{{white-space:nowrap}} .totals i{{display:inline-block;width:9px;height:9px;border-radius:2px;margin-right:4px}}
  .detail{{list-style:none;padding:0;margin:8px 0 0;max-height:340px;overflow-y:auto}}
  li{{display:flex;gap:8px;padding:4px 0;border-bottom:1px solid #eee;align-items:baseline}}
  .a{{min-width:110px;font-weight:600}} .t{{color:#444;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}}
  .m{{color:#888;white-space:nowrap}}
</style></head><body>
<h1>Timeline</h1>
<div class="legend">{legend}</div>
{overall}
{day_html}
</body></html>"""


def check() -> None:
    """Self-check: merging and parsing behave."""
    base = datetime(2026, 8, 1, 12, 0, tzinfo=UTC)
    ev = [
        {"app": "Chrome", "title": "A", "start": base, "dur": 60},
        {"app": "Chrome", "title": "A", "start": base + timedelta(seconds=60), "dur": 60},
        {"app": "Chrome", "title": "B", "start": base + timedelta(seconds=120), "dur": 60},
    ]
    m = merge_segments(ev)
    assert len(m) == 2, f"expected 2 segments, got {len(m)}"
    assert m[0]["dur"] == 120
    assert m[1]["dur"] == 60
    assert parse_ts("2026-07-25 05:41:55.07394+00:00").tzinfo is not None
    print("timeline self-check OK")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=DB)
    ap.add_argument("--days", type=int, default=0, help="only events in last N days (0 = all capture)")
    ap.add_argument("--no-open", action="store_true")
    ap.add_argument("--check", action="store_true")
    a = ap.parse_args()
    if a.check:
        check()
        return
    html = build(a.db, a.days)
    with open(OUT, "w") as f:
        f.write(html)
    print(f"wrote {OUT} ({len(html)} bytes)")
    if not a.no_open:
        webbrowser.open(f"file://{OUT}")


if __name__ == "__main__":
    main()
