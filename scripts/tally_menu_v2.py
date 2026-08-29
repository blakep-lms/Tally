#!/usr/bin/env python3
"""Tally V2 menu-bar timer."""
import json
import sys
import time
from typing import Any

STATUS = "/tmp/tally-status.json"
DASHBOARD = "http://127.0.0.1:7788"
TIMELINE = "file:///Users/bpmac2/LMS/scripts/timeline.html"
AppKit: Any = None


def fmt_secs(secs):
    secs = max(0, int(secs))
    m, s = divmod(secs, 60)
    return f"{m:02d}:{s:02d}"


def parse_status(raw, now=None):
    now = time.time() if now is None else now
    bucket = str(raw.get("bucket") or "Other")
    secs = int(raw.get("bucket_secs") or 0)
    if raw.get("now") is not None:
        secs += max(0, int(now - float(raw["now"])))
    return {
        "bucket": bucket,
        "secs": secs,
        "app": str(raw.get("app") or ""),
        "title": str(raw.get("title") or ""),
    }


def read_status(path=STATUS, now=None):
    try:
        with open(path) as f:
            return parse_status(json.load(f), now)
    except Exception:
        return {"bucket": "Tally", "secs": 0, "app": "", "title": "waiting for watcher"}


def make_label(state):
    return f"{state['bucket']} · {fmt_secs(state['secs'])}"


def open_url(url):
    AppKit.NSWorkspace.sharedWorkspace().openURL_(AppKit.NSURL.URLWithString_(url))


class TallyMenu:
    def __init__(self):
        self.item = AppKit.NSStatusBar.systemStatusBar().statusItemWithLength_(-1)
        self.item.button().setTitle_("Tally · 00:00")
        self.item.setMenu_(self.menu())

    def menu(self):
        menu = AppKit.NSMenu.alloc().init()
        for title, action in (
            ("Open Dashboard", "openDashboard:"),
            ("Open Timeline", "openTimeline:"),
            ("Hide Timer", "quit:"),
        ):
            mi = AppKit.NSMenuItem.alloc().initWithTitle_action_keyEquivalent_(title, action, "")
            mi.setTarget_(self)
            menu.addItem_(mi)
        menu.addItem_(AppKit.NSMenuItem.separatorItem())
        quit_item = AppKit.NSMenuItem.alloc().initWithTitle_action_keyEquivalent_("Quit", "quit:", "q")
        quit_item.setTarget_(self)
        menu.addItem_(quit_item)
        return menu

    def tick_(self, _timer):
        state = read_status()
        self.item.button().setTitle_(make_label(state))
        tip = " — ".join(x for x in (state["app"], state["title"]) if x)
        if tip:
            self.item.setToolTip_(tip)

    def openDashboard_(self, _sender):
        open_url(DASHBOARD)

    def openTimeline_(self, _sender):
        open_url(TIMELINE)

    def quit_(self, _sender):
        AppKit.NSApp.terminate_(self)

    def run(self):
        self.tick_(None)
        AppKit.NSTimer.scheduledTimerWithTimeInterval_target_selector_userInfo_repeats_(
            1.0, self, "tick:", None, True
        )
        AppKit.NSApp.run()


def self_check():
    st = parse_status({"bucket": "LMS", "bucket_secs": 61, "app": "Hermes", "title": "Tally", "now": 10}, 12)
    assert fmt_secs(0) == "00:00"
    assert fmt_secs(3599) == "59:59"
    assert st == {"bucket": "LMS", "secs": 63, "app": "Hermes", "title": "Tally"}
    assert make_label(st) == "LMS · 01:03"
    assert make_label(read_status("/no/such/file")) == "Tally · 00:00"
    return "self-check ok: parse_status, fmt_secs, make_label, fallback"


def main():
    if "--self-check" in sys.argv:
        print(self_check())
        return
    global AppKit
    import AppKit as _AppKit  # type: ignore[import-not-found]
    AppKit = _AppKit
    app = AppKit.NSApplication.sharedApplication()
    app.setActivationPolicy_(AppKit.NSApplicationActivationPolicyAccessory)
    TallyMenu().run()


if __name__ == "__main__":
    main()
