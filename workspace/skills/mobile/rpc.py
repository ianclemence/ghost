#!/usr/bin/env python3
"""
PhoneAgent RPC client.

This talks to the PhoneAgent UI-test JSON-RPC bridge (newline-delimited JSON over TCP).

Typical usage:
  ./.agents/skills/phoneagent/scripts/rpc.py get-tree
  ./.agents/skills/phoneagent/scripts/rpc.py open-app com.apple.Preferences
  ./.agents/skills/phoneagent/scripts/rpc.py enter-text --coordinate '{{33.0, 861.0}, {364.0, 38.0}}' --text 'Display'
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import socket
import sys
import time
from typing import Any, Dict, Optional


DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 45678
DEFAULT_CONNECT_TIMEOUT_S = 5.0
DEFAULT_READ_TIMEOUT_S = 30.0
DEFAULT_MAX_BYTES = 10 * 1024 * 1024  # 10 MiB
ARTIFACT_DIR = "/tmp/phoneagent-artifacts"


def eprint(*args: object) -> None:
    print(*args, file=sys.stderr, flush=True)

def build_request(req_id: int, method: str, params: Optional[Dict[str, Any]]) -> Dict[str, Any]:
    p: Dict[str, Any] = dict(params or {})
    return {"id": req_id, "method": method, "params": p}


def _recv_one_line(sock: socket.socket, max_bytes: int) -> bytes:
    buf = bytearray()
    while True:
        if len(buf) > max_bytes:
            raise RuntimeError(f"Response exceeded max size ({max_bytes} bytes).")

        chunk = sock.recv(65536)
        if not chunk:
            break

        nl = chunk.find(b"\n")
        if nl != -1:
            buf.extend(chunk[:nl])
            break

        buf.extend(chunk)
    return bytes(buf)


def rpc_call(
    host: str,
    port: int,
    req: Dict[str, Any],
    *,
    connect_timeout_s: float,
    read_timeout_s: float,
    max_bytes: int,
) -> Dict[str, Any]:
    payload = (json.dumps(req, separators=(",", ":")) + "\n").encode("utf-8")
    try:
        sock = socket.create_connection((host, port), timeout=connect_timeout_s)
    except Exception as e:
        raise RuntimeError(f"Failed to connect to {host}:{port}: {type(e).__name__}: {e}") from e

    try:
        sock.settimeout(read_timeout_s)
        sock.sendall(payload)
        line = _recv_one_line(sock, max_bytes=max_bytes)
    finally:
        try:
            sock.close()
        except OSError:
            pass

    if not line:
        raise RuntimeError("Empty response (server closed the connection).")

    try:
        return json.loads(line.decode("utf-8"))
    except Exception as e:
        head = line[:200].decode("utf-8", errors="replace")
        raise RuntimeError(f"Invalid JSON response: {type(e).__name__}: {e}. Head={head!r}") from e


def parse_params_json(raw: Optional[str]) -> Dict[str, Any]:
    if not raw:
        return {}
    try:
        obj = json.loads(raw)
    except json.JSONDecodeError as e:
        raise SystemExit(f"--params must be valid JSON. {e}") from e
    if not isinstance(obj, dict):
        raise SystemExit("--params must be a JSON object (e.g. '{\"x\":1}').")
    return obj


def extract_tree(resp: Dict[str, Any]) -> Optional[str]:
    result = resp.get("result")
    if not isinstance(result, dict):
        return None
    tree = result.get("tree")
    if isinstance(tree, str):
        return tree
    return None


def ensure_ok(resp: Dict[str, Any]) -> None:
    if "error" in resp:
        err = resp.get("error")
        if isinstance(err, dict) and "message" in err:
            raise SystemExit(f"RPC error: {err.get('message')}")
        raise SystemExit(f"RPC error: {json.dumps(err)}")


def cmd_call(args: argparse.Namespace) -> int:
    params = parse_params_json(args.params)
    req = build_request(args.id, args.method, params)
    resp = rpc_call(
        args.host,
        args.port,
        req,
        connect_timeout_s=args.connect_timeout,
        read_timeout_s=args.read_timeout,
        max_bytes=args.max_bytes,
    )
    if args.print == "json":
        print(json.dumps(resp, indent=2 if args.pretty else None))
    elif args.print == "result":
        print(json.dumps(resp.get("result"), indent=2 if args.pretty else None))
    elif args.print == "tree":
        tree = extract_tree(resp)
        if tree is None:
            print(json.dumps(resp, indent=2 if args.pretty else None))
        else:
            print(tree)
    else:
        raise SystemExit(f"Unknown --print mode: {args.print}")

    ensure_ok(resp)
    return 0


def _tree_command(args: argparse.Namespace, req_id: int, method: str, params: Optional[Dict[str, Any]] = None) -> int:
    req = build_request(req_id, method, params)
    resp = rpc_call(
        args.host,
        args.port,
        req,
        connect_timeout_s=args.connect_timeout,
        read_timeout_s=args.read_timeout,
        max_bytes=args.max_bytes,
    )
    tree = extract_tree(resp)
    if tree is not None:
        print(tree)
    else:
        print(json.dumps(resp, indent=2 if args.pretty else None))

    ensure_ok(resp)
    return 0


def cmd_get_tree(args: argparse.Namespace) -> int:
    return _tree_command(args, req_id=args.id, method="get_tree")


def cmd_get_context(args: argparse.Namespace) -> int:
    req = build_request(args.id, "get_context", {})
    resp = rpc_call(
        args.host,
        args.port,
        req,
        connect_timeout_s=args.connect_timeout,
        read_timeout_s=args.read_timeout,
        max_bytes=args.max_bytes,
    )

    result = resp.get("result")
    if isinstance(result, dict):
        b64 = result.get("screenshot_base64")
        if isinstance(b64, str):
            os.makedirs(ARTIFACT_DIR, exist_ok=True)
            png_out = os.path.join(ARTIFACT_DIR, f"{int(time.time())}_context_{args.id}.png")
            with open(png_out, "wb") as f:
                f.write(base64.b64decode(b64))
            eprint(f"Wrote screenshot: {png_out}")

        tree = result.get("tree")
        if isinstance(tree, str):
            print(tree)
        else:
            print(json.dumps(resp, indent=2 if args.pretty else None))
    else:
        print(json.dumps(resp, indent=2 if args.pretty else None))

    ensure_ok(resp)
    return 0


def cmd_get_screen_image(args: argparse.Namespace) -> int:
    req = build_request(args.id, "get_screen_image", {})
    resp = rpc_call(
        args.host,
        args.port,
        req,
        connect_timeout_s=args.connect_timeout,
        read_timeout_s=args.read_timeout,
        max_bytes=args.max_bytes,
    )

    result = resp.get("result")
    if not isinstance(result, dict):
        print(json.dumps(resp, indent=2 if args.pretty else None))
        ensure_ok(resp)
        return 0

    b64 = result.get("screenshot_base64")
    if not isinstance(b64, str):
        raise SystemExit("RPC response missing result.screenshot_base64")

    os.makedirs(ARTIFACT_DIR, exist_ok=True)
    png_out = os.path.join(ARTIFACT_DIR, f"{int(time.time())}_screen_{args.id}.png")
    with open(png_out, "wb") as f:
        f.write(base64.b64decode(b64))
    eprint(f"Wrote screenshot: {png_out}")

    if args.print_metadata:
        meta = result.get("metadata")
        print(json.dumps(meta, indent=2 if args.pretty else None))

    ensure_ok(resp)
    return 0


def cmd_open_app(args: argparse.Namespace) -> int:
    return _tree_command(
        args,
        req_id=args.id,
        method="open_app",
        params={"bundle_identifier": args.app_identifier},
    )


def cmd_tap(args: argparse.Namespace) -> int:
    return _tree_command(args, req_id=args.id, method="tap", params={"x": args.x, "y": args.y})


def cmd_tap_element(args: argparse.Namespace) -> int:
    params: Dict[str, Any] = {"coordinate": args.coordinate}
    if args.count is not None:
        params["count"] = args.count
    if args.long_press:
        params["longPress"] = True
    return _tree_command(args, req_id=args.id, method="tap_element", params=params)


def cmd_enter_text(args: argparse.Namespace) -> int:
    return _tree_command(
        args,
        req_id=args.id,
        method="enter_text",
        params={"coordinate": args.coordinate, "text": args.text},
    )


def cmd_scroll(args: argparse.Namespace) -> int:
    return _tree_command(
        args,
        req_id=args.id,
        method="scroll",
        params={"x": args.x, "y": args.y, "distanceX": args.distance_x, "distanceY": args.distance_y},
    )


def cmd_swipe(args: argparse.Namespace) -> int:
    return _tree_command(
        args,
        req_id=args.id,
        method="swipe",
        params={"x": args.x, "y": args.y, "direction": args.direction},
    )


def cmd_stop(args: argparse.Namespace) -> int:
    req = build_request(args.id, "stop", {})
    resp = rpc_call(
        args.host,
        args.port,
        req,
        connect_timeout_s=args.connect_timeout,
        read_timeout_s=args.read_timeout,
        max_bytes=args.max_bytes,
    )
    print(json.dumps(resp, indent=2 if args.pretty else None))
    ensure_ok(resp)
    return 0


def cmd_repl(args: argparse.Namespace) -> int:
    req_id = 1
    eprint("PhoneAgent RPC REPL. Enter: <method> [<json_params_object>]. Use 'quit' to exit.")
    while True:
        try:
            line = input("> ").strip()
        except EOFError:
            break
        if not line:
            continue
        if line in ("quit", "exit"):
            break

        parts = line.split(None, 1)
        method = parts[0]
        params: Dict[str, Any] = {}
        if len(parts) == 2:
            params = parse_params_json(parts[1])

        req = build_request(req_id, method, params)
        try:
            resp = rpc_call(
                args.host,
                args.port,
                req,
                connect_timeout_s=args.connect_timeout,
                read_timeout_s=args.read_timeout,
                max_bytes=args.max_bytes,
            )
        except Exception as e:
            eprint(f"RPC failed: {e}")
            continue

        if args.print == "tree":
            tree = extract_tree(resp)
            if tree is not None:
                print(tree)
            else:
                print(json.dumps(resp, indent=2 if args.pretty else None))
        elif args.print == "result":
            print(json.dumps(resp.get("result"), indent=2 if args.pretty else None))
        else:
            print(json.dumps(resp, indent=2 if args.pretty else None))

        try:
            ensure_ok(resp)
        except SystemExit as e:
            eprint(str(e))

        req_id += 1
    return 0


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="PhoneAgent JSON-RPC client (newline-delimited JSON over TCP).")
    p.add_argument("--host", default=DEFAULT_HOST)
    p.add_argument("--port", type=int, default=DEFAULT_PORT, help=f"RPC port (default: {DEFAULT_PORT})")
    p.add_argument("--connect-timeout", type=float, default=DEFAULT_CONNECT_TIMEOUT_S)
    p.add_argument("--read-timeout", type=float, default=DEFAULT_READ_TIMEOUT_S)
    p.add_argument("--max-bytes", type=int, default=DEFAULT_MAX_BYTES)
    p.add_argument("--pretty", action="store_true", help="Pretty-print JSON outputs.")

    sp = p.add_subparsers(dest="cmd", required=True)

    call = sp.add_parser("call", help="Call an arbitrary RPC method with JSON params.")
    call.add_argument("method")
    call.add_argument("--params", default=None, help="JSON object string, e.g. '{\"x\":1}'")
    call.add_argument("--id", type=int, default=1)
    call.add_argument("--print", choices=("json", "result", "tree"), default="json")
    call.set_defaults(func=cmd_call)

    get_tree = sp.add_parser("get-tree", help="get_tree (prints tree)")
    get_tree.add_argument("--id", type=int, default=1)
    get_tree.set_defaults(func=cmd_get_tree)

    get_context = sp.add_parser("get-context", help="get_context (prints tree; writes screenshot to /tmp/phoneagent-artifacts)")
    get_context.add_argument("--id", type=int, default=1)
    get_context.set_defaults(func=cmd_get_context)

    get_img = sp.add_parser("get-screen-image", help="get_screen_image (writes screenshot to /tmp/phoneagent-artifacts)")
    get_img.add_argument("--id", type=int, default=1)
    get_img.add_argument("--print-metadata", action="store_true")
    get_img.set_defaults(func=cmd_get_screen_image)

    open_app = sp.add_parser("open-app", help="open_app <app_identifier> (iOS bundle id or Android package name; prints tree)")
    open_app.add_argument("app_identifier")
    open_app.add_argument("--id", type=int, default=1)
    open_app.set_defaults(func=cmd_open_app)

    tap = sp.add_parser("tap", help="tap x y (prints tree)")
    tap.add_argument("x", type=float)
    tap.add_argument("y", type=float)
    tap.add_argument("--id", type=int, default=1)
    tap.set_defaults(func=cmd_tap)

    tap_el = sp.add_parser("tap-element", help="tap_element (prints tree)")
    tap_el.add_argument("--coordinate", required=True, help="XCUI frame string like '{{x, y}, {w, h}}'")
    tap_el.add_argument("--count", type=int, default=None)
    tap_el.add_argument("--long-press", action="store_true")
    tap_el.add_argument("--id", type=int, default=1)
    tap_el.set_defaults(func=cmd_tap_element)

    enter = sp.add_parser("enter-text", help="enter_text (prints tree)")
    enter.add_argument("--coordinate", required=True)
    enter.add_argument("--text", required=True)
    enter.add_argument("--id", type=int, default=1)
    enter.set_defaults(func=cmd_enter_text)

    scroll = sp.add_parser("scroll", help="scroll (prints tree)")
    scroll.add_argument("x", type=float)
    scroll.add_argument("y", type=float)
    scroll.add_argument("distance_x", type=float)
    scroll.add_argument("distance_y", type=float)
    scroll.add_argument("--id", type=int, default=1)
    scroll.set_defaults(func=cmd_scroll)

    swipe = sp.add_parser("swipe", help="swipe (prints tree)")
    swipe.add_argument("x", type=float)
    swipe.add_argument("y", type=float)
    swipe.add_argument("direction", choices=("up", "down", "left", "right"))
    swipe.add_argument("--id", type=int, default=1)
    swipe.set_defaults(func=cmd_swipe)

    stop = sp.add_parser("stop", help="stop (prints JSON response)")
    stop.add_argument("--id", type=int, default=1)
    stop.set_defaults(func=cmd_stop)

    repl = sp.add_parser("repl", help="Interactive mode (one request at a time).")
    repl.add_argument("--print", choices=("json", "result", "tree"), default="tree")
    repl.set_defaults(func=cmd_repl)

    return p


def main(argv: list[str]) -> int:
    p = build_parser()
    args = p.parse_args(argv)
    try:
        return int(args.func(args))
    except BrokenPipeError:
        return 0
    except SystemExit:
        raise
    except Exception as e:
        eprint(f"RPC client error: {e}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
