---
type: observations
created: 2026-03-19
updated: 2026-03-19
tags: [notes, skills, observations, API-quirks]
description: Operational notes on skill behavior, API quirks, and tool quirks observed during use.
---

# Skill Observations

Notes on skill and tool behavior gathered through actual use. Links to relevant skill files.

## API Behavior

### CoinGecko API (crypto skill)
- Free tier: 50 req/min. Cache aggressively.
- Uses **coin IDs** (e.g., `bitcoin`), not ticker symbols (`BTC`). Search endpoint available.
- Returns 404 for unrecognized IDs — always validate with search first.

### AviationStack API (flight skill)
- Free tier: 100 requests/month — very easy to exhaust.
- Coverage: US/Europe good, some airports limited.
- Returns 404 when flight not found, not an empty array.

### TheMealDB API (recipe skill)
- No auth required. ~400 meals total.
- Lookup by ID returns full recipe (ingredients, instructions).
- YouTube links provided for some meals — not all.

### Open-Meteo APIs (weather, aqi)
- No API key ever needed.
- AQI coverage: most cities globally, but not all pollutants available everywhere.
- Geocoding is separate endpoint from weather/AQI — always geocode first.

### wttr.in (weather skill)
- Fast, no auth.
- For structured JSON: `?format=j1`
- For specific coordinates: `wttr.in/{lat},{lon}`

### r.jina.ai (scraper skill)
- Reader mode: strips ads/trackers, returns clean text.
- Markdown mode: `m/` prefix preserves structure.
- Batch: POST newline-separated URLs to `/m/`.
- Doesn't handle JavaScript-heavy SPAs well.
- No rate limit documented — be respectful.

## Tool Quirks

### curl + JSON parsing
- Always use `curl -s` (silent) to avoid progress meter pollution.
- For JSON: pipe to `python3 -c "import sys,json; ..."` for clean extraction.
- Always set `--max-time 10` on external APIs to avoid hangs.

### Python scripts
- Scripts in `workspace/skills/*/scripts/` must exist and be functional.
- Python stdlib preferred — avoid adding dependencies unless skill requires it.
- Use `python3` explicitly, not `python` (ambiguous on some systems).

### skill dependency checking
- `prerequisites.commands` in SKILL.md frontmatter lists required binaries.
- Commands checked via `exec.LookPath` — must be on PATH.
- Skills without `prerequisites` are skipped by the dependency checker.

## Skill Architecture Notes

### Cross-platform patterns
- Skills that use curl: always use `curl -s` and parse JSON in Python.
- Avoid PowerShell-specific code — prefer bash that works on Linux/Pi/macOS.
- PowerShell fallbacks acceptable only when bash equivalent doesn't exist.

### Skill file structure
```
SKILL.md          — description, trigger phrases, quick reference, commands
scripts/          — supporting Python/shell scripts
references/       — supplementary docs (API cheatsheets, templates, etc.)
templates/        — reusable templates
```
