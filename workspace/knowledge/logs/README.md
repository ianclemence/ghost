---
type: logs
created: 2026-03-19
updated: 2026-03-19
tags: [logs, session, operations]
description: Session logs and error logs. Timestamp-ordered records of what happened.
---

# Logs

Timestamped records of sessions and significant events. Unlike [[ops/]] (task-oriented), logs are a chronological record of what happened.

## Session Log

- [[logs/sessions]] — Per-session summaries, topics covered, decisions made

## Error Log

- [[logs/errors]] — Errors encountered, their causes, and resolutions
  (synced with [[self/recent-errors]])

## Log Entry Format

Sessions follow this format:

```markdown
## YYYY-MM-DD HH:MM — Session Topic

**Duration**: ~N minutes
**Topic**: Brief description
**Outcome**: What was accomplished
**Decisions**: Key choices made
**Next**: Follow-up items for next session
```

## Retention

- Keep all sessions — they form a searchable history
- Error log entries are deduplicated against [[self/recent-errors]]
- Archive old logs (>6 months) by moving to [[logs/archive]]

## Update Schedule

- Log at the **end of every significant session**
- If a session produces no errors, note that explicitly
- Copy resolved errors to [[self/recent-errors]] immediately
