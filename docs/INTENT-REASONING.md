# Intent & Reasoning

Intent and reasoning turn a user's words into a concrete, governed capability
request. They do not grant authority and they do not execute consequential
actions.

## From request to intent

```
User request
    ↓
Interpretation (what did the user mean?)
    ↓
Reasoning / planning (what needs to happen?)
    ↓
Semantic capability request
    ↓
Governance (Permission Broker)
```

Interpretation resolves language against context: recent conversation, memory,
the active context/session, and any media. Reasoning then decides what the
request implies — which capabilities are relevant, what arguments they need,
and whether the request is ambiguous.

## The model participates, but does not authorize

Ghost is model-agnostic. The active model is a replaceable reasoning
participant. It can:

- interpret and plan;
- choose among the semantic tools Ghost exposes;
- propose a capability request with arguments.

It cannot:

- authorize an action;
- bypass the Permission Broker;
- assert success;
- grant itself or a skill any authority.

Reasoning is advisory. Authority is Ghost's.

## Not this

Ghost's canonical architecture is **not**:

```
User → Model → arbitrary tool → machine
```

That pattern lets the model reach execution directly. Ghost instead bounds the
model to a small set of **semantic surfaces** and places governance between
the request and execution.

## Semantic surfaces, not raw primitives

The model sees bounded, semantic tools such as:

- `calendar` — read or modify the calendar;
- `device` — read or control smart-home devices;
- `message` — send a message;
- `weather_now`, `aqi_now`, `currency_convert`, `crypto_price`, `places_nearby`, `flight_status` — read-only information;
- `browser_*` — observe and interact with web pages;
- `computer_*` — observe and control the desktop;
- memory tools, `artifact` publishing, and scheduling.

These are invocation surfaces. Underneath, Ghost resolves them to capabilities
and implementations. A tool name is never the permission identity. See
[Capability](CAPABILITY.md).

There is deliberately no generic `execute(capability, args)` surface. A single
unrestricted executor would hide the old problem behind an escape hatch.

The on-device Mini is the limiting case of this design: it sees no tool
surfaces at all. The phone answers from bounded context plus deterministically
injected notebook facts; collection (`remember ...`) is a pipeline rule, never
a model-invoked tool call. Anything needing tools, routines, or hardware
routes to the Pod.

## Ambiguity and clarification

When a request is ambiguous, Ghost asks rather than guessing at a consequential
action. Clarification is a first-class path: Ghost can ask the user a question,
wait, and resume the same work once the answer arrives.

## The distinctions that matter

| Concept | Meaning |
|---|---|
| Reasoning | Deciding what to do |
| Intent | What the user meant |
| Capability request | The semantic action Ghost proposes |
| Authorization | Whether Ghost may do it (broker) |
| Execution | Doing it |
| Evidence | Proving it happened |

A reasoning error can produce a bad capability request. It cannot, by itself,
produce a consequential execution: authorization still stands between the
request and the action.

## Proactive opportunities

Reasoning also runs without a request. Ghost watches the state it already
keeps — reminders that never reached the owner, routines whose latest run
failed, routines parked waiting on an approval, goals that went quiet,
background tasks waiting on a human — and turns what it finds into one
actionable offer.

The shape is fixed and deterministic:

```
observation → candidate → gate → proposal → approval → broker → execution
            → evidence → verification → canonical event → result → memory
```

- **Observation.** Derived from real rows (the scheduler, the routine
  sidecar, the goal store, the durable job store). No model output is
  involved, and an observation that cannot cite its rows is not made.
- **Candidate.** One live opportunity per subject, keyed stably. The
  state digest is carried separately, so a change invalidates the
  existing proposal instead of spawning a second one.
- **Gate.** Priority, confidence, freshness, actionability, the existing
  noticer budget/cooldown/dedupe, quiet hours, and a category filter. All
  local and cheap — deciding whether to interrupt the owner never costs a
  model call.
- **Proposal.** Rendered from structured fields: the observation, why it
  matters now, and exactly one action. Delivery rides the existing
  outbound + suggestion-card path, with the approve action bound to a real
  broker request.
- **Approval.** The owner answers once — card button, console, or a chat
  reply. The approval is bound to the proposal id, the capability, the
  arguments, and the state the proposal was built from.
- **Broker.** The proposal asks the broker *before* it is shown, so an
  offer Ghost cannot keep is never made. There is no proactive bypass.
- **Execution and verification.** The action runs through the same
  registry (or the same service the API calls) as any other work, then the
  runtime reads the state back. The result the owner sees is derived from
  runtime evidence, and a failure says so.

Interruptions are bounded in the runtime, not in a prompt: one live
opportunity per situation, a dismissal cooldown, a failure backoff, and
automatic withdrawal (supersession) when the situation resolves or
changes before the owner answers. A stale approval is refused rather than
executed.

The model's role here is unchanged: it may interpret what an approved
action needs, but it never grants permission, never declares success, and
never decides whether Ghost should speak. See
[Permission Broker](PERMISSION-BROKER.md) for where authority is enforced.

## Why this design

If reasoning were authoritative, a model mistake or a malicious instruction
embedded in fetched content could cause real-world effects. By separating
reasoning from authority, Ghost ensures that:

- the model can be wrong without being dangerous;
- external content cannot become an instruction to act;
- the user's authority decisions are enforced consistently.

## Related concepts

See [Capability](CAPABILITY.md) for the semantic boundary, and
[Permission Broker](PERMISSION-BROKER.md) for where authority is enforced.
