---
type: area
created: 2026-02-18
updated: 2026-03-19
tags: [knowledge, system]
description: Practices for maintaining the knowledge graph.
---

# Knowledge Management

## Scope

Maintaining the integrity and utility of Ghost's knowledge graph.

## Practices

- [[methodology]] — Three-space architecture and daily review habit
- [[wikilinks]] — Graph edge syntax and conventions

## Daily Review Habit

1. Update [[self/context]] with current session state
2. Review [[ops/inbox]] — process or discard captures
3. Update [[ops/tasks]] — mark completed items, add new ones
4. Log significant events to [[logs/sessions]]

## Skill Observations

See [[skill-observations]] for API quirks and tool behavior notes accumulated through use.

## Knowledge Graph Structure

```
self/       — Identity, current context, channel state, skill health, errors
notes/      — MOCs, area notes, project notes, concept notes
  areas/    — Hub notes linking to related knowledge
  projects/ — Project tracking (active and archived)
  references/ — Ingested external documents
ops/        — Inbox, task queue (transient)
logs/       — Session logs, error logs (timestamped records)
```

## Related

- [[ops/inbox]]
- [[ops/tasks]]
- [[logs/]]
- [[skill-development]] — Skill-specific knowledge management
