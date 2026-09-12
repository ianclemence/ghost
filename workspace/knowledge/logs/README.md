---
type: logs
created: 2026-03-19
updated: 2026-09-12
tags: [logs, session, history]
description: Chronological history. What happened, not what is true.
---

# Logs

Timestamped records of sessions and significant events. Logs answer "what
happened" — they are history, not truth. A log line never proves an execution
occurred; execution truth lives in scheduler rows, execution receipts, and
canonical runtime events. Durable user facts belong in memory, not here.

## Session Log

- `sessions.md` — Per-session summaries: topics covered, decisions made,
  outcomes. One entry per significant session.

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

- Sessions accumulate as searchable history. Groom only when the file becomes
  unwieldy: summarize older spans, never rewrite them into false precision.
- Log at the **end of significant sessions**. Routine uneventful turns need
  no entry.
- Never store credentials, keys, or secrets in logs.
