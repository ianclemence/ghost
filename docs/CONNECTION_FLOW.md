# Connection Flow

How users set up Ghost for the first time and connect devices.

---

## First-Time Setup

### Step 1: Power on the Ghost device

- The `ghost-web` service starts on port 80
- The `ghost` gateway refuses to start until setup is done
- A setup-complete flag file (`/var/ghost/.setup-complete`) does not exist yet

### Step 2: Open a browser on any device on the same network

- Navigate to `http://ghost.local` (or the device's IP address, e.g. `http://192.168.0.104`)
- The setup wizard loads

### Step 3: Walk through the wizard (8 steps)

| Step | What you do |
|------|-------------|
| Welcome | Click "Set up Ghost" |
| Identity | Enter your name (what Ghost calls you) and a name for Ghost (what you call it) |
| Password | Create an admin password (min 8 chars) — stored as a bcrypt hash, never in config |
| Preparing | Ghost configures itself: identity, security, local storage, Ollama AI, system checks |
| Local AI | If Ollama has models, select one. Otherwise skip |
| Cloud AI | Optionally configure OpenAI/Anthropic/Kimi API keys. Skip if not needed |
| Phone | Shows a pairing code for the mobile app (optional at this stage) |
| Done | Click to enter the admin dashboard |

### Step 4: What happens behind the scenes

- A 32-byte bridge secret is generated (the master key)
- `.env`, `config.json`, and `.secrets.json` are written
- The `.setup-complete` flag is created
- The ghost gateway service starts on port 8766

---

## Connecting a Phone (New Device)

### Option A: Same Network (LAN)

#### Step 1: Open the web dashboard

- Go to `http://ghost.local` on any browser
- Log in with your admin password

#### Step 2: Navigate to Devices

- Click "Devices" in the sidebar
- Click "Connect another device"

#### Step 3: Scan the QR code

- The web UI shows a `ghost://pair?v=1&pod=...&transport=lan&token=...` code
- Open the Ghost app on your phone
- Scan or enter this code (expires in 5 minutes)

#### Step 4: What happens behind the scenes

- The app sends the token to `POST /v1/pairing/complete`
- Ghost validates the token (atomic delete to prevent replay)
- Ghost generates a `device_id` (24 hex) and `credential` (64 hex)
- The credential is shown to the app **once** and stored in SecureStore
- All future requests use `X-Ghost-Device-ID` + `X-Ghost-Credential` headers

#### Step 5: You're connected

- The phone now appears in the Devices list
- You can chat with Ghost, use voice, access memory, etc.

### Option B: Remote (Relay)

If the phone is NOT on the same network:

#### Step 1: Run the CLI on the Ghost device

```
ghost relay pair
```

#### Step 2: It outputs a URI

```
ghost://connect?transport=relay&relay=<server>&ghost=<ghostId>&token=<token>
```

#### Step 3: Open this URI on the phone

- The Ghost app connects to the relay server
- The relay tunnels traffic back to your Ghost device
- The bridge secret is injected locally — the relay never sees it

---

## How the Bridge Secret Ties It Together

```
Setup generates it → stored in .env + .secrets.json
         ↓
Gateway reads it → validates X-Ghost-Secret header
         ↓
Web UI proxy reads it → injects into requests server-side (browser never sees it)
         ↓
Mobile app never gets it → uses device_id + credential instead
         ↓
Relay server never gets it → stripped before forwarding
```

The security boundary is deliberate:

- **config.json** can be read by the process but never sent over the network
- **.secrets.json** has 0600 permissions (owner-only read)
- The relay server strips `X-Ghost-Secret` headers before forwarding
- The web proxy reads the secret server-side and injects it into proxied requests

---

## What the Web UI Is Used For vs the Mobile App

| Web UI | Mobile App |
|--------|------------|
| First-time setup wizard | Daily conversation |
| Admin dashboard (config, skills, channels, backups) | Voice chat |
| Device management (pair/unpair) | Quick commands |
| System monitoring and diagnostics | Notifications |
| Skill creation and editing | On-the-go access |

The web UI is the **control center**. The mobile app is the **daily driver**.

---

## Authentication Layers

The gateway uses three authentication layers:

| Layer | Headers | Used By |
|-------|---------|---------|
| Bridge Secret | `X-Ghost-Secret` or `Authorization: Bearer <secret>` | ghost-web proxy, relay client, CLI tools |
| Device Credentials | `X-Ghost-Device-ID` + `X-Ghost-Credential` | Mobile app after pairing |
| Public | None (token itself is authorization) | Pairing completion only |

WebSocket connections support both auth methods via query params or headers.

---

## Recovery Mode

If Ghost fails to start:

1. Set `GHOST_RECOVERY_MODE=1` environment variable
2. A minimal recovery server starts on port 8766
3. Provides: status, logs, config viewer, config reset, password reset, restart
4. Auto-shutdowns after 15 minutes
5. Can be disabled with a `.recovery-disabled` flag file

---

## CLI Interface

The `ghost` binary provides these commands:

| Command | Purpose |
|---------|---------|
| `ghost onboard` | Initialize config and workspace |
| `ghost agent` | Interactive chat or single message |
| `ghost gateway` | Start the full gateway (API + channels + cron + heartbeat) |
| `ghost relay run` | Connect to relay server for remote access |
| `ghost relay pair` | Generate pairing token for phone |
| `ghost relay clients` | List paired clients |
| `ghost relay revoke <token>` | Revoke client access |
| `ghost skills` | Manage skills (list, install, remove, sync) |
| `ghost cron` | Manage scheduled tasks |
| `ghost state export/import` | Portable Ghost State archives |
| `ghost auth login/logout` | OAuth for AI providers |
| `ghost reset-password --force` | Reset admin dashboard password |
