# Ghost Mobile Contract (authoritative, code-verified)

**«Ghost decides. Mobile renders.»** The backend is authoritative over
permissions, execution, evidence, state, and outcomes. Mobile never infers
success, authorization, validity, or truth from prose. Additive-only
compatibility: clients must ignore unknown JSON fields and unknown SSE
frames.

- Base URL: `http://<ghost>:<api-port>` on LAN, or the relay tunnel.
- All `/v1/*` routes require **device authentication** unless marked public.
- Status codes: `200` success, `400 invalid_request/action_failed`,
  `401 authentication_required/authentication_failed`, `403 forbidden`,
  `404 not_found`, `409 conflict`, `500 io_error/…`, `503 unavailable`.
  Error bodies: `{"error":{"code":"<code>","message":"<product language>"}}`.
- `ok:true` present on all successful JSON objects.

This document is verified against the implementation (`cmd/ghost/internal_api.go`,
`pkg/…`). Areas marked **NOT SUPPORTED** have no backend transport yet; they
are explicit limitations, not rendering bugs.

## 1. Authentication / pairing

- Public: `POST /v1/pairing/complete` `{token, display_name, platform}`
  and `POST /v1/pairing/redeem` `{token}`. Redeem an invitation once →
  `{device_id, credential, paired_at, ghost_name}`. The plaintext
  `credential` is returned **exactly once**; the server stores only its hash.
- Authenticated: every other request presents `X-Ghost-Device-ID` +
  `X-Ghost-Credential` (LAN). Loopback peers are trusted. The relay adds its
  own client auth and tunnels to loopback.
- Management (all authenticated): `POST /v1/pairing/invitations`
  (→ pairing token + QR payload), `GET /v1/pairing/devices`,
  `POST /v1/pairing/revoke {device_id}`, `POST /v1/pairing/cancel`.
- Failure: `401` with product language; distinguish `authentication_failed`
  (bad credential) from `authentication_required` (missing) on `/v1/health`.

## 2. Identity

`GET /v1/identity` (read-only) → `{ok, ghost:{ghost_id, name, owner,
agent:"agent-main"}}`. Render `ghost.name` everywhere. One Ghost; no model /
provider / agent picker semantics anywhere.

## 3. Conversation — one persistent relationship

Default conversation: a client that never sets a session selects
`mobile:default` (one continuous relationship per appliance). A client may
pin a conversation with `X-Ghost-Session` / `?session=` / body
`session_key`; treat it as opaque. There is intentionally **no chat
creation UI** — send to an existing key to continue, or a fresh key to start
a new one (owner/admin console lists conversations).

### `POST /v1/chat` (consequential: sends a user turn)

Request: `{request_id?, content, session_key?, channel?, chat_id?,
media?:[{base64,mime}], media_items?, metadata?:{timezone?}}`.
`request_id` is the correlation token echoed in lifecycle frames; it is NOT
an idempotency key (a duplicate POST duplicates the turn).

SSE stream (`text/event-stream`). Each frame is `data: <json>`. Frame kinds:

| Frame | Shape | Meaning |
|---|---|---|
| text delta | JSON string `"…"` | streamed assistant tokens |
| `tool_status` | `{type, tool, label}` | “Ghost is searching…” (never raw args/secrets) |
| `clarify_request` | `{type, question_id, question, choices?, request_id}` | turn is waiting for the user |
| `lifecycle` | `{type:"lifecycle", request_id, state}` | `queued` → `agent_processing` → `channel_delivery` |
| `lifecycle` terminal | `{type:"lifecycle", request_id, state:"completed", outcome}` | **authoritative outcome** |
| `[DONE]` | bare literal | stream end (success path) |

Terminal `outcome` (backend-stated): `success` | `failed` |
`waiting_for_user` | `waiting_for_permission`. The client ends its
“working” state on `state:"completed"` and renders by `outcome`. On
`failed` an additional JSON-string `"Error: …"` frame precedes it
(kept for compatibility). Other waiting classes
(`waiting_for_configuration`, `waiting_for_authorization`,
`temporarily_unavailable`, `offline`, `cancelled`) surface through
Activity/doctor outcomes, not chat prose; the client must not parse model
text to decide success.

