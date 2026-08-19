# Ghost

> **Your AI, Your Memory, Your Machine.**  
> *A personal AI that belongs to you — it lives on your hardware, remembers you, and works for you.*

---

## Documentation

- **[Product Strategy](docs/PRODUCT.md)** — who Ghost is for, what it sells, and how it makes money.
- **[Roadmap](docs/ROADMAP.md)** — the phased plan from the foundation for persistent identity to a personal AI you own.
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
- Local embeddings

### Local Brain
- Runs small LLMs via Ollama
- Handles memory, RAG, and tools
- Works offline

### Cloud Brain (Optional)
- Kimi / OpenAI / Anthropic
- Used only for deep reasoning, coding, complex tasks

An **Intelligence Router** decides where each task runs — local reflex, local
model, or cloud — based on capability, latency, privacy, cost, and available
hardware. You don't have to think about models; Ghost handles it.

---

## Core Features

- **Local-First**: Runs on your own hardware (Raspberry Pi, RK1, or x86)
- **Persistent Memory**: Continuous context via SQLite + HNSW Vector Index
- **Hybrid Intelligence**: Local-first routing with cloud fallback
- **Ghost Moves With You**: Replace your hardware — your identity, memory, skills, and configuration come along
- **Robust**: JSON Schema validation prevents hallucinated tool calls
- **Proactive**: Briefings, reminders, scheduled automation
- **Observable**: Diagnostics via `/doctor` and `GET /v1/doctor`

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

### Software: Ollama (Required for Local AI)

**Linux / Raspberry Pi:**

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull qwen3:0.6b
```

**Windows:**

1. Install from https://ollama.com  
2. Run: `ollama pull qwen3:0.6b`

---

## Quick Start

### Raspberry Pi (Recommended)

On a fresh device, install the prerequisites first (skip if you already have
`git`, `make`, Go, and Ollama):

```bash
sudo apt install -y git make golang-go
curl -fsSL https://ollama.com/install.sh | sh
ollama pull qwen3:0.6b
```

Then install Ghost:

```bash
git clone https://github.com/ianclemence/ghost.git
cd ghost
sudo make install-ghost
sudo reboot
```

After reboot, open `http://<pi-ip>` on your phone to complete setup:
1. Connect to WiFi
2. Create admin password
3. Select AI model
4. Connect the Ghost app

### How setup works

**Before setup:** `ghost-web.service` starts the setup wizard on port 80 and
opens the firewall for it. Completing the wizard:
1. Writes `/var/ghost/.setup-complete`
2. Starts the `ghost` service (port 8766)

**Always-on wizard:** the wizard stays running as an always-on service, so you can
reach it from your phone at any time at `http://<pi-ip>`:
- **Before setup:** shows the setup screen
- **After setup:** shows a login screen (your admin password) that opens the
  **admin dashboard** with tabs for:
  - **Home** — system health (CPU/memory/disk), service status, diagnostics
    checks, and one-click software updates
  - **AI** — provider, model, fallback models, API keys, and Ollama model
    management (list, pull, delete)
  - **Channels** — Telegram, Discord, and Email bot configuration plus the
    heartbeat interval
  - **System** — hostname, backup download, admin password, bridge-secret
    regeneration, and reboot
  - **Skills** — browse installed skills, edit bundled skills (edited
    skills are marked and never overwritten by updates), resync bundled
    skills, and install more from any public GitHub repo (including skills.sh)

The wizard and `ghost` are separate: wizard on port 80, API on port 8766.

If you ever want the wizard turned off entirely:
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

### Setup

| Command | Description |
|---------|-------------|
| `ghost-web` | Web console — setup wizard + admin dashboard (always-on service on port 80; setup screen before config, login after) |

---

## Configuration

### `.env` (Secrets)

```bash
cp .env.example .env
nano .env
```

```env
TELEGRAM_TOKEN=your_token_here
KIMI_API_KEY=your_key_here
ANTHROPIC_API_KEY=your_key_here
BRIDGE_SECRET=strong_secret_here
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

This starts a web UI at `http://ghost.local:8766` with:
- System status
- Logs viewer
- Config reset option
- Restart button

---

## Mobile App

Ghost exposes a unified API on port **8766**:

* Chat
* Memory
* Voice
* Remote control

### API Setup

```env
GHOST_API_PORT=8766
BRIDGE_SECRET=your_secret_here
```

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

WebSocket messages on `/ws` are broadcast per channel; `mobile` receives
`assistant_message`, `clarify_request`, `canvas_update`, `cron_update`, and
`progress_event` payloads.

---

## Tech Stack

* Go (runtime)
* Ollama (local LLMs)
* SQLite + HNSW
* JSON Schema validation
* Mobile App API
* Linux (systemd)

---

## License

MIT
