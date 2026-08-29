#!/usr/bin/env python3
"""Auto-start script for the Tally menu-bar timer: installs a Login Item that runs
the watcher + menu-bar app at login. Run once (or re-run after paths change).

Usage: python3 install_menubar.py
"""
import os
import plistlib
import shutil
import subprocess
import sys
from pathlib import Path

HOME = Path.home()
AGENT_DIR = HOME / "Library" / "LaunchAgents"
AGENT = AGENT_DIR / "com.lms.tally-menubar.plist"
SCRIPTS = Path(__file__).resolve().parent
VENV_PY = Path.home() / ".tally" / "menubar-venv" / "bin" / "python3"


def main() -> None:
    if not VENV_PY.exists():
        sys.exit(f"venv python missing: {VENV_PY} — run: /opt/homebrew/bin/python3 -m venv ~/.tally/menubar-venv && ~/.tally/menubar-venv/bin/pip install pyobjc-framework-Cocoa")
    AGENT_DIR.mkdir(parents=True, exist_ok=True)
    # Watcher runs as its own launchd job so menu crashes don't kill capture.
    watch_plist = {
        "Label": "com.lms.tally-watch",
        "ProgramArguments": ["python3", str(SCRIPTS / "watch.py")],
        "RunAtLoad": True,
        "KeepAlive": True,
        "StandardOutPath": str(HOME / ".tally" / "watch.log"),
        "StandardErrorPath": str(HOME / ".tally" / "watch.err.log"),
    }
    watch_agent = AGENT_DIR / "com.lms.tally-watch.plist"
    with open(watch_agent, "wb") as f:
        plistlib.dump(watch_plist, f)
    subprocess.run(["launchctl", "unload", str(watch_agent)], capture_output=True)
    subprocess.run(["launchctl", "load", str(watch_agent)], check=True)

    plist = {
        "Label": "com.lms.tally-menubar",
        "ProgramArguments": [str(VENV_PY), str(SCRIPTS / "tally_menu_v2.py")],
        "RunAtLoad": True,
        "KeepAlive": True,
        "StandardOutPath": str(HOME / ".tally" / "menubar.log"),
        "StandardErrorPath": str(HOME / ".tally" / "menubar.err.log"),
    }
    with open(AGENT, "wb") as f:
        plistlib.dump(plist, f)
    subprocess.run(["launchctl", "unload", str(AGENT)], capture_output=True)
    subprocess.run(["launchctl", "load", str(AGENT)], check=True)
    print(f"installed + loaded {AGENT}")


if __name__ == "__main__":
    main()