Keep-alive: SSE comment `: keepalive` every 15 s. **Durable turn identity:**
send a stable `request_id`; identical `request_id` + `session` never
re-executes. Repeating a completed turn returns
`{ok, replay:true, status:"completed", outcome}`; a live/waiting turn returns
`409 turn_in_progress` — reconnect is observation, never re-submission.
`GET /v1/chat/turn?session=&request_id=` (read-only) returns the durable turn
record (pending/running/waiting/completed/failed/interrupted). On a process
crash mid-turn, stale turns are marked `interrupted` at boot.
Reconnect (older behavior): on a dropped stream the in-flight turn is not
resumable mid-stream; the client should NOT resend the same `request_id`.
Permission waits are durable across restart (see §6).

### History / session management

- `GET /v1/history?limit&offset&since` (read-only) → `{messages:[{id, role,
  content, timestamp, media_type?, media_url?}], total}`. Filters out
  `tool`/`system` and internal-looking rows (skill dumps, prompts, paths).
- `GET /v1/sessions` (read-only) → `{sessions:[{id, title, message_count,
  last_activity}]}` — used only by the owner/admin “conversations” surface;
  not a product selector for the primary chat.
- `DELETE /v1/messages` (archives the conversation),
  `DELETE /v1/message?id=`, `DELETE /v1/session?id=`, `POST
  /v1/session/rename {old_id,new_id}` — owner controls; consequential.
- Live push: `GET /v1/ws` (WebSocket, LAN) is a server→client fan-out of
  `mobile`-channel messages (`assistant_message`) plus `canvas_update`,
  `cron_update`, `clarify_request`, `progress_event`. Frame:
  `{channel, chat_id, content, metadata, type?, id?, session_id?,
  timestamp?}`. No client→server commands. Optional; polling is always
  valid.

## 4. Approvals / permission cards

- `GET /v1/permissions/requests?status=pending` (read-only) → `{ok,
  requests:[{id, request_id, agent_id, capability, action, target?,
  reason?, risk, status, created_at, expires_at, card:{request_id,
  agent_id, title, description, risk, expires_at, actions:[
  {id:"allow_once", label:"Allow once", style:"primary"},
  {id:"allow_always", label:"Always allow", style:"secondary"},
  {id:"deny", label:"Deny", style:"danger"}]}}]}`. Render `card` natively.
  Ignore `continuation`/tool args if ever present; secrets are never
  present.
- `POST /v1/permissions/resolve` `{id, grant: allow_once|allow_always|
  deny, scope?}` (consequential) → `{ok, request}`; `400 resolve_failed`
  when expired/answered (“no longer answerable”).
- `GET /v1/permissions/grants` (read-only); `POST
  /v1/permissions/revoke {capability, action, scope}` (consequential).
- The card is the source of truth for allowed actions; mobile never invents
  transitions.

## 5. Activity (human-readable timeline)

`GET /v1/activity?limit=&conversation_id=&since_seq=` (read-only) →
`{ok, activity:[{id, event_id, seq, title, kind, state, timestamp,
summary?, detail?}]}`. Titles are server narrative (“Sent the reply”,
“Waiting for approval”, …); `state` ∈ running|waiting|success|failed|
cancelled|paused. `kind` is the safe projected event category — the client
should not build logic on raw types. Reconnect/resume: pass the last
received chip’s `seq` as `since_seq` (exclusive, ascending, read-only
replay — never re-executes). Live: no activity push exists; poll, or use
the `/v1/ws` channel for live chat/progress. Live push: `GET
/v1/activity/stream?since_seq=` (SSE, read-only) streams user-visible
activity chips `{seq, type:"activity", title, kind, state, time, summary}`
resuming from the cursor with no duplicates. Internal/tool-only events,
reasoning, secrets, and raw payloads never reach this feed.

## 6. Memory — “what Ghost knows about you”

- `GET /v1/memory/self` (read-only, owner) → `{ok, entries:[{id, kind,
  label, title, summary, domain, domain_label, value, created_at,
  reinforce_count, reinforced_at}], notes:[…], you:[…]}`. `entries` are
  the structured beliefs (rendered as prose), `notes` = agent notes,
  `you` = user profile lines. Group client-side by `kind`/`domain_label`.
  No scopes, context ids, vectors, embedding terms, or raw journal are
  exposed.
- `POST /v1/memory/self/forget` `{id}` or `{target:"user"|"memory",
  entry}` (consequential, owner-wide forget).
- `GET /v1/memory/files`, `GET /v1/memory/file?name=`,
  `DELETE /v1/memory/file?name=` — legacy markdown notes (read/list/delete).
