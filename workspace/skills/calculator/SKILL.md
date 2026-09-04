---
name: calculator
description: Evaluate math expressions, percentages, tips, and splits. Invoke when user asks "what is 15% of 850", "calculate X", "15% tip on 850 baht", "split 1200 three ways", "what is 48*17+3", or any arithmetic. Fully offline, no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [calculator, math, arithmetic, tip, percent]
prerequisites:
  commands: [python]
---

# Calculator

Local arithmetic via python. Fully offline, no network, no API key.

> **Mandatory:** Run the exact `python` command below with the `exec` tool and use its output. Do NOT use `web_search` to "check" math — local computation is authoritative.

## Quick Reference

| Task | Command |
|------|---------|
| Evaluate | `python skills/calculator/scripts/calc.py "15% of 850"` |
| Plain expression | `python skills/calculator/scripts/calc.py "48*17+3"` |
| Tip + split | `python skills/calculator/scripts/calc.py "15% tip on 850 split 3"` |

## Supported Forms

Plain expressions (`48*17+3`, `(1200+300)/4`), `X% of Y`, `X% tip on Y [split N]`, `split Y N ways`. Only safe arithmetic characters are accepted — anything else is rejected rather than executed.

## Failure Behavior

Unparseable input → script prints accepted forms. Ask the user to rephrase naturally. Never guess the answer.
