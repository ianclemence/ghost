# Ghost Mobile API Contract (backend → mobile team)

The backend is the authority. The mobile app is a surface: it renders
backend semantics and never decides success, authorization, or truth.
Base URL: `https://<ghost>:<port>` (LAN) or relay tunnel. All `/v1/*`
endpoints below require device authentication unless noted.

## Authentication

- Pairing first (see existing pairing flow); then every request carries
  `X-Ghost-Device-ID` + `X-Ghost-Credential` headers (LAN) or the relay
  client token. Loopback callers are trusted.
- Failure: HTTP 401 with `{"error": {"code": "auth_required",
  "message": "Device authentication required."}}` (product language,
  no internals).

## Identity — "I'm talking to Ghost"

`GET /v1/identity` → `{"ok": true, "ghost": {"ghost_id", "name",
"owner", "agent": "agent-main"}}`. Render the Ghost name everywhere
(chat header, activity, permissions, routines). One Ghost; no model
picker semantics.

## Chat — `POST /v1/chat` (existing, unchanged)

Streams SSE: text deltas + `tool_status` + `clarify_request` events.
Contract: the final text is the response; success is stated by the
backend (never infer it client-side). New: approval asks arrive as
assistant text with `[allow_once] [always_allow] [deny]` actions AND
as structured cards below — prefer cards when present.

## Permissions & approval cards

- `GET /v1/permissions/requests?status=pending` → `{"requests": [{
  "id", "request_id", "capability", "action", "target", "reason",
  "risk", "status", "created_at", "expires_at",
  "card": {"request_id", "agent_id", "title", "description", "risk",
           "expires_at", "actions": [
             {"id": "allow_once", "label": "Allow once", "style": "primary"},
             {"id": "allow_always", "label": "Always allow", "style": "secondary"},
             {"id": "deny", "label": "Deny", "style": "danger"}]}}]}`
  Render `card` natively. Never render raw `continuation` (absent),
  tool args (absent), secrets (never present), or reasoning (absent).
- `POST /v1/permissions/resolve` `{id, grant: allow_once|allow_always|deny,
  scope}` → resolves or `resolve_failed` (expired/answered: show
  "no longer answerable", do not retry).
- `GET /v1/permissions/grants` → standing grants with exact
  `capability/action/scope` (show scope verbatim on "Always allow").
- `POST /v1/permissions/revoke` `{capability, action, scope}`.
- Backward compat: unknown fields must be ignored; `requests` without
  `card` (older backend) falls back to text actions.

## Activity — `GET /v1/activity?limit=&conversation_id=&since_seq=`

`{"activity": [{"id", "title", "kind", "state": running|waiting|
success|failed|cancelled|paused, "timestamp", "summary?", "detail?"}]}`
Render title + state glyph; `detail` on expand. `since_seq` resumes
after reconnect (pass last seen event seq; read-only replay, never
executes). Only user-safe content ever appears here.

## Routines — `/v1/routines`

- `GET` → `{"routines": [{id, name, instruction, timezone, status,
  next_run?, last_run?}]}`. Statuses: active/paused/waiting/completed/
  cancelled/failed. Never show cron expressions (use `instruction` +
  human schedule text from the server where provided).
- `POST` `{name, instruction, timezone, kind: cron|every|at,
  expr?, every_seconds?, at?, allowed_capabilities?}` → creates.
- `POST /v1/routines/{id}/{pause,resume,cancel,delete}`.

## Contexts — `/v1/contexts`, `/v1/contexts/switch`

- `GET` → list (`{id, kind, name}`). `POST {kind, name}` → create.
- `POST /v1/contexts/switch {session_key, context_id}` → moves a
  conversation's scope. Memory/permissions/capabilities follow the
  context server-side; the client only displays the active context.

## Connections — `GET /v1/connections`

`{"connections": [{id, provider, display_name, category, type,
status, capabilities}]}`. Statuses: connected / not_configured /
configuring / expired / revoked / invalid / error / disconnected.
Show status + Connect/Reconnect/Disconnect affordances. Values are
NEVER present (write-only secrets).

## Voice — `POST /v1/voice/turn`

`{audio_base64, mime?, session_key?, conversation_id?, speak?}` →
`{transcript, response_text, audio_base64?, mime?}`. Same runtime as
text (same identity/memory/permissions). Failures: `voice_unavailable`
(not set up), `transcribe_failed` (try again). Never retain audio
(server doesn't).

## Health — existing `/v1/health`, `/v1/doctor`

Appliance states; product language. Show "needs attention" items with
their server-provided remediation actions.

## Events/SSE, pairing, sessions, memory, scheduled

Unchanged existing contracts (chat SSE, pairing redemption, sessions,
`/v1/memory/*`, `/v1/scheduled/*`, `/v1/recall`). Additive fields may
appear; clients must ignore unknown fields.

## Outcome semantics (authoritative)

`success | failed | partially_completed | waiting_for_user |
waiting_for_configuration | waiting_for_authorization |
waiting_for_permission | temporarily_unavailable | offline | cancelled`.
The backend sets them; the client displays them. Waiting states are not
errors; offline/unavailable are not failures.

## Offline behavior

The app must distinguish (per server outcome): completed / waiting /
offline / unavailable / failed. Queueing outbound actions offline is a
client choice; the server never fabricates execution.

## Compatibility promise

 additive fields only; no renames/removals without versioning.
Authentication headers stable. `since_seq` cursors stable.
