# Ghost Testing Guide

This document contains useful commands and prompts to test the Ghost system based on currently loaded **Tools** and **Skills**.

## 1. System Commands (Pi Terminal)

Run these from the `~/ghost` directory on your Pi.

### Basic Connectivity
- `ghost agent -m "ping"`: Quick test of the agent loop.
- `ghost gateway`: Starts the full API server (for phone connection).
- `sudo systemctl status ghost`: Check if the background service is healthy.

### Monitoring
- `ghost dashboard`: Launch the terminal UI dashboard.
- `sudo journalctl -u ghost -f`: Follow real-time logs (essential for watching tool usage).

### Development
- `make build`: Recompile the binary for ARM64.
- `sudo cp build/ghost-linux-arm64 /usr/local/bin/ghost && sudo systemctl restart ghost`: Apply code changes.

---

### 2. Slash Commands (Mobile App)

Type these directly into the chat input starting with `/`.

- `/status`: Displays Pi system stats (CPU, Disk, Memory) using `uptime` and `df`.
- `/skills`: Lists all 28+ installed skills from `workspace/skills`.
- `/tools`: Shows the raw JSON schemas for all 24+ loaded tools (e.g., `exec`, `web_search`).
- `/clear`: Archives the current chat session.
- `/help`: Shows available commands and tool descriptions.

---

### 3. Test Prompts (Aligned with Tools & Skills)

#### Filesystem & System (Tools: `exec`, `read_file`, `write_file`, `list_dir`)
- *"List the files in my ghost workspace."*
- *"What is your current CPU temperature and disk usage?"*
- *"Check if the ghost service is running using a shell command."*
- *"Read the content of GHOST.md and summarize it."*

#### Web & Research (Tools: `web_search`, `web_fetch`, `scraper`)
- *"Search the web for the latest news on NVIDIA and summarize it."*
- *"Scrape the latest headlines from a news site."*
- *"Find the difference between Raspberry Pi 5 and Orange Pi 5."*

#### Specialized Skills (Skills: `weather`, `aqi`, `crypto`, `speedtest`, `currency`)
- *"What's the weather like in Tokyo?"*
- *"Check the current AQI in Singapore."*
- *"What is the current price of Bitcoin?"*
- *"Run a speedtest on my Pi."*
- *"Convert 100 USD to EUR."*

#### Multimedia & Environment (Skills: `camera`, `screenshot`, `spotify`, `homeassistant`)
- *"Take a screenshot of the Pi desktop."*
- *"Take a photo using the Pi camera."*
- *"What's currently playing on my Spotify?"*
- *"Check the status of my home assistant devices."*

#### Productivity & Memory (Tools: `remember`, `oracle`; Skills: `calendar`, `journal`, `remind`)
- *"Set a reminder to check the logs in 10 minutes."*
- *"Add a note to my memory about my project ideas."*
- *"Search my history for the last time we discussed the ghost-bridge port."*
- *"Check my calendar for upcoming events."*

#### Visuals & Canvas (Tool: `canvas`)
- *"Create a dashboard showing my Pi's CPU and Memory usage with a gauge chart."*
- *"Show me a real-time clock with a futuristic neon design."*
- *"Draw a clean, dark-themed landing page for a personal AI project."*

---

### 4. Troubleshooting Commands
- `sudo lsof -i :8766`: Check if the Internal API port is occupied.
- `sudo fuser -k 8766/tcp`: Force close any process hogging the API port.
- `pkill -9 ghost`: Force kill any "zombie" ghost processes.
