"""Grow the Sting anti-hallucination dataset (deterministic + teacher).

Every row is validated by flywheel.synthesize.validate before it is
kept: required args present, enums/bounds respected, numerics evidenced
by the query, duplicates rejected, negated/off-topic rows carry [].
Nothing ungrounded ever reaches the JSONL.

Composition target (~430 rows):
  positives ~310 with HEAVILY varied values (amounts, counts, limits,
    cities, URLs) — value variation is what teaches grounding;
  missing-slot / negated / off-topic / invalid -> [] fill the rest,
  because hallucination prevention is mostly negative training.

Teacher pass: ~12 calls rephrasing base positives (numbers/names/URLs
pinned); the validator drops any drifted paraphrase automatically.

Usage:
  python3 grow.py --tools tools.json --train train.jsonl --heldout heldout.jsonl
  STING_TEACHER_KEY=... python3 grow.py --tools tools.json --train train.jsonl \\
      --heldout heldout.jsonl --paraphrase
"""

import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from synthesize import validate  # noqa: E402  (same package, strict gate)

rng = random.Random(7)

CURRENCIES = [("USD", "EUR"), ("EUR", "USD"), ("USD", "GBP"), ("GBP", "EUR"),
              ("USD", "JPY"), ("EUR", "CHF"), ("USD", "CNY"), ("CAD", "USD")]
AMOUNTS = [5, 12, 25, 40, 75, 100, 150, 250, 400, 750, 1200, 2500, 5000, 99.99]
CITIES = ["Berlin", "Bangkok", "Lagos", "Paris", "Tokyo", "Nairobi", "Lima",
          "Oslo", "Cairo", "Seoul", "Lisbon", "Toronto", "Mumbai", "Sydney",
          "Reykjavik", "Mexico City"]
TOPICS = ["raspberry pi 5 nvme boot guide", "current bitcoin price in USD",
          "ghost local-first ai personal AI", "best sourdough starter method",
          "qwen3 0.6b ollama pi 5 speed", "how do escs work", "tailscale exit node setup",
          "fermenting hot sauce at home", "sourdough hydration calculator",
          "local vector search on raspberry pi", "bike touring routes in wales",
          "how to season cast iron", "openclaw vs ghost architecture",
          "whisper small-en on pi benchmarks", "growing tomatoes indoors",
          "usb gadget mode raspberry pi", "restoring cast iron skillet",
          "explaining dns to a ten year old", "cheap home server ups",
          "bike packing list for beginners"]
MEMO_TOPICS = ["green tea", "sister Ana", "shopping list", "dentist appointment",
               "project ideas", "book recommendations", "wifi password",
               "mom's birthday", "holiday plans", "meeting notes",
               "grandma's birthday", "car service", "gift ideas", "dream journal"]
URLS = ["https://example.com/guide", "https://www.themealdb.com/meal/52818",
        "https://arxiv.org/abs/2607.18363", "https://github.com/cactus-compute/needle",
        "https://tailscale.com/kb/1103/exit-nodes", "https://www.raspberrypi.com/documentation/",
        "https://huggingface.co/Cactus-Compute/needle2", "https://openai.com/index/introducing-gpt-4o/",
        "https://en.wikipedia.org/wiki/Sourdough", "https://docs.python.org/3/library/json.html"]
OFFTOPIC = ["hello ghost", "hi there", "what is the capital of france",
            "tell me a joke", "how are you today", "good morning",
            "what is the meaning of life", "who won the match yesterday",
            "sing me a song", "what time is it", "do you dream",
            "what is 12 times 12", "spell supercalifragilisticexpialidocious",
            "are you conscious", "what is your favourite colour",
            "tell me something interesting", "i am bored", "thanks",
            "good night", "see you later", "what day is it",
            "how old are you", "where do you live", "knock knock"]


def R(query, answers, reasoning):
    return {"query": query, "answers": answers, "reasoning": reasoning}


