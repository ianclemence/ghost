# Ghost Workspace

This directory is Ghost's persistent runtime context: identity rules, user profile, memory artifacts, and scheduled behavior definitions.

## Bootstrap Files

- **`GHOST.md`**: Primary identity, execution constraints, and tool usage policy.
- **`HEARTBEAT.md`**: Scheduled autonomous tasks with guardrails and runtime budgets.
- **`USER.md`**: Durable user facts and behavior preferences.

## Knowledge Graph (`knowledge/`)

Structured operational knowledge using the Three-Space Architecture:

- **`knowledge/self/`**: Persistent working memory — identity, current context, channel status, skill health, error log.
- **`knowledge/notes/`**: MOCs (projects, areas, resources), concept notes, skill observations.
- **`knowledge/ops/`**: Transient operational state — inbox captures, task queue.
- **`knowledge/logs/`**: Timestamp-ordered session and error records.

## Skills (`skills/`)

Installed skill packs organized by domain. Each skill is a directory containing a `SKILL.md` plus optional scripts, references, and templates.

## State (`state/`)

Runtime state managed by the agent — last active channel, timestamps, other dynamic variables. Automatically updated. Excluded from git tracking.

## Memory (`memory/`)

Long-term agent memory. **`memory/MEMORY.md`** grows over time and is excluded from git tracking.

## Temporary Artifacts (`tmp/`)

Safe workspace-scoped outputs. Can be regenerated — safe to clean.

## Source of Truth

If two files conflict, precedence is:
1. `GHOST.md` for behavior and execution policy
2. `USER.md` for user-specific preferences
3. `HEARTBEAT.md` for autonomous scheduling policy

## Customization

- Edit `GHOST.md` to change decision policy, risk posture, and tool routing rules.
- Edit `HEARTBEAT.md` to tune periodic routines and runtime budgets.
- Edit `USER.md` to set stable user preferences and communication defaults.
- Edit `knowledge/ops/inbox.md` to capture task ideas for later processing.
