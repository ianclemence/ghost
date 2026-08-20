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

A single unified entry type, persisted as an append-only JSONL log plus an
in-memory index rebuilt from the log on load (non-canonical, rebuildable):

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
    Type      string // "conversation" | "command" | "document" | "workflow" | "import" | "manual_edit" | "agent_inference"
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

1. **Active Context Digest** — a compact, bounded (~600 byte) summary of only
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

### Rule-based extractor (v1): predicate mapping and grammar

The v1 extractor (`pkg/personalcontext/extractor.go`) is a small explicit
grammar, not general NLP. It is pure and deterministic: given a user message
plus session/message metadata and the current context it returns zero or more
actions (create / supersede), and it never requires a model. Persistence stays
in `Store`; the extractor only builds entries.

**Predicate mapping.** Namespaced predicates (`kind/name`) are stable and
documented here:

| pattern (case-insensitive)          | kind        | predicate                     | value (canonical JSON string, case preserved) |
|-------------------------------------|-------------|-------------------------------|-----------------------------------------------|
| `my favorite color is X`            | preference  | `preference/favorite_color`   | X                                             |
| `my name is X`                      | identity    | `identity/name`               | X                                             |
| `I live in X`                       | fact        | `fact/location`               | X (trailing " now" stripped)                  |
| `I prefer <style> answers`          | preference  | `preference/communication.style` | `<style>` (concise, brief, short, detailed, thorough, elaborate, direct, casual, formal, verbose) |
| `I like X`                          | preference  | `preference/likes`            | X (additive, low-signal captures rejected)    |
| `my goal is to X`                   | goal        | `goal/primary`                | X                                             |
| `I want to build X`                 | goal        | `goal/primary`                | `build X`                                     |

**Grammar.** Lead-ins are stripped before matching: correction markers
(`actually, `, `no, `, `that's wrong, `, `correction: `) and the memory command
language (`remember that ...`, `remember: ...`). Values are cut at the first
` and ` / ` but ` and bounded by clause punctuation so one message can declare
several facts ("my name is Ian and I live in Bangkok"). Deictic corrections
("actually, it's green") are resolved only from the immediately preceding user
turn and only when it yields exactly one declaration; otherwise no candidate is
produced.

**Lifecycle.** Declarations are `user_declared` (confidence 0.95); corrections
are `user_corrected` (confidence 1.0) and supersede the current entry for the
same subject+predicate. A correction with no current entry becomes a new
declaration. Restating the current value produces no action. `likes` entries
are additive and never superseded. Contradicting declarations supersede the
current value (supersession is primary, §5).

### Active Context Digest (v1)

The digest (`pkg/personalcontext/digest.go`) is a pure, deterministic,
LLM-free function: `BuildDigest(current []Entry, budget int) string`. It renders
only `status = current` entries in a bounded, prioritized summary that replaces
the old unbounded MEMORY.md + daily-notes dump in the system prompt. The
MEMORY.md file and `MemoryStore.GetMemoryContext` are preserved but no longer
injected.

**Format.** `## Personal Context` with a `<personal_context>` / `</personal_context>`
delimiter and one `- Label: value` bullet per entry, so prompt-injection looks
like data.

**Budget.** 600 bytes hard cap (`DigestBudget`). Entries are admitted in
priority order and dropped once the cap is hit; an individual value that alone
exceeds the cap is byte-safe truncated with `…`. `BuildDigest` returns `""` when
there is nothing to show, and the empty digest is not emitted.

**Priority.** identity (1) → important preferences (2) → communication
preferences (3) → goals (4) → relationships (5) → routines (6) → other facts
(7). Ties break by predicate then entry id, so output is stable across runs and
processes.

**Labels.** A fixed label map gives human names to the common predicates
(identity/name → Name, fact/location → Location, preference/favorite_color →
Favorite color, preference/communication.style → Communication style, goals →
Goal, …); unknown predicates fall back to the predicate suffix.

**Injection.** The digest is injected exactly once per turn by
`ContextBuilder.BuildSystemPrompt` (`pkg/agent/context.go`), which runs once per
model turn before the tool loop; tool iterations reuse the same messages, so the
digest is never duplicated. Subagents keep their own hardcoded system prompt and
receive no digest. Heartbeat turns pass through the same prompt path and receive
the digest; this follows the existing architecture and is accepted for v1.

