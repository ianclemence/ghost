#!/usr/bin/env python3
"""A simulated phone: pairs with a real Pod the way the app does, then checks
what the app depends on. Run it against your own Pod as an acceptance test.

    scripts/phone-sim.py [--host 192.168.0.107] [--port 8766] [--reminder]

It uses the network path and device credentials a real phone uses (not the
loopback shortcut), so it proves the paired-device experience end to end.
Everything it creates is removed, and the simulated device is revoked at the end.

    --reminder   also set a one-minute reminder and wait for it to arrive over
                 the live connection (takes about 90 seconds)
"""
import argparse, asyncio, base64, json, re, subprocess, sys, time, urllib.error, urllib.request

ap = argparse.ArgumentParser()
ap.add_argument("--host", default="127.0.0.1")
ap.add_argument("--port", default="8766")
ap.add_argument("--reminder", action="store_true")
ap.add_argument("--ghost", default="ghost")
args = ap.parse_args()
BASE = f"http://{args.host}:{args.port}"
results = []


def check(name, ok, detail=""):
    results.append(ok)
    print(("PASS  " if ok else "FAIL  ") + name + (f"  ({detail})" if detail and not ok else ""))
    return ok


def call(method, path, body=None, dev=None, timeout=60, raw=False):
    hdr = {"Content-Type": "application/json"}
    if dev:
        hdr.update({"X-Ghost-Device-ID": dev["device_id"], "X-Ghost-Credential": dev["credential"]})
    req = urllib.request.Request(BASE + path, json.dumps(body).encode() if body is not None else None, hdr, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            data = r.read()
            return r.status, data if raw else (json.loads(data) if data else {})
    except urllib.error.HTTPError as e:
        data = e.read()
        try:
            return e.code, json.loads(data)
        except Exception:
            return e.code, {"raw": data[:200].decode(errors="replace")}


def chat(dev, text, media_items=None, session="main", timeout=180):
    body = {"request_id": f"sim-{time.time_ns()}", "content": text, "session_key": session, "channel": "mobile"}
    if media_items:
        body["media_items"] = media_items
    hdr = {"Content-Type": "application/json", "X-Client-Type": "mobile",
           "X-Ghost-Device-ID": dev["device_id"], "X-Ghost-Credential": dev["credential"]}
    req = urllib.request.Request(BASE + "/v1/chat", json.dumps(body).encode(), hdr)
    out, frames = "", []
    with urllib.request.urlopen(req, timeout=timeout) as r:
        for line in r:
            line = line.decode().rstrip("\r\n")
            if not line.startswith("data:"):
                continue
            p = line[5:].strip()
            if p == "[DONE]":
                break
            try:
                v = json.loads(p)
            except Exception:
                continue
            if isinstance(v, str):
                out += v
            else:
                frames.append(v)
    return out, frames


# 1. Pair, exactly like the app: an invitation, then /v1/pairing/complete.
inv = subprocess.run([args.ghost, "pair", "Simulated phone"], capture_output=True, text=True, timeout=60)
m = re.search(r"code\s+([0-9a-f]{64})", inv.stdout)
if not check("ghost pair mints a single-use invitation", bool(m), inv.stdout[-200:] + inv.stderr[-200:]):
    sys.exit(1)
code, dev = call("POST", "/v1/pairing/complete", {"token": m.group(1), "display_name": "Simulated phone", "platform": "sim"})
if not check("the phone pairs and receives its own credential", code == 200 and dev.get("credential"), str(dev)):
    sys.exit(1)
code, again = call("POST", "/v1/pairing/complete", {"token": m.group(1), "display_name": "x", "platform": "sim"})
check("an invitation cannot be used twice", code != 200)

try:
    # 2. Auth: the paired credential works, an anonymous request does not.
    code, h = call("GET", "/v1/health", dev=dev)
    check("paired device can reach the Pod", code == 200 and h.get("status") == "ok", str(h))
    if args.host not in ("127.0.0.1", "localhost"):
        code, _ = call("GET", "/v1/files")
        check("an anonymous LAN request is refused", code == 401, f"got {code}")

    # 3. Conversation.
    reply, frames = chat(dev, "Reply with exactly the word: pong")
    check("chat streams a reply", "pong" in reply.lower(), reply[:120])
    check("the turn says where it ran (served_by)", any(f.get("type") == "served_by" for f in frames))

    # 4. Files: send a CSV, have it read, preview it, then delete it.
    csv = b"item,cost\ncoffee,4\ntea,3\nbagel,5\n"
    reply, _ = chat(dev, "What is the total of the cost column in the attached file? Answer with just the number.",
                    [{"base64": base64.b64encode(csv).decode(), "mime_type": "text/csv", "filename": "phone-sim.csv"}],
                    session="phone-sim")
    check("an attached spreadsheet is read", "12" in reply, reply[:120])
    code, fl = call("GET", "/v1/files", dev=dev)
    mine = [f for f in fl.get("files", []) if f["name"] == "phone-sim.csv"]
    check("the file is listed as a spreadsheet", bool(mine) and mine[0]["kind"] == "spreadsheet", str(fl)[:200])
    if mine:
        fid = mine[0]["id"]
        code, pv = call("GET", f"/v1/files/{fid}/preview", dev=dev)
        check("the file previews as its text", code == 200 and pv.get("previewable") and "coffee" in pv.get("content", ""), str(pv)[:200])
        code, ct = call("GET", f"/v1/files/{fid}/content", dev=dev)
        check("the original can be opened", code == 200 and base64.b64decode(ct.get("base64", "")) == csv)
        code, _ = call("DELETE", f"/v1/files/{fid}", dev=dev)
        code2, fl2 = call("GET", "/v1/files", dev=dev)
        check("deleting removes it", code == 200 and not [f for f in fl2.get("files", []) if f["id"] == fid])

    # 5. Push registration.
    tok = "ExponentPushToken[" + "sim" * 8 + "]"
    code, _ = call("POST", "/v1/push/register", {"token": tok, "platform": "sim"}, dev=dev)
    code2, st = call("GET", "/v1/push/status", dev=dev)
    check("the phone can register a push token", code == 200 and st.get("registered", 0) >= 1, str(st))
    code, _ = call("POST", "/v1/push/register", {"token": "junk", "platform": "sim"}, dev=dev)
    check("a malformed push token is refused", code == 400)

    # 6. Live surfaces (what the browser card reads).
    code, sf = call("GET", "/v1/live/surfaces?kind=browser", dev=dev)
    check("live browser sessions can be listed", code == 200 and "surfaces" in sf, str(sf)[:120])

    # 7. Approvals endpoint.
    code, ap_ = call("GET", "/v1/permissions/requests", dev=dev)
    check("pending approvals can be read", code == 200, str(ap_)[:120])

    # 8. A reminder arrives over the live connection.
    if args.reminder:
        import websockets

        async def reminder():
            hdr = [("X-Ghost-Device-ID", dev["device_id"]), ("X-Ghost-Credential", dev["credential"])]
            uri = f"ws://{args.host}:{args.port}/v1/ws"
            async with websockets.connect(uri, additional_headers=hdr) as ws:
                await asyncio.get_running_loop().run_in_executor(
                    None, lambda: chat(dev, "Remind me in 1 minute to stretch. Just confirm briefly.", session="main"))
                deadline = time.time() + 150
                while time.time() < deadline:
                    try:
                        msg = json.loads(await asyncio.wait_for(ws.recv(), timeout=10))
                    except asyncio.TimeoutError:
                        continue
                    text = (msg.get("content") or "").lower()
                    if "stretch" in text and msg.get("metadata", {}).get("type", "assistant_message") in ("assistant_message", ""):
                        # the confirmation of the request also matches; the reminder is the later one
                        if time.time() > deadline - 100:
                            return msg
                return None

        got = asyncio.run(reminder())
        check("a due reminder reaches the phone over the live connection", bool(got))
finally:
    call("DELETE", "/v1/push/register", dev=dev)
    call("POST", "/v1/pairing/revoke", {"device_id": dev["device_id"]})
    print("simulated device revoked")

print(f"\n{sum(results)}/{len(results)} checks passed")
sys.exit(0 if all(results) else 1)