def cur_full():
    out = []
    for amt, (frm, to) in rng.sample([(a, c) for a in AMOUNTS for c in CURRENCIES], 90):
        q = rng.choice([
            f"how much is {amt} {frm} in {to}",
            f"convert {amt} {frm} to {to}",
            f"{amt} {frm} to {to} please",
        ])
        out.append(R(q, [{"name": "currency_convert",
                                  "arguments": {"amount": amt, "from": frm, "to": to}}],
                      f"'{amt}' -> amount; '{frm}' -> from; '{to}' -> to"))
    return out


def cur_no_amount():
    out = []
    for frm, to in rng.sample(CURRENCIES, 6):
        q = rng.choice([f"convert {frm} to {to}", f"{frm} in {to} please"])
        out.append(R(q, [{"name": "currency_convert",
                                  "arguments": {"from": frm, "to": to}}],
                      f"'{frm}' -> from; '{to}' -> to; no amount stated so omitted"))
    # extra phrasings to reach ~15
    for _ in range(9):
        frm, to = rng.choice(CURRENCIES)
        out.append(R(f"what is the {frm} to {to} rate",
                     [{"name": "currency_convert", "arguments": {"from": frm, "to": to}}],
                     f"'{frm}' -> from; '{to}' -> to; no amount stated so omitted"))
    return out


def cur_missing():
    return [R(q, [], "no currency stated; refusing to guess")
            for q in ["how much is 250 in euros", "convert 100 please",
                      "what is 50 worth", "exchange some money for me",
                      "how much money is that", "convert this amount",
                      "what would that be in yen", "change it to pounds"]]


def weather_loc():
    out = []
    for city in rng.sample(CITIES, 16):
        q = rng.choice([f"check the weather in {city} right now",
                        f"what is the weather like in {city}",
                        f"weather in {city}"])
        out.append(R(q, [{"name": "weather_now", "arguments": {"location": city}}],
                      f"'{city}' -> location"))
    for city in rng.sample(CITIES, 8):
        out.append(R(f"is it raining in {city}",
                     [{"name": "weather_now", "arguments": {"location": city}}],
                     f"'{city}' -> location"))
    for city in rng.sample(CITIES, 8):
        out.append(R(f"do i need a jacket in {city} today",
                     [{"name": "weather_now", "arguments": {"location": city}}],
                     f"'{city}' -> location"))
    for city in rng.sample(CITIES, 8):
        out.append(R(f"temperature in {city}",
                     [{"name": "weather_now", "arguments": {"location": city}}],
                     f"'{city}' -> location"))
    return out


def weather_bare():
    # location is optional in the schema: omission is the correct call.
    return [R(q, [{"name": "weather_now", "arguments": {}}], "no place stated; optional location omitted")
            for q in ["is it raining", "what is the weather like",
                      "do i need a jacket today", "how hot is it",
                      "will it rain later", "is it cold outside",
                      "what is the temperature", "should i bring an umbrella"]]


def search_topic():
    out = []
    for t in rng.sample(TOPICS, 20):
        q = rng.choice([f"search the web for {t}", f"look up {t}", f"find {t}"])
        out.append(R(q, [{"name": "web_search", "arguments": {"query": t}}],
                      f"'{t}' -> query"))
    for t in rng.sample(TOPICS, 15):
        out.append(R(f"google {t}", [{"name": "web_search", "arguments": {"query": t}}],
                      f"'{t}' -> query"))
    for t in rng.sample(TOPICS, 10):
        out.append(R(f"what does the internet say about {t}",
                     [{"name": "web_search", "arguments": {"query": t}}],
                     f"'{t}' -> query"))
    return out


def search_count():
    out = []
    for n in [1, 2, 3, 5, 7, 10]:
        for t in rng.sample(TOPICS, 6):
            q = rng.choice([f"find {n} results for {t}",
                            f"search for {t}, give me {n}"])
            out.append(R(q, [{"name": "web_search",
                                      "arguments": {"query": t, "count": n}}],
                          f"'{t}' -> query; '{n}' -> count"))
    return out[:40]


