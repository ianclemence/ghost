"""Ghost Sting sidecar: loopback-only offline tool-router daemon.

Sting is Ghost's own intent router. Today the engine underneath is the
Needle runtime by Cactus Compute, Inc. (Apache-2.0 — see
THIRD-PARTY-NOTICES.md); the sidecar protocol is Ghost's, so the engine
can be swapped without touching the Go side. The model proposes tool
calls; Ghost's Permission Broker still allows/asks/denies and runtime
evidence still decides success. The sidecar holds no authority, no
memory, no secrets.

Endpoints:
  GET  /health    -> {"ok": true} (also warms the engine + base weights)
  POST /complete  -> {"system","query","tools"[, "weights"]}
                     returns the engine envelope verbatim:
                     {type, function_calls, reasoning, confidence, ...}

Conversation policy: single-turn routing. Each request reuses a cached
agent per toolset fingerprint and calls reset() first, so no cross-turn
state leaks. Ghost owns sessions, memory, and history.

Run:
  NEEDLE_TELEMETRY=0 python3 sting_sidecar.py [--port 11436]

Air-gapped: prefetch once on a connected machine, then copy the cache:
  needle fetch            # engine -> ~/.cache/cactus-needle/v2/...
  python3 -c "import needle; needle.Needle(tools=[])"  # base weights
  HF_HUB_OFFLINE=1 NEEDLE_TELEMETRY=0 python3 sting_sidecar.py
"""

import hashlib
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

os.environ.setdefault("NEEDLE_TELEMETRY", "0")

try:
    import needle  # upstream runtime (Cactus Compute, Apache-2.0); see THIRD-PARTY-NOTICES.md
except ImportError:
    print("sting-sidecar: pip install -r requirements.txt first", file=sys.stderr)
    sys.exit(2)

PORT = int(os.environ.get("STING_PORT", os.environ.get("NEEDLE_PORT", "11436")))
for i, arg in enumerate(sys.argv[1:]):
    if arg == "--port" and i + 2 <= len(sys.argv[1:]):
        PORT = int(sys.argv[2 + i])

_agents = {}


def fingerprint(tools, weights, system):
    blob = json.dumps({"tools": tools, "weights": weights or "", "system": system or ""}, sort_keys=True, default=str)
    return hashlib.sha256(blob.encode()).hexdigest()[:16]


def get_agent(tools, weights, system):
    key = fingerprint(tools, weights, system)
    agent = _agents.get(key)
    if agent is None:
        kwargs = {"tools": tools, "system": system or ""}
        if weights:
            kwargs["weights"] = weights
        agent = needle.Needle(**kwargs)
        _agents[key] = agent
    agent.reset()
    return agent


class Handler(BaseHTTPRequestHandler):
    server_version = "ghost-sting/1"

    def log_message(self, *args):
        pass  # journald gets enough noise already

    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if urlparse(self.path).path != "/health":
            return self._send(404, {"ok": False})
        try:
            get_agent([], None, "")  # warms engine + base weights on first hit
            return self._send(200, {"ok": True})
        except Exception as exc:  # noqa: BLE001 - health must report, not raise
            return self._send(503, {"ok": False, "error": str(exc)[:200]})

    def do_POST(self):
        if urlparse(self.path).path != "/complete":
            return self._send(404, {"ok": False})
        try:
            length = int(self.headers.get("Content-Length") or 0)
            req = json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, OSError):
            return self._send(400, {"ok": False, "error": "invalid JSON"})
        query = req.get("query") or ""
        tools = req.get("tools") or []
        system = req.get("system") or ""
        weights = req.get("weights") or None
        if not tools:
            return self._send(400, {"ok": False, "error": "no tools"})
        try:
            agent = get_agent(tools, weights, system)
            raw = agent.complete(query)
            return self._send(200, raw)
        except Exception as exc:  # noqa: BLE001 - engine errors escalate, not 500s
            return self._send(200, {
                "type": "error", "function_calls": [],
                "error": str(exc)[:300], "error_code": "sidecar_failure",
            })


if __name__ == "__main__":
    # Loopback only by construction: never bind LAN. Ghost talks to this
    # daemon; nothing else should.
    server = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print(f"sting-sidecar: listening on 127.0.0.1:{PORT}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
