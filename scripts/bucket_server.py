#!/usr/bin/env python3
"""Serves the bucket editor at http://127.0.0.1:7788 and reads/writes ~/.tally/buckets.json.

GET  /            -> bucket-editor.html
GET  /api/buckets -> current config JSON
POST /api/buckets -> save config (writes atomically)
"""
import json
import os
import shutil
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

CONFIG = os.path.expanduser("~/.tally/buckets.json")
HTML = Path(__file__).parent / "bucket-editor.html"
PORT = 7788


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/api/buckets":
            try:
                cfg = json.load(open(CONFIG))
            except Exception:
                cfg = {"rules": [], "default_bucket": "Other"}
            return self._json(200, cfg)
        if self.path == "/":
            body = HTML.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_error(404)

    def do_POST(self):
        if self.path == "/api/buckets":
            # Reject cross-origin writes (CSRF guard): same-origin or no Origin only.
            origin = self.headers.get("Origin", "")
            if origin and not origin.rstrip("/").endswith(f"http://127.0.0.1:{PORT}"):
                return self._json(403, {"error": "cross-origin write rejected"})
            try:
                cfg = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))))
                rules = cfg.get("rules", [])
                if not isinstance(rules, list) or not rules:
                    return self._json(400, {"error": "at least one rule is required"})
                for r in rules:
                    if not r.get("pattern") or not r.get("bucket"):
                        return self._json(400, {"error": "each rule needs a pattern and bucket"})
                if os.path.exists(CONFIG):  # keep a timestamped backup before overwrite
                    shutil.copy2(CONFIG, f"{CONFIG}.bak-{int(time.time())}")
                tmp = CONFIG + ".tmp"
                with open(tmp, "w") as f:
                    json.dump(cfg, f, indent=2)
                os.replace(tmp, CONFIG)
                return self._json(200, {"ok": True})
            except Exception as e:
                return self._json(400, {"error": str(e)})
        self.send_error(404)


if __name__ == "__main__":
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
