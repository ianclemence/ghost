---
type: area
created: 2026-02-18
updated: 2026-09-12
tags: [skills, dev]
description: Conventions for creating and refining Ghost skills.
---

# Skill Development

Conventions for creating, maintaining, and improving Ghost skills. Skills
are knowledge and procedure — they never grant authority and never create
automation by themselves.

## Skills Registry

See [[skills-state]] for skill health and dependency status.

## Skill Standards

When creating or updating skills, follow these conventions:

- **Frontmatter**: `name`, `description` (with trigger phrases), `version`, `author`, `license`, `metadata.ghost.tags`, `prerequisites.commands`
- **Structure**: Quick reference table → primary method → detailed sections → error handling → troubleshooting
- **Description**: Always include "Invoke when user asks..." with concrete trigger phrases
- **Cross-linking**: Link to related skills in `metadata.ghost.tags` and body text
- **No authority claims**: A skill describes how to do a task with Ghost's
  capabilities. It must never imply it authorizes actions, owns credentials,
  or runs on its own.

## Related

- [[skill-observations]] — Operational notes on skill behavior
- [[wikilinks]] — Link syntax used between notes
