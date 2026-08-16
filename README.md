# Ghost

> **Your Sovereign Intelligence.**  
> *An edge-native AI system that thinks locally, reacts instantly, and scales infinitely.*

---

## 👻 What is Ghost?

Ghost isn’t just an AI assistant.

It’s a **distributed intelligence system** that runs on your own hardware — combining:

- ⚡ **Real-time local reflexes** (instant responses)
- 🧠 **On-device reasoning** (private, fast, offline-capable)
- ☁️ **Cloud intelligence** (for deep thinking when needed)

👉 The result:  
An AI that feels **alive, responsive, and truly yours** — not dependent on the cloud.

---

## 🧠 The Architecture (What makes Ghost different)

Most AI apps:

```

You → Cloud → Response

```

Ghost:

```

You → Local Reflex → Local Brain → Cloud (only if needed)

````

### ⚡ Reflex Layer (Edge AI)
- Instant intent detection (<50ms)
- Command routing
- Wake-word + triggers
- Local embeddings

### 🧠 Local Brain
- Runs small LLMs via Ollama
- Handles memory, RAG, and tools
- Works offline

### ☁️ Cloud Brain (Optional)
- Kimi / OpenAI / Anthropic
- Used only for:
  - deep reasoning
  - coding
  - complex tasks

👉 **90% of interactions never leave your device**

---

## 🌟 Core Features

- **Sovereign**: Runs locally on your own hardware (RK1 recommended)
- **Persistent**: Continuous context via **SQLite** + **HNSW Vector Index**
- **Hybrid Intelligence**: Local-first routing with cloud fallback
- **Robust**: JSON Schema validation prevents hallucinated tool calls
- **Proactive**: Briefings, reminders, scheduled automation
- **Self-Modifying**: Full source available for on-device hacking
- **Observable**: Diagnostics via `/doctor` and `GET /v1/doctor`

---

## ⚙️ Hardware Philosophy

Ghost is built for **edge-first computing**.

### 🥇 Recommended
**RK1 (16GB RAM)**

Why:
- Built-in **NPU (AI acceleration)**
- Better performance than Raspberry Pi
- Enables real-time local intelligence

---

### 🥈 Compatible
- Raspberry Pi 5 / CM5
- x86 mini-PCs

👉 Works fine, but limited to **basic local intelligence**

---

### 🧠 Scaling Up
- Add GPU → run 7B–13B models locally  
- Full offline reasoning becomes possible

---

## 🛠️ Requirements

### Hardware

- RK1 (16GB) **recommended**
- Raspberry Pi 5 (8GB+) supported
- 32GB MicroSD storage
- Mobile phone with Ghost app

---

### Software: Ollama (Required for Local AI)

**Windows:**

1. Install from https://ollama.com  
2. Run:

```powershell
ollama pull qwen3.5:0.8b
````

**Linux / Raspberry Pi:**

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull qwen3.5:0.8b
```

---

## 🚀 Quick Start

### Appliance Mode (Recommended)

The easiest way to run Ghost on a Raspberry Pi:

```bash
git clone https://github.com/ianclemence/ghost.git
cd ghost
sudo make install-appliance
sudo reboot
```

After reboot, open `http://<pi-ip>` on your phone to complete setup:
1. Connect to WiFi
2. Create admin password
3. Select AI model
4. Connect the Ghost app

---

### Developer Mode

**Windows:**

Double-click `setup.bat`

---

### Linux / RK1 / Raspberry Pi

```bash
chmod +x setup.sh
./setup.sh
```

This script:

1. Installs dependencies
2. Installs Python tools
3. Installs Ollama
4. Creates `.env`
5. Generates `BRIDGE_SECRET`
6. Builds Ghost
7. Installs binary
8. (Optional) installs systemd service

---

## ⚙️ Configuration

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

---

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

## 🧭 Ghost CLI Commands

### Core

