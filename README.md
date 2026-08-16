# Ghost

> **Your AI, Your Hardware.**  
> *A personal AI assistant that runs locally, responds instantly, and scales with you.*

---

## What is Ghost?

Ghost is a **personal AI assistant** that runs on your own hardware — combining:

- **Real-time local reflexes** (instant responses)
- **On-device reasoning** (private, fast, offline-capable)
- **Cloud intelligence** (for deep thinking when needed)

The result: An AI that feels **alive, responsive, and truly yours** — not dependent on the cloud.

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

**90% of interactions never leave your device**

---

## Core Features

- **Local-First**: Runs on your own hardware (Raspberry Pi, RK1, or x86)
- **Persistent Memory**: Continuous context via SQLite + HNSW Vector Index
- **Hybrid Intelligence**: Local-first routing with cloud fallback
- **Robust**: JSON Schema validation prevents hallucinated tool calls
- **Proactive**: Briefings, reminders, scheduled automation
- **Observable**: Diagnostics via `/doctor` and `GET /v1/doctor`

---

## Hardware

### Recommended
**RK1 (16GB RAM)** — Built-in NPU for AI acceleration

### Compatible
- Raspberry Pi 5 / CM5
- x86 mini-PCs

---

## Requirements

### Hardware
- Raspberry Pi 5 (8GB+) or RK1 (16GB)
- 32GB MicroSD storage
- Mobile phone with Ghost app

### Software: Ollama (Required for Local AI)

**Linux / Raspberry Pi:**

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull qwen3.5:0.8b
```

**Windows:**

1. Install from https://ollama.com  
2. Run: `ollama pull qwen3.5:0.8b`

---

## Quick Start

### Raspberry Pi (Recommended)

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

### First Boot

| Command | Description |
|---------|-------------|
| `ghost-firstboot` | Setup wizard (runs automatically on first boot) |

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
      "model": "kimi-k2.5",
      "temperature": 0.7
    }
  },
  "channels": {
    "telegram": { "enabled": true }
  }
}
```

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

---

## Running as a Service

### Raspberry Pi

```bash
sudo make install-ghost
```

Then:

```bash
ghost-firstboot  # Start setup wizard
ghost            # Start Ghost (after setup)
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

## Updating Ghost

### Manual Update

```bash
ghost update
```

### Auto-Update Daemon

```bash
ghost updater
```

This checks for updates every 6 hours automatically.

### Developer Mode

```bash
cd ~/ghost
git pull
make install && sudo systemctl restart ghost
```

---

## Memory System

* SQLite database
* HNSW vector index
* Episodic logs
* Reflective summaries

---

## Tech Stack

* Go (runtime)
* Ollama (local LLMs)
* SQLite + HNSW
* JSON Schema validation
* Mobile App API
* Linux (systemd)

---

## Evolution

### Phase 1
* API-first assistant

### Phase 2
* Local-first routing
* Reflex intelligence
* Reduced cloud usage

### Phase 3
* Multi-agent system
* Autonomous reasoning
* Skills self-improvement
* Trajectory compression
* Cross-session learning graph

### Phase 4
* First-boot wizard
* OTA updates
* Recovery mode
* Custom Linux distribution
* Hardware abstraction layer

---

## License

MIT
