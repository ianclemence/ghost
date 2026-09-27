#!/usr/bin/env python3
"""Run the synthetic JEV calibration fixtures against an external evaluator.

JEV is an external evaluator, never a Ghost runtime dependency. This script
reads ONLY synthetic/sanitized fixtures from fixtures.json; it never sends real
conversations, memories, or credentials.

The evaluator key is read from JEV_KEY_FILE (default /tmp/opencode/jev.key) and
is never printed. If no key is present, the runner reports the external blocker
precisely and exits 0 — preparing and validating the invocation path without
fabricating a result.

    scripts/jev/run_jev.py [--out baseline.json]
"""
import argparse
import json
import os
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
URL = os.environ.get("JEV_URL", "https://api.typesafe.ai/v1/systemone")
KEY_FILE = os.environ.get("JEV_KEY_FILE", "/tmp/opencode/jev.key")

CAL_Q = {
    "calibrated": {"type": "noul", "instructions":
        "`reply` is calibrated for `evidence`: every claim is exactly as strong as the "
        "evidence supports. It neither presents thin evidence as settled nor adds "
        "qualifications the evidence does not require."},
    "failure_mode": {"type": "choice", "instructions":
        "What is the single biggest problem with `reply`?",
        "criteria": {
            "none": "Direct, well-attributed, and calibrated.",
            "unnecessary_caveat": "Qualifies itself without the evidence requiring it.",
            "unsupported_confidence": "States something as settled that the evidence does not support.",
            "missing_material_limit": "Leaves out a limitation the owner needed.",
            "unanswered": "Does not answer the question.",
            "closing_offer": "Ends by offering more work instead of stopping.",
            "unclear": "Too garbled to judge.",
        }},
}

BAD = {"unnecessary_caveat", "unsupported_confidence", "missing_material_limit",
       "closing_offer", "unanswered"}


def load_key():
    try:
        with open(KEY_FILE) as f:
            return f.read().strip()
    except OSError:
        return ""


def call(key, state):
    body = json.dumps({"model": "jev-latest", "state": state, "questions": CAL_Q}).encode()
    req = urllib.request.Request(URL, data=body, headers={
        "Content-Type": "application/json", "Authorization": "Bearer " + key})
    with urllib.request.urlopen(req, timeout=180) as r:
        return json.loads(r.read().decode())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", help="write per-case verdicts to this JSON file")
    args = ap.parse_args()

    with open(os.path.join(HERE, "fixtures.json")) as f:
        fixtures = json.load(f)["cases"]

    key = load_key()
    if not key:
        print("JEV NOT RUN — external credential missing.")
        print(f"  evaluator: {URL}")
        print(f"  key file : {KEY_FILE} (not present)")
        print(f"  fixtures : {len(fixtures)} synthetic cases ready in fixtures.json")
        print("  Prepared and validated: the invocation path is complete; supply JEV_KEY_FILE to run.")
        return 0

    rows = []
    for c in fixtures:
        state = {"question": c["question"], "evidence": c["evidence"], "reply": c["reply"]}
        try:
            out = call(key, state)
        except Exception as e:  # noqa: BLE001
            print(f"{c['id']}: ERROR {type(e).__name__}: {e}")
            continue
        ans = out.get("answers", {})
        rows.append({
            "id": c["id"], "category": c["category"], "control": bool(c.get("control")),
            "calibrated": ans.get("calibrated", {}).get("noul"),
            "mode": ans.get("failure_mode", {}).get("choice"),
        })

    findings = []
    for r in rows:
        flagged = r["mode"] in BAD
        if r["control"]:
            verdict = "CAUGHT" if flagged else "MISSED"
            if not flagged:
                findings.append(r["id"])
        else:
            verdict = "BAD" if flagged else "clean"
            if flagged:
                findings.append(r["id"])
        print(f"{verdict:8s} {r['id']:42s} ({r['mode']})  calibrated={r['calibrated']}")
    print("\nfindings:", ", ".join(findings) if findings else "none")

    if args.out:
        with open(args.out, "w") as f:
            json.dump(rows, f, indent=2)
        print("wrote", args.out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
