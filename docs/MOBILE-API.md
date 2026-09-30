<!-- Reference for how the Ghost app connects to a Pod and what the gateway
exposes. Moved out of the main README so that stays a front door; the content
is unchanged apart from this heading. Newer routes (live replies, tool servers,
the console password reset code, connected-app checks) are described in the
CHANGELOG entries for 0.24.90 and later. -->

# The Ghost app and the gateway API

Ghost exposes a unified API on port **8766**:

* Chat
* Memory
* Voice
* Remote control

### Connecting the Mobile App

The mobile app connects via device pairing — no manual API key configuration is needed.

#### Set up a brand-new Pod from the phone (phone-first)

The app can bring a fresh Pod online without opening a browser:

1. Start `ghost-web` on the Pod and read its setup code:
   `journalctl -u ghost-web | grep 'Setup code'`
2. In the app: **Connect a Ghost Pod → Set up a new Ghost Pod**
3. Enter the Pod address, the setup code, your name, and an owner password
4. The phone claims the Pod, then pairs automatically

If the gateway is still starting, setup still succeeds and the app offers the
manual pairing path as a fallback.

#### Same Network (LAN)

1. Open admin dashboard at `http://<pi-ip>` on any browser
2. Log in with your admin password
3. Navigate to Devices → "Connect another device"
4. Scan the QR code with the Ghost app
5. The app is now connected

#### Remote (Relay)

For when you're away from home:

```bash
ghost relay pair
```

This outputs a URI that you open on your phone. The relay tunnels traffic back to your Ghost device.

### Run Mobile App

```bash
cd ghost-app
npm install
npx expo start
```

### Tailscale Setup

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
tailscale ip -4
```

Use that IP in app settings. The gateway listens on the LAN
(`0.0.0.0:8766`), so same-network and Tailscale connections reach it directly
with device credentials; the relay tunnel is only needed off-network.

### Mobile API Endpoints

Key HTTP endpoints the app uses on port `8766`:

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/v1/health` | GET | Connectivity + latency check (authed; loopback bypass) |
| `/v1/chat` | POST | Send a chat message (SSE stream) |
| `/v1/history` | GET | Conversation history |
| `/v1/steering` | POST | Redirect / interrupt / abort the running agent loop |
| `/v1/clarify/respond` | POST | Answer an in-flight clarification question |
| `/v1/model` | GET/POST | Read active model + presets, switch model |
| `/v1/doctor` | GET | Diagnostics and service health checks |
| `/v1/tools` | GET | List available skills/tools |
| `/v1/identity` | GET | Owner/Ghost identity |
| `/v1/activity` | GET | User-safe activity |
| `/v1/permissions/requests` + `/v1/permissions/resolve` | GET/POST | Pending approvals |
| `/v1/routinefeed` | GET | The unified Routines feed (routines + scheduled items) |
| `/v1/routines` | GET/POST | Routines (product view over scheduled automations) |
| `/v1/goals` | GET/POST | Goals |
| `/v1/connected-apps` | GET/POST | Connected services |
| `/v1/intelligence/config` | GET/POST | AI config (masked keys, routing) |
| `/v1/ollama/models` + `/v1/ollama/pull` | GET/POST | Local model management |
| `/v1/voice/turn` | POST | Voice message transcription + reply (audio is never stored) |
| `/v1/files` | GET | Files the owner has sent (name, kind, size, date) |
| `/v1/files/{id}` | DELETE | Delete one uploaded file and its metadata |

All mobile API endpoints require device credentials (`X-Ghost-Device-ID` +
`X-Ghost-Credential` headers) unless the request arrives on loopback, which the
gateway trusts. Auth failures return `401` (`authentication_required` /
`authentication_failed`); the only public pairing endpoint is
`POST /v1/pairing/complete`, where the short-lived token is the authorization.

WebSocket messages on `/v1/ws` are broadcast per channel; `mobile` receives
`assistant_message`, `clarify_request`, `canvas_update`, `cron_update`, and
`progress_event` payloads. The app opens `/v1/ws` without auth headers
(React Native WebSockets can't set them).

### When the Pod is unreachable

The phone carries no model. Offline, the app keeps the last conversation
readable and holds new messages in an outbox that sends in order when the Pod
is back. Routines, home control, files, notifications, and memory stay on the
Pod. Each live reply shows where it ran: on your Pod, or which cloud model.

---

## API Authentication

### Device Authentication (Mobile App)

After pairing, the mobile app authenticates using:

```
X-Ghost-Device-ID: <device_id>
X-Ghost-Credential: <credential>
```

These headers must be included in every request to the gateway API.

### Owner Authentication (Web Dashboard)

The web dashboard uses session-based authentication:

1. POST to `/api/login` with admin password
2. Receive a session cookie (`ghost_admin_session`)
3. All subsequent requests include the cookie automatically

### Internal Authentication (Web Proxy, Relay, CLI)

Internal components run on the device itself and connect via loopback, which the
gateway trusts:
- Web proxy forwards requests to `127.0.0.1:8766`
- Relay client connects to `127.0.0.1:8766`
- Terminal agent connects to `127.0.0.1:8766`

No authentication headers are needed for loopback traffic. Requests arriving
from other machines on the LAN require valid device credentials.

---
