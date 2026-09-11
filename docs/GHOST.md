# Ghost

Ghost is the central runtime. It is the control plane that surrounds a
replaceable language model with identity, authority, memory, execution,
evidence, and persistence.

The model lives **inside** Ghost. Ghost is not a thin wrapper around a model.

## Identity

Ghost has a persistent identity: a stable `ghost_id` minted once and preserved
for the life of the installation. Identity is hardware-independent and
survives upgrades and migrations. It is the anchor for ownership, pairing, and
the durable relationship with the user.

## One persistent relationship

Ghost serves one owner. Memory, preferences, routines, artifacts, and standing
grants belong to that relationship. There is no per-agent or per-workspace
fragmentation of the user model.

## Runtime ownership

Ghost owns the semantics of trust. Specifically, Ghost owns:

- **authority** — a single Permission Broker decides allow / ask / deny;
- **execution orchestration** — how authorized work is dispatched;
- **evidence** — runtime proof that work happened;
- **canonical events** — the durable record of activity;
- **memory semantics** — what is remembered and under what boundaries;
- **persistence** — sessions, state, routines, and artifacts.

## Orchestration and context

For each turn, Ghost assembles context (identity, memory digest, session
history, capability index) and routes the request through reasoning. It then
governs and executes any requested action. Ghost keeps the model's view
bounded and intentional rather than exposing the whole runtime.

## Authority

Ghost does not delegate authority to the model, a tool, a skill, a connected
app, a provider, or a resolver. All consequential actions pass through the
Permission Broker. See [Permission Broker](PERMISSION-BROKER.md).

## Lifecycle and persistence

Ghost persists:

- sessions and conversation history;
- memory entries and profiles;
- routines and their execution history;
- artifacts;
- canonical events;
- permissions and grants;
- connected-app and credential state (through their own boundaries).

State is durable across restarts. A scheduled or paused piece of work survives
a restart and is recovered deterministically.

## What Ghost deliberately does not delegate to the model

Ghost never lets the model:

- grant or widen authority;
- decide whether it is allowed to act;
- assert that a consequential action succeeded without runtime evidence;
- own canonical event history;
- define capability identity;
- own credential storage;
- silently create persistent automation.

The model may propose, plan, and request. Ghost decides, executes, verifies,
and records.

## The control plane

```
                 GHOST (control plane)
                 ├── Identity
                 ├── Permission Broker        (authority)
                 ├── Capability registry      (semantics)
                 ├── Resolver                 (availability)
                 ├── Credential Vault         (secrets)
                 ├── Canonical events         (record)
                 ├── Memory / Routines / Artifacts
                 └── Model (replaceable reasoning participant)
```

> The model is replaceable; Ghost is the product.

## Implementation

The runtime is assembled in `pkg/agent` (turn loop, governance) and
`cmd/ghost` (process wiring). Identity and durable state live in
`pkg/ghoststate` and `pkg/state`.

## Related concepts

See [Intent & Reasoning](INTENT-REASONING.md) for how Ghost interprets
requests, and [Capability](CAPABILITY.md) for the semantic abilities Ghost
owns.
