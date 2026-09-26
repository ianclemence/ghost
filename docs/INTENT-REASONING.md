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
  sidecar, the goal store, the durable job store, the promise ledger). No
  model output is involved, and an observation that cannot cite its rows is
  not made.
- **Candidate.** One live opportunity per subject, keyed stably. The
  state digest is carried separately, so a change invalidates the
  existing proposal instead of spawning a second one.
- **Gate.** Priority, confidence, freshness, actionability, the existing
  noticer budget/cooldown/dedupe, quiet hours, and a category filter. All
  local and cheap — deciding whether to interrupt the owner never costs a
  model call.
- **Proposal.** Rendered from structured fields: the observation, why it
  matters now, and exactly one action. Delivery rides the existing
  outbound + suggestion-card path.
- **Approval.** The owner answers once — card button, console, or a chat
  reply. The approval is bound to the proposal id, the capability, the
  arguments, and the state the proposal was built from.
- **Broker.** The proposal asks the broker *before* it is shown, so an
  offer Ghost cannot keep is never made. There is no proactive bypass.
  Authorization is minted at the moment of approval, never at surfacing:
  an unanswered suggestion therefore leaves no expiring token behind, and
  an owner who answers late is still acting on something real.
- **Execution and verification.** The action runs through the same
  registry (or the same service the API calls) as any other work, then the
  runtime reads the state back. The result the owner sees is derived from
  runtime evidence. Every outcome carries an evidence level — `verified`,
  `acknowledged`, `dispatched`, `unavailable`, or `failed` — and the
  sentence shown is derived from it, so a dispatched action is never
  called done.

### Promises

A promise is the bridge from conversation to action. When the owner says
"I need to send Alex those photos Friday", the runtime records a durable
obligation in `commitments/commitments.json`: the obligation in the
owner's words, the entity involved, the closed-vocabulary shape of the
task, the instant it resolves to, the provenance (session, message id,
verbatim quote), and a lifecycle state (`open`, `blocked`, `completed`,
`cancelled`, `expired`).

- **Only real promises.** Speculation ("I might…"), hypotheticals,
  questions, requests addressed to Ghost, and explicit reminder requests
  (which the scheduler already owns) never become obligations.
- **Nothing is invented.** The deterministic pass handles the common
  shapes without a model call. When the semantic pass runs, the quote, the
  subject and the time phrase must each appear in the message, or the
  extraction is discarded. Dates are resolved by the runtime's own
  natural-language schedule parser, never by model arithmetic.
- **Actionability is planned, not guessed.** A planner decides what Ghost
  can honestly offer. When the obligation needs a channel that has never
  carried a message, the offer degrades to a reminder Ghost really
  creates, rather than promising to send something it cannot send.
- **Settlement is driven by outcomes.** A verified success completes the
  promise; a failure leaves it open with the reason; a dismissal leaves it
  open and quiet; the owner can close it explicitly.

### Awareness

Evaluation is driven by state change, with the heartbeat as reconciliation
rather than the primary source. The canonical stream wakes the engine when
something relevant happens (a promise recorded, a routine failed, a task
stopped, an approval resolved); irrelevant events are filtered before any
work. Wake-ups are coalesced with a floor between runs, and evaluation
itself is deterministic, so a local event never becomes a model call.

Interruptions are bounded in the runtime, not in a prompt: one live
opportunity per situation, a dismissal cooldown, a failure backoff, and
automatic withdrawal (supersession) when the situation resolves or
changes before the owner answers. A stale approval is refused rather than
executed.

### Controls

Observations, candidates, gating and delivery are all governed by
`PROACTIVE_PREFERENCES.md` in the workspace, parsed deterministically
(never interpreted by a model). Missing file means the conservative
defaults; malformed values are ignored rather than failing closed on a
preferences file.