**Relationship to `context_get`.** The digest is the always-on floor for facts
the Ghost must never get wrong; `context_get` is the on-demand lookup when the
digest is insufficient. Both read the same entry store; the digest is derived
and never mutated by tools.

### `/context` command (v1)

`/context` (`pkg/commands/context.go`) is the **user-facing** inspection
interface for Personal Context: what Ghost currently believes about the user. It
is not another retrieval mechanism. It reads `personalcontext.Store` directly
and **never** calls an LLM, never uses RAG, never searches conversations, and
never consults MEMORY.md.

**Syntax.** `/context [kind|subject|predicate] [--verbose]`.

**What it shows.** Only the current view: `Store.CurrentAt(now)` semantics
(status current, `valid_from <= now`, `valid_until >= now`). Superseded,
rejected/forgotten, expired, and future-valid entries never appear — they remain
inspectable only through the store's history/provenance queries. This is the
store's own lifecycle truth, not a second filtering layer.

**Output.** Compact, grouped by kind (Identity, Preferences, Facts, Goals, …)
with `predicate: value` lines — internal ids, timestamps, and provenance are
hidden. Conflicting and uncertain entries are never presented as facts: they are
listed under **Unresolved** (full predicate, one bullet per candidate value)
with an explicit "has not resolved these conflicts" note. An empty store returns
`Personal Context is empty.`; unresolved state with no current beliefs is shown
with `No current beliefs.` plus the Unresolved section. A missing store degrades
to a clear `Personal Context is unavailable.` message and never breaks the turn.

**Verbose.** `--verbose` (or `-v`) is the auditability surface: for every entry
it shows id, kind, subject, predicate, value, status, confidence, valid_from,
valid_until, superseded_by, created_at, updated_at, and each source (type, kind,
ref, timestamp) — exactly the fields and provenance the Entry/Source model
carries, never invented.

**Filtering.** Optional single argument: a kind (`/context preference`), an exact
predicate containing `/` (`/context fact/location`), or a subject
(`/context user`). Current entries come from `CurrentAt` filtered to the match;
unresolved state comes from the status-agnostic `ByKind`/`ByPredicate`/`BySubject`
queries. `history` is intentionally not surfaced — `/context` answers "what does
Ghost currently believe?", not "how did it change".

**Relationship to `context_get`.** `context_get` is the narrow, structured,
model-facing tool the agent invokes on demand; `/context` is the broad,
human-readable, user-initiated inspection command. Both read the same store; the
command renders, the tool queries.

### `/forget` command (v1)

`/forget` (`pkg/commands/forget.go`) is the **user-facing control** interface for
Personal Context. It is deterministic, never calls an LLM, never searches
conversations, and never touches RAG or MEMORY.md.

**The three operations.** Forgetting is three distinct operations (§6), and
`/forget` implements the two that are reachable from a chat:

- **Retire** (the default): `Store.Forget(id)` appends a `rejected` revision to
  the entry's append-only log. The entry stops appearing in the digest,
  `context_get`, and `/context` (all three read only current entries), while its
  record and provenance remain inspectable. **Conversation evidence is never
  touched.**
- **Delete evidence** (explicit): `/forget session <id>` removes the session
  transcript and summary through the session storage API (`DeleteSession` on the
  session store, wired through `SessionManager`), then retires any active
  Personal Context entries whose provenance references that session. Unrelated
  entries and other sessions are untouched. This is the only irreversible
  operation, and it is never reachable implicitly.

**Syntax.**

```
/forget <predicate>                preference/favorite_color
/forget <suffix>                   favorite_color
/forget <topic>                    my favorite color, location, communication style
/forget everything about <topic>   everything about my relationship with Jane
/forget session <session-id>       delete conversation evidence for a session
```

**Resolution.** A target phrase is matched against active entries — current and
temporally valid (the store's `CurrentAt` semantics, so expired and future-valid
entries are never touched) plus unresolved conflicting/uncertain entries — by
exact predicate, predicate suffix, a canonicalized phrase (lowercase, `my `
stripped, word separators collapsed, e.g. `my favorite color` →
`favorite_color`), or a known kind. Matching favors false negatives.

