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

### Step 3: Walk through the wizard

| Step | What you do |
|------|-------------|
| Welcome | Click "Set up Ghost" |
| Identity | Enter your name (what Ghost calls you) and a name for Ghost (what you call it) |
| Password | Create an owner password (min 8 chars) — stored as a bcrypt hash, never in config |
| Preparing | Ghost configures itself: identity, security, local storage, Ollama AI, system checks |
| Local AI | If Ollama has models, select one. Otherwise skip |
| Cloud AI | Optionally configure cloud AI providers. Skip if not needed |
| Phone | Shows a pairing code for the mobile app (optional at this stage) |
| Done | Click to enter the Web Console |

### Step 4: What happens behind the scenes

- `config.json` and `.secrets.json` are written (secrets never touch `.env`)
- The `.setup-complete` flag is created
- The ghost gateway service starts on port 8766 (LAN-reachable; loopback trusted, LAN requires device credentials)

---

## Connecting a Phone (New Device)

### Option A: Same Network (LAN)

#### Step 1: Open the Web Console

- Go to `http://ghost.local` on any browser
- Log in with your owner password

#### Step 2: Navigate to Devices

- Click "Devices" in the sidebar (under Connections)
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
- The relay tunnels traffic back to your Ghost device (via localhost)

---

## Security Architecture

### Authentication Model

```
                    GHOST POD
                        │
             ┌──────────┴──────────┐
             │                     │
        Owner access          Device access
             │                     │
       Owner password       Device credential
             │                     │
       Web session          Mobile/API/WS
```

### Secrets Storage

```
                  Ghost Secrets Layer
                         │
             ┌───────────┴───────────┐
             │                       │
       .secrets.json              SQLite DB
             │                       │
      provider/channel         device credentials
          secrets              (SHA-256 hashed)
```

- **`.secrets.json`**: Canonical store for API keys, channel tokens
- **`admin.hash`**: bcrypt-hashed owner password
- **SQLite**: device credentials (SHA-256 hashed), pairing tokens (SHA-256 hashed)

### Pairing Flow

```
Web UI
    ↓
temporary pairing invitation (5-minute expiry)
    ↓
QR code (ghost://pair?v=1&pod=...&token=...)
    ↓
mobile app scans QR
    ↓
one-time token exchange
    ↓
device credential issued (shown once, stored in SecureStore)
    ↓
persistent trust established
```

After pairing, the QR/token disappears from the equation.

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

The gateway listens on the LAN with a layered trust model:

- **Loopback peers are trusted** — no credential headers needed:
  - Web proxy forwards requests to `127.0.0.1:8766`
  - Relay client connects to `127.0.0.1:8766`
  - TUI dashboard and CLI connect to `127.0.0.1:8766`
- **LAN peers must present device credentials** on every request:
  - Requests without valid `X-Ghost-Device-ID` + `X-Ghost-Credential` headers are rejected with `authentication_required` / `authentication_failed` errors
  - The WebSocket upgrade enforces the same rule (credentials in headers, never in URLs)
- **One public door**: `POST /v1/pairing/complete` needs no credential headers — the short-lived, single-use pairing token is the authorization

Mobile apps use device credentials:

| Mechanism | Headers | Used By |
|-----------|---------|---------|
| Device Credentials | `X-Ghost-Device-ID` + `X-Ghost-Credential` | Mobile app after pairing (LAN or relay) |
| Client Token | `X-Ghost-Client-Id` + `X-Ghost-Client-Token` | App ↔ relay server (relay-forwarded traffic arrives at the gateway via loopback) |

WebSocket connections use the same device credential mechanism via headers.

---

## Recovery Mode

If Ghost fails to start:

1. Set `GHOST_RECOVERY_MODE=1` environment variable
2. A minimal recovery server starts on port 8766 (**localhost only**)
3. Provides: status, logs, config viewer, config reset, password reset, restart
4. Auto-shutdowns after 15 minutes
5. Can be disabled with a `.recovery-disabled` flag file

Recovery is bound to `127.0.0.1` — it cannot be accessed from other devices on the network.

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
