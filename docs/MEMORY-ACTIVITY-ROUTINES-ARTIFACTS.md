# Memory, Activity, Routines & Artifacts

These are the systems downstream of canonical execution. They turn Ghost's
runtime record into persistent behavior and durable outputs.

---

# Memory

Memory is Ghost's persistent information about the user, conversations, and
interactions. It is governed, scoped, and durable — not an uncontrolled
transcript dump.

## What Ghost remembers

- **Beliefs** — identity facts, preferences, decisions, relationships, goals —
  stored as structured entries with provenance.
- **Conversation history** — sessions and messages.
- **Knowledge** — retrieved documents and notes.
- **Profiles** — curated user and context profiles.

## Provenance and trust

Every belief carries its provenance: whether it came from an explicit user
statement, an observation, or a model inference. Trust is derived from the
strongest source. A model inference is evidence, not authority, and is held as
uncertain unless it meets a confidence threshold. Network-derived content is
treated as untrusted and never promoted on its own.

## Writes and retrieval

Memory is written through governed paths: per-turn extraction, explicit user
requests, and consolidation. Retrieval is scoped: a belief visible in one
context is not automatically visible in another. The runtime assembles a
bounded memory digest for each turn rather than injecting everything.

## Context and security boundaries

Memory respects context/session boundaries. Cross-context recall is prevented
by scope, not by prompt instruction. Credentials and secrets never enter
memory.

## Correction and forgetting

The user can correct or forget. Forgetting retires a belief (it is marked
rejected, not silently erased from history) and can delete a session's
conversation evidence. Corrections supersede the prior belief so exactly one
current value remains.

## Relationship to conversation

Conversation is not memory. A conversation can inform memory through governed
extraction, but memory is a curated, provenance-bearing store, not a raw
transcript.

## Phone collection and op sync

An offline phone collects without governing: `remember ...` queues a
`shared_durable` op, and queued chat turns wait in the outbox. On reconnect
the phone pushes ops to `POST /v1/sync/ops` and pulls Pod ops; application is
idempotent (`op_id` dedupe, version-ordered), so retries never duplicate.
Phone-local facts are origin-stamped, never promoted above Pod-curated belief
until merged through the governed path. Implementation: `pkg/memsync` on the
Pod, `ghost-sync.db` on the phone.

---

# Activity

Activity is the human-facing representation of what Ghost actually did or is
doing.

## What it shows

Activity is presented as compact, safe cards (chips) with a lifecycle state:

| State | Meaning |
|---|---|
| running | In progress |
| waiting | Blocked on approval or input |
| success | Completed |
| failed | Did not complete |
| cancelled | Stopped |
| paused | Held |

A chip expands for detail, and explicit diagnostics can show provider and
latency.

## Derived from runtime truth

Activity is projected from **safe canonical events**, not from model claims.
Raw tool names, manifests, schemas, prompts, and secrets cannot reach a chip;
this is structural, not prompt-based.

## Not a second authority

Activity reflects runtime state. It does not authorize anything and is not a
source of authority.

---

# Routines

**Routines** is the single owner-facing destination for anything Ghost runs on
its own: a reminder, a recurring instruction, a scheduled action, or a task.

The owner never chooses between the internal models. They express an intent —
"every Monday at 9, prepare my weekly brief" — and Ghost infers the shape. The
split between "Routines" and "Automations" was an internal taxonomy leaking
into the product; it no longer exists on any owner surface.

## One presentation boundary, not a new authority

`pkg/routinefeed` is a **read-only normalization**, not a storage or scheduling
layer. It reads the existing models and returns one feed:

```
pkg/routines (product view over scheduled automations)
pkg/scheduled (the one scheduler authority)
        ↓
     pkg/routinefeed   (normalize + infer shape + sort)
        ↓
  GET /v1/routinefeed (one feed for every surface)
```

It owns no tables, no scheduler, no permission. Kind (`reminder`, `routine`,
`automation`, `task`) and state (`active`, `paused`, `waiting`, `done`,
`failed`, `cancelled`) are inferred deterministically, and each inference
carries an audit reason. A routine appears exactly once even though it is also
a scheduled row. Human schedule phrasing reuses the scheduler's own humanizer
so no surface can disagree about what `0 9 * * 1-5` means.

The deep rules below still govern execution: a Thing is surfaced by
`pkg/routinefeed`, but it wakes Ghost through the scheduler and re-enters the
normal capability, permission, and evidence path like any other work.

---

# Routines

A **routine** is the user-facing product primitive for persistent, recurring
behavior. Scheduling is the implementation machinery beneath it.

## One scheduler

Ghost has one active scheduler authority (`pkg/scheduled`). The former JSON
cron engine (`pkg/cron`) is retired. Legacy scheduled jobs are migrated into
the authoritative scheduler idempotently.

## The canonical flow

```
ROUTINE
  ↓
SCHEDULER
  ↓
AGENT TURN
  ↓
CAPABILITY
  ↓
PERMISSION BROKER
  ↓
EXECUTION
  ↓
EVIDENCE
  ↓
EVENT
```

A routine wakes Ghost. It does not authorize anything. Each run re-enters the
normal capability, permission, and evidence path, so a routine whose authority
has been revoked fails or waits instead of executing.

## Recurring work and scheduled agent turns

Routines can be one-shot reminders or recurring automations. A recurring
routine runs an agent turn at each trigger, so it can use capabilities and
reason about its instruction — always under current governance.

## Missed runs and persistence

Routine state (schedule, run history, failures) is durable and survives
restarts. Missed runs are handled by policy rather than silently dropped.

## Skills and schedules

Installing or reading a skill does not create persistent automation. A skill
may describe a possible routine, but creating one is an explicit user action.

---

# Artifacts

An **artifact** is a concrete output Ghost produces or hands back: a file, a
generated document, or another bounded output.

## Runtime entities

Artifacts are runtime-managed entities with an identifier, kind, title, and
provenance. Publishing an artifact is a capability (`artifact.create`) that
produces evidence.

## Storage and confinement

Artifacts are stored and confined by the runtime. They are not raw model blobs
dropped into the filesystem.

## Conversation association and persistence

Artifacts are associated with the conversation that produced them and persist
across restarts.

## Follow-up operations

Acting on an artifact — sharing, delivering, transforming — is itself a
governed capability. An artifact does not become executable merely because the
model produced it:

> An artifact is an output, not an authority.

---

## Related concepts

See [Canonical Events](CANONICAL-EVENT.md) for the record these systems
consume, and [Permission Broker](PERMISSION-BROKER.md) for how routine and
artifact actions remain governed.

## Implementation

Memory lives in `pkg/personalcontext` and `pkg/rag`. Activity lives in
`pkg/activity`. Routines live in `pkg/routines` and the scheduler in
`pkg/scheduled`. Artifacts live in `pkg/artifacts`.
