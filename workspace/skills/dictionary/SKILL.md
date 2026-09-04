---
name: dictionary
description: Define English words with pronunciation and examples. Invoke when user asks "define X", "what does X mean", "meaning of X", "pronunciation of X", or any single-word definition. Uses free dictionaryapi.dev, no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [dictionary, define, meaning, vocabulary]
prerequisites:
  commands: [curl]
---

# Dictionary

Free dictionaryapi.dev, no key. One word per lookup, any language of explanation follows the user's language.

> **Mandatory:** Run the exact `curl` command below with the `exec` tool and use its output. Do NOT use `web_search` to "look up" the word — the dictionary API is authoritative.

## Quick Reference

| Task | Command |
|------|---------|
| Define | `curl -s "https://api.dictionaryapi.dev/api/v2/entries/en/<word>"` |

Replace `<word>` with a single English word (lowercase, no spaces).

## Failure Behavior

Unknown word → API returns `No Definitions Found`. Report plainly: "I couldn't find a definition for X." If offline, say so plainly and stop — do not wander into web search. Phrases and sentences are out of scope — ask for a single word.