**Safety.**

- **Never a silent mass delete.** A bare `/forget everything` is refused.
  Generic self-referential topics (`me`, `myself`, `user`, `personal`, …) are
  refused. Distinct beliefs are never resolved silently: a phrase matching more
  than one belief lists the candidates (values shown when a predicate is shared)
  and asks the user to narrow it. A bare kind with parallel current entries
  (e.g. two `relationship/partner` values) is ambiguous and refused.
- **One contested belief, retired whole.** A conflicting pair for a single
  (subject, predicate) is one belief; `/forget` retires both, and `/context`
  then reports no unresolved state.
- **No duplicate work.** A second `/forget` of an already-retired entry reports
  `That Personal Context is already forgotten.` and appends no extra revision.
  A phrase with no current match reports
  `No current Personal Context entry matches …` and mutates nothing.
- **`everything about <topic>` is scoped.** It matches only structured,
  unambiguous targets: an exact subject, a named relationship partner
  (`relationship with/to X` → relationship entries whose value is X), or a
  predicate whose suffix contains the canonicalized topic. `everything about my
  relationship with X` retires only X's entries.

**Session deletion.** `/forget session <id>` requires the session to carry
evidence (a transcript or summary), reports `No session found with id "…"` for a
missing id, deletes the evidence, then retires dependent entries with
`Deleted session "…" and retired N dependent Personal Context entries.`. Derived
RAG chunks for a deleted session are not removed: the RAG store has no
session-scoped deletion API, so that cleanup is reported as a limitation rather
than silently attempted.

## 10. Ghost State portability (v1)

Personal Context travels through the same portable Ghost State archive as
conversations and every other personal artifact. The canonical artifact is the
append-only revision log itself, `personal-context/entries.jsonl` — the exact
file the store appends to — so nothing is derived, rendered, or regenerated
during export or import.

**Classification.** The whole `personal-context/` directory is classified
`portable` (`pkg/ghoststate/classify.go`). `ghost.db` stays `rebound`
(conversations rehydrate from the portable `conversations/*.jsonl` format), and
the RAG/embedding store stays `derived` — never exported, rebuilt on demand.
Because the directory classification and the store's own constants
(`personalcontext.EntriesDir`, `personalcontext.EntriesFile`) are the same
source of truth, the portable path can never drift from where the store writes.

**Export.** The walk stages the log byte-exact (`stageFromDisk`), records its
digest and size in the manifest as a portable file, and the archive is encrypted
and written atomically like any other export. Export stays deterministic: the
log is copied, never rewritten, so no timestamps or ordering are introduced.

**Import.** The archive's `personal-context/entries.jsonl` is validated with the
store's own parser (`personalcontext.ValidateEntries`) *before* it is written.
Every record survives or the import fails loudly with the offending line — a
malformed log can never silently become an empty context or a context with
dropped records. Valid bytes are then written verbatim, and
`personalcontext.Open(workspace)` on the target reads it directly: no
conversion, no rebuild, no LLM, no regeneration of entries from the imported
conversations. The imported store is a first-class store — `Current()`,
`CurrentAt(t)`, `History(id)`, `ByPredicate`, `BySubject`, `ByKind` and the
`/context`, `context_get`, and digest surfaces all serve the imported state.

**Lifecycle fidelity.** The append-only log carries the complete revision
history, so a round-trip preserves current entries, superseded chains
(`superseded_by`), rejected/forgotten entries, conflicting pairs, provenance
sources, temporal validity windows, confidence, and every historical revision.
Forgotten entries stay absent from the current surfaces (`/context`,
`context_get`, the digest) exactly as on the source machine, while remaining
present in `All()` and `History()`.

**Optional artifact.** A workspace that never used Personal Context exports no
`personal-context/` files and imports none; `Open` on the target creates the
same empty store as on the source. No placeholder file is fabricated.

**Determinism guarantee.** The imported log is byte-identical to the exported
log, and exporting the same workspace twice yields byte-identical archives for
Personal Context. The proof-of-correctness scenario holds: the same user on a
new device, with only the portable archive, is served as if nothing changed —
including the conversation evidence their context was extracted from.

The architecture is intentionally divided into vertical slices so each can be
built, tested, and shipped alone without changing unowned behavior:

