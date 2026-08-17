# Implementation Plan — Simple Install

## Objective

Let a privacy-conscious mainstream user install Ghost on a supported board without flashing a raw image or using a terminal, so the product reaches people who will not tinker.

The image is plumbing, not the product. This plan makes the image invisible to the user.

---

## Product description

A user wants a personal AI on a Raspberry Pi. They have two options:

1. **Buy a pre-flashed microSD card** from the Ghost store (with Ghost baked in). They insert it, power on, and the setup wizard appears.
2. **Use the official installer.** They download the Ghost app or a small desktop tool, pick their board, and it writes the card and hands off to the wizard.

In both cases the user's next step is the same: power on, connect to the `Ghost-Setup-XXXX` hotspot, and complete the wizard. No terminal, no flashing, no router configuration.

---

## Install paths

### Path A — Pre-flashed microSD (primary, for the non-technical buyer)

- Ghost sells microSD cards pre-written with the Ghost OS image.
- The user inserts the card and powers on.
- The card boots straight into the setup wizard.

### Path B — Official installer (for the owner who wants to use their own card)

- A desktop installer (or a flow inside the mobile app) that:
  - detects the board,
  - downloads and verifies the Ghost OS image,
  - writes it to a microSD card,
  - verifies the write.
- Delivered as:
  - an **official Raspberry Pi Imager entry**, and
  - a **`ghost-install` CLI/desktop tool** for cross-platform use.

### Path C — Existing developer path (unchanged)

- The current `make install-ghost` / `ghost update` path remains for developers and advanced users.

---

## Components

### 1. Ghost OS image (build pipeline)

A reproducible build that produces a bootable image with Ghost pre-baked:

- Base: minimal Debian (ARM64) or Raspberry Pi OS lite.
- Ghost appliance layer:
  - `ghost` (gateway) and `ghost-web` (web console) binaries.
  - `ghost-updater` and the OTA client.
  - Ollama pre-installed with a default model.
  - systemd services: `ghost.service`, `ghost-web.service`, and later `ghost-relay.service` (see relay plan).
  - Read-only root with a writable overlay for user data at `/var/ghost`.
  - `ghost.local` (mDNS) so the device is discoverable.
  - WiFi hotspot + captive portal for first-boot setup.

The build is produced by a `ghost-os-build` pipeline (Docker-based), versioned, and signed. The signing is required by the OTA plan.

### 2. First-boot wizard (exists, verify)

The web console already runs the setup wizard on first boot: WiFi, admin password, model, pairing. This plan verifies and hardens it for a non-technical user (clear copy, captive-portal redirect, no terminal steps).

### 3. Pre-flashed card fulfillment

- A process to image cards at volume (a small imaging rig or a third-party fulfillment partner).
- Cards ship with a card-specific identifier so the user can be supported.
- This is operational, not code, but it needs the signed image from step 1.

### 4. Official installer

- **Raspberry Pi Imager entry:** a custom OS entry that points at the Ghost OS image URL. Users see "Ghost" in the Imager list.
- **`ghost-install` tool:** a small cross-platform Go/desktop app that reproduces the Imager flow for any board and writes the card.

---

## Phased delivery

### Milestone 1 — Reproducible image build

- `ghost-os-build` produces a bootable, signed Pi 5 image containing Ghost.
- The image boots to the setup wizard.

**Definition of done:** a developer can build the image, flash it to a Pi, and complete setup to the dashboard.

### Milestone 2 — Imager entry + installer

- Raspberry Pi Imager entry for Ghost.
- `ghost-install` desktop/CLI tool.

**Definition of done:** a user writes the Ghost image to a card using the official installer, no terminal required.

### Milestone 3 — Pre-flashed fulfillment

- Cards are imaged and shipped.
- A card identifier links the user to support.

**Definition of done:** a buyer inserts a shipped card and completes setup without any other step.

---

## Dependencies

- The web console and wizard already exist and are consumer-ready.
- The signed image is the prerequisite for the OTA plan.
- The relay plan's device credential can be baked into the image or generated at setup.

---

## Non-goals (this phase)

- A bespoke Ghost-branded hardware device. This is BYO-hardware installation; hardware comes later.
- Making the developer `make install` path the primary path.
- Windows/macOS desktop installers beyond the Imager and `ghost-install` tool.

---

## Acceptance criteria

1. The image builds reproducibly from a single command and is signed.
2. A user completes install-to-dashboard without using a terminal.
3. Both a pre-flashed card and the official installer reach the same setup wizard.
4. The developer `make install` path still works.
5. The image boots to the wizard with no configuration.
