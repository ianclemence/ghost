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
