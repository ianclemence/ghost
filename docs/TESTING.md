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

## 2. Slash Commands (Mobile App)

Type these directly into the chat input starting with `/`.

- `/status`: Displays Pi system stats (CPU, Disk, Memory) using `uptime` and `df`.
- `/skills`: Lists all installed skills in `workspace/skills` (34 currently).
- `/tools`: Shows the JSON schemas for all loaded tools (e.g., `exec`, `web_search`, `canvas`).
- `/clear`: Archives the current chat session.
- `/help`: Shows available commands and tool descriptions.

---

## 3. Test Prompts (Aligned with Tools & Skills)

#### Filesystem & System (Tools: `exec`, `read_file`, `write_file`, `list_dir`)

- _"List the files in my ghost workspace."_
- _"What is your current CPU temperature and disk usage?"_
- _"Check if the ghost service is running using a shell command."_
- _"Read the content of GHOST.md and summarize it."_

#### Web & Research (Tools: `web_search`, `web_fetch`, `scraper`)

- _"Search the web for the latest news on NVIDIA and summarize it."_
- _"Scrape the latest headlines from a news site."_
- _"Find the difference between Raspberry Pi 5 and Orange Pi 5."_

#### Specialized Skills (Skills: `weather`, `aqi`, `crypto`, `speedtest`, `currency`)

- _"What's the weather like in Tokyo?"_
- _"Check the current AQI in Singapore."_
- _"What is the current price of Bitcoin?"_
- _"Run a speedtest on my Pi."_
- _"Convert 100 USD to EUR."_

#### Multimedia & Environment (Skills: `camera`, `mobile`, `spotify`, `homeassistant`)

- _"Take a photo using the Pi camera."_
- _"Take a screenshot of my Android phone."_
- _"What's currently playing on my Spotify?"_
- _"Check the status of my home assistant devices."_

#### Productivity & Memory (Tools: `remember`, `oracle`; Skills: `calendar`, `journal`; Command: `/remind`)

- _"Set a reminder to check the logs in 10 minutes."_
- _"Add a note to my memory about my project ideas."_
- _"Search my history for the last time we discussed the ghost-bridge port."_
- _"Check my calendar for upcoming events."_

#### Visuals & Canvas (Tool: `canvas`)

- _"Create a dashboard showing my Pi's CPU and Memory usage with a gauge chart."_
- _"Show me a real-time clock with a futuristic neon design."_
- _"Draw a clean, dark-themed landing page for a personal AI project."_

---

## 4. Prompt Ladder (Simple → Complex)

Use this sequence to progressively stress-test Ghost from basic response quality to deep autonomous behavior.

#### Level 1 — Simple Sanity Checks

- _"Ping test: reply with one sentence confirming you are online."_
- _"What tools do you have right now for filesystem, web, and shell access?"_
- _"Summarize your current mission in 3 bullet points."_

#### Level 2 — Practical Daily Tasks

- _"Check current CPU temperature, RAM usage, and disk status, then give a quick health summary."_
- _"List recent files in my workspace and tell me which ones changed most recently."_
- _"Read GHOST.md and explain the top 5 operating rules in plain English."_

#### Level 3 — Research Quality Tests

- _"Find the latest Raspberry Pi 5 performance tuning recommendations and summarize with citations in this format: [Source: Institution / Article Title]."_
- _"Compare Tailscale vs WireGuard for remote Pi access. Include trade-offs and one final recommendation."_
- _"Give me a short report using: phenomenon → cause → impact → solution."_

#### Level 4 — Reasoning + Planning

- _"/think Diagnose why a mobile app can connect in Expo Go but fail in a preview build on the same network. Rank top 3 likely causes and verification steps."_
- _"Create a prioritized hardening plan for my Ghost deployment: quick wins, medium effort, and long-term upgrades."_
- _"Identify 3 operational risks in my current setup and mark each key finding with 【Insight】."_

#### Level 5 — Agentic End-to-End Scenarios

- _"Act as my SRE assistant: run a full connectivity investigation plan, then provide an executive incident summary and prevention checklist."_
- _"Prepare a mini research brief on self-hosted AI agent security best practices with citations and a practical action list for this Pi."_
- _"Design a weekly maintenance runbook for Ghost (health checks, backups, updates, logs, security), optimized for low downtime."_

#### Level 6 — Reporting Workflow (Research Skill + LaTeX)

