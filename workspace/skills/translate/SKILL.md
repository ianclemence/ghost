---
name: translate
description: Translate short phrases between any two languages. Invoke when user asks "translate X to French", "how do you say X in Spanish", "translate hello to Japanese", "what is X in German", or any short-phrase translation between any languages worldwide. Uses free MyMemory API, no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [translate, translation, language, multilingual]
prerequisites:
  commands: [curl]
---

# Translate

Free MyMemory API (`api.mymemory.translated.net`), no key. Short phrases only, any language pair worldwide — never assume English/Thai only.

> **Mandatory:** Detect source and target languages from the request, map them to ISO codes, run the exact `curl` command below with the `exec` tool, and use its output. Do NOT use `web_search` to "translate" — the translation API is authoritative.

## Quick Reference

| Task | Command |
|------|---------|
| Translate | `curl -s "https://api.mymemory.translated.net/get?q=<text>&langpair=<src>&#124;<dst>"` |

URL-encode `<text>`. ISO codes: en, fr, es, de, it, pt, nl, ru, zh, ja, ko, th, vi, ar, hi, tr, pl, uk, id, ms. Any other pair works the same way.

## Language Detection

- "translate X to French" → target fr, source auto (use `en` unless the text is clearly another language).
- "how do you say X in Spanish" → target es.
- "translate bonjour to English" → source fr, target en.
- Always state the detected pair (e.g. "EN → TH") so the user can correct it.

## Failure Behavior

Long documents (>500 chars) → ask the user to shorten to a phrase, or use `document-convert` for files. API quota/empty → say translation is unavailable right now and stop. Never guess translations — if the API gives nothing usable, say so plainly.
