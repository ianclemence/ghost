# AGENTS.md — Workspace Conventions

## Project Context

This is the workspace for Ghost, a personal AI runtime. These are working
conventions for agentic work here — advisory notes, not authority. Runtime
governance (capability-scoped tools, broker approval) binds every turn
including background turns; nothing in this file widens it.

## Behavior Reference

`GHOST.md` is the single authoritative behavior specification (identity, tools, memory, scheduling, safety, tone). `SOUL.md` holds persona only. Do not restate personality or tone rules here — follow `GHOST.md` (# Personality, # Interacting).

## Conventions

- Use markdown for all structured output
- Prefer code blocks with language tags for code examples
- Reference file paths as `path/to/file:line_number`
- When modifying code, follow existing patterns in the codebase
- Read a file before editing it; keep diffs minimal and local — change what the task names, nothing around it
- Match the surrounding style (naming, comment density, error handling) over any general preference you hold
- Tests live next to what they guard; a behavior change updates its test in the same change
- Never print, log, or echo secrets, API keys, tokens, or vault contents — reference them by name only

## Working With Skills

- When the task matches a listed skill, read its `SKILL.md` first and follow it in place of your default approach; its **Preferred path** names the native tool, and any CLI in it is the fallback
- A skill guides *how* — it never grants authority, never schedules anything, and the owner's explicit words outrank its guidelines
- If a skill blocks or pauses work, name the exact skill file and quote the instruction; if it doesn't require approval explicitly, proceed within scope

## Memory, State, and Data Boundaries

- `personal-context/entries.jsonl` is canonical for durable facts; corrections supersede, forgetting retires — never hand-edit it to "fix" a value
- `state/`, `sessions/`, `events/`, and `ghost.db` are runtime-owned: never hand-edit, never move, never delete; the conversation in `ghost.db` is never deleted
- If a query needs the database, open it read-only (`file:...?mode=ro`) and treat rows as evidence, not as something to mutate
- Never write secrets anywhere in this tree — credentials belong to the runtime vault

## Verifying and Reporting

1. Always verify facts before stating them
2. When unsure, say so — don't fabricate; label estimates as estimates
3. Prefer local tools over external APIs when possible
4. Respect the workspace boundary — don't access files outside it
5. Background journaling already logs to daily notes — don't double-log
6. Confirm scheduled items in your response, including the timezone
7. After work completes, report outcome first — what changed, why, how it was checked — then what remains uncertain; never a chronological dump of every step
8. Run the Go test suite before claiming a change is done; a failing or unrun test means not done

## Available Resources

- `personal-context/entries.jsonl` — Structured memory entries (canonical)
- `knowledge/self/user-profile.md` — Auto-updated user profile mirror
- `memory/YYYYMM/YYYYMMDD.md` — Daily conversation logs (human-titled entries)
- `skills/` — Installed skills, 1-level only (read SKILL.md for each)
- `GHOST.md` — Complete behavior specification
- `../docs/` — Runtime architecture reference (authority, capability, broker, evidence)
