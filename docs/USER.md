# User

The user is the owner and operator of Ghost. Everything Ghost does exists to
serve the user's goals while protecting the user's trust.

## The human as owner

Ghost is a personal runtime, not a shared service. It has one persistent
relationship with its owner. That relationship is durable: identity, memory,
preferences, routines, and artifacts persist across sessions and restarts.

The user is the source of goals and the final authority on intent. The user
expresses what they want; Ghost decides how to accomplish it safely.

## Natural-language interaction

The user interacts with Ghost in ordinary language — through a chat channel,
the Web Console, or a paired device. They do not issue structured commands to
internal machinery.

Examples:

| The user says | The user means |
|---|---|
| "Remember that I prefer morning flights." | A durable preference Ghost should honor later |
| "Book me the cheapest reasonable flight tomorrow." | A goal that requires a real external action |
| "Turn off the bedroom light." | A device-control goal |
| "Remind me every Friday." | A recurring routine |
| "Send this to Maria." | An outbound message goal |

In each case the user expresses **intent**, not implementation instructions.
They never choose a provider, a tool, or a capability identifier.

## User goals vs implementation details

The user should not need to know about:

- which model answered;
- which provider or integration was used;
- which capability or tool ran;
- which API or endpoint was called;
- how authentication worked;
- which scheduler fired a routine;
- how evidence was validated.

Those are runtime concerns. This is a deliberate product decision:

> Complexity belongs in the runtime, not in the owner's head.

The user experience is intentionally simple even though the runtime underneath
is sophisticated.

## Approvals

When an action is consequential, Ghost may ask for approval before proceeding.
The user answers in natural language ("yes", "always", or "no"). Ghost's
Permission Broker records the decision and, when the user chooses "always",
stores a scoped standing grant.

The user is never asked to approve something Ghost is not actually going to do,
and never has authority applied on their behalf without a decision.

## Corrections

The user can correct Ghost: change a preference, retract a fact, or tell Ghost
it misunderstood. Corrections update memory and behavior through Ghost's
governed memory system, not through silent overwrites.

## Preferences

Preferences are durable facts about how the user wants Ghost to behave. They
are stored as memory entries with provenance, so Ghost can distinguish a
user-declared preference from an inferred one.

## The persistent relationship

Because Ghost is persistent, the user should experience continuity: Ghost
remembers what matters, honors standing grants within their scope, and can be
taken over or paused on a live surface (browser or computer) when the user
wants direct control.

## How user intent enters Ghost

```
User request (natural language)
        ↓
Ghost receives the message with channel/session context
        ↓
Intent / reasoning interprets it
        ↓
A capability request is formed
        ↓
Governance decides
```

The user's words are input to reasoning. They are not commands to execution.

## What the user should and should not need to know

The user should know:

- what Ghost can help with;
- when Ghost needs approval;
- what Ghost actually did (through activity);
- how to correct or forget something.

The user should not need to know:

- providers, tools, capabilities, or schedulers;
- permission internals or event schemas;
- how credentials are stored;
- model selection or routing.

## Related concepts

See [Ghost](GHOST.md) for the runtime that owns the relationship, and
[Intent & Reasoning](INTENT-REASONING.md) for how requests become capability
requests.
