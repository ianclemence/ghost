# Implementation Plan — Cloud Relay / Pairing

## Objective

Allow the mobile app to reach a Ghost device from anywhere, without port forwarding or a VPN on the user's network, and establish the relay as the paid subscription anchor.

This plan covers the in-house relay service and the device-side relay client. It is **LAN-first**: LAN connectivity must keep working and remain the fallback. The relay augments, never replaces, local access.

---

## Product description

A user installs Ghost at home. From their phone, while away from the home network, they open the Ghost app and talk to their assistant. The app connects through a Ghost-hosted relay that bridges to the device over a persistent outbound connection the device established at boot. No router configuration is required.

The user experience is a single "pair with QR code" step during setup. After pairing, remote access "just works" for subscribed accounts; LAN access works for everyone.

---

## Architecture

```
Mobile app ──TLS──> Ghost Relay (cloud) <──persistent outbound TLS── Ghost device
                       │
                       └── device registry, auth, message forwarding
```

- **Relay (cloud):** a server that maintains a registry of online devices and forwards messages between a paired app and the device.
- **Device client:** a small daemon (`ghost-relay`) that opens and maintains an outbound TLS connection to the relay at boot, so no inbound port is needed.
- **Pairing:** QR-code exchange during setup binds a phone (and its account) to a device.

### Why outbound-only

The device establishes an outbound connection to the relay and keeps it open. This avoids opening inbound ports on the home router and works behind NAT. The relay can push messages to the device over the open connection.

### Transport

- **WebSocket over TLS** for device ↔ relay and app ↔ relay. Both ends already speak WebSocket (`/v1/ws` exists on the device). Reuse the same framing.
- **Application layer:** the existing Ghost internal API messages (chat, memory, voice, steering) are carried verbatim through the relay.

---

## Components

### 1. Relay server (cloud)

A Go service. Responsibilities:

- Accept persistent outbound connections from devices.
- Accept connections from apps.
- Bind a device to its paired apps.
- Forward messages between bound app and device.
- Maintain device presence (online/offline, version, last seen).
- Authenticate both ends.

#### Endpoints

| Endpoint | Purpose |
|----------|---------|
| `POST /v1/devices/register` | Device registers with a device credential on first boot. |
| `POST /v1/devices/{id}/connect` | Device opens the persistent WebSocket. |
| `POST /v1/apps/{deviceId}/connect` | App opens a WebSocket for a bound device. |
| `POST /v1/devices/{id}/bind` | Pair an app/account to a device (after QR scan). |
| `POST /v1/pairing/generate` | Issue a short-lived pairing token shown as a QR. |
| `POST /v1/pairing/redeem` | App redeems the token and receives a device binding. |

#### Storage

- Device registry (device ID, credential hash, owner, last seen).
- Pairing bindings (device ↔ account).
- Connection state (in memory).

Database choice: SQLite for the registry (consistent with the rest of the project) with a small schema. Connection state stays in memory.

### 2. Device relay client (`ghost-relay`)

A daemon on the device. Responsibilities:

- On boot, read the relay config (relay URL + device credential from `.secrets.json`).
- Open and maintain the outbound WebSocket; reconnect with backoff on drop.
- Receive messages and dispatch them to the local agent loop (same path as `/v1/chat`).
- Send responses back over the socket.
- Heartbeat to report presence.

It is a new binary (`cmd/ghost-relay`) installed as a systemd service, `ghost-relay.service`. It depends on `ghost.service` (the gateway) being up.

### 3. Device credentials

A device credential is generated at install/setup and stored in `.secrets.json` (the existing secrets boundary). It identifies the device to the relay and is used to authenticate the persistent connection.

### 4. Pairing flow

1. During setup, the web console shows a QR code encoding a short-lived pairing token (from `POST /v1/pairing/generate`).
2. The mobile app scans the QR and redeems it (`POST /v1/pairing/redeem`), receiving the device ID and a long-lived app credential.
3. The relay binds the app's account to the device.
4. The app connects through the relay.

---

## Security

- **TLS everywhere.** Device ↔ relay and app ↔ relay are TLS.
- **Device credential** proves the device identity to the relay (from `.secrets.json`, `0600`).
- **Pairing token** is single-use and short-lived (e.g. 10 minutes).
- **App credential** is long-lived and revocable; revoking a binding disconnects the app.
- **No inbound ports** on the home network.
- **Message authorization:** the relay forwards a message only if the sender is bound to the target device.

---

## Phased delivery

### Milestone 1 — Minimal relay (proof of connectivity)

- Relay server accepts a device connection and an app connection.
- Device client connects and stays connected.
- App sends a message; relay forwards to device; device replies; app receives.

**Definition of done:** end-to-end message round-trip over the relay, LAN still working.

### Milestone 2 — Pairing

- QR token generate/redeem.
- Device registry and bindings.
- App↔device authorization.

**Definition of done:** a user pairs a new phone by scanning the QR during setup and talks to the device remotely.

### Milestone 3 — Robustness

- Reconnect/backoff, presence, heartbeat.
- Revocation of bindings.
- Operational logging.

**Definition of done:** a device survives network changes and app bindings can be revoked from the dashboard.

---

## Dependencies

- The device already exposes `/v1/ws`; the relay reuses the same message framing on the device side.
- Secrets boundary (`.secrets.json`) exists; device credentials slot into it.
- The mobile app is being built separately and must implement the relay client and QR pairing.

---

## Non-goals (this phase)

- Billing / subscription enforcement. The relay is built first; charging is deferred to the monetization phase.
- Tailscale-style full mesh. This is a central relay, not a P2P mesh.
- LAN fallback rework. LAN connectivity is unchanged and remains the default.

---

## Acceptance criteria

1. A device behind a home NAT connects to the relay at boot with no router configuration.
2. A paired app on cellular data sends a message and receives the assistant's response.
3. LAN access still works with the relay disabled or unreachable.
4. Unbinding an app revokes its access.
5. The relay never requires an inbound port on the home network.
