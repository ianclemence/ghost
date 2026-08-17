# Ghost Roadmap

## Overview

This roadmap sequences the work needed to turn Ghost from a developer project into a sellable local-first personal AI appliance. It is aligned to the [Product Strategy](PRODUCT.md): open core, paid managed service, BYO-hardware first, hardware deferred.

Each phase maps to one or more implementation plans under [`plans/`](plans/). The phases build on each other: a phase's plan is only workable once its prerequisites are met.

---

## Phases

| Phase | Goal | Build track | Plan |
|-------|------|-------------|------|
| **0 — Foundation** (mostly done) | The appliance core: web console, auth lifecycle, recovery, security. | — | — |
| **1 — Remote access** | The subscription anchor: the mobile app works from anywhere. | Cloud relay / pairing | [`01-cloud-relay.md`](plans/01-cloud-relay.md) |
| **2 — Install** | A mainstream user can get Ghost onto hardware without flashing a raw image. | Simple install | [`02-install-experience.md`](plans/02-install-experience.md) |
| **3 — Updates** | Ghost ships and installs updates to devices we never touch. | OTA updates | [`03-ota-updates.md`](plans/03-ota-updates.md) |
| **4 — Observability** | We can run updates and support on devices we cannot see. | Opt-in telemetry | [`04-telemetry.md`](plans/04-telemetry.md) |
| **5 — Monetization** | Turn the relay + updates + support into a subscription. | Billing / account | (follows telemetry) |
| **6 — Hardware (optional)** | A physical Ghost bundle. Shares the pipeline; only if demand is proven. | Device bundle | (deferred) |

---

## Phase 0 — Foundation (current)

The appliance core is in place and stable:

- **Web console** (`ghost-web`): setup wizard + admin dashboard on port 80.
- **Admin credential lifecycle**: password creation, change, and reset; recovery mode; session invalidation on change.
- **Security hardening**: `.secrets.json` secrets boundary, atomic config writes, cron command gating.
- **Self-improvement**: learning pipeline, skill drafts, mid-turn steering, recall summarization.
- **Dashboard redesign**: responsive layout, polished cards, skill editor.

**Definition of done for Phase 0:** a clean first-boot-to-dashboard flow on a Pi, with recovery and password reset working. This is largely complete.

**Exit criterion:** the mobile app can pair over LAN (in progress separately).

---

## Phase 1 — Remote access (cloud relay)

**Goal:** the mobile app reaches Ghost from anywhere without port forwarding, and this is the subscription anchor.

**Why now:** the mobile app is the interface for an appliance, and it is useless off the home network without a relay. The relay is also the single component users must pay for, so it must exist before any monetization.

**Plan:** [`01-cloud-relay.md`](plans/01-cloud-relay.md)

**Definition of done:** a user on cellular data can open the app and talk to a Ghost on their home network, end-to-end, via a Ghost-hosted relay. LAN pairing still works as the fallback.

---

## Phase 2 — Simple install

**Goal:** a mainstream user can install Ghost on a Pi (or supported board) without flashing a raw image or using a terminal.

**Why now:** the image is plumbing, not the product. The product requires a trivial install path so the privacy-conscious mainstream — not just hobbyists — can adopt it.

**Plan:** [`02-install-experience.md`](plans/02-install-experience.md)

**Definition of done:** a user can either (a) buy a pre-flashed microSD card, or (b) follow a two-minute flow using an official installer, and reach the setup wizard without touching a terminal.

---

## Phase 3 — OTA updates

**Goal:** Ghost ships and installs updates to devices it never touches, with automatic rollback on failure.

**Why now:** updates are the recurring value that justifies the subscription. A user who cannot update has no reason to keep paying.

**Plan:** [`03-ota-updates.md`](plans/03-ota-updates.md)

**Definition of done:** a device receives, verifies, and installs an update over the relay; if the device fails to boot after the update, it rolls back automatically.

---

## Phase 4 — Opt-in telemetry

**Goal:** we can see the health and version of the devices we support, with explicit user consent.

**Why now:** we cannot run updates and support on devices we cannot see. Telemetry must be in place before the subscription support promise is made.

**Plan:** [`04-telemetry.md`](plans/04-telemetry.md)

**Definition of done:** opted-in devices report version, health, and error counts to the relay; the data is privacy-preserving and removable.

---

## Phase 5 — Monetization

**Goal:** turn relay, updates, and support into a subscription.

**Preconditions:** Phase 1 (relay) and Phase 3 (updates) shipped; Phase 4 (telemetry) gives us visibility.

**Out of scope for now:** billing implementation. The subscription mechanics are deferred until the preceding phases prove demand. See the Product Strategy for the business model.

---

## Phase 6 — Hardware (optional)

**Goal:** a physical Ghost bundle (device + pre-flashed OS + subscription).

**Decision gate:** only if the BYO subscription proves demand. It shares the image, OTA, and relay pipeline, so deferring it costs nothing.

**Out of scope for now.**

---

## Principles that govern the roadmap

1. **The service is the product.** The image and hardware are distribution; the relay, updates, and support are the value.
2. **Local-first is non-negotiable.** Nothing in Phases 1–4 makes the device dependent on the cloud. The relay and updates enhance a local-first product; they never replace it.
3. **Build shared plumbing first.** Relay, image, and OTA are needed by both the BYO path and any future hardware bundle. No work is wasted.
4. **Monetize last.** Ship the value (relay, updates, telemetry) before adding billing.
