# Canonical Events

Canonical events are the durable, structured record of significant Ghost
activity. They are runtime-owned facts about what Ghost did.

## Why they exist

Activity, memory, routines, the UI, and auditing all need to know what
happened. If each derived its own answer from model output or ad-hoc logs, they
would disagree. Canonical events give them one authoritative source.

```
MODEL OUTPUT  ≠  CANONICAL EVENT
```

The model does not author event history.

## Event taxonomy

Events are typed. Major families include:

| Family | Examples |
|---|---|
| Agent lifecycle | `agent.started`, `agent.progress`, `agent.waiting`, `agent.completed`, `agent.failed` |
| Tool execution | `tool.started`, `tool.completed`, `tool.failed` |
| Permission lifecycle | `permission.requested`, `permission.approved`, `permission.denied`, `permission.expired` |
| Memory | `memory.created`, `memory.updated`, `memory.retrieved`, `memory.deleted` |
| Routines | `routine.created`, `routine.started`, `routine.waiting`, `routine.completed`, `routine.failed` |

Not every type is persisted. Some events are **transient** (useful for live
feedback, not kept long-term) while others are **durable** (kept as part of the
record). Each type declares its persistence and its default visibility.

## Sequencing and correlation

Each event carries a monotonic sequence number so consumers can order history
and resume a stream from a known point. Events also carry correlation
identifiers:

- a **request ID** linking the events of one turn;
- a **trajectory ID** linking the events of one execution trace.

Correlation is what lets Ghost reconstruct "what happened during this turn"
rather than merely "what events exist".

## Persistence

Durable events are written to persistent storage and to a redacted append log.
Retention is bounded: transient events age out quickly, durable events are kept
longer. Persistence is runtime-owned.

## Redaction and visibility

Events are redacted at publication: secrets and secret-shaped values never
enter the stream. Each event has a visibility class, and user-facing consumers
(for example, the activity feed) receive only user-visible events. Raw tool
names, schemas, prompts, and secrets cannot reach user-visible projections.

## Consumers

Canonical events feed:

- **Activity** — the human-facing "what is Ghost doing?" narrative;
- **Memory** — significant memory changes are recorded;
- **Routines** — routine lifecycle is recorded;
- **UI** — live and historical views;
- **Auditing and reconstruction** — the record can be inspected and replayed
  for observation.

## Why activity derives from events

If activity were derived from the model's own narration, it could show work
that never happened. Deriving activity from authoritative events keeps the
human-facing history honest. See
[Memory, Activity, Routines & Artifacts](MEMORY-ACTIVITY-ROUTINES-ARTIFACTS.md).

## Precision about scope

Canonical events are the runtime's structured record. They are not a promise of
a single universal append-only log for every conceivable runtime fact; the
stream contains the typed events the runtime publishes, with the persistence
and visibility each type declares.

## Related concepts

See [Evidence](EVIDENCE.md) for what makes execution trustworthy and
[Memory, Activity, Routines & Artifacts](MEMORY-ACTIVITY-ROUTINES-ARTIFACTS.md)
for downstream consumers.

## Implementation

The event stream lives in `pkg/cevents`. The human-facing projection lives in
`pkg/activity`.
