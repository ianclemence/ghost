---
type: methodology
created: 2026-02-18
updated: 2026-03-19
tags: [methodology, principles]
description: Operational principles and architectural standards for the knowledge graph.
---

# Methodology

This file documents the operational principles of this knowledge graph.

## Three-Space Architecture
- **self/**: Persistent memory — identity, current context, channel state, skill health, errors.
- **notes/**: The knowledge graph — interconnected notes on topics, skills, and projects.
- **ops/**: Transient operational state — inbox, task queue, logs.
- **logs/**: Timestamp records of sessions and significant events.

## Wikilinks
We use `[[wikilinks]]` to connect concepts.
- Use prose-friendly titles.
- Link generously to build context.

## Daily Review Habit

At the start of each session:
1. Read [[context]] — update with current topic and priorities
2. Review [[inbox]] — process captures, move stale items
3. Check [[tasks]] — update task statuses
4. Check [[skills-state]] — note any dependency issues

At the end of each significant session:
1. Log the session to [[logs/sessions]]
2. Note any errors or observations in [[recent-errors]]
3. Update [[inbox]] with new captures