def fetch_url():
    out = []
    for u in URLS:
        out.append(R(f"fetch {u}", [{"name": "web_fetch", "arguments": {"url": u}}],
                      f"'{u}' -> url (verbatim copy)"))
    for u in rng.sample(URLS, 5):
        out.append(R(f"get me the text of {u}",
                     [{"name": "web_fetch", "arguments": {"url": u}}],
                     f"'{u}' -> url (verbatim copy)"))
    return out


def fetch_maxchars():
    out = []
    for n in [100, 500, 1000, 5000]:
        for u in rng.sample(URLS, 3):
            out.append(R(f"fetch {u}, first {n} chars",
                         [{"name": "web_fetch", "arguments": {"url": u, "maxChars": n}}],
                         f"'{u}' -> url; '{n}' -> maxChars"))
    return out


def memo_query():
    out = []
    for t in rng.sample(MEMO_TOPICS, 14):
        q = rng.choice([f"what did i remember about {t}",
                        f"search my memory for {t}",
                        f"recall {t}"])
        out.append(R(q, [{"name": "memory_recall", "arguments": {"query": t}}],
                      f"'{t}' -> query"))
    for t in rng.sample(MEMO_TOPICS, 8):
        out.append(R(f"do i have any notes on {t}",
                     [{"name": "memory_recall", "arguments": {"query": t}}],
                     f"'{t}' -> query"))
    for t in rng.sample(MEMO_TOPICS, 8):
        out.append(R(f"find {t} in my notes",
                     [{"name": "memory_recall", "arguments": {"query": t}}],
                     f"'{t}' -> query"))
    return out


def memo_limit():
    out = []
    for n in [1, 2, 3, 5, 10]:
        for t in rng.sample(MEMO_TOPICS, 3):
            out.append(R(f"find {n} notes about {t}",
                         [{"name": "memory_recall", "arguments": {"query": t, "limit": n}}],
                         f"'{t}' -> query; '{n}' -> limit"))
    return out


def missing_slot():
    rows = [R(q, [], "required argument absent; refusing to guess") for q in [
        "search the web", "look something up for me", "google it",
        "find that thing online", "search please", "look up",
        "fetch that page", "get the page text", "fetch it for me",
        "download that url", "search my memory", "recall that thing",
        "check my notes", "remember what", "find it in memory",
        "how much is it", "convert that", "what is this worth"]]
    return rows


def negated():
    verbs = ["don't search the web for {}", "never look up {}",
             "do not check the weather in {}", "stop fetching {}",
             "don't convert {} to euros", "never recall {}",
             "do not google {}", "don't fetch {}", "stop searching for {}",
             "do not check my notes on {}"]
    fills = ["bitcoin prices", "berlin weather", "that page", "my notes",
             "sourdough recipes", "pi guides", "tomatoes", "cast iron",
             "dns guides", "server deals"]
    out = []
    for i in range(30):
        v = verbs[i % len(verbs)]
        f = fills[(i * 3) % len(fills)]
        out.append(R(v.format(f), [], "negated request; no call"))
    return out


def offtopic():
    return [R(q, [], "off-topic; no declared tool serves this") for q in OFFTOPIC[:25]]


def invalid():
    rows = []
    for n in [0, 15, 99]:
        for t in rng.sample(TOPICS, 2):
            rows.append(R(f"find {n} results for {t}", [],
                           f"count {n} outside 1-10; no call"))
    for n in [0, 99]:
        for t in rng.sample(MEMO_TOPICS, 2):
            rows.append(R(f"find {n} notes about {t}", [],
                           f"limit {n} outside 1-10; no call"))
    rows.append(R("fetch https://example.com/guide, first 10 chars", [],
                   "maxChars 10 below minimum 100; no call"))
    rows.append(R("fetch https://example.com/guide, first 5 chars", [],
                   "maxChars 5 below minimum 100; no call"))
    return rows


