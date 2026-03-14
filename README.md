# Ghost Pi

> "Your autonomous, private AI companion."

Ghost is a personal AI assistant designed for Raspberry Pi. It merges a lightweight Go-based agent with the advanced reasoning of **Kimi K2.5** (256k context), providing a responsive and continuous AI presence.

## 🌟 Core Features

- **Sovereign**: Runs natively on your Raspberry Pi 5.
- **Persistent**: Maintains a continuous thread of context via **SQLite** database and **HNSW Vector Index** (RAG).
- **Intelligent**: Powered by Kimi K2.5 (or other LLMs) for complex reasoning, coding, and vision.
- **Robust**: Built-in JSON Schema validation for tool parameters to prevent hallucinations.
- **Proactive**: Wakes up to brief you on news, schedule, and reminders.
- **Self-Modifying**: Includes full source code for on-device hacking.

## 🛠️ Requirements

### Hardware

- **Raspberry Pi 5** (8GB RAM recommended)
- **32GB MicroSD Card** (or larger)
- **Telegram Account**

### Software: Ollama (Required for Local AI)

To run Ghost locally, you need **Ollama** installed and running.

**Windows:**

1. Download and install Ollama from [ollama.com](https://ollama.com).
2. Once installed, open your terminal (Command Prompt or PowerShell) and run:
   ```powershell
   ollama pull qwen3.5:0.8b
   ```

**Linux / Raspberry Pi:**

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama pull qwen3.5:0.8b
```

After installing, ensure the model name matches your configuration. Then run the following command to build the Ghost binary:

```bash
go build ./cmd/ghost
```

## 🚀 Quick Start

### 🪟 Windows (Recommended)

Double-click `setup.bat` to install skills, build, and run automatically.

### 🍓 Raspberry Pi / Linux (Recommended)

Run the all-in-one setup script:

```bash
chmod +x setup.sh
./setup.sh
```

This script will:

1. Install system dependencies (ffmpeg, adb, etc.)
2. Build the Ghost binary
3. (Optional) Install as a system service

---

### Configuration

Create a `.env` file in the project root with your API keys:

```bash
cp .env.example .env
nano .env
```

Add your keys:

```bash
# Telegram Bot Token (from @BotFather)
TELEGRAM_TOKEN=your_token_here

# Your Telegram User ID (from @userinfobot) - Critical for privacy!
TELEGRAM_USER_ID=your_id_here

# Moonshot AI API Key (from platform.moonshot.cn)
KIMI_API_KEY=your_key_here

# Config directory location (relative to project root)
GHOST_CONFIG_DIR=config
```

Alternatively, you can edit `config/config.json` directly.

---

## 📱 Ghost Bridge (Mobile App)

### 1. Add to Ghost's .env

```env
# ── ghost-bridge settings (add to existing Ghost .env) ──────────────────

BRIDGE_PORT=8765
BRIDGE_SECRET=pick_a_strong_secret_here

# Absolute paths (adjust username if not 'pi')
GHOST_DB_PATH=/home/pi/ghost/workspace/ghost.db
MEMORY_DIR=/home/pi/ghost/workspace/memory

# Optional: system prompt prepended to every request
GHOST_SYSTEM_PROMPT=You are Ghost, a sovereign AI on a Raspberry Pi. Be concise.

# Optional: comma-separated command prefixes to allow beyond the safe defaults
# ALLOWED_CMDS=python3,ollama,curl

# Optional: override screenshot command
# SCREENSHOT_CMD=scrot /tmp/ghost-bridge-screen.png
```

### 2. Build and run manually (first test)

```bash
cd ~/ghost/bridge
go mod tidy
go build -o ghost-bridge .
./ghost-bridge
# 👻 Ghost Bridge running on 0.0.0.0:8765
```

### 3. Deploy and Install Service (Recommended)

The easiest way to install is using the included Makefile, which handles building, copying, and service configuration for you (even if your user isn't 'pi'). If your host or user differs, set `PI_HOST` and `PI_USER` once in `ghost/.env` and keep the deploy command short.

**From your development machine:**

```bash
cd ../ghost/bridge

make deploy
```

### 4. Manual Service Installation (Alternative)

If you prefer to set it up manually:

```bash
# Edit the service file if your username isn't 'pi'
nano ghost-bridge.service

sudo cp ghost-bridge.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable ghost-bridge
sudo systemctl start ghost-bridge

# Check it's running
sudo journalctl -u ghost-bridge -f
```

### 5. Firewall (local network only)

```bash
# Allow only your home network range
sudo ufw allow from 192.168.0.0/16 to any port 8765
sudo ufw reload
```

### 6. Install scrot for screenshots (optional)

```bash
sudo apt install scrot
```

---

## Updating ghost-bridge After Code Changes

```bash
# On your dev machine:
cd ../ghost/bridge

make deploy
```

If the Pi IP changes, update `PI_HOST` in `ghost/.env` and run `make deploy` again.

### 🤖 Running as a Service (Linux/Pi)

To keep Ghost running in the background and auto-start on boot:

1. **Install the binary:**

   ```bash
   sudo cp ghost /usr/local/bin/
   ```

2. **Configure the service:**
   Check `ghost.service` and ensure `User` matches your username (default: `pi`) and paths are correct.

   ```bash
   nano ghost.service
   ```

3. **Enable the service:**

   ```bash
   sudo cp ghost.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable ghost
   sudo systemctl start ghost
   ```

4. **View logs:**
   ```bash
   journalctl -u ghost -f
   ```

### 🔄 Updating the Service

If you modify code or configuration while Ghost is running as a service:

**For Configuration Changes (`.env` or `config.json`):**
Simply restart the service to apply changes:

```bash
sudo systemctl restart ghost
```

**For Code Changes (Go files):**
You must rebuild and replace the binary:

```bash
make install && sudo systemctl restart ghost
```

## 🧠 Memory System

Ghost's memory lives in `ghost.db` (SQLite) and `workspace/memory/`.

- **Structured History**: All conversations are stored in a local SQLite database for fast retrieval.
- **RAG (Retrieval-Augmented Generation)**: Important facts are vectorized and stored in an **HNSW index** (via [chromem-go](https://github.com/philippgille/chromem-go)) for O(log n) semantic search, ensuring fast recall even as memory grows.
- **Episodic**: Daily logs of your conversations in `workspace/memory/`.
- **Reflective**: Periodic summaries generated by Kimi.

## 🤖 Tech Stack

- **Runtime**: Ghost (Go 1.25+)
- **Database**: SQLite (local) + HNSW Vector Index (in-memory)
- **Cognition**: Kimi K2.5, Ollama, or OpenAI (API)
- **Validation**: JSON Schema (santhosh-tekuri/jsonschema)
- **Interface**: Telegram Bot API
- **System**: systemd (Linux Service)

## 📄 License

MIT
