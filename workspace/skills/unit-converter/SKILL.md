---
name: unit-converter
description: Convert units of length, weight, volume, temperature, and cooking measures. Invoke when user asks "convert X to Y", "how many ml in 2 cups", "kg to lbs", "cm to inches", "F to C", "celsius to fahrenheit", or any metric/imperial/cooking conversion. Fully offline, no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [units, conversion, metric, imperial, cooking, temperature]
prerequisites:
  commands: [python]
---

# Unit Converter

Fully offline unit conversion. No network, no API key.

> **Mandatory:** Run the exact `python` command below with the `exec` tool and use its output. Do NOT use `web_search` or `web_fetch` to "check" a conversion — that is duplicate work and local math is authoritative.

## Quick Reference

| Task | Command |
|------|---------|
| Any conversion | `python skills/unit-converter/scripts/convert.py <amount> <from> <to>` |
| Examples | `python skills/unit-converter/scripts/convert.py 2 cups ml` |
| | `python skills/unit-converter/scripts/convert.py 70 kg lbs` |
| | `python skills/unit-converter/scripts/convert.py 100 F C` |

## Supported Units

Length: mm, cm, m, km, in, ft, yd, mi
Weight: g, kg, oz, lbs
Volume: ml, l, tsp, tbsp, cups, pints, quarts, gallons
Temperature: C, F, K

Cooking aliases: cup/cups, tablespoon/tbsp, teaspoon/tsp work worldwide — no locale assumptions.

## Failure Behavior

Unknown unit → script prints supported list. Use it to ask the user for clarification naturally. Never guess conversion factors — the script is the source of truth.
