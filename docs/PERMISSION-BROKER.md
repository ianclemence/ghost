# Permission Broker

The Permission Broker is Ghost's authoritative authority boundary. It answers
one question for every consequential action:

> May Ghost do this?

Nothing else in Ghost may answer that question.

## Why it exists

The model can be wrong. External content can be malicious. A connected app may
hold valid credentials for operations the user never intended. Without a single
authority, any of these could cause real-world effects.

The broker centralizes the decision so it can be reasoned about, tested, and
audited.

## Authentication vs authorization

- **Authentication** establishes identity: which provider, which app, which
  credential, which device.
- **Authorization** decides permission: whether Ghost may perform a specific
  operation.

```
AUTHENTICATED ≠ AUTHORIZED
AVAILABLE     ≠ AUTHORIZED
```

A connected app having credentials does not mean Ghost may use them for the
requested operation. The broker decides separately.

## The flow

```
Capability request
       ↓
Permission Broker
       ↓
ALLOW / ASK / DENY
       ↓
Execution (only on ALLOW)
```

## Verdicts

| Verdict | Meaning |
|---|---|
| **ALLOW** | Proceed without interrupting the user |
| **ASK** | Pause and request the user's decision |
| **DENY** | Refuse; do not execute |

An `ASK` produces a durable, resumable approval request. The user answers in
one of four ways:

| Answer | Effect |
|---|---|
| **Allow once** | This one execution. A second reply finds nothing to consume. |
| **Allow for this task** | A scoped grant that follows the work: each use extends it by 10 minutes, it ends after 10 quiet minutes, and it hard-stops one hour after it was given. The runtime has no task identifier to bind to, so "task" means "while Ghost keeps using this". |
| **Always allow** | A scoped standing grant (see Grants). |
| **Deny** | Refuse, and remember the refusal for this scope. |

A task grant never shortens a standing grant that already covers the action.

## Risk levels

The broker evaluates a capability's declared risk:

| Risk | Default behavior |
|---|---|
| `read_only` | Allowed without approval |
| `low_risk` | Depends on mode; may ask |
| `consequential` | Asks unless explicitly allowed |
| `high_impact` | Never auto-authorized; always asks |

Unknown risk fails closed (ask/deny).

## Modes

The broker operates in a mode that shapes the default behavior:

| Mode | Behavior |
|---|---|
| `ask` | Consequential actions ask |
| `auto` | More actions are allowed without asking |
| `full` | Consequential actions are allowed without asking |
| `custom` | Deployment-defined policy |

Read-only actions are allowed regardless. `high_impact` actions are never
auto-authorized.

## Grants

A **standing grant** records that the user chose "always" for a specific
capability, action, and scope. Grants are narrow:

- they cover one capability and action;
- they are bound to a scope (for example `owner`, a session, or a resource);
- they never widen beyond what was approved.

Grants have a finite lifetime (a default of 90 days). A grant that was valid
once does not authorize forever.

## Scope

Authorization is evaluated against the capability **plus** relevant
resource/context dimensions. A grant for one target does not authorize another.
Context and routine scopes can only make a decision stricter, never looser.

## Expiry, revocation, and revalidation

- Approvals expire and are swept.
- Grants expire and are pruned.
- The user can revoke a grant; revocation takes effect immediately.
- On restart, stale leases and expired authority are recovered rather than
  carried forward.
- A resumed approval re-checks current policy before executing, so a revoked
  or expired grant cannot be replayed.

## Monotonicity

Additional constraints (context scope, routine scope, deny-only guards) compose
monotonically. None of them can turn a broker denial into an allow. Each can
only make the outcome stricter.

## Fail-closed behavior

Malformed input, missing boundaries, and unknown risk fail closed. If the
runtime has no authority boundary wired, consequential primitives are refused
rather than executed.

## Scheduled execution

A routine does not become an authority because it is scheduled:

```
ROUTINE
  ↓
SCHEDULER
  ↓
AGENT TURN
  ↓
CAPABILITY
  ↓
PERMISSION BROKER
  ↓
EXECUTION
```

The scheduler wakes Ghost. It does not grant permission. A scheduled action
whose authority has been revoked fails or waits rather than executing.

## Browser and computer control

Browser and computer operations pass through their own gates, which compose
with the broker. Observation and control are distinct: observing a page or
screen is read-only; interacting with it is consequential and governed.
Control may be leased to the user (takeover), which pauses Ghost on that
surface. See [Execution](EXECUTION.md).

## Subagents

A delegated subtask receives an explicit, deny-by-default capability scope and
cannot acquire more authority than the parent delegates. The broker still
governs each capability the subtask requests.

## Why the broker must remain singular

If individual providers, skills, schedulers, or tools could authorize
themselves, authority would be fragmented and unauditable. Ghost has exactly
one broker so that "may Ghost do this?" has exactly one answer.

## Related concepts

See [Capability](CAPABILITY.md) for what is being authorized and
[Evidence](EVIDENCE.md) for what happens after execution.

## Who may answer an approval

The model can run shell commands, and those commands share the Pod's network, so
they can reach the gateway on `127.0.0.1` like any local program. Loopback alone
is therefore not trusted for the routes that grant authority, mint credentials,
change policy or install code (approvals and grants, mode, pairing, connecting
apps and tool servers, skills, providers, reset, system update, live takeover).
Those need either a paired device's credentials or the **local token**: a secret
in the config directory, which the command sandbox never mounts. The console, the
terminal and the CLI read it and send it; a command the model runs cannot.

## Implementation

The broker lives in `pkg/permissions`; the local token is `pkg/localtrust`. Browser and computer gates live in
`pkg/agent`.
