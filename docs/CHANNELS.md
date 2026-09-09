# Channel Support Matrix

A channel appears as connected/available/send-capable/receive-capable
only when that exact behavior exists. Anything else is a bug — file it
as one.

## Live channels (started by the channel manager when configured)

| Channel | Send | Receive | Voice notes transcribed | Notes |
|---|---|---|---|---|
| Telegram | yes | yes | yes (.ogg Opus) | Bot token |
| Discord | yes | yes | yes (audio attachments) | Bot + app tokens |
| Slack | yes | yes | yes (audio attachments) | Socket mode, bot + app tokens |
| WhatsApp | yes | yes | yes, via bridge media | **Requires a user-run bridge** (see below) |
| LINE | yes | yes | yes (.m4a via Get content) | Webhook + channel token |
| Email | yes | **no** | n/a | Send-only by design; the console labels it as such |
| SMS (Twilio) | yes | yes | yes, MMS `audio/*` parts | Webhook + account credentials |
| WeChat Work | yes | yes | yes (AMR via media/get) | Corp ID + secret + agent ID |

SMS and WeChat are configured by file/env only (no console UI yet);
they are fully live in the runtime, not experiments.

## WhatsApp bridge contract (Option B: explicit external dependency)

Ghost does not speak the WhatsApp protocol. It speaks to a **user-run,
whatsapp-web.js-compatible gateway** over websocket (`bridge_url`).
The bridge must deliver JSON messages shaped as:

```json
{
  "type": "message",
  "from": "<sender id>",
  "chat": "<chat id, defaults to sender>",
  "content": "<text, may be empty>",
  "media": ["<http(s) URL to download, or a path local to the Ghost host>"],
  "id": "<message id>",
  "from_name": "<display name>"
}
```

- Voice notes arrive as `media` entries; audio-looking entries
  (`.ogg`/`.opus`/`.m4a`/`.mp3`/`.wav`/`.amr`/…) are downloaded (URLs)
  or read (local paths) and transcribed. Anything else passes through
  untouched. Entries that are neither reachable URLs nor existing local
  files are skipped, never trusted.
- If the bridge is down, WhatsApp shows disconnected; Ghost keeps
  running. There is no bundled bridge — run and supervise it yourself,
  on the Ghost host (for local paths) or anywhere reachable (for URLs).

## Voice transcription coverage

Every channel above marked "yes" transcribes through the same local
sidecar when provisioned, with cloud fallback per the voice engine
order. Markers in chat read `[voice transcription: …]`, or
`[voice (transcription failed)]` — never silence, never a hallucinated
transcript.