| Key | Meaning | Default |
|---|---|---|
| `enabled` | Master switch for notices **and** opportunities. Off silences Ghost volunteering things; it never disables work the owner explicitly asked for. | `true` |
| `quiet_hours` | Window in which non-urgent delivery is held, in the owner's timezone. | `23:00-08:00` |
| `max_pushes_per_day` | Daily cap on non-urgent check-ins. | `5` |
| `cooldown_per_topic` | Per-topic silence after a push. | `6h` |
| `dedupe_window` | Identical-content suppression. | `24h` |
| `categories` | Which opportunity categories may surface (`reminders`, `routines`, `goals`, `tasks`). Empty means all. | all |
| `preferred_channel` | Deliver offers on one channel only. Empty means the last active channel. | last active |
| `proposal_ttl_hours` | How long an unanswered offer stays on the owner's list. | `24` |

Authorization is minted when the owner approves, not when a suggestion is
surfaced, so there is no separate approval window to lapse while the
proposal stays valid: what the owner answers is what the broker decides on.

Forgetting is not a control here: dismissing an opportunity records a
receipt and suppresses that opportunity for the dismissal cooldown; it
never deletes memory. See [User](USER.md) for what the owner can change
about Ghost's memory.

The model's role here is unchanged: it may interpret what an approved
action needs, but it never grants permission, never declares success, and
never decides whether Ghost should speak. See
[Permission Broker](PERMISSION-BROKER.md) for where authority is enforced.

## Turn latency

A turn does as little as it can and still be correct. The order is fixed:

```
request
  → deterministic gates (existing)
  → is the answer authoritative runtime state?
        yes → direct runtime response (no model, no retrieval)
        no  → does this turn need durable memory?
                 yes → embed query → vector search → minimal memory
                 no  → skip retrieval entirely
  → compact context (behavioural core + request-dependent extras)
  → relevant tools
  → model → stream
  → capability (if asked) → broker → execution → evidence → verification
  → runtime-authored outcome
```

**Authoritative state answers.** Reminders, open opportunities, pending
approvals, routine health, stuck tasks, recent activity, device health, the
active model and the proactive policy are read from the stores that own them
and answered directly. Every renderer is state-bound: it prints only fields
read from those stores, falls through to the normal path when a store is
unwired, and never invents a name, id, date, count or completion state. The
model is still free to answer these questions in its own words when the
deterministic path does not match; this is an optimisation, not a gate.

**Memory is retrieved only when the turn needs it.** Deciding whether a
message depends on durable memory is deterministic: explicit recall intent, a
temporal back-reference, or a possessive reference to something the owner
owns that is not runtime state. Greetings, actions, and state questions skip
the embedding entirely. `memory_recall` and `session_search` remain available
to the model as the fallback for anything the classifier does not anticipate,
which is what makes a conservative gate safe. Retrieval counts and deliberate
skips are visible through `/v1/doctor`.

**Context is tiered, statically.** The behavioural core of `GHOST.md` —
invariants, identity, authority and permissions, execution and evidence,
memory, routines, time, personality, interacting, safety, recovery, final
rules — always ships. Operational reference (the per-tool operating manual,
skills prose, channels, browser/computer surfaces, credentials setup,
artifacts) is read on demand with `read_file`, and the core says so.
`AGENTS.md` and `HEARTBEAT.md` are not conversation context and are not
injected. Tiering is deliberately static rather than per-turn: varying the
prefix would invalidate the provider's prompt-prefix cache and cost more than
it saves.

**Work that does not shape the reply happens after it.** Model-backed
personal-context and commitment extraction ride a bounded, durable queue
drained once the answer is on the wire, with single-flight protection,
bounded retries, restart recovery and an interactive-priority gate.
Deterministic extraction stays inline because it is free.

**Interactive work outranks background work.** Heartbeat, journaling,
summarization and deferred extraction all check a shared interactive gate and
yield to the owner. The heartbeat additionally runs only the `HEARTBEAT.md`
sections whose own cadence is due, so a tick with nothing to do costs no model
call at all.

**Observability is local and honest.** Each turn logs total duration,
time-to-first-byte, iteration count, token counts and where those counts came
from (`measured` from the provider, `estimated` when a streamed response
reports none, or `unknown`). An estimate is never presented as a measurement.

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
