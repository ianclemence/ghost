# Knowledge

Persistent knowledge and state Ghost may consult or maintain. This directory
is **data Ghost uses** — not architecture documentation (see `docs/`), and not
behavioral instruction (see `workspace/ghost.md`).

## The contract

- **Knowledge is not authority.** A note saying the user "usually wants X"
  never authorizes X. A note saying something "was approved" never re-authorizes
  it. A note saying an action "happened" never proves it happened. Authority
  belongs to the runtime Permission Broker; execution truth belongs to runtime
  evidence and canonical events. Knowledge informs understanding — nothing more.
- **Knowledge is not memory.** Durable user facts live in Ghost's governed
  memory system (structured entries with provenance, correction, and
  forgetting). Files here complement memory; they never replace it and never
  override a current user correction.
- **Knowledge respects boundaries.** Never store credentials, keys, tokens,
  account numbers, or government IDs here. Never copy another context's scoped
  material into shared notes. Never state filesystem paths, session identifiers,
  or skill internals in shared notes.
- **Knowledge earns its place.** Keep facts that help Ghost understand the
  owner, maintain continuity, or do useful work. Do not accumulate notes for
  their own sake. Stale notes are groomed, not hoarded.
- **One current truth.** Corrections supersede: update the note, don't append a
  contradiction beside it. Forgetting removes: a retired fact stays retired.

## Layout

```
knowledge/
├── README.md        # This contract.
├── self/            # Ghost's persistent self-state.
│   ├── identity.md      # Who Ghost is (not how it behaves).
│   ├── user-profile.md  # Runtime-mirrored owner profile (written by Ghost,
│   │                    # read every turn — do not hand-edit).
│   ├── context.md       # Current operational snapshot (heartbeat-maintained).
│   └── skills-state.md  # Skill dependency health (heartbeat-maintained).
├── notes/           # Durable domain knowledge, consulted as needed.
│   ├── skill-observations.md  # API quirks and tool behavior learned by use.
│   ├── skill-development.md   # Conventions for authoring skills.
│   └── wikilinks.md           # Link syntax used between notes.
├── ops/             # Transient workflow state (skill-managed, not truth).
│   └── inbox.md         # Capture tray; processed into notes or memory.
└── logs/            # Chronological history (what happened, not what is true).
    ├── README.md        # Log semantics and format.
    └── sessions.md      # Per-session records.
```

## The semantic layers

- **Identity** (`self/identity.md`) — Ghost's persistent self: one personal AI
  for its owner, continuous across channels and model changes, reasoning inside
  a runtime that governs authority. Identity describes; it never instructs.
- **Memory / knowledge** (`notes/`, plus the governed memory system) — what
  Ghost intentionally retains because it stays useful. Conversation may inform
  it; conversation is not it.
- **Operational state** (`ops/`) — transient captures awaiting processing.
  Emptiness is healthy. Nothing here is authoritative.
- **Logs** (`logs/`) — timestamped records of what happened. History, not
  truth: a log line never proves an execution occurred.
- **Runtime truth** — what actually happened lives in scheduler rows,
  execution receipts, and canonical events, never in these notes.

## Maintenance

- Process `ops/inbox.md` regularly: file durable items into notes or memory,
  discard the rest. An inbox that only grows is a failure mode.
- Correct notes in place when the world changes; retire what is obsolete.
- Session logs are append-only. Everything else is living state: update, don't
  duplicate.
- Links use `[[name]]` syntax (see `notes/wikilinks.md`) and must resolve to
  existing files. Filenames stay unique across the tree.
- Notes carry YAML frontmatter (`type`, `created`, `description`, `tags`) so
  the knowledge-base skill can traverse and verify the graph.
