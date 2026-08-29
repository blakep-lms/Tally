#!/usr/bin/env python3
"""Tally V2 setup wizard: create buckets + titles, configure timer visibility.

Shows a full-screen TUI, guides the user through bucket creation, writes
~/.tally/buckets.json, and configures the menu-bar timer (on/off).

Usage: python3 tally_setup.py
"""
import json
import os
import shutil
import subprocess
import sys

CONFIG = os.path.expanduser("~/.tally/buckets.json")
WATCH = "/Users/bpmac2/LMS/scripts/watch.py"
MENU = "/Users/bpmac2/LMS/scripts/tally_menu.py"
VENV_PY = "/Users/bpmac2/.tally/menubar-venv/bin/python3"
PLIST = os.path.expanduser("~/Library/LaunchAgents/com.lms.tally-menubar.plist")

CLEAR = "\033[2J\033[H"
BOLD, DIM, GREEN, CYAN, RESET = "\033[1m", "\033[2m", "\033[32m", "\033[36m", "\033[0m"


def clear():
    sys.stdout.write(CLEAR)
    sys.stdout.flush()


def banner():
    clear()
    print(f"""{CYAN}
      ████████╗ █████╗ ██╗     ██╗     ██╗   ██╗
      ╚══██╔══╝██╔══██╗██║     ██║     ╚██╗ ██╔╝
         ██║   ███████║██║     ██║      ╚████╔╝
         ██║   ██╔══██║██║     ██║       ╚██╔╝
         ██║   ██║  ██║███████╗███████╗   ██║
         ╚═╝   ╚═╝  ╚═╝╚══════╝╚══════╝   ╚═╝{RESET}
      {BOLD}Local-first time tracking — setup wizard{RESET}
      {DIM}What you focus on, bucketed.{RESET}
""")


def ask(prompt: str, default: str = "") -> str:
    suf = f" [{default}]" if default else ""
    try:
        return input(f"  {prompt}{suf}: ").strip() or default
    except EOFError:
        return default


def yesno(prompt: str, default: bool = True) -> bool:
    suf = " [Y/n]" if default else " [y/N]"
    try:
        return input(f"  {prompt}{suf}: ").strip().lower() not in ("n", "no") if default else \
               input(f"  {prompt}{suf}: ").strip().lower() in ("y", "yes")
    except EOFError:
        return default


def load_cfg() -> dict:
    try:
        return json.load(open(CONFIG))
    except Exception:
        return {"rules": [], "default_bucket": "Other"}


def save_cfg(cfg: dict):
    os.makedirs(os.path.dirname(CONFIG), exist_ok=True)
    with open(CONFIG, "w") as f:
        json.dump(cfg, f, indent=2)


def setup_buckets(cfg: dict) -> dict:
    banner()
    print(f"  {BOLD}Step 1: Buckets{RESET}  {DIM}— the categories your time falls into.{RESET}\n")
    rules = cfg.get("rules", [])
    existing = sorted({r["bucket"] for r in rules})
    if existing:
        print(f"  Current buckets: {', '.join(existing)}")
        if yesno("  Add more?", default=False):
            existing = []
        else:
            return cfg
    print(f"  {DIM}Examples: Installation Pros · COLDBLOOD · LMS · Personal{RESET}\n")
    while True:
        name = ask("Bucket name (blank to finish)")
        if not name:
            break
        titles = ask(f"Titles/patterns for '{name}' (comma-separated, e.g. Field Ops, Housecall)", name)
        for t in [x.strip() for x in titles.split(",") if x.strip()]:
            rules.append({"pattern": t, "bucket": name})
        print(f"  {GREEN}✓{RESET} '{name}' added with {len([x for x in titles.split(',') if x.strip()])} pattern(s)\n")
    cfg["rules"] = rules
    return cfg


def setup_timer() -> None:
    banner()
    print(f"  {BOLD}Step 2: Menu-bar timer{RESET}  {DIM}— the live 'BUCKET · 00:12' counter.{RESET}\n")
    if yesno("  Show the timer in the menu bar?", default=True):
        if not os.path.exists(VENV_PY):
            print(f"\n  {DIM}Installing the timer runtime…{RESET}")
            subprocess.run(["python3", "-m", "venv", VENV_PY])
            subprocess.run([VENV_PY, "-m", "pip", "install", "--quiet", "pyobjc-framework-Cocoa"])
        # Ensure watcher + menu app run.
        for py, script in (("python3", WATCH), (VENV_PY, MENU)):
            subprocess.Popen([py, script], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print(f"  {GREEN}✓{RESET} Timer enabled — look for '{BOLD}BUCKET · 00:00{RESET}' top-right.")
    else:
        subprocess.run(["launchctl", "unload", PLIST], capture_output=True)
        print(f"  {DIM}Timer hidden.{RESET}")


def main() -> None:
    banner()
    # One-time migration of a legacy Go/AW db into the V2 store, if present.
    from store import migrate_legacy, open_db
    legacy = os.path.expanduser("~/.tally-uninstalled-*/tally.db")
    import glob
    paths = glob.glob(legacy)
    if paths:
        con = open_db()
        n = migrate_legacy(con, paths[0])
        con.close()
        if n:
            print(f"  {GREEN}✓{RESET} Migrated {n} historical events from your previous Tally install.\n")
    cfg = load_cfg()
    cfg = setup_buckets(cfg)
    save_cfg(cfg)
    setup_timer()
    banner()
    print(f"  {GREEN}✓ Setup complete.{RESET}\n")
    print(f"  Buckets: {', '.join(sorted({r['bucket'] for r in cfg['rules']}))}")
    print(f"  Config:  {CONFIG}")
    print(f"  Editor:  http://127.0.0.1:7788")
    print(f"  {DIM}The watcher picks up your new buckets immediately.{RESET}\n")


if __name__ == "__main__":
    main()
