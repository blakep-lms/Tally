#!/usr/bin/env python3
"""Tally V2 dashboard: live bucket timer + per-day tracked totals."""
import argparse
import datetime as dt
import json
import os
import sqlite3

DB = os.path.expanduser("~/.tally/tally.db")
CONFIG = os.path.expanduser("~/.tally/buckets.json")
STATUS = "/tmp/tally-status.json"

DEFAULT_CONFIG = {"rules": [], "default_bucket": "Other"}


def load_json(path, default):
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return default


def bucket_for(app, title, cfg):
    hay = f"{app or ''} {title or ''}".lower()
    for rule in cfg.get("rules", []):
        pat = rule.get("pattern", "").lower()
        if pat and pat in hay:
            return rule.get("bucket") or cfg.get("default_bucket", "Other")
    return cfg.get("default_bucket", "Other")


def day_bounds(day):
    start = dt.datetime.strptime(day, "%Y-%m-%d").replace(tzinfo=dt.datetime.now().astimezone().tzinfo)
    end = start + dt.timedelta(days=1)
    return start.timestamp(), end.timestamp()


def split_duration(start, duration, lo, hi):
    return max(0, min(start + duration, hi) - max(start, lo))


def parse_start(text):
    """Parse the two timestamp formats Tally stores: ISO '2026-08-29T00:00:00'
    and legacy '2026-07-25 05:41:13.108 +0000 UTC'. Returns epoch float."""
    t = str(text).strip().replace(" UTC", "")
    try:
        if "T" in t:
            return dt.datetime.fromisoformat(t).timestamp()
        return dt.datetime.strptime(t, "%Y-%m-%d %H:%M:%S.%f +0000").timestamp()
    except ValueError:
        try:
            return dt.datetime.strptime(t, "%Y-%m-%d %H:%M:%S +0000").timestamp()
        except ValueError:
            return 0.0


def aggregate(conn, cfg, day):
    lo, hi = day_bounds(day)
    totals = {}
    # start is TEXT in both stored formats; parse + clip overlaps in Python so
    # cross-midnight segments count correctly. Db is small (thousands of rows).
    # Legacy dbs (pre-V2) lack the bucket column — detect and fall back to rules.
    has_bucket = any(r[1] == "bucket" for r in conn.execute("PRAGMA table_info(events)"))
    cols = "app, title, start, duration, bucket" if has_bucket else "app, title, start, duration, ''"
    rows = conn.execute(f"SELECT {cols} FROM events ORDER BY start")
    for app, title, start, duration, bucket in rows:
        s = parse_start(start)
        if not s:
            continue
        secs = split_duration(s, float(duration or 0), lo, hi)
        if secs:
            b = bucket or bucket_for(app, title, cfg)  # recorded bucket wins; legacy falls back to rules
            totals[b] = totals.get(b, 0) + secs
    return totals


def live_totals(status, selected_day):
    today = dt.date.today().isoformat()
    if selected_day != today or not status:
        return {}
    bucket = status.get("bucket") or "Other"
    return {bucket: float(status.get("bucket_secs") or 0)}


def fmt(secs):
    secs = int(round(secs))
    h, rem = divmod(secs, 3600)
    m, s = divmod(rem, 60)
    return f"{h:02d}:{m:02d}:{s:02d}"


def print_dashboard(day, tracked, live, status):
    buckets = sorted(set(tracked) | set(live))
    print(f"Tally dashboard — {day}")
    if status:
        print(f"Now: {status.get('bucket', 'Other')} · {status.get('app', '')} · {status.get('title', '')}".rstrip(" ·"))
    print("\nBucket                         Live today   Tracked")
    print("-----------------------------  -----------  -----------")
    for bucket in buckets:
        print(f"{bucket[:29]:29}  {fmt(live.get(bucket, 0)):>11}  {fmt(tracked.get(bucket, 0)):>11}")
    print("-----------------------------  -----------  -----------")
    print(f"{'Overall':29}  {fmt(sum(live.values())):>11}  {fmt(sum(tracked.values())):>11}")


def check():
    day = dt.date.today().isoformat()
    lo, _ = day_bounds(day)
    cfg = {"rules": [{"pattern": "Editor", "bucket": "Work"}], "default_bucket": "Other"}
    conn = sqlite3.connect(":memory:")
    # Match production: start is TEXT, bucket column present.
    conn.execute("CREATE TABLE events (app TEXT, title TEXT, start TEXT, duration REAL, bucket TEXT)")
    conn.executemany(
        "INSERT INTO events VALUES (?, ?, ?, ?, ?)",
        [
            ("Editor", "Coding", dt.datetime.fromtimestamp(lo + 60).strftime("%Y-%m-%dT%H:%M:%S"), 120, "Work"),
            ("Browser", "Reading", dt.datetime.fromtimestamp(lo + 180).strftime("%Y-%m-%dT%H:%M:%S"), 60, None),
            ("Editor", "Cross-midnight", dt.datetime.fromtimestamp(lo - 30).strftime("%Y-%m-%dT%H:%M:%S"), 90, "Work"),
        ],
    )
    totals = aggregate(conn, cfg, day)
    assert totals == {"Work": 180.0, "Other": 60.0}, totals
    assert live_totals({"bucket": "Work", "bucket_secs": 30}, day) == {"Work": 30.0}
    assert fmt(3661) == "01:01:01"
    # parse both stored formats
    assert parse_start("2026-08-29T00:00:00") == parse_start("2026-08-29 00:00:00.000 +0000 UTC")
    print("self-check ok")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--day", default=dt.date.today().isoformat())
    ap.add_argument("--check", action="store_true")
    a = ap.parse_args()
    if a.check:
        check()
        return
    try:
        dt.date.fromisoformat(a.day)
    except ValueError:
        print(f"Invalid date {a.day!r} — use YYYY-MM-DD.")
        return
    cfg = load_json(CONFIG, DEFAULT_CONFIG)
    status = load_json(STATUS, {})
    if not os.path.exists(DB):
        print_dashboard(a.day, {}, live_totals(status, a.day), status)
        return
    conn = sqlite3.connect(f"file:{DB}?mode=ro", uri=True)
    try:
        tracked = aggregate(conn, cfg, a.day)
    finally:
        conn.close()
    print_dashboard(a.day, tracked, live_totals(status, a.day), status)


if __name__ == "__main__":
    main()
