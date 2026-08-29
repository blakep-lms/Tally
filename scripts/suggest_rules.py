#!/usr/bin/env python3
"""Suggests new bucket rules from ActivityWatch capture that is still un-bucketed.

Reads the captured events (app/title) from ~/.tally/tally.db, counts time per
(app,title) that no existing bucket rule matches, and prints the top candidates
with proposed buckets — for a human (or agent) to review and add to buckets.json.

Usage: python3 suggest_rules.py [--top N]
"""
import argparse
import json
import os
import sqlite3
from collections import defaultdict
from pathlib import Path

DB = os.path.expanduser("~/.tally/tally.db")
CONFIG = os.path.expanduser("~/.tally/buckets.json")


def load_rules() -> list[dict]:
    try:
        return json.load(open(CONFIG)).get("rules", [])
    except Exception:
        return []


def matched(rule: dict, app: str, title: str) -> bool:
    hay = f"{app} {title}".lower()
    return rule.get("pattern", "").lower() in hay


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--top", type=int, default=12)
    a = ap.parse_args()

    rules = load_rules()
    buckets = sorted({r["bucket"] for r in rules} | {"Other"})

    if not os.path.exists(DB):
        print("No capture db yet — run `tally timer on` to start collecting.")
        return
    con = sqlite3.connect(f"file:{DB}?mode=ro", uri=True)
    rows = con.execute(
        "SELECT app, title, SUM(duration) AS secs FROM events GROUP BY app, title"
    ).fetchall()
    con.close()

    # Time not captured by any existing rule.
    loose = defaultdict(float)
    for app, title, secs in rows:
        if not any(matched(r, app, title) for r in rules):
            loose[f"{app} | {title}"] += secs

    print("Existing buckets:", ", ".join(buckets))
    print(f"Top {a.top} un-bucketed windows by time:\n")
    for key, secs in sorted(loose.items(), key=lambda kv: kv[1], reverse=True)[: a.top]:
        app, title = key.split(" | ", 1)
        h = secs / 3600
        print(f"  {h:6.2f}h  {app}  |  {title[:80]}")


if __name__ == "__main__":
    main()
