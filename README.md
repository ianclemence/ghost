# Ghost

> **Your AI. Your Memory. Your Machine.**
> *A personal AI that belongs to you — it lives on your hardware, remembers you, and works for you.*

---

## What Ghost does for you

Ghost runs the recurring admin of your life, so you don't have to think about it:

- **It briefs you.** “Every Monday at 9, prepare my weekly brief.” Ghost reads your calendar and mail, writes the brief, and has it waiting for you.
- **It remembers for you.** Birthdays, preferences, who's who, what you decided last month — Ghost keeps it and recalls it when it matters.
- **It handles the follow-through.** Renewals, check-ins, reminders, the thing you'd otherwise forget — Ghost does them on time and tells you when it's done.
- **It warns you ahead.** “Your sister's birthday is in a week.” Ghost notices and speaks up before it's too late.
- **It makes things for you.** Reports, plans, checklists, links — Ghost produces durable work you can open and keep, and shows it as a card right in the conversation.

You say what you want in plain language — in a chat, on your phone, or out loud.
Ghost figures out the rest.

And it asks before it does anything consequential, with proof it actually did it.
That last part matters more than it sounds — see [Why trust is the point](#why-trust-is-the-point).

---

## Why trust is the point

Most “AI agents” say “Done” and hope you believe them. Ghost is built so you
don't have to. The core rule: **the model is not the authority.** Runtime
execution evidence decides whether an action happened — a model saying
“Done” is a claim, not proof.

- **It asks before consequential actions.** The model cannot self-grant authority; one Permission Broker decides *allow / ask / deny*, and unknown risk fails closed.
- **Every answer says where it ran.** Local model, home Pod, or cloud — you always know.
- **Every action leaves proof.** What Ghost says it did is backed by runtime evidence, not a language model's optimism.
- **Your data stays on your machine.** Memory, permissions, and identity live on your hardware; cloud models are optional intelligence providers, and offline-local capabilities keep working when the network does not.

You get a capable assistant. You keep the guarantees.

---

## How it works

You express a goal. Ghost interprets it against your memory and context, picks a
capability, and asks the Permission Broker whether it may proceed. Authorized
work executes through a replaceable implementation. Runtime evidence proves what
actually happened. A canonical event records it, and memory, activity, and
routines all consume that one record.

```
User → Ghost → intent/memory/context → Permission Broker → capability
     → execution → evidence → events/activity → response
```

You choose an outcome — **Local / Hybrid / Cloud** — and the runtime derives the
details (models, fallbacks, context sizes). RAG is always enabled. You never need
to configure temperature, top-p, quantization, or routing tables.

**One product, two surfaces.** The **Ghost app** is the daily driver — talk,
approve, review, connect. The **Web Console** is the owner control plane —
system, security, devices, skills, channels, memory. They overlap only where
they must (pairing, first-run setup, model selection), so the same task never
has two different homes.

---

## Core features

- **Routines** — one place for everything Ghost runs for you: recurring instructions, reminders, and scheduled actions. You say it in chat (“every Monday at 9…”); Ghost figures out the rest.
- **Artifacts** — reports, plans, checklists, and links Ghost produces on your behalf. A runtime-validated handoff shown as a card in the conversation; acting on one is a normal approved request.
- Persistent memory with retrieval and context isolation
- Consequential actions governed by a permission broker (the model can't self-grant)
- Deterministic capability execution — no fabricated live data
- Durable routines with duplicate prevention and idempotency
- Canonical event evidence with user-safe activity
- Provider fallback, retries, circuit breaking, honest unavailability
- Credential vault with redaction and backup exclusion
- Offline-honest behaviour
- `ghost verify`, `ghost benchmark`, and the Golden Conversation Suite

---

## Requirements

### Hardware
Ghost runs on any Linux device. These are the reference device targets:
- Raspberry Pi 5 (8 GB+) or RK1 (16 GB)
- 256 GB NVMe SSD (recommended) or 32 GB microSD storage
- Mobile phone with Ghost app

### Software

Ghost is a single Go binary — no external runtime is required for the core
system. For local AI, you can install Ollama (optional):

```bash
curl -fsSL https://ollama.com/install.sh | sh
```

Or configure any OpenAI-compatible provider through the Web Console.

---

## Quick Start

### Raspberry Pi (Recommended)

On a fresh device, install the prerequisites first (skip if you already have
`git`, `make`, Go, and `ffmpeg`):

```bash
sudo apt install -y git make golang-go ffmpeg
```

`ffmpeg` is a hard dependency of the voice stack: local speech-to-text
converts voice notes (ogg/m4a/mp3) with it, and local speech synthesis uses
it to deliver mp3 replies. The voice engines and models are installed
automatically by `make install-ghost` (verified against pinned checksums).

**Optional cloud fallback for speech replies:** for languages without a
local voice, Ghost can fall back to the keyless Edge TTS (an unofficial
community client). Install it only if you want that:
`pip install edge-tts`. Local speech does not need it and never phones home
when the local voice is present.

Then install Ghost:

```bash
git clone https://github.com/ianclemence/ghost.git
cd ghost
sudo make install-ghost
sudo reboot
```

After reboot, open `http://<pi-ip>` in a browser to complete setup:
1. Set up Ghost — name yourself, name Ghost, create an owner password
2. Ghost is ready — you're in the Web Console
3. Optionally configure AI providers (Ollama, OpenAI, Anthropic, etc.)
4. Optionally connect your phone by scanning a QR code

### How setup works

**Before setup:** `ghost-web.service` starts the Web Console on port 80 (with
the `-force` flag, so it stays running permanently). The first-run wizard
appears automatically. Completing the wizard:
1. Writes `/var/ghost/.setup-complete`
2. Starts the `ghost` gateway service (port 8766)
3. The Web Console transitions from wizard to login → control plane

**Setup code:** the first configure must present a one-time setup code, which
proves you are at the device (not a host elsewhere on the network). While Ghost
is unconfigured, `ghost-web` mints a fresh code on every start and prints it:

```bash
journalctl -u ghost-web | grep 'Setup code'
```

Enter it in the wizard's first step. The code is cleared once setup completes;
restarting `ghost-web` invalidates the previous one.

**Web Console:** the `ghost-web` service runs on port 80 and serves as Ghost's
persistent control plane — the place where you own, configure, understand, and
take care of Ghost.
- **Before setup:** shows the first-run wizard (identity, password, AI, phone pairing)
- **After setup:** shows a login screen (your owner password) that opens the
  **Web Console** with sections organized around the product:

  **Ghost** — what Ghost is and does:
  - **Home** — is Ghost okay, what has it been doing, does it need you
  - **Routines** — one list for everything Ghost runs for you: recurring instructions (“every Monday at 9…”), reminders, and scheduled tasks. You say it in chat; Ghost infers the shape. (Backed by `/v1/routinefeed`; the internal scheduler and routine models stay unified underneath.)
  - **Memory** — browse, search, and manage what Ghost remembers
  - **Activity** — what Ghost has done, with the reason each consequential action asked or ran
  - **Intelligence** — local and cloud AI, model management, routing
  - **Skills** — installed capabilities, enable/disable, install from GitHub

  **Connect** — how Ghost reaches people and services:
  - **Devices** — paired phones, secure QR pairing flow
  - **Channels** — Telegram, Discord, Slack, WhatsApp, and Email configuration
  - **Apps** — the services Ghost can act on (Calendar, Gmail, Outlook, Spotify, Home Assistant, and more); connect and manage credentials here

  **System** — how Ghost itself is maintained:
  - **System** — hardware, services, updates, diagnostics
  - **Security** — owner password, active sessions, failed sign-in visibility, backups, recovery, standing permissions
  - **Help** — guidance for what Ghost actually does
  - **About** — version and product information

The Web Console and `ghost` gateway are separate services: console on port 80,
API on port 8766. The console proxies authenticated requests to the gateway.

If you ever want the Web Console turned off:
```bash
sudo systemctl disable --now ghost-web
```

---

## Updating

### On a device already running Ghost

```bash
cd ghost                 # the cloned repo
sudo ghost update        # git pull + rebuild + redeploy + restart services
```

`sudo ghost update`:
1. `git pull` from GitHub
2. Builds all binaries and deploys them
3. Reinstalls the systemd services
4. Restarts `ghost` and the always-on wizard

Requires root. If you no longer have a repo clone on the device, clone one:

```bash
sudo apt install -y git make golang-go   # if not installed
git clone https://github.com/ianclemence/ghost.git && cd ghost
sudo make install-ghost
```

These commands run as root, and git exempts root from its repository-ownership
check, so updating a user-owned clone works with no extra configuration. If you
ever run git in the repo as a *different* non-root user and hit git's "dubious
ownership" error, allow the path for that user:

```bash
git config --global --add safe.directory /home/<user>/ghost   # run as <user>, not root
```

### Auto-Update Daemon

```bash
sudo ghost updater
```

Checks for updates every 6 hours automatically (`git pull` + rebuild + redeploy + restart).

### Developer Mode

**Windows:** Double-click `setup.bat`

**Linux / Raspberry Pi:**

```bash
chmod +x setup.sh
./setup.sh
```

Developer Mode installs to the same canonical location as production
(`/usr/local/bin/ghost`) and uses the same service. There is exactly **one**
`ghost` binary: a second copy earlier in `PATH` used to shadow the installed
one and make updates appear not to take. `ghost update` removes any stale copy
automatically, and `ghost status` warns if one is ever found.

---

## Commands

### Core Commands

| Command | Description |
|---------|-------------|
| `ghost` | Chat with Ghost in the terminal (`ghost agent` is an alias) |
| `ghost serve` | Start Ghost (main service) |
| `ghost status` | Show system status |
| `ghost update` | Pull latest changes and rebuild |
| `ghost updater` | Run auto-update daemon |

### Management Commands

| Command | Description |
|---------|-------------|
| `ghost onboard` | Initialize configuration |
| `ghost auth` | Manage authentication |
| `ghost skills` | Manage skills |
| `ghost state` | Export / import / inspect Ghost State archives |
| `ghost model` | View or switch the active model |
| `ghost reset` | Factory reset (`all --exclude=devices,secrets`, scopes, `--no-restart`) |
| `ghost reset-password` | Reset the admin dashboard password (requires `--force`) |
| `ghost replay` | Reconstruct an execution trace from canonical events (read-only) |
| `ghost version` | Show version info |

### Evaluation Commands

| Command | Description |
|---------|-------------|
| `ghost verify` | Real personal AI checks: identity, memory, capabilities, governance, automation, events, activity, credentials, offline, security |
| `ghost benchmark` | Personal AI benchmark + Ghost Core Score + hard governance gates + local history |
| `ghost golden` | Golden Conversation Suite — model-agnostic natural-language evaluation (`--model --suite --cases --offline --json --compare`) |

### Relay Commands

| Command | Description |
|---------|-------------|
| `ghost relay run` | Connect to relay server for remote access |
| `ghost relay pair` | Generate pairing token for phone |
| `ghost relay clients` | List paired clients |
| `ghost relay revoke <token>` | Revoke client access |

### Setup

| Command | Description |
|---------|-------------|
| `ghost-web` | Web Console — setup wizard + control plane (always-on service on port 80; wizard before setup, login after) |

### Chat Slash Commands

| Command | Description |
|---------|-------------|
| `/help` | Show help and tool list |
| `/clear` | Clear current session history. `/clear all` clears all chat history |
| `/reset` | Factory reset: `/reset all` wipes everything including secrets and paired devices. `/reset all --exclude=devices,secrets` spares scopes. Selective: `/reset <scope> [<scope>...]` with scopes `chats` `memory` `activity` `automations` `context` `devices` `secrets` `model` |
| `/context` | Show what Ghost believes about you |
| `/forget` | Forget a belief or session (`/forget <topic>` / `/forget session <id>`) |
| `/remind` | Set a reminder (`/remind buy milk in 10m`) |
| `/model` | Show or switch AI model |
| `/doctor` | Diagnostics |
| `/status` | System status |
| `/skills` | List skills |
| `/install <url>` | Install skill |
| `/tools` | Show tool schemas |
| `/think <msg>` | Deep reasoning mode |
| `/compress` | Summarize and compact context |
| `/personality` | List or switch personality (`default` `hacker` `creative` `teacher` `minimal` `adaptive` — adaptive learns your style) |
| `/affect` | Show relational state (affinity, mood) — aggregates only, cleared by `/reset context` |

---

## Configuration

### Configuration Precedence

Ghost uses a clear precedence model for configuration:

1. **Environment variables** (highest priority) — runtime overrides
2. **`.secrets.json`** — sealed secrets (AES-256-GCM; API keys, channel tokens)
3. **`config.json`** — persistent configuration (model, providers, channels)
4. **Defaults** (lowest priority)

### `.secrets.json` (Secrets)

Secrets (API keys, channel tokens) are stored in `.secrets.json` with `0600` permissions. They are configured through the Web Console, not by editing files directly.

**Never edit `.secrets.json` manually** — use the Web Console to configure providers and channels.

### `.env` (System Overrides)

The `.env` file contains only system-level configuration, not secrets:

```bash
cp .env.example .env
nano .env
```

```env
GHOST_API_PORT=8766
TZ=UTC
```

### `config.json` (Behavior)

```bash
cp config/config.example.json config/config.json
```

```json
{
  "agents": {
    "defaults": {
      "model": "ollama/qwen3:0.6b",
      "temperature": 0.7
    },
    "model_list": [
      { "name": "local", "provider": "ollama", "model": "ollama/qwen3:0.6b" },
      { "name": "claude", "provider": "anthropic", "model": "claude-sonnet-4" },
      { "name": "gpt", "provider": "openai", "model": "gpt-4o" }
    ]
  },
  "channels": {
    "telegram": { "enabled": true }
  }
}
```

**Switching models at runtime** — use `/model` in any chat (e.g. Telegram):
- `/model` — show the current model and list saved presets
- `/model <preset-name>` — switch to a named preset from `model_list`
- `/model <provider:model>` — switch to an arbitrary provider/model (e.g. `anthropic:claude-sonnet-4`, `ollama:qwen3:0.6b`)

The selection is persisted to `config.json` and takes effect immediately for new turns.

---

## Skills Subcommands

| Command | Description |
|---------|-------------|
| `ghost skills list` | List installed skills |
| `ghost skills install <repo>` | Install from repository |
| `ghost skills remove <name>` | Remove skill |
| `ghost skills install-builtin` | Copy built-ins |
| `ghost skills list-builtin` | List built-ins |
| `ghost skills search` | Search registry |
| `ghost skills show <name>` | Show details |
| `ghost skills sync` | Sync bundled skills — update unchanged skills, preserve user edits |

---

## Running as a Service

### Raspberry Pi

```bash
sudo make install-ghost
```

Then:

```bash
ghost-web  # Start web console (setup wizard + admin dashboard)
ghost      # Start Ghost (after setup)
```

### Service Commands

```bash
sudo systemctl status ghost
sudo journalctl -u ghost -f
sudo systemctl restart ghost
```

### Recovery Mode

If Ghost fails to start, enable recovery mode:

```bash
GHOST_RECOVERY_MODE=1 ghost serve
```

This starts a web UI at `http://127.0.0.1:8766` (localhost only) with:
- System status
- Logs viewer
- Config reset option
- Password reset
- Restart button

**Security note:** Recovery mode is bound to `127.0.0.1` — it cannot be accessed from other devices on the network. This ensures only someone with physical access to the device can use recovery.

The recovery server auto-shuts down after 15 minutes.

---

## Security Architecture

Ghost uses a layered security model designed for a self-hosted personal AI.

Consequential actions pass through the **Permission Broker**; secrets live behind
a **credential vault** boundary (write-only from the UI, presence-only in
events); events/activity/API/logs/backups are redacted by construction; and
context isolation is enforced at retrieval and execution — not by prompts.

Model browser calls are additionally governed by a runtime **browser gate**
that enforces server-side binding, the Permission Broker, session isolation,
and runtime evidence. Computer control follows the same authority model:
a bounded operation taxonomy with explicit placement and durable leases, all
enforced by the broker before any executor runs.

### Authentication

| Mechanism | Purpose | Used By |
|-----------|---------|---------|
| Owner password | Protects the Web Console | Web browser (session cookie) |
| Device credential | Authenticates mobile app and API access | Mobile app, CLI tools |

### Secrets Storage

| Secret | Location | Permissions |
|--------|----------|-------------|
| Admin password | `/var/ghost/data/admin.hash` | `0600` |
| API keys, channel tokens | `/var/ghost/config/.secrets.json` | `0600` |
| Device credentials | SQLite database | Database-level |
| Pairing tokens | SQLite database | Database-level |

Credential rules enforced at runtime:

- secrets are never printed, logged, serialized to events/SSE/activity/APIs, or
  archived in backups (backup walkers use the centralized exclusion boundary)
- OAuth credentials are stored locally and only their presence/state is
  exposed; tokens never travel to the UI or the model
- the model cannot read, grant, or revoke permissions

### How Pairing Works

1. Owner opens admin dashboard → Devices → "Connect another device"
2. Web UI generates a QR code (`ghost://pair?v=1&pod=...&token=...`)
3. Mobile app scans QR code (token expires in 5 minutes)
4. Ghost validates token (atomic delete to prevent replay)
5. Ghost issues a unique device credential (shown once, stored in SecureStore)
6. All future requests use `X-Ghost-Device-ID` + `X-Ghost-Credential` headers

After pairing, the QR token disappears from the equation. Each device gets its own unique credential.

### Gateway Binding

The gateway listens on the LAN (`0.0.0.0:8766`) with a layered trust model. Loopback peers (web proxy, relay client, terminal agent) are trusted and need no credential headers. Other machines on the network must present valid per-device credentials on every request — unauthenticated LAN requests are rejected with structured errors. The only credential-free endpoint is pairing redemption, where the short-lived single-use token is the authorization. The relay server forwards remote app traffic to the gateway via localhost on the device.

### Directory Permissions

| Directory | Permissions | Contents |
|-----------|-------------|----------|
| `/var/ghost/` | `0700` | Ghost installation root |
| `/var/ghost/config/` | `0700` | Configuration and secrets |
| `/var/ghost/data/` | `0700` | Admin password hash, metadata |
| `/var/lib/ghost/workspace/` | `0755` | Skills, memory, sessions (`/var/ghost/workspace` is a legacy fallback) |

---

## Mobile App

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
| `/v1/models/catalog` | GET | Phone-local model catalog (Mini only) |
| `/v1/sync/ops` | GET/POST | Memory-sync op push/pull |
| `/v1/voice/turn` | POST | Voice message transcription + reply |

All mobile API endpoints require device credentials (`X-Ghost-Device-ID` +
`X-Ghost-Credential` headers) unless the request arrives on loopback, which the
gateway trusts. Auth failures return `401` (`authentication_required` /
`authentication_failed`); the only public pairing endpoint is
`POST /v1/pairing/complete`, where the short-lived token is the authorization.

WebSocket messages on `/v1/ws` are broadcast per channel; `mobile` receives
`assistant_message`, `clarify_request`, `canvas_update`, `cron_update`, and
`progress_event` payloads. The app opens `/v1/ws` without auth headers
(React Native WebSockets can't set them).

### Offline phone (travel cache)

With Ghost Mini downloaded, the phone answers and collects (`remember ...`
notes + queued messages) while the Pod is unreachable, then syncs on
reconnect. Routines, home control, files, notifications, and full memory stay
on the Pod. Chat shows where each answer ran: phone vs home Pod.

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

## License

MIT
