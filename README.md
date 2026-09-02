# Ghost

> **Your AI. Your Memory. Your Machine.**  
> *A personal AI that belongs to you — it lives on your hardware, remembers you, and works for you.*

---

## Documentation

- **[Product Strategy](docs/PRODUCT.md)** — who Ghost is for, what it sells, and how it makes money.
- **[Roadmap](docs/ROADMAP.md)** — the phased plan from the foundation for persistent identity to a personal AI you own.
- **[Connection Flow](docs/CONNECTION_FLOW.md)** — how users set up Ghost and connect devices.
- **[Implementation plans](docs/plans/)** — detailed plans for the cloud relay, install experience, OTA updates, and telemetry.
- **[Testing](docs/TESTING.md)** — how to test Ghost.

---

## What is Ghost?

Ghost is the **persistent home of your personal AI**. Plug it in, connect your
phone, and talk to it — it remembers you, does things for you, and keeps working.
It runs on hardware you own, works offline, and keeps your data on-device.

Under the hood that combines:

- **Real-time local reflexes** (instant responses)
- **On-device reasoning** (private, fast, offline-capable)
- **Cloud intelligence** (for deep thinking when needed)

Ghost is not an appliance that happens to run an LLM. It is a personal AI that
lives somewhere persistent: your memory, identity, skills, tools, and automations
have a home. That is what makes it *yours* — not dependent on the cloud.

---

## Architecture

Most AI apps:

```
You → Cloud → Response
```

Ghost:

```
You → Local Reflex → Local Brain → Cloud (only if needed)
```

### Local Reflex Layer
- Instant intent detection (<50ms)
- Command routing
- Wake-word + triggers
- Memory-first recall (SQLite FTS + personal-context)

### Local Brain
- Runs small LLMs via Ollama (optional)
- Handles memory, tools, and automations
- Works offline

### Cloud Brain (Optional)
- Kimi / OpenAI / Anthropic
- Used only for deep reasoning, coding, complex tasks

An **Intent Triage** system decides where each task runs — fast-path (memory),
local model, or cloud — based on capability, latency, privacy, cost, and
available hardware. You don't have to think about models; Ghost handles it.

---

## Core Features

- **Local-First**: Runs on your own hardware (Raspberry Pi, RK1, or x86)
- **Persistent Memory**: Continuous context via SQLite + personal-context files
- **Hybrid Intelligence**: Local-first routing with cloud fallback
- **Ghost Moves With You**: Replace your hardware — your identity, memory, skills, and configuration come along
- **Robust**: JSON Schema validation prevents hallucinated tool calls
- **Proactive**: Briefings, reminders, scheduled automation
- **Observable**: Diagnostics via `/doctor` and `GET /v1/doctor`

---

## Security Architecture

Ghost uses a layered security model designed for a self-hosted appliance.

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

### How Pairing Works

1. Owner opens admin dashboard → Devices → "Connect another device"
2. Web UI generates a QR code (`ghost://pair?v=1&pod=...&token=...`)
3. Mobile app scans QR code (token expires in 5 minutes)
4. Ghost validates token (atomic delete to prevent replay)
5. Ghost issues a unique device credential (shown once, stored in SecureStore)
6. All future requests use `X-Ghost-Device-ID` + `X-Ghost-Credential` headers

After pairing, the QR token disappears from the equation. Each device gets its own unique credential.

### Gateway Binding

The gateway listens on the LAN (`0.0.0.0:8766`) with a layered trust model. Loopback peers (web proxy, relay client, TUI dashboard) are trusted and need no credential headers. Other machines on the network must present valid per-device credentials on every request — unauthenticated LAN requests are rejected with structured errors. The only credential-free endpoint is pairing redemption, where the short-lived single-use token is the authorization. The relay server forwards remote app traffic to the gateway via localhost on the device.

### Directory Permissions

| Directory | Permissions | Contents |
|-----------|-------------|----------|
| `/var/ghost/` | `0700` | Ghost installation root |
| `/var/ghost/config/` | `0700` | Configuration and secrets |
| `/var/ghost/data/` | `0700` | Admin password hash, metadata |
| `/var/ghost/workspace/` | `0755` | Skills, memory, sessions |

---

## Hardware

