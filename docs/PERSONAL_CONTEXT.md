# Personal Context Architecture

This document specifies the architecture for **Personal Context**, the layer that
separates the durable facts a Ghost knows about its person from the raw
conversations they are derived from. It supersedes the previous "Memory v1"
approach (MEMORY.md + RAG + kv_store) as the design for how Ghost remembers.

This is an architecture, not an implementation plan. The build order is defined
in [the vertical slice](#implementation-boundary) at the end.

## 1. Canonical model: two layers, one source of truth

Ghost holds two kinds of state about a person:

1. **Conversations** — the raw, timestamped record of every message exchanged.
   This is **evidence**. It is the source of truth.
2. **Personal Context** — a curated set of *entries* (facts, preferences,
   identities, relationships, goals, decisions, consents, routines) derived from
   conversations plus direct user input. This is an **operational
   representation**: a cache the agent consults so it does not have to re-read
   thousands of messages every turn.

The invariant is:

> **Conversations are the source of truth. Personal Context is a derived,
> lossy-in-detail but faithful-in-intent cache. Extraction never destroys
> evidence, and no entry may exist without a traceable source.**

Context may be rebuilt from conversations at any time. Conversations can never
be rebuilt from context.

## 2. What a conversation is

A conversation is a sequence of messages within a session:

- Each message has a role (`user`, `assistant`, `tool`), content, optional tool
  metadata, a creation timestamp, and a stable id.
- A session groups messages by channel and participant (e.g. `telegram:<id>`),
  and carries a rolling summary produced by the summarizer.

Conversations are **append-only evidence**. Editing history to change what the
Ghost "knows" is forbidden; correcting a belief is done by writing a new entry
(or superseding an old one), never by rewriting what was said.

## 3. What Personal Context is

A single unified entry type, persisted as append-only JSONL entries plus a
small SQLite index (non-canonical, rebuildable):

| field          | meaning                                                               |
|----------------|-----------------------------------------------------------------------|
| `id`           | stable entry id                                                       |
| `kind`         | `identity` \| `fact` \| `preference` \| `relationship` \| `goal` \| `decision` \| `consent` \| `routine` |
| `predicate`    | short label, e.g. `name`, `job`, `prefers_coffee`                     |
| `value`        | typed value                                                           |
| `status`       | `current` \| `superseded` \| `conflicting` \| `uncertain` \| `rejected` |
| `confidence`   | 0..1 (inferred entries only)                                          |
| `valid_from` / `valid_until` | optional temporal validity                                   |
| `superseded_by`| id of the entry that replaced this one                                |
| `sources`      | provenance: which messages/user inputs established, corrected, or reinforced the entry |

**Deliberate exclusions.** Documents, skills, and workflows are NOT entries:
they are files with their own lifecycle and are referenced from context, not
duplicated into it.

**The mapping function.** An extractor turns a conversation into entries:

```
extract(conversation) -> entries ∪ contradictions
```

The reverse direction does not exist: there is no `reconstruct(conversation)`
from entries.

## 4. Provenance

Every entry carries a `Source`:

```go
type Source struct {
    Type      string // "conversation" | "user_input" | "workflow" | "import" | "manual"
    Kind      string // user_declared | user_corrected | inferred | imported | manual | workflow
    Ref       string // session_id:message_id for conversations; "" otherwise
    Timestamp string // RFC3339
}
```

Rules:

- **Append-only.** Sources are never deleted, only added. An entry may gain
  sources over time (reinforcement).
- **Every entry must have ≥1 source** with a resolvable reference where the
  entry came from a conversation.
- A correction is a new entry that supersedes the old one and references the
  message that corrected it (`user_corrected`).

## 5. Contradiction handling

Contradictions are resolved by **revision and supersession**, not deletion:

1. **Supersession is primary.** When new evidence contradicts an entry, the
   old entry's status becomes `superseded`, `superseded_by` points at the new
   entry, and both remain on disk. History is never rewritten.
2. **Temporal validity is supplementary.** `valid_from` / `valid_until` cover
   the common "she used to live in X, now Y" case cleanly; they are an
   optimization over version chains, not a replacement.
3. **Unresolvable inference** is marked `conflicting` and surfaced to the user
   rather than silently merged. Ghost never guesses which of two
   contradictory statements is true when the user said both.
4. **Never merge silently.** Two entries are never quietly combined into a
   hedge.
5. **Tie-break order** when evidence conflicts: `user_declared` > `user_corrected`
   > newer evidence > higher confidence. If still tied, ask the person.

## 6. Forgetting

Forgetting is three distinct operations, not one:

| operation          | what happens                                                          |
|--------------------|-----------------------------------------------------------------------|
| **Retire**         | entry status → `rejected`/`superseded`; it stops appearing in the digest. Provenance is retained. This is the default. |
| **Purge derived**  | RAG chunks, summaries, and cached digests for a topic are removed. Workflows/skills that depend on a purged fact are flagged `stale-dependency`, not deleted. |
| **Delete evidence**| conversation messages themselves are removed (the only irreversible one), cascading to derived chunks. |

Notes:

- `/clear` (wiping conversation history) is **archiving**, not forgetting:
  messages are flagged archived, provenance survives, and nothing is destroyed.
  Forgetting is an explicit, deliberate act with its own commands.
- `forget` without a qualifier defaults to **Retire**, the reversible op.

## 7. Retrieval

Injection is bounded and layered. The agent does not slurp the context store
every turn.

1. **Active Context Digest** — a compact, bounded (~600 char) summary of only
   `status = current` entries, injected into every prompt. It answers the
   questions the Ghost must never get wrong (name, address, job, household,
   standing preferences).
2. **`context_get` tool** — targeted lookup on demand (the entry store, not the
   digest). The agent calls it when the digest is insufficient for the current
   task.
3. **`session_search`** — full-text search over conversations when the agent
   needs the raw evidence behind an entry.
4. **RAG (memory_chunks)** — content only. **Facts are never embedded.** An
   embedding is a fuzzy pointer, and a fact is a contract; a fact belongs in an
   entry, not in a vector.
5. **Workflows & skills** are loaded by reference when the task matches them;
   context does not inline them.

**Conflict discipline:** contradictions are resolved *before* injection, never
after. The digest only ever contains entries with a settled status.

**Structural staleness, not heuristic staleness:** staleness is a property of
the model (status + valid_until + the machine-state separation below), not a
prompt heuristic. There is no "trust the digest, distrust after N days" rule.

## 8. Personal vs machine state, and the Ghost State v1 correction

Two kinds of state are routinely conflated:

- **Personal state** — belongs to the person, must follow them across devices:
  identity, facts, preferences, relationships, goals, consents, decisions,
  routines, and the raw conversations that ground them.
- **Machine / runtime state** — belongs to this installation: which provider
  serves the current model, local networking, routing history, tool session
  state, heartbeats, agent-process state.

**Boundary test:** *"If a new hard drive can't know it, it's personal context.
If only this machine could have produced it, it's runtime state."*

Consequences:

- Machine lines must migrate out of `MEMORY.md` (e.g. "Ghost runs on an Ollama
  box at 192.168.x.x") into runtime state files that never travel.
- **Corrected classification of `ghost.db`.** An audit of the actual data (the
  current `cmd/ghost/workspace` at the time of writing: 3,102 messages across
  34 sessions, 0 session summaries, 0 memory chunks, 0 kv rows) plus a read of
  the Ghost State v1 code shows the database has never been REBOUND — it is
  classified `CategoryPortable` and exported as a single-file binary snapshot
  (`VACUUM INTO`). The portability *intent* was right; the *format* is wrong
  for the reasons below.

### Ghost State v1 amendment

| artifact               | v1 (current)                                  | amended                              |
|------------------------|-----------------------------------------------|--------------------------------------|
| `ghost.db`             | Portable, exported as binary snapshot         | **Rebound** — runtime index, rebuilt from conversations on import |
| `conversations/`       | (inside ghost.db)                             | **Portable** — new versioned, deterministic `conversations/*.jsonl` |
| embeddings / chunks    | (inside ghost.db snapshot)                    | **Derived** — rebuilt on import      |
| `personal-context/`    | —                                             | **Portable** — entries JSONL (future) |
| config, identity, skills, memory files, workflows | unchanged                 | unchanged                            |

Why a binary DB snapshot is the wrong portable format:

1. **Non-deterministic.** A `VACUUM INTO` snapshot embeds page layout, free
   lists, and schema-internal ids; the same logical content yields different
   bytes on different runs and different SQLite versions. Nothing can be
   diffed, versioned, or reviewed.
2. **Opaque.** A person cannot open a `.ghost` archive and read what the Ghost
   remembers; conversations are invisible to the eye and to tools.
3. **Binds the schema.** Moving the format to JSONL decouples the archive from
   the SQLite schema, so the store can change (or be replaced) without breaking
   portability.

The amended model:

- Conversations travel as `conversations/*.jsonl` (versioned, deterministic —
  see below).
- Import writes those files as the portable record **and** rehydrates a fresh
  `ghost.db` (schema + FTS index built by the DB layer, messages re-inserted in
  order) so the runtime works immediately on the new machine.
- Embeddings and other derived tables are deliberately not exported; they are
  rebuilt as the agent runs.

### The portable conversations format (v1)

```
conversations/
  format.json                     {"format":"ghost-conversations","version":1}
  sessions/<sha256(session)[:16]>-<sanitized-session-id>.jsonl
```

One file per session. Each file is JSONL: a session header line, then one
message per line, in chronological order (sorted by `created_at`, ties broken
by insertion order), each carrying an explicit `seq`.

- **Versioned:** the sub-format version lives in `format.json`; imports refuse
  an unknown version instead of guessing.
- **Deterministic:** filenames derive from the session id, sessions are sorted
  by id, messages by `(created_at, insertion order)`, no export-time timestamps
  or random ids enter the payload, and `meta` is preserved byte-for-byte.
  Exporting the same database twice yields identical conversation files.

## 9. Minimum viable architecture

The smallest coherent slice that proves the model:

1. **Store** — append-only `personal-context/entries.jsonl` with the Entry type,
   an in-memory index, and a rebuild path.
2. **Extractor** — rule-based extraction into entries (no LLM dependency; the
   LLM is an accelerator, not the foundation). One-shot extraction at
   conversation close plus a cheap streaming pass.
3. **Injector** — the bounded Active Context Digest replaces the MEMORY.md
   injection; `MEMORY.md` stops being authoritative and becomes an optional
   human-readable mirror.
4. **Tools & commands** — `context_get`; `/remember`, `/forget`, `/context`
   (user-facing), with `/forget` defaulting to retire.
5. **Conversations portability** — the amendment in §8.

## 10. Implementation boundary

The architecture is intentionally divided into vertical slices so each can be
built, tested, and shipped alone without changing unowned behavior:

| slice | scope | status |
|-------|-------|--------|
| **Conversation portability** | `conversations/*.jsonl` export/import replaces the binary `ghost.db` snapshot; fresh-DB rehydration on import; versioned + deterministic format with tests. Everything outside this slice keeps its current behavior. | **first** |
| Entries store | `Entry` type + `entries.jsonl` + index + rebuild | later |
| Rule-based extractor | conversation → entries with provenance | later |
| Digest injector | bounded active-context digest replaces MEMORY.md injection | later |
| Tools & commands | `context_get`, `/remember`, `/forget`, `/context` | later |
| Retirement of Memory v1 | RAG facts migration, kv_store deprecation, machine-state split | later |

Each slice keeps a proof-of-correctness scenario: the same user on a new
device, with only the portable archive, must be served as if nothing changed.