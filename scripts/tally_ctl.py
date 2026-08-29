#!/usr/bin/env python3
"""Tally control script — one command to turn Tally on or off on this machine.

Usage:
  python3 tally_ctl.py off    # stop everything: launchd jobs, processes, cron, servers
  python3 tally_ctl.py on     # start watcher + menu bar (launchd) and the editor server
  python3 tally_ctl.py status # show what's running
"""
import os
import subprocess
import sys
from pathlib import Path

HOME = Path.home()
AGENTS = [
    HOME / "Library" / "LaunchAgents" / "com.lms.tally-menubar.plist",
    HOME / "Library" / "LaunchAgents" / "com.lms.tally-watch.plist",
]
SCRIPTS = Path(__file__).resolve().parent
BUCKET_SRV = SCRIPTS / "bucket_server.py"
STATUS_FILE = "/tmp/tally-status.json"
CRON_JOB = "409ef87996f8"  # tally-bucket-suggestions


def sh(cmd, silent=True):
    return subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=30)


def off():
    for p in AGENTS:
        if p.exists():
            sh(f"launchctl unload {p}")
    sh("pkill -f tally_menu_v2.py; pkill -f tally_menu.py; pkill -f watch.py; pkill -f bucket_server.py; pkill -f 'tally ui'")
    sh(f"rm -f {STATUS_FILE}")
    # pause the daily suggestion cron so it doesn't fire with the stack off
    sh(f"hermes cron pause {CRON_JOB} 2>/dev/null || true")
    print("Tally OFF — launchd jobs unloaded, processes stopped, cron paused.")


def on():
    for p in AGENTS:
        if p.exists():
            sh(f"launchctl load {p}")
    # editor server as a background process
    subprocess.Popen(["python3", str(BUCKET_SRV)],
                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    sh(f"hermes cron resume {CRON_JOB} 2>/dev/null || true")
    print("Tally ON — watcher + menu bar + editor server started, cron resumed.")


def status():
    print("launchd jobs:")
    out = sh("launchctl list | grep -E 'tally|watch'").stdout.strip()
    print(" ", out or "none")
    print("processes:")
    procs = sh("pgrep -fl 'tally_menu_v2|watch.py|bucket_server'").stdout.strip()
    print(" ", procs or "none")
    print("cron:", CRON_JOB)
    sh("hermes cron list 2>/dev/null | grep -E '409ef87996f8' || echo '  (cron status unknown)'")


if __name__ == "__main__":
    cmd = sys.argv[1] if len(sys.argv) > 1 else "status"
    {"off": off, "on": on, "status": status}.get(cmd, status)()
