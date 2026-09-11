# Execution

Execution is the attempt to perform an authorized capability. It happens only
after the Permission Broker allows it, and it produces evidence about what
actually occurred.

## Execution is not a claim

A request to do something is not proof it happened. A tool returning is not
proof it happened. Execution is the runtime's attempt; evidence is the runtime's
proof.

Ghost distinguishes:

| Stage | Meaning |
|---|---|
| Request | The model asks for a capability operation |
| Authorization | The broker decides allow / ask / deny |
| Execution attempt | Ghost dispatches the authorized work |
| Execution outcome | What the implementation reported |
| Evidence | Runtime proof of what happened |
| Completion | The product-level result Ghost tells the user |

Only the evidence validator may turn a consequential execution attempt into
success. See [Evidence](EVIDENCE.md).

## The execution path

```
AUTHORIZATION (ALLOW)
      ↓
CAPABILITY RESOLVER
      ↓
IMPLEMENTATION / CONNECTED APP / PROVIDER
      ↓
EXECUTION
      ↓
EVIDENCE
```

The resolver chooses an implementation. The implementation performs the work.
The runtime validates the resulting evidence.

## Implementations

An implementation is a replaceable way to fulfil a capability. It may be:

- a **local** implementation (a tool running on the device);
- a **provider** adapter (an external service or model provider);
- a **connected app** integration (an authenticated external system);
- an **MCP** adapter (a third-party tool over a protocol).

Implementations execute. They do not authorize, and they do not define
capability identity.

## Local execution

Local capabilities — memory, files, artifacts, scheduling — run on the device
under Ghost's control and confinement. Local writes that produce durable output
carry evidence (for example, a file write records its path and hash).

## Browser and computer

Browser and computer are governed live surfaces:

- **Observation** (navigating, snapshotting a page, inspecting the screen) is
  read-only.
- **Control** (clicking, typing, pressing keys) is consequential and governed.

Control may be **leased to the user** as a takeover: while the user holds
control, Ghost is paused on that surface. Releasing control does not blindly
resume Ghost; the surface pauses and resumes only through revalidation. User
control leases expire, so a dead connection cannot leave a permanent owner.

A successful control operation without runtime evidence is treated as
unverified, never as done.

## Devices and integrations

Smart-home and external integrations execute through their adapters. A device
control records a requested state and, where observable, the resulting state.
An external action records an acknowledgement with an operation identity.

## Scheduled execution

Scheduled work follows the same authority model as interactive work:

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
  ↓
EVIDENCE
```

The scheduler is an execution mechanism. It does not become an authority
because execution originated from a routine. See
[Memory, Activity, Routines & Artifacts](MEMORY-ACTIVITY-ROUTINES-ARTIFACTS.md).

## Artifacts

Publishing an artifact is a capability (`artifact.create`). It produces a
runtime-managed entity with evidence (identifier, kind, title). An artifact is
an output, not an executable object: acting on an artifact is itself a governed
capability.

## Failure handling

Execution outcomes map to product-level results:

| Outcome | Meaning |
|---|---|
| success | Authorized work completed with valid evidence |
| failed | The attempt did not succeed |
| waiting | Blocked on permission or a missing input |
| temporarily unavailable | The implementation could not serve now |
| offline | The implementation is unreachable |
| cancelled | The user or system cancelled the work |

A provider failure is not silently converted into success, and it is not
necessarily a permanent capability failure — the resolver may select another
available implementation.

## Related concepts

See [Evidence](EVIDENCE.md) for how outcomes are proven, and
[Canonical Events](CANONICAL-EVENT.md) for how they are recorded.

## Implementation

Execution surfaces live in `pkg/tools` (registry, sandboxing, isolation),
`pkg/providers` (external adapters), `pkg/browser` and `pkg/computer`
(governed surfaces), and `pkg/mcp` (third-party tools).
