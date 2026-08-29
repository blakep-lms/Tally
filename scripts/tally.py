#!/usr/bin/env python3
"""Tally — local-first passive time tracking. Single CLI entry point.

Commands:
  tally setup                 First-run wizard: buckets, patterns, timer on/off
  tally bucket list           Show all buckets + patterns
  tally bucket add NAME PATT  Add a bucket (patterns comma-separated)
  tally bucket rm NAME        Remove a bucket
  tally bucket rename OLD NEW Rename a bucket
  tally bucket start NAME     Focus on one project only (everything else = Other)
  tally bucket stop           Clear project focus
  tally timer on              Start watcher + menu-bar timer + dashboard server
  tally timer off             Stop everything (incl. the daily suggestion cron)
  tally status                What's running, current bucket, live time
  tally dashboard [--day D]   Live per-bucket dashboard
  tally export [--day D] [--format json|csv]
  tally timeline              Generate + open the visual day timeline
  tally suggest [--top N]     Show un-bucketed windows for new rules

Install: curl installer or brew (see README). No login — everything is local.
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

CONFIG = os.path.expanduser("~/.tally/buckets.json")
# Resolve the scripts dir: repo checkout (scripts/), or Homebrew (bin/ -> ../libexec).
_here = Path(__file__).resolve().parent
SCRIPTS = _here if (_here / "doctor.py").exists() else (_here.parent / "libexec")
STATUS = "/tmp/tally-status.json"


# ---------- config helpers ----------

def load_cfg() -> dict:
    try:
        return json.load(open(CONFIG))
    except Exception:
        return {"rules": [], "default_bucket": "Other"}


def save_cfg(cfg: dict) -> None:
    os.makedirs(os.path.dirname(CONFIG), exist_ok=True)
    if os.path.exists(CONFIG):
        shutil.copy2(CONFIG, f"{CONFIG}.bak-{int(time.time())}")
    with open(CONFIG, "w") as f:
        json.dump(cfg, f, indent=2)


def rules_for(cfg: dict, bucket: str) -> list:
    return [r for r in cfg.get("rules", []) if r["bucket"] == bucket]


# ---------- commands ----------

def cmd_bucket(args):
    sub = args.sub
    cfg = load_cfg()
    if sub == "list":
        if not cfg.get("rules"):
            print("No buckets yet — run `tally setup` or `tally bucket add`.")
            return
        focus = cfg.get("focus")
        print("FOCUS:", focus if focus else "(none — all buckets tracked)")
        print()
        by_bucket = {}
        for r in cfg["rules"]:
            by_bucket.setdefault(r["bucket"], []).append(r["pattern"])
        for name, pats in sorted(by_bucket.items()):
            print(f"  {name}")
            for p in pats:
                print(f"      - {p}")
    elif sub == "add":
        name = args.name
        pats = [p.strip() for p in args.patterns.split(",") if p.strip()]
        if not pats:
            print("No patterns provided — bucket not added (need at least one window-title pattern).")
            return
        for p in pats:
            cfg["rules"].append({"pattern": p, "bucket": name})
        save_cfg(cfg)
        print(f"Added bucket '{name}' with {len(pats)} pattern(s).")
    elif sub == "rm":
        before = len(rules_for(cfg, args.name))
        cfg["rules"] = [r for r in cfg["rules"] if r["bucket"] != args.name]
        if cfg.get("focus") == args.name:
            cfg.pop("focus", None)
        save_cfg(cfg)
        print(f"Removed bucket '{args.name}' ({before} pattern(s)).")
    elif sub == "rename":
        n = 0
        for r in cfg["rules"]:
            if r["bucket"] == args.old:
                r["bucket"] = args.new
                n += 1
        if cfg.get("focus") == args.old:
            cfg["focus"] = args.new
        save_cfg(cfg)
        print(f"Renamed '{args.old}' -> '{args.new}' ({n} pattern(s)).")
    elif sub == "start":
        if not rules_for(cfg, args.name):
            print(f"No bucket named '{args.name}' — see `tally bucket list`.")
            return
        cfg["focus"] = args.name
        save_cfg(cfg)
        print(f"Focus ON: only '{args.name}' counts; everything else -> Other.")
    elif sub == "stop":
        cfg.pop("focus", None)
        save_cfg(cfg)
        print("Focus cleared — all buckets tracked again.")


def cmd_setup(_args):
    run_py("tally_setup.py")


def cmd_timer(args):
    if args.mode == "on":
        run_py("tally_ctl.py", "on")
    elif args.mode == "off":
        run_py("tally_ctl.py", "off")
    else:
        run_py("tally_ctl.py", "status")


def cmd_status(_args):
    try:
        st = json.load(open(STATUS))
        print(f"Now: {st.get('bucket')} · {st.get('app')} · {st.get('bucket_secs')}s")
    except Exception:
        print("Now: (watcher not running)")
    run_py("tally_ctl.py", "status")


def cmd_dashboard(args):
    argv = ["tally_dashboard.py"]
    if args.day:
        argv.append(f"--day={args.day}")
    run_py(*argv)


def cmd_export(args):
    argv = ["tally_export.py"]
    if args.day:
        argv.append(f"--day={args.day}")
    if args.format:
        argv.append(f"--format={args.format}")
    run_py(*argv)


def cmd_timeline(_args):
    run_py("timeline.py")


def cmd_suggest(args):
    argv = ["suggest_rules.py"]
    if args.top:
        argv.append(f"--top={args.top}")
    run_py(*argv)


def cmd_doctor(_args):
    sys.exit(run_py("doctor.py"))


def run_py(script: str, *extra: str) -> int:
    return subprocess.run([sys.executable, str(SCRIPTS / script), *extra]).returncode


# ---------- parser ----------

def main() -> None:
    ap = argparse.ArgumentParser(prog="tally", description="Local-first passive time tracking.")
    sub = ap.add_subparsers(dest="cmd")

    sub.add_parser("setup", help="first-run wizard (buckets, patterns, timer)")

    b = sub.add_parser("bucket", help="manage buckets").add_subparsers(dest="sub")
    b.add_parser("list", help="show all buckets + patterns")
    ba = b.add_parser("add", help="add a bucket")
    ba.add_argument("name")
    ba.add_argument("patterns", help="comma-separated window-title patterns")
    br = b.add_parser("rm", help="remove a bucket").add_argument("name")
    brn = b.add_parser("rename", help="rename a bucket")
    brn.add_argument("old")
    brn.add_argument("new")
    bs = b.add_parser("start", help="focus on one project only").add_argument("name")
    b.add_parser("stop", help="clear project focus")

    t = sub.add_parser("timer", help="timer control").add_argument(
        "mode", choices=["on", "off", "status"])
    sub.add_parser("status", help="what's running + current bucket")
    d = sub.add_parser("dashboard", help="per-bucket dashboard")
    d.add_argument("--day")
    e = sub.add_parser("export", help="export day totals for invoicing")
    e.add_argument("--day")
    e.add_argument("--format", choices=["json", "csv"])
    sub.add_parser("timeline", help="generate + open visual timeline")
    s = sub.add_parser("suggest", help="suggest new rules from un-bucketed time")
    s.add_argument("--top", type=int)
    sub.add_parser("doctor", help="health check + fix hints")

    args = ap.parse_args()
    if not args.cmd:
        ap.print_help()
        return
    {
        "setup": cmd_setup,
        "bucket": cmd_bucket,
        "timer": cmd_timer,
        "status": cmd_status,
        "dashboard": cmd_dashboard,
        "export": cmd_export,
        "timeline": cmd_timeline,
        "suggest": cmd_suggest,
        "doctor": cmd_doctor,
    }[args.cmd](args)


if __name__ == "__main__":
    main()
