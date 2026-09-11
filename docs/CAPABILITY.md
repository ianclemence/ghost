# Capability

A **capability** is a stable, Ghost-owned semantic ability to accomplish a
bounded task on behalf of the user. It is the contract between user intent,
governance, execution, and evidence.

Examples:

```
memory.remember      memory.recall        memory.forget
calendar.read        calendar.modify
message.send
device.read          device.control
weather.get          aqi.get
repository.search
web.search           web.fetch
browser.inspect      browser.control
computer.inspect     computer.control
artifact.create
routine.create       routine.modify       routine.cancel
```

## What a capability is not

| A capability is not | Because |
|---|---|
| a **tool** | a tool is an internal invocation mechanism that requests a capability |
| a **provider** | a provider is a replaceable implementation of a capability |
| a **connected app** | an app is an authenticated external system that helps fulfil capabilities |
| a **skill** | a skill is knowledge/procedure that may declare capability requirements |
| **permission** | permission is decided by the Permission Broker |
| **implementation** | implementation is how a capability is carried out |

Third-party systems do not define Ghost's capability vocabulary. A provider's
tool names never become Ghost's semantic identity.

## The capability contract

Each capability carries, explicitly or through associated metadata:

- **identity** — the canonical semantic ID (`calendar.modify`);
- **risk** — `read_only`, `low_risk`, `consequential`, or `high_impact`;
- **evidence** — the kind of runtime proof required on success;
- **tools** — the model-facing surfaces that currently fulfil it;
- **description** — a product-level explanation.

The contract is intentionally small. It is a semantic registry, not a
framework.

## Lifecycle

```
Intent
  ↓
Capability            (semantic identity)
  ↓
Permission Broker     (may Ghost do this?)
  ↓
Resolver              (which implementation can fulfil it?)
  ↓
Implementation        (provider / connected app / local)
  ↓
Execution
  ↓
Evidence
```

The capability is fixed across this flow. The implementation beneath it can
change.

## Resolver

The resolver answers one question:

```
Resolve(capability, context) → implementation
```

It selects a registered implementation deterministically, considering
availability (for example, whether a credential or connected app is usable)
and preference (for example, a local implementation when local-first mode is
on). It may report that no implementation is available.

> The resolver determines availability. It does not grant authority.

## Provider replacement

A capability can have several implementations. For example, `weather.get` may
be fulfilled by a keyless provider by default and a keyed provider when one is
configured. Swapping the provider does not change:

- the capability identity;
- the permission identity;
- the evidence contract;
- the user-facing semantics.

Adding a provider is a registration change, never a model-facing change.

## Connected apps

A connected app is a user's authenticated connection to an external system
(Google Calendar, Home Assistant, GitHub, and so on). It represents external
identity, a credential reference, scopes, status, and health.

```
CONNECTED APP
    ↓
availability / identity
    ↓
implementation
    ↓
CAPABILITY
```

A connected app helps Ghost fulfil capabilities. It does not authorize them.
Having credentials does not mean Ghost may use them for a given operation —
the Permission Broker still decides.

> Connected app ≠ permission.

## Tools

A **tool** is an internal, model-facing invocation mechanism. It is how the
model requests a capability operation. Tools are implementation details, not
product concepts and not authority boundaries.

The model's tool name never becomes the permission identity. For example, the
`calendar` tool resolves to `calendar.read` or `calendar.modify` depending on
the requested operation, and authorization uses the capability identity.

## Skills

A **skill** is reusable knowledge or procedure that helps Ghost accomplish a
class of tasks using capabilities it already has. A skill may declare
requirements:

```
requires:
  - calendar.read
  - web.search
```

That is a **request**, never a grant. Ghost's normal capability and permission
system still authorizes each capability. A skill cannot grant authority, bypass
the broker, redefine capability risk, or create persistent automation simply by
being installed or read.

> Skill ≠ authority.

## MCP

MCP is an integration/protocol mechanism, not Ghost's authority model. Known
read-oriented MCP operations may map to a semantic capability; unknown or
mutating operations remain governed as a high-impact, scoped `mcp.execute`.

An MCP server authenticating successfully does not grant permission. MCP
cannot mint authority and cannot bypass evidence validation. MCP OAuth tokens
are currently held as an in-memory session cache, not as a persistent second
credential authority; persistent credentials are owned by Ghost's credential
boundary (`pkg/credentials`).

## Capability and arguments

The runtime distinguishes capability identity from operation parameters. For
example:

```
Capability:  calendar.modify
Arguments:   calendar=work, event=dentist, new_start=Friday 15:00
```

Authorization is evaluated against the capability plus the relevant
resource/scope — not against a bare capability name that then lets arbitrary
arguments flow through unchecked.

## Related concepts

See [Permission Broker](PERMISSION-BROKER.md) for authority and
[Execution](EXECUTION.md) for how implementations run.

## Implementation

The capability contract and resolver live in `pkg/capability`. Connected apps
live in `pkg/connectedapp`. The Permission Broker lives in `pkg/permissions`.