- `GET /v1/recall?query=` — conversation recall excerpts + optional
  summary; not the “About me” view.
- `GET /v1/workspace/files` & `GET /v1/workspace/file?name=` — generic
  artifact preview (text/images up to a bound). **Internal estate is
  excluded**: the memory journal (`personal-context/`), runtime `state/`,
  event logs, SQLite database files, and curated note stores
  (`knowledge/self/`) are neither listed nor readable here (403). Memory is
  consumed through the dedicated endpoints above.
- There are **no user-facing contexts**: no selectors, no scope/vector/
  context-id terminology in any response the UI should show.

## 7. Connected Apps

`GET /v1/connections` (read-only) → `{ok, connections:[{id, provider,
display_name, category, type, status, capabilities, …}]}`. Statuses:
`connected | not_configured | configuring | expired | revoked | invalid |
error | disconnected`. Secret values are never present. Connect/disconnect:
`POST /v1/connections/{id}` `{"value":"…"}` stores an API-key/token secret
into the existing `.secrets.json` credential boundary and never echoes it;
`POST /v1/connections/{id}/disconnect` removes it. OAuth-only providers
(e.g. Google Calendar) are refused on this surface
(`oauth_required`: their secret never exists in a shareable form) and keep
their secure browser handoff.

## 8. Routines (management, not a builder)

- `GET /v1/routines` (read-only) → `{ok, routines:[{id, name, instruction,
  timezone, status, allowed_capabilities?, schedule_kind?, schedule_expr?,
  schedule_every_seconds?, next_run?, last_run?, …}]}`. Effective statuses:
  `active | paused | completed | cancelled | failed`. Show `instruction` +
  next/last run; never expose raw cron as product copy.
- `POST /v1/routines` `{name, instruction, timezone, kind:cron|every|at,
  expr?|every_seconds?|at?, allowed_capabilities?}` (consequential, creates
  through the real scheduler path).
- `POST /v1/routines/{id}/{pause,resume,cancel,delete}` (consequential).

## 9. Voice

`POST /v1/voice/turn` `{audio_base64, mime?, session_key?,
conversation_id?, speak?}` (consequential) → `{ok, transcript,
response_text, audio_base64?, mime?}` when `speak`. Errors:
`voice_unavailable`, `transcribe_failed`, `invalid_request`. Audio is never
retained server-side.

## 10. Device / appliance / health

- `GET /v1/health` (read-only) → `{status:"ok", timestamp, version,
  uptime_s}`.
- `GET /v1/doctor` (read-only) → `{status: ok|warning|error, checks:[…],
  channels:{…}}` with per-check `status/message` and remediation copy.
  Render “needs attention” items from server-provided messages.
- Restart / reboot / update are available on the device API:
  `POST /v1/device/restart`, `POST /v1/device/update` (consequential,
  durable), `GET /v1/device/operations` and
  `GET /v1/device/operations/{id}` (read-only status). Operations survive
  disconnect/reconnect and are reconciled at boot (a pre-restart operation
  is marked completed once the device returns; a runner that is not
  configured yields an honest `failed`/`restart_unavailable` state — never
  a fake success). The web-console `/api/admin/*` remains a separate,
  web-session-only surface.

## 11. Browser & Computer (transient surfaces)

**NOT SUPPORTED as client transports today.** The runtime has real,
proven, brokered browser and computer executors, and now a **Live Surface
plane** (`pkg/live`): an in-process domain primitive that tracks each active
browser/computer surface, its safe observation, and its control owner
(`ghost | user | none`) with expiring user takeover leases. When a plane is
attached, the browser/computer gates consult it before executing, so a
human takeover pauses Ghost and release does not auto-resume (revalidation
required). This is a runtime primitive only.

The Live Surface endpoints expose the plane to authenticated devices (all
behind device auth):

- `GET /v1/live/surfaces?kind=browser|computer` — discovery (state, control
  owner, updated, sequence).
- `GET /v1/live/surfaces/{kind}/{id}` — surface state.
- `GET /v1/live/surfaces/{kind}/{id}/observation` — safe observation
  (title/url/text) plus an internal `image_base64`/`mime_type` when a real
  screenshot was captured by the executor.
- `POST /v1/live/surfaces/{kind}/{id}/takeover` — acquire an expiring user
  control lease (device-scoped); **Ghost pauses** while held.
- `POST /v1/live/surfaces/{kind}/{id}/release` — clear user control; Ghost
  does **not** auto-resume (surface stays `paused` until revalidated).
