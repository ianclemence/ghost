---
name: world-clock
description: Convert times between world timezones. Invoke when user asks "what time is 9am Bangkok in London", "convert 3pm Tokyo to Paris time", "what time is it in New York", "time difference between X and Y", or any timezone conversion. Fully offline, no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [timezone, world-clock, time-conversion, zones]
prerequisites:
  commands: [python]
---

# World Clock

Timezone math via system zoneinfo. Fully offline, no network, no API key.

> **Mandatory:** Run the exact `python` command below with the `exec` tool and use its output. Do NOT use `web_search` to "check" the time — local zoneinfo is authoritative. Never guess offsets — the script resolves them, including daylight saving.

## Quick Reference

| Task | Command |
|------|---------|
| Convert time | `python skills/world-clock/scripts/clock.py "9am" "Asia/Bangkok" "Europe/London"` |
| Current time somewhere | `python skills/world-clock/scripts/clock.py "now" "Asia/Bangkok" "Europe/London"` |
| List matches | City names are fuzzy-matched to IANA zones (e.g. Bangkok, London, Tokyo, Paris, New York). |

## City Matching

Common cities map automatically: Bangkok, London, Tokyo, Paris, Berlin, Singapore, Sydney, New York, Los Angeles, Dubai, Hong Kong, Seoul. Unknown input prints IANA suggestions — use them to clarify naturally.

## Failure Behavior

Unknown city → script prints close matches. Ask the user to pick one. Never invent offsets.
