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

Keep-alive: SSE comment `: keepalive` every 15 s. Reconnect: on a dropped
stream the in-flight turn is not resumable (no durable turn checkpoint);
the client should re-send the user intent as a fresh turn. Permission
waits are durable across restart (see §6). The session history is loaded
from the DB, so a resumed conversation continues from persisted messages.

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
the `/v1/ws` channel for live chat/progress. Internal/tool-only events,
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
error | disconnected`. Secret values are never present. Connect/OAuth
handoff and disconnect are **NOT currently exposed on the device API**:
connection writes happen through the web console / OAuth callbacks. Mobile
may show status + capability summaries and route “connect/repair” to the
console for V1.

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
- Restart / reboot / update are **NOT exposed on the device API** (they are
  web-console `/api/admin/*` actions). Document as limitation for V1.

## 11. Browser & Computer (transient surfaces)

**NOT SUPPORTED as client transports today.** The runtime has real,
proven, brokered browser and computer executors, but there is **no** mobile
endpoint to: list a live browser/computer session, read the current
observation/state, receive a screenshot/frame stream, learn who controls it
(Ghost vs user), request/release takeover, or revalidate control after a
takeover. Do not fake or synthesize these. When the product needs
browser/computer visualization + takeover, a new, separately-designed
transport (snapshot/observation + control-ownership plane) must be added
below the existing Permission Broker / evidence / lease authority; mobile
must never open a second browser or drive a raw executor. Screenshots of
the **physical appliance display** (`/v1/screenshot`) and app launch
(`/v1/open`) are legacy local-machine commands, not browser/computer
transports.

## 12. Product outcomes (authoritative)

Shared vocabulary (server-set): `success | failed | partially_completed |
waiting_for_user | waiting_for_configuration | waiting_for_authorization |
waiting_for_permission | temporarily_unavailable | offline | cancelled`.
Waiting and unavailable states are not failures and not errors. Chat turns
report the terminal subset via the `completed` lifecycle outcome (§3);
Activity and doctor report the rest.

## 13. Security invariants for mobile integration

- Mobile gains no bypass: permissions, credentials, evidence, execution,
  and identity stay server-authoritative. Assume the client is
  compromised: all consequential endpoints are governed, secrets never
  leave the vault, the memory journal/vectors/scopes are never serialized,
  and activity replay is read-only.
- Never trust client-supplied owner/context/task/permission fields.

## Summary of known limitations (do not build against these)

1. No live browser/computer observation/takeover transport (see §11).
2. No device-API restart/reboot/update (see §10).
3. No connected-app connect/disconnect on the device API (see §7).
4. Chat turns are not durably resumable mid-stream (see §3 reconnect).
5. `/v1/activity` has no push; poll with `since_seq`.
