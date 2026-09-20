# The Desk

A **Desk** is Ghost's persistent, owner-visible working space on the owner's
own hardware. It is where Ghost's work accumulates and where the owner can
see, inspect, and resume it.

The Desk is a **product model over primitives Ghost already has**. It is not a
new execution environment, not a container, and not a second storage engine.
The Pod *is* the computer; the Desk is the honest name for the workspace
Ghost already uses.

This document is the canonical reference. It defines what a Desk item is,
where it comes from, what boundary it respects, and why the boundary is drawn
where it is. It is written for engineers and architects.

---

## Why the Desk exists

Ghost has nine strong subsystems — workspace files, artifacts, live surfaces,
skills Ghost builds, scheduled work, goals, memory, activity, connectors — but
the owner experiences them as separate, mostly invisible pieces. The agent
does real work that the owner cannot easily see, browse, or resume.

Muse's advantage is not more capability; it is coherence. One agent, one
place to look, one history. Ghost does not need a cloud Secure VM to match
that — it needs to make what it already does **legible**, and to make "your
machine" a feature rather than a limitation.

The Desk closes the coherence gap with a thin product model, not a rewrite.

---

## What a Desk item is

A **Desk item** is one durable thing present on the owner's workspace that
the owner may inspect. There are exactly four kinds:

| Kind | What it is | Backing authority |
|---|---|---|
| `document` | A file Ghost created or works with | the workspace filesystem |
| `artifact` | A runtime-validated handoff Ghost produced | `pkg/artifacts` |
| `tool` | A skill Ghost built or installed | `pkg/skills`, workspace `skills/` |
| `surface` | A live browser/computer Ghost acted on | `pkg/live` |

A Desk item is a **view**, never a source of truth. It carries identity,
provenance, and a render hint. It grants nothing.

---

## The boundary (what makes this safe)

The Desk is a read-only projection. It owns:

- no storage (it reads the filesystem, `pkg/artifacts`, `pkg/skills`, `pkg/live`);
- no execution (acting on an item is a new governed capability call);
- no authority (it never decides allow/ask/deny);
- no credentials (it never touches the vault).

Two boundaries are inherited, unchanged:

1. **The estate boundary.** Desk items respect `workspaceFileProtected` and
   the `pkg/artifacts` protected prefixes. `personal-context/`, `state/`,
   `events/`, `knowledge/self/`, and every database file are never Desk
   items. Ghost's internal memory is not the owner's document shelf.

2. **The trust boundary.** An item's existence is proven by the runtime, not
   by model prose — the same rule as `pkg/artifacts`. The Desk surfaces what
   the runtime validated.

---

## Desk item contract

Each item is deterministic and total; the same source always yields the same
item.

- **id** — stable, namespaced by kind (`doc:<relpath>`, `art:<id>`,
  `tool:<name>`, `surf:<kind>:<id>`).
- **kind** — one of `document`, `artifact`, `tool`, `surface`.
- **title** — human name.
- **summary** — one honest line (what it is, not a claim about it).
- **source** — provenance: `workspace`, `artifact`, `skill`, `live`.
- **created_at / updated_at** — from the backing authority.
- **size** — for documents, bytes; otherwise omitted.
- **render** — declared affordances (`preview`, `open`, `download`,
  `visit`), computed server-side. Never an execution grant.
- **protected** — always false for surfaced items; protected paths are
  filtered before an item is constructed, and the field exists so a future
  caller cannot silently surface one.

---

## The layers

```
workspace files   pkg/artifacts   pkg/skills   pkg/live
        \              |             |            /
         \             |             |           /
          +------------+-------------+----------+
                            |
                        pkg/desk          (read-only projection)
                            |
                       GET /v1/desk       (one feed for every surface)
                            |
              mobile "Desk" screen · console "Desk" section
```

`pkg/desk` is the single product model. Surfaces read it; they never rebuild
the merge themselves. This mirrors `pkg/things`: one normalization at the
presentation boundary, no new authority.

---

## What the Desk is not

| The Desk is not | Because |
|---|---|
| a virtual machine | the Pod is the computer; a VM would chase Muse and betray local-first |
| a new storage layer | it reads existing authorities and owns no tables |
| an execution grant | acting on an item is a new capability through the broker |
| a chat list | the conversation is the relationship; the Desk is the work |
| a file manager | it is a curated view of Ghost's work, not a general browser |

---

## Interaction rules

- **Preview is read-only.** Opening a document previews it; it never runs.
- **Acting is governed.** "Send this file", "submit this form", "run this
  tool" are new conversation turns that pass through the normal capability
  and Permission Broker path.
- **Surfaces are leased.** A live surface shown on the Desk is observed;
  taking control is the existing takeover lease, never a Desk affordance.
- **Built tools are inert until invoked.** A tool on the Desk is a skill
  Ghost wrote; it has no authority until a turn runs it under governance.

---

## Why the boundary is drawn here

If the Desk could act, it would become a second authority beside the broker —
the exact fragmentation Ghost's trust model forbids. By keeping the Desk a
read-only projection, "may Ghost do this?" still has exactly one answer, and
the Desk can never be a way around it.

The Desk makes Ghost's work visible. It does not make Ghost's work
unsupervised.

---

## Related concepts

- [Capability](CAPABILITY.md) — what acting on a Desk item requests.
- [Permission Broker](PERMISSION-BROKER.md) — the one authority.
- [Evidence](EVIDENCE.md) — why an item's existence is trustworthy.
- [Execution](EXECUTION.md) — how live surfaces run.
