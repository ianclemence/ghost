---
type: skills-state
created: 2026-03-19
updated: 2026-03-19
tags: [self, skills, dependencies, health]
description: Current state of skill system and known dependency issues.
---

# Skill Health

Current state of the skill system. Run `ghost doctor` for live dependency status.

## System Overview

- **Total skills**: ~40
- **Skill loader**: `pkg/skills/loader.go`
- **Dependency checker**: `pkg/skills/dependencies.go`
- **Skill discovery**: Three-tier (workspace → global → builtin)

## Last Dependency Check

Run: `ghost doctor` (check #6: skill_dependencies)

## Known Missing Dependencies

_(Update after each `ghost doctor` run)_

| Skill | Missing | Installed Via |
|-------|---------|---------------|
| calendar | gcalcli | `pip install gcalcli` |
| crypto | — | All available |
| flight | AVIATION_API_KEY | https://aviationstack.com |
| git | git | System binary |
| homeassistant | HASS_URL, HASS_TOKEN | Home Assistant instance |
| network | nmap | `apt-get install nmap` |
| organizer | — | All available |
| process-manager | — | All available |
| scraper | — | All available |
| shopping | — | All available |
| skill-creator | — | All available |
| speedtest | speedtest-cli | `pip install speedtest-cli` |
| spotify | CLI wrapper | Platform-specific |
| summarize | python | Built-in |
| system | — | All available |
| weather | — | All available |

## Skill Quality Summary

| Status | Count | Notes |
|--------|-------|-------|
| Complete (v1.1.0+) | 15 | Quick ref table + detailed sections |
| Functional (v1.0.0) | ~25 | Basic, needs depth |
| Needs audit | — | Excluded skills (github, research, etc.) |

## Skill Naming Conventions

- Skill name matches directory name
- `prerequisites.commands` lists required binaries
- `metadata.ghost.tags` for categorization
- Use `metadata.hermes` only for legacy skills not yet migrated

## Related

See [[skill-observations]] for notes on API quirks and tool behavior.
