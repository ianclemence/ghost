# Implementation Plan — Opt-in Telemetry

## Objective

See the health and version of the devices we support, with explicit user consent, so we can run updates and support on devices we cannot physically access.

Telemetry is **opt-in and privacy-preserving**. It must never send user content, memory, or conversation data.

---

## Product description

A user opts in during setup (or later in the dashboard). Their device then reports non-identifying operational data to the Ghost service: version, health metrics, error counts, and update status. The user can disable it at any time, and it is clearly described as improving updates and support.

---

## What we collect (and what we never collect)

### We collect

- Device model / board type.
- Ghost version and build.
- Uptime and last boot reason.
- Health metrics: CPU, memory, disk usage (aggregated, sampled).
- Error counts and the names of failed services (no log bodies).
- Update status and result.
- Optional: model sizes installed (for capacity planning), region.

### We never collect

- Chat or conversation content.
- User memory, profile, or knowledge.
- File names or paths.
- Anything that identifies the content of what the user does.
- Credentials or secrets.

### Privacy controls

- Opt-in by default off.
- Clear, plain-language description of what is sent and why.
- One-click disable that stops all transmission.
- Local data is never used to train models.
- Consent and data are tied to the device credential, not to personal identity where avoidable.

---

## Architecture

```
Ghost device ──> local telemetry collector ──> relay ──> telemetry ingest (cloud)
```

### 1. Device telemetry collector

A component (part of the gateway or a small daemon) that:

- Samples system health on a schedule (e.g. every 10 minutes) and on significant events (boot, update, service failure).
- Batches events and sends them to the relay/ingest when the device is online.
- Buffers locally if offline and retries.
- Obeys the opt-in flag; sends nothing when disabled.

### 2. Telemetry ingest (cloud)

- Receives batched telemetry from devices.
- Stores it in a time-series store (or SQLite/Postgres) keyed by device ID.
- Exposes dashboards for device health, version spread, and error trends.

### 3. Consent storage

- The opt-in flag is stored on-device (in `.secrets.json` or config) and mirrored to the service.
- The service honors the flag; no telemetry is accepted from a device that has not opted in.

---

## Privacy & data handling

- **Minimization:** collect only what is needed for updates and support.
- **Purpose limitation:** telemetry is used for reliability, not advertising or model training.
- **Retention:** telemetry is retained for a bounded window (e.g. 30 days) and then purged, unless the user has a support case.
- **Deletion:** disabling telemetry stops collection; a "delete my telemetry" action removes stored data.

---

## Phased delivery

### Milestone 1 — Local collection

- Device samples health and version, honors the opt-in flag.
- Events are logged locally.

**Definition of done:** a device produces a local telemetry log only when opted in.

### Milestone 2 — Ingestion

- Relay forwards telemetry to the ingest service.
- Ingest stores events by device.

**Definition of done:** opted-in devices appear in a simple device-health view.

### Milestone 3 — Dashboards + control

- Dashboards for version spread, health, and errors.
- User-facing toggle and deletion.

**Definition of done:** we can see fleet health and a user can fully opt out and delete their data.

---

## Dependencies

- The relay plan (transport).
- The OTA plan (update status reporting).

---

## Non-goals (this phase)

- Content-level analytics or model training on user data.
- Automatic alerts (follows dashboards).
- Billing integration.

---

## Acceptance criteria

1. No telemetry is sent unless the user opts in.
2. Telemetry never contains conversation content, memory, or credentials.
3. Disabling telemetry stops all transmission immediately.
4. A user can request deletion of stored telemetry.
5. We can see version spread and error trends across opted-in devices.
