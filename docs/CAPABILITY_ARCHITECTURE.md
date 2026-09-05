# Ghost Capability Architecture (substrate)

Ghost is a persistent personal AI entity with memory, capabilities,
permissions, and the ability to act — not a chatbot with tools.

```
                         GHOST (pkg/ghost)
                      Persistent Entity
         id · name · owner · status · timezone · locale
                      primary Agent (+ future kinds)
   ┌──────────────────────┼──────────────────────┐
   │                      │                      │
 BRAIN               MEMORY /              PERSONAL MODEL
 (providers,         PERSONAL               (personalcontext:
 local/cloud)        CONTEXT                  append-only, temporal)
   │                      │                      │
   └──────────────────────┼──────────────────────┘
                          │
                    CAPABILITIES (skills/capability.go)
                    id · risk · readiness · allowed tools
                          │
              ┌───────────┼───────────┐
              │           │           │
            TOOLS        APPS       DEVICES
      (provider-backed,  (integra-  (trust lattice,
       deterministic)    tions)      capabilities only)
              │           │           │
              └───────────┼───────────┘
                          │
                 PERMISSION BROKER (pkg/permissions)
                 Intent→Capability→Risk→ALLOW/ASK/DENY
                 durable requests · scoped grants · expiry
                          │
                  EXECUTION (agent loop + tools)
                          │
               CANONICAL EVENTS (pkg/cevents)
               redacted at publish · ordered · SQLite
                          │
          ┌───────────────┼───────────────┐
          │               │               │
       ACTIVITY         CHAT           ROUTINES
   (pkg/activity      (SSE,             (pkg/routines
    chips)            mobile)            on scheduler)
          │               │               │
          └───────────────┼───────────────┘
                          │
              WEB / MOBILE / VOICE (channels in,
              same runtime — never separate brains)
```

## Ghost Identity (pkg/ghost)

One canonical source for "which Ghost is this": `state/ghost-entity.json`
(id minted once, adopted from ghoststate identity when present), owner in
`state/ghost-owner.json`, agents in `state/ghost-agents.json`. Exactly one
primary agent in V1 (`agent-main`); `RegisterAgent` allows future kinds
(work/research/…) but rejects a second primary. Conversations belong to
Ghost; sessions never mint identity.

## Capability Model

Capabilities are runtime-registered (`skills.GetCapability`), never
LLM-invented. Each declares id, risk (`read_only`/`low_risk`/
`consequential`/`high_impact`), readiness, allowed tools, providers.
Lifecycle: REGISTERED → READY/NEEDS_SETUP → EXECUTING → SUCCESS/FAILURE,
with the existing readiness + product-outcome system authoritative (no
second framework). Provider-backed tools (`weather_now`,
`flight_status`, …) are the deterministic primary path.

## Permission Broker (pkg/permissions)

Central authority between capability resolution and consequential
execution — never scattered per-tool, never prompt-based. Modes
ask/auto/full/custom; verdicts ALLOW/ASK/DENY; grants allow_once /
allow_always(scoped) / deny. Risk matrix: read_only always allows;
low_risk allows except in ask/custom; consequential asks except in full;
high_impact always asks; unknown fails closed. ASK creates a durable
SQLite request (idempotent per request_id, TTL-bounded, restart-safe);
approval chat replies ("allow once"/"deny") resolve + consume exactly
once and re-execute deterministically with preserved continuation
(secrets stripped). allow_always stores exact-scope grants; revoke
removes them. Expired approvals are unusable.

## Event Stream (pkg/cevents)

One canonical model: id, type, request/session/conversation/ghost/agent/
routine ids, timestamp, monotonic seq, visibility (`product.Visibility`),
status, redacted payload. Taxonomy covers conversation, reasoning
(product-level, never chain-of-thought), capability, tools, permissions,
memory, integrations, routines, system, errors. Durable types persist in
SQLite; transient ones (progress heartbeats) stay in NDJSON + memory.
`SSEForm` returns ok=false for non-user-visible events — internal events
can never accidentally serialize. Subscriptions filter by owner/request/
session; panicking listeners can't break publish; retention via Prune.

## Activity (pkg/activity)

Chips derived ONLY from user-visible canonical events with allowlisted
titles: ◌ running / ✓ success / ! waiting / × failed. Three layers:
chip ("Weather checked"), expanded detail (safe provenance: "Open Meteo"),
diagnostics (explicit opt-in: provider, latency, request_id). Raw types,
manifests, schemas, paths, and secrets structurally cannot project.

