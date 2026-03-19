---
type: area
created: 2026-02-18
updated: 2026-03-19
tags: [skills, dev]
description: Focus area for creating and refining agent skills.
---

# Skill Development

## Scope

Creating, maintaining, and improving skills for the Ghost agent.

## Active Efforts

- [[skill-graph-implementation]] — Archived: completed 2026-03-19
- [[skill-observations]] — Live notes on API quirks and tool behavior

## Skills Registry

See [[self/skills-state]] for skill health and dependency status.

All skill files: [[workspace/skills/]]

## Skill Standards

When creating or updating skills, follow these conventions:

- **Frontmatter**: `name`, `description` (with trigger phrases), `version`, `author`, `license`, `metadata.ghost.tags`, `prerequisites.commands`
- **Structure**: Quick reference table → primary method → detailed sections → error handling → troubleshooting
- **Description**: Always include "Invoke when user asks..." with concrete trigger phrases
- **Cross-linking**: Link to related skills in `metadata.ghost.tags` and body text

## Related

- [[skill-observations]] — Operational notes on skill behavior
- [[knowledge-management]] — Where skill development fits in the knowledge system
