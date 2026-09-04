# Ghost Workspace

This directory is Ghost's persistent runtime context: identity rules, user profile, memory artifacts, and scheduled behavior definitions. Everything in this directory survives restarts and is the foundation of Ghost's personality and capabilities.

## Documentation Files

| File | Purpose |
|------|---------|
| `GHOST.md` | **Authoritative behavior specification.** Defines Ghost's identity, personality, memory system, tools, scheduling, safety, and communication style. Core directives, operational standards, and principles. |
| `SOUL.md` | Personality traits and communication style. Defines who Ghost is at a character level. |
| `USER.md` | Durable user facts and preferences. User identity, communication style, and system preferences. |
| `AGENTS.md` | Instructions for sub-agents and tool interactions. Conventions, resources, and behavior rules. |
| `HEARTBEAT.md` | Autonomous background routines. Scheduled tasks with guardrails and runtime budgets. |
| `README.md` | This file. Workspace structure and conventions. |

## Source of Truth

When files conflict, precedence is:
1. `GHOST.md` — complete behavior specification and execution policy
2. `USER.md` — user-specific preferences
3. `HEARTBEAT.md` — autonomous scheduling policy

## Directories

### Memory (`memory/`)

Long-term agent memory organized by month.

- `MEMORY.md` — legacy distilled notes. The live per-turn context is the bounded Active Context Digest from personal-context, not this file.
- `YYYYMM/YYYYMMDD.md` — daily conversation logs with human-titled timestamped entries. Append-only journals. Never surface raw `YYYYMM/filename` paths as user-facing titles.

Excluded from git tracking.

### Personal Context (`personal-context/`)

Structured memory store. The canonical memory system for durable facts.

- `entries.jsonl` — JSONL file where each line is a typed memory entry (facts, preferences, relationships, goals) with confidence scores, source attribution, and status (current/rejected/archived).

### Knowledge (`knowledge/`)

Long-term knowledge storage and reference material.

- `self/user-profile.md` — auto-updated profile of the user built from conversations. Contains durable facts: name, location, work, goals, preferences, relationships.

### Skills (`skills/`)

Installed skill packs organized by domain. Each skill is a directory containing a `SKILL.md` with instructions, plus optional helper scripts. Ghost reads these to learn how to perform specific tasks.

### State (`state/`)

Persistent machine state that survives restarts.

- `identity.json` — Ghost's unique ID, pod ID, owner name, creation timestamp
- `state.json` — current runtime state (last active channel, last active chat, timestamp)
- `evolution/` — behavioral evolution tracking: learned patterns about user behavior and communication preferences

### Sessions (`sessions/`)

Conversation session storage. Live storage is SQLite (`ghost.db` messages/sessions tables with FTS); the directory remains for file-based exports.

## Architecture Docs

- `docs/SKILLS.md` — skill product tiers (core / optional packs / dev-docs)
- Capability contract, readiness states (`ready` / `needs_user_input` / `needs_configuration`), pending-resume continuations, provider resilience, and mobile metadata contract (`timezone`, `city`, `latitude`, `longitude`) live in code: `pkg/skills/capability.go`, `pkg/skills/readiness.go`, `pkg/skills/pending.go`, `pkg/skills/provider.go`, `pkg/tools/timezone.go`
- Integrations setup (Calendar device flow, flight key, Home Assistant) lives in Web Console → Connections → Integrations

### Cron (`cron/`)

Cron job definitions. `jobs.json` contains scheduled recurring tasks that run independently of user messages.

### Data (`data/`)

Ad-hoc data files the agent creates or manages during tasks — shopping lists, captured notes, reminders, or any scratch data from conversations.

### Journal (`journal/`)

Activity journal. Reserved for structured activity logs.

### Temporary (`tmp/`)

Safe workspace-scoped outputs. Can be regenerated — safe to clean.

## Customization

- Edit `GHOST.md` to change Ghost's complete behavior specification
- Edit `SOUL.md` to adjust personality traits and communication style
- Edit `USER.md` to update user-specific preferences
- Edit `HEARTBEAT.md` to tune periodic routines and runtime budgets