## Credentials (pkg/credentials)

Vault over the existing AES store + secrets file + per-integration
readers. Normal code uses `Ref()` metadata (status, never values);
`Use()` lends a secret to exactly one server-side function. Lifecycle:
STORE → CONFIGURING → VALIDATE → CONNECTED, with auth failures marking
INVALID/REVOKED (transport blips preserve good state). UI lists
Connections as states. Disconnect deletes secrets. OAuth entries resolve
presence, never content.

## Routines (pkg/routines)

Adapter over `scheduled.Store` (no duplicate tables): routine =
automation item + metadata sidecar (instruction, allowed capabilities,
ownership). `Run` executes through the standard pipeline with
idempotency keys; NEEDS_* outcomes park the run as waiting (runnable,
not terminal); SUCCESS/FAILED/CANCELLED transition scheduler state.
Unattended runs never inherit unlimited permissions.

The scheduled executor is routine-aware (`setupScheduledService`):
items with routine metadata run through `routines.Run` with the agent
invoked synchronously (`ProcessDirectWithChannel`), routine-scoped gate
context, permission-block detection via pending-request evidence (not
text parsing), result delivery to the origin channel, and
routine.started/completed/waiting/failed events. Non-routine items keep
the legacy bus path. Duplicate firings are rejected by idempotency key.

Natural language ("Every Monday at 9 remind me to…") parses
deterministically (`routines.ParseIntent` over the existing schedule
parser; one-time patterns stay reminders), proposes once, confirms via
durable pending continuation, and creates through `routines.Service` —
delivery target recorded at creation. Console Routines screen lists,
pauses, resumes, cancels, and creates routines.

## Channels (pkg/interaction)

Every inbound message becomes one `Request` envelope (channel, channel
identity, conversation, context, owner, ghost, agent). Web, mobile,
voice, Telegram, CLI share the runtime; channel differences are data,
not code paths.

## Voice (pkg/voice)

`SpeechRecognizer` / `SpeechSynthesizer` interfaces; existing
file-based transcribers adapt via `FileTranscriberAdapter`. Final
transcripts convert to the canonical inbound shape; partials never enter
the runtime. TTS is presentation over normal responses.

Productized as `POST /v1/voice/turn` (push-to-talk): base64 audio →
Groq transcription → same `ProcessDirectWithChannel` runtime →
optional edge-tts speech. No audio stored (10MB cap, temp files
removed); unavailable providers return honest product outcomes.
`Engine.InputAvailable/OutputAvailable` report per-direction readiness.

## Contexts (pkg/contexts)

Scoped environments in one Ghost: personal (default), work, home,
travel, project. Pure-function isolation: memory visibility by scope
tags (global shared, foreign-scoped invisible), capability allowlists,
file-root confinement. No graph database.

Enforced, not just modeled: `Entry.Scopes` tags (additive, old entries
stay global); `CurrentInScope` filters all retrieval paths (prompt
digest, fast-path recall, semantic dedup); the permission gate denies
out-of-allowlist capabilities per session context; sessions stick to
contexts (`/v1/contexts`, `/v1/contexts/switch`) across restart.
Personal writes stay global in V1; non-personal contexts tag writes.

## Devices (pkg/devices/trust.go)

Trust lattice unknown→paired→trusted, anything→revoked; new devices
start paired, never trusted. Registration rejects unrestricted
endpoints (`shell`/`exec`/`root`). `CanInvoke` requires trust +
connection + declared capability.

First device-class proof: Home Assistant (`pkg/providers/hass` +
`hass` tool) — states read, actuates validated against
domain.service/entity shape (injection rejected), consequential risk so
the broker asks first, credentials from the vault path. "Turn off the
bedroom lights" flows device → capability → permission → verified
execution → event → activity chip.

## Rules enforced

1. LLM has no execution authority (broker gate sits in code).
2. Capabilities registered by runtime, never invented.
3. Consequential execution passes the broker.
4. Meaningful actions produce canonical events.
5. Activity derives from safe events only.
6. Internals never auto-escalate visibility.
7. Credentials via the vault boundary.
8. Secrets never enter conversation/memory/activity/events.
9. Routines use the interactive pipeline.
10. Voice/channels are interfaces, not brains.
11. Contexts scope one Ghost.
12. Devices expose capabilities, not shells.
13. Runtime evidence determines success.
14. Replay describes; never re-executes consequential actions.
15. Local-first: SQLite + files; relay is transport, not truth.
