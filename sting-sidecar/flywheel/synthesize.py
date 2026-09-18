"""Sting dataset flywheel: teacher-proposed, Ghost-validated training data.

Pipeline (Ghost's labeling moat: the teacher proposes, Ghost disposes):
  1. Read tool schemas (sting.ToolSchema JSON) from `ghost sting dump-tools`.
  2. A strong teacher model (default DeepSeek chat via OpenAI-compatible
     HTTP) drafts {answers, reasoning} per seed query.
  3. Strict local validation keeps only grounded examples:
     required args present, enums/bounds respected, every numeric leaf
     evidenced by the query text, no calls for negated/off-topic seeds.
  4. Emit Needle-format JSONL: {query, tools, answers[, reasoning][, system]}.
     Off-topic seeds emit {"answers": []} — without them a tuned model
     calls a tool on everything.

Secrets: teacher key ONLY via STING_TEACHER_KEY env. Never logged,
never written. Teacher base/model via STING_TEACHER_BASE /
STING_TEACHER_MODEL (OpenAI-compatible /chat/completions).

Usage:
  ghost sting dump-tools > tools.json
  STING_TEACHER_KEY=... python3 synthesize.py --tools tools.json \
      --out sting_data.jsonl [--max-seeds 12]
"""

import json
import os
import re
import sys
import urllib.request

SEEDS = [
    ("search the web for raspberry pi 5 nvme boot guide", "positive"),
    ("what is the capital of france", "offtopic"),
    ("what is 22 plus 18 degrees", "positive"),
    ("remind me to water the plants tomorrow at 7am", "positive"),
    ("how much is 250 dollars in euros", "positive"),
    ("check the weather in berlin right now", "positive"),
    ("don't search anything, just saying hello", "negated"),
    ("search for ghost local-first ai personal AI and fetch the top result", "parallel"),
    ("what did i remember about green tea", "positive"),
    ("turn on the lights", "missing"),
    ("delete all my memories", "negated"),
    ("hello ghost", "offtopic"),
]

NEGATION = re.compile(r"\b(don'?t|do not|never|stop|cancel|not )\b", re.I)
NUMBER = re.compile(r"[-+]?\d+(?:\.\d+)?")
YEAR = re.compile(r"\b(19|20)\d{2}\b")


def teacher_complete(base, key, model, tools, query):
    system = ("You are a tool router. Given tools and a user query, reply with JSON only: "
              '{"answers": [{"name": ..., "arguments": {...}}], "reasoning": "short derivation"} '
              "Use [] when no declared tool serves the query (off-topic, negated, or missing "
              "required arguments). Arguments must only use values stated in the query.")
    body = json.dumps({
        "model": model,
        "temperature": 0,
        "response_format": {"type": "json_object"},
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": json.dumps({"tools": tools, "query": query})},
        ],
    }).encode()
    req = urllib.request.Request(base.rstrip("/") + "/chat/completions", data=body, headers={
        "Content-Type": "application/json", "Authorization": "Bearer " + key})
    with urllib.request.urlopen(req, timeout=90) as resp:
        data = json.load(resp)
    return json.loads(data["choices"][0]["message"]["content"])


def validate(query, tools, answers):
    """Strict Ghost-side validation. Returns (ok, reason)."""
    by_name = {t["name"]: t for t in tools}
    if not answers:
        return True, "empty-call"
    if NEGATION.search(query):
        return False, "negated-request"
    seen = set()
    for call in answers:
        name = call.get("name", "")
        args = call.get("arguments") or {}
        if name not in by_name:
            return False, f"unknown-tool:{name}"
        key = name + "\x00" + json.dumps(args, sort_keys=True)
        if key in seen:
            return False, "duplicate-call"
        seen.add(key)
        schema = by_name[name].get("parameters", {})
        required = schema.get("required", [])
        for r in required:
            if r not in args or args[r] in (None, ""):
                return False, f"missing-required:{name}.{r}"
        props = schema.get("properties", {})
        for k, v in args.items():
            spec = props.get(k, {})
            if "enum" in spec and v not in spec["enum"]:
                return False, f"enum-violation:{name}.{k}"
            if isinstance(v, (int, float)) and not isinstance(v, bool):
                if "minimum" in spec and v < spec["minimum"]:
                    return False, f"range-violation:{name}.{k}"
                if "maximum" in spec and v > spec["maximum"]:
                    return False, f"range-violation:{name}.{k}"
                query_nums = {float(m) for m in NUMBER.findall(query)}
                if float(v) not in query_nums:
                    return False, f"ungrounded-numeric:{name}.{k}={v}"
    return True, "ok"