def paraphrase_pass(tools, base_rows, key, base_url, model, want=36):
    """Teacher rephrasings with values pinned; validator drops drift."""
    import urllib.request
    kept = []
    system = ("Rephrase the user request 3 ways, keeping every number, name, "
              "URL and currency code character-identical. Reply JSON only: "
              '{"paraphrases": ["...", "...", "..."]}')
    cands = [r for r in base_rows if r["answers"]
             and any(isinstance(a.get("arguments", {}).get(k), str)
                     for a in r["answers"] for k in ("query", "location", "url"))
             ][:12]
    for r in cands:
        if len(kept) >= want:
            break
        body = json.dumps({
            "model": model, "temperature": 0.7,
            "response_format": {"type": "json_object"},
            "messages": [{"role": "system", "content": system},
                         {"role": "user", "content": r["query"]}],
        }).encode()
        req = urllib.request.Request(base_url.rstrip("/") + "/chat/completions",
                                     data=body, headers={
                                         "Content-Type": "application/json",
                                         "Authorization": "Bearer " + key})
        try:
            with urllib.request.urlopen(req, timeout=90) as resp:
                data = json.load(resp)
            paras = json.loads(data["choices"][0]["message"]["content"]).get("paraphrases", [])
        except Exception as exc:  # noqa: BLE001 - one bad teacher call skips one seed
            print(f"  paraphrase skip [{r['query'][:40]}]: {str(exc)[:60]}")
            continue
        for p in paras[:3]:
            row = {"query": p, "answers": r["answers"],
                   "reasoning": r["reasoning"] + " (rephrased; values pinned)"}
            ok, reason = validate(p, tools, row["answers"])
            if ok:
                kept.append(row)
            else:
                print(f"  paraphrase drop [{reason}]: {p[:50]}")
    return kept


def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--tools", required=True)
    ap.add_argument("--train", required=True)
    ap.add_argument("--heldout", required=True)
    ap.add_argument("--paraphrase", action="store_true")
    ap.add_argument("--seed", type=int, default=7)
    a = ap.parse_args()

    tools = json.load(open(a.tools))[:5]
    rows = (cur_full() + cur_no_amount() + cur_missing() + weather_loc()
            + weather_bare() + search_topic() + search_count() + fetch_url()
            + fetch_maxchars() + memo_query() + memo_limit() + missing_slot()
            + negated() + offtopic() + invalid())
    print(f"deterministic candidates: {len(rows)}")

    if a.paraphrase:
        key = os.environ.get("STING_TEACHER_KEY", "")
        if not key:
            print("STING_TEACHER_KEY required for --paraphrase", file=sys.stderr)
            sys.exit(2)
        base = os.environ.get("STING_TEACHER_BASE", "https://api.deepseek.com")
        model = os.environ.get("STING_TEACHER_MODEL", "deepseek-chat")
        para = paraphrase_pass(tools, rows, key, base, model)
        print(f"teacher paraphrases kept: {len(para)}")
        rows += para

    # Validate everything (defense in depth: templates are code too).
    kept, dropped = [], 0
    for r in rows:
        ok, reason = validate(r["query"], tools, r["answers"])
        if ok:
            kept.append(r)
        else:
            dropped += 1
            print(f"  TEMPLATE DROP [{reason}]: {r['query'][:60]}")
    print(f"validated: {len(kept)} kept, {dropped} dropped")

    r2 = random.Random(a.seed)
    r2.shuffle(kept)
    n_hold = max(1, len(kept) // 10)
    held, train = kept[:n_hold], kept[n_hold:]

    def dump(path, rs):
        with open(path, "w") as f:
            for r in rs:
                row = {"query": r["query"], "tools": tools, "answers": r["answers"]}
                if r.get("reasoning"):
                    row["reasoning"] = r["reasoning"]
                f.write(json.dumps(row) + "\n")

    dump(a.train, train)
    dump(a.heldout, held)
    neg = sum(1 for r in train if not r["answers"])
    print(f"train={len(train)} (negatives={neg}) heldout={len(held)}")


if __name__ == "__main__":
    main()
