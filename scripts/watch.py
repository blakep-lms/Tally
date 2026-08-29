#!/usr/bin/env python3
"""Live-window watcher: polls the frontmost macOS app/window, maps it to a bucket,
and writes the current state to a JSON file the menu-bar app reads.

Buckets are configured in ~/.tally/buckets.json (rules + default_bucket); the
watch.py config is re-read on every poll, so edits apply immediately.

Writes: /tmp/tally-status.json  {"app","title","bucket","bucket_start","bucket_secs","now"}
Usage: python3 watch.py [--interval SECS]
"""
import argparse
import json
import os
import subprocess
import sys
import time

from store import append_segment, open_db

OUT = "/tmp/tally-status.json"
CONFIG = os.path.expanduser("~/.tally/buckets.json")

DEFAULT_CONFIG = {
    "rules": [
        {"pattern": "Installation Pros", "bucket": "Installation Pros"},
        {"pattern": "Field Ops OS", "bucket": "Installation Pros"},
        {"pattern": "Housecall Pro", "bucket": "Installation Pros"},
        {"pattern": "Secure Automation", "bucket": "Installation Pros"},
        {"pattern": "COLDBLOOD", "bucket": "COLDBLOOD"},
        {"pattern": "Tattoo", "bucket": "COLDBLOOD"},
        {"pattern": "Linea", "bucket": "LMS"},
        {"pattern": "Hermes", "bucket": "LMS"},
        {"pattern": "Obsidian", "bucket": "LMS"},
        {"pattern": "GitHub", "bucket": "LMS"},
    ],
    "default_bucket": "Other",
}


def load_config() -> dict:
    try:
        with open(CONFIG) as f:
            return json.load(f)
    except Exception:
        return DEFAULT_CONFIG


def bucket_for(app: str, title: str) -> str:
    cfg = load_config()
    focus = cfg.get("focus")
    hay = f"{app} {title}".lower()
    for rule in cfg.get("rules", []):
        if rule.get("pattern", "").lower() in hay:
            # In focus mode, only the focused bucket counts; everything else is Other.
            if focus and rule["bucket"] != focus:
                return "Other"
            return rule["bucket"]
    return cfg.get("default_bucket", "Other")


def current_window() -> tuple[str, str]:
    """Returns (app, title) of the frontmost window via System Events."""
    try:
        out = subprocess.run(
            ["osascript", "-e",
             'tell application "System Events" to tell (first process whose frontmost is true) '
             'to return {name, name of first window}'],
            capture_output=True, text=True, timeout=5,
        ).stdout.strip()
        parts = [p.strip() for p in out.split(",", 1)]
        if len(parts) == 2 and parts[0] and parts[1]:
            return parts[0], parts[1]
    except Exception:
        pass
    return "Unknown", ""


def flush(con, bucket, app, title, bucket_start, now=None):
    """Persist the open segment. Called on window change and on shutdown."""
    if not bucket:
        return
    now = time.time() if now is None else now
    if now - bucket_start < 5:
        return  # sub-5s blip, not worth a row
    start_iso = time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(bucket_start))
    append_segment(con, start_iso, now - bucket_start, app, title, bucket)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--interval", type=float, default=2.0)
    a = ap.parse_args()
    cur_bucket = ""
    cur_app = ""
    cur_title = ""
    bucket_start = 0.0
    con = open_db()  # persist completed segments to ~/.tally/tally.db

    def shutdown(_sig, _frame):
        flush(con, cur_bucket, cur_app, cur_title, bucket_start)
        con.close()
        sys.exit(0)

    import signal
    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)

    while True:
        t0 = time.time()
        app, title = current_window()
        bucket = bucket_for(app, title)
        now = time.time()
        if (bucket, app, title) != (cur_bucket, cur_app, cur_title):
            # Close the previous segment if it lasted long enough to matter.
            flush(con, cur_bucket, cur_app, cur_title, bucket_start, now)
            cur_bucket, cur_app, cur_title, bucket_start = bucket, app, title, now
        state = {
            "app": app, "title": title, "bucket": cur_bucket,
            "bucket_start": bucket_start, "bucket_secs": int(now - bucket_start),
            "now": now,
        }
        tmp = OUT + ".tmp"
        with open(tmp, "w") as f:
            json.dump(state, f)
        os.replace(tmp, OUT)
        # Skip the sleep if the window read itself took longer than the interval,
        # so osascript calls never overlap.
        elapsed = time.time() - t0
        time.sleep(max(0.0, a.interval - elapsed))


if __name__ == "__main__":
    main()
