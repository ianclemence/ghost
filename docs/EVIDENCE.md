# Evidence

Evidence is runtime-produced proof about what an execution actually did.

The model can say something happened. Ghost needs to know whether it actually
did. Evidence is how Ghost knows.

## Why evidence exists

Language models produce fluent, confident, and sometimes false statements.
An agent that trusts those statements will report success for actions that
never occurred. Ghost refuses to do that:

> Ghost must not claim successful consequential execution without sufficient
> runtime evidence.

## Revelation: evidence the owner can see

Evidence protects the owner even when they never read it. But an owner who
cannot see why Ghost acted cannot fully trust it, so the same facts are
surfaced in owner language on the Activity feed:

- **why an approval was asked** — derived from the broker's own risk class,
  never model prose ("Consequential action, so Ghost asked first.");
- **what a decision meant** — an approval, a denial, or an expiry, each with
  the consequence stated honestly ("You declined this action, so Ghost did
  not do it.");
- **that consequential work ran under approval** — and, on failure, that
  nothing was changed.

The revelation line is quiet by construction: it appears only where there is
something the owner would want to reason about. Routine reads get no line.
The values come from `pkg/activity` (`whyFor`), rendered on both the mobile
app and the Web Console.

## Model claim is not evidence

```
MODEL CLAIM  ≠  RUNTIME EVIDENCE
```

Evidence is produced by the runtime as a side effect of execution — a file
hash, a device state read back, an artifact identifier, a governed action
record. It is never ordinary model prose. The model supplies arguments; it does
not supply evidence.

## Capability-specific evidence

Different operations require different proof. Ghost's evidence kinds are:

| Kind | Used for | Required shape |
|---|---|---|
| `acknowledgement` | Outbound actions (for example `message.send`) | operation identity + timestamp |
| `state_transition` | Device control | entity, requested state, timestamp |
| `artifact` | Artifact creation | artifact identifier + timestamp |
| `file_write` | File writes/edits | path + timestamp (with size/hash) |
| `action` | Browser/computer control | operation, outcome, timestamp |
| (none) | Read-only capabilities | no evidence required |

A capability declares which kind it requires. Read-only capabilities require
none.

## Validation

Evidence is validated before completion:

- it must be **present**;
- it must be the **right kind** for the capability;
- it must be **structurally complete** (required fields present);
- it must be tied to the **actual operation**.

If validation fails, the execution is not presented as success.

## Successful completion

The invariant:

```
CAPABILITY
  ↓
EVIDENCE CONTRACT
  ↓
EVIDENCE VALIDATOR
  ↓
COMPLETION OUTCOME
```

Only the runtime evidence validator may transition a consequential execution
into success. The registry refuses success when the evidence is missing or
invalid.

## Failure

Absence or invalidity of evidence produces a non-success outcome — typically
`failed`, `waiting`, `temporarily unavailable`, or `offline` — depending on the
actual runtime semantics. It is never silently collapsed into success.

## Evidence strength and user-facing language

Evidence has a shape, and Ghost records that shape separately from the prose it
writes. What was read, whether a page was read through or only listed, which
publications stand behind an answer, and whether a source the owner asked for
could be read are all facts the runtime keeps — for audit, activity, replay and
evaluation.

Whether those facts *reach the owner* is a separate decision, and it is
deterministic:

| Level | When | What the owner sees |
|---|---|---|
| `none` | the limitation is true about the retrieval and does not change the answer | nothing. A page that would not parse while other sources corroborate the same facts stays internal. |
| `contextual` | the evidence is thin enough that the wording should be more careful | attribution and calibrated phrasing ("early reports indicate"), not a note |
| `material` | the owner asked for a source that could not be read; sources disagree; an action ran but its outcome is unconfirmed | the limitation stated plainly, in the sentence it matters to |
| `blocking` | the action failed, or no usable evidence exists | the honest statement of what could not be done, and why |

The policy lives in `pkg/product` (the package that owns Ghost's
LLM-independent user-facing language) and the prompt states the same rule
verbatim, so the model's prose and the runtime's decision cannot drift apart.

Attribution is not a caveat. Naming a source, quoting its figure, and saying
when sources disagree is how an answer carries its own confidence; a separate
disclaimer block explaining the retrieval adds nothing the attribution did not
already convey. Internal mechanics — byte counts, extractors, cache paths,
whether a page was a listing — are never part of an answer, and tools record
them as structured facts rather than as prose the model can relay.

A closing question is not a caveat either, but it has the same cost: it spends
the owner's attention on Ghost's willingness to continue rather than on what
was asked. The runtime drops a purely conversational closing offer at the
output boundary unless a tool failed, in which case "want me to retry?" is
information.

## Relationship to canonical events

```
EXECUTION
  ↓
EVIDENCE
  ↓
COMPLETION
  ↓
CANONICAL EVENT
```

The canonical event records the capability, the runtime-selected provider, and
the outcome. Evidence makes that record trustworthy; it is not a separate
user-facing product layer.

## Related concepts

See [Execution](EXECUTION.md) for where evidence is produced and
[Canonical Events](CANONICAL-EVENT.md) for how it is recorded.

## Implementation

Evidence kinds and validation live in `pkg/capability`. Evidence is attached
to tool results and validated by the tool registry in `pkg/tools`. Browser and
computer gates enforce their own evidence rules in `pkg/agent`.
