# Ghost

> **Your Sovereign Intelligence.**  
> *An edge-native AI system that thinks locally, reacts instantly, and scales infinitely.*

---

## ⚡ What is Ghost?

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
- 32GB MicroSD / NVMe storage
- Telegram Account

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

### Windows

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

```bash
make install-service
```

### Commands

```bash
sudo systemctl status ghost
sudo journalctl -u ghost -f
sudo systemctl restart ghost
```

---

## 🔄 Updating Ghost

```bash
cd ~/ghost
git pull --rebase origin main
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

* Multi-agent system
* Autonomous reasoning

---

## 🔐 Sovereignty

* Local-first execution
* No forced cloud
* Full data ownership
* Secure API access

---

## 📄 License

MIT

