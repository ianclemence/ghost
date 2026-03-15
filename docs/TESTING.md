# Ghost Testing Guide

This document contains useful commands and prompts to test the Ghost system (Pi and Mobile App).

## 1. System Commands (Pi Terminal)

Run these from the `~/ghost` directory on your Pi.

### Basic Connectivity

- `ghost agent -m "ping"`: Quick test of the agent loop.
- `ghost gateway`: Starts the full API server (for phone connection).
- `sudo systemctl status ghost`: Check if the background service is healthy.

### Monitoring

- `ghost dashboard`: Launch the terminal UI dashboard.
- `sudo journalctl -u ghost -f`: Follow real-time logs (useful for watching tool usage).

### Development

- `make build`: Recompile the binary for ARM64.
- `make install`: Install the binary to `~/.local/bin/ghost`.

---

### 2. Slash Commands (Mobile App)

Type these directly into the chat input starting with `/`.

- `/status`: Displays Pi system stats (CPU, Disk, Memory).
- `/skills`: Lists all installed skills in your workspace.
- `/tools`: Shows the raw JSON schemas for all loaded tools.
- `/clear`: Archives the current chat session.
- `/help`: Shows available commands and tool descriptions.

---

### 3. Test Prompts (AI Capabilities)

Use these to verify that the agent's reasoning and tools are working.

#### Filesystem & Tools

- _"What's in my ghost workspace?"_
- _"Read the content of GHOST.md and summarize it."_
- _"Take a screenshot of the Pi desktop and show me."_

#### Web & Information

- _"Search the web for the latest Raspberry Pi news."_
- _"Check the weather in Tokyo."_
- _"What is the current AQI in Singapore?"_
- _"Find the latest stock price for NVIDIA and summarize the sentiment."_
- _"Research the difference between Raspberry Pi 5 and Orange Pi 5."_
- _"Browse the news for any breakthroughs in AI from the last 24 hours."_

#### Logic & Automation

- _"Set a reminder to check the logs in 10 minutes."_
- _"Write a bash script to list all running python processes and execute it."_
- _"Summarize my recent memory entries."_
- _"Create a new markdown file in my workspace called 'ideas.md' and add 5 project ideas for Ghost."_
- _"Analyze the disk usage on my Pi and suggest which directories are taking up the most space."_
- _"Search my history for the last time we discussed the ghost-bridge port."_
- _"Check if any services are currently failing on the system using `systemctl --failed`."_
- _"Write a python script to calculate the first 10 prime numbers and run it."_

---

### 4. Troubleshooting Commands

- `sudo lsof -i :8766`: Check if the Internal API port is occupied.
- `sudo fuser -k 8766/tcp`: Force close any process hogging the API port.
- `pkill -9 ghost`: Force kill any "zombie" ghost processes.
