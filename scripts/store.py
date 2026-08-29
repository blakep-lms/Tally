#!/usr/bin/env python3
"""Tally data layer — local SQLite, owned by Tally, written by the watcher.

Schema (migrated from the legacy Go/AW sync db):
  events(id, start, duration, app, title, bucket, source)
  - bucket is denormalized at write time: exports are stable even if rules change.
  - start is ISO-8601 UTC (same format the legacy rows used).
  - WAL mode so the watcher (writer) and readers never block each other.

Usage:
  from store import open_db, append_segment, events_for_day
"""
import json
import os
import sqlite3

DB = os.path.expanduser("~/.tally/tally.db")
SCHEMA_VERSION = 1

DDL = """
CREATE TABLE IF NOT EXISTS events (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    start    TEXT NOT NULL,
    duration REAL NOT NULL DEFAULT 0,
    app      TEXT NOT NULL DEFAULT '',
    title    TEXT NOT NULL DEFAULT '',
    bucket   TEXT NOT NULL DEFAULT '',
    source   TEXT NOT NULL DEFAULT 'watch'
);
CREATE INDEX IF NOT EXISTS idx_events_start ON events(start);
CREATE INDEX IF NOT EXISTS idx_events_bucket ON events(bucket);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
"""


def open_db(path: str = DB) -> sqlite3.Connection:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    con = sqlite3.connect(path)
    con.execute("PRAGMA journal_mode=WAL")
    con.execute("PRAGMA synchronous=NORMAL")
    con.executescript(DDL)
    con.execute("INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', ?)",
                (str(SCHEMA_VERSION),))
    con.commit()
    return con


def append_segment(con: sqlite3.Connection, start_iso: str, duration: float,
                   app: str, title: str, bucket: str) -> None:
    """Append one completed focus segment. Called by the watcher when the
    active window's bucket changes."""
    con.execute(
        "INSERT INTO events(start, duration, app, title, bucket, source) VALUES (?,?,?,?,?,?)",
        (start_iso, round(duration, 3), app, title, bucket, "watch"),
    )
    con.commit()


def events_for_day(con: sqlite3.Connection, day: str) -> list[tuple]:
    """All events overlapping day YYYY-MM-DD (local) — same contract the
    dashboard/export already use. Uses LIKE prefix so both the legacy
    ('2026-07-25 05:41:13.108 +0000 UTC') and ISO (T) formats match."""
    return con.execute(
        "SELECT app, title, start, duration, bucket FROM events WHERE start LIKE ? ORDER BY start",
        (f"{day}%",),
    ).fetchall()


def migrate_legacy(con: sqlite3.Connection, legacy_path: str) -> int:
    """One-time import of the legacy Go/AW db events (which lack a bucket column)
    into the V2 schema. Returns rows imported. Idempotent via meta marker."""
    cur = con.execute("SELECT value FROM meta WHERE key='legacy_imported'").fetchone()
    if cur:
        return 0
    if not os.path.exists(legacy_path):
        return 0
    legacy = sqlite3.connect(f"file:{legacy_path}?mode=ro", uri=True)
    rows = legacy.execute(
        "SELECT start, duration, app, title, '' FROM events ORDER BY start"
    ).fetchall()
    legacy.close()
    con.executemany(
        "INSERT INTO events(start, duration, app, title, bucket, source) VALUES (?,?,?,?,?, 'legacy')",
        rows,
    )
    con.execute("INSERT OR REPLACE INTO meta(key, value) VALUES ('legacy_imported', ?)",
                (str(len(rows)),))
    con.commit()
    return len(rows)


if __name__ == "__main__":
    # self-check
    import tempfile
    d = tempfile.mktemp(suffix=".db")
    c = open_db(d)
    append_segment(c, "2026-08-29T10:00:00", 300.0, "Hermes", "Hermes", "LMS")
    rows = events_for_day(c, "2026-08-29")
    assert len(rows) == 1 and rows[0][4] == "LMS", f"expected 1 LMS row, got {rows}"
    rows_other = events_for_day(c, "2026-08-28")
    assert len(rows_other) == 0, f"expected 0 rows for other day, got {rows_other}"
    c.close()
    os.unlink(d)
    print("store self-check OK")
