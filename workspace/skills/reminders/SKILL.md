---
name: reminders
description: Keep track of one-off reminders the user will want later ("remind me to call mum Saturday"). Invoke when the user says "remind me to", "remind me that", "don't let me forget", "set a reminder", "what reminders do I have", or "cancel that reminder". Works fully on-device with a simple file. No API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [reminders, todo, memory, follow-up]
---

# Reminders

Small, personal nudges. This is *not* the automations system (cron schedules —
recurring jobs). Reminders are one-off things the user doesn't want to forget.

## Storage

A single file, one reminder per line:

```
workspace/data/reminders.md
```

Each line is `when — what`. Example:

```
Saturday — call Mum
2026-09-03 15:00 — renew passport
next Tuesday — book dentist
```

Keep `when` human but durable (a date or a clear day-of-week, not "later") so
it stays meaningful.

## Actions

- **Set:** append a line with `append_file`. Confirm in one line: "Noted — I'll
  keep that in mind."
- **List:** read the file with `read_file` and repeat the upcoming ones, most
  urgent first. If there are none: "No reminders right now."
- **Clear:** when a reminder is done or the user says "cancel", edit the file
  with `edit_file` to remove just that line and confirm.
- **Check daily:** I read this file during the daily briefing and surface
  anything due today.

## Rules

- Never guess a time the user didn't give. If they say "soon", ask — or keep
  it as a standing note and surface it each day until resolved.
- Reminders are for the user's own tasks, not Ghost's scheduled jobs (those go
  in Automations).
- Do not store sensitive info (PINs, passwords, account numbers).
