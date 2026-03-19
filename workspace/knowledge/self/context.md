---
type: context
created: 2026-03-19
updated: 2026-03-19
tags: [self, context, session]
description: Current session state and priorities. Updated each session.
---

# Current Context

Last updated: 2026-03-19 14:00 UTC

## Active Topic

Skill system overhaul and knowledge graph expansion.

## Session Summary

- Completed full audit and rewrite of 28 skill SKILL.md files
- Added dependency checker (pkg/skills/dependencies.go)
- Integrated skill dependency check into `ghost doctor`
- Expanded knowledge graph: ops/, self/ working docs, logs/
- Archived `skill-graph-implementation` project

## Current Priorities

1. Complete knowledge graph expansion tasks (this session)
2. Verify dependency checker integration works in production
3. Document API quirks as they accumulate

## Recent Decisions

- Skill prerequisites: prefer built-in Ghost tools over system binaries where available
- Skill quality: use Quick Reference tables, then detailed sections
- ops/ inbox: process within 24 hours to avoid stale captures

## User Preferences

_(Update as user expresses preferences)_

- Direct, straight-to-point responses
- No Socratic questioning unless genuinely ambiguous
- Windows development environment

## Notes

- Workspace: `d:\laragon\www\ghost`
- Skills: `workspace/skills/`
- Knowledge: `workspace/knowledge/`
