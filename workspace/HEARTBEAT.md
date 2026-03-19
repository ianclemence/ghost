# Periodic Tasks (Heartbeat)

This file defines autonomous background routines. Heartbeat tasks must never degrade active chat responsiveness.

## Global Guardrails

- Maximum heartbeat runtime per cycle: 120 seconds.
- Maximum single task runtime: 45 seconds.
- If active user chat is in progress, defer non-critical heartbeat tasks.
- On network failure, retry once after 5 minutes, then skip until next scheduled run.
- Every task must be idempotent: re-running should not duplicate user-facing outputs.

## Priority Levels

- P0: System health and safety alerts.
- P1: Time-based briefings and reminders.
- P2: Knowledge grooming and cleanup.
- P3: Weekly optimization and suggestions.

## Morning Routine (08:00, Local Time)

- [ ] Check system time and timezone consistency.
- [ ] Collect top tech and world headlines (max 5 sources total).
- [ ] Review `state/state.json` for pending multi-day tasks.
- [ ] Send one concise morning briefing to the primary configured channel.

Output constraints:
- One briefing message only.
- Max 12 lines.
- Include timestamp and one actionable highlight.

## Evening Reflection (22:00, Local Time)

- [ ] Summarize significant interactions from the day.
- [ ] Append one structured entry to `knowledge/logs/sessions.md`.
- [ ] Update `knowledge/self/context.md` with current topic and session outcome.
- [ ] Archive obsolete `state/` files older than 7 days.

Output constraints:
- One session log entry per day.
- No duplicate entries for the same date key.

## Maintenance (Every 4 Hours)

- [ ] Check CPU temperature, disk usage, and memory pressure.
- [ ] Raise alert if thresholds are exceeded:
  - CPU temp >= 80°C
  - Disk usage >= 90%
  - Available RAM < 10%
- [ ] Perform memory grooming only if `memory/MEMORY.md` exceeds 2 MB.

## Continuous Learning (Weekly)

- [ ] List installed skills and short descriptions (`ghost skills list`).
- [ ] Run `ghost doctor` and note any new skill dependency issues.
- [ ] Review `knowledge/ops/inbox.md` — process any stale captures (>48h).
- [ ] Recommend up to 3 improvements based on recurring user requests.
- [ ] Avoid repeating prior recommendations from the last 14 days.

## Skill Health Check (Daily)

- [ ] Run `ghost doctor` skill dependency check.
- [ ] Update `knowledge/self/skills-state.md` with current missing dependencies.
- [ ] Flag any skill whose prerequisite binary is permanently unavailable.