| slice | scope | status |
|-------|-------|--------|
| **Conversation portability** | `conversations/*.jsonl` export/import replaces the binary `ghost.db` snapshot; fresh-DB rehydration on import; versioned + deterministic format with tests. Everything outside this slice keeps its current behavior. | **first** |
| **Ghost State portability of Personal Context** | `personal-context/entries.jsonl` classified Portable, exported byte-exact, imported byte-exact and directly loadable by the store — no conversion, no regeneration, no LLM during import | **built** (this slice, §10) |
| Entries store | `Entry` type + `entries.jsonl` + index + rebuild | **built** (Slices 4–5: store, lifecycle, conflict, temporal validity, concurrency hardening) |
| Rule-based extractor | conversation → entries with provenance | **built** (`pkg/personalcontext/extractor.go`) and wired into the agent turn loop (`pkg/agent/loop.go`) |
| Digest injector | bounded active-context digest replaces MEMORY.md injection | **built** (`pkg/personalcontext/digest.go`), injected via `ContextBuilder.BuildSystemPrompt` (`pkg/agent/context.go`) once per turn; MEMORY.md is no longer auto-injected |
| `context_get` tool | on-demand structured Personal Context lookup — filters by kind/subject/predicate, returns current entries plus explicit unresolved (conflicting/uncertain) state, preserves provenance; never automatic, never RAG | **built** (`pkg/tools/context_get.go`), wired into the agent tool registry (`pkg/agent/loop.go`) |
| User commands | `/context` (user-facing inspection) and `/forget` (user-facing control) | **built** (`/context` `pkg/commands/context.go`, `/forget` `pkg/commands/forget.go`), registered in `commands.DefaultDefinitions` and wired into the command runtime (`pkg/agent/loop.go`) |
| Retirement of Memory v1 | RAG facts migration, kv_store deprecation, machine-state split | later |

Each slice keeps a proof-of-correctness scenario: the same user on a new
device, with only the portable archive, must be served as if nothing changed.

## 11. Known limitations and deferred work

These are deliberate v1.1 boundaries, accepted after the final review. None are
release blockers.

**Correctness-adjacent, documented:**

- **Provenance refs use the turn's RequestID, not the persisted DB message id.**
  A `Source.Ref` is `session_key:<request_id>`. It is stable for the life of the
  store and sufficient for `/forget session` provenance matching, but it is not
  the conversation store's own message id.
- **JSONL appends are not fsynced.** A power loss immediately after an append
  can lose the trailing record. The in-memory index and the log stay
  consistent; no fsync is performed on the common path.
- **`DeclareConflict` appends two revisions without a transaction.** Both
  appends happen under the store lock, so no interleaving is possible, but a
  crash between the two leaves one side conflicting and the other still
  current.
- **Legacy or manually imported duplicate current entries are possible outside
  the normal paths.** The extractor/`Apply` re-resolution guarantees one current
  entry per `(subject, predicate)` for everything it writes; hand-written logs
  bypass that guarantee and `currentEntry` picks one deterministically.

**By design / conservative:**

- The rule-based extractor is deliberately conservative: it understands only
  the documented grammar, so most natural phrasing is simply not captured.
- Deictic corrections (`actually, it's green`) resolve only from an unambiguous
  immediately preceding user declaration; otherwise nothing is recorded.
- `I like X` is additive and never supersedes; only the first match per message
  is captured, and low-signal captures are rejected.

**Operational / environment:**

- `/forget session <id>` cannot remove derived RAG chunks: the RAG store has no
  session-scoped deletion API, so that cleanup is reported rather than
  silently attempted.
- Cron-triggered sessions are eligible for extraction except the heartbeat
  turn; this follows the existing turn-loop architecture.
- `/context <filter>` can report an empty match even when unrelated context
  exists; the message says what was matched, not that the store is empty.
- `-race` cannot run in this sandbox (TSan "unsupported VMA range"); the
  concurrency behavior is exercised with `-count` repeats instead.

**Deferred work (not started, by scope):** RAG/smarter extraction, `/remember`
user command, history/restore of rejected entries, Memory v1 retirement
(§9 slice table), and session-scoped RAG deletion for `/forget session`.