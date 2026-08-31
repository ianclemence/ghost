---
name: quick-capture
description: Capture a note, idea, or reminder-to-self quickly and reliably. Invoke when the user says "remember this", "note that down", "add a note", "capture this", "write that down", "save this for later", or asks you to jot something down. Appends to a single plain file so it is never lost, no matter the channel.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [capture, notes, remember, quick, inbox]
---

# Quick Capture

A few words in, it's saved. No rambling, no extra steps — this is the *capture
everything* inbox. The goal is to get the thought out of the user's head and
onto disk instantly, so nothing is lost.

## Where it goes

Everything appends to a single file:

```
workspace/data/captures.md
```

Use the raw write/append path (the workspace-relative data dir), not the memory
system. Captures are the user's inbox, not curated memory.

## How to capture

1. Append one line to `workspace/data/captures.md` with a timestamp, then the
   captured text, then which channel it came from.
2. Do not summarize, reformat, or "improve" the capture. Save it close to how
   the user said it — that's what makes it trustworthy.
3. Keep it to one line. If the user gave several items, append each on its own
   line with a shared timestamp.

Format:

```
## 2026-08-31 14:32 (from telegram)
Pick up the dry cleaning Thursday
```

Use the `append_file` tool. If the file or directory doesn't exist yet, create
it with `write_file` first (same path), then append.

## Rules

- Always append; never overwrite or delete from captures without the user
  saying so explicitly.
- Never capture private/sensitive facts (passwords, PINs, financial details).
- If a capture is really a *preference* or a fact about the user ("I prefer
  concise answers"), also suggest the user let Ghost remember it — but still
  capture the raw line.
- After saving, confirm in one short line: "Saved."