def self_test():
    tools = [{"name": "set_thermostat", "description": "t",
              "parameters": {"type": "object",
                             "properties": {"temperature": {"type": "integer", "minimum": 10, "maximum": 30}},
                             "required": ["temperature"]}}]
    cases = [
        ("set the thermostat to 22 degrees",
         [{"name": "set_thermostat", "arguments": {"temperature": 22}}], True),
        ("set the thermostat to 22 degrees",
         [{"name": "set_thermostat", "arguments": {"temperature": 19}}], False),
        ("don't touch the thermostat",
         [{"name": "set_thermostat", "arguments": {"temperature": 22}}], False),
        ("hello", [], True),
        ("set the thermostat to 99 degrees",
         [{"name": "set_thermostat", "arguments": {"temperature": 99}}], False),
    ]
    failed = 0
    for query, answers, want in cases:
        ok, reason = validate(query, tools, answers)
        status = "PASS" if ok == want else "FAIL"
        if ok != want:
            failed += 1
        print(f"{status} [{reason}] {query[:45]}")
    print(f"self-test: {len(cases) - failed}/{len(cases)}")
    return 1 if failed else 0


def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--tools", required=False, default="")
    ap.add_argument("--out", required=False, default="")
    ap.add_argument("--max-seeds", type=int, default=len(SEEDS))
    ap.add_argument("--system", default="")
    ap.add_argument("--self-test", action="store_true",
                    help="run offline validation checks, no teacher calls")
    args = ap.parse_args()

    if args.self_test:
        sys.exit(self_test())

    if not args.tools or not args.out:
        print("--tools and --out are required (unless --self-test)", file=sys.stderr)
        sys.exit(2)
    key = os.environ.get("STING_TEACHER_KEY", "")
    if not key:
        print("STING_TEACHER_KEY is required", file=sys.stderr)
        sys.exit(2)
    base = os.environ.get("STING_TEACHER_BASE", "https://api.deepseek.com")
    model = os.environ.get("STING_TEACHER_MODEL", "deepseek-chat")
    tools = json.load(open(args.tools))
    # The flywheel trains the bounded router: cap like the runtime does.
    tools = tools[:5]

    kept, dropped = 0, {}
    with open(args.out, "w") as out:
        for query, kind in SEEDS[:args.max_seeds]:
            try:
                draft = teacher_complete(base, key, model, tools, query)
            except Exception as exc:  # noqa: BLE001 - teacher flakiness must not kill the run
                dropped[query] = f"teacher-error:{str(exc)[:60]}"
                continue
            answers = draft.get("answers", [])
            if kind in ("offtopic",):
                answers = []  # contract anchor: off-topic MUST be []
            ok, reason = validate(query, tools, answers)
            if not ok:
                dropped[query] = reason
                continue
            row = {"query": query, "tools": tools, "answers": answers}
            if draft.get("reasoning"):
                row["reasoning"] = draft["reasoning"]
            if args.system:
                row["system"] = args.system
            out.write(json.dumps(row) + "\n")
            kept += 1
    print(f"kept={kept} dropped={len(dropped)}")
    for q, r in dropped.items():
        print(f"  DROP [{r}] {q[:60]}")


if __name__ == "__main__":
    main()
