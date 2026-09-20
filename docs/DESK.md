# The Desk

A **Desk** is Ghost's read-only feed of the things it has made **for** the
owner on their own hardware. It is a product model over the artifacts
authority Ghost already validates — not a new execution environment, not a
container, not a file browser.

## Why the Desk exists

Ghost produces durable work on the owner's behalf: a report, a plan, a
document, a link. That work should be visible and inspectable, on hardware
the owner controls. The Desk is where it lives.

## Scope — deliberately narrow

The Desk surfaces **only artifacts** — things Ghost produced and the runtime
validated. It does not surface:

- raw workspace files (internal logs, proactive outboxes, state),
- file sizes or engine-level details,
- live surfaces or built tools.

An earlier version walked the workspace and exposed everything in it. That
made the Desk a file manager the owner had to interpret, showing internal
files they had no reason to see. A person wants to look at what Ghost made
for them; the Desk is exactly that and nothing else.

## The boundary (what makes this safe)

The Desk is a read-only projection. It owns:

- no storage (it reads the artifacts store);
- no execution (acting on an item is a new governed capability call);
- no authority (it never decides allow/ask/deny);
- no credentials (it never touches the vault).

An item's existence is proven by the runtime, never by model prose — the same
rule as `pkg/artifacts` itself.

## Where it appears

The Desk is **not a top-level destination** in the apps. It is a projection
the surfaces use where relevant; the artifact cards in a conversation are the
primary place an owner meets their work.

## Related concepts

- [Evidence](EVIDENCE.md) — why an item's existence is trustworthy.
- [Capability](CAPABILITY.md) — what acting on an item requests.
- [Permission Broker](PERMISSION-BROKER.md) — the one authority.
