#!/usr/bin/env python3
"""Terminal regression checks that need a real terminal (Go unit tests can't
see renderer artifacts). Fails if the dock is duplicated after popups close.

    scripts/tui-regress.py /path/to/ghost
"""
import json, os, subprocess, sys, tempfile

binary = sys.argv[1] if len(sys.argv) > 1 else "ghost"
here = os.path.dirname(os.path.abspath(__file__))

def screen(steps):
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as f:
        json.dump(steps + [["snap", "end"]], f)
    out = subprocess.run([sys.executable, os.path.join(here, "tui-pty.py"), "90", "26", f.name, "--", binary],
                         capture_output=True, text=True, timeout=120).stdout
    os.unlink(f.name)
    return [l.split("|", 1)[1] for l in out.splitlines() if len(l) > 3 and l[2] == "|" and l[:2].isdigit()]

def rules(lines):
    return sum(1 for l in lines if l and set(l) == {"─"})

failures = []
# 1. Open and close the slash palette three times: still exactly one dock (2 rules).
cycle = [["type", "/"], ["key", "<bs>"]] * 3
n = rules(screen(cycle))
if n != 2:
    failures.append(f"slash palette open/close x3 left {n} rules on screen, want 2 (duplicate dock)")
# 2. Model picker open then close.
n = rules(screen([["keys", "\x0c"], ["sleep", 1.5], ["key", "<esc>"]]))
if n != 2:
    failures.append(f"model picker open/close left {n} rules, want 2")
# 3. A two-row message keeps its first row visible.
lines = screen([["type", "a" * 85 + " b" * 10]])
if not any(l.startswith("aaaa") for l in lines):
    failures.append("first row of a wrapped message disappeared")

for f in failures:
    print("FAIL:", f)
print("tui regressions:", "FAILED" if failures else "ok")
sys.exit(1 if failures else 0)
