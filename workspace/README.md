# Ghost Workspace

This directory is Ghost's working persistence layer: the model-facing
behavioral contract, the owner profile projection, routine definitions,
durable memory, runtime-owned state, and disposable scratch — for one
installation. It is **not** the runtime itself. The runtime (authority,
execution, evidence, canonical truth) lives in code; this tree holds what
that runtime reads, writes, and consults.

Related layers: `docs/` explains what the runtime is. `workspace/ghost.md`
is the model-facing behavioral contract. `workspace/knowledge/` holds
persistent semantic knowledge under its own contract.

## Ownership, not precedence

There is no precedence ladder between these files. Each concept has one
owner, and files that describe the same concept say which one is canonical:

| Concept | Canonical owner | Workspace representation |
|---|---|---|
| Model behavior | `workspace/ghost.md` (injected every turn) | Persona flavor in `SOUL.md` (subordinate) |
| Owner memory | `personal-context/entries.jsonl` (governed store) | Projections: `USER.md`, `knowledge/self/user-profile.md`, `memory/MEMORY.md` |
| Authority | Runtime Permission Broker | Nothing here grants permission |
| Execution truth | Runtime evidence + canonical events | Logs/notes describe; they never prove |
| Routines | Routine subsystem + scheduler (SQLite) | Prose definitions in `HEARTBEAT.md` |
| Credentials | Runtime Vault | Never stored anywhere in this tree |
| Machine identity | `state/identity.json` (runtime-minted) | — |
| Conversation history | `ghost.db` messages/sessions (SQLite) | `sessions/` holds file exports only |

Workspace files inform understanding. They never authorize actions, never
prove executions, and never override the runtime, memory, or the user's
live words.

## Documentation Files

| File | Purpose |
|------|---------|
| `GHOST.md` | **Model-facing behavioral contract.** Identity, reasoning, capability, authority, evidence, memory, routines, artifacts, personality, safety, communication. |
| `SOUL.md` | Persona flavor only (traits, voice, boundaries). Subordinate to `GHOST.md`; never restates behavior rules. |
| `USER.md` | Projection of durable owner facts for prompt context. Synced from structured memory; placeholders until set. Not independently authoritative. |
| `AGENTS.md` | Workspace conventions and resource pointers for agentic work. Advisory; subagents run under capability-scoped runtime governance, not this file. |
| `HEARTBEAT.md` | Prose definitions of background routines, interpreted by the model each tick. Advisory routines, not runtime configuration; the scheduler owns execution. |
| `README.md` | This file. Workspace map and ownership. |

## Directories

### Memory (`memory/`)

Durable human-readable memory, dual-sinked with the vector index.

- `MEMORY.md` — distilled notes. Actively written (remember tool, consolidation backfill) and searched (memory recall). Not legacy.
- `YYYYMM/YYYYMMDD.md` — daily conversation journals (system-authored summaries) plus user-facing entries. Append-only; history, not verdicts.

Regenerable from structured memory + history. Excluded from git tracking.

### Personal Context (`personal-context/`)

Structured memory store. **The canonical memory system for durable facts.**

- `entries.jsonl` — typed entries (facts, preferences, relationships, goals) with confidence, provenance, and lifecycle status. Governed CRUD: corrections supersede, forgetting retires.

### Knowledge (`knowledge/`)

Persistent semantic knowledge under its own contract (see `knowledge/README.md`).

- `self/user-profile.md` — runtime-mirrored owner profile, injected into prompts. Hands off; the mirror is maintained by Ghost.

### Skills (`skills/`)

Installed skill packs: knowledge and procedure for performing tasks. A skill
declares what it needs; it never grants authority and never creates
automation by itself. Managed by the skill system (bundled manifest).

### State (`state/`)

Runtime-owned machine state. Opaque to the model; never hand-edit.

- `identity.json` — Ghost's unique ID (pairing, continuity, event attribution). Deleting it mints a new identity.
- `state.json` — last channel/chat routing hints. Deleting it degrades delivery, nothing else.
- `evolution/`, `turns/`, `failure-corpus.jsonl`, `device-ops/` — derived/auxiliary runtime state. Safe to lose; regenerated.

### Sessions (`sessions/`)

File-based session transcripts. Legacy/compat surface: live storage is
SQLite (`ghost.db`), this directory serves exports and the opt-in file store.

### Cron (`cron/`)

Retired scheduler artifact. The JSON cron engine no longer exists; the
authoritative scheduler owns all timing. `jobs.json` persists only as a
migration source (imported once at boot) and a factory-reset stub.

### Data (`data/`)

Live-only user inbox: captures, due reminders, shopping lists, scratch from
conversations. Created on demand by the runtime and skills; absent on fresh
checkouts. Content loss if deleted, never a crash. (Note: not covered by
state export — keep nothing irreplaceable here that isn't also in memory.)

### Journal (`journal/`)

Manual user journal owned by the journal skill (`YYYY-MM-DD.md` notes the
user asked to keep). Distinct from system-authored `memory/` daily notes
(which log what happened) and from activity/canonical events (which record
what the runtime did). User-authored, never authoritative.

### Events (`events/`)

Transient debug trail (`YYYY-MM-DD.ndjson`, 30-day retention). The durable
record is the `canonical_events` table; these files are write-only
diagnostics, protected from generic reads.

### Temporary (`tmp/`)

Disposable workspace-scoped outputs (screenshots, renders, drops). Pruned
after 7 days. Anything here may vanish; never treat it as memory or truth.

### Live database (`ghost.db*`)

SQLite store for sessions, messages, memory chunks, schedules, events.
WAL mode; never log-rotated (rows pruned, file retained). Ignored by git.

## Durability

Not everything here survives equally, and not everything should:

- **Durable**: `personal-context/`, `memory/`, `knowledge/` seeds, `state/identity.json`, `ghost.db` rows (per retention), user-authored `journal/` and `data/`.
- **Regenerable**: `USER.md` (placeholder), `state/` auxiliaries, `tmp/`, heartbeat telemetry snapshots.
- **Disposable**: `tmp/` contents, `events/*.ndjson`, `ghost.db-shm/-wal`, logs.
- **Per-installation, never committed**: `USER.md`, live telemetry (`context.md`, `skills-state.md`, `inbox.md`, `sessions.md`), all of the above except tracked seeds.

## Customization

- Edit `GHOST.md` to change model behavior (deploys to prompts).
- Edit `SOUL.md` for persona flavor within `GHOST.md`'s contract.
- Edit `USER.md` placeholders freely; structured memory stays canonical.
- Edit `HEARTBEAT.md` to tune background routine prose (advisory; execution stays scheduler-owned).
- Never store secrets anywhere in this tree. Never write instructions that claim authority, finality, or proof — the runtime owns those.
