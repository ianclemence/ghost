---
name: timer
description: Set short one-shot countdown timers. Invoke when user asks "set a timer for 10 minutes", "timer 5 min for pasta", "countdown 30 seconds", "wake me in 20 minutes", or any minutes/seconds countdown. Ephemeral by design — fires once then disappears, unlike durable reminders.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [timer, countdown, alarm, pomodoro]
prerequisites:
  commands: []
---

# Timer

Ephemeral countdowns. Fires once, then auto-deletes — unlike `reminders`/`schedule` automations which persist.

> **Mandatory:** Use the `schedule` tool with a one-time relative message. Do NOT use `cron` (recurring), `exec sleep` (blocks), or `web_search`. After creating, confirm duration and what happens at fire time.

## Quick Reference

| Task | Command (via schedule tool) |
|------|------------------------------|
| 10-min timer | message: "in 10 minutes to check the pasta timer" |
| 30-sec countdown | message: "in 30 seconds to check the timer" |
| Pomodoro 25 min | message: "in 25 minutes to end the pomodoro timer" |

Always include the word "timer" in the content so it reads as a countdown, not a calendar event.

## Lifecycle

One-time items auto-delete after firing (`DeleteAfterRun` / `IsOneTime` in the scheduler). Never create recurring schedules for timers. If the user gives no duration, ask naturally: "How long should the timer run?" and resume when they reply.

## Failure Behavior

No duration → ask once and wait. Invalid duration → state accepted forms (seconds, minutes, hours) and stop. Never invent a duration.
