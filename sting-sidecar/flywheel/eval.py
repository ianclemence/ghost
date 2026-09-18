"""Score Sting router weights against held-out flywheel rows.

Compares model variants on identical held-out queries:
  base2  base Needle-2 engine (current sidecar default)
  base3  base Needle-3 engine (the fine-tune parent)
  tuned  a built .cact archive (auto-detected generation)

Metrics per variant: overall exact-match, positive exact-match,
negative no-call rate (1 - hallucination rate), and ungrounded-number
rate on positives (calls whose numerics are not evidenced by the query
— the hallucination shape we train against).

Selector mode (--selector embed) benchmarks the zero-training Jev arm:
embed the query with the on-device embedder, cosine-pick top-1
tool, and score tool-NAME accuracy only (selection, not argument
filling) plus latency. Needs Ollama reachable with an embedding model.

Usage:
  python3 eval.py --heldout heldout.jsonl [--tuned sting_ghost.cact]
  python3 eval.py --heldout heldout.jsonl --selector embed
"""

import json
import os
import re
import sys

os.environ.setdefault("NEEDLE_TELEMETRY", "0")

NUMBER = re.compile(r"[-+]?\d+(?:\.\d+)?")


def norm_args(args):
    return json.dumps(args or {}, sort_keys=True, default=str)


def exact_match(got, want):
    if (not got) != (not want):
        return False
    if not want:
        return True
    if len(got) != len(want):
        return False
    g = sorted((c.get("name"), norm_args(c.get("arguments"))) for c in got)
    w = sorted((c.get("name"), norm_args(c.get("arguments"))) for c in want)
    return g == w


def ungrounded_numbers(query, calls):
    try:
        nums = {float(m) for m in NUMBER.findall(query)}
    except ValueError:
        nums = set()
    bad = 0
    for c in calls or []:
        for v in (c.get("arguments") or {}).values():
            if isinstance(v, bool):
                continue
            if isinstance(v, (int, float)) and float(v) not in nums:
                bad += 1
    return bad


def ollama_embed(base, model, text, timeout=90):
    import urllib.request
    body = json.dumps({"model": model, "prompt": text}).encode()
    req = urllib.request.Request(base.rstrip("/") + "/api/embeddings", data=body,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.load(resp)["embedding"]


def cosine(a, b):
    dot = sum(x * y for x, y in zip(a, b))
    na = sum(x * x for x in a) ** 0.5
    nb = sum(y * y for y in b) ** 0.5
    return dot / (na * nb) if na and nb else 0.0


def score_selector(rows, base, model):
    """Embedding pre-selection: top-1 tool NAME accuracy + latency.

    Selection only — argument filling stays generative. A negative row
    (want []) counts correct only if... it can't be: top-1 always picks
    a tool. So negatives measure the abstention gap the generative
    gate must cover. Reported honestly as such.
    """
    import time
    tools = rows[0]["tools"]
    texts = {t["name"]: (t["name"] + " " + t.get("description", "")) for t in tools}
    tvecs = {n: ollama_embed(base, model, t) for n, t in texts.items()}
    tot = sel_ok = neg_tot = 0
    pos_lat, lat_n = 0.0, 0
    for i, r in enumerate(rows):
        if i % 10 == 0:
            print(f"  ...{i}/{len(rows)}", flush=True)
        t0 = time.time()
        qv = ollama_embed(base, model, r["query"])
        ranked = sorted(tools, key=lambda t: cosine(qv, tvecs[t["name"]]), reverse=True)
        pos_lat += (time.time() - t0) * 1000
        lat_n += 1
        want = r["answers"] or []
        tot += 1
        if want:
            if ranked and ranked[0]["name"] == want[0].get("name"):
                sel_ok += 1
        else:
            neg_tot += 1
    pos_tot = tot - neg_tot
    return {"n": tot, "selection_acc": sel_ok / pos_tot if pos_tot else 0,
            "neg_abstain_gap": neg_tot,
            "mean_select_ms": pos_lat / lat_n if lat_n else 0}


def score_variant(make_agent, rows):
    agent = make_agent()
    tot = pos_tot = pos_ok = neg_tot = neg_ok = ungrounded = 0
    for i, r in enumerate(rows):
        if i % 10 == 0:
            print(f"  ...{i}/{len(rows)}", flush=True)
        try:
            resp = agent.complete(r["query"])
        except Exception as exc:  # noqa: BLE001 - one bad turn must not kill eval
            print(f"  turn error [{r['query'][:40]}]: {str(exc)[:80]}")
            continue
        got = resp.get("function_calls") or []
        want = r["answers"] or []
        tot += 1
        if exact_match(got, want):
            if want:
                pos_ok += 1
            else:
                neg_ok += 1
        if want:
            pos_tot += 1
            ungrounded += 1 if ungrounded_numbers(r["query"], got) else 0
        else:
            neg_tot += 1
    try:
        agent.reset()
    except Exception:  # noqa: BLE001, S110 - best-effort cleanup
        pass
    return {"n": tot,
            "overall": (pos_ok + neg_ok) / tot if tot else 0,
            "positive": pos_ok / pos_tot if pos_tot else 0,
            "neg_nocall": neg_ok / neg_tot if neg_tot else 0,
            "halluc_rate": 1 - (neg_ok / neg_tot) if neg_tot else 0,
            "ungrounded_pos_rate": ungrounded / pos_tot if pos_tot else 0}


def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--heldout", required=True)
    ap.add_argument("--tuned", default="")
    ap.add_argument("--skip-base2", action="store_true")
    ap.add_argument("--selector", choices=["", "embed"], default="",
                    help="score the embedding pre-selector instead of generative variants")
    ap.add_argument("--ollama-base", default="http://localhost:11434")
    ap.add_argument("--embed-model", default="nomic-embed-text")
    a = ap.parse_args()

    import needle
    rows = [json.loads(l) for l in open(a.heldout)]
    tools = rows[0]["tools"]
    print(f"heldout rows: {len(rows)}")

    if a.selector == "embed":
        m = score_selector(rows, a.ollama_base, a.embed_model)
        print(f"embed-selector: n={m['n']} selection_acc={m['selection_acc']:.3f} "
              f"mean_select_ms={m['mean_select_ms']:.0f}")
        print(f"  note: {m['neg_abstain_gap']} negative rows always pick a tool "
              f"(abstention stays with the generative gate)")
        return

    variants = {}
    if not a.skip_base2:
        variants["base2"] = lambda: needle.Needle(tools=tools)
    variants["base3"] = lambda: needle.Needle(tools=tools, generation=3)
    if a.tuned:
        variants["tuned"] = lambda: needle.Needle(weights=a.tuned, tools=tools)

    results = {}
    for name, make in variants.items():
        print(f"scoring {name}...")
        results[name] = score_variant(make, rows)
    print(f"{'variant':<8} {'n':>3} {'overall':>8} {'positive':>9} {'neg_nocall':>11} {'halluc':>7} {'ungrounded':>11}")
    for name, m in results.items():
        print(f"{name:<8} {m['n']:>3} {m['overall']:>8.3f} {m['positive']:>9.3f} "
              f"{m['neg_nocall']:>11.3f} {m['halluc_rate']:>7.3f} {m['ungrounded_pos_rate']:>11.3f}")


if __name__ == "__main__":
    main()