- `GET /v1/live/surfaces/{kind}/{id}/stream` — SSE of surface state changes
  (read-only, 500ms poll, `surface_closed` terminal).
- `POST /v1/live/surfaces/{kind}/{id}/resume` - return a paused surface to
  Ghost. Fails closed (`resume_refused`) unless paused with no user in
  control; the agent gates revalidate ownership, permission, and lease on
  the next real operation.

Surface lifecycle vocabulary (server-set): `created | starting | active |
waiting | user_control | paused | completed | failed | disconnected |
expired`, control `ghost | user | none`. Gates set `waiting` while an
approval pends and `failed` when execution errors; `user_control` pauses
Ghost; expiry and reconcile fail closed to `paused`/`none`. Dead user
leases are reaped every minute, so a dead phone can never hold control.

Conversation linkage: when a surface becomes relevant to a turn, the
runtime publishes a mobile-channel `surface_update`
`{surface_id, kind, session_id}` frame (identity only - clients fetch
authoritative state). The same frame is emitted on takeover, release,
and resume.

Errors use product vocabulary: `surface_not_found`, `no_observation`,
`control_conflict` (another device holds control / cross-device release),
`resume_refused`, `forbidden`. Observation is always read-only; control always requires an
explicit lease. Screenshots of the **physical appliance display**
(`/v1/screenshot`) and app launch (`/v1/open`) remain legacy local-machine
commands, not browser/computer transports.

## 12. Artifacts (runtime-validated handoffs)

Artifacts are things Ghost hands you: a workspace file, a short text
result, or a link. The model proposes; the runtime disposes. An artifact
exists only after server-side validation, so mobile never infers success
from prose.

- `GET /v1/artifacts?conversation_id=&limit=` - a conversation's
  artifacts, newest first (conversation isolation by session key, the
  same unit as history/activity).
- `GET /v1/artifacts/{id}` - one artifact, revalidated on read (a
  deleted file surfaces as `unavailable` with a reason, never a break).

Artifact shape: `{id, kind: file|text|link, title, summary, state:
available|unavailable, reason?, actions[], evidence_request_id?,
created_at}` plus the payload (`path` for files, inline bounded `text`,
`url` for links). File references are workspace-confined, existence- and
size-checked, and blocked from the protected estate
(`personal-context/`, `state/`, event logs, knowledge stores,
databases); secret-shaped text is redacted before persistence.

Actions are backend-declared render hints (`preview | open |
download` applicability computed server-side). They grant no execution:
consequential follow-ups ("book it") travel as new conversation turns
through the normal capability + Permission Broker path. File bytes are
served by the existing bounded `/v1/workspace/file` preview, not by the
artifact endpoints.

## 13. Product outcomes (authoritative)

Shared vocabulary (server-set): `success | failed | partially_completed |
waiting_for_user | waiting_for_configuration | waiting_for_authorization |
waiting_for_permission | temporarily_unavailable | offline | cancelled`.
Waiting and unavailable states are not failures and not errors. Chat turns
report the terminal subset via the `completed` lifecycle outcome (§3);
Activity and doctor report the rest.

## 14. Security invariants for mobile integration

- Mobile gains no bypass: permissions, credentials, evidence, execution,
  and identity stay server-authoritative. Assume the client is
  compromised: all consequential endpoints are governed, secrets never
  leave the vault, the memory journal/vectors/scopes are never serialized,
  and activity replay is read-only.
- Never trust client-supplied owner/context/task/permission fields.

## Summary of known limitations (do not build against these)

1. Browser/computer **live pixel streaming (video) is not implemented** — the
   transport is bounded structured observation + snapshots/SSE change events,
   matching the V1 product (no CDP/X11 raw access, no fake streaming).
2. `POST /v1/device/restart` and `POST /v1/device/update` require a
   configured host runner (`GHOST_DEVICE_RESTART_CMD` /
   `GHOST_DEVICE_UPDATE_CMD`); without one the durable operation fails
   honestly rather than pretending.
3. OAuth connected-app handoff (e.g. Google Calendar) is console/browser-side;
   the device API refuses to proxy OAuth secrets (`oauth_required`).
4. Durable turn identity prevents duplicate execution on reconnect but does
   not replay the dropped mid-stream token deltas; resuming an in-flight
   turn mid-stream is not supported (attach via `/v1/chat/turn` + history).
