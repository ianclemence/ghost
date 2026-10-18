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

A turn Ghost starts on its own (a routine, a promise, an idea it acted on)
also runs under a wall-clock limit, because nobody is there to notice a hung
page or a provider that never answers. The default is 20 minutes;
`GHOST_UNATTENDED_TIMEOUT` takes a Go duration (`45m`), and `0` removes it.
The tool-call cap still bounds how much such a turn does.

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

## What executed code can reach on the network

Commands Ghost runs (`exec`, `sandbox`) run in a bubblewrap sandbox. By default
the sandbox shares the Pod's network, so a command can reach the internet and
also the Pod's own services, the router and every device on the LAN. Set
`GHOST_EXEC_EGRESS` to narrow that:

| Mode | What a sandboxed command can reach |
|------|------------------------------------|
| `open` (default) | Everything the Pod can. |
| `guard` | The public internet only. Loopback, private and link-local ranges, carrier-grade NAT and the cloud metadata address are refused. |
| `allowlist` | Only hosts named in the allow file. |

In `guard` and `allowlist` the sandbox has no network of its own. Its only way
out is a proxy Ghost runs, which resolves each name itself, judges the address
it actually dials (so a public name pointing at a private address is refused),
and logs every allow and block. Hosts go in `$GHOST_DIR/egress-allow.txt`
(`GHOST_EXEC_EGRESS_ALLOW` overrides the path), one per line: an exact host,
`*.example.com`, or an IP. In `guard` an entry also opens that one private host,
which is how a single LAN device is reached. The file sits beside the
configuration, not in the workspace, so code the agent runs cannot edit it.

The cost of enforcing is that only HTTP and HTTPS work from inside the
sandbox: `curl`, `pip`, `npm`, `go` and git over https do; ssh, ping, MQTT and
raw sockets do not. That is why `open` stays the default. If the proxy cannot
start, commands do not run rather than running unguarded.

The Pod's service also runs under a memory ceiling (`MemoryHigh` 70% and
`MemoryMax` 85% of the machine) and a task limit, so a runaway browser or build
slows and is killed before it can starve the Pod or SSH.

## Leaks and faults

Ghost's instructions carry a private marker that is meaningless outside them.
If it appears in the arguments of any tool call, the call is refused and
logged: something Ghost read has steered it into copying its context out.

Each turn also records what it ran on (release, model, a fingerprint of the
instructions, the tool set) and, when a reply looks like a fault (stray
characters of a script nobody used, a reply stuck repeating itself, a
replacement character), a trace event naming the sign. Replies are never
changed on that basis.

## Secrets never reach the model

Credentials live in the vault and are used inside the tool that needs them
(the browser fills a saved login without the model ever seeing the password).
As a last line of defence the registry scrubs every tool result — the text the
model reads, the text shown to the owner, and the evidence — of every secret
value the vault holds (API keys, channel tokens, website-login passwords),
so a tool that echoes a page, prints a file or logs a request still cannot
carry one out. This is by exact value, on top of the shape-based masking in
`pkg/redact`.

## Files the owner sends

An attachment is stored as an upload in the workspace (`uploads/<id>/<name>`)
with its type detected from the bytes, never from the sender's claim. Ghost
opens it with the ordinary workspace-confined tools (`doc_parser` for
documents and spreadsheets, `read_file` for text, `video_frames` for video,
the model itself for images); files Ghost cannot open are declared unreadable
rather than guessed at. Uploads are listable and deletable from every surface
and leave on their own after 30 days. Raw voice audio is never stored.

## Related concepts

See [Evidence](EVIDENCE.md) for how outcomes are proven, and
[Canonical Events](CANONICAL-EVENT.md) for how they are recorded.

## Implementation

Execution surfaces live in `pkg/tools` (registry, sandboxing, isolation),
`pkg/providers` (external adapters), `pkg/browser` and `pkg/computer`
(governed surfaces), and `pkg/mcp` (third-party tools).