- _"Use the research skill workflow to draft a professional report outline on AI agents for small businesses. Include section headers and source plan."_
- _"Generate a LaTeX-ready report draft with executive summary, analysis, recommendations, and references."_
- _"Produce a concise decision memo: should I run Ghost as local-only, Tailscale-only, or public with reverse proxy + TLS?"_

---

## 5. Pairing & Device Auth

Test the secure pairing flow and per-device authentication.

### Pairing Flow

The recommended pairing flow is through the web admin dashboard:

1. Open `http://<pi-ip>` on any browser
2. Log in with admin password
3. Navigate to Devices → "Connect another device"
4. Scan the QR code with the Ghost app
5. Verify the device appears in the Devices list

### Pairing API (for testing)

```bash
# Create pairing invitation (gateway binds to localhost only)
curl -s -X POST http://localhost:8766/v1/pairing/invitations \
  -H "Content-Type: application/json" \
  -d '{"display_name": "Test Phone", "transport": "lan"}'

# Complete pairing (PUBLIC — no auth required)
curl -s -X POST http://localhost:8766/v1/pairing/complete \
  -H "Content-Type: application/json" \
  -d '{"token": "<token>", "display_name": "Test Phone", "platform": "android"}'

# List paired devices (gateway binds to localhost only)
curl -s http://localhost:8766/v1/pairing/devices

# Revoke a device (gateway binds to localhost only)
curl -s -X POST http://localhost:8766/v1/pairing/revoke \
  -H "Content-Type: application/json" \
  -d '{"device_id": "<device_id>"}'
```

### Structured Error Responses

Pairing endpoints return structured errors:

```json
{
  "error": {
    "code": "pairing_expired",
    "message": "Pairing invitation expired."
  }
}
```

Error codes:
- `pairing_invalid` — token not found
- `pairing_expired` — token expired (>5 minutes)
- `pairing_consumed` — token already used
- `pairing_rejected` — pairing rejected by server
- `authentication_required` — no credentials provided
- `authentication_failed` — invalid credentials
- `device_revoked` — device has been revoked
- `device_not_found` — device ID not found

### Device Auth (after pairing)

```bash
# Health check with device credentials
curl -s http://localhost:8766/v1/health \
  -H "X-Ghost-Device-ID: <device_id>" \
  -H "X-Ghost-Credential: <credential>"

# WebSocket with device credentials (headers only, no query params)
wscat -c "ws://localhost:8766/v1/ws" \
  -H "X-Ghost-Device-ID: <device_id>" \
  -H "X-Ghost-Credential: <credential>" \
  -H "X-Ghost-Session: mobile:default"
```

**Note:** WebSocket connections must use headers for authentication, not query parameters. Query parameters are not supported for security reasons.

---

## 6. Security Testing

### Verify Secrets Are Not Exposed

```bash
# Check that config.json does NOT contain API keys
grep -i "api_key\|token\|secret" /var/ghost/config/config.json

# Check that .secrets.json exists with correct permissions
stat -c "%a %U %G" /var/ghost/config/.secrets.json

# Check that admin.hash exists with correct permissions
stat -c "%a %U %G" /var/ghost/data/admin.hash
```

### Verify Directory Permissions

```bash
# Check directory permissions
ls -la /var/ghost/          # Should show 0700 for config/ and data/
ls -la /var/ghost/config/   # Should show 0700
ls -la /var/ghost/data/     # Should show 0700
```

### Verify Recovery Mode is Localhost-Only

```bash
# Start recovery mode
sudo GHOST_RECOVERY_MODE=1 ghost gateway &

# From another device on the network (should FAIL):
curl http://<pi-ip>:8766/api/status

# From the Pi itself (should SUCCEED):
curl http://127.0.0.1:8766/api/status
```

### Verify Password Policy

```bash
# Try to set a short password (should fail)
curl -s -X POST http://localhost:8766/api/admin/password \
  -H "Cookie: ghost_admin_session=<session>" \
  -H "Content-Type: application/json" \
  -d '{"current": "current-pass", "new": "short", "confirm": "short"}'

# Should return error about minimum 8 characters
```

---

## 7. Troubleshooting Commands

- `sudo lsof -i :8766`: Check if the Internal API port is occupied.
- `sudo fuser -k 8766/tcp`: Force close any process hogging the API port.
- `pkill -9 ghost`: Force kill any "zombie" ghost processes.
