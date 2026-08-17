# Implementation Plan — OTA Updates

## Objective

Ship and install updates to Ghost devices we never touch, securely and with automatic rollback on failure. This is the recurring value that justifies the subscription.

---

## Product description

A user's Ghost device periodically checks for updates. When one is available, the device downloads and verifies it, installs it, and reboots. If the device fails to boot after the update, it rolls back to the previous version automatically. The user does nothing; the system is self-healing.

---

## Architecture

```
Ghost build pipeline ──sign──> Update server (cloud) ──> device (via relay or direct)
                                                              │
                                                              └── A/B root partitions
```

### Update model: A/B partitions

Two root partitions, `A` and `B`. The device boots one (active) and updates the other (inactive):

- A running device downloads the new image to the inactive partition.
- On reboot, the bootloader switches to the updated partition.
- If the updated system fails to boot within a watchdog window, the bootloader boots the previous partition instead.

This gives a clean rollback with no destructive overwrite of the running system.

---

## Components

### 1. Signed images

Every image produced by the install plan's build pipeline is cryptographically signed. The device verifies the signature before applying an update, so a compromised update server cannot push malicious images.

- Signing key held offline or in a secure HSM.
- Public verification key shipped in the base image.

### 2. Update server (cloud)

- Hosts signed images and a version manifest.
- Serves update metadata the device polls.
- Coordinates with the relay so the device can be notified of an available update (push) or the device can poll (pull).

Endpoints:

| Endpoint | Purpose |
|----------|---------|
| `GET /v1/updates/manifest` | Latest version + image metadata for a given device model. |
| `GET /v1/updates/{version}.img` | Signed image download. |
| `POST /v1/devices/{id}/updates/status` | Device reports update result (success / rollback). |

### 3. Device update agent

Extend the existing `ghost-updater` (currently git-pull based) to the A/B model:

- Poll the update server for the current version.
- On a newer version, verify the signature, download to the inactive partition, and stage it.
- Reboot into the updated partition.
- Report the result back to the update server.

### 4. Bootloader + rollback

- Configure the bootloader to track the active partition and a boot-count / watchdog.
- If the updated partition fails to boot (or the service fails a health check within a window), mark it bad and boot the previous partition.

### 5. Version manifest

A signed manifest lists:

- version,
- supported device models,
- image checksums,
- minimum supported version (for staged rollouts),
- release date.

---

## Phased delivery

### Milestone 1 — Signed images + verification

- Build pipeline signs images.
- Device verifies the signature before install.

**Definition of done:** an image download is verified before any write occurs.

### Milestone 2 — A/B install + reboot

- Update agent downloads to the inactive partition.
- Device reboots into the new partition.

**Definition of done:** an update installs across a reboot with no data loss.

### Milestone 3 — Rollback

- Watchdog detects a failed boot.
- Device boots the previous partition.

**Definition of done:** a deliberately broken update rolls back automatically to a working system.

### Milestone 4 — Rollout management

- Update server stages rollouts (percentage, canary).
- Device reports update status.

**Definition of done:** we can roll out to a canary, observe health, then widen the rollout.

---

## Dependencies

- The install plan's signed image build pipeline.
- The relay plan's connectivity (push notification of updates, status reporting) — optional if the device polls.

---

## Non-goals (this phase)

- Delta/block-level updates. Full-partition images first; deltas are a later optimization.
- Updating the bootloader itself (out of scope for the first pass).
- Billing integration. Updates are free of subscription gating until monetization.

---

## Acceptance criteria

1. A device verifies an image signature before install.
2. An update installs across a reboot without data loss (user memory, config, secrets survive).
3. A failed boot rolls back automatically to the previous version.
4. We can stage a rollout to a canary group and monitor it.
5. The device can recover from a partial download or interrupted update.
