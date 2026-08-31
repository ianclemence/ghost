---
name: daily-briefing
description: Give a short, useful "what you should know today" briefing. Invoke when the user says "morning briefing", "what should I know today", "brief me", "what's on today", "what do I have going on", or "my daily summary". Reads the user's memory, notes, and captured items and composes a calm, personal summary. No API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [briefing, daily, summary, morning, review]
---

# Daily Briefing

The briefing is *the user's* day, not a generic weather report. It should feel
like a trusted assistant who knows them. Short, calm, and ordered by what
matters most to this person.

## Sources to read (in order)

1. `workspace/data/captures.md` — the latest unsnoozed captures (what they
   jotted down; these are *priorities* tonight).
2. `workspace/memory/MEMORY.md` and the most recent daily note
   (`workspace/memory/YYYYMM/YYYYMMDD.md`) — ongoing threads and context.
3. `workspace/HEARTBEAT.md` and any scheduled automations — what Ghost is
   supposed to be doing and anything due today.
4. `workspace/knowledge/self/user-profile.md` — who the user is, so the
   briefing is worded for them.
5. `workspace/data/reminders.md` — anything due today the user asked Ghost to
   keep on top of.
6. Current weather for the user's location, if it's known or easy to get.

Use the `read_file` tool for each. Some files may not exist yet — that's fine,
skip them.

## Style

- 5–8 bullets maximum, plain sentences, no headers or markdown unless the user
  clearly wants it.
- Lead with what is *due or time-sensitive* (today's tasks, reminders, captures
  that need action). Then one helpful thing they'd care about (weather for
  their location, a thread carrying on). Then one gentle prompt if something
  needs their input.
- If there's nothing pressing, keep it genuinely short: "Nothing urgent today.
  It's going to be {weather}. Want me to {useful suggestion}?"
- Never invent facts or tasks. If a source is empty, don't pad it.

## Rules

- Do not restate the user's whole history. Briefings are forward-looking.
- If the user asks for a "weekly" or specific briefing, adjust the time window
  accordingly.
- If nothing is known about the user yet (no memory, no captures), say so
  honestly and invite them to tell you a little.

## When run automatically (heartbeat)

When the heartbeat invokes this skill on its own (not on request), do this:

1. Read the sources as above.
2. Compose the briefing (same style).
3. **Save it** so it's visible and kept: write it to
   `workspace/memory/YYYYMM/YYYYMMDD-briefing.md` (create the folder if needed)
   using `write_file`. Include a title line with the date. This is what shows
   up in the user's Memory, so keep it calm and short.
4. If a reminder or capture is due today, mention it in the briefing and check
   `workspace/data/reminders.md` so it isn't lost.
5. If there is genuinely nothing to say (no memory, no captures, no due
   reminders, nothing scheduled), do **not** write a briefing — just finish
   briefly. Don't invent content.
6. Never duplicate: if a briefing for today already exists, update it rather
   than writing a new file.