| Command     | Purpose                                   | Example             |
| ----------- | ----------------------------------------- | ------------------- |
| `onboard`   | Initialize config and workspace templates | `ghost onboard`     |
| `agent`     | Chat directly with Ghost in terminal      | `ghost agent`       |
| `dashboard` | Open the terminal operator dashboard      | `ghost dashboard`   |
| `gateway`   | Start multi-channel runtime               | `ghost gateway`     |
| `status`    | Show runtime status                       | `ghost status`      |
| `auth`      | Auth workflows                            | `ghost auth status` |
| `cron`      | Manage scheduled jobs                     | `ghost cron list`   |
| `skills`    | Manage skills                             | `ghost skills list` |
| `migrate`   | Migrate configs                           | `ghost migrate`     |
| `version`   | Show build info                           | `ghost version`     |

### Appliance

| Command              | Purpose                              | Example                    |
| -------------------- | ------------------------------------ | -------------------------- |
| `ghost-firstboot`    | Setup wizard for first boot          | `ghost-firstboot`          |
| `ghost-updater`      | OTA update daemon                    | `ghost-updater -url <url>` |

---

### Skills Subcommands

| Command                        | Purpose                 |
| ------------------------------ | ----------------------- |
| `ghost skills list`            | List installed skills   |
| `ghost skills install <repo>`  | Install from repository |
| `ghost skills remove <name>`   | Remove skill            |
| `ghost skills install-builtin` | Copy built-ins          |
| `ghost skills list-builtin`    | List built-ins          |
| `ghost skills search`          | Search registry         |
| `ghost skills show <name>`     | Show details            |

---

## 📱 Ghost Mobile

Ghost exposes a unified API on port **8766**:

* Chat
* Memory
* Voice
* Remote control

---

### API Setup

```env
GHOST_API_PORT=8766
BRIDGE_SECRET=your_secret_here
```

---

### Run Mobile App

```bash
cd ghost-app
npm install
npx expo start
```

---

### Tailscale Setup

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

```bash
tailscale ip -4
```

Use that IP in app settings.

---

## 🤖 Running as a Service

### Appliance Mode (Recommended)

```bash
make install-appliance
sudo systemctl start ghost
```

### Developer Mode

```bash
make install-service
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
# Set recovery mode in the service file or environment
GHOST_RECOVERY_MODE=1 ghost gateway
```

This starts a web UI at `http://ghost.local:8766` with:
- System status
- Logs viewer
- Config reset option
- Restart button

---

## 🔄 Updating Ghost

### Appliance Mode

```bash
ghost-update
```

That's it. Pulls latest code, rebuilds, and restarts.

### Developer Mode

```bash
cd ~/ghost
git pull
make install && sudo systemctl restart ghost
```

---

## 🧠 Memory System

* SQLite database
* HNSW vector index
* Episodic logs
* Reflective summaries

---

## 🤖 Tech Stack

* Go (runtime)
* Ollama (local LLMs)
* SQLite + HNSW
* JSON Schema validation
* Telegram + Mobile API
* Linux (systemd)

---

## 🔄 Evolution

### Phase 1

* API-first assistant

### Phase 2 (current)

* Local-first routing
* Reflex intelligence
* Reduced cloud usage

### Phase 3

* Multi-agent system (Mixture of Agents with parallel advisors + aggregator)
* Autonomous reasoning (reasoning chain tracking, background self-review)
* Skills self-improvement (learned skill refinement from execution patterns)
* Trajectory compression (interaction summaries for context window management)
* Cross-session learning graph (knowledge graph connecting skills, memory, trajectories)

### Phase 4: Ghost OS

* Appliance mode (first-boot wizard, OTA updates, recovery mode)
* Custom Linux distribution
* Multi-agent isolation
* Hardware abstraction layer

---

## 🔐 Sovereignty

* Local-first execution
* No forced cloud
* Full data ownership
* Secure API access

---

## 📄 License

MIT

