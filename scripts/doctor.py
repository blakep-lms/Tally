#!/usr/bin/env python3
"""Tally doctor — checks the whole install and prints what's broken + how to fix it.

Usage: python3 doctor.py   (or `tally doctor`)
"""
import json
import os
import shutil
import sqlite3
import subprocess
import sys
from pathlib import Path

CONFIG = os.path.expanduser("~/.tally/buckets.json")
DB = os.path.expanduser("~/.tally/tally.db")
STATUS = "/tmp/tally-status.json"
VENV_PY = os.path.expanduser("~/.tally/menubar-venv/bin/python3")
SCRIPTS = Path(__file__).resolve().parent

OK, BAD, WARN = "✓", "✗", "!"


def check(name: str, ok: bool, hint: str = "", warn: bool = False) -> bool:
    mark = OK if ok else (WARN if warn else BAD)
    print(f"  {mark} {name}")
    if not ok and hint:
        print(f"      {hint}")
    return ok


def main() -> int:
    print("Tally doctor\n")
    bad = 0

    # 1. Python
    ok = sys.version_info >= (3, 9)
    if not check("python >= 3.9", ok, "Install a newer python (brew install python@3.12)."):
        bad += 1

    # 2. Config exists + valid
    cfg = None
    if os.path.exists(CONFIG):
        try:
            cfg = json.load(open(CONFIG))
            rules = cfg.get("rules", [])
            ok = isinstance(rules, list) and len(rules) > 0
            check("config ~/.tally/buckets.json valid", ok,
                  "Run `tally setup` to create buckets.")
            bad += not ok
        except Exception:
            check("config ~/.tally/buckets.json valid", False,
                  "Corrupt JSON — restore from a .bak file or run `tally setup`.")
            bad += 1
    else:
        check("config ~/.tally/buckets.json exists", False,
              "Run `tally setup` — it creates your buckets.")
        bad += 1

    # 3. DB readable (source of truth)
    if os.path.exists(DB):
        try:
            con = sqlite3.connect(f"file:{DB}?mode=ro", uri=True)
            n = con.execute("SELECT COUNT(*) FROM events").fetchone()[0]
            con.close()
            check(f"capture db readable ({n} events)", True)
        except Exception as e:
            check("capture db readable", False, f"Error: {e}")
            bad += 1
    else:
        check("capture db ~/.tally/tally.db exists", False,
              "The db is created by tally on first capture — run `tally timer on`.")

    # 4. Watcher running
    watcher = subprocess.run(["pgrep", "-f", "watch.py"], capture_output=True).returncode == 0
    if not check("watcher running", watcher, "Run `tally timer on`."):
        bad += 1

    # 5. Menu-bar timer
    menu = subprocess.run(["pgrep", "-f", "tally_menu_v2.py"], capture_output=True).returncode == 0
    if not check("menu-bar timer running", menu,
                 "Run `tally timer on` (needs the pyobjc venv — see next)."):
        bad += 1

    # 6. pyobjc venv for the menu bar
    has_venv = os.path.exists(VENV_PY)
    if not check("pyobjc venv present", has_venv,
                 "Install it: python3 -m venv ~/.tally/menubar-venv && "
                 "~/.tally/menubar-venv/bin/pip install pyobjc-framework-Cocoa"):
        bad += 1

    # 7. Editor server
    srv = subprocess.run(["curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
                          "--max-time", "2", "http://127.0.0.1:7788/api/buckets"],
                         capture_output=True, text=True).stdout == "200"
    if not check("bucket editor http://127.0.0.1:7788", srv,
                 "Run `tally timer on` to start it."):
        bad += 1

    # 8. System Events permission (the classic one)
    # The watcher's status file shows app=Unknown when it can't read the window.
    perm_ok = True
    hint = ""
    try:
        st = json.load(open(STATUS))
        if st.get("app") in ("Unknown", ""):
            perm_ok = False
            hint = ("The watcher can't read your active window — macOS needs "
                    "System Events access. Allow it in System Settings > "
                    "Privacy & Security > Automation, then `tally timer on`.")
    except Exception:
        perm_ok = False
        hint = "No live status yet — start the watcher first (`tally timer on`)."
    if not check("screen/System Events access", perm_ok, hint, warn=not perm_ok):
        bad += 1

    # 9. Launchd jobs
    jobs = subprocess.run(["launchctl", "list"], capture_output=True, text=True).stdout
    for label in ("com.lms.tally-watch", "com.lms.tally-menubar"):
        ok = label in jobs
        if not check(f"launchd job {label}", ok, "Run `tally timer on` (or `install_menubar.py`)."):
            bad += 1

    # 10. Bucket rules sanity: patterns are non-trivial
    if cfg and cfg.get("rules"):
        weak = [r["pattern"] for r in cfg["rules"] if len(r["pattern"]) < 3]
        if weak:
            check("bucket patterns sane", False,
                  f"Very short patterns match too much: {weak} — narrow them.", warn=True)

    print()
    if bad:
        print(f"  {BAD} {bad} problem(s) found. Fix the items above, then re-run `tally doctor`.")
        return 1
    print(f"  {OK} All checks passed — Tally is healthy.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
