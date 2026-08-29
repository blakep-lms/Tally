#!/usr/bin/env python3
"""Export Tally bucket totals for one day."""
import argparse
import csv
import datetime as dt
import json
import os
import sqlite3
import sys
from collections import defaultdict

TALLY_DIR = os.path.expanduser("~/.tally")
DB_PATH = os.path.join(TALLY_DIR, "tally.db")
BUCKETS_PATH = os.path.join(TALLY_DIR, "buckets.json")


def parse_day(s):
    try:
        return dt.date.fromisoformat(s)
    except ValueError as e:
        raise argparse.ArgumentTypeError("day must be YYYY-MM-DD") from e


def parse_start(value):
    if isinstance(value, (int, float)):
        return dt.datetime.fromtimestamp(value, dt.timezone.utc)
    text = str(value).strip()
    if text.replace(".", "", 1).isdigit():
        return dt.datetime.fromtimestamp(float(text), dt.timezone.utc)
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    # Normalize " +0000" / "+0000" and pad microseconds (macOS python < 3.11).
    # Offset is optional — the watcher writes bare local ISO (no tz suffix).
    text = text.replace(" UTC", "")
    import re as _re
    m = _re.match(r"^(.+?)(?:\.(\d+))?(?: ([+-]\d{4})|([+-]\d{2}:\d{2}))?$", text)
    if not m:
        raise ValueError(f"unparseable timestamp: {text!r}")
    base, frac, off4, off6 = m.groups()
    frac = (frac or "").ljust(6, "0")
    off = off6 or (off4[:3] + ":" + off4[3:] if off4 else "")
    text = f"{base}.{frac}{off}" if frac else f"{base}{off}"
    text = text.replace("T", " ", 1)
    for fmt in ("%Y-%m-%d %H:%M:%S.%f%z", "%Y-%m-%d %H:%M:%S%z",
                "%Y-%m-%d %H:%M:%S.%f", "%Y-%m-%d %H:%M:%S"):
        try:
            out = dt.datetime.strptime(text, fmt)
            break
        except ValueError:
            continue
    else:
        raise ValueError(f"unparseable timestamp: {text!r}")
    if out.tzinfo is None:
        out = out.replace(tzinfo=dt.datetime.now().astimezone().tzinfo)
    return out


def load_buckets(path=BUCKETS_PATH):
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return data.get("rules", []), data.get("default_bucket", "Other")


def bucket_for(app, title, rules, default_bucket):
    haystack = f"{app or ''} {title or ''}".lower()
    for rule in rules:
        pattern = str(rule.get("pattern", "")).lower()
        if pattern and pattern in haystack:
            return rule.get("bucket", default_bucket)
    return default_bucket


def day_window(day):
    local_tz = dt.datetime.now().astimezone().tzinfo
    start = dt.datetime.combine(day, dt.time.min, tzinfo=local_tz)
    return start, start + dt.timedelta(days=1)


def overlap_seconds(start, duration, day_start, day_end):
    end = start + dt.timedelta(seconds=float(duration or 0))
    lo = max(start.astimezone(day_start.tzinfo), day_start)
    hi = min(end.astimezone(day_start.tzinfo), day_end)
    return max(0.0, (hi - lo).total_seconds())


def compile_totals(events, rules, default_bucket, day):
    totals = defaultdict(float)
    day_start, day_end = day_window(day)
    for app, title, start, duration in events:
        seconds = overlap_seconds(parse_start(start), duration, day_start, day_end)
        if seconds:
            totals[bucket_for(app, title, rules, default_bucket)] += seconds
    return [
        {"bucket": bucket, "seconds": int(round(seconds)), "hours": round(seconds / 3600, 4)}
        for bucket, seconds in sorted(totals.items())
    ]


def read_events(db_path=DB_PATH):
    uri = "file:" + os.path.abspath(os.path.expanduser(db_path)) + "?mode=ro"
    con = sqlite3.connect(uri, uri=True)
    try:
        return con.execute("select app, title, start, duration from events").fetchall()
    finally:
        con.close()


def write_json(rows):
    print(json.dumps(rows, indent=2))


def write_csv(rows):
    writer = csv.DictWriter(sys.stdout, fieldnames=["bucket", "seconds", "hours"])
    writer.writeheader()
    writer.writerows(rows)


def self_check():
    rules = [
        {"pattern": "linear", "bucket": "Client"},
        {"pattern": "code", "bucket": "LMS"},
    ]
    events = [
        ("Code", "tally export", "2026-08-28T09:00:00-07:00", 3600),
        ("Safari", "Linear issue", "2026-08-28T10:00:00-07:00", 1800),
        ("Notes", "misc", "2026-08-28T23:30:00-07:00", 7200),
        ("Code", "other day", "2026-08-29T09:00:00-07:00", 3600),
    ]
    rows = compile_totals(events, rules, "Other", dt.date(2026, 8, 28))
    got = {r["bucket"]: r["seconds"] for r in rows}
    assert got == {"Client": 1800, "LMS": 3600, "Other": 1800}, got
    print("self-check passed")


def main(argv=None):
    p = argparse.ArgumentParser(description="Export Tally per-bucket day totals.")
    p.add_argument("--day", type=parse_day, default=dt.date.today())
    p.add_argument("--format", choices=("json", "csv"), default="json")
    p.add_argument("--check", action="store_true", help="run built-in self-check")
    args = p.parse_args(argv)

    if args.check:
        self_check()
        return 0

    rules, default_bucket = load_buckets()
    rows = compile_totals(read_events(), rules, default_bucket, args.day)
    write_csv(rows) if args.format == "csv" else write_json(rows)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