Ghost runs on any Linux device and is not tied to any particular board. Think of
it as a **control plane** (a Raspberry Pi 5, RK1, or similar always-on device that
hosts Ghost's memory, identity, and automations) with optional **compute** attached
(x86 mini-PC, NPU, or GPU for heavier local models). Recommended for the control
plane: **RK1 (16 GB)** for built-in NPU acceleration.

Your Ghost's identity lives in the software, not the hardware — upgrade your box
and your Ghost moves with you.

See the [Roadmap](docs/ROADMAP.md) for supported hardware, capability tiers, and minimum requirements.

---

## Requirements

### Hardware
Ghost runs on any Linux device. These are the reference appliance targets:
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
`git`, `make`, and Go):

```bash
sudo apt install -y git make golang-go
```

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

**Web Console:** the `ghost-web` service runs on port 80 and serves as Ghost's
persistent control plane — the place where you own, configure, understand, and
take care of Ghost.
- **Before setup:** shows the first-run wizard (identity, password, AI, phone pairing)
- **After setup:** shows a login screen (your owner password) that opens the
  **Web Console** with sections organized around the product:

  **Main** — what Ghost does:
  - **Home** — is Ghost okay, what has it been doing, does it need you
  - **AI** — local and cloud intelligence, model management, routing
  - **Memory** — browse, search, and manage what Ghost remembers
  - **Activity** — timeline of conversations, automations, memory writes
  - **Automations** — scheduled tasks (briefings, research, check-ins)
  - **Skills** — installed capabilities, enable/disable, install from GitHub

  **Connections** — how Ghost reaches people and services:
  - **Devices** — paired phones, secure QR pairing flow
  - **Channels** — Telegram, Discord, Slack, WhatsApp, and Email configuration

  **System** — how Ghost itself is maintained:
  - **System** — hardware, services, updates, diagnostics
  - **Security** — owner password, active sessions, backups, failed sign-in visibility
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

### On a fresh device

Setting up a brand-new device? Follow the
[Quick Start](#raspberry-pi-recommended) instead — it covers prerequisites
(dependencies, Ollama, model) and the install from zero in one place.

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

---

## Commands

### Core Commands

| Command | Description |
|---------|-------------|
| `ghost gateway` | Start Ghost (main service) |
| `ghost agent` | Chat directly in terminal |
| `ghost dashboard` | Launch operator TUI |
| `ghost status` | Show system status |
| `ghost update` | Pull latest changes and rebuild |
| `ghost updater` | Run auto-update daemon |

### Management Commands

| Command | Description |
|---------|-------------|
| `ghost onboard` | Initialize configuration |
| `ghost auth` | Manage authentication |
| `ghost cron` | Manage scheduled tasks |
| `ghost skills` | Manage skills |
| `ghost version` | Show version info |

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

---

## Configuration

### Configuration Precedence

Ghost uses a clear precedence model for configuration:

1. **Environment variables** (highest priority) — runtime overrides
2. **`.secrets.json`** — persistent secrets (API keys, channel tokens)
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
GHOST_RECOVERY_MODE=1 ghost gateway
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

## Mobile App

Ghost exposes a unified API on port **8766**:

* Chat
* Memory
* Voice
* Remote control

### Connecting the Mobile App

The mobile app connects via device pairing — no manual API key configuration is needed.

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

Use that IP in app settings.

### Mobile API Endpoints

Key HTTP endpoints the app uses on port `8766`:

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/v1/health` | GET | Connectivity + latency check |
| `/v1/chat` | POST | Send a chat message (SSE stream) |
| `/v1/steering` | POST | Redirect / interrupt / abort the running agent loop |
| `/v1/clarify/respond` | POST | Answer an in-flight clarification question |
| `/v1/model` | GET/POST | Read active model + presets, switch model |
| `/v1/sessions` | GET | List recent sessions with titles and activity |
| `/v1/doctor` | GET | Diagnostics and service health checks |
| `/v1/tools` | GET | List available skills/tools |

All mobile API endpoints require device credentials (`X-Ghost-Device-ID` + `X-Ghost-Credential` headers).

WebSocket messages on `/v1/ws` are broadcast per channel; `mobile` receives
`assistant_message`, `clarify_request`, `canvas_update`, `cron_update`, and
`progress_event` payloads.

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
- TUI dashboard connects to `127.0.0.1:8766`

No authentication headers are needed for loopback traffic. Requests arriving
from other machines on the LAN require valid device credentials.

---

## License

MIT
