#!/usr/bin/env bash
# Latency baseline harness.
#
# Reads Ghost's own turn telemetry from the systemd journal (no new
# instrumentation: the loop already logs iterations, prompt/completion tokens,
# ttfb_ms, duration_ms, usage_source and tools_used per turn) and reports
# p50/p95 turn duration, p50/p95 first-token latency, and the usage-source
# mix. Use it before and after a change to see where the time went rather
# than only the total.
#
#   scripts/latency-baseline.sh [since]      # default: "24 hours ago"
#
# Examples:
#   scripts/latency-baseline.sh "30 min ago"   # the turn just deployed
#   scripts/latency-baseline.sh "7 days ago"   # a longer baseline
set -uo pipefail

SINCE="${1:-24 hours ago}"

journalctl -u ghost --since "$SINCE" --no-pager 2>/dev/null \
  | grep 'turn telemetry' \
  | python3 -c '
import re, sys

def pct(xs, p):
    if not xs:
        return None
    xs = sorted(xs)
    i = min(len(xs) - 1, int(round((p / 100.0) * (len(xs) - 1))))
    return xs[i]

dur, ttfb, prompt, total = [], [], [], []
src, tools = {}, 0
for line in sys.stdin:
    m = re.search(r"duration_ms=(-?\d+)", line)
    if m:
        dur.append(int(m.group(1)))
    m = re.search(r"ttfb_ms=(-?\d+)", line)
    if m and int(m.group(1)) >= 0:
        ttfb.append(int(m.group(1)))
    m = re.search(r"prompt_tokens=(\d+)", line)
    if m:
        prompt.append(int(m.group(1)))
    m = re.search(r"total_tokens=(\d+)", line)
    if m:
        total.append(int(m.group(1)))
    m = re.search(r"usage_source=(\w+)", line)
    if m:
        src[m.group(1)] = src.get(m.group(1), 0) + 1
    if "tools_used=[]" not in line:
        tools += 1

print(f"turns:            {len(dur)}")
print(f"duration_ms  p50: {pct(dur, 50)}   p95: {pct(dur, 95)}")
print(f"ttfb_ms      p50: {pct(ttfb, 50)}   p95: {pct(ttfb, 95)}   (n={len(ttfb)})")
print(f"prompt_tokens p50: {pct(prompt, 50)}   p95: {pct(prompt, 95)}")
print(f"total_tokens  p50: {pct(total, 50)}   p95: {pct(total, 95)}")
print(f"turns_using_tools: {tools}")
print("usage_source:      " + (str(src) if src else "(none)"))
'
