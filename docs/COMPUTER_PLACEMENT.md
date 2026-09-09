# Computer Placement

This document records what placement means *now*, what it binds to, and
what a future paired-executor topology would require. It is deliberately
not an implementation plan: Ghost's current executor is the appliance
itself, and nothing here should be read as a committed product timeline.

## What placement means today

A placement is the **runtime environment a computer operation executes
in**, resolved by the code — never by the model. Ghost currently has one
real placement, plus two that exist only in classification:

- `local` — the appliance itself. All operations today run here.
- `paired` — a user device Ghost has bonded with. **Not yet executable.**
- `sandbox` / `remote` — reserved classifications (isolated container,
  relay-attached executor). **Not yet executable.**

The runtime expresses a placement as a capability descriptor
(`pkg/computer`): an identity, a placement kind, and a contact/last-seen
signal. If a placement cannot be reached, availability reports
`unavailable` with a reason — it never silently falls back to another
placement or to "unlimited".

## What identity it binds to

Every placement record is bound to the **Ghost principal** (owner) plus
the **context** and **task/work item** that requested it. A placement
discovered by one owner is never offered to another, and a placement
pinned to one context is not visible from a different context. Task
binding means an executor is only usable by the work item that holds it.

## What leases represent

A lease is the runtime's guarantee that one task may use one resource for
a bounded window (`pkg/computer/leases.go`):

- **Owner + context + task** scope every lease; the same task/session may
  renew its own hold, any other live holder fails closed with "busy".
- Leases carry a **TTL and are renewed by heartbeat**; a hold that stops
  renewing lapses on its own clock.
- Every boot **expires all survivors** (`RecoverStale`): after a restart
  no pre-restart task is alive, so no lease may outlive it. The restarted
  task re-acquires explicitly.
- A task that lost its lease (expiry, recovery, release) checks
  `OwnedBy` before acting and **fails closed** rather than driving a
  resource it no longer holds.

## What evidence follows placement

Each operation records the placement that ran it, its identity, the
operation, and the outcome (`pkg/computer/evidence.go`, and the browser
gate's canonical events). Evidence never claims a side effect occurred
without an execution record, and never asserts more than the executor
confirmed (completed / failed / waiting / awaiting-verification).

## What a paired Windows executor would require later

Not built now. When/if Ghost supports "I need a computer" against a
paired Windows PC, the following are the seams this slice deliberately
kept compatible:

1. A **device-side executor process** Ghost can supervise, reach, and
   authenticate — paired the same way the mobile app pairs (device
   credentials, re-pair after restore), never by raw network trust.
2. A **transport for operations + evidence** (the relay already moves
   typed messages; an executor protocol would ride it or its peer).
3. **Placement-aware leases**: leases must name the executor, and the
   executor must heartbeat/renew from the device side so a partition
   cannot leave a phantom hold.
4. **Placement-scoped permissions**: a broker decision must name the
   target placement, and grants must be able to restrict to it.
5. **Evidence provenance**: the executor signs/returns evidence the
   appliance verifies before any canonical event claims success.
6. **Context/session isolation on the executor**: per-context profiles
   and storage must live on the device but remain Ghost-scoped.

The current abstractions (placement kinds, availability with reasons,
owner/context/task-scoped leases, evidence-first execution) are not
hostile to that topology; nothing here requires rework to add it.
