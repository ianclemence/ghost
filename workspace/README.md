# Ghost Workspace

This directory is Ghost's persistent runtime context: identity rules, user profile, memory artifacts, and scheduled behavior definitions.

## Directory Structure

- **`GHOST.md`**: Primary identity, execution constraints, and tool usage policy.
- **`HEARTBEAT.md`**: Scheduled autonomous tasks with guardrails and budgets.
- **`USER.md`**: Durable user facts and behavior preferences.
- **`knowledge/`**: Structured knowledge graph notes and operational knowledge.
- **`skills/`**: Installed skill packs (`<skill>/SKILL.md` plus optional scripts).
- **`tmp/`**: Temporary runtime artifacts (safe workspace-scoped outputs).

## Customization

- Edit `GHOST.md` to change decision policy, risk posture, and tool routing rules.
- Edit `HEARTBEAT.md` to tune periodic routines and runtime budgets.
- Edit `USER.md` to set stable user preferences and communication defaults.

## Source of Truth

- If two files conflict, precedence is:
  1. `GHOST.md` for behavior and execution policy
  2. `USER.md` for user-specific preferences
  3. `HEARTBEAT.md` for autonomous scheduling policy
